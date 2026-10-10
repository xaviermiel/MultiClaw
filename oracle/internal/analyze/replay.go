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
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

// callFrame is a call of a transaction's trace. Its JSON form is the
// callTracer's (tests write traces with it); traces are read through
// rawFrame.
type callFrame struct {
	Type   string         `json:"type"`
	To     common.Address `json:"to"`
	Output hexutil.Bytes  `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
	Calls  []callFrame    `json:"calls,omitempty"`
}

// rawFrame is a frame of debug_traceTransaction's callTracer output or of
// Tenderly's call_trace, decoded leniently: a field may be missing, null or
// malformed without failing the trace.
type rawFrame struct {
	Type         string     `json:"type"`      // callTracer
	CallType     string     `json:"call_type"` // Tenderly
	To           string     `json:"to"`
	Output       string     `json:"output"`
	ErrorHexData string     `json:"error_hex_data"` // Tenderly's top frame
	Error        string     `json:"error"`
	Calls        []rawFrame `json:"calls"`
}

// callTypes are the frame types of calls: Tenderly also lists internal
// jumps, storage accesses, logs and reverts.
var callTypes = map[string]bool{
	"CALL": true, "STATICCALL": true, "DELEGATECALL": true, "CALLCODE": true,
	"CREATE": true, "CREATE2": true, "SELFDESTRUCT": true,
}

func (r *rawFrame) kind() string {
	if r.Type != "" {
		return strings.ToUpper(r.Type)
	}
	return strings.ToUpper(r.CallType)
}

// frame converts r. The calls under a frame that is not a call belong to
// the call enclosing it.
func (r *rawFrame) frame() callFrame {
	f := callFrame{Type: r.kind(), To: common.HexToAddress(r.To), Output: hexBytes(r.Output), Error: r.Error}
	if len(f.Output) == 0 {
		f.Output = hexBytes(r.ErrorHexData)
	}
	f.Calls = r.calls()
	return f
}

func (r *rawFrame) calls() []callFrame {
	var out []callFrame
	for i := range r.Calls {
		if c := &r.Calls[i]; callTypes[c.kind()] {
			out = append(out, c.frame())
		} else {
			out = append(out, c.calls()...)
		}
	}
	return out
}

// hexBytes decodes a hex string (nil when it is not one).
func hexBytes(s string) []byte {
	b, err := hexutil.Decode(s)
	if err != nil {
		return nil
	}
	return b
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
// call). A proxy's failed delegatecall whose revert it passed on is the
// proxy's own failure, searched like f. nil if there is none.
func reportedFailure(f *callFrame, as common.Address) *failure {
	var last *failure
	for i := range f.Calls {
		c := &f.Calls[i]
		cas := runsAs(c, as)
		if c.Error != "" && !passedOn(f, c) {
			last = &failure{c, cas}
		} else if inner := reportedFailure(c, cas); inner != nil {
			last = inner
		}
	}
	return last
}

// passedOn reports whether f failed by passing on the revert of c, its
// delegatecall (a proxy calling its implementation).
func passedOn(f, c *callFrame) bool {
	return f.Error != "" && c.Error != "" && (c.Type == "DELEGATECALL" || c.Type == "CALLCODE") && bytes.Equal(c.Output, f.Output)
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
		return evmError(f.Error)
	}
	return decodeRevert(f.Output)
}

// fromTrace takes the errors from a trace: the transaction's, with the
// contract that raised it, and the failure a call reported back (what
// TransactionFailed wraps).
func (an *analysis) fromTrace(root callFrame) {
	an.revert, an.revertAt = frameRevert(&root), origin(failure{&root, root.To}).as
	if f := reportedFailure(&root, root.To); f != nil {
		u := origin(*f)
		an.underlying, an.underlyingAt = frameRevert(u.frame), u.as
	}
}

// revertData returns the revert data an RPC error carries, even when it is
// shorter than a selector.
func revertData(err error) ([]byte, bool) {
	var de rpc.DataError
	if !errors.As(err, &de) {
		return nil, false
	}
	s, ok := de.ErrorData().(string)
	if !ok {
		return nil, false
	}
	data, decErr := hexutil.Decode(s)
	return data, decErr == nil
}

// replay gets the revert data: from the node's trace of the transaction,
// else by calling it again at the block before (which misses what changed
// within its block), else from Tenderly's trace.
func (a *Analyzer) replay(ctx context.Context, an *analysis) {
	var steps []field
	done := func(source string) {
		steps = append(steps, field{source, "reverted: " + an.revert.title()})
		an.add("REPLAY", steps...)
	}

	var trace *rawFrame
	err := a.b.CallRPC(ctx, &trace, "debug_traceTransaction", an.hash, map[string]string{"tracer": "callTracer"})
	switch {
	case err != nil:
		steps = append(steps, field{"debug_traceTransaction", "unavailable: " + oneLine(err)})
	case trace == nil || trace.Error == "":
		steps = append(steps, field{"debug_traceTransaction", "the trace shows no revert"})
	default:
		an.fromTrace(trace.frame())
		done("debug_traceTransaction")
		return
	}

	before := fmt.Sprintf("eth_call at block %d", an.before())
	msg := ethereum.CallMsg{From: an.tx.From, To: an.tx.To, Gas: uint64(an.tx.Gas), Value: an.value, Data: an.tx.Input}
	_, err = a.b.CallContract(ctx, msg, an.before())
	data, hasData := revertData(err)
	outOfGas := err != nil && strings.Contains(err.Error(), "out of gas") // with the transaction's gas limit
	switch {
	case err == nil:
		steps = append(steps, field{before, "succeeds: the failure came from its block (an earlier transaction in it, or its later time: a passed deadline or maximum age)"})
	case hasData && len(data) > 0:
		an.revert = decodeRevert(data)
		done(before)
		return
	case outOfGas && an.gasUsed >= uint64(an.tx.Gas):
		an.revert = evmError("out of gas")
		done(before)
		return
	case outOfGas:
		steps = append(steps, field{before, fmt.Sprintf("runs out of gas, but the transaction used only %d of its %d: the call does not run as the transaction did", an.gasUsed, uint64(an.tx.Gas))})
	case ethx.IsExecutionError(err):
		an.revert = decodeRevert(nil)
		done(before)
		return
	default:
		steps = append(steps, field{before, "unavailable: " + oneLine(err)})
	}

	if !a.opts.Tenderly {
		an.add("REPLAY", append(steps, field{"Tenderly", "not asked (--no-tenderly)"})...)
		return
	}
	root, err := a.tenderly(ctx, an.hash)
	switch {
	case err != nil:
		steps = append(steps, field{"Tenderly", "unavailable: " + oneLine(err)})
	case root.Error == "":
		steps = append(steps, field{"Tenderly", "the trace shows no revert"})
	default:
		an.fromTrace(*root)
		done("Tenderly")
		return
	}
	an.add("REPLAY", steps...)
}

// tenderly reads the transaction's trace from Tenderly's public API.
func (a *Analyzer) tenderly(ctx context.Context, hash common.Hash) (*callFrame, error) {
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
		CallTrace *rawFrame `json:"call_trace"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&trace); err != nil {
		return nil, fmt.Errorf("decode trace: %w", err)
	}
	if trace.CallTrace == nil {
		return nil, errors.New("the answer has no trace")
	}
	root := trace.CallTrace.frame()
	return &root, nil
}

// wraps reports whether r reports that a call the module or the Safe made
// failed, which the error behind it explains.
func wraps(r *revertInfo) bool {
	switch r.source {
	case moduleError:
		return r.name == "TransactionFailed" || r.name == "ApprovalFailed" || r.name == "ModuleTransactionFailed"
	case reasonError:
		return r.detail == "GS013" // a Safe transaction whose call failed
	}
	return false
}

// explain reports the error and, for a failed call the module or the Safe
// made, the error behind it.
func (a *Analyzer) explain(ctx context.Context, an *analysis) {
	r := an.revert
	if r == nil {
		fields := []field{
			{"", "No revert data. Possible causes: the node keeps no history and Tenderly could not help,"},
			{"", "or the failure depended on what changed within the transaction's block."},
		}
		if an.gasUsed >= uint64(an.tx.Gas) {
			fields = append(fields, field{"", fmt.Sprintf("It used all of its gas (%d): it most likely ran out of gas.", an.gasUsed)})
		}
		an.add("ERROR", fields...)
		return
	}
	a.tokenAmounts(ctx, r, an.revertAt)
	a.fitHelp(ctx, an, r, an.revertAt, true)
	fields := []field{{"Error", r.title()}}
	for _, f := range r.args {
		fields = append(fields, field{"  " + f.key, f.value})
	}
	fields = append(fields, field{"Meaning", r.help.description}, field{"Solution", r.help.solution})
	if wraps(r) {
		if an.underlying == nil && an.inner != nil && an.raisedByModule() {
			a.replayInner(ctx, an)
		}
		switch u := an.underlying; {
		case u != nil:
			a.tokenAmounts(ctx, u, an.underlyingAt)
			a.fitHelp(ctx, an, u, an.underlyingAt, false)
			where := ""
			switch {
			case an.underlyingAt != (common.Address{}):
				where = " in " + a.label(ctx, an.underlyingAt)
			case an.inner != nil:
				where = ", from the call to " + a.label(ctx, an.inner.target)
			}
			fields = append(fields, field{"Underlying error", u.title() + where})
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

// moduleCall reports whether the transaction calls a module function of a
// target that answers as a module.
func (an *analysis) moduleCall() bool {
	return an.method != nil && an.module.err == nil
}

// raisedByModule reports whether the transaction's error is the module's
// own: the transaction calls a module function, and the trace shows the
// module raised it (or, without a trace, it surfaced from the module).
func (an *analysis) raisedByModule() bool {
	return an.moduleCall() && (an.revertAt == (common.Address{}) || an.revertAt == *an.to())
}

// fitHelp fits r's help to the contract that raised it, at (zero when no
// trace shows it; top for the transaction's own error). The module's and
// its parsers' explanations only hold for their own errors, as in another
// contract the same name can mean something else, and some depend on the
// function called.
func (a *Analyzer) fitHelp(ctx context.Context, an *analysis, r *revertInfo, at common.Address, top bool) {
	switch r.source {
	case moduleError:
		own := top && an.raisedByModule()
		switch {
		case own && r.name == "Unauthorized" && kindOf(an.method.RawName) == subaccountFunction:
			r.help = roleHelp
		case own && r.name == "Unauthorized":
			r.help = ownerHelp
		case own && r.name == "EnforcedPause" && an.method.RawName == "pause":
			r.help = alreadyPausedHelp
		case own:
		case at == (common.Address{}) || !a.answersAsModule(ctx, an, at):
			// Raised by a contract that is not a module, or not known to be.
			r.help = foreignErrorHelp
		}
	case parserError:
		// Parsers run in module calls, called by the module.
		if !top || !an.moduleCall() {
			r.help = foreignErrorHelp
		}
	case noData:
		switch {
		case top && len(an.tx.Input) == 0 && an.value.Sign() > 0:
			r.help = plainETHHelp
		case top && an.legacy:
			r.help = legacyFunctionHelp
		case !top && an.inner != nil && len(an.inner.data) == 0 && an.inner.value.Sign() > 0:
			r.help = ethRefusedHelp
		}
	}
}

// answersAsModule reports whether the contract at addr answers avatar(), as
// a module does: at the block before the transaction, or at the latest
// block when that block cannot be read.
func (a *Analyzer) answersAsModule(ctx context.Context, an *analysis, addr common.Address) bool {
	if addr == *an.to() && an.method != nil {
		return an.module.err == nil // already read
	}
	_, err := contracts.NewModule(addr, a.b).At(an.before()).Avatar(ctx)
	if err != nil && !ethx.IsExecutionError(err) {
		_, err = contracts.NewModule(addr, a.b).Avatar(ctx)
	}
	return err == nil
}

// tokenAmounts shows the amounts of a token error with the decimals of the
// token that raised it, when the trace shows which one did (and it is a
// token: it has a symbol). Otherwise they stay in its smallest unit.
func (a *Analyzer) tokenAmounts(ctx context.Context, r *revertInfo, token common.Address) {
	if r.name != "ERC20InsufficientBalance" && r.name != "ERC20InsufficientAllowance" {
		return
	}
	known := token != (common.Address{}) && a.symbol(ctx, token) != ""
	for i, v := range r.values {
		if x, ok := v.(*big.Int); ok {
			if known {
				r.args[i].value = a.amount(ctx, token, x)
			} else {
				r.args[i].value += " (in the smallest unit of the token that raised it)"
			}
		}
	}
}

// replayInner calls the Safe's call again, from the Safe at the block before,
// to find the error behind TransactionFailed when there is no trace.
func (a *Analyzer) replayInner(ctx context.Context, an *analysis) {
	if an.module.err != nil {
		an.underlyingNote = "unknown (could not read the module's Safe: " + oneLine(an.module.err) + ")"
		return
	}
	c := an.inner
	_, err := a.b.CallContract(ctx, ethereum.CallMsg{From: an.module.safe, To: &c.target, Value: c.value, Data: c.data}, an.before())
	data, hasData := revertData(err)
	switch {
	case err == nil:
		an.underlyingNote = fmt.Sprintf("unknown: the call succeeds when made alone at block %d", an.before())
	case hasData:
		an.underlying = decodeRevert(data)
	case ethx.IsExecutionError(err):
		an.underlying = decodeRevert(nil)
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
