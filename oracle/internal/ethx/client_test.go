package ethx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

// ============ Fake JSON-RPC node ============

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// reply is what a fake node answers: an HTTP status (non-200 short-circuits),
// a JSON-RPC error, or a result. hang blocks until the request is canceled.
type reply struct {
	status int
	err    *rpcError
	result any
	hang   bool
}

type fakeNode struct {
	mu       sync.Mutex
	methods  []string
	params   [][]json.RawMessage
	handle   func(call int, method string, params []json.RawMessage) reply
	server   *httptest.Server
	requests int
}

func newFakeNode(t *testing.T, handle func(call int, method string, params []json.RawMessage) reply) *fakeNode {
	t.Helper()
	n := &fakeNode{handle: handle}
	n.server = httptest.NewServer(n)
	t.Cleanup(n.server.Close)
	return n
}

func (n *fakeNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	n.mu.Lock()
	call := n.requests
	n.requests++
	n.methods = append(n.methods, req.Method)
	n.params = append(n.params, req.Params)
	n.mu.Unlock()

	rep := n.handle(call, req.Method, req.Params)
	if rep.hang {
		<-r.Context().Done()
		return
	}
	if rep.status != 0 && rep.status != http.StatusOK {
		http.Error(w, http.StatusText(rep.status), rep.status)
		return
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rep.err != nil {
		resp["error"] = rep.err
	} else {
		resp["result"] = rep.result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (n *fakeNode) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.requests
}

func always(r reply) func(int, string, []json.RawMessage) reply {
	return func(int, string, []json.RawMessage) reply { return r }
}

// logRecorder collects Logf output.
type logRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (l *logRecorder) Logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logRecorder) contains(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

func dialTest(t *testing.T, logs *logRecorder, opts Options, nodes ...*fakeNode) *FallbackClient {
	t.Helper()
	urls := make([]string, len(nodes))
	for i, n := range nodes {
		urls[i] = n.server.URL
	}
	if logs != nil {
		opts.Logf = logs.Logf
	}
	if opts.RetryDelay == 0 {
		opts.RetryDelay = time.Millisecond
	}
	c, err := Dial(context.Background(), urls, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// ============ Failover and health ============

func TestFailoverRotatesPerAttemptAndTracksHealth(t *testing.T) {
	down := newFakeNode(t, always(reply{status: http.StatusServiceUnavailable}))
	up := newFakeNode(t, always(reply{result: "0x10"}))
	logs := &logRecorder{}
	c := dialTest(t, logs, Options{}, down, up)

	for i := 1; i <= 3; i++ {
		n, err := c.BlockNumber(context.Background())
		if err != nil || n != 16 {
			t.Fatalf("call %d: %d, %v", i, n, err)
		}
		// Each call tries the preferred endpoint once, then fails over
		// immediately (the TS retried the same endpoint instead).
		if down.count() != i || up.count() != i {
			t.Fatalf("call %d: down=%d up=%d requests", i, down.count(), up.count())
		}
	}
	if c.Preferred() != 1 || !logs.contains("RPC endpoint 0 exceeded failure threshold, rotated to endpoint 1") {
		t.Fatalf("expected rotation after 3 failures; preferred=%d logs=%v", c.Preferred(), logs.lines)
	}
	if !logs.contains("getBlockNumber failed on RPC endpoint 0") {
		t.Fatalf("missing failure log: %v", logs.lines)
	}

	// The healthy endpoint is now tried first.
	if _, err := c.BlockNumber(context.Background()); err != nil {
		t.Fatal(err)
	}
	if down.count() != 3 || up.count() != 4 {
		t.Fatalf("after rotation: down=%d up=%d", down.count(), up.count())
	}
}

func TestSingleEndpointRetriesWithBackoff(t *testing.T) {
	flaky := newFakeNode(t, func(call int, _ string, _ []json.RawMessage) reply {
		if call < 2 {
			return reply{status: http.StatusTooManyRequests}
		}
		return reply{result: "0x2a"}
	})
	c := dialTest(t, nil, Options{}, flaky)
	n, err := c.BlockNumber(context.Background())
	if err != nil || n != 42 || flaky.count() != 3 {
		t.Fatalf("got %d, %v after %d requests", n, err, flaky.count())
	}
}

func TestAllEndpointsDownResetsHealth(t *testing.T) {
	a := newFakeNode(t, always(reply{status: http.StatusBadGateway}))
	b := newFakeNode(t, always(reply{err: &rpcError{Code: -32005, Message: "limit exceeded", Data: map[string]any{"see": "https://example"}}}))
	logs := &logRecorder{}
	c := dialTest(t, logs, Options{}, a, b)

	_, err := c.BlockNumber(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	// 1 + 3 retries, alternating endpoints. Rate-limit errors carrying a JSON
	// object as data are retryable (not mistaken for revert data).
	if a.count()+b.count() != 4 || a.count() != 2 {
		t.Fatalf("requests a=%d b=%d", a.count(), b.count())
	}
	for i := 0; i < 3; i++ {
		_, _ = c.BlockNumber(context.Background())
	}
	if !logs.contains("All RPC endpoints unhealthy, resetting failure counts") {
		t.Fatalf("expected a health reset: %v", logs.lines)
	}
}

func TestDeterministicErrorsAreNotRetriedOrCounted(t *testing.T) {
	capErr := contracts.ModuleABI.Errors["ExceedsAllowanceCap"]
	args, _ := capErr.Inputs.Pack(big.NewInt(5), big.NewInt(4))
	revertData := append(append([]byte{}, capErr.ID[:4]...), args...)

	cases := []struct {
		name string
		rep  reply
		call func(*FallbackClient) error
		want func(error) bool
	}{
		{
			name: "revert with data",
			rep:  reply{err: &rpcError{Code: 3, Message: "execution reverted", Data: hexutil.Encode(revertData)}},
			call: func(c *FallbackClient) error {
				to := common.HexToAddress("0x01")
				_, err := c.CallContract(context.Background(), ethereum.CallMsg{To: &to, Data: []byte{1, 2, 3, 4}}, nil)
				return err
			},
			want: func(err error) bool {
				// End to end: geth's RPC error exposes the revert data.
				return contracts.DescribeRevert(err) == "ExceedsAllowanceCap(5, 4)"
			},
		},
		{
			name: "bare revert",
			rep:  reply{err: &rpcError{Code: -32000, Message: "execution reverted"}},
			call: func(c *FallbackClient) error {
				to := common.HexToAddress("0x01")
				_, err := c.CallContract(context.Background(), ethereum.CallMsg{To: &to}, nil)
				return err
			},
			want: IsExecutionError,
		},
		{
			name: "receipt not found",
			rep:  reply{result: nil},
			call: func(c *FallbackClient) error {
				_, err := c.TransactionReceipt(context.Background(), common.Hash{1})
				return err
			},
			want: func(err error) bool { return errors.Is(err, ethereum.NotFound) },
		},
		{
			name: "tx pool rejection",
			rep:  reply{err: &rpcError{Code: -32000, Message: "nonce too low: next nonce 5, tx nonce 4"}},
			call: func(c *FallbackClient) error {
				return c.SendTransaction(context.Background(), signedTestTx(t))
			},
			want: IsTxPoolRejection,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			primary := newFakeNode(t, always(tc.rep))
			backup := newFakeNode(t, always(reply{status: http.StatusInternalServerError}))
			c := dialTest(t, nil, Options{MaxFailures: 1}, primary, backup)

			err := tc.call(c)
			if err == nil || !tc.want(err) {
				t.Fatalf("unexpected error: %v", err)
			}
			if primary.count() != 1 || backup.count() != 0 {
				t.Fatalf("deterministic error must not be retried: primary=%d backup=%d", primary.count(), backup.count())
			}
			if c.Preferred() != 0 {
				t.Fatal("deterministic error must not count against the endpoint")
			}
		})
	}
}

func TestCallerCancellationIsNotCounted(t *testing.T) {
	hang := newFakeNode(t, always(reply{hang: true}))
	other := newFakeNode(t, always(reply{result: "0x1"}))
	c := dialTest(t, nil, Options{MaxFailures: 1}, hang, other)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.BlockNumber(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if other.count() != 0 || c.Preferred() != 0 {
		t.Fatalf("a caller timeout must not fail over or mark the endpoint unhealthy")
	}
}

func TestAttemptTimeoutFailsOver(t *testing.T) {
	hang := newFakeNode(t, always(reply{hang: true}))
	up := newFakeNode(t, always(reply{result: "0x7"}))
	c := dialTest(t, nil, Options{AttemptTimeout: 50 * time.Millisecond}, hang, up)

	n, err := c.BlockNumber(context.Background())
	if err != nil || n != 7 {
		t.Fatalf("got %d, %v", n, err)
	}
}

// ============ Block info ============

func TestBlockInfoUsesRPCHashAndIgnoresExtraFields(t *testing.T) {
	// An OP-stack style block with fields geth's header type may not know.
	// The hash deliberately doesn't match the header fields: we must use the
	// node's hash verbatim, never recompute it.
	hash := common.HexToHash("0xabc0000000000000000000000000000000000000000000000000000000000def")
	block := map[string]any{
		"number":                "0x1b4",
		"hash":                  hash.Hex(),
		"parentHash":            "0x" + strings.Repeat("11", 32),
		"timestamp":             "0x6620a0b2",
		"baseFeePerGas":         "0xfa",
		"withdrawalsRoot":       "0x" + strings.Repeat("22", 32),
		"blobGasUsed":           "0x0",
		"excessBlobGas":         "0x0",
		"parentBeaconBlockRoot": "0x" + strings.Repeat("33", 32),
		"requestsHash":          "0x" + strings.Repeat("44", 32),
		"someFutureField":       map[string]any{"x": 1},
		"transactions":          []string{"0x" + strings.Repeat("55", 32)},
	}
	var gotParams [][]json.RawMessage
	var mu sync.Mutex
	node := newFakeNode(t, func(_ int, method string, params []json.RawMessage) reply {
		mu.Lock()
		gotParams = append(gotParams, params)
		mu.Unlock()
		if method != "eth_getBlockByNumber" {
			return reply{err: &rpcError{Code: -32601, Message: "method not found"}}
		}
		return reply{result: block}
	})
	c := dialTest(t, nil, Options{}, node)

	info, err := c.BlockInfo(context.Background(), 436)
	if err != nil {
		t.Fatal(err)
	}
	if info.Number != 436 || info.Hash != hash || info.Timestamp != 0x6620a0b2 || info.BaseFee.Int64() != 250 {
		t.Fatalf("info = %+v", info)
	}
	if string(gotParams[0][0]) != `"0x1b4"` || string(gotParams[0][1]) != "false" {
		t.Fatalf("params = %s, %s", gotParams[0][0], gotParams[0][1])
	}

	if _, err := c.LatestBlockInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if string(gotParams[1][0]) != `"latest"` {
		t.Fatalf("latest params = %s", gotParams[1][0])
	}
}

func TestMissingBlockFailsOverToAnotherEndpoint(t *testing.T) {
	// A lagging endpoint may not have the block yet.
	lagging := newFakeNode(t, always(reply{result: nil}))
	synced := newFakeNode(t, always(reply{result: map[string]any{
		"number": "0x5", "hash": common.Hash{9}.Hex(), "timestamp": "0x10",
	}}))
	c := dialTest(t, nil, Options{}, lagging, synced)
	info, err := c.BlockInfo(context.Background(), 5)
	if err != nil || info.Hash != (common.Hash{9}) || info.BaseFee != nil {
		t.Fatalf("info = %+v, %v", info, err)
	}
}

// ============ Errors classification ============

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("dial tcp: connection refused"), true},
		{errors.New("429 Too Many Requests"), true},
		{context.DeadlineExceeded, true}, // per-attempt timeout
		{context.Canceled, false},
		{ethereum.NotFound, false},
		{errors.New("execution reverted"), false},
		{errors.New("replacement transaction underpriced"), false},
		{errors.New("already known"), false},
		{errors.New("insufficient funds for gas * price + value"), false},
		{fmt.Errorf("wrapped: %w", ethereum.NotFound), false},
	}
	for _, c := range cases {
		if got := IsRetryable(c.err); got != c.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestDialRequiresURLs(t *testing.T) {
	if _, err := Dial(context.Background(), nil, Options{}); err == nil {
		t.Fatal("expected an error")
	}
}

// signedTestTx returns a signed dynamic-fee transaction (anvil key #0).
func signedTestTx(t *testing.T) *types.Transaction {
	t.Helper()
	key, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress("0x01")
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(84532), Nonce: 4, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2), Gas: 21000, To: &to})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(84532)), key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// ============ Chain ID and transaction lookup ============

func chainIDNode(t *testing.T, id string) *fakeNode {
	return newFakeNode(t, func(_ int, method string, _ []json.RawMessage) reply {
		if method != "eth_chainId" {
			return reply{err: &rpcError{Code: -32601, Message: "method not found"}}
		}
		return reply{result: id}
	})
}

func TestCheckChainIDAsksEveryEndpoint(t *testing.T) {
	ctx := context.Background()
	baseSepolia, base := chainIDNode(t, "0x14a34"), chainIDNode(t, "0x2105")
	down := newFakeNode(t, always(reply{status: http.StatusBadGateway}))

	logs := &logRecorder{}
	if err := dialTest(t, logs, Options{}, down, baseSepolia).CheckChainID(ctx, 84532); err != nil {
		t.Fatal(err)
	}
	if !logs.contains("Could not check the chain ID of RPC endpoint 0") {
		t.Fatalf("logs %q", logs.lines)
	}

	// A fallback endpoint on another chain is caught too, not just the
	// preferred one.
	err := dialTest(t, nil, Options{}, baseSepolia, base).CheckChainID(ctx, 84532)
	if err == nil || !strings.Contains(err.Error(), "RPC endpoint 1 serves chain 8453") {
		t.Fatalf("err = %v", err)
	}

	if err := dialTest(t, nil, Options{}, down).CheckChainID(ctx, 84532); err == nil {
		t.Fatal("no endpoint answered, want an error")
	}
}

func TestTransactionByHashNotFoundIsNotRetried(t *testing.T) {
	node := newFakeNode(t, always(reply{result: nil}))
	_, _, err := dialTest(t, nil, Options{}, node).TransactionByHash(context.Background(), common.Hash{1})
	if !errors.Is(err, ethereum.NotFound) || node.count() != 1 {
		t.Fatalf("err = %v after %d requests", err, node.count())
	}
}
