package analyze

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

var update = flag.Bool("update", false, "rewrite the golden reports in testdata/")

const (
	block     = uint64(47_910_262)
	blockTime = uint64(1_760_052_965)
)

var (
	module    = common.HexToAddress("0xbB2470CB800358Fa38a4849C20453563a1e34c00")
	safe      = common.HexToAddress("0x6E7692fFE42ca2A3FA2b08611AA7e79A2AaA8e8C")
	sub       = common.HexToAddress("0x4573A0C9428e56B0d4EEfcb43DCd99299BBD2e6c")
	oracle    = common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")
	recipient = common.HexToAddress("0x1111111111111111111111111111111111111111")
	usdcImpl  = common.HexToAddress("0xd74cc5d436923b8ba2c179b4bCA2841D8A52C5B5")
	usdcFeed  = common.HexToAddress("0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165")
	router    = common.HexToAddress("0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4")
)

// fakeNode is a Backend over a contractstest.Fake, with canned
// transactions, receipts and traces.
type fakeNode struct {
	*contractstest.Fake
	txs, receipts, traces map[common.Hash]any
	traceErr              error
	// noHistory fails calls at past blocks, like a node without archive
	// state.
	noHistory bool
}

func newFakeNode() *fakeNode {
	return &fakeNode{Fake: contractstest.New(), txs: map[common.Hash]any{}, receipts: map[common.Hash]any{}, traces: map[common.Hash]any{}}
}

func (n *fakeNode) CallRPC(_ context.Context, result any, method string, args ...any) error {
	hash := args[0].(common.Hash)
	var v any
	switch method {
	case "eth_getTransactionByHash":
		v = n.txs[hash]
	case "eth_getTransactionReceipt":
		v = n.receipts[hash]
	case "debug_traceTransaction":
		if n.traceErr != nil {
			return n.traceErr
		}
		v = n.traces[hash]
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func (n *fakeNode) CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	if n.noHistory && block != nil {
		return nil, errors.New("missing trie node 6c0c5a2e (path ) state is not available")
	}
	return n.Fake.CallContract(ctx, msg, block)
}

func (n *fakeNode) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (n *fakeNode) BlockInfo(_ context.Context, b uint64) (ethx.BlockInfo, error) {
	return ethx.BlockInfo{Number: b, Timestamp: blockTime - (block-b)*2}, nil
}

// sent records a transaction: mined in block (failed unless ok), or pending
// when gasUsed is 0.
func (n *fakeNode) sent(hash common.Hash, from, to common.Address, value *big.Int, input []byte, ok bool, gasUsed uint64) {
	tx := map[string]any{"from": from, "to": to, "value": (*hexutil.Big)(value), "gas": hexutil.Uint64(500_000), "input": hexutil.Bytes(input), "blockNumber": nil}
	if gasUsed > 0 {
		tx["blockNumber"] = hexutil.Uint64(block)
		status := hexutil.Uint64(0)
		if ok {
			status = 1
		}
		n.receipts[hash] = map[string]any{"status": status, "gasUsed": hexutil.Uint64(gasUsed)}
	}
	n.txs[hash] = tx
}

func e18(v int64) *big.Int { return new(big.Int).Mul(big.NewInt(v), big.NewInt(1e18)) }

func call(t *testing.T, method string, args ...any) []byte {
	t.Helper()
	data, err := contracts.ModuleABI.Pack(method, args...)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func moduleError(t *testing.T, name string, args ...any) []byte {
	return packError(t, &contracts.ModuleABI, name, args...)
}

// installModule answers the module views a subaccount call's report reads.
func installModule(n *fakeNode) {
	m := &contracts.ModuleABI
	n.Return(module, m, "avatar", safe)
	n.Return(module, m, "getSpendingAllowance", e18(20))
	n.Return(module, m, "lastOracleUpdate", new(big.Int).SetUint64(blockTime-600))
	n.Return(module, m, "authorizedOracle", oracle)
	n.Return(usdc, &contracts.ERC20ABI, "decimals", uint8(6))
	n.Return(weth, &contracts.ERC20ABI, "decimals", uint8(18))
}

func TestReports(t *testing.T) {
	chain, err := config.ChainByName("base-sepolia")
	if err != nil {
		t.Fatal(err)
	}
	hash := common.HexToHash("0xa07ffb16ae3415a94903ae57faacca5b533555b269d0afe492410ef92153a54f")
	approve := pack(t, "approve", recipient, big.NewInt(500_000_000))
	reverted := "execution reverted"
	noDebug := errors.New("the method debug_traceTransaction does not exist/is not available")

	cases := []struct {
		name     string
		setup    func(t *testing.T, n *fakeNode)
		tenderly []byte // revert data Tenderly returns (nil: Tenderly not asked)
	}{
		{"transaction_failed_traced", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "transferToken", usdc, recipient, big.NewInt(10_000_000)), false, 95_521)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted, Output: moduleError(t, "TransactionFailed"), Calls: []callFrame{
				{Type: "STATICCALL", To: usdcFeed},
				{Type: "CALL", To: safe, Calls: []callFrame{
					{Type: "CALL", To: usdc, Error: reverted, Output: reasonData(t, "ERC20: transfer amount exceeds balance"), Calls: []callFrame{
						{Type: "DELEGATECALL", To: usdcImpl, Error: reverted, Output: reasonData(t, "ERC20: transfer amount exceeds balance")},
					}},
				}},
			}}
		}, nil},
		{"approval_exceeds_limit_replayed", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", usdc, approve), false, 61_204)
			n.traceErr = noDebug
			n.Revert(module, &contracts.ModuleABI, "executeOnProtocol", moduleError(t, "ApprovalExceedsLimit"))
			n.Return(module, &contracts.ModuleABI, "allowedAddresses", true)
			n.Return(module, &contracts.ModuleABI, "getAcquiredBalance", big.NewInt(100_000_000))
			n.Return(usdc, &contracts.ERC20ABI, "balanceOf", big.NewInt(1_000_000_000))
			n.Return(module, &contracts.ModuleABI, "tokenPriceFeeds", usdcFeed)
			n.Return(usdcFeed, &contracts.ChainlinkABI, "latestRoundData", big.NewInt(1), big.NewInt(99_990_000), big.NewInt(0), big.NewInt(0), big.NewInt(1))
			n.Return(usdcFeed, &contracts.ChainlinkABI, "decimals", uint8(8))
		}, nil},
		{"stale_safe_value_from_tenderly", func(t *testing.T, n *fakeNode) {
			n.sent(hash, oracle, module, new(big.Int), call(t, "batchUpdate", sub, big.NewInt(55), e18(29), []common.Address{}, []*big.Int{}, []*big.Int{}), false, 32_400)
			n.traceErr = noDebug
			n.noHistory = true
			n.Return(module, &contracts.ModuleABI, "getSafeValue", e18(584), new(big.Int).SetUint64(blockTime-90), big.NewInt(12))
			n.Return(module, &contracts.ModuleABI, "maxSafeValueAge", big.NewInt(0))
		}, moduleError(t, "StalePortfolioValue")},
		{"non_payable", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, big.NewInt(1e15), call(t, "executeOnProtocol", usdc, approve), false, 21_644)
		}, nil},
		{"succeeded", func(t *testing.T, n *fakeNode) {
			n.sent(hash, oracle, module, new(big.Int), call(t, "updateSafeValue", e18(584)), true, 41_000)
		}, nil},
		{"pending", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", usdc, approve), false, 0)
		}, nil},
		{"no_revert_data", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", usdc, approve), false, 500_000)
			n.traceErr = noDebug
			n.Return(module, &contracts.ModuleABI, "executeOnProtocol", []byte{}) // succeeds when replayed
		}, nil},
		{"unknown_protocol_call", func(t *testing.T, n *fakeNode) {
			data := append([]byte{0xde, 0xad, 0xbe, 0xef}, make([]byte, 40)...)
			data[4+31] = 7
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", router, data), false, 40_310)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted, Output: moduleError(t, "UnknownSelector", [4]byte{0xde, 0xad, 0xbe, 0xef})}
		}, nil},
		{"uniswap_deadline", func(t *testing.T, n *fakeNode) {
			swap := swapParams("exactInput", new(big.Int).SetUint64(blockTime-30), map[string]any{
				"path": path(usdc, 500, weth), "recipient": safe, "amountIn": big.NewInt(100_000_000), "amountOutMinimum": big.NewInt(25e15)})
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", router, swap), false, 151_920)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted, Output: moduleError(t, "TransactionFailed"), Calls: []callFrame{
				{Type: "CALL", To: safe, Calls: []callFrame{{Type: "CALL", To: router, Error: reverted, Output: reasonData(t, "Transaction too old")}}},
			}}
		}, nil},
		{"out_of_gas_traced", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "transferToken", usdc, recipient, big.NewInt(10_000_000)), false, 500_000)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: "out of gas"}
		}, nil},
		{"out_of_gas_replayed", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "transferToken", usdc, recipient, big.NewInt(10_000_000)), false, 500_000)
			n.traceErr = noDebug
			n.On(module, &contracts.ModuleABI, "transferToken", func([]any) ([]any, error) { return nil, errors.New("out of gas") })
		}, nil},
		{"transaction_failed_replayed", func(t *testing.T, n *fakeNode) {
			// No trace: the Safe's transfer is called again on its own.
			n.sent(hash, sub, module, new(big.Int), call(t, "transferToken", usdc, recipient, big.NewInt(10_000_000)), false, 95_521)
			n.traceErr = noDebug
			n.Revert(module, &contracts.ModuleABI, "transferToken", moduleError(t, "TransactionFailed"))
			n.Revert(usdc, &contracts.ProtocolCallsABI, "transfer", reasonData(t, "ERC20: transfer amount exceeds balance"))
		}, nil},
		{"echoed_calldata", func(t *testing.T, n *fakeNode) {
			input := call(t, "executeOnProtocol", usdc, approve)
			n.sent(hash, sub, module, new(big.Int), input, false, 61_204)
			n.traceErr = noDebug
			n.Revert(module, &contracts.ModuleABI, "executeOnProtocol", input)
		}, nil},
		{"uniswap_exact_output", func(t *testing.T, n *fakeNode) {
			swap := swapParams("exactOutput0", nil, map[string]any{ // SwapRouter02: no deadline
				"path": path(weth, 3000, usdc), "recipient": safe, "amountOut": big.NewInt(1e16), "amountInMaximum": big.NewInt(30_000_000)})
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", router, swap), false, 151_920)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted, Output: moduleError(t, "TransactionFailed"), Calls: []callFrame{
				{Type: "CALL", To: safe, Calls: []callFrame{{Type: "CALL", To: router, Error: reverted, Output: reasonData(t, "Too much requested"), Calls: []callFrame{
					// A failure the router caught: not the cause.
					{Type: "STATICCALL", To: weth, Error: reverted, Output: reasonData(t, "caught")},
					{Type: "STATICCALL", To: usdcFeed},
				}}}},
			}}
		}, nil},
		{"oracle_acquired_budget", func(t *testing.T, n *fakeNode) {
			n.sent(hash, oracle, module, new(big.Int), call(t, "batchUpdate", sub, big.NewInt(55), e18(20),
				[]common.Address{usdc}, []*big.Int{big.NewInt(0)}, []*big.Int{big.NewInt(100_000_000)}), false, 155_988)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted,
				Output: moduleError(t, "ExceedsOracleAcquiredBudget", bnParse("72913521860964000000"), bnParse("7999736080000000000"))}
			// The subaccount's views: the oracle sent it, but the budget is the subaccount's.
			perSub := func(v *big.Int) contractstest.Handler {
				return func(args []any) ([]any, error) {
					if args[0].(common.Address) != sub {
						return []any{big.NewInt(0)}, nil
					}
					return []any{v}, nil
				}
			}
			n.On(module, &contracts.ModuleABI, "acquiredGrantWindowStart", perSub(new(big.Int).SetUint64(blockTime-3600)))
			n.On(module, &contracts.ModuleABI, "cumulativeOracleGrantedUSD", perSub(big.NewInt(0)))
			n.On(module, &contracts.ModuleABI, "windowSafeValue", perSub(bnParse("39998680400000000000")))
			n.Return(module, &contracts.ModuleABI, "maxOracleAcquiredBps", big.NewInt(2000))
		}, nil},
		{"cumulative_spending_limit", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "transferToken", usdc, recipient, big.NewInt(30_000_000)), false, 88_000)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted,
				Output: moduleError(t, "ExceedsCumulativeSpendingLimit", e18(35), bnParse("29200000000000000000"))}
			n.Return(module, &contracts.ModuleABI, "windowStart", new(big.Int).SetUint64(blockTime-7200))
			n.Return(module, &contracts.ModuleABI, "windowSafeValue", e18(584))
			n.Return(module, &contracts.ModuleABI, "cumulativeSpent", e18(5))
			n.Return(module, &contracts.ModuleABI, "getSubAccountLimits", big.NewInt(500), big.NewInt(0), big.NewInt(86400))
		}, nil},
		{"allowance_cap", func(t *testing.T, n *fakeNode) {
			n.sent(hash, oracle, module, new(big.Int), call(t, "batchUpdate", sub, big.NewInt(55), e18(40), []common.Address{}, []*big.Int{}, []*big.Int{}), false, 60_000)
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted, Output: moduleError(t, "ExceedsAllowanceCap", e18(40), e18(25))}
			n.Return(module, &contracts.ModuleABI, "getSafeValue", e18(500), new(big.Int).SetUint64(blockTime-90), big.NewInt(12))
			n.Return(module, &contracts.ModuleABI, "maxSafeValueAge", big.NewInt(3600))
			n.Return(module, &contracts.ModuleABI, "getSubAccountLimits", big.NewInt(0), e18(25), big.NewInt(86400))
		}, nil},
		{"bubbled_revert", func(t *testing.T, n *fakeNode) {
			pool := common.HexToAddress("0x07eA79F68B2B3df564D0A34F8e19D9B1e339814b")
			supply := pack(t, "supply", usdc, big.NewInt(100_000_000), safe, uint16(0))
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", pool, supply), false, 120_000)
			// The token's error, passed on unchanged by the pool.
			allowance := packError(t, &contracts.CommonErrorsABI, "ERC20InsufficientAllowance", pool, big.NewInt(0), big.NewInt(100_000_000))
			n.traces[hash] = callFrame{Type: "CALL", To: module, Error: reverted, Output: moduleError(t, "TransactionFailed"), Calls: []callFrame{
				{Type: "CALL", To: safe, Calls: []callFrame{{Type: "CALL", To: pool, Error: reverted, Output: allowance, Calls: []callFrame{
					{Type: "CALL", To: usdc, Error: reverted, Output: allowance, Calls: []callFrame{
						{Type: "DELEGATECALL", To: usdcImpl, Error: reverted, Output: allowance},
					}},
				}}}},
			}}
		}, nil},
		{"trace_without_revert", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, module, new(big.Int), call(t, "executeOnProtocol", usdc, approve), false, 61_204)
			n.traces[hash] = callFrame{Type: "CALL", To: module}
			n.Revert(module, &contracts.ModuleABI, "executeOnProtocol", moduleError(t, "SpenderNotAllowed"))
			n.Return(module, &contracts.ModuleABI, "allowedAddresses", false)
		}, nil},
		{"direct_token_call", func(t *testing.T, n *fakeNode) {
			n.sent(hash, sub, usdc, new(big.Int), pack(t, "transfer", recipient, big.NewInt(80_000_000)), false, 34_120)
			n.traces[hash] = callFrame{Type: "CALL", To: usdc, Error: reverted,
				Output: packError(t, &contracts.CommonErrorsABI, "ERC20InsufficientBalance", sub, big.NewInt(1_000_000), big.NewInt(80_000_000))}
		}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := newFakeNode()
			installModule(n)
			tc.setup(t, n)
			opts := Options{}
			if tc.tenderly != nil {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/api/v1/public-contract/84532/trace/"+hash.Hex() {
						http.NotFound(w, r)
						return
					}
					fmt.Fprintf(w, `{"call_trace": {"error": "reverted", "error_hex_data": %q}}`, hexutil.Encode(tc.tenderly))
				}))
				defer server.Close()
				opts = Options{Tenderly: true, TenderlyURL: server.URL}
			}
			var out bytes.Buffer
			if err := New(n, chain, opts).Analyze(context.Background(), hash, &out); err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", tc.name+".golden")
			if *update {
				if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (go test ./internal/analyze -update writes it)", err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatalf("report differs from %s (-update rewrites it):\n%s", golden, out.String())
			}
		})
	}
}

func TestReportReadsStateAtTheBlockBefore(t *testing.T) {
	chain, _ := config.ChainByName("base-sepolia")
	hash := common.Hash{1}
	n := newFakeNode()
	installModule(n)
	n.sent(hash, sub, module, new(big.Int), call(t, "transferToken", usdc, recipient, big.NewInt(1)), false, 50_000)
	n.traces[hash] = callFrame{Type: "CALL", To: module, Error: "execution reverted", Output: moduleError(t, "StaleOracleData")}
	n.Return(module, &contracts.ModuleABI, "maxOracleAge", big.NewInt(3600))
	if err := New(n, chain, Options{}).Analyze(context.Background(), hash, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, c := range n.Calls() {
		if c.To == module && slices.Contains([]string{"avatar", "getSpendingAllowance", "lastOracleUpdate", "maxOracleAge"}, c.Method) {
			reads++
			if c.Block == nil || c.Block.Uint64() != block-1 {
				t.Errorf("%s read at block %v, want %d", c.Method, c.Block, block-1)
			}
		}
	}
	if reads != 4 {
		t.Fatalf("%d module reads, want 4", reads)
	}
}

func bnParse(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return v
}

func TestTokenErrorAmountsUseTheTokensDecimals(t *testing.T) {
	chain, _ := config.ChainByName("base-sepolia")
	n := newFakeNode()
	installModule(n)
	a := New(n, chain, Options{})
	data := packError(t, &contracts.CommonErrorsABI, "ERC20InsufficientAllowance", router, big.NewInt(5_000_000), big.NewInt(7_000_000))
	r := decodeRevert(data)
	a.tokenAmounts(context.Background(), r, usdc)
	if r.args[1].value != "5 USDC" || r.args[2].value != "7 USDC" {
		t.Errorf("raised by USDC: %v", r.args)
	}
	// Bubbled up by a contract that is not a token: its decimals mean nothing.
	r = decodeRevert(data)
	a.tokenAmounts(context.Background(), r, router)
	if r.args[1].value != "5000000" || r.args[2].value != "7000000" {
		t.Errorf("raised by the router: %v", r.args)
	}
}

func TestUnknownTransaction(t *testing.T) {
	chain, _ := config.ChainByName("base-sepolia")
	err := New(newFakeNode(), chain, Options{}).Analyze(context.Background(), common.Hash{2}, &bytes.Buffer{})
	if err == nil || err.Error() != "transaction 0x0200000000000000000000000000000000000000000000000000000000000000 not found" {
		t.Fatalf("err = %v", err)
	}
}
