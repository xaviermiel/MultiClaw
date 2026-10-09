package contracts

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// dataErr implements rpc.DataError like go-ethereum's JSON-RPC errors.
type dataErr struct {
	msg  string
	data any
}

func (e *dataErr) Error() string          { return e.msg }
func (e *dataErr) ErrorData() interface{} { return e.data }

func encodeError(t *testing.T, e abi.Error, args ...any) []byte {
	t.Helper()
	enc, err := e.Inputs.Pack(args...)
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]byte{}, e.ID[:4]...), enc...)
}

func TestDecodeRevertModuleErrors(t *testing.T) {
	data := encodeError(t, ModuleABI.Errors["ExceedsAllowanceCap"], big.NewInt(1500), big.NewInt(1000))
	if got, ok := DecodeRevert(data); !ok || got != "ExceedsAllowanceCap(1500, 1000)" {
		t.Fatalf("ExceedsAllowanceCap: %q %v", got, ok)
	}
	// No arguments: just the name (viem formatting).
	data = encodeError(t, ModuleABI.Errors["LengthMismatch"])
	if got, ok := DecodeRevert(data); !ok || got != "LengthMismatch" {
		t.Fatalf("LengthMismatch: %q %v", got, ok)
	}
	// Parser errors bubble up through module calls.
	data = encodeError(t, ParserErrorsABI.Errors["InvalidPath"])
	if got, ok := DecodeRevert(data); !ok || got != "InvalidPath" {
		t.Fatalf("InvalidPath: %q %v", got, ok)
	}
}

func TestDecodeRevertBuiltins(t *testing.T) {
	strArgs := abi.Arguments{{Type: mustType("string")}}
	enc, _ := strArgs.Pack("boom")
	data := append([]byte{0x08, 0xc3, 0x79, 0xa0}, enc...)
	if got, ok := DecodeRevert(data); !ok || got != "Error(boom)" {
		t.Fatalf("Error(string): %q %v", got, ok)
	}
	uintArgs := abi.Arguments{{Type: mustType("uint256")}}
	enc, _ = uintArgs.Pack(big.NewInt(17))
	data = append([]byte{0x4e, 0x48, 0x7b, 0x71}, enc...)
	if got, ok := DecodeRevert(data); !ok || got != "Panic(17)" {
		t.Fatalf("Panic: %q %v", got, ok)
	}
}

func TestDescribeRevert(t *testing.T) {
	// Decodable revert data.
	data := encodeError(t, ModuleABI.Errors["ExceedsOracleAcquiredBudget"], big.NewInt(3), big.NewInt(2))
	err := fmt.Errorf("simulate: %w", &dataErr{msg: "execution reverted", data: hexutil.Encode(data)})
	if got := DescribeRevert(err); got != "ExceedsOracleAcquiredBudget(3, 2)" {
		t.Errorf("decoded: %q", got)
	}

	// Unknown selector: raw data truncated to 74 characters, like the TS.
	unknown := append([]byte{0xde, 0xad, 0xbe, 0xef}, make([]byte, 64)...)
	got := DescribeRevert(&dataErr{msg: "execution reverted", data: hexutil.Encode(unknown)})
	if !strings.HasPrefix(got, "raw=0xdeadbeef") || len(got) != len("raw=")+74 {
		t.Errorf("raw: %q (len %d)", got, len(got))
	}

	// No revert data: the message, truncated to 200 characters.
	long := strings.Repeat("x", 250)
	if got := DescribeRevert(errors.New(long)); len(got) != 200 {
		t.Errorf("message truncation: len %d", len(got))
	}
	if got := DescribeRevert(&dataErr{msg: "execution reverted", data: nil}); got != "execution reverted" {
		t.Errorf("data-less DataError: %q", got)
	}
	if got := DescribeRevert(&dataErr{msg: "short", data: "0x1234"}); got != "short" {
		t.Errorf("data shorter than a selector should be ignored: %q", got)
	}
}

func TestEveryKnownErrorDecodes(t *testing.T) {
	// Each module/parser error with zero-valued arguments must round-trip.
	for _, set := range []abi.ABI{ModuleABI, ParserErrorsABI} {
		for name, e := range set.Errors {
			args := make([]any, len(e.Inputs))
			for i, in := range e.Inputs {
				args[i] = zeroValue(t, in.Type)
			}
			data := encodeError(t, e, args...)
			got, ok := DecodeRevert(data)
			if !ok || !strings.HasPrefix(got, name) {
				t.Errorf("%s: decoded %q, ok=%v", name, got, ok)
			}
		}
	}
}

func zeroValue(t *testing.T, typ abi.Type) any {
	t.Helper()
	switch typ.T {
	case abi.UintTy, abi.IntTy:
		if typ.Size > 64 {
			return big.NewInt(0)
		}
		switch typ.Size {
		case 8:
			return uint8(0)
		case 16:
			return uint16(0)
		case 32:
			return uint32(0)
		default:
			return uint64(0)
		}
	case abi.AddressTy:
		return [20]byte{}
	case abi.BoolTy:
		return false
	case abi.StringTy:
		return ""
	case abi.BytesTy:
		return []byte{}
	case abi.FixedBytesTy:
		if typ.Size == 4 {
			return [4]byte{}
		}
		return [32]byte{}
	}
	t.Fatalf("unsupported error arg type %s", typ)
	return nil
}
