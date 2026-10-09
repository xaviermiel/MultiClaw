package analyze

import (
	"math/big"
	"reflect"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

func TestEveryErrorIsExplained(t *testing.T) {
	known := map[string]bool{}
	for _, set := range []abi.ABI{contracts.ModuleABI, contracts.ParserErrorsABI, contracts.CommonErrorsABI} {
		for name := range set.Errors {
			known[name] = true
			if h, ok := errorHelp[name]; !ok || h.description == "" || h.solution == "" {
				t.Errorf("no help for %s", name)
			}
		}
	}
	for name := range errorHelp {
		if !known[name] {
			t.Errorf("help for %s, which no ABI has", name)
		}
	}
	if len(contracts.ModuleABI.Errors) < 40 || len(contracts.ParserErrorsABI.Errors) < 4 {
		t.Fatalf("%d module and %d parser errors", len(contracts.ModuleABI.Errors), len(contracts.ParserErrorsABI.Errors))
	}
}

func packError(t *testing.T, a *abi.ABI, name string, args ...any) []byte {
	t.Helper()
	e := a.Errors[name]
	data, err := e.Inputs.Pack(args...)
	if err != nil {
		t.Fatal(err)
	}
	return append(slices.Clone(e.ID[:4]), data...)
}

func reasonData(t *testing.T, reason string) []byte {
	t.Helper()
	data, err := stringArgs.Pack(reason)
	if err != nil {
		t.Fatal(err)
	}
	return append(errorStringSelector[:], data...)
}

func panicData(t *testing.T, code int64) []byte {
	t.Helper()
	data, err := uint256Args.Pack(big.NewInt(code))
	if err != nil {
		t.Fatal(err)
	}
	return append(panicSelector[:], data...)
}

func TestDecodeRevert(t *testing.T) {
	e18 := func(v int64) *big.Int { return new(big.Int).Mul(big.NewInt(v), big.NewInt(1e18)) }
	safe := common.HexToAddress("0x6E7692fFE42ca2A3FA2b08611AA7e79A2AaA8e8C")
	other := common.HexToAddress("0x1111111111111111111111111111111111111111")
	m, p, c := &contracts.ModuleABI, &contracts.ParserErrorsABI, &contracts.CommonErrorsABI
	cases := []struct {
		name  string
		data  []byte
		title string
		args  []field
		help  help
	}{
		{"module error with USD arguments", packError(t, m, "ExceedsAllowanceCap", e18(30), bn.MustParse("29224015602441363890")),
			"ExceedsAllowanceCap", []field{{"requested", "30 USD"}, {"maximum", "29.22401560244136389 USD"}}, errorHelp["ExceedsAllowanceCap"]},
		{"module error with addresses", packError(t, m, "InvalidRecipient", other, safe),
			"InvalidRecipient", []field{{"recipient", other.Hex()}, {"expected", safe.Hex()}}, errorHelp["InvalidRecipient"]},
		{"bytes4 argument", packError(t, m, "UnknownSelector", [4]byte{0xde, 0xad, 0xbe, 0xef}),
			"UnknownSelector", []field{{"selector", "0xdeadbeef"}}, errorHelp["UnknownSelector"]},
		{"parser error", packError(t, p, "UnsupportedSelector"), "UnsupportedSelector", nil, errorHelp["UnsupportedSelector"]},
		{"token error", packError(t, c, "ERC20InsufficientBalance", safe, big.NewInt(5), big.NewInt(7)),
			"ERC20InsufficientBalance", []field{{"sender", safe.Hex()}, {"balance", "5"}, {"needed", "7"}}, errorHelp["ERC20InsufficientBalance"]},
		{"known reason", reasonData(t, "STF"), `"STF"`, nil, reasonHelp["STF"]},
		{"other reason", reasonData(t, "Invalid active nodes count"), `"Invalid active nodes count"`, nil,
			help{"The contract reverted with this reason.", "The reason comes from the contract that reverted."}},
		{"panic", panicData(t, 0x11), "Panic 0x11 (arithmetic overflow or underflow)", nil, panicHelp},
		{"unknown panic code", panicData(t, 0x99), "Panic 0x99 (unknown code)", nil, panicHelp},
		{"unknown error", []byte{0xde, 0xad, 0xbe, 0xef, 1}, "unknown error 0xdeadbeef (5 bytes)", nil, unknownHelp},
		{"known selector, malformed arguments", packError(t, m, "InvalidRecipient", other, safe)[:40],
			"unknown error " + hexutil.Encode(packError(t, m, "InvalidRecipient", other, safe)[:4]) + " (40 bytes)", nil, unknownHelp},
		{"no data", nil, "reverted without data", nil, unknownHelp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeRevert(tc.data)
			if r.title() != tc.title || !slices.Equal(r.args, tc.args) || r.help != tc.help {
				t.Fatalf("got %q %v %v", r.title(), r.args, r.help)
			}
		})
	}
}

// path encodes a Uniswap V3 path.
func path(hops ...any) []byte {
	var out []byte
	for _, h := range hops {
		switch x := h.(type) {
		case common.Address:
			out = append(out, x.Bytes()...)
		case int:
			out = append(out, byte(x>>16), byte(x>>8), byte(x))
		}
	}
	return out
}

var (
	usdc = common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e")
	weth = common.HexToAddress("0x4200000000000000000000000000000000000006")
	dai  = common.HexToAddress("0x00000000000000000000000000000000000000da")
)

func TestSwapPath(t *testing.T) {
	tokens, fees, ok := swapPath(path(usdc, 500, weth, 3000, dai))
	if !ok || !slices.Equal(tokens, []common.Address{usdc, weth, dai}) || len(fees) != 2 || fees[0].Int64() != 500 || fees[1].Int64() != 3000 {
		t.Fatalf("tokens %v fees %v ok %v", tokens, fees, ok)
	}
	for _, bad := range [][]byte{nil, path(usdc), path(usdc, 500), append(path(usdc, 500, weth), 1)} {
		if _, _, ok := swapPath(bad); ok {
			t.Errorf("accepted a %d-byte path", len(bad))
		}
	}
}

func pack(t *testing.T, method string, args ...any) []byte {
	t.Helper()
	data, err := contracts.ProtocolCallsABI.Pack(method, args...)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// swapParams builds a Uniswap V3 router params tuple; deadline nil for
// SwapRouter02.
func swapParams(m string, deadline *big.Int, fields map[string]any) []byte {
	method := contracts.ProtocolCallsABI.Methods[m]
	typ := method.Inputs[0].Type
	v := newTuple(typ)
	for name, val := range fields {
		setTuple(v, name, val)
	}
	if deadline != nil {
		setTuple(v, "deadline", deadline)
	}
	data, err := method.Inputs.Pack(v.Interface())
	if err != nil {
		panic(err)
	}
	return append(slices.Clone(method.ID), data...)
}

// newTuple returns a zero value of a tuple type, and setTuple sets one of
// its components (if it has it).
func newTuple(typ abi.Type) reflect.Value { return reflect.New(typ.TupleType).Elem() }

func setTuple(v reflect.Value, name string, val any) {
	if f := v.FieldByName(abi.ToCamelCase(name)); f.IsValid() {
		f.Set(reflect.ValueOf(val))
	}
}

func TestInnerCallSpends(t *testing.T) {
	router := common.HexToAddress("0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4")
	pool := common.HexToAddress("0x07eA79F68B2B3df564D0A34F8e19D9B1e339814b")
	safe := common.HexToAddress("0x6E7692fFE42ca2A3FA2b08611AA7e79A2AaA8e8C")
	amt := big.NewInt(1_000_000)
	single := map[string]any{"tokenIn": usdc, "tokenOut": weth, "fee": big.NewInt(500), "recipient": safe,
		"amountIn": amt, "amountOut": big.NewInt(7), "amountOutMinimum": big.NewInt(1), "amountInMaximum": big.NewInt(2_000_000), "sqrtPriceLimitX96": new(big.Int)}
	multi := map[string]any{"path": path(usdc, 500, weth), "recipient": safe, "amountIn": amt, "amountOut": big.NewInt(7),
		"amountOutMinimum": big.NewInt(1), "amountInMaximum": big.NewInt(2_000_000)}
	exactOutMulti := map[string]any{"path": path(weth, 500, usdc), "recipient": safe, "amountOut": big.NewInt(7), "amountInMaximum": big.NewInt(2_000_000)}
	cases := []struct {
		name   string
		target common.Address
		data   []byte
		method string
		spends *tokenAmount
	}{
		{"approve", usdc, pack(t, "approve", router, amt), "approve", &tokenAmount{usdc, amt}},
		{"transfer", usdc, pack(t, "transfer", router, amt), "transfer", &tokenAmount{usdc, amt}},
		{"supply", pool, pack(t, "supply", usdc, amt, safe, uint16(0)), "supply", &tokenAmount{usdc, amt}},
		{"withdraw spends nothing", pool, pack(t, "withdraw", usdc, amt, safe), "withdraw", nil},
		{"exactInputSingle (SwapRouter)", router, swapParams("exactInputSingle", big.NewInt(1), single), "exactInputSingle", &tokenAmount{usdc, amt}},
		{"exactInputSingle (SwapRouter02)", router, swapParams("exactInputSingle0", nil, single), "exactInputSingle", &tokenAmount{usdc, amt}},
		{"exactOutputSingle", router, swapParams("exactOutputSingle0", nil, single), "exactOutputSingle", &tokenAmount{usdc, big.NewInt(2_000_000)}},
		{"exactInput spends the path's first token", router, swapParams("exactInput", big.NewInt(1), multi), "exactInput", &tokenAmount{usdc, amt}},
		{"exactOutput spends the path's last token", router, swapParams("exactOutput0", nil, exactOutMulti), "exactOutput", &tokenAmount{usdc, big.NewInt(2_000_000)}},
		{"unknown function", router, []byte{0xde, 0xad, 0xbe, 0xef}, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newInnerCall(tc.target, tc.data, new(big.Int))
			name := ""
			if c.method != nil {
				name = c.method.RawName
			}
			if name != tc.method {
				t.Fatalf("method %q, want %q", name, tc.method)
			}
			switch {
			case (c.spends == nil) != (tc.spends == nil):
				t.Fatalf("spends %+v, want %+v", c.spends, tc.spends)
			case c.spends != nil && (c.spends.token != tc.spends.token || c.spends.amount.Cmp(tc.spends.amount) != 0):
				t.Fatalf("spends %v %s, want %v %s", c.spends.token, c.spends.amount, tc.spends.token, tc.spends.amount)
			}
		})
	}
}

func TestWords(t *testing.T) {
	data := make([]byte, 4+32*10+5)
	got := words(data)
	if len(got) != 9 || got[0].key != "word 0" || got[7].key != "word 7" || got[8].value != "… 69 more bytes" {
		t.Fatalf("%v", got)
	}
	if got := words(make([]byte, 4+40)); len(got) != 2 || len(got[1].value) != 2+16 {
		t.Fatalf("a partial last word: %v", got)
	}
}
