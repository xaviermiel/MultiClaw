package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
)

// fakeRPC is a minimal JSON-RPC node for command tests. eth_call is answered
// by a contractstest.Fake; other methods by fixed values.
type fakeRPC struct {
	*contractstest.Fake
	URL string

	chainID uint64
	block   uint64
	balance *big.Int

	mu      sync.Mutex
	methods []string
}

func newFakeRPC(t *testing.T, chainID uint64) *fakeRPC {
	t.Helper()
	f := &fakeRPC{Fake: contractstest.New(), chainID: chainID, block: 16, balance: big.NewInt(0)}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	f.URL = server.URL
	return f
}

func (f *fakeRPC) called(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.methods {
		if m == method {
			return true
		}
	}
	return false
}

type rpcRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (f *fakeRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.methods = append(f.methods, req.Method)
	f.mu.Unlock()

	result, rpcErr := f.answer(r.Context(), req)
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *fakeRPC) answer(ctx context.Context, req rpcRequest) (any, *rpcErrorBody) {
	switch req.Method {
	case "eth_chainId":
		return hexutil.Uint64(f.chainID), nil
	case "eth_blockNumber":
		return hexutil.Uint64(f.block), nil
	case "eth_getBalance":
		return (*hexutil.Big)(f.balance), nil
	case "eth_getLogs":
		return []any{}, nil // a chain without events
	case "eth_getBlockByNumber":
		var tag string
		if len(req.Params) == 0 || json.Unmarshal(req.Params[0], &tag) != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "invalid params"}
		}
		n := f.block
		if tag != "latest" {
			v, err := hexutil.DecodeUint64(tag)
			if err != nil {
				return nil, &rpcErrorBody{Code: -32602, Message: err.Error()}
			}
			n = v
		}
		return map[string]any{
			"number":        hexutil.Uint64(n),
			"hash":          common.BigToHash(new(big.Int).SetUint64(n + 1)),
			"timestamp":     hexutil.Uint64(1_700_000_000 + n*2),
			"baseFeePerGas": (*hexutil.Big)(big.NewInt(1_000_000)),
		}, nil
	case "eth_call":
		var arg struct {
			From  common.Address  `json:"from"`
			To    *common.Address `json:"to"`
			Data  hexutil.Bytes   `json:"data"`
			Input hexutil.Bytes   `json:"input"`
		}
		if len(req.Params) == 0 || json.Unmarshal(req.Params[0], &arg) != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "invalid params"}
		}
		data := arg.Input
		if len(data) == 0 {
			data = arg.Data
		}
		out, err := f.CallContract(ctx, ethereum.CallMsg{From: arg.From, To: arg.To, Data: data}, nil)
		if err != nil {
			var de rpc.DataError
			if errors.As(err, &de) {
				revert, _ := de.ErrorData().(string)
				return nil, &rpcErrorBody{Code: 3, Message: "execution reverted", Data: revert}
			}
			return nil, &rpcErrorBody{Code: -32000, Message: err.Error()}
		}
		return hexutil.Bytes(out), nil
	}
	return nil, &rpcErrorBody{Code: -32601, Message: "the method " + req.Method + " does not exist"}
}
