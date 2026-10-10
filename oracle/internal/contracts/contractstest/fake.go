// Package contractstest provides an in-memory eth_call backend for tests:
// handlers are registered per (contract address, method) and receive the
// decoded call arguments.
package contractstest

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Handler returns the output values for a call with the given decoded args.
type Handler func(args []any) ([]any, error)

// Call records one eth_call made against the fake.
type Call struct {
	From   common.Address
	To     common.Address
	Gas    uint64
	Value  *big.Int
	Method string
	Args   []any
	Block  *big.Int
}

type key struct {
	addr     common.Address
	selector [4]byte
}

type entry struct {
	method abi.Method
	h      Handler
}

// Fake implements contracts.Caller.
type Fake struct {
	mu       sync.Mutex
	handlers map[key]entry
	calls    []Call
}

// New returns an empty Fake.
func New() *Fake { return &Fake{handlers: make(map[key]entry)} }

// On registers h for method of contract a at addr.
func (f *Fake) On(addr common.Address, a *abi.ABI, method string, h Handler) *Fake {
	m, ok := a.Methods[method]
	if !ok {
		panic(fmt.Sprintf("contractstest: method %s not in ABI", method))
	}
	var sel [4]byte
	copy(sel[:], m.ID)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[key{addr, sel}] = entry{method: m, h: h}
	return f
}

// Return registers a handler that always returns outs.
func (f *Fake) Return(addr common.Address, a *abi.ABI, method string, outs ...any) *Fake {
	return f.On(addr, a, method, func([]any) ([]any, error) { return outs, nil })
}

// Revert registers a handler that reverts with data (nil for a bare revert).
func (f *Fake) Revert(addr common.Address, a *abi.ABI, method string, data []byte) *Fake {
	return f.On(addr, a, method, func([]any) ([]any, error) { return nil, &RevertError{Data: data} })
}

// Calls returns the calls made so far.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// CallContract implements contracts.Caller.
func (f *Fake) CallContract(_ context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	if msg.To == nil || len(msg.Data) < 4 {
		return nil, fmt.Errorf("contractstest: malformed call")
	}
	var sel [4]byte
	copy(sel[:], msg.Data[:4])

	f.mu.Lock()
	e, ok := f.handlers[key{*msg.To, sel}]
	f.mu.Unlock()
	if !ok {
		// Like calling a contract without that function: revert, no data.
		return nil, &RevertError{}
	}
	args, err := e.method.Inputs.Unpack(msg.Data[4:])
	if err != nil {
		return nil, fmt.Errorf("contractstest: unpack %s args: %w", e.method.Name, err)
	}

	f.mu.Lock()
	f.calls = append(f.calls, Call{From: msg.From, To: *msg.To, Gas: msg.Gas, Value: msg.Value, Method: e.method.Name, Args: args, Block: block})
	f.mu.Unlock()

	outs, err := e.h(args)
	if err != nil {
		return nil, err
	}
	if outs == nil {
		return []byte{}, nil // empty return data
	}
	return e.method.Outputs.Pack(outs...)
}

// RevertError mimics the error go-ethereum returns for a reverted eth_call:
// it implements rpc.DataError with the revert data hex-encoded.
type RevertError struct{ Data []byte }

func (e *RevertError) Error() string { return "execution reverted" }

// ErrorCode is the JSON-RPC code used by geth for reverts.
func (e *RevertError) ErrorCode() int { return 3 }

// ErrorData returns the hex-encoded revert data (nil when there is none).
func (e *RevertError) ErrorData() interface{} {
	if e.Data == nil {
		return nil
	}
	return hexutil.Encode(e.Data)
}
