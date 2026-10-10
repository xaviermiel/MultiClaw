package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// envMap returns a getenv backed by m.
func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// tsChainDump mirrors one entry of testdata/ts-config.json, produced by
// scripts/dump-ts-config.ts from the real TypeScript config.ts.
type tsChainDump struct {
	ChainID             uint64   `json:"chainId"`
	RPCURLs             []string `json:"rpcUrls"`
	SafeValueCron       string   `json:"safeValueCron"`
	SpendingOracleCron  string   `json:"spendingOracleCron"`
	PollIntervalMs      int64    `json:"pollIntervalMs"`
	BlocksToLookBack    int64    `json:"blocksToLookBack"`
	ConfirmationBlocks  int64    `json:"confirmationBlocks"`
	MaxBlocksPerQuery   int64    `json:"maxBlocksPerQuery"`
	MaxHistoricalBlocks int64    `json:"maxHistoricalBlocks"`
	GasLimit            string   `json:"gasLimit"`
	ETHPriceFeedAddress string   `json:"ethPriceFeedAddress"`
	Tokens              []struct {
		Address          string `json:"address"`
		PriceFeedAddress string `json:"priceFeedAddress"`
		Symbol           string `json:"symbol"`
		Type             string `json:"type"`
		UnderlyingAsset  string `json:"underlyingAsset"`
		Token0           string `json:"token0"`
		Token1           string `json:"token1"`
		PriceFeed0       string `json:"priceFeed0"`
		PriceFeed1       string `json:"priceFeed1"`
	} `json:"tokens"`
}

func TestPresetsMatchTypeScriptConfig(t *testing.T) {
	raw, err := os.ReadFile("testdata/ts-config.json")
	if err != nil {
		t.Fatal(err)
	}
	var dump map[string]tsChainDump
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatal(err)
	}
	if len(dump) != len(SupportedChains()) {
		t.Fatalf("TS dump has %d chains, Go has %d", len(dump), len(SupportedChains()))
	}

	for _, name := range SupportedChains() {
		t.Run(name, func(t *testing.T) {
			ts, ok := dump[name]
			if !ok {
				t.Fatalf("chain %s missing from TS dump", name)
			}
			cfg, err := Load(envMap(map[string]string{"CHAIN": name}))
			if err != nil {
				t.Fatal(err)
			}

			if cfg.Chain.ID != ts.ChainID {
				t.Errorf("chain id = %d, TS %d", cfg.Chain.ID, ts.ChainID)
			}
			if strings.Join(cfg.RPCURLs, ",") != strings.Join(ts.RPCURLs, ",") {
				t.Errorf("rpc urls = %v, TS %v", cfg.RPCURLs, ts.RPCURLs)
			}
			if cfg.SafeValueCron != ts.SafeValueCron || cfg.SpendingOracleCron != ts.SpendingOracleCron {
				t.Errorf("crons = %q/%q, TS %q/%q", cfg.SafeValueCron, cfg.SpendingOracleCron, ts.SafeValueCron, ts.SpendingOracleCron)
			}
			if cfg.PollInterval != time.Duration(ts.PollIntervalMs)*time.Millisecond {
				t.Errorf("poll interval = %v, TS %dms", cfg.PollInterval, ts.PollIntervalMs)
			}
			if cfg.BlocksToLookBack != ts.BlocksToLookBack {
				t.Errorf("blocksToLookBack = %d, TS %d", cfg.BlocksToLookBack, ts.BlocksToLookBack)
			}
			if cfg.ConfirmationBlocks != ts.ConfirmationBlocks {
				t.Errorf("confirmationBlocks = %d, TS %d", cfg.ConfirmationBlocks, ts.ConfirmationBlocks)
			}
			if cfg.MaxBlocksPerQuery != ts.MaxBlocksPerQuery {
				t.Errorf("maxBlocksPerQuery = %d, TS %d", cfg.MaxBlocksPerQuery, ts.MaxBlocksPerQuery)
			}
			if cfg.MaxHistoricalBlocks != ts.MaxHistoricalBlocks {
				t.Errorf("maxHistoricalBlocks = %d, TS %d", cfg.MaxHistoricalBlocks, ts.MaxHistoricalBlocks)
			}
			if got := strings.TrimSpace(ts.GasLimit); got != "500000" || cfg.GasLimit != 500000 {
				t.Errorf("gas limit = %d, TS %s", cfg.GasLimit, ts.GasLimit)
			}
			if cfg.Chain.ETHPriceFeed != common.HexToAddress(ts.ETHPriceFeedAddress) {
				t.Errorf("ETH feed = %s, TS %s", cfg.Chain.ETHPriceFeed.Hex(), ts.ETHPriceFeedAddress)
			}

			if len(cfg.Chain.Tokens) != len(ts.Tokens) {
				t.Fatalf("token count = %d, TS %d", len(cfg.Chain.Tokens), len(ts.Tokens))
			}
			for i, want := range ts.Tokens {
				got := cfg.Chain.Tokens[i]
				if got.Address != common.HexToAddress(want.Address) ||
					got.PriceFeed != common.HexToAddress(want.PriceFeedAddress) ||
					got.Symbol != want.Symbol ||
					string(got.Type) != want.Type {
					t.Errorf("token %d = {%s %s %q %s}, TS {%s %s %q %s}", i,
						got.Address.Hex(), got.PriceFeed.Hex(), got.Symbol, got.Type,
						want.Address, want.PriceFeedAddress, want.Symbol, want.Type)
				}
				if want.UnderlyingAsset != "" || want.Token0 != "" || want.Token1 != "" || want.PriceFeed0 != "" || want.PriceFeed1 != "" {
					t.Errorf("token %d: TS preset has morpho/univ2 fields; extend this test", i)
				}
			}
		})
	}
}

func TestDefaultChainIsSepolia(t *testing.T) {
	cfg, err := Load(envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChainName != "sepolia" || cfg.Chain.ID != 11155111 {
		t.Fatalf("default chain = %s (%d)", cfg.ChainName, cfg.Chain.ID)
	}
}

func TestUnknownChain(t *testing.T) {
	_, err := Load(envMap(map[string]string{"CHAIN": "optimism"}))
	if err == nil || !strings.Contains(err.Error(), "Supported: sepolia, base, base-sepolia") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptyValuesCountAsUnset(t *testing.T) {
	// The TS uses `process.env.X || default`, so "" falls back to the default.
	cfg, err := Load(envMap(map[string]string{
		"CHAIN": "", "RPC_URL": "", "POLL_INTERVAL_MS": "", "GAS_LIMIT": "  ", "MODULE_ADDRESS": "",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChainName != "sepolia" || cfg.PrimaryRPCURL() != cfg.Chain.DefaultRPCURL ||
		cfg.PollInterval != 24*time.Second || cfg.GasLimit != 500000 || cfg.ModuleAddress != nil {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestOverrides(t *testing.T) {
	module := "0x16D44268C4e3a567c5ff87cCCa10Fe46f8a656Bb"
	registry := "0xb19a2aaf5472b1e49e35eea14f4c0dd25d89ab96" // lowercase is accepted
	cfg, err := Load(envMap(map[string]string{
		"CHAIN":                                 "base-sepolia",
		"RPC_URL":                               "https://rpc.example/v2/KEY",
		"RPC_FALLBACK_URLS":                     " https://a.example , ,https://b.example ",
		"MODULE_ADDRESS":                        module,
		"REGISTRY_ADDRESS":                      registry,
		"SAFE_VALUE_CRON":                       "*/30 * * * * *",
		"SPENDING_ORACLE_CRON":                  "*/5 * * * *",
		"POLL_INTERVAL_MS":                      "1500",
		"BLOCKS_TO_LOOK_BACK":                   "100",
		"CONFIRMATION_BLOCKS":                   "0",
		"MAX_BLOCKS_PER_QUERY":                  "2000",
		"MAX_HISTORICAL_BLOCKS":                 "9000",
		"GAS_LIMIT":                             "800000",
		"SAFE_VALUE_CHANGE_THRESHOLD_BPS":       "50",
		"SAFE_VALUE_MAX_STALENESS_SECONDS":      "600",
		"ALLOWANCE_INCREASE_THRESHOLD_BPS":      "300",
		"SPENDING_ORACLE_MAX_STALENESS_SECONDS": "900",
		"DRY_RUN":                               "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		ok   bool
	}{
		{"rpc urls", strings.Join(cfg.RPCURLs, ",") == "https://rpc.example/v2/KEY,https://a.example,https://b.example"},
		{"module", cfg.ModuleAddress != nil && *cfg.ModuleAddress == common.HexToAddress(module)},
		{"registry", cfg.RegistryAddress != nil && *cfg.RegistryAddress == common.HexToAddress(registry)},
		{"crons", cfg.SafeValueCron == "*/30 * * * * *" && cfg.SpendingOracleCron == "*/5 * * * *"},
		{"poll", cfg.PollInterval == 1500*time.Millisecond},
		{"lookback", cfg.BlocksToLookBack == 100},
		{"confirmations", cfg.ConfirmationBlocks == 0},
		{"chunk", cfg.MaxBlocksPerQuery == 2000},
		{"historical", cfg.MaxHistoricalBlocks == 9000},
		{"gas", cfg.GasLimit == 800000},
		{"value bps", cfg.SafeValueChangeThresholdBps.Int64() == 50},
		{"value staleness", cfg.SafeValueMaxStalenessSeconds == 600},
		{"allowance bps", cfg.AllowanceIncreaseThresholdBps.Int64() == 300},
		{"allowance staleness", cfg.SpendingOracleMaxStalenessSeconds == 900},
		{"dry run", cfg.DryRun},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s not applied", c.name)
		}
	}
}

func TestHistoricalDefaultIgnoresLookbackOverride(t *testing.T) {
	// TS derives maxHistoricalBlocks from the chain's default lookback, not
	// from BLOCKS_TO_LOOK_BACK.
	cfg, err := Load(envMap(map[string]string{"CHAIN": "base", "BLOCKS_TO_LOOK_BACK": "10"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxHistoricalBlocks != 43200*30 {
		t.Fatalf("maxHistoricalBlocks = %d", cfg.MaxHistoricalBlocks)
	}
}

func TestStrictParsing(t *testing.T) {
	bad := map[string]string{
		"POLL_INTERVAL_MS":                "4000ms", // TS parseInt would accept this
		"BLOCKS_TO_LOOK_BACK":             "-1",
		"MAX_BLOCKS_PER_QUERY":            "0",
		"GAS_LIMIT":                       "abc",
		"SAFE_VALUE_CHANGE_THRESHOLD_BPS": "1e2",
		"DRY_RUN":                         "maybe",
		"SAFE_VALUE_CRON":                 "every minute",
		"MODULE_ADDRESS":                  "0x1234",
		"REGISTRY_ADDRESS":                "0xB19A2AAF5472b1e49e35eEa14f4c0dD25D89Ab96", // bad checksum
		"RPC_URL":                         "ws://localhost:8546",
	}
	_, err := Load(envMap(bad))
	if err == nil {
		t.Fatal("expected errors")
	}
	for key := range bad {
		if !strings.Contains(err.Error(), key) && !(key == "RPC_URL" && strings.Contains(err.Error(), "invalid RPC URL")) {
			t.Errorf("no error reported for %s; got: %v", key, err)
		}
	}
}

// Values that would overflow the block or time arithmetic: a poll interval
// beyond time.Duration's range (a negative interval, a busy loop), 2^62
// confirmations (2x it overflows and slices panic).
func TestUpperBounds(t *testing.T) {
	for key, value := range map[string]string{
		"POLL_INTERVAL_MS":      "9223372036855",
		"CONFIRMATION_BLOCKS":   "4611686018427387904",
		"BLOCKS_TO_LOOK_BACK":   "4611686018427387904",
		"MAX_BLOCKS_PER_QUERY":  "18446744073709551",
		"MAX_HISTORICAL_BLOCKS": "4611686018427387904",
	} {
		_, err := Load(envMap(map[string]string{key: value}))
		if err == nil || !strings.Contains(err.Error(), key+": expected an integer from") {
			t.Errorf("%s=%s: err = %v", key, value, err)
		}
	}
	cfg, err := Load(envMap(map[string]string{"POLL_INTERVAL_MS": "86400000", "CONFIRMATION_BLOCKS": "100000"}))
	if err != nil || cfg.PollInterval != 24*time.Hour || cfg.ConfirmationBlocks != 100_000 {
		t.Fatalf("the maximums: %+v, %v", cfg, err)
	}
}

// A key pasted into an address variable must not end up in the error.
func TestAddressErrorsDontQuoteTheValue(t *testing.T) {
	const key = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	for _, name := range []string{"ORACLE_ADDRESS", "MODULE_ADDRESS", "REGISTRY_ADDRESS"} {
		_, err := Load(envMap(map[string]string{name: key}))
		if err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), key[2:20]) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestCronAcceptsFiveAndSixFields(t *testing.T) {
	for _, spec := range []string{"*/5 * * * *", "*/30 * * * * *", "0 */10 * * * *", "@every 1m"} {
		if _, err := CronParser.Parse(spec); err != nil {
			t.Errorf("Parse(%q): %v", spec, err)
		}
	}
	sched, err := CronParser.Parse("*/30 * * * * *")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC)
	if next := sched.Next(start); next != start.Add(29*time.Second) {
		t.Fatalf("6-field spec should be every 30 seconds, next = %v", next)
	}
	sched, _ = CronParser.Parse("*/5 * * * *")
	if next := sched.Next(start); next != time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC) {
		t.Fatalf("5-field spec should be every 5 minutes at second 0, next = %v", next)
	}
}

func TestWindowDurationSecondsIsIgnoredWithWarning(t *testing.T) {
	cfg, err := Load(envMap(map[string]string{"WINDOW_DURATION_SECONDS": "86400"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "WINDOW_DURATION_SECONDS") {
		t.Fatalf("warnings = %v", cfg.Warnings)
	}
}

// Hardhat/anvil default account #0 (public test key).
const (
	testKey     = "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	testKeyAddr = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
)

func TestValidateServiceMode(t *testing.T) {
	module := "0x16D44268C4e3a567c5ff87cCCa10Fe46f8a656Bb"
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"missing key", map[string]string{"MODULE_ADDRESS": module}, "PRIVATE_KEY"},
		{"missing module and registry", map[string]string{"PRIVATE_KEY": testKey}, "MODULE_ADDRESS or REGISTRY_ADDRESS"},
		{"bad key", map[string]string{"PRIVATE_KEY": "0x1234", "MODULE_ADDRESS": module}, "not a valid secp256k1"},
		{"ok with key", map[string]string{"PRIVATE_KEY": testKey, "MODULE_ADDRESS": module}, ""},
		{"key without 0x", map[string]string{"PRIVATE_KEY": testKey[2:], "MODULE_ADDRESS": module}, ""},
		{"oracle address without dry run", map[string]string{"ORACLE_ADDRESS": testKeyAddr, "MODULE_ADDRESS": module}, "PRIVATE_KEY"},
		{"dry run with oracle address", map[string]string{"DRY_RUN": "true", "ORACLE_ADDRESS": testKeyAddr, "MODULE_ADDRESS": module}, ""},
		{"key and matching address", map[string]string{"PRIVATE_KEY": testKey, "ORACLE_ADDRESS": testKeyAddr, "MODULE_ADDRESS": module}, ""},
		{"key and mismatched address", map[string]string{"PRIVATE_KEY": testKey, "ORACLE_ADDRESS": "0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E", "MODULE_ADDRESS": module}, "does not match"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := Load(envMap(c.env))
			if err != nil {
				t.Fatal(err)
			}
			err = cfg.Validate(ModeService)
			if c.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v, want containing %q", err, c.wantErr)
			}
			if err := cfg.Validate(ModeAnalyze); err != nil {
				t.Fatalf("analyze mode should not require keys: %v", err)
			}
		})
	}
}

func TestOracleIdentity(t *testing.T) {
	cfg, _ := Load(envMap(map[string]string{"PRIVATE_KEY": testKey}))
	if addr, err := cfg.OracleIdentity(); err != nil || addr != common.HexToAddress(testKeyAddr) {
		t.Fatalf("identity from key = %s, %v", addr.Hex(), err)
	}
	cfg, _ = Load(envMap(map[string]string{"ORACLE_ADDRESS": testKeyAddr}))
	if addr, err := cfg.OracleIdentity(); err != nil || addr != common.HexToAddress(testKeyAddr) {
		t.Fatalf("identity from ORACLE_ADDRESS = %s, %v", addr.Hex(), err)
	}
	cfg, _ = Load(envMap(nil))
	if _, err := cfg.OracleIdentity(); err == nil {
		t.Fatal("expected an error without key or address")
	}
}

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"https://base-mainnet.g.alchemy.com/v2/SECRET": "https://base-mainnet.g.alchemy.com/***",
		"https://mainnet.base.org":                     "https://mainnet.base.org",
		"https://mainnet.base.org/":                    "https://mainnet.base.org",
		"https://rpc.example?apikey=SECRET":            "https://rpc.example/***",
		"https://user:pass@rpc.example":                "https://rpc.example/***",
		"http://127.0.0.1:8545":                        "http://127.0.0.1:8545",
		"not a url":                                    "<invalid url>",
	}
	for in, want := range cases {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseAddress(t *testing.T) {
	good := []string{
		"0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E", // checksummed
		"0x763072e0fda74eecab3e60bf5bc5b8a46866be7e", // lowercase
	}
	for _, s := range good {
		if _, err := ParseAddress(s); err != nil {
			t.Errorf("ParseAddress(%q): %v", s, err)
		}
	}
	bad := []string{
		"763072E0FDa74Eecab3e60BF5BC5b8A46866be7E",   // no prefix
		"0x763072E0FDA74EECAB3E60BF5BC5B8A46866BE7E", // all upper, not the checksum (viem rejects)
		"0x763072e0FDa74Eecab3e60BF5BC5b8A46866be7E", // broken checksum
		"0x763072E0FDa74Eecab3e60BF5BC5b8A46866be",   // too short
	}
	for _, s := range bad {
		if _, err := ParseAddress(s); err == nil {
			t.Errorf("ParseAddress(%q) should fail", s)
		}
	}
}

func TestTokenByAddressReturnsFirstMatch(t *testing.T) {
	chain, _ := ChainByName("base-sepolia")
	tok, ok := chain.TokenByAddress(common.HexToAddress("0x036cbd53842c5426634e7929541ec2318f3dcf7e"))
	if !ok || tok.Symbol != "USDC" {
		t.Fatalf("lookup = %+v, %v", tok, ok)
	}
	if _, ok := chain.TokenByAddress(common.Address{}); ok {
		t.Fatal("zero address should not match")
	}
}
