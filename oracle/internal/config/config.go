// Package config loads the oracle configuration from environment variables.
//
// Variable names and defaults match the TypeScript oracle (oracle/src/config.ts
// plus the thresholds read in safe-value.ts and spending-oracle.ts). As in the
// TS, an empty value counts as unset. Unlike the TS, malformed values are
// rejected at startup instead of becoming NaN.
package config

import (
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
)

// Defaults shared by every chain.
const (
	DefaultChain                 = "sepolia"
	DefaultSafeValueCron         = "0 */10 * * * *"
	DefaultSpendingOracleCron    = "0 */2 * * * *"
	DefaultMaxBlocksPerQuery     = 5000
	DefaultGasLimit              = 500_000
	DefaultValueChangeBps        = 100
	DefaultAllowanceIncreaseBps  = 200
	DefaultMaxStalenessSeconds   = 2700
	historicalLookbackMultiplier = 30 // days of history scanned for acquired tokens
	secondsPerDay                = 86400
	pollIntervalBlockMultiplier  = 2
)

// CronParser accepts both 5-field and 6-field (leading seconds) expressions,
// like node-cron. Existing deployments use both forms.
var CronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// Config is the resolved oracle configuration.
type Config struct {
	ChainName string
	Chain     Chain

	// RPCURLs holds RPC_URL (or the chain default) followed by RPC_FALLBACK_URLS.
	RPCURLs []string

	// PrivateKey is the oracle key as hex without 0x ("" when unset).
	PrivateKey string
	// OracleAddress (ORACLE_ADDRESS) identifies the oracle in dry-run mode
	// without a key. When both are set they must agree.
	OracleAddress   *common.Address
	ModuleAddress   *common.Address
	RegistryAddress *common.Address

	SafeValueCron      string
	SpendingOracleCron string

	PollInterval        time.Duration
	BlocksToLookBack    int64
	ConfirmationBlocks  int64
	MaxBlocksPerQuery   int64
	MaxHistoricalBlocks int64
	GasLimit            uint64

	SafeValueChangeThresholdBps       *big.Int
	SafeValueMaxStalenessSeconds      int64
	AllowanceIncreaseThresholdBps     *big.Int
	SpendingOracleMaxStalenessSeconds int64

	DryRun bool

	// Warnings lists non-fatal notes produced while loading (e.g. variables
	// that are accepted but ignored).
	Warnings []string
}

// Mode selects which requirements Validate enforces.
type Mode int

const (
	// ModeInspect has no requirements (used by `oracle config`).
	ModeInspect Mode = iota
	// ModeService is for the long-running oracles (run, safe-value, spending).
	ModeService
	// ModeAnalyze only needs an RPC endpoint (analyze-tx).
	ModeAnalyze
)

// LoadEnvFile loads KEY=VALUE pairs from path into the process environment
// without overriding variables that are already set (dotenv semantics).
// A missing file is ignored unless required is true.
func LoadEnvFile(path string, required bool) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return nil
		}
		return fmt.Errorf("env file %s: %w", path, err)
	}
	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("env file %s: %w", path, err)
	}
	return nil
}

// FromEnv resolves the configuration from the process environment.
func FromEnv() (*Config, error) { return Load(os.Getenv) }

// Load resolves the configuration using getenv.
func Load(getenv func(string) string) (*Config, error) {
	env := func(key string) string { return strings.TrimSpace(getenv(key)) }
	var errs []error
	fail := func(err error) { errs = append(errs, err) }

	c := &Config{}
	c.ChainName = env("CHAIN")
	if c.ChainName == "" {
		c.ChainName = DefaultChain
	}
	chain, err := ChainByName(c.ChainName)
	if err != nil {
		return nil, err
	}
	c.Chain = chain

	// RPC endpoints: primary first, then comma-separated fallbacks.
	primary := env("RPC_URL")
	if primary == "" {
		primary = chain.DefaultRPCURL
	}
	c.RPCURLs = []string{primary}
	for _, u := range strings.Split(env("RPC_FALLBACK_URLS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			c.RPCURLs = append(c.RPCURLs, u)
		}
	}
	for _, u := range c.RPCURLs {
		if err := validateRPCURL(u); err != nil {
			fail(err)
		}
	}

	c.PrivateKey = strings.TrimPrefix(strings.TrimPrefix(env("PRIVATE_KEY"), "0x"), "0X")
	c.OracleAddress = parseOptionalAddress(env, "ORACLE_ADDRESS", fail)
	c.ModuleAddress = parseOptionalAddress(env, "MODULE_ADDRESS", fail)
	c.RegistryAddress = parseOptionalAddress(env, "REGISTRY_ADDRESS", fail)

	c.SafeValueCron = orDefault(env("SAFE_VALUE_CRON"), DefaultSafeValueCron)
	c.SpendingOracleCron = orDefault(env("SPENDING_ORACLE_CRON"), DefaultSpendingOracleCron)
	for _, cr := range []struct{ name, spec string }{
		{"SAFE_VALUE_CRON", c.SafeValueCron},
		{"SPENDING_ORACLE_CRON", c.SpendingOracleCron},
	} {
		if _, err := CronParser.Parse(cr.spec); err != nil {
			fail(fmt.Errorf("%s: invalid cron expression %q: %v", cr.name, cr.spec, err))
		}
	}

	// Block-derived defaults: 24h of blocks to look back, 30 days for the
	// historical acquired-token scan (derived from the default lookback, not
	// from BLOCKS_TO_LOOK_BACK, as in the TS), polling at 2x block time.
	defaultLookBack := secondsPerDay / chain.BlockTimeSeconds
	pollMs := parseInt(env, "POLL_INTERVAL_MS", chain.BlockTimeSeconds*pollIntervalBlockMultiplier*1000, 1, fail)
	c.PollInterval = time.Duration(pollMs) * time.Millisecond
	c.BlocksToLookBack = parseInt(env, "BLOCKS_TO_LOOK_BACK", defaultLookBack, 0, fail)
	c.ConfirmationBlocks = parseInt(env, "CONFIRMATION_BLOCKS", chain.ConfirmationBlocks, 0, fail)
	c.MaxBlocksPerQuery = parseInt(env, "MAX_BLOCKS_PER_QUERY", DefaultMaxBlocksPerQuery, 1, fail)
	c.MaxHistoricalBlocks = parseInt(env, "MAX_HISTORICAL_BLOCKS", defaultLookBack*historicalLookbackMultiplier, 0, fail)
	c.GasLimit = uint64(parseInt(env, "GAS_LIMIT", DefaultGasLimit, 1, fail))

	c.SafeValueChangeThresholdBps = big.NewInt(parseInt(env, "SAFE_VALUE_CHANGE_THRESHOLD_BPS", DefaultValueChangeBps, 0, fail))
	c.SafeValueMaxStalenessSeconds = parseInt(env, "SAFE_VALUE_MAX_STALENESS_SECONDS", DefaultMaxStalenessSeconds, 0, fail)
	c.AllowanceIncreaseThresholdBps = big.NewInt(parseInt(env, "ALLOWANCE_INCREASE_THRESHOLD_BPS", DefaultAllowanceIncreaseBps, 0, fail))
	c.SpendingOracleMaxStalenessSeconds = parseInt(env, "SPENDING_ORACLE_MAX_STALENESS_SECONDS", DefaultMaxStalenessSeconds, 0, fail)

	if v := env("DRY_RUN"); v != "" {
		dry, err := strconv.ParseBool(v)
		if err != nil {
			fail(fmt.Errorf("DRY_RUN: invalid boolean %q", v))
		}
		c.DryRun = dry
	}

	if env("WINDOW_DURATION_SECONDS") != "" {
		c.Warnings = append(c.Warnings,
			"WINDOW_DURATION_SECONDS is ignored: each subaccount's window is read on-chain via getSubAccountLimits")
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// Validate checks the requirements for mode.
func (c *Config) Validate(mode Mode) error {
	switch mode {
	case ModeInspect, ModeAnalyze:
		return nil
	case ModeService:
	default:
		return fmt.Errorf("unknown config mode %d", mode)
	}

	var errs []error
	if c.PrivateKey == "" {
		if !c.DryRun || c.OracleAddress == nil {
			errs = append(errs, errors.New("PRIVATE_KEY environment variable is required (or DRY_RUN=true with ORACLE_ADDRESS)"))
		}
	} else {
		addr, err := c.KeyAddress()
		switch {
		case err != nil:
			errs = append(errs, err)
		case c.OracleAddress != nil && *c.OracleAddress != addr:
			errs = append(errs, fmt.Errorf("ORACLE_ADDRESS %s does not match the address of PRIVATE_KEY (%s)", c.OracleAddress.Hex(), addr.Hex()))
		}
	}
	if c.ModuleAddress == nil && c.RegistryAddress == nil {
		errs = append(errs, errors.New("either MODULE_ADDRESS or REGISTRY_ADDRESS environment variable is required"))
	}
	return errors.Join(errs...)
}

// KeyAddress returns the address of PrivateKey.
func (c *Config) KeyAddress() (common.Address, error) {
	key, err := crypto.HexToECDSA(c.PrivateKey)
	if err != nil {
		return common.Address{}, errors.New("PRIVATE_KEY is not a valid secp256k1 private key")
	}
	return crypto.PubkeyToAddress(key.PublicKey), nil
}

// OracleIdentity returns the address the oracle acts as: the key's address,
// or ORACLE_ADDRESS when running without a key.
func (c *Config) OracleIdentity() (common.Address, error) {
	if c.PrivateKey != "" {
		return c.KeyAddress()
	}
	if c.OracleAddress != nil {
		return *c.OracleAddress, nil
	}
	return common.Address{}, errors.New("no PRIVATE_KEY or ORACLE_ADDRESS configured")
}

// PrimaryRPCURL returns the first RPC endpoint.
func (c *Config) PrimaryRPCURL() string { return c.RPCURLs[0] }

// RedactURL keeps only the scheme and host of an RPC URL, since provider URLs
// usually embed API keys in the path or query.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<invalid url>"
	}
	redacted := u.Scheme + "://" + u.Host
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		redacted += "/***"
	}
	return redacted
}

func validateRPCURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid RPC URL %q: must be an http(s) URL", RedactURL(raw))
	}
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseInt(env func(string) string, key string, def, min int64, fail func(error)) int64 {
	raw := env(key)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < min {
		fail(fmt.Errorf("%s: expected an integer >= %d, got %q", key, min, raw))
		return def
	}
	return v
}

func parseOptionalAddress(env func(string) string, key string, fail func(error)) *common.Address {
	raw := env(key)
	if raw == "" {
		return nil
	}
	addr, err := ParseAddress(raw)
	if err != nil {
		fail(fmt.Errorf("%s: %w", key, err))
		return nil
	}
	return &addr
}

// ParseAddress parses a 0x-prefixed hex address with viem's strict rules:
// all-lowercase is accepted, anything else must carry a valid EIP-55 checksum.
func ParseAddress(s string) (common.Address, error) {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") || !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("invalid address %q", s)
	}
	if s[2:] != strings.ToLower(s[2:]) {
		if mixed, err := common.NewMixedcaseAddressFromString(s); err != nil || !mixed.ValidChecksum() {
			return common.Address{}, fmt.Errorf("invalid EIP-55 checksum in %q", s)
		}
	}
	return common.HexToAddress(s), nil
}
