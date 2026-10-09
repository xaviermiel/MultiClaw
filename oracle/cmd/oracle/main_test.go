package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearOracleEnv unsets every variable the oracle reads for the duration of
// the test (t.Setenv restores the previous values afterwards).
func clearOracleEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CHAIN", "RPC_URL", "RPC_FALLBACK_URLS", "PRIVATE_KEY", "ORACLE_ADDRESS",
		"MODULE_ADDRESS", "REGISTRY_ADDRESS", "SAFE_VALUE_CRON", "SPENDING_ORACLE_CRON",
		"POLL_INTERVAL_MS", "BLOCKS_TO_LOOK_BACK", "WINDOW_DURATION_SECONDS",
		"CONFIRMATION_BLOCKS", "MAX_BLOCKS_PER_QUERY", "MAX_HISTORICAL_BLOCKS", "GAS_LIMIT",
		"SAFE_VALUE_CHANGE_THRESHOLD_BPS", "SAFE_VALUE_MAX_STALENESS_SECONDS",
		"ALLOWANCE_INCREASE_THRESHOLD_BPS", "SPENDING_ORACLE_MAX_STALENESS_SECONDS", "DRY_RUN",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "oracle.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDispatchUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := dispatch(nil, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "Usage: oracle") {
		t.Fatalf("no-arg dispatch: code=%d stderr=%q", code, errOut.String())
	}
	errOut.Reset()
	if code := dispatch([]string{"bogus"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), `unknown command "bogus"`) {
		t.Fatalf("unknown command: code=%d stderr=%q", code, errOut.String())
	}
}

func TestConfigCommandValidConfigRedactsSecrets(t *testing.T) {
	clearOracleEnv(t)
	envFile := writeEnvFile(t, strings.Join([]string{
		"CHAIN=base-sepolia",
		"RPC_URL=https://base-sepolia.g.alchemy.com/v2/SECRET_API_KEY",
		"PRIVATE_KEY=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		"REGISTRY_ADDRESS=0xB19a2AAf5472b1e49e35eEa14f4c0dD25D89Ab96",
		"WINDOW_DURATION_SECONDS=86400",
	}, "\n"))

	var out, errOut bytes.Buffer
	code := dispatch([]string{"config", "--env-file", envFile}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}
	s := out.String()
	for _, want := range []string{
		"base-sepolia (id 84532)",
		"https://base-sepolia.g.alchemy.com/***",
		"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 (from PRIVATE_KEY)",
		"0xB19a2AAf5472b1e49e35eEa14f4c0dD25D89Ab96",
		"Tokens (14)",
		"Warning: WINDOW_DURATION_SECONDS is ignored",
		"Configuration is valid",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
	for _, secret := range []string{"SECRET_API_KEY", "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"} {
		if strings.Contains(s, secret) || strings.Contains(errOut.String(), secret) {
			t.Errorf("output leaks secret %q", secret)
		}
	}
}

func TestConfigCommandReportsInvalidServiceConfig(t *testing.T) {
	clearOracleEnv(t)
	envFile := writeEnvFile(t, "CHAIN=base\n")

	var out, errOut bytes.Buffer
	if code := dispatch([]string{"config", "--env-file", envFile}, &out, &errOut); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(errOut.String(), "PRIVATE_KEY") || !strings.Contains(errOut.String(), "MODULE_ADDRESS or REGISTRY_ADDRESS") {
		t.Fatalf("stderr=%q", errOut.String())
	}
}

func TestConfigCommandEnvFileSemantics(t *testing.T) {
	clearOracleEnv(t)

	// An explicitly requested env file must exist.
	var out, errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "nope.env")
	if code := dispatch([]string{"config", "--env-file", missing}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "nope.env") {
		t.Fatalf("missing explicit env file: code=%d stderr=%q", code, errOut.String())
	}

	// Variables already set in the environment win over the file (dotenv semantics).
	t.Setenv("CHAIN", "base")
	envFile := writeEnvFile(t, "CHAIN=sepolia\n")
	out.Reset()
	errOut.Reset()
	dispatch([]string{"config", "--env-file", envFile}, &out, &errOut)
	if !strings.Contains(out.String(), "base (id 8453)") {
		t.Fatalf("process env should win over the env file:\n%s", out.String())
	}
}
