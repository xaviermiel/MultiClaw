package ethx

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// Sender sends the oracle's transactions. Both services share one Sender, so
// their transactions get consecutive nonces. TxSender signs and sends them;
// DryRunSender only simulates and logs them.
type Sender interface {
	// Address is the oracle address the transactions come from.
	Address() common.Address
	// Send submits a call of data to `to` without waiting for it to be mined.
	Send(ctx context.Context, to common.Address, data []byte) (*SentTx, error)
	// Wait blocks until tx is mined and returns its receipt, which may
	// record a revert.
	Wait(ctx context.Context, tx *SentTx) (*types.Receipt, error)
}

// SentTx is a submitted transaction.
type SentTx struct {
	Hash  common.Hash
	From  common.Address
	To    common.Address
	Data  []byte
	Nonce uint64

	// simulatedAt is the block a dry-run transaction was simulated against,
	// and revert why that simulation reverted ("" if it did not).
	simulatedAt uint64
	revert      string
}

// TxBackend is what TxSender needs from the RPC client.
type TxBackend interface {
	ChainID(ctx context.Context) (*big.Int, error)
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
	NonceAt(ctx context.Context, account common.Address, block *big.Int) (uint64, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	LatestBlockInfo(ctx context.Context) (BlockInfo, error)
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	TransactionByHash(ctx context.Context, hash common.Hash) (tx *types.Transaction, isPending bool, err error)
	TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error)
}

// SenderOptions tunes a TxSender. Zero values select the defaults.
type SenderOptions struct {
	Logf logx.Logf
	// ReceiptTimeout bounds Wait (default 2m, the TS spending oracle's
	// receipt timeout; its Safe value oracle used viem's 3m).
	ReceiptTimeout time.Duration
	// PollInterval is how often Wait polls for the receipt (default 2s).
	PollInterval time.Duration
}

func (o *SenderOptions) setDefaults() {
	if o.Logf == nil {
		o.Logf = logx.Discard
	}
	if o.ReceiptTimeout <= 0 {
		o.ReceiptTimeout = 2 * time.Minute
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 2 * time.Second
	}
}

// maxNonceResyncs bounds how often one Send resyncs its nonce (the TS
// retried up to 5 times).
const maxNonceResyncs = 5

// TxSender signs EIP-1559 transactions with a fixed gas limit and manages the
// nonce locally: it starts from the pending nonce, counts up per transaction
// and resyncs when the node rejects a nonce, or from the latest block after a
// transaction is not mined in time (see Wait). (The TS re-read the latest
// nonce before every send and slept 2s on nonce errors, so back-to-back
// transactions waited for each other to be mined.)
type TxSender struct {
	backend  TxBackend
	key      *ecdsa.PrivateKey
	from     common.Address
	chainID  *big.Int
	signer   types.Signer
	gasLimit uint64
	opts     SenderOptions

	mu    sync.Mutex
	nonce *uint64 // next nonce to use; nil until synced
	// fromLatest makes the next sync read the latest block's nonce instead
	// of the pending one.
	fromLatest bool
}

// NewTxSender returns a sender for key on chain chainID, which the backend
// must serve.
func NewTxSender(ctx context.Context, backend TxBackend, key *ecdsa.PrivateKey, chainID, gasLimit uint64, opts SenderOptions) (*TxSender, error) {
	opts.setDefaults()
	got, err := backend.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("get chain ID: %w", err)
	}
	if !got.IsUint64() || got.Uint64() != chainID {
		return nil, fmt.Errorf("RPC serves chain %s, not the configured chain %d", got, chainID)
	}
	id := new(big.Int).SetUint64(chainID)
	return &TxSender{
		backend:  backend,
		key:      key,
		from:     crypto.PubkeyToAddress(key.PublicKey),
		chainID:  id,
		signer:   types.LatestSignerForChainID(id),
		gasLimit: gasLimit,
		opts:     opts,
	}, nil
}

// Address returns the key's address.
func (s *TxSender) Address() common.Address { return s.from }

// Send signs and submits a call of data to `to`. Sends are serialized, so
// concurrent callers get consecutive nonces.
func (s *TxSender) Send(ctx context.Context, to common.Address, data []byte) (*SentTx, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tip, feeCap, err := s.fees(ctx)
	if err != nil {
		return nil, err
	}
	for resyncs := 0; ; resyncs++ {
		if s.nonce == nil {
			n, err := s.syncNonce(ctx)
			if err != nil {
				return nil, err
			}
			s.nonce = &n
		}
		tx, err := types.SignNewTx(s.key, s.signer, &types.DynamicFeeTx{
			ChainID:   s.chainID,
			Nonce:     *s.nonce,
			GasTipCap: tip,
			GasFeeCap: feeCap,
			Gas:       s.gasLimit,
			To:        &to,
			Data:      data,
		})
		if err != nil {
			return nil, fmt.Errorf("sign transaction: %w", err)
		}

		err = s.backend.SendTransaction(ctx, tx)
		if err == nil || s.alreadySent(ctx, tx, err) {
			*s.nonce++
			return &SentTx{Hash: tx.Hash(), From: s.from, To: to, Data: data, Nonce: tx.Nonce()}, nil
		}
		switch {
		case errorContains(err, nonceRejections) && resyncs < maxNonceResyncs:
			s.opts.Logf("Nonce %d rejected (%v), resyncing from the pending nonce", *s.nonce, err)
			s.nonce = nil
			continue
		case !IsTxPoolRejection(err):
			// Transport failure or cancellation: the node may have taken the
			// transaction anyway, so the next send resyncs.
			s.nonce = nil
		}
		return nil, err
	}
}

// syncNonce reads the account's nonce from the node: the pending one, or the
// latest block's after a transaction was not mined in time. Called with mu
// held.
func (s *TxSender) syncNonce(ctx context.Context) (uint64, error) {
	if s.fromLatest {
		n, err := s.backend.NonceAt(ctx, s.from, nil)
		if err != nil {
			return 0, fmt.Errorf("get nonce: %w", err)
		}
		s.fromLatest = false
		return n, nil
	}
	n, err := s.backend.PendingNonceAt(ctx, s.from)
	if err != nil {
		return 0, fmt.Errorf("get pending nonce: %w", err)
	}
	return n, nil
}

// notMined handles a transaction that was not mined within ReceiptTimeout.
// The node may have dropped it (an eviction, a restart, an endpoint that never
// forwarded it). Nodes queue a transaction whose nonce leaves a gap without
// an error, so every later transaction would wait behind the lost one and the
// oracle would never write again. The next Send therefore takes the latest
// block's nonce, the lowest one not mined: it fills the gap, or replaces a
// stuck transaction if its fees are 10% higher, as the TS did by reading the
// latest nonce before every send. Transactions queued behind the gap are then
// mined after it, as they would have been.
func (s *TxSender) notMined(tx *SentTx) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (s.nonce == nil && s.fromLatest) || (s.nonce != nil && *s.nonce <= tx.Nonce) {
		return // already resyncing, or resynced below this transaction
	}
	s.opts.Logf("Transaction %s (nonce %d) not mined: the next transaction resyncs its nonce from the latest block", tx.Hash.Hex(), tx.Nonce)
	s.nonce, s.fromLatest = nil, true
}

// alreadySent reports whether the node already has tx itself, e.g. when a
// send was retried after a timeout and the first attempt got through.
func (s *TxSender) alreadySent(ctx context.Context, tx *types.Transaction, err error) bool {
	if errorContains(err, alreadyKnown) {
		return true
	}
	if !errorContains(err, nonceRejections) {
		return false
	}
	_, _, lookupErr := s.backend.TransactionByHash(ctx, tx.Hash())
	return lookupErr == nil
}

// fees returns the EIP-1559 tip (eth_maxPriorityFeePerGas, or like viem the
// gas price minus the base fee when the node lacks that method) and fee cap
// (2 * base fee + tip, which keeps the transaction includable while the base
// fee doubles; viem used 1.2 * base fee + tip).
func (s *TxSender) fees(ctx context.Context) (tip, feeCap *big.Int, err error) {
	head, err := s.backend.LatestBlockInfo(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get base fee: %w", err)
	}
	if head.BaseFee == nil {
		return nil, nil, errors.New("latest block has no base fee: the oracle sends EIP-1559 transactions only")
	}
	tip, err = s.backend.SuggestGasTipCap(ctx)
	if err != nil {
		price, priceErr := s.backend.SuggestGasPrice(ctx)
		if priceErr != nil {
			return nil, nil, fmt.Errorf("get priority fee: %w", err)
		}
		tip = new(big.Int).Sub(price, head.BaseFee)
		if tip.Sign() < 0 {
			tip.SetInt64(0)
		}
	}
	feeCap = new(big.Int).Mul(head.BaseFee, big.NewInt(2))
	return tip, feeCap.Add(feeCap, tip), nil
}

// Wait polls for tx's receipt until it is mined or ReceiptTimeout passes. A
// transaction not mined in time makes the next Send resync its nonce (see
// notMined).
func (s *TxSender) Wait(ctx context.Context, tx *SentTx) (*types.Receipt, error) {
	deadline := time.Now().Add(s.opts.ReceiptTimeout)
	for {
		receipt, err := s.backend.TransactionReceipt(ctx, tx.Hash)
		switch {
		case err == nil:
			return receipt, nil
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case !time.Now().Before(deadline):
			s.notMined(tx)
			if errors.Is(err, ethereum.NotFound) {
				return nil, fmt.Errorf("transaction %s not mined within %s", tx.Hash.Hex(), s.opts.ReceiptTimeout)
			}
			return nil, fmt.Errorf("transaction %s: no receipt within %s: %w", tx.Hash.Hex(), s.opts.ReceiptTimeout, err)
		}
		// Not mined yet (or the read failed after the client's own
		// retries): poll again.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.opts.PollInterval):
		}
	}
}

// RevertReason explains why a mined transaction reverted by replaying it with
// eth_call at the block before (as the TS did); "unknown" when the replay
// succeeds. A dry-run transaction's reason is its simulation's.
func RevertReason(ctx context.Context, c contracts.Caller, tx *SentTx, receipt *types.Receipt) string {
	if tx.revert != "" {
		return tx.revert
	}
	var block *big.Int
	if receipt.BlockNumber != nil && receipt.BlockNumber.Sign() > 0 {
		block = new(big.Int).Sub(receipt.BlockNumber, big.NewInt(1))
	}
	to := tx.To
	if _, err := c.CallContract(ctx, ethereum.CallMsg{From: tx.From, To: &to, Data: tx.Data}, block); err != nil {
		return contracts.DescribeRevert(err)
	}
	return "unknown"
}

// DryRunBackend is what DryRunSender needs from the RPC client.
type DryRunBackend interface {
	contracts.Caller
	BlockNumber(ctx context.Context) (uint64, error)
}

// DryRunSender is the Sender of dry-run mode: it never signs. Send simulates
// the call from the oracle address and logs what would have been sent; Wait
// reports the simulated outcome at once, so the services carry on as if the
// transaction was mined, successfully or reverted.
type DryRunSender struct {
	backend  DryRunBackend
	from     common.Address
	gasLimit uint64
	logf     logx.Logf

	mu   sync.Mutex
	sent uint64
}

// NewDryRunSender returns a dry-run sender acting as from, simulating with
// the gas limit real transactions get (0: the node's eth_call cap).
func NewDryRunSender(backend DryRunBackend, from common.Address, gasLimit uint64, logf logx.Logf) *DryRunSender {
	if logf == nil {
		logf = logx.Discard
	}
	return &DryRunSender{backend: backend, from: from, gasLimit: gasLimit, logf: logf}
}

// Address returns the oracle address the sender acts as.
func (d *DryRunSender) Address() common.Address { return d.from }

// Send simulates the call at the latest block and logs it. Only a failure to
// read the block number is an error.
func (d *DryRunSender) Send(ctx context.Context, to common.Address, data []byte) (*SentTx, error) {
	d.mu.Lock()
	n := d.sent
	d.sent++
	d.mu.Unlock()

	block, err := d.backend.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get block number: %w", err)
	}
	outcome, revert := "simulation ok", ""
	msg := ethereum.CallMsg{From: d.from, To: &to, Gas: d.gasLimit, Data: data}
	if _, err := d.backend.CallContract(ctx, msg, new(big.Int).SetUint64(block)); err != nil {
		if IsExecutionError(err) {
			revert = contracts.DescribeRevert(err)
			outcome = "would revert: " + revert
		} else {
			outcome = "simulation failed: " + err.Error()
		}
	}
	d.logf("DRY RUN: not sending %s to %s (%d bytes of calldata); at block %d: %s",
		methodName(data), to.Hex(), len(data), block, outcome)

	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], n)
	hash := crypto.Keccak256Hash([]byte("multiclaw-oracle-dry-run"), counter[:], to.Bytes(), data)
	return &SentTx{Hash: hash, From: d.from, To: to, Data: data, Nonce: n, simulatedAt: block, revert: revert}, nil
}

// Wait returns the receipt of the simulated outcome, in the block the call
// was simulated at: reverted when the simulation reverted (production would
// have mined a reverting transaction), otherwise successful, also when the
// simulation could not run.
func (d *DryRunSender) Wait(_ context.Context, tx *SentTx) (*types.Receipt, error) {
	status := types.ReceiptStatusSuccessful
	if tx.revert != "" {
		status = types.ReceiptStatusFailed
	}
	return &types.Receipt{
		Status:      status,
		TxHash:      tx.Hash,
		BlockNumber: new(big.Int).SetUint64(tx.simulatedAt),
	}, nil
}

// methodName names the module method data calls, or shows its selector.
func methodName(data []byte) string {
	if len(data) < 4 {
		return fmt.Sprintf("call 0x%x", data)
	}
	if m, err := contracts.ModuleABI.MethodById(data[:4]); err == nil {
		return m.Name
	}
	return fmt.Sprintf("call 0x%x", data[:4])
}
