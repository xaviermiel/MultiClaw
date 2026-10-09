package analyze

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

// field is a line of the report: "key: value", or value alone when key is "".
type field struct{ key, value string }

// revertInfo is decoded revert data.
type revertInfo struct {
	// name is the error's name ("Error" for Error(string), "Panic" for
	// Panic(uint256)), or "" when the analyzer does not know it.
	name string
	// detail is Error's reason, or what Panic's code means.
	detail string
	// args are a custom error's arguments, and values their decoded values.
	args   []field
	values []any
	data   []byte
	help   help
}

var (
	errorStringSelector = [4]byte{0x08, 0xc3, 0x79, 0xa0} // Error(string)
	panicSelector       = [4]byte{0x4e, 0x48, 0x7b, 0x71} // Panic(uint256)

	stringArgs  = abi.Arguments{{Type: mustType("string")}}
	uint256Args = abi.Arguments{{Type: mustType("uint256")}}
)

// commonErrors indexes contracts.CommonErrorsABI by selector.
var commonErrors = func() map[[4]byte]abi.Error {
	m := map[[4]byte]abi.Error{}
	for _, e := range contracts.CommonErrorsABI.Errors {
		m[[4]byte(e.ID[:4])] = e
	}
	return m
}()

// usdErrorArgs are the errors whose number arguments are USD values with 18
// decimals.
var usdErrorArgs = map[string]bool{
	"ExceedsAllowanceCap":            true,
	"ExceedsCumulativeSpendingLimit": true,
	"ExceedsOracleAcquiredBudget":    true,
}

// decodeRevert decodes revert data: Error(string), Panic(uint256), a module
// or parser error, or an OpenZeppelin token error.
func decodeRevert(data []byte) *revertInfo {
	r := &revertInfo{data: data, help: unknownHelp}
	if len(data) < 4 {
		return r
	}
	sel := [4]byte(data[:4])
	switch sel {
	case errorStringSelector:
		vals, err := stringArgs.Unpack(data[4:])
		if err != nil {
			return r
		}
		reason := vals[0].(string)
		r.name, r.detail = "Error", reason
		r.help = help{"The contract reverted with this reason.", "The reason comes from the contract that reverted."}
		if h, ok := reasonHelp[reason]; ok {
			r.help = h
		}
		return r
	case panicSelector:
		vals, err := uint256Args.Unpack(data[4:])
		if err != nil {
			return r
		}
		code := vals[0].(*big.Int)
		meaning := "unknown code"
		if m, ok := panicCodes[code.Uint64()]; ok && code.IsUint64() {
			meaning = m
		}
		r.name, r.detail, r.help = "Panic", fmt.Sprintf("0x%x (%s)", code, meaning), panicHelp
		return r
	}
	e, ok := contracts.LookupError(sel)
	if !ok {
		e, ok = commonErrors[sel]
	}
	if !ok {
		return r
	}
	vals, err := e.Inputs.Unpack(data[4:])
	if err != nil {
		return r
	}
	r.name, r.help, r.values = e.Name, errorHelp[e.Name], vals
	for i, in := range e.Inputs {
		v := formatPlain(vals[i])
		if x, ok := vals[i].(*big.Int); ok && usdErrorArgs[e.Name] {
			v = usd(x)
		}
		r.args = append(r.args, field{in.Name, v})
	}
	return r
}

// title names the error for the report.
func (r *revertInfo) title() string {
	switch {
	case r.name == "Error":
		return fmt.Sprintf("%q", r.detail)
	case r.name == "Panic":
		return "Panic " + r.detail
	case r.name != "":
		return r.name
	case len(r.data) == 0:
		return "reverted without data"
	case len(r.data) < 4:
		return "unknown error " + hexutil.Encode(r.data)
	}
	return fmt.Sprintf("unknown error %s (%d bytes)", hexutil.Encode(r.data[:4]), len(r.data))
}

// decodeCall decodes data as a call of one of a's functions.
func decodeCall(a *abi.ABI, data []byte) (*abi.Method, []any, error) {
	if len(data) < 4 {
		return nil, nil, errors.New("no function selector")
	}
	m, err := a.MethodById(data[:4])
	if err != nil {
		return nil, nil, err
	}
	args, err := m.Inputs.Unpack(data[4:])
	if err != nil {
		return m, nil, fmt.Errorf("the arguments do not match %s: %w", m.Sig, err)
	}
	return m, args, nil
}

// innerCall is the call the module has the Safe make.
type innerCall struct {
	target common.Address
	data   []byte
	value  *big.Int
	// method and args decode data with ProtocolCallsABI (nil method when it
	// does not know the selector).
	method *abi.Method
	args   []any
	// spends is the token the call spends and how much, when it is known.
	spends *tokenAmount
}

type tokenAmount struct {
	token  common.Address
	amount *big.Int
}

func newInnerCall(target common.Address, data []byte, value *big.Int) *innerCall {
	c := &innerCall{target: target, data: data, value: value}
	m, args, err := decodeCall(&contracts.ProtocolCallsABI, data)
	if err != nil {
		return c
	}
	c.method, c.args = m, args
	switch m.RawName {
	case "approve", "transfer":
		c.spends = &tokenAmount{target, args[1].(*big.Int)}
	case "supply":
		c.spends = &tokenAmount{args[0].(common.Address), args[1].(*big.Int)}
	case "exactInputSingle":
		c.spends = &tokenAmount{tuple[common.Address](args[0], "tokenIn"), tuple[*big.Int](args[0], "amountIn")}
	case "exactOutputSingle":
		c.spends = &tokenAmount{tuple[common.Address](args[0], "tokenIn"), tuple[*big.Int](args[0], "amountInMaximum")}
	case "exactInput":
		if tokens, _, ok := swapPath(tuple[[]byte](args[0], "path")); ok {
			c.spends = &tokenAmount{tokens[0], tuple[*big.Int](args[0], "amountIn")}
		}
	case "exactOutput":
		// The path of an exact-output swap goes from the output token.
		if tokens, _, ok := swapPath(tuple[[]byte](args[0], "path")); ok {
			c.spends = &tokenAmount{tokens[len(tokens)-1], tuple[*big.Int](args[0], "amountInMaximum")}
		}
	}
	return c
}

// tuple returns the component name of v, an unpacked ABI tuple (the zero
// value when it has none).
func tuple[T any](v any, name string) T {
	var zero T
	f := reflect.ValueOf(v).FieldByName(abi.ToCamelCase(name))
	if !f.IsValid() {
		return zero
	}
	x, _ := f.Interface().(T)
	return x
}

// swapPath decodes a Uniswap V3 path: token, fee, token, ... with 20-byte
// tokens and 3-byte fees.
func swapPath(path []byte) (tokens []common.Address, fees []*big.Int, ok bool) {
	if len(path) < 43 || (len(path)-20)%23 != 0 {
		return nil, nil, false
	}
	for i := 0; ; i += 23 {
		tokens = append(tokens, common.BytesToAddress(path[i:i+20]))
		if i+20 == len(path) {
			return tokens, fees, true
		}
		fees = append(fees, new(big.Int).SetBytes(path[i+20:i+23]))
	}
}

// formatPlain formats a decoded ABI value without looking anything up.
func formatPlain(v any) string {
	switch x := v.(type) {
	case common.Address:
		return x.Hex()
	case *big.Int:
		return x.String()
	case []byte:
		return shortBytes(x)
	case string:
		return x
	}
	rv := reflect.ValueOf(v)
	switch {
	case rv.Kind() == reflect.Array && rv.Type().Elem().Kind() == reflect.Uint8: // bytesN
		b := make([]byte, rv.Len())
		reflect.Copy(reflect.ValueOf(b), rv)
		return hexutil.Encode(b)
	case rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array:
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = formatPlain(rv.Index(i).Interface())
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return fmt.Sprint(v)
}

// shortBytes shows short byte strings whole, and longer ones as their first
// 4 bytes (a selector, for calldata) and length.
func shortBytes(b []byte) string {
	if len(b) <= 36 {
		return hexutil.Encode(b)
	}
	return fmt.Sprintf("%s… (%d bytes)", hexutil.Encode(b[:4]), len(b))
}

// words shows calldata the analyzer cannot decode as 32-byte words.
func words(data []byte) []field {
	const max = 8
	var out []field
	n := 0
	for i := 4; i < len(data); i += 32 {
		if n == max {
			out = append(out, field{"", fmt.Sprintf("… %d more bytes", len(data)-i)})
			break
		}
		out = append(out, field{fmt.Sprintf("word %d", n), hexutil.Encode(data[i:min(i+32, len(data))])})
		n++
	}
	return out
}

// usd formats a USD value with 18 decimals.
func usd(v *big.Int) string {
	return bn.FormatUnits(v, 18) + " USD"
}

// usdArgs are the module function arguments holding USD values (18
// decimals).
var usdArgs = map[string]bool{"newAllowance": true, "totalValueUSD": true, "maxSpendingUSD": true}

// formatArg formats an argument of a module function, naming tokens.
func (a *Analyzer) formatArg(ctx context.Context, name string, v any) string {
	switch x := v.(type) {
	case common.Address:
		return a.label(ctx, x)
	case []common.Address:
		parts := make([]string, len(x))
		for i, addr := range x {
			parts[i] = a.label(ctx, addr)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *big.Int:
		if usdArgs[name] {
			return x.String() + " (" + usd(x) + ")"
		}
	}
	return formatPlain(v)
}

// label names an address by its token symbol, when it has one.
func (a *Analyzer) label(ctx context.Context, addr common.Address) string {
	if s := a.symbol(ctx, addr); s != "" {
		return s + " (" + addr.Hex() + ")"
	}
	return addr.Hex()
}

// symbol returns a token's symbol: the chain preset's, else the token's own
// ("" for an address that is not a token).
func (a *Analyzer) symbol(ctx context.Context, token common.Address) string {
	if s, ok := a.symbols[token]; ok {
		return s
	}
	s, err := contracts.NewERC20(token, a.b).Symbol(ctx)
	if err != nil {
		s = ""
	}
	a.symbols[token] = s
	return s
}

// tokenDecimals returns a token's decimals (18 for ETH, as address 0, and
// when the token does not say).
func (a *Analyzer) tokenDecimals(ctx context.Context, token common.Address) int {
	if token == (common.Address{}) {
		return 18
	}
	if d, ok := a.decimals[token]; ok {
		return d
	}
	d := 18
	if v, err := contracts.NewERC20(token, a.b).Decimals(ctx); err == nil {
		d = int(v)
	}
	a.decimals[token] = d
	return d
}

var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// amount formats an amount of token (address 0 for ETH).
func (a *Analyzer) amount(ctx context.Context, token common.Address, v *big.Int) string {
	sym := a.symbol(ctx, token)
	switch {
	case token == (common.Address{}):
		sym = "ETH"
	case sym == "":
		sym = shortAddress(token)
	}
	if v.Cmp(maxUint256) == 0 {
		return "MAX (unlimited " + sym + ")"
	}
	return bn.FormatUnits(v, a.tokenDecimals(ctx, token)) + " " + sym
}

func shortAddress(a common.Address) string {
	h := a.Hex()
	return h[:6] + "…" + h[len(h)-4:]
}

func mustType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return typ
}
