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
// analyze-failed-tx.ts). It only needs CHAIN and an RPC endpoint.
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
	cfg, err := config.FromEnv()
	if err == nil {
		err = cfg.Validate(config.ModeAnalyze)
	}
	if err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := dial(ctx, cfg, logx.Discard)
	if err != nil {
		fmt.Fprintln(stderr, "Startup error:", err)
		return 1
	}
	defer client.Close()

	a := analyze.New(client, cfg.Chain, analyze.Options{Tenderly: !*noTenderly})
	if err := a.Analyze(ctx, hash, stdout); err != nil {
		fmt.Fprintln(stderr, "Analysis failed:", err)
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
