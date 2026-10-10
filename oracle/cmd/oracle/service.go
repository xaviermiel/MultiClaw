package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// serviceFlags are the flags shared by the service commands.
type serviceFlags struct {
	envFile *string
	once    *bool
	dryRun  *bool
}

func addServiceFlags(fs *flag.FlagSet) serviceFlags {
	return serviceFlags{
		envFile: envFileFlag(fs),
		once:    fs.Bool("once", false, "run one check and exit"),
		dryRun:  fs.Bool("dry-run", false, "simulate and log every transaction instead of signing it (also DRY_RUN=true)"),
	}
}

// loadServiceConfig loads the configuration of a service command and checks
// it is complete; it reports problems on stderr.
func loadServiceConfig(fs *flag.FlagSet, flags serviceFlags, stderr io.Writer) (*config.Config, bool) {
	if err := loadEnvFile(fs, *flags.envFile); err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return nil, false
	}
	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return nil, false
	}
	if *flags.dryRun {
		cfg.DryRun = true
	}
	if err := cfg.Validate(config.ModeService); err != nil {
		fmt.Fprintln(stderr, "Configuration error:", err)
		return nil, false
	}
	return cfg, true
}

// newLogger returns a logger named like the TS service writing to w (stdout
// shares one lock across loggers, so concurrent services never interleave).
func newLogger(name string, w io.Writer) *logx.Logger {
	if w == os.Stdout {
		return logx.New(name)
	}
	return logx.NewWithWriter(name, w, time.Now)
}

// connect dials the RPC endpoints (see dial) and returns the transaction
// sender (see newSender).
func connect(ctx context.Context, cfg *config.Config, logf logx.Logf) (*ethx.FallbackClient, ethx.Sender, error) {
	client, err := dial(ctx, cfg, logf)
	if err != nil {
		return nil, nil, err
	}
	sender, err := newSender(ctx, cfg, client, logf)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return client, sender, nil
}

// dial connects to the RPC endpoints and checks that all of them serve the
// configured chain.
func dial(ctx context.Context, cfg *config.Config, logf logx.Logf) (*ethx.FallbackClient, error) {
	for _, w := range cfg.Warnings {
		logf("Warning: %s", w)
	}
	client, err := ethx.Dial(ctx, cfg.RPCURLs, ethx.Options{Logf: logf})
	if err != nil {
		return nil, err
	}
	if err := client.CheckChainID(ctx, cfg.Chain.ID); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// newSender returns a dry-run sender in dry-run mode, otherwise one signing
// with PRIVATE_KEY.
func newSender(ctx context.Context, cfg *config.Config, client *ethx.FallbackClient, logf logx.Logf) (ethx.Sender, error) {
	if cfg.DryRun {
		oracle, err := cfg.OracleIdentity()
		if err != nil {
			return nil, err
		}
		logf("DRY RUN: transactions are simulated and logged, never signed (acting as %s)", oracle.Hex())
		return ethx.NewDryRunSender(client, oracle, cfg.GasLimit, logf), nil
	}
	key, err := crypto.HexToECDSA(cfg.PrivateKey)
	if err != nil {
		return nil, errors.New("PRIVATE_KEY is not a valid private key")
	}
	return ethx.NewTxSender(ctx, client, key, cfg.Chain.ID, cfg.GasLimit, ethx.SenderOptions{Logf: logf})
}
