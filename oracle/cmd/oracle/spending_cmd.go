package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/spendingoracle"
)

// runSpending runs the spending oracle (port of spending-oracle.ts): an
// initial refresh, then event polling every POLL_INTERVAL_MS and a refresh
// every SPENDING_ORACLE_CRON tick. With --once it runs one refresh.
func runSpending(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("spending", stderr)
	flags := addServiceFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, ok := loadServiceConfig(fs, flags, stderr)
	if !ok {
		return 1
	}
	sched, err := config.CronParser.Parse(cfg.SpendingOracleCron)
	if err != nil { // validated when loading; kept for safety
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := newLogger("SpendingOracle", stdout)
	client, sender, err := connect(ctx, cfg, log.Printf)
	if err != nil {
		fmt.Fprintln(stderr, "Startup error:", err)
		return 1
	}
	defer client.Close()

	svc := spendingoracle.New(cfg, client, sender, log.Printf)
	if *flags.once {
		svc.Refresh(ctx)
		return 0
	}
	logSpendingStart(log, cfg, sender)
	svc.Run(ctx, sched)
	log.Printf("Stopped")
	return 0
}

// logSpendingStart logs the TS start() banner.
func logSpendingStart(log *logx.Logger, cfg *config.Config, sender ethx.Sender) {
	log.Printf("Starting Spending Oracle")
	log.Printf("Module address: %s", addrOrNone(cfg.ModuleAddress))
	if cfg.RegistryAddress != nil {
		log.Printf("Registry address: %s (multi-module mode)", cfg.RegistryAddress.Hex())
	}
	log.Printf("Updater address: %s", sender.Address().Hex())
	log.Printf("Poll interval: %dms", cfg.PollInterval.Milliseconds())
	log.Printf("Cron schedule: %s", cfg.SpendingOracleCron)
}
