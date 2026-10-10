package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
)

// TestSafeValueOnceDryRun runs `oracle safe-value --once --dry-run` against
// a fake node: the module's Safe holds 1 ETH at $2000 and no value is stored
// yet, so the oracle writes $2000, which dry-run mode only simulates.
func TestSafeValueOnceDryRun(t *testing.T) {
	clearOracleEnv(t)
	chain, err := config.ChainByName("base-sepolia")
	if err != nil {
		t.Fatal(err)
	}
	node := newFakeRPC(t, chain.ID)
	installSafeValueModule(node, chain)

	envFile := writeEnvFile(t, strings.Join([]string{
		"CHAIN=base-sepolia",
		"RPC_URL=" + node.URL,
		"ORACLE_ADDRESS=" + testOracle.Hex(),
		"MODULE_ADDRESS=" + testModule.Hex(),
	}, "\n"))
	var out, errOut bytes.Buffer
	code := dispatch([]string{"safe-value", "--env-file", envFile, "--once", "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}
	s := out.String()
	for _, want := range []string{
		"[SafeValue ",
		"DRY RUN: transactions are simulated and logged, never signed (acting as " + testOracle.Hex() + ")",
		"Processing 1 module(s)",
		"Monitoring Safe: " + testSafe.Hex(),
		"  Value: $2000 USD",
		"  Balance: 0, skipping",
		"Updating due to staleness",
		"DRY RUN: not sending updateSafeValue to " + testModule.Hex() + " (36 bytes of calldata); at block 16: simulation ok",
		"Transaction confirmed in block 16",
		"=== Safe Value Monitor: Complete ===",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Error") {
		t.Errorf("unexpected error in the output:\n%s", s)
	}
	if node.called("eth_sendRawTransaction") {
		t.Fatal("dry run sent a transaction")
	}
}

func TestSafeValueStartupErrors(t *testing.T) {
	clearOracleEnv(t)
	node := newFakeRPC(t, 8453) // Base mainnet, while CHAIN says Base Sepolia
	envFile := writeEnvFile(t, strings.Join([]string{
		"CHAIN=base-sepolia",
		"RPC_URL=" + node.URL,
		"ORACLE_ADDRESS=" + testOracle.Hex(),
		"MODULE_ADDRESS=" + testModule.Hex(),
	}, "\n"))

	var out, errOut bytes.Buffer
	code := dispatch([]string{"safe-value", "--env-file", envFile, "--once", "--dry-run"}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "Startup error: RPC endpoint 0 serves chain 8453, not the configured chain 84532") {
		t.Fatalf("wrong chain: code=%d stderr=%q", code, errOut.String())
	}

	// Without --dry-run, ORACLE_ADDRESS alone is not enough: a key is needed.
	errOut.Reset()
	code = dispatch([]string{"safe-value", "--env-file", envFile, "--once"}, &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "Configuration error:") {
		t.Fatalf("no key: code=%d stderr=%q", code, errOut.String())
	}
}
