// Package spendingoracle is the Go port of the TS spending oracle service
// (oracle/src/spending-oracle.ts): it watches the modules' events, rebuilds
// each affected subaccount's spending and acquired balances with package
// spending, and pushes the result on-chain with batchUpdate.
package spendingoracle

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/robfig/cron/v3"
	"golang.org/x/sync/errgroup"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/discovery"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/pricing"
	"github.com/xaviermiel/MultiClaw/oracle/internal/schedule"
	"github.com/xaviermiel/MultiClaw/oracle/internal/spending"
)

// Chain is what the spending oracle reads from the chain.
type Chain interface {
	contracts.Caller
	ethx.LogFilterer
	BlockNumber(ctx context.Context) (uint64, error)
	BlockInfo(ctx context.Context, n uint64) (ethx.BlockInfo, error)
}

// maxParallelReads bounds concurrent independent reads (block infos, token
// balances and versions).
const maxParallelReads = 8

// maxWindowDuration caps the on-chain window duration (a uint256 with no
// upper bound) so timestamp arithmetic can't overflow.
const maxWindowDuration = int64(1) << 62

type updateKey struct{ module, subAccount common.Address }

// Service is the spending oracle. Poll and Refresh share one lock, as the
// TS's poll and cron shared isProcessing: whichever comes second skips.
type Service struct {
	chain  Chain
	sender ethx.Sender
	prices *pricing.Reader
	cfg    *config.Config
	logf   logx.Logf

	// Now is the wall clock (replaced in tests).
	Now func() time.Time

	mu sync.Mutex // held by a running Poll or Refresh

	// Poll state, guarded by mu.
	started            bool
	lastProcessedBlock uint64
	// lastUpdate is when each module/subaccount's last confirmed batchUpdate
	// was prepared. It lives in memory, so the first refresh after a restart
	// always pushes an update, as in the TS.
	lastUpdate map[updateKey]int64
	// history keeps each module/subaccount's AcquiredBalanceUpdated events
	// (historicalTokens).
	history map[updateKey]*tokenHistory

	blocks *blockHashes // recorded by event queries; safe for concurrent use
}

// New returns the spending oracle for cfg.
func New(cfg *config.Config, chain Chain, sender ethx.Sender, logf logx.Logf) *Service {
	if logf == nil {
		logf = logx.Discard
	}
	return &Service{
		chain:      chain,
		sender:     sender,
		prices:     pricing.NewReader(chain, cfg.Chain, logf),
		cfg:        cfg,
		logf:       logf,
		Now:        time.Now,
		lastUpdate: map[updateKey]int64{},
		history:    map[updateKey]*tokenHistory{},
		blocks:     newBlockHashes(),
	}
}

// Run refreshes every subaccount once, then polls for new events every
// POLL_INTERVAL_MS and refreshes again on refreshSchedule, until ctx ends.
func (s *Service) Run(ctx context.Context, refreshSchedule cron.Schedule) {
	// The TS started polling and refreshing at the same time, but its first
	// poll found the initial refresh running and skipped.
	s.Refresh(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		schedule.Every(ctx, refreshSchedule, s.Refresh)
	}()
	go func() {
		defer wg.Done()
		schedule.Every(ctx, schedule.Interval(s.cfg.PollInterval), s.Poll)
	}()
	wg.Wait()
}

// Poll processes the subaccounts with events in the blocks confirmed since
// the last poll (pollForNewEvents). It is silently skipped while a refresh
// runs.
func (s *Service) Poll(ctx context.Context) {
	if !s.mu.TryLock() {
		return
	}
	defer s.mu.Unlock()

	var pending []pendingTx
	current, err := s.poll(ctx, &pending)
	// Unlike the TS, transactions already sent are awaited even when the
	// poll failed, so their confirmation is still recorded.
	s.waitPending(ctx, pending)
	if err != nil {
		s.logf("Error polling for events: %v", err)
		return
	}
	if current > s.lastProcessedBlock {
		s.lastProcessedBlock = current
	}
}

// poll returns the block it processed up to (lastProcessedBlock when there
// was nothing new).
func (s *Service) poll(ctx context.Context, pending *[]pendingTx) (uint64, error) {
	latest, err := s.chain.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	confirmations := uint64(s.cfg.ConfirmationBlocks)
	current := ethx.SubClamped(latest, confirmations)
	if !s.started {
		// First run: start from recent blocks.
		s.lastProcessedBlock = ethx.SubClamped(current, uint64(s.cfg.BlocksToLookBack))
		s.started = true
	}
	if rewindTo, ok := s.checkReorg(ctx); ok {
		// The acquired-token histories may hold events of replaced blocks.
		clear(s.history)
		if rewindTo < s.lastProcessedBlock {
			s.logf("Reorg detected! Rewinding from block %d to %d", s.lastProcessedBlock, rewindTo)
			s.lastProcessedBlock = rewindTo
		}
	}
	if current <= s.lastProcessedBlock {
		return s.lastProcessedBlock, nil // no new blocks
	}
	from := s.lastProcessedBlock + 1
	s.logf("Polling blocks %d to %d (%d blocks, %d confirmation depth)",
		from, current, current-s.lastProcessedBlock, confirmations)

	modules, err := discovery.ActiveModules(ctx, s.chain, s.cfg.RegistryAddress, s.cfg.ModuleAddress, s.logf)
	if err != nil {
		return 0, err
	}
	for _, module := range modules {
		s.logf("--- Processing module: %s ---", module.Hex())
		if !discovery.Authorized(ctx, s.chain, module, s.sender.Address(), s.logf) {
			continue
		}
		events, err := s.newEvents(ctx, module, from, current)
		if err != nil {
			// The poll stops here and the same blocks are polled again.
			return 0, err
		}
		if events.count() == 0 {
			continue
		}
		s.logf("Found %d protocol events, %d transfer events, %d limits-set events",
			len(events.protocol), len(events.transfers), events.limitsSet)
		for _, sub := range events.subAccounts() {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			s.process(ctx, module, sub, current, pending, false)
		}
	}
	// Interrupted (shutdown): these blocks were not all processed.
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return current, nil
}

// Refresh processes every subaccount of every module (onCronRefresh). It is
// skipped, with a log line, while a poll runs.
func (s *Service) Refresh(ctx context.Context) {
	if !s.mu.TryLock() {
		s.logf("Skipping cron refresh - another operation is running")
		return
	}
	defer s.mu.Unlock()
	s.logf("=== Spending Oracle: Periodic Refresh ===")

	var pending []pendingTx
	err := s.refresh(ctx, &pending)
	s.waitPending(ctx, pending)
	if err != nil {
		s.logf("Error in periodic refresh: %v", err)
		return
	}
	s.logf("=== Periodic Refresh Complete ===")
}

func (s *Service) refresh(ctx context.Context, pending *[]pendingTx) error {
	modules, err := discovery.ActiveModules(ctx, s.chain, s.cfg.RegistryAddress, s.cfg.ModuleAddress, s.logf)
	if err != nil {
		return err
	}
	latest, err := s.chain.BlockNumber(ctx)
	if err != nil {
		return err
	}
	current := ethx.SubClamped(latest, uint64(s.cfg.ConfirmationBlocks))
	s.logf("Processing %d module(s)", len(modules))

	for _, module := range modules {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.logf("--- Processing module: %s ---", module.Hex())
		if !discovery.Authorized(ctx, s.chain, module, s.sender.Address(), s.logf) {
			continue
		}
		subAccounts, err := s.subAccounts(ctx, module)
		if err != nil {
			// The TS gave up on the whole refresh here; the other modules
			// still deserve theirs.
			s.logf("Error processing module %s: %v", module.Hex(), err)
			continue
		}
		s.logf("Found %d active subaccounts", len(subAccounts))
		if len(subAccounts) == 0 {
			s.logf("No active subaccounts for this module, skipping")
			continue
		}
		for _, sub := range subAccounts {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.process(ctx, module, sub, current, pending, true)
		}
	}
	// A refresh interrupted (shutdown) during its last subaccount is not
	// complete either.
	return ctx.Err()
}

// subAccounts lists the module's subaccounts holding any of the execute,
// transfer and repay roles, in that role order, without duplicates.
func (s *Service) subAccounts(ctx context.Context, module common.Address) ([]common.Address, error) {
	m := contracts.NewModule(module, s.chain)
	var out []common.Address
	seen := map[common.Address]bool{}
	for _, role := range []uint16{contracts.DefiExecuteRole, contracts.DefiTransferRole, contracts.DefiRepayRole} {
		accounts, err := m.GetSubaccountsByRole(ctx, role)
		if err != nil {
			return nil, fmt.Errorf("getSubaccountsByRole(%d): %w", role, err)
		}
		for _, a := range accounts {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	return out, nil
}

// process runs processSubaccount (announced in the log, as refreshes did in
// the TS) and logs its failure; a panic counts as a failure of this
// subaccount only.
func (s *Service) process(ctx context.Context, module, sub common.Address, block uint64, pending *[]pendingTx, announce bool) {
	if sub == module {
		s.logf("Skipping %s - this is the module address, not a subaccount", lower(sub))
		return
	}
	if announce {
		s.logf("Processing subaccount: %s", lower(sub))
	}
	err := func() (err error) {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic: %v", p)
			}
		}()
		return s.processSubaccount(ctx, module, sub, block, pending)
	}()
	if err != nil {
		s.logf("Error processing %s: %v", lower(sub), err)
	}
}

// processSubaccount rebuilds sub's state from its events of the last
// 2*BLOCKS_TO_LOOK_BACK blocks up to block, and submits a batchUpdate when
// the on-chain values need one.
func (s *Service) processSubaccount(ctx context.Context, module, sub common.Address, block uint64, pending *[]pendingTx) error {
	now := s.Now().Unix()
	// 2x the lookback, to find tokens acquired before the current window.
	from := ethx.SubClamped(block, uint64(2*s.cfg.BlocksToLookBack))

	var (
		limits    contracts.SubAccountLimits
		protocol  []spending.ProtocolExecutionEvent
		transfers []spending.TransferExecutedEvent
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		limits, err = contracts.NewModule(module, s.chain).GetSubAccountLimits(gctx, sub)
		return err
	})
	g.Go(func() (err error) {
		protocol, err = s.protocolEvents(gctx, module, from, block, &sub)
		return err
	})
	g.Go(func() (err error) {
		transfers, err = s.transferEvents(gctx, module, from, block, &sub)
		return err
	})
	if err := g.Wait(); err != nil {
		return err
	}

	// Price every token involved, for USD-weighted ratios.
	var tokens []common.Address
	seen := map[common.Address]bool{}
	add := func(t common.Address) {
		if !seen[t] {
			seen[t] = true
			tokens = append(tokens, t)
		}
	}
	for _, e := range protocol {
		for _, t := range e.TokensIn {
			add(t)
		}
		for _, t := range e.TokensOut {
			add(t)
		}
	}
	for _, e := range transfers {
		add(e.Token)
	}
	priceCache := s.prices.BuildPriceCache(ctx, tokens)
	if len(priceCache) > 0 {
		s.logf("Built price cache for %d tokens", len(priceCache))
	}

	window := maxWindowDuration
	if limits.WindowDuration.IsInt64() && limits.WindowDuration.Int64() < maxWindowDuration {
		window = limits.WindowDuration.Int64()
	}
	state := spending.BuildSubAccountState(protocol, transfers, sub, now, window, priceCache, s.logf)

	newAllowance, err := s.allowance(ctx, module, limits, state.TotalSpendingInWindow)
	if err != nil {
		return err
	}
	b, err := s.prepareBatch(ctx, module, sub, newAllowance, state.AcquiredBalances)
	if err != nil || b == nil {
		return err
	}
	tx, err := s.submit(ctx, b)
	if err != nil {
		return err
	}
	*pending = append(*pending, pendingTx{tx: tx, batch: b})
	return nil
}

// lower formats an address like the TS subaccount strings (lowercase).
func lower(a common.Address) string { return strings.ToLower(a.Hex()) }
