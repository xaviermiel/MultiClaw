package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
)

// loadEnvFileFn is swapped in tests.
var loadEnvFileFn = config.LoadEnvFile

// runConfig prints the resolved configuration (secrets redacted) and exits
// non-zero if it is not valid for running the oracle services.
func runConfig(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config", stderr)
	envFile := envFileFlag(fs)
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if err := loadEnvFile(fs, *envFile); err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}
	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}

	printConfig(stdout, cfg)

	for _, w := range cfg.Warnings {
		fmt.Fprintln(stdout, "Warning:", w)
	}
	if err := cfg.Validate(config.ModeService); err != nil {
		fmt.Fprintln(stderr, "Not valid for running the oracle:", err)
		return 1
	}
	fmt.Fprintln(stdout, "Configuration is valid for running the oracle.")
	return 0
}

func printConfig(w io.Writer, cfg *config.Config) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(k string, v any) { fmt.Fprintf(tw, "%s\t%v\n", k, v) }

	rpcs := make([]string, len(cfg.RPCURLs))
	for i, u := range cfg.RPCURLs {
		rpcs[i] = config.RedactURL(u)
	}

	row("Chain", fmt.Sprintf("%s (id %d)", cfg.ChainName, cfg.Chain.ID))
	row("RPC URLs", joinOrNone(rpcs))
	row("Oracle identity", identity(cfg))
	row("Module address", addrOrNone(cfg.ModuleAddress))
	row("Registry address", addrOrNone(cfg.RegistryAddress))
	row("Safe value cron", cfg.SafeValueCron)
	row("Spending oracle cron", cfg.SpendingOracleCron)
	row("Poll interval", cfg.PollInterval)
	row("Blocks to look back", cfg.BlocksToLookBack)
	row("Confirmation blocks", cfg.ConfirmationBlocks)
	row("Max blocks per query", cfg.MaxBlocksPerQuery)
	row("Max historical blocks", cfg.MaxHistoricalBlocks)
	row("Gas limit", cfg.GasLimit)
	row("Safe value change threshold", fmt.Sprintf("%s bps", cfg.SafeValueChangeThresholdBps))
	row("Safe value max staleness", fmt.Sprintf("%ds", cfg.SafeValueMaxStalenessSeconds))
	row("Allowance increase threshold", fmt.Sprintf("%s bps", cfg.AllowanceIncreaseThresholdBps))
	row("Spending oracle max staleness", fmt.Sprintf("%ds", cfg.SpendingOracleMaxStalenessSeconds))
	row("Dry run", cfg.DryRun)
	row("ETH price feed", cfg.Chain.ETHPriceFeed.Hex())

	symbols := make([]string, len(cfg.Chain.Tokens))
	for i, t := range cfg.Chain.Tokens {
		symbols[i] = t.Symbol
	}
	row(fmt.Sprintf("Tokens (%d)", len(symbols)), joinOrNone(symbols))
	_ = tw.Flush()
}

func identity(cfg *config.Config) string {
	switch {
	case cfg.PrivateKey != "":
		addr, err := cfg.KeyAddress()
		if err != nil {
			return "invalid PRIVATE_KEY"
		}
		return addr.Hex() + " (from PRIVATE_KEY)"
	case cfg.OracleAddress != nil:
		return cfg.OracleAddress.Hex() + " (from ORACLE_ADDRESS, no key)"
	default:
		return "(none)"
	}
}

func addrOrNone(a *common.Address) string {
	if a == nil {
		return "(none)"
	}
	return a.Hex()
}
