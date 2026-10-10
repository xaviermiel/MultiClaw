package main

import (
	"bytes"
	"context"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/analyze"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
)

const testTxHash = "0xa07ffb16ae3415a94903ae57faacca5b533555b269d0afe492410ef92153a54f"

func analyzeEnv(t *testing.T, node *fakeRPC) string {
	t.Helper()
	return writeEnvFile(t, "CHAIN=base-sepolia\nRPC_URL="+node.URL+"\n")
}

func TestAnalyzeTxArguments(t *testing.T) {
	clearOracleEnv(t)
	for _, tc := range []struct {
		args   []string
		stderr string
	}{
		{nil, "Usage: oracle analyze-tx [flags] <tx-hash>"},
		{[]string{"0x1234"}, `invalid transaction hash "0x1234"`},
		{[]string{testTxHash, testTxHash}, "Usage: oracle analyze-tx"},
	} {
		var out, errOut bytes.Buffer
		if code := dispatch(append([]string{"analyze-tx"}, tc.args...), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), tc.stderr) {
			t.Errorf("%v: code=%d stderr=%q", tc.args, code, errOut.String())
		}
	}
}

// failedTransfer is the node's answers for a transferToken by testSub that
// failed in block 16.
func failedTransfer(t *testing.T) map[string]any {
	t.Helper()
	input, err := contracts.ModuleABI.Pack("transferToken", testSafe, testSub, big.NewInt(5))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"eth_getTransactionByHash": map[string]any{
			"from": testSub, "to": testModule, "value": "0x0", "gas": "0x7a120", "input": hexutil.Bytes(input), "blockNumber": "0x10",
		},
		"eth_getTransactionReceipt": map[string]any{"status": "0x0", "gasUsed": "0xb5b4"},
	}
}

// TestAnalyzeTxReport analyzes a transferToken that reverted with
// StaleOracleData, with the flags after the hash.
func TestAnalyzeTxReport(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID)
	m := &contracts.ModuleABI
	stale := m.Errors["StaleOracleData"].ID
	node.canned = failedTransfer(t)
	node.canned["debug_traceTransaction"] = map[string]any{
		"type": "CALL", "to": testModule, "error": "execution reverted", "output": hexutil.Bytes(stale[:4]),
	}
	node.Return(testModule, m, "avatar", testSafe).
		Return(testModule, m, "getSpendingAllowance", big.NewInt(0)).
		Return(testModule, m, "lastOracleUpdate", big.NewInt(0)).
		Return(testModule, m, "maxOracleAge", big.NewInt(3600))

	var out, errOut bytes.Buffer
	code := dispatch([]string{"analyze-tx", "--env-file", analyzeEnv(t, node), testTxHash, "--no-tenderly"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}
	wantOutput(t, out.String(),
		"TRANSACTION ANALYSIS "+testTxHash,
		"Status:    FAILED",
		"Function:     transferToken(address,address,uint256)",
		"debug_traceTransaction:  reverted: StaleOracleData",
		"Last oracle update:  never",
		"Max oracle age:      1h0m0s",
	)
	if !node.called("debug_traceTransaction") || node.called("eth_sendRawTransaction") {
		t.Fatalf("methods %v", node.methods)
	}
	if !slices.ContainsFunc(node.Calls(), func(c contractstest.Call) bool {
		return c.Method == "maxOracleAge" && c.Block != nil && c.Block.Uint64() == 15
	}) {
		t.Fatal("the module state was not read at the block before the transaction")
	}
}

func TestAnalyzeTxFailures(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")

	unknown := newFakeRPC(t, chain.ID)
	unknown.canned = map[string]any{"eth_getTransactionByHash": nil}
	var out, errOut bytes.Buffer
	if code := dispatch([]string{"analyze-tx", "--env-file", analyzeEnv(t, unknown), testTxHash}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "Analysis failed: transaction "+testTxHash+" not found") {
		t.Errorf("unknown transaction: code=%d stderr=%q", code, errOut.String())
	}

	clearOracleEnv(t) // the first run's env file set RPC_URL
	otherChain := newFakeRPC(t, 1)
	errOut.Reset()
	if code := dispatch([]string{"analyze-tx", "--env-file", analyzeEnv(t, otherChain), testTxHash}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "serves chain 1, not the configured chain 84532") {
		t.Errorf("chain mismatch: code=%d stderr=%q", code, errOut.String())
	}
}

// analyze-tx reads only CHAIN and the RPC endpoints: other variables, even
// malformed, don't stop it.
func TestAnalyzeTxIgnoresOtherVariables(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID)
	node.canned = failedTransfer(t)
	node.canned["debug_traceTransaction"] = map[string]any{"type": "CALL", "to": testModule, "error": "execution reverted"}
	env := writeEnvFile(t, "CHAIN=base-sepolia\nRPC_URL="+node.URL+"\nGAS_LIMIT=lots\nSAFE_VALUE_CRON=hourly\nMODULE_ADDRESS=0x1234\nDRY_RUN=maybe\n")
	var out, errOut bytes.Buffer
	if code := dispatch([]string{"analyze-tx", "--env-file", env, testTxHash}, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	wantOutput(t, out.String(), "debug_traceTransaction:  reverted: reverted without data")

	// Its own variables are still checked.
	clearOracleEnv(t)
	errOut.Reset()
	env = writeEnvFile(t, "CHAIN=base-sepolia\nRPC_URL=ftp://node\n")
	if code := dispatch([]string{"analyze-tx", "--env-file", env, testTxHash}, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "Configuration error: invalid RPC URL") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

// A node without the debug API or archive state, and --no-tenderly: the
// report says so, asks no one else, and reads the module state now.
func TestAnalyzeTxWithoutTraceOrHistory(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID) // answers debug_traceTransaction with "method not found"
	node.canned = failedTransfer(t)
	node.noHistory = true
	node.Return(testModule, &contracts.ModuleABI, "avatar", testSafe)

	var out, errOut bytes.Buffer
	if code := dispatch([]string{"analyze-tx", "--no-tenderly", "--env-file", analyzeEnv(t, node), testTxHash}, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	wantOutput(t, out.String(),
		"debug_traceTransaction:  unavailable: the method debug_traceTransaction does not exist",
		"eth_call at block 15:    unavailable: missing trie node",
		"Tenderly:                not asked (--no-tenderly)",
		"No revert data.",
		"MODULE STATE NOW (the block before the transaction could not be read)",
		"Safe:                "+testSafe.Hex(),
	)
}

// An interrupt makes analyze-tx fail without a report: what it read after
// it would be errors.
func TestAnalyzeTxInterrupted(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID)
	node.canned = failedTransfer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node.onRequest = func(method string) {
		if method == "debug_traceTransaction" {
			cancel()
		}
	}
	cfg, err := config.LoadEndpoint(func(key string) string {
		return map[string]string{"CHAIN": "base-sepolia", "RPC_URL": node.URL}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := analyzeTx(ctx, cfg, common.HexToHash(testTxHash), analyze.Options{}, &out, &errOut); code != 1 ||
		out.Len() != 0 || errOut.String() != "Interrupted: no report\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

// The replay at the block before is the transaction's call: same sender,
// gas and value (here to the payable function of older modules).
func TestAnalyzeTxReplaysTheTransaction(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID)
	node.canned = failedTransfer(t)
	input, err := contracts.LegacyModuleABI.Pack("executeOnProtocolWithValue", testSafe, []byte{0xde, 0xad, 0xbe, 0xef})
	if err != nil {
		t.Fatal(err)
	}
	tx := node.canned["eth_getTransactionByHash"].(map[string]any)
	tx["value"], tx["input"] = "0x2a", hexutil.Bytes(input)
	node.canned["debug_traceTransaction"] = map[string]any{"type": "CALL", "to": testModule} // no revert in it
	node.Revert(testModule, &contracts.LegacyModuleABI, "executeOnProtocolWithValue", nil)

	var out, errOut bytes.Buffer
	if code := dispatch([]string{"analyze-tx", "--no-tenderly", "--env-file", analyzeEnv(t, node), testTxHash}, &out, &errOut); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
	i := slices.IndexFunc(node.Calls(), func(c contractstest.Call) bool { return c.Method == "executeOnProtocolWithValue" })
	if i < 0 {
		t.Fatalf("no replay: %v", node.Calls())
	}
	c := node.Calls()[i]
	if c.From != testSub || c.Gas != 0x7a120 || c.Value == nil || c.Value.Int64() != 42 || c.Block == nil || c.Block.Uint64() != 15 {
		t.Fatalf("replayed from %s with gas %d, value %v, at block %v", c.From.Hex(), c.Gas, c.Value, c.Block)
	}
}
