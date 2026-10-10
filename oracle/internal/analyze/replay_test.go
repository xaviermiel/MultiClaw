package analyze

import (
	"bytes"
	"context"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

// The replays are the transaction's calls at the block before: the module
// call from the sender with its gas and value, and the Safe's call from the
// Safe with the value the module passes.
func TestReplaysAreTheTransactionsCalls(t *testing.T) {
	chain, _ := config.ChainByName("base-sepolia")
	hash := common.Hash{3}
	n := newFakeNode()
	installModule(n)
	swap := swapParams("exactInputSingle", new(big.Int).SetUint64(blockTime+600), map[string]any{
		"tokenIn": weth, "tokenOut": usdc, "fee": big.NewInt(500), "recipient": safe,
		"amountIn": big.NewInt(1e16), "amountOutMinimum": big.NewInt(1), "sqrtPriceLimitX96": new(big.Int)})
	n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocolWithValue", router, swap, big.NewInt(1e16)), false, 90_000)
	n.traceErr = errNoDebug
	n.Revert(module, &contracts.ModuleABI, "executeOnProtocolWithValue", moduleRevert(t, "TransactionFailed"))
	n.Revert(router, &contracts.ProtocolCallsABI, "exactInputSingle", reasonData(t, "Too little received"))
	var out bytes.Buffer
	if err := New(n, chain, Options{}).Analyze(context.Background(), hash, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `Underlying error:  "Too little received", from the call to`) {
		t.Fatalf("report:\n%s", out.String())
	}
	var replays int
	for _, c := range n.Calls() {
		switch c.Method {
		case "executeOnProtocolWithValue":
			replays++
			if c.From != sub || c.Gas != 500_000 || c.Value == nil || c.Value.Sign() != 0 || c.Block == nil || c.Block.Uint64() != block-1 {
				t.Errorf("module call replayed from %s with gas %d, value %v, at block %v", c.From.Hex(), c.Gas, c.Value, c.Block)
			}
		case "exactInputSingle":
			replays++
			if c.From != safe || c.Value == nil || c.Value.Cmp(big.NewInt(1e16)) != 0 || c.Block == nil || c.Block.Uint64() != block-1 {
				t.Errorf("Safe's call replayed from %s with value %v, at block %v", c.From.Hex(), c.Value, c.Block)
			}
		}
	}
	if replays != 2 {
		t.Fatalf("%d replays: %v", replays, n.Calls())
	}
}

var errNoDebug = &rpcError{-32601, "the method debug_traceTransaction does not exist/is not available"}

// rpcError is a JSON-RPC error, as the node client returns it.
type rpcError struct {
	code int
	msg  string
}

func (e *rpcError) Error() string  { return e.msg }
func (e *rpcError) ErrorCode() int { return e.code }

func TestTenderly(t *testing.T) {
	hash := common.Hash{4}
	base, _ := config.ChainByName("base") // Tenderly is asked for CHAIN's id
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string // the error, or the top frame's error
	}{
		{"trace", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/public-contract/8453/trace/"+hash.Hex() {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(tenderlyBody(tenderlyFrame("CALL", module, "reverted", reasonData(t, "nope")))))
		}, "reverted"},
		{"not found", http.NotFound, "HTTP 404"},
		{"not JSON", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) }, "decode trace: invalid character '<'"},
		{"no trace", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"error": {"message": "not found"}}`)) }, "the answer has no trace"},
		{"timeout", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, "Client.Timeout exceeded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			a := New(newFakeNode(), base, Options{Tenderly: true, TenderlyURL: server.URL, HTTP: &http.Client{Timeout: 100 * time.Millisecond}})
			root, err := a.tenderly(context.Background(), hash)
			got := ""
			switch {
			case err != nil:
				got = err.Error()
			case root.Error != "":
				got = root.Error
				if r := frameRevert(root); r.title() != `"nope"` {
					t.Errorf("revert %s", r.title())
				}
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Traces are read leniently, and only calls are kept: Tenderly's other
// frames (jumps, storage reads, reverts) are skipped, their calls moved to
// the enclosing call.
func TestTraceFrames(t *testing.T) {
	raw := rawFrame{CallType: "call", To: module.Hex(), Output: "0x1234", Error: "x", Calls: []rawFrame{
		{CallType: "JUMP", Calls: []rawFrame{{CallType: "STATICCALL", To: usdcFeed.Hex()}, {CallType: "SLOAD", To: ""}}},
		{CallType: "REVERT", Error: "x"},
		{Type: "DELEGATECALL", To: "not an address", Output: "0xzz"},
	}}
	f := raw.frame()
	if f.Type != "CALL" || f.To != module || !bytes.Equal(f.Output, []byte{0x12, 0x34}) || len(f.Calls) != 2 ||
		f.Calls[0].To != usdcFeed || f.Calls[1].Type != "DELEGATECALL" || f.Calls[1].Output != nil {
		t.Fatalf("%+v", f)
	}
	// Tenderly's top frame has its revert data in error_hex_data too.
	if f := (&rawFrame{Error: "x", ErrorHexData: "0xabcd"}).frame(); !bytes.Equal(f.Output, []byte{0xab, 0xcd}) {
		t.Fatalf("output %x", f.Output)
	}
}
