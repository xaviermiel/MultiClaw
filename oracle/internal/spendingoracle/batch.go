package spendingoracle

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/sync/errgroup"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/spending"
)

// batch is a batchUpdate to send (prepareBatchUpdate's result).
type batch struct {
	module, subAccount common.Address
	allowanceVersion   *big.Int
	newAllowance       *big.Int
	tokens             []common.Address
	balances           []*big.Int
	tokenVersions      []*big.Int
	// preparedAt becomes the subaccount's last update once confirmed.
	preparedAt int64
}

type pendingTx struct {
	tx    *ethx.SentTx
	batch *batch
}

// prepareBatch returns the batchUpdate module/sub needs, or nil. As in the
// TS, it is needed when
//   - the allowance decreased, increased by more than
//     ALLOWANCE_INCREASE_THRESHOLD_BPS (or from 0), or the last confirmed
//     update is older than SPENDING_ORACLE_MAX_STALENESS_SECONDS (never, after
//     a restart); or
//   - an acquired balance differs from the on-chain one, or a token acquired
//     earlier (per its AcquiredBalanceUpdated history) still has an on-chain
//     balance that must be cleared to 0.
//
// The batch carries every computed acquired balance, in state order, then
// the balances to clear.
func (s *Service) prepareBatch(ctx context.Context, module, sub common.Address, newAllowance *big.Int,
	acquired *spending.OrderedMap[common.Address, *big.Int]) (*batch, error) {
	m := contracts.NewModule(module, s.chain)
	onChainAllowance, err := m.GetSpendingAllowance(ctx, sub)
	if err != nil {
		return nil, err
	}
	allowanceVersion, err := m.AllowanceVersion(ctx, sub)
	if err != nil {
		return nil, err
	}
	now := s.Now().Unix()
	sinceUpdate := now - s.lastUpdate[updateKey{module, sub}]
	stale := sinceUpdate > s.cfg.SpendingOracleMaxStalenessSeconds

	decreased := newAllowance.Cmp(onChainAllowance) < 0
	increased := newAllowance.Cmp(onChainAllowance) > 0
	significantIncrease := false
	if increased {
		if onChainAllowance.Sign() > 0 {
			threshold := bn.MulDiv(onChainAllowance, s.cfg.AllowanceIncreaseThresholdBps, bn.BPS)
			significantIncrease = bn.Sub(newAllowance, onChainAllowance).Cmp(threshold) > 0
		} else {
			significantIncrease = newAllowance.Sign() > 0
		}
	}
	allowanceChanged := decreased || significantIncrease || stale
	if !allowanceChanged {
		if increased {
			increase := bn.Sub(newAllowance, onChainAllowance)
			increaseBps := bn.Zero()
			if onChainAllowance.Sign() > 0 {
				increaseBps = bn.MulDiv(increase, bn.BPS, onChainAllowance)
			}
			s.logf("  Allowance increase within threshold: %s -> %s (+%sbps, threshold: %sbps)",
				bn.FormatUnits(onChainAllowance, 18), bn.FormatUnits(newAllowance, 18), increaseBps, s.cfg.AllowanceIncreaseThresholdBps)
		} else {
			s.logf("  Allowance unchanged: %s", bn.FormatUnits(onChainAllowance, 18))
		}
	}

	b := &batch{module: module, subAccount: sub, allowanceVersion: allowanceVersion, newAllowance: newAllowance, preparedAt: now}
	acquiredChanged := false
	tokens := acquired.Keys()
	onChain, versions, err := s.acquiredBalances(ctx, m, sub, tokens)
	if err != nil {
		return nil, err
	}
	for i, token := range tokens {
		balance, _ := acquired.Get(token)
		if balance.Cmp(onChain[i]) != 0 {
			acquiredChanged = true
		}
		b.tokens = append(b.tokens, token)
		b.balances = append(b.balances, balance)
		b.tokenVersions = append(b.tokenVersions, versions[i])
	}

	// Tokens with an on-chain balance that the events no longer support
	// (aged out, or matched differently) are cleared to 0.
	history, err := s.historicalTokens(ctx, module, sub)
	if err != nil {
		return nil, err
	}
	var candidates []common.Address
	for _, token := range history {
		if !acquired.Has(token) {
			candidates = append(candidates, token)
		}
	}
	onChain, versions, err = s.acquiredBalances(ctx, m, sub, candidates)
	if err != nil {
		return nil, err
	}
	for i, token := range candidates {
		if onChain[i].Sign() > 0 {
			s.logf("  Clearing stale acquired balance for %s: %s -> 0", lower(token), onChain[i])
			acquiredChanged = true
			b.tokens = append(b.tokens, token)
			b.balances = append(b.balances, bn.Zero())
			b.tokenVersions = append(b.tokenVersions, versions[i])
		}
	}

	if !allowanceChanged && !acquiredChanged {
		s.logf("Skipping batch update - no changes needed:")
		s.logf("  Allowance: %s -> %s (no decrease, increase <2%%)", bn.FormatUnits(onChainAllowance, 18), bn.FormatUnits(newAllowance, 18))
		s.logf("  Staleness: %ds (max: %ds)", sinceUpdate, s.cfg.SpendingOracleMaxStalenessSeconds)
		s.logf("  Acquired tokens: %d (no changes)", len(b.tokens))
		return nil, nil
	}

	var reasons []string
	if acquiredChanged {
		reasons = append(reasons, "acquired changed")
	}
	if decreased {
		reasons = append(reasons, "allowance decreased")
	}
	if significantIncrease {
		reasons = append(reasons, "allowance increased >2%")
	}
	if stale {
		reasons = append(reasons, fmt.Sprintf("stale (%ds > %ds)", sinceUpdate, s.cfg.SpendingOracleMaxStalenessSeconds))
	}
	s.logf("Preparing batch update: subAccount=%s", lower(sub))
	s.logf("  Reason: %s", strings.Join(reasons, ", "))
	s.logf("  Allowance: %s -> %s", bn.FormatUnits(onChainAllowance, 18), bn.FormatUnits(newAllowance, 18))
	s.logf("  Tokens: %d", len(b.tokens))
	return b, nil
}

// acquiredBalances reads the on-chain acquired balance and version of each
// token (in parallel, results in order).
func (s *Service) acquiredBalances(ctx context.Context, m contracts.Module, sub common.Address, tokens []common.Address) (balances, versions []*big.Int, err error) {
	balances = make([]*big.Int, len(tokens))
	versions = make([]*big.Int, len(tokens))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallelReads)
	for i, token := range tokens {
		goSafe(g, func() (err error) {
			if balances[i], err = m.GetAcquiredBalance(gctx, sub, token); err != nil {
				return err
			}
			versions[i], err = m.AcquiredBalanceVersion(gctx, sub, token)
			return err
		})
	}
	return balances, versions, g.Wait()
}

// submit sends the batchUpdate without waiting for it.
func (s *Service) submit(ctx context.Context, b *batch) (*ethx.SentTx, error) {
	data, err := contracts.PackBatchUpdate(b.subAccount, b.allowanceVersion, b.newAllowance, b.tokens, b.tokenVersions, b.balances)
	if err != nil {
		return nil, err
	}
	tx, err := s.sender.Send(ctx, b.module, data)
	if err != nil {
		s.logf("Error submitting batch update for %s: %v", lower(b.subAccount), err)
		return nil, err
	}
	s.logf("Transaction submitted: %s (nonce: %d)", tx.Hash.Hex(), tx.Nonce)
	return tx, nil
}

// waitPending waits for the submitted batches (waitForPendingTransactions).
// Only a confirmed, successful one records its subaccount's last update.
func (s *Service) waitPending(ctx context.Context, pending []pendingTx) {
	if len(pending) == 0 {
		return
	}
	s.logf("Waiting for %d pending transactions...", len(pending))
	confirmed := make([]bool, len(pending))
	var wg sync.WaitGroup
	for i, p := range pending {
		wg.Add(1)
		go func() {
			defer wg.Done()
			confirmed[i] = s.confirm(ctx, p)
		}()
	}
	wg.Wait()

	ok := 0
	for i, p := range pending {
		if confirmed[i] {
			ok++
			s.lastUpdate[updateKey{p.batch.module, p.batch.subAccount}] = p.batch.preparedAt
		}
	}
	s.logf("Transaction results: %d confirmed, %d failed", ok, len(pending)-ok)
}

func (s *Service) confirm(ctx context.Context, p pendingTx) (confirmed bool) {
	short, sub := p.tx.Hash.Hex()[:10], lower(p.batch.subAccount)
	defer func() {
		if r := recover(); r != nil {
			s.logf("Transaction %s... failed for %s: panic: %v", short, sub, r)
			confirmed = false
		}
	}()
	receipt, err := s.sender.Wait(ctx, p.tx)
	if err == nil && receipt.Status != types.ReceiptStatusSuccessful {
		reason := ethx.RevertReason(ctx, s.chain, p.tx, receipt)
		s.logf("Transaction %s... REVERTED on-chain in block %s (%s) — gasUsed=%d, reason=%s",
			short, receipt.BlockNumber, sub, receipt.GasUsed, reason)
		err = fmt.Errorf("transaction reverted on-chain (block %s, tx %s, reason=%s)", receipt.BlockNumber, p.tx.Hash.Hex(), reason)
	}
	if err != nil {
		s.logf("Transaction %s... failed for %s: %v", short, sub, err)
		return false
	}
	s.logf("Transaction %s... confirmed in block %s (%s)", short, receipt.BlockNumber, sub)
	return true
}
