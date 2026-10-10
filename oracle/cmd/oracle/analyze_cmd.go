package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/analyze"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// runAnalyzeTx explains why a transaction failed (port of
// analyze-failed-tx.ts). It only reads CHAIN and the RPC endpoints: other
// variables, even malformed, don't matter.
func runAnalyzeTx(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("analyze-tx", stderr)
	envFile := envFileFlag(fs)
	noTenderly := fs.Bool("no-tenderly", false, "never send the transaction hash to Tenderly's public trace API")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: oracle analyze-tx [flags] <tx-hash>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Explains why a transaction failed: decodes it, replays it for its revert")
		fmt.Fprintln(stderr, "data and reads the module state behind the error. Reads CHAIN, RPC_URL")
		fmt.Fprintln(stderr, "and RPC_FALLBACK_URLS.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) > 1 { // flags after the hash
		if err := fs.Parse(rest[1:]); err != nil {
			return 2
		}
		rest = append(rest[:1], fs.Args()...)
	}
	if len(rest) != 1 {
		fs.Usage()
		return 2
	}
	hash, err := parseTxHash(rest[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if err := loadEnvFile(fs, *envFile); err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}
	cfg, err := config.EndpointFromEnv()
	if err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return analyzeTx(ctx, cfg, hash, analyze.Options{Tenderly: !*noTenderly}, stdout, stderr)
}

// analyzeTx writes the report on transaction hash to stdout, or nothing when
// ctx ends first (an interrupt), which is a failure.
func analyzeTx(ctx context.Context, cfg *config.Config, hash common.Hash, opts analyze.Options, stdout, stderr io.Writer) int {
	failed := "Startup error:"
	client, err := dial(ctx, cfg, logx.Discard)
	if err == nil {
		defer client.Close()
		failed = "Analysis failed:"
		err = analyze.New(client, cfg.Chain, opts).Analyze(ctx, hash, stdout)
	}
	switch {
	case ctx.Err() != nil:
		fmt.Fprintln(stderr, "Interrupted: no report")
		return 1
	case err != nil:
		fmt.Fprintln(stderr, failed, err)
		return 1
	}
	return 0
}

func parseTxHash(s string) (common.Hash, error) {
	b, err := hexutil.Decode(s)
	if err != nil || len(b) != common.HashLength {
		return common.Hash{}, fmt.Errorf("invalid transaction hash %q: want 0x and 64 hex digits", s)
	}
	return common.BytesToHash(b), nil
}
