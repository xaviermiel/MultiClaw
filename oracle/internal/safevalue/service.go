package safevalue

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/discovery"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// Service checks every module's Safe value and writes it on-chain when it
// changed by more than the threshold or the on-chain value went stale.
type Service struct {
	chain    ChainReader
	sender   ethx.Sender
	valuer   *Valuer
	registry *common.Address
	module   *common.Address
	// thresholdBps is SAFE_VALUE_CHANGE_THRESHOLD_BPS; maxStaleness is
	// SAFE_VALUE_MAX_STALENESS_SECONDS.
	thresholdBps *big.Int
	maxStaleness int64
	logf         logx.Logf

	// Now is the wall clock (replaced in tests).
	Now func() time.Time
}

// NewService returns the safe-value oracle for cfg, reading through chain
// and writing through sender.
func NewService(cfg *config.Config, chain ChainReader, sender ethx.Sender, logf logx.Logf) *Service {
	if logf == nil {
		logf = logx.Discard
	}
	return &Service{
		chain:        chain,
		sender:       sender,
		valuer:       NewValuer(chain, cfg.Chain, logf),
		registry:     cfg.RegistryAddress,
		module:       cfg.ModuleAddress,
		thresholdBps: cfg.SafeValueChangeThresholdBps,
		maxStaleness: cfg.SafeValueMaxStalenessSeconds,
		logf:         logf,
		Now:          time.Now,
	}
}

// RunOnce checks every module this oracle is authorized for (one cron run).
func (s *Service) RunOnce(ctx context.Context) {
	s.logf("=== Safe Value Monitor: Starting check ===")
	modules, err := discovery.ActiveModules(ctx, s.chain, s.registry, s.module, s.sender.Address(), s.logf)
	if err != nil {
		s.logf("Error in safe value update: %v", err)
		return
	}
	s.logf("Processing %d module(s)", len(modules))
	for _, module := range modules {
		if ctx.Err() != nil {
			return
		}
		if !discovery.Authorized(ctx, s.chain, module, s.sender.Address(), s.logf) {
			continue
		}
		if err := s.processModule(ctx, module); err != nil {
			s.logf("Error processing module %s: %v", module.Hex(), err)
		}
	}
	s.logf("=== Safe Value Monitor: Complete ===")
}

// processModule values one module's Safe and writes the value if needed. A
// panic is returned as an error, so one module can't stop the others (the
// TS caught every exception per module).
func (s *Service) processModule(ctx context.Context, module common.Address) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	s.logf("--- Processing module: %s ---", module.Hex())

	total, err := s.valuer.SafeValue(ctx, module)
	if err != nil {
		return err
	}
	onChain, lastUpdated := s.onChainValue(ctx, module)
	// Interrupted (shutdown): the reads that failed counted as 0.
	if err := ctx.Err(); err != nil {
		return err
	}
	sinceUpdate := s.Now().Unix() - lastUpdated

	s.logf("Total USD Value: $%s (on-chain: $%s)", bn.FormatUnits(total, 18), bn.FormatUnits(onChain, 18))
	s.logf("Last updated: %ds ago (max staleness: %ds)", sinceUpdate, s.maxStaleness)

	if total.Sign() == 0 {
		s.logf("Skipping write - total value is 0")
		return nil
	}
	diff, exceedsThreshold, stale := s.compare(total, onChain, sinceUpdate)
	if !exceedsThreshold && !stale {
		divisor := onChain
		if divisor.Sign() == 0 {
			divisor = big.NewInt(1)
		}
		s.logf("Skipping update - value change within threshold:")
		s.logf("  Diff: $%s (%sbps, threshold: %sbps)", bn.FormatUnits(diff, 18), bn.MulDiv(diff, bn.BPS, divisor), s.thresholdBps)
		s.logf("  Time since update: %ds (max: %ds)", sinceUpdate, s.maxStaleness)
		return nil
	}
	if stale {
		s.logf("Updating due to staleness (%ds > %ds)", sinceUpdate, s.maxStaleness)
	} else {
		s.logf("Updating due to value change >%s%% (diff: $%s)", bn.Quo(s.thresholdBps, big.NewInt(100)), bn.FormatUnits(diff, 18))
	}
	return s.write(ctx, module, total)
}

// compare returns how far total is from the on-chain value, whether that is
// more than thresholdBps of the on-chain value, and whether the on-chain
// value is older than maxStaleness. Both comparisons are strict.
func (s *Service) compare(total, onChain *big.Int, sinceUpdate int64) (diff *big.Int, exceedsThreshold, stale bool) {
	diff = new(big.Int).Abs(bn.Sub(total, onChain))
	threshold := bn.MulDiv(onChain, s.thresholdBps, bn.BPS)
	return diff, diff.Cmp(threshold) > 0, sinceUpdate > s.maxStaleness
}

// onChainValue reads the module's stored value and its update time, or
// (0, 0) when they can't be read: the write then happens as if stale.
func (s *Service) onChainValue(ctx context.Context, module common.Address) (*big.Int, int64) {
	v, err := contracts.NewModule(module, s.chain).GetSafeValue(ctx)
	if err != nil {
		s.logf("Error reading on-chain safe value: %v", err)
		return bn.Zero(), 0
	}
	lastUpdated := int64(math.MaxInt64)
	if v.LastUpdated.IsInt64() {
		lastUpdated = v.LastUpdated.Int64()
	}
	return v.TotalValueUSD, lastUpdated
}

// write sends updateSafeValue and waits for it. Unlike the TS, which logged
// any mined transaction as confirmed, a revert is reported as a failure.
func (s *Service) write(ctx context.Context, module common.Address, total *big.Int) error {
	s.logf("Writing Safe value to chain: %s ($%s USD) for module %s", total, bn.FormatUnits(total, 18), module.Hex())
	data, err := contracts.PackUpdateSafeValue(total)
	if err != nil {
		return err
	}
	sent, err := s.sender.Send(ctx, module, data)
	if err != nil {
		s.logf("Error writing safe value: %v", err)
		return err
	}
	s.logf("Transaction submitted: %s", sent.Hash.Hex())
	receipt, err := s.sender.Wait(ctx, sent)
	if err != nil {
		s.logf("Error writing safe value: %v", err)
		return err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		err := fmt.Errorf("transaction %s reverted on-chain in block %s (gasUsed=%d, reason=%s)",
			sent.Hash.Hex(), receipt.BlockNumber, receipt.GasUsed, ethx.RevertReason(ctx, s.chain, sent, receipt))
		s.logf("Error writing safe value: %v", err)
		return err
	}
	s.logf("Transaction confirmed in block %s", receipt.BlockNumber)
	return nil
}
