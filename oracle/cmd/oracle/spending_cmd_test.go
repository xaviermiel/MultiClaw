package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
)

func dryRunEnv(t *testing.T, node *fakeRPC) string {
	t.Helper()
	return writeEnvFile(t, strings.Join([]string{
		"CHAIN=base-sepolia",
		"RPC_URL=" + node.URL + "/v2/SECRET_KEY",
		"ORACLE_ADDRESS=" + testOracle.Hex(),
		"MODULE_ADDRESS=" + testModule.Hex(),
	}, "\n"))
}

func wantOutput(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

// TestSpendingOnceDryRun runs `oracle spending --once --dry-run`: the
// subaccount's allowance goes from 0 to $1000, which dry-run mode only
// simulates.
func TestSpendingOnceDryRun(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID)
	installSpendingModule(node)

	var out, errOut bytes.Buffer
	code := dispatch([]string{"spending", "--env-file", dryRunEnv(t, node), "--once", "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}
	sub := strings.ToLower(testSub.Hex())
	wantOutput(t, out.String(),
		"[SpendingOracle ",
		"=== Spending Oracle: Periodic Refresh ===",
		"Processing 1 module(s)",
		"Found 1 active subaccounts",
		"Processing subaccount: "+sub,
		"Allowance: safeValue=10000, mode=BPS, max=1000, spent=0, new=1000",
		"Preparing batch update: subAccount="+sub,
		"  Reason: allowance increased >2%, stale (",
		"DRY RUN: not sending batchUpdate to "+testModule.Hex(),
		"at block 16: simulation ok",
		"Transaction results: 1 confirmed, 0 failed",
		"=== Periodic Refresh Complete ===",
	)
	if node.called("eth_sendRawTransaction") {
		t.Fatal("dry run sent a transaction")
	}
}

// TestRunOnceDryRun runs both oracles once through `oracle run`.
func TestRunOnceDryRun(t *testing.T) {
	clearOracleEnv(t)
	chain, _ := config.ChainByName("base-sepolia")
	node := newFakeRPC(t, chain.ID)
	installSafeValueModule(node, chain)
	installSpendingModule(node)

	var out, errOut bytes.Buffer
	code := dispatch([]string{"run", "--env-file", dryRunEnv(t, node), "--once", "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}
	s := out.String()
	wantOutput(t, s,
		"  MultiClaw Local Oracle",
		"Chain: base-sepolia",
		"Single-module mode: "+testModule.Hex(),
		"[SafeValue ",
		"=== Safe Value Monitor: Complete ===",
		"[SpendingOracle ",
		"=== Periodic Refresh Complete ===",
	)
	if strings.Contains(s, "SECRET_KEY") {
		t.Fatalf("the RPC URL's key leaked:\n%s", s)
	}
	// Each dry-run simulation logs under its own service.
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "DRY RUN: not sending updateSafeValue") && !strings.HasPrefix(line, "[SafeValue ") ||
			strings.Contains(line, "DRY RUN: not sending batchUpdate") && !strings.HasPrefix(line, "[SpendingOracle ") {
			t.Errorf("simulation logged under the wrong service: %q", line)
		}
	}
}

func TestRunReportsConfigurationProblems(t *testing.T) {
	clearOracleEnv(t)
	envFile := writeEnvFile(t, "CHAIN=base-sepolia\n")
	var out, errOut bytes.Buffer
	if code := dispatch([]string{"run", "--env-file", envFile}, &out, &errOut); code != 1 {
		t.Fatalf("code=%d", code)
	}
	wantOutput(t, errOut.String(), "Configuration error:", "Please copy .env.example to .env and configure:")
}
