package main

import (
	"bytes"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"

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

// TestAnalyzeTxReport analyzes a transferToken that reverted with
// StaleOracleData, with the flags after the hash.
func TestAnalyzeTxReport(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID)
	m := &contracts.ModuleABI
	input, err := m.Pack("transferToken", testSafe, testSub, big.NewInt(5))
	if err != nil {
		t.Fatal(err)
	}
	stale := m.Errors["StaleOracleData"].ID
	node.canned = map[string]any{
		"eth_getTransactionByHash": map[string]any{
			"from": testSub, "to": testModule, "value": "0x0", "gas": "0x7a120", "input": hexutil.Bytes(input), "blockNumber": "0x10",
		},
		"eth_getTransactionReceipt": map[string]any{"status": "0x0", "gasUsed": "0xb5b4"},
		"debug_traceTransaction": map[string]any{
			"type": "CALL", "to": testModule, "error": "execution reverted", "output": hexutil.Bytes(stale[:4]),
		},
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
