package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/safevalue"
	"github.com/xaviermiel/MultiClaw/oracle/internal/schedule"
	"github.com/xaviermiel/MultiClaw/oracle/internal/spendingoracle"
)

// runAll runs both oracles in one process (port of index.ts). They share the
// RPC client and the transaction sender, so their transactions get
// consecutive nonces. With --once each runs one check.
func runAll(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("run", stderr)
	flags := addServiceFlags(fs)
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	fmt.Fprintln(stdout, "===========================================")
	fmt.Fprintln(stdout, "  MultiClaw Local Oracle")
	fmt.Fprintln(stdout, "===========================================")
	fmt.Fprintln(stdout)

	cfg, ok := loadServiceConfig(fs, flags, stderr)
	if !ok {
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Please copy .env.example to .env and configure:")
		fmt.Fprintln(stderr, "  - PRIVATE_KEY: Private key of the authorized updater")
		fmt.Fprintln(stderr, "    (with --dry-run, ORACLE_ADDRESS can name the oracle instead)")
		fmt.Fprintln(stderr, "  - REGISTRY_ADDRESS: ModuleRegistry address for shared multi-module mode")
		fmt.Fprintln(stderr, "    or MODULE_ADDRESS: DeFiInteractorModule contract address for single-module mode")
		fmt.Fprintln(stderr, "  - RPC_URL: Ethereum RPC URL")
		return 1
	}
	safeSched, err := config.CronParser.Parse(cfg.SafeValueCron)
	if err != nil { // validated when loading; kept for safety
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}
	spendSched, err := config.CronParser.Parse(cfg.SpendingOracleCron)
	if err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}

	fmt.Fprintf(stdout, "Chain: %s\n", cfg.ChainName)
	// The TS printed the URL as is, API key included.
	fmt.Fprintf(stdout, "RPC URL: %s\n", config.RedactURL(cfg.PrimaryRPCURL()))
	if cfg.RegistryAddress != nil {
		fmt.Fprintf(stdout, "Registry mode: %s\n", cfg.RegistryAddress.Hex())
	} else {
		fmt.Fprintf(stdout, "Single-module mode: %s\n", addrOrNone(cfg.ModuleAddress))
	}
	fmt.Fprintln(stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	safeLog, spendLog := newLogger("SafeValue", stdout), newLogger("SpendingOracle", stdout)
	// The TS RPC client logged as the spending oracle.
	client, err := dial(ctx, cfg, spendLog.Printf)
	if err != nil {
		fmt.Fprintln(stderr, "Startup error:", err)
		return 1
	}
	defer client.Close()
	safeSender, spendSender, err := sharedSenders(ctx, cfg, client, safeLog.Printf, spendLog.Printf)
	if err != nil {
		fmt.Fprintln(stderr, "Startup error:", err)
		return 1
	}
	safe := safevalue.NewService(cfg, client, safeSender, safeLog.Printf)
	spend := spendingoracle.New(cfg, client, spendSender, spendLog.Printf)

	if *flags.once {
		safe.RunOnce(ctx)
		spend.Refresh(ctx)
		return 0
	}

	fmt.Fprintln(stdout, "Starting Safe Value Oracle...")
	logSafeValueStart(safeLog, cfg, safeSender)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Starting Spending Oracle...")
	logSpendingStart(spendLog, cfg, spendSender)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Both oracles are now running.")
	fmt.Fprintln(stdout, "Press Ctrl+C to stop.")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		schedule.Loop(ctx, safeSched, safe.RunOnce)
	}()
	go func() {
		defer wg.Done()
		spend.Run(ctx, spendSched)
	}()
	wg.Wait()
	spendLog.Printf("Stopped")
	return 0
}

// sharedSenders returns the services' senders: one TxSender for both (one
// nonce sequence), or in dry-run mode one dry-run sender each, so their
// simulations log under their own service.
func sharedSenders(ctx context.Context, cfg *config.Config, client *ethx.FallbackClient,
	safeLogf, spendLogf func(string, ...any)) (safe, spend ethx.Sender, err error) {
	if cfg.DryRun {
		if safe, err = newSender(ctx, cfg, client, safeLogf); err != nil {
			return nil, nil, err
		}
		spend, err = newSender(ctx, cfg, client, spendLogf)
		return safe, spend, err
	}
	sender, err := newSender(ctx, cfg, client, spendLogf)
	return sender, sender, err
}
