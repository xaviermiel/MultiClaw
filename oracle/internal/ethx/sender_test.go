package ethx

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
)

const testChainID = 84532

var (
	gwei      = big.NewInt(1_000_000_000)
	recipient = common.HexToAddress("0x1111111111111111111111111111111111111111")
)

// fakeChain is an in-memory TxBackend whose tx pool, like a node's, accepts
// a transaction only at the account's next nonce.
type fakeChain struct {
	mu         sync.Mutex
	chainID    int64
	tip        *big.Int
	baseFee    *big.Int
	next       uint64 // the account's next nonce
	nonceReads int
	sent       []*types.Transaction
	known      map[common.Hash]bool
	// hooks replace the default handling of the next SendTransaction calls,
	// one each (they run with mu held and may call accept).
	hooks []func(tx *types.Transaction) error
	// receipts are answered after receiptMisses NotFound answers.
	receipts      map[common.Hash]*types.Receipt
	receiptMisses int
	receiptReads  int
}

func newFakeChain(next uint64) *fakeChain {
	return &fakeChain{
		chainID:  testChainID,
		tip:      new(big.Int).Set(gwei),
		baseFee:  new(big.Int).Mul(gwei, big.NewInt(10)),
		next:     next,
		known:    map[common.Hash]bool{},
		receipts: map[common.Hash]*types.Receipt{},
	}
}

func (f *fakeChain) ChainID(context.Context) (*big.Int, error) { return big.NewInt(f.chainID), nil }

func (f *fakeChain) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nonceReads++
	return f.next, nil
}

func (f *fakeChain) SuggestGasTipCap(context.Context) (*big.Int, error) { return f.tip, nil }

func (f *fakeChain) LatestBlockInfo(context.Context) (BlockInfo, error) {
	return BlockInfo{Number: 100, BaseFee: f.baseFee}, nil
}

func (f *fakeChain) SendTransaction(_ context.Context, tx *types.Transaction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hooks) > 0 {
		hook := f.hooks[0]
		f.hooks = f.hooks[1:]
		return hook(tx)
	}
	switch {
	case tx.Nonce() < f.next:
		return errors.New("nonce too low: next nonce 9, tx nonce 6")
	case tx.Nonce() > f.next:
		return errors.New("nonce too high")
	}
	f.accept(tx)
	return nil
}

func (f *fakeChain) accept(tx *types.Transaction) {
	f.next++
	f.sent = append(f.sent, tx)
	f.known[tx.Hash()] = true
}

func (f *fakeChain) TransactionByHash(_ context.Context, hash common.Hash) (*types.Transaction, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.known[hash] {
		return nil, true, nil
	}
	return nil, false, ethereum.NotFound
}

func (f *fakeChain) TransactionReceipt(_ context.Context, hash common.Hash) (*types.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.receiptReads++
	if f.receiptReads <= f.receiptMisses {
		return nil, ethereum.NotFound
	}
	if r, ok := f.receipts[hash]; ok {
		return r, nil
	}
	return nil, ethereum.NotFound
}

func (f *fakeChain) sentNonces() []uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uint64, len(f.sent))
	for i, tx := range f.sent {
		out[i] = tx.Nonce()
	}
	return out
}

func newTestSender(t *testing.T, chain *fakeChain, logs *logRecorder) (*TxSender, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	opts := SenderOptions{PollInterval: time.Millisecond, ReceiptTimeout: 50 * time.Millisecond}
	if logs != nil {
		opts.Logf = logs.Logf
	}
	s, err := NewTxSender(context.Background(), chain, key, testChainID, 500_000, opts)
	if err != nil {
		t.Fatal(err)
	}
	return s, key
}

func TestTxSenderSignsDynamicFeeTransactions(t *testing.T) {
	chain := newFakeChain(7)
	s, key := newTestSender(t, chain, nil)
	data := []byte{0xe7, 0x0d, 0x08, 0x40, 1, 2, 3}

	sent, err := s.Send(context.Background(), recipient, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain.sent) != 1 {
		t.Fatalf("sent %d transactions", len(chain.sent))
	}
	tx := chain.sent[0]
	wantFeeCap := new(big.Int).Mul(gwei, big.NewInt(21)) // 2 * 10 gwei + 1 gwei
	switch {
	case tx.Type() != types.DynamicFeeTxType:
		t.Fatalf("tx type %d", tx.Type())
	case tx.ChainId().Int64() != testChainID:
		t.Fatalf("chain ID %s", tx.ChainId())
	case tx.Nonce() != 7:
		t.Fatalf("nonce %d", tx.Nonce())
	case tx.GasTipCap().Cmp(gwei) != 0 || tx.GasFeeCap().Cmp(wantFeeCap) != 0:
		t.Fatalf("fees tip=%s cap=%s", tx.GasTipCap(), tx.GasFeeCap())
	case tx.Gas() != 500_000:
		t.Fatalf("gas %d", tx.Gas())
	case *tx.To() != recipient || !slices.Equal(tx.Data(), data) || tx.Value().Sign() != 0:
		t.Fatalf("to=%s data=%x value=%s", tx.To(), tx.Data(), tx.Value())
	}
	from, err := types.Sender(types.LatestSignerForChainID(big.NewInt(testChainID)), tx)
	if err != nil || from != crypto.PubkeyToAddress(key.PublicKey) || from != s.Address() {
		t.Fatalf("signed by %s (%v), want %s", from.Hex(), err, s.Address().Hex())
	}
	if sent.Hash != tx.Hash() || sent.Nonce != 7 || sent.From != from || sent.To != recipient || !slices.Equal(sent.Data, data) {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestTxSenderNoncesAreConsecutiveAcrossGoroutines(t *testing.T) {
	chain := newFakeChain(7)
	s, _ := newTestSender(t, chain, nil)

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if _, err := s.Send(context.Background(), recipient, nil); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()

	nonces := chain.sentNonces()
	sort.Slice(nonces, func(i, j int) bool { return nonces[i] < nonces[j] })
	for i, n := range nonces {
		if n != uint64(7+i) {
			t.Fatalf("nonces %v are not consecutive from 7", nonces)
		}
	}
	if len(nonces) != 20 || chain.nonceReads != 1 {
		t.Fatalf("%d transactions, %d nonce reads", len(nonces), chain.nonceReads)
	}
}

func TestTxSenderResyncsWhenTheNonceIsTaken(t *testing.T) {
	chain := newFakeChain(5)
	logs := &logRecorder{}
	s, _ := newTestSender(t, chain, logs)
	ctx := context.Background()

	if _, err := s.Send(ctx, recipient, nil); err != nil {
		t.Fatal(err)
	}
	chain.mu.Lock()
	chain.next = 9 // the key sent transactions from elsewhere
	chain.mu.Unlock()

	sent, err := s.Send(ctx, recipient, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Nonce != 9 || !logs.contains("Nonce 6 rejected") || !logs.contains("resyncing") {
		t.Fatalf("nonce %d, logs %q", sent.Nonce, logs.lines)
	}
	if sent, err = s.Send(ctx, recipient, nil); err != nil || sent.Nonce != 10 {
		t.Fatalf("next nonce %v (%v), want 10", sent, err)
	}
	if chain.nonceReads != 2 {
		t.Fatalf("%d nonce reads, want 2", chain.nonceReads)
	}
}

func TestTxSenderRecognisesItsOwnTransaction(t *testing.T) {
	chain := newFakeChain(3)
	s, _ := newTestSender(t, chain, nil)
	ctx := context.Background()

	// The node already has this exact transaction (a retried send).
	chain.hooks = append(chain.hooks, func(tx *types.Transaction) error {
		chain.accept(tx)
		return errors.New("already known")
	})
	// A retried send whose first attempt got mined: the node now says the
	// nonce is used, but by our transaction.
	chain.hooks = append(chain.hooks, func(tx *types.Transaction) error {
		chain.accept(tx)
		return errors.New("nonce too low")
	})
	for want := uint64(3); want < 6; want++ {
		sent, err := s.Send(ctx, recipient, nil)
		if err != nil || sent.Nonce != want {
			t.Fatalf("send: nonce %v (%v), want %d", sent, err, want)
		}
	}
	if got := chain.sentNonces(); !slices.Equal(got, []uint64{3, 4, 5}) || chain.nonceReads != 1 {
		t.Fatalf("nonces %v, %d nonce reads: each calldata must be sent once", got, chain.nonceReads)
	}
}

func TestTxSenderGivesUpAfterRepeatedNonceRejections(t *testing.T) {
	chain := newFakeChain(0)
	s, _ := newTestSender(t, chain, nil)
	for range maxNonceResyncs + 1 {
		chain.hooks = append(chain.hooks, func(*types.Transaction) error {
			return errors.New("replacement transaction underpriced")
		})
	}
	if _, err := s.Send(context.Background(), recipient, nil); err == nil || !strings.Contains(err.Error(), "underpriced") {
		t.Fatalf("err = %v", err)
	}
	if chain.nonceReads != maxNonceResyncs+1 || len(chain.hooks) != 0 {
		t.Fatalf("%d nonce reads, %d unused hooks", chain.nonceReads, len(chain.hooks))
	}
}

func TestTxSenderKeepsTheNonceOnOtherRejections(t *testing.T) {
	chain := newFakeChain(4)
	s, _ := newTestSender(t, chain, nil)
	ctx := context.Background()
	chain.hooks = append(chain.hooks, func(*types.Transaction) error {
		return errors.New("insufficient funds for gas * price + value")
	})
	if _, err := s.Send(ctx, recipient, nil); err == nil {
		t.Fatal("want the rejection")
	}
	sent, err := s.Send(ctx, recipient, nil)
	if err != nil || sent.Nonce != 4 || chain.nonceReads != 1 {
		t.Fatalf("nonce %v (%v), %d nonce reads", sent, err, chain.nonceReads)
	}
}

func TestTxSenderResyncsAfterATransportFailure(t *testing.T) {
	chain := newFakeChain(4)
	s, _ := newTestSender(t, chain, nil)
	ctx := context.Background()
	// The node took the transaction, but the answer was lost.
	chain.hooks = append(chain.hooks, func(tx *types.Transaction) error {
		chain.accept(tx)
		return errors.New("Post \"http://rpc\": connection reset by peer")
	})
	if _, err := s.Send(ctx, recipient, nil); err == nil {
		t.Fatal("want the transport error")
	}
	sent, err := s.Send(ctx, recipient, nil)
	if err != nil || sent.Nonce != 5 || chain.nonceReads != 2 {
		t.Fatalf("nonce %v (%v), %d nonce reads", sent, err, chain.nonceReads)
	}
}

func TestTxSenderRequiresABaseFee(t *testing.T) {
	chain := newFakeChain(0)
	chain.baseFee = nil
	s, _ := newTestSender(t, chain, nil)
	if _, err := s.Send(context.Background(), recipient, nil); err == nil || !strings.Contains(err.Error(), "EIP-1559") {
		t.Fatalf("err = %v", err)
	}
	if len(chain.sent) != 0 {
		t.Fatal("sent a transaction")
	}
}

func TestNewTxSenderChecksTheChainID(t *testing.T) {
	chain := newFakeChain(0)
	chain.chainID = 8453
	key, _ := crypto.GenerateKey()
	_, err := NewTxSender(context.Background(), chain, key, testChainID, 500_000, SenderOptions{})
	if err == nil || !strings.Contains(err.Error(), "8453") {
		t.Fatalf("err = %v", err)
	}
}

func TestTxSenderWait(t *testing.T) {
	ctx := context.Background()

	t.Run("polls until mined", func(t *testing.T) {
		chain := newFakeChain(0)
		s, _ := newTestSender(t, chain, nil)
		sent, err := s.Send(ctx, recipient, nil)
		if err != nil {
			t.Fatal(err)
		}
		chain.receipts[sent.Hash] = &types.Receipt{Status: types.ReceiptStatusSuccessful, BlockNumber: big.NewInt(42)}
		chain.receiptMisses = 3
		receipt, err := s.Wait(ctx, sent)
		if err != nil || receipt.BlockNumber.Int64() != 42 || chain.receiptReads != 4 {
			t.Fatalf("receipt %v (%v) after %d reads", receipt, err, chain.receiptReads)
		}
	})

	t.Run("times out", func(t *testing.T) {
		chain := newFakeChain(0)
		s, _ := newTestSender(t, chain, nil)
		sent, err := s.Send(ctx, recipient, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Wait(ctx, sent); err == nil || !strings.Contains(err.Error(), "not mined within 50ms") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("stops when the context ends", func(t *testing.T) {
		chain := newFakeChain(0)
		s, _ := newTestSender(t, chain, nil)
		s.opts.ReceiptTimeout = time.Hour
		sent, err := s.Send(ctx, recipient, nil)
		if err != nil {
			t.Fatal(err)
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		if _, err := s.Wait(cctx, sent); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}

// replayCaller records the eth_call RevertReason makes.
type replayCaller struct {
	msg   ethereum.CallMsg
	block *big.Int
	err   error
}

func (c *replayCaller) CallContract(_ context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	c.msg, c.block = msg, block
	return nil, c.err
}

// argumentlessModuleError returns a module error without inputs, so its
// revert data is just the selector.
func argumentlessModuleError(t *testing.T) abi.Error {
	t.Helper()
	names := make([]string, 0, len(contracts.ModuleABI.Errors))
	for name := range contracts.ModuleABI.Errors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if e := contracts.ModuleABI.Errors[name]; len(e.Inputs) == 0 {
			return e
		}
	}
	t.Fatal("the module ABI has no error without inputs")
	return abi.Error{}
}

func TestRevertReasonReplaysAtThePreviousBlock(t *testing.T) {
	moduleErr := argumentlessModuleError(t)
	selector := moduleErr.ID[:4]
	sent := &SentTx{From: common.HexToAddress("0xf00d"), To: recipient, Data: []byte{1, 2, 3, 4}}
	receipt := &types.Receipt{Status: types.ReceiptStatusFailed, BlockNumber: big.NewInt(100)}

	c := &replayCaller{err: &contractstest.RevertError{Data: selector}}
	if got := RevertReason(context.Background(), c, sent, receipt); got != moduleErr.Name {
		t.Fatalf("reason = %q, want %q", got, moduleErr.Name)
	}
	if c.block.Int64() != 99 || c.msg.From != sent.From || *c.msg.To != recipient || !slices.Equal(c.msg.Data, sent.Data) {
		t.Fatalf("replayed %+v at block %s", c.msg, c.block)
	}

	if got := RevertReason(context.Background(), &replayCaller{}, sent, receipt); got != "unknown" {
		t.Fatalf("reason when the replay succeeds = %q", got)
	}
}

// dryRunBackend is a contractstest.Fake with a block number.
type dryRunBackend struct {
	*contractstest.Fake
	block uint64
}

func (b dryRunBackend) BlockNumber(context.Context) (uint64, error) { return b.block, nil }

func TestDryRunSenderSimulatesAndNeverSigns(t *testing.T) {
	okModule := common.HexToAddress("0xaaaa")
	revertingModule := common.HexToAddress("0xbbbb")
	moduleErr := argumentlessModuleError(t)
	fake := contractstest.New().
		Return(okModule, &contracts.ModuleABI, "updateSafeValue").
		Revert(revertingModule, &contracts.ModuleABI, "updateSafeValue", moduleErr.ID[:4])
	oracle := common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")
	logs := &logRecorder{}
	var s Sender = NewDryRunSender(dryRunBackend{fake, 1234}, oracle, logs.Logf)
	ctx := context.Background()

	data, err := contracts.PackUpdateSafeValue(big.NewInt(5))
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Send(ctx, okModule, data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Send(ctx, revertingModule, data)
	if err != nil {
		t.Fatal(err)
	}
	if !logs.contains("DRY RUN: not sending updateSafeValue to "+okModule.Hex()+" (36 bytes of calldata); at block 1234: simulation ok") ||
		!logs.contains("at block 1234: would revert: "+moduleErr.Name) {
		t.Fatalf("logs %q", logs.lines)
	}
	if first.Hash == second.Hash || first.From != oracle || s.Address() != oracle {
		t.Fatalf("first %+v second %+v", first, second)
	}
	calls := fake.Calls()
	if len(calls) != 2 || calls[0].Block.Uint64() != 1234 {
		t.Fatalf("calls %+v", calls)
	}

	receipt, err := s.Wait(ctx, second)
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful || receipt.TxHash != second.Hash || receipt.BlockNumber.Uint64() != 1234 {
		t.Fatalf("receipt %+v (%v)", receipt, err)
	}
}
