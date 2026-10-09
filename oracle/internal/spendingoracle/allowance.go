package spendingoracle

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

// allowance is a subaccount's new spending allowance: its spending limit
// (maxSpendingUSD in USD mode, else maxSpendingBps of the Safe value) minus
// what it spent in the window, floored at 0 (calculateSpendingAllowance).
func (s *Service) allowance(ctx context.Context, module common.Address, limits contracts.SubAccountLimits, spent *big.Int) (*big.Int, error) {
	m := contracts.NewModule(module, s.chain)
	safeValue, err := m.GetSafeValue(ctx)
	if err != nil {
		return nil, fmt.Errorf("getSafeValue: %w", err)
	}
	mode, maxSpending := "BPS", bn.MulDiv(safeValue.TotalValueUSD, limits.MaxSpendingBps, bn.BPS)
	if limits.MaxSpendingUSD.Sign() > 0 {
		mode, maxSpending = "USD", limits.MaxSpendingUSD
	}
	// Implementations deployed before April 20 cap allowances at
	// absoluteMaxSpendingBps of the Safe value and revert batchUpdates above
	// it. Newer ones dropped the getter: if it can't be read, there's no cap.
	if absoluteBps, err := m.AbsoluteMaxSpendingBps(ctx); err == nil {
		capped := bn.MulDiv(safeValue.TotalValueUSD, absoluteBps, bn.BPS)
		if maxSpending.Cmp(capped) > 0 {
			s.logf("Allowance capped by absoluteMaxSpendingBps (%sbps): %s -> %s",
				absoluteBps, bn.FormatUnits(maxSpending, 18), bn.FormatUnits(capped, 18))
			maxSpending = capped
		}
	}
	newAllowance := bn.Zero()
	if maxSpending.Cmp(spent) > 0 {
		newAllowance = bn.Sub(maxSpending, spent)
	}
	s.logf("Allowance: safeValue=%s, mode=%s, max=%s, spent=%s, new=%s",
		bn.FormatUnits(safeValue.TotalValueUSD, 18), mode, bn.FormatUnits(maxSpending, 18),
		bn.FormatUnits(spent, 18), bn.FormatUnits(newAllowance, 18))
	return newAllowance, nil
}
