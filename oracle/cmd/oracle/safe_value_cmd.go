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
	"github.com/xaviermiel/MultiClaw/oracle/internal/safevalue"
	"github.com/xaviermiel/MultiClaw/oracle/internal/schedule"
)

// runSafeValue runs the Safe value oracle (port of safe-value.ts): every
// SAFE_VALUE_CRON tick, and once right away.
func runSafeValue(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("safe-value", stderr)
	flags := addServiceFlags(fs)
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	cfg, ok := loadServiceConfig(fs, flags, stderr)
	if !ok {
		return 1
	}
	sched, err := config.CronParser.Parse(cfg.SafeValueCron)
	if err != nil { // validated when loading; kept for safety
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := newLogger("SafeValue", stdout)
	client, sender, err := connect(ctx, cfg, log.Printf)
	if err != nil {
		fmt.Fprintln(stderr, "Startup error:", err)
		return 1
	}
	defer client.Close()

	svc := safevalue.NewService(cfg, client, sender, log.Printf)
	if *flags.once {
		svc.RunOnce(ctx)
		return 0
	}
	logSafeValueStart(log, cfg, sender)
	schedule.Loop(ctx, sched, svc.RunOnce)
	log.Printf("Stopped")
	return 0
}

// logSafeValueStart logs the TS startCron banner.
func logSafeValueStart(log *logx.Logger, cfg *config.Config, sender ethx.Sender) {
	log.Printf("Starting Safe Value Oracle with cron: %s", cfg.SafeValueCron)
	log.Printf("Module address: %s", addrOrNone(cfg.ModuleAddress))
	log.Printf("Updater address: %s", sender.Address().Hex())
}
