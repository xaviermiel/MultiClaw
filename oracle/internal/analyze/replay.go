package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

// callFrame is a call of debug_traceTransaction's callTracer output.
type callFrame struct {
	Type   string         `json:"type"`
	To     common.Address `json:"to"`
	Output hexutil.Bytes  `json:"output"`
	Error  string         `json:"error"`
	Calls  []callFrame    `json:"calls"`
}

// failure is a failed call, and the contract it ran as: for a delegatecall
// its caller's (a proxy, rather than the proxy's implementation).
type failure struct {
	frame *callFrame
	as    common.Address
}

// runsAs is the contract call c (made by a frame running as `as`) runs as.
func runsAs(c *callFrame, as common.Address) common.Address {
	if c.Type == "DELEGATECALL" || c.Type == "CALLCODE" {
		return as
	}
	return c.To
}

// reportedFailure returns the last failed call under f, in execution order,
// that is not inside another failed call: a failure that a call which did
// not revert reported back (the Safe returning false for its protocol
// call). nil if there is none.
func reportedFailure(f *callFrame, as common.Address) *failure {
	var last *failure
	for i := range f.Calls {
		c := &f.Calls[i]
		switch cas := runsAs(c, as); {
		case c.Error != "":
			last = &failure{c, cas}
		default:
			if inner := reportedFailure(c, cas); inner != nil {
				last = inner
			}
		}
	}
	return last
}

// origin follows a failed call's revert down to where it was raised: into
// its last failed callee as long as the revert data was passed on unchanged.
// A different revert (the callee's failure was caught) stops there.
func origin(f failure) failure {
	for {
		var last *failure
		for i := range f.frame.Calls {
			if c := &f.frame.Calls[i]; c.Error != "" {
				last = &failure{c, runsAs(c, f.as)}
			}
		}
		if last == nil || !bytes.Equal(last.frame.Output, f.frame.Output) {
			return f
		}
		f = *last
	}
}

// frameRevert decodes a failed frame's revert: its output, or its error
// when it has none (out of gas, invalid opcode...).
func frameRevert(f *callFrame) *revertInfo {
	if len(f.Output) == 0 && f.Error != "" && f.Error != "execution reverted" {
		r := &revertInfo{name: f.Error, help: unknownHelp}
		if strings.Contains(f.Error, "out of gas") {
			r.help = outOfGasHelp
		}
		return r
	}
	return decodeRevert(f.Output)
}

// replay gets the revert data: from the node's trace of the transaction,
// else by calling it again at the block before (which misses the effect of
// the transactions before it in its block), else from Tenderly.
func (a *Analyzer) replay(ctx context.Context, an *analysis) {
	var steps []field
	done := func(source string) {
		steps = append(steps, field{source, "reverted: " + an.revert.title()})
		an.add("REPLAY", steps...)
	}

	var frame *callFrame
	err := a.b.CallRPC(ctx, &frame, "debug_traceTransaction", an.hash, map[string]string{"tracer": "callTracer"})
	switch {
	case err != nil:
		steps = append(steps, field{"debug_traceTransaction", "unavailable: " + oneLine(err)})
	case frame == nil || frame.Error == "":
		steps = append(steps, field{"debug_traceTransaction", "the trace shows no revert"})
	default:
		an.revert = frameRevert(frame)
		if f := reportedFailure(frame, frame.To); f != nil {
			o := origin(*f)
			an.underlying, an.underlyingAt = frameRevert(o.frame), o.as
		}
		done("debug_traceTransaction")
		return
	}

	before := fmt.Sprintf("eth_call at block %d", an.block-1)
	msg := ethereum.CallMsg{From: an.tx.From, To: an.tx.To, Gas: uint64(an.tx.Gas), Value: an.value, Data: an.tx.Input}
	_, err = a.b.CallContract(ctx, msg, an.before())
	data, hasData := contracts.RevertData(err)
	switch {
	case err == nil:
		steps = append(steps, field{before, "succeeds: the failure depended on the transactions before it in its block"})
	case hasData && len(data) > 50 && len(an.tx.Input) >= 4 && bytes.Equal(data[:4], an.tx.Input[:4]):
		// Some nodes without the block's state answer with the calldata.
		steps = append(steps, field{before, "the node returned the call's own input: it lacks the state of that block"})
	case hasData:
		an.revert = decodeRevert(data)
		done(before)
		return
	case ethx.IsExecutionError(err):
		an.revert = decodeRevert(nil)
		done(before)
		return
	case strings.Contains(err.Error(), "out of gas"): // with the transaction's gas limit
		an.revert = &revertInfo{name: "out of gas", help: outOfGasHelp}
		done(before)
		return
	default:
		steps = append(steps, field{before, "unavailable: " + oneLine(err)})
	}

	if !a.opts.Tenderly {
		an.add("REPLAY", append(steps, field{"Tenderly", "not asked (--no-tenderly)"})...)
		return
	}
	data, err = a.tenderly(ctx, an.hash)
	if err != nil {
		an.add("REPLAY", append(steps, field{"Tenderly", "unavailable: " + oneLine(err)})...)
		return
	}
	an.revert = decodeRevert(data)
	done("Tenderly")
}

// tenderly reads the transaction's revert data from Tenderly's public trace
// API.
func (a *Analyzer) tenderly(ctx context.Context, hash common.Hash) ([]byte, error) {
	url := fmt.Sprintf("%s/api/v1/public-contract/%d/trace/%s", a.opts.TenderlyURL, a.chain.ID, hash.Hex())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.opts.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var trace struct {
		CallTrace *struct {
			ErrorHexData string `json:"error_hex_data"`
		} `json:"call_trace"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&trace); err != nil {
		return nil, fmt.Errorf("decode trace: %w", err)
	}
	if trace.CallTrace == nil || trace.CallTrace.ErrorHexData == "" {
		return nil, errors.New("the trace has no revert data")
	}
	return hexutil.Decode(trace.CallTrace.ErrorHexData)
}

// wrappers are the module errors that report a failure of the call the Safe
// made, which the underlying error explains.
var wrappers = map[string]bool{"TransactionFailed": true, "ApprovalFailed": true, "ModuleTransactionFailed": true}

// explain reports the error and, for a failed Safe call, the protocol error
// behind it.
func (a *Analyzer) explain(ctx context.Context, an *analysis) {
	r := an.revert
	if r == nil {
		fields := []field{
			{"", "No revert data. Possible causes: the node keeps no history and Tenderly could not help,"},
			{"", "the transaction ran out of gas, or it depended on state that changed within its block."},
		}
		if an.gasUsed >= uint64(an.tx.Gas) {
			fields = append(fields, field{"", fmt.Sprintf("It used all of its gas (%d): it most likely ran out of gas.", an.gasUsed)})
		}
		an.add("ERROR", fields...)
		return
	}
	a.tokenAmounts(ctx, r, *an.to())
	fields := []field{{"Error", r.title()}}
	for _, f := range r.args {
		fields = append(fields, field{"  " + f.key, f.value})
	}
	fields = append(fields, field{"Meaning", r.help.description}, field{"Solution", r.help.solution})
	if wrappers[r.name] && an.inner != nil {
		if an.underlying == nil {
			a.replayInner(ctx, an)
		}
		switch u := an.underlying; {
		case u != nil:
			a.tokenAmounts(ctx, u, an.underlyingAt)
			at := ""
			if an.underlyingAt != (common.Address{}) {
				at = " in " + a.label(ctx, an.underlyingAt)
			}
			fields = append(fields, field{"Underlying error", u.title() + at})
			for _, f := range u.args {
				fields = append(fields, field{"  " + f.key, f.value})
			}
			if u.help != unknownHelp {
				fields = append(fields, field{"  meaning", u.help.description}, field{"  solution", u.help.solution})
			}
		case an.underlyingNote != "":
			fields = append(fields, field{"Underlying error", an.underlyingNote})
		}
	}
	an.add("ERROR", fields...)
}

// tokenAmounts shows the amounts of a token error raised by token with the
// token's decimals (when it is a token: it has a symbol).
func (a *Analyzer) tokenAmounts(ctx context.Context, r *revertInfo, token common.Address) {
	if r.name != "ERC20InsufficientBalance" && r.name != "ERC20InsufficientAllowance" || a.symbol(ctx, token) == "" {
		return
	}
	for i, v := range r.values {
		if x, ok := v.(*big.Int); ok {
			r.args[i].value = a.amount(ctx, token, x)
		}
	}
}

// replayInner calls the Safe's call again, from the Safe at the block before,
// to find the error behind TransactionFailed when there is no trace.
func (a *Analyzer) replayInner(ctx context.Context, an *analysis) {
	safe, err := contracts.NewModule(*an.to(), a.b).At(an.before()).Avatar(ctx)
	if err != nil {
		an.underlyingNote = "unknown (could not read the module's Safe: " + oneLine(err) + ")"
		return
	}
	c := an.inner
	_, err = a.b.CallContract(ctx, ethereum.CallMsg{From: safe, To: &c.target, Value: c.value, Data: c.data}, an.before())
	data, hasData := contracts.RevertData(err)
	switch {
	case err == nil:
		an.underlyingNote = fmt.Sprintf("unknown: the call succeeds when made alone at block %d", an.block-1)
	case hasData:
		an.underlying, an.underlyingAt = decodeRevert(data), c.target
	case ethx.IsExecutionError(err):
		an.underlying, an.underlyingAt = decodeRevert(nil), c.target
	default:
		an.underlyingNote = "unknown (could not replay the call: " + oneLine(err) + ")"
	}
}

// oneLine shortens an error for the report.
func oneLine(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}
