package contracts

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

var (
	errorStringSelector = [4]byte{0x08, 0xc3, 0x79, 0xa0} // Error(string)
	panicSelector       = [4]byte{0x4e, 0x48, 0x7b, 0x71} // Panic(uint256)
)

// knownErrors maps a 4-byte selector to its custom error (module errors first,
// then parser errors, since parsers revert inside module calls).
var knownErrors = func() map[[4]byte]abi.Error {
	m := make(map[[4]byte]abi.Error)
	for _, set := range []abi.ABI{ModuleABI, ParserErrorsABI} {
		for _, e := range set.Errors {
			var sel [4]byte
			copy(sel[:], e.ID[:4])
			if _, dup := m[sel]; !dup {
				m[sel] = e
			}
		}
	}
	return m
}()

// LookupError returns the custom error with the given selector, if known.
func LookupError(selector [4]byte) (abi.Error, bool) {
	e, ok := knownErrors[selector]
	return e, ok
}

// RevertData extracts the revert data carried by an RPC error (the JSON-RPC
// error's "data" field).
func RevertData(err error) ([]byte, bool) {
	var de rpc.DataError
	if !errors.As(err, &de) {
		return nil, false
	}
	s, ok := de.ErrorData().(string)
	if !ok {
		return nil, false
	}
	data, decErr := hexutil.Decode(s)
	if decErr != nil || len(data) < 4 {
		return nil, false
	}
	return data, true
}

// DescribeRevert renders an error for logs the way the TS decodeRevertReason
// did: a decoded "ErrorName(arg, ...)" when revert data is present, the raw
// data (truncated) when it can't be decoded, else the error message
// truncated to 200 characters.
func DescribeRevert(err error) string {
	if data, ok := RevertData(err); ok {
		if s, ok := DecodeRevert(data); ok {
			return s
		}
		raw := hexutil.Encode(data)
		if len(raw) > 74 {
			raw = raw[:74]
		}
		return "raw=" + raw
	}
	return truncateRunes(err.Error(), 200)
}

// DecodeRevert decodes revert data as Error(string), Panic(uint256), or a
// module/parser custom error. Without arguments only the name is returned,
// matching viem-based formatting in the TS oracle.
func DecodeRevert(data []byte) (string, bool) {
	if len(data) < 4 {
		return "", false
	}
	var sel [4]byte
	copy(sel[:], data[:4])

	switch sel {
	case errorStringSelector:
		vals, err := abi.Arguments{{Type: mustType("string")}}.Unpack(data[4:])
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("Error(%s)", vals[0]), true
	case panicSelector:
		vals, err := abi.Arguments{{Type: mustType("uint256")}}.Unpack(data[4:])
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("Panic(%s)", vals[0]), true
	}

	e, ok := knownErrors[sel]
	if !ok {
		return "", false
	}
	vals, err := e.Inputs.Unpack(data[4:])
	if err != nil {
		return "", false
	}
	if len(vals) == 0 {
		return e.Name, true
	}
	args := make([]string, len(vals))
	for i, v := range vals {
		args[i] = formatValue(v)
	}
	return e.Name + "(" + strings.Join(args, ", ") + ")", true
}

// formatValue mimics JavaScript's String(x) for decoded ABI values.
func formatValue(v any) string {
	switch x := v.(type) {
	case *big.Int:
		return x.String()
	case common.Address:
		return x.Hex()
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []byte:
		return hexutil.Encode(x)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = formatValue(rv.Index(i).Interface())
		}
		return strings.Join(parts, ",") // String([a, b]) === "a,b"
	case reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 { // bytesN
			b := make([]byte, rv.Len())
			reflect.Copy(reflect.ValueOf(b), rv)
			return hexutil.Encode(b)
		}
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = formatValue(rv.Index(i).Interface())
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v)
}

func mustType(t string) abi.Type {
	typ, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(err)
	}
	return typ
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
