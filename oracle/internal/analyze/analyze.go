// Package analyze explains why a transaction failed: the Go port of
// oracle/src/analyze-failed-tx.ts, run as `oracle analyze-tx <hash>`.
//
// It decodes the call (module functions with the module's full ABI, and the
// protocol call inside executeOnProtocol), replays it to get the revert data
// (debug_traceTransaction, else eth_call at the block before, else Tenderly's
// public trace API), explains the error, and reads the module state behind it
// at the block before the transaction (or now, without historical state).
package analyze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

// Backend is what the analyzer reads from the node; *ethx.FallbackClient
// implements it.
type Backend interface {
	contracts.Caller
	BalanceAt(ctx context.Context, account common.Address, block *big.Int) (*big.Int, error)
	BlockInfo(ctx context.Context, n uint64) (ethx.BlockInfo, error)
	CallRPC(ctx context.Context, result any, method string, args ...any) error
}

// Options tunes an Analyzer.
type Options struct {
	// Tenderly lets the analyzer ask Tenderly's public API for the trace
	// when the node cannot replay the transaction.
	Tenderly bool
	// TenderlyURL is the API's base URL (default https://api.tenderly.co).
	TenderlyURL string
	// HTTP is the client for Tenderly (default: one with a 15 s timeout).
	HTTP *http.Client
}

// Analyzer explains transactions of one chain.
type Analyzer struct {
	b     Backend
	chain config.Chain
	opts  Options

	symbols  map[common.Address]string
	decimals map[common.Address]int
}

// New returns an analyzer reading chain through b.
func New(b Backend, chain config.Chain, opts Options) *Analyzer {
	if opts.TenderlyURL == "" {
		opts.TenderlyURL = "https://api.tenderly.co"
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	a := &Analyzer{b: b, chain: chain, opts: opts, symbols: map[common.Address]string{}, decimals: map[common.Address]int{}}
	for _, t := range chain.Tokens {
		a.symbols[t.Address] = t.Symbol
	}
	return a
}

// rpcTx is the part of eth_getTransactionByHash the analyzer uses, decoded
// by hand: go-ethereum's types.Transaction rejects OP-stack transaction
// types.
type rpcTx struct {
	From        common.Address  `json:"from"`
	To          *common.Address `json:"to"`
	Value       *hexutil.Big    `json:"value"`
	Gas         hexutil.Uint64  `json:"gas"`
	Input       hexutil.Bytes   `json:"input"`
	BlockNumber *hexutil.Uint64 `json:"blockNumber"`
}

type rpcReceipt struct {
	Status  hexutil.Uint64 `json:"status"`
	GasUsed hexutil.Uint64 `json:"gasUsed"`
}

// analysis is what the analyzer learns about one transaction.
type analysis struct {
	hash    common.Hash
	tx      rpcTx
	value   *big.Int
	block   uint64 // 0 while pending
	time    time.Time
	failed  bool
	gasUsed uint64

	// method and args decode the input as a module function call.
	method *abi.Method
	args   []any
	// inner is the call the module has the Safe make (executeOnProtocol,
	// transferToken).
	inner *innerCall

	revert     *revertInfo
	underlying *revertInfo
	// underlyingAt is the contract that raised underlying.
	underlyingAt common.Address
	// underlyingNote explains a missing underlying error.
	underlyingNote string

	sections []section
}

// Analyze explains transaction hash and writes the report to w. It fails only
// when the transaction cannot be read.
func (a *Analyzer) Analyze(ctx context.Context, hash common.Hash, w io.Writer) error {
	an := &analysis{hash: hash}
	var tx *rpcTx
	if err := a.b.CallRPC(ctx, &tx, "eth_getTransactionByHash", hash); err != nil {
		return fmt.Errorf("get transaction: %w", err)
	}
	if tx == nil {
		return fmt.Errorf("transaction %s not found", hash.Hex())
	}
	an.tx, an.value = *tx, new(big.Int)
	if tx.Value != nil {
		an.value = tx.Value.ToInt()
	}
	if err := a.transaction(ctx, an); err != nil {
		return err
	}
	a.call(ctx, an)

	switch {
	case an.block == 0:
		an.add("RESULT", field{"", "The transaction is still pending."})
	case !an.failed:
		an.add("RESULT", field{"", "The transaction succeeded: there is no error to analyze."})
	case an.value.Sign() > 0 && an.method != nil && !an.method.IsPayable():
		a.nonPayable(an)
	default:
		a.replay(ctx, an)
		a.explain(ctx, an)
		a.state(ctx, an)
	}
	return render(w, an)
}

// transaction reads the receipt and block, for the first section.
func (a *Analyzer) transaction(ctx context.Context, an *analysis) error {
	tx := an.tx
	to := "(contract creation)"
	if tx.To != nil {
		to = a.label(ctx, *tx.To)
	}
	fields := []field{
		{"From", tx.From.Hex()},
		{"To", to},
		{"Value", bn.FormatUnits(an.value, 18) + " ETH"},
	}
	if tx.BlockNumber == nil {
		an.add("TRANSACTION", append(fields, field{"Status", "pending"})...)
		return nil
	}
	an.block = uint64(*tx.BlockNumber)
	var receipt *rpcReceipt
	if err := a.b.CallRPC(ctx, &receipt, "eth_getTransactionReceipt", an.hash); err != nil {
		return fmt.Errorf("get receipt: %w", err)
	}
	if receipt == nil {
		return errors.New("the transaction is mined but has no receipt")
	}
	an.failed, an.gasUsed = receipt.Status == 0, uint64(receipt.GasUsed)
	block := fmt.Sprint(an.block)
	if info, err := a.b.BlockInfo(ctx, an.block); err == nil {
		an.time = time.Unix(int64(info.Timestamp), 0).UTC()
		block += " (" + an.time.Format(time.RFC3339) + ")"
	}
	status := "SUCCESS"
	if an.failed {
		status = "FAILED"
	}
	gas := fmt.Sprintf("%d of %d", an.gasUsed, uint64(tx.Gas))
	if an.failed && an.gasUsed >= uint64(tx.Gas) {
		gas += " (all of it)"
	}
	an.add("TRANSACTION", append(fields, field{"Block", block}, field{"Status", status}, field{"Gas used", gas})...)
	return nil
}

// call decodes the input: a module function (with the call it makes the
// Safe execute), else a protocol call made directly.
func (a *Analyzer) call(ctx context.Context, an *analysis) {
	input := []byte(an.tx.Input)
	if len(input) < 4 {
		an.add("CALL", field{"", fmt.Sprintf("No function call (%d bytes of input).", len(input))})
		return
	}
	m, args, err := decodeCall(&contracts.ModuleABI, input)
	if err != nil {
		if m != nil {
			an.add("CALL", field{"Function", m.Sig}, field{"", err.Error()})
			return
		}
		// Not a module function: maybe a direct protocol call.
		c := newInnerCall(*an.to(), input, an.value)
		if c.method == nil {
			an.add("CALL", field{"", fmt.Sprintf("Unknown function, selector %s (%d bytes of input).", hexutil.Encode(input[:4]), len(input))})
			return
		}
		an.add("CALL", a.innerFields(ctx, an, c)...)
		return
	}
	an.method, an.args = m, args
	fields := []field{{"Function", m.Sig}}
	for i, in := range m.Inputs {
		fields = append(fields, field{"  " + in.Name, a.formatArg(ctx, in.Name, args[i])})
	}
	switch m.RawName {
	case "executeOnProtocol":
		an.inner = newInnerCall(args[0].(common.Address), args[1].([]byte), new(big.Int))
	case "executeOnProtocolWithValue":
		an.inner = newInnerCall(args[0].(common.Address), args[1].([]byte), args[2].(*big.Int))
	case "transferToken":
		token, recipient, amount := args[0].(common.Address), args[1].(common.Address), args[2].(*big.Int)
		fields[len(fields)-1].value = a.amount(ctx, token, amount)
		if token == (common.Address{}) {
			an.inner = &innerCall{target: recipient, value: amount, spends: &tokenAmount{token, amount}}
		} else {
			data, _ := contracts.ProtocolCallsABI.Pack("transfer", recipient, amount)
			an.inner = newInnerCall(token, data, new(big.Int))
		}
	}
	if an.inner != nil && m.RawName != "transferToken" {
		fields = append(fields, field{"", "The call the Safe makes:"})
		for _, f := range a.innerFields(ctx, an, an.inner) {
			fields = append(fields, field{"  " + f.key, f.value})
		}
	}
	an.add("CALL", fields...)
}

// nonPayable reports ETH sent to a module function: none of them is payable,
// so the call reverted before running.
func (a *Analyzer) nonPayable(an *analysis) {
	name := an.method.RawName
	solution := fmt.Sprintf("Send the transaction without ETH: '%s' is not payable.", name)
	switch name {
	case "executeOnProtocol":
		solution = "To have the Safe send ETH with the protocol call, call executeOnProtocolWithValue(target, data, value) without attaching ETH: the Safe pays value from its own balance."
	case "executeOnProtocolWithValue":
		solution = "Do not attach ETH: pass the amount as the value argument, and the Safe pays it from its own balance."
	}
	an.add("ERROR",
		field{"Error", "NonPayableFunctionWithValue"},
		field{"Meaning", fmt.Sprintf("The transaction sent %s ETH to the module's '%s', which is not payable: it reverted before running.", bn.FormatUnits(an.value, 18), name)},
		field{"Solution", solution},
	)
}

// add appends a section to the report.
func (an *analysis) add(title string, fields ...field) {
	an.sections = append(an.sections, section{title, fields})
}

// to is the transaction's recipient (the zero address for a creation).
func (an *analysis) to() *common.Address {
	if an.tx.To == nil {
		return &common.Address{}
	}
	return an.tx.To
}

// before is the block before the transaction's.
func (an *analysis) before() *big.Int {
	return new(big.Int).SetUint64(an.block - 1)
}
