package spending

// Port of the TS oracle's spending-oracle-edge.test.ts (26 cases). Subtest
// names are the TS describe/it names.

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

var e = struct {
	SubA, SubB, Aave, Uni            common.Address
	USDC, WETH, DAI, LINK, Recipient common.Address
	Now, HourAgo, DayAgo, TwoDaysAgo int64
}{
	SubA:       addr("0x1111111111111111111111111111111111111111"),
	SubB:       addr("0x2222222222222222222222222222222222222222"),
	Aave:       addr("0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa"),
	Uni:        addr("0xBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBb"),
	USDC:       addr("0x3333333333333333333333333333333333333333"),
	WETH:       addr("0x4444444444444444444444444444444444444444"),
	DAI:        addr("0x5555555555555555555555555555555555555555"),
	LINK:       addr("0x6666666666666666666666666666666666666666"),
	Recipient:  addr("0x7777777777777777777777777777777777777777"),
	Now:        1700100000,
	HourAgo:    1700100000 - 3600,
	DayAgo:     1700100000 - oneDay,
	TwoDaysAgo: 1700100000 - 2*oneDay,
}

func eProtocol(override func(ev *ProtocolExecutionEvent)) ProtocolExecutionEvent {
	ev := ProtocolExecutionEvent{
		SubAccount: e.SubA, Target: e.Aave, OpType: contracts.OpSwap,
		TokensIn: addrs(e.USDC), AmountsIn: ints(1000),
		TokensOut: addrs(e.WETH), AmountsOut: ints(500),
		SpendingCost: big.NewInt(100), Timestamp: e.HourAgo, BlockNumber: 1000, LogIndex: 0,
	}
	if override != nil {
		override(&ev)
	}
	return ev
}

func eTransfer(override func(ev *TransferExecutedEvent)) TransferExecutedEvent {
	ev := TransferExecutedEvent{
		SubAccount: e.SubA, Token: e.USDC, Recipient: e.Recipient,
		Amount: big.NewInt(100), SpendingCost: big.NewInt(50),
		Timestamp: e.HourAgo, BlockNumber: 1000, LogIndex: 0,
	}
	if override != nil {
		override(&ev)
	}
	return ev
}

func eDeposit(token common.Address, amount int64, ts, block int64) ProtocolExecutionEvent {
	return eProtocol(func(ev *ProtocolExecutionEvent) {
		ev.OpType, ev.Target = contracts.OpDeposit, e.Aave
		ev.TokensIn, ev.AmountsIn = addrs(token), ints(amount)
		ev.TokensOut, ev.AmountsOut = addrs(), ints()
		ev.Timestamp, ev.BlockNumber, ev.LogIndex = ts, block, 0
	})
}

func eWithdraw(target, token common.Address, amount int64, ts, block int64) ProtocolExecutionEvent {
	return eProtocol(func(ev *ProtocolExecutionEvent) {
		ev.OpType, ev.Target = contracts.OpWithdraw, target
		ev.TokensIn, ev.AmountsIn = addrs(), ints()
		ev.TokensOut, ev.AmountsOut = addrs(token), ints(amount)
		ev.Timestamp, ev.BlockNumber, ev.LogIndex = ts, block, 0
	})
}

func TestEdgeWindowBoundaryPrecision(t *testing.T) {
	swapAt := func(ts int64) []ProtocolExecutionEvent {
		return pe(eProtocol(func(ev *ProtocolExecutionEvent) {
			ev.TokensOut, ev.AmountsOut = addrs(e.WETH), ints(100)
			ev.SpendingCost, ev.Timestamp = big.NewInt(50), ts
		}))
	}
	t.Run("should treat event at exact window start as valid", func(t *testing.T) {
		// The TS accepted either outcome; the implementation uses >=, so the
		// event at exactly NOW - window is valid. Asserted strictly here.
		s := build(swapAt(e.DayAgo), nil, e.SubA, e.Now, nil)
		wantBalance(t, s, e.WETH, 100)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 50)
	})
	t.Run("should treat event 1 second before window as expired", func(t *testing.T) {
		s := build(swapAt(e.DayAgo-1), nil, e.SubA, e.Now, nil)
		wantNoBalance(t, s, e.WETH)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 0)
	})
	t.Run("should treat event 1 second after window start as valid", func(t *testing.T) {
		s := build(swapAt(e.DayAgo+1), nil, e.SubA, e.Now, nil)
		wantBalance(t, s, e.WETH, 100)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 50)
	})
}

func TestEdgeSubAccountIsolation(t *testing.T) {
	t.Run("should not leak acquired balances between sub-accounts", func(t *testing.T) {
		events := pe(
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.SubAccount = e.SubA
				ev.TokensOut, ev.AmountsOut = addrs(e.WETH), ints(500)
				ev.Timestamp, ev.BlockNumber = e.HourAgo, 1000
			}),
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.SubAccount = e.SubB
				ev.TokensOut, ev.AmountsOut = addrs(e.USDC), ints(1000)
				ev.Timestamp, ev.BlockNumber, ev.LogIndex = e.HourAgo, 1001, 1
			}),
		)
		a := build(events, nil, e.SubA, e.Now, nil)
		b := build(events, nil, e.SubB, e.Now, nil)
		wantBalance(t, a, e.WETH, 500)
		wantNoBalance(t, a, e.USDC)
		wantBalance(t, b, e.USDC, 1000)
		wantNoBalance(t, b, e.WETH)
	})
	t.Run("should not leak spending between sub-accounts", func(t *testing.T) {
		events := pe(
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.SubAccount, ev.SpendingCost, ev.Timestamp = e.SubA, big.NewInt(100), e.HourAgo
			}),
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.SubAccount, ev.SpendingCost, ev.Timestamp = e.SubB, big.NewInt(200), e.HourAgo
				ev.BlockNumber, ev.LogIndex = 1001, 1
			}),
		)
		wantInt(t, "A spending", build(events, nil, e.SubA, e.Now, nil).TotalSpendingInWindow, 100)
		wantInt(t, "B spending", build(events, nil, e.SubB, e.Now, nil).TotalSpendingInWindow, 200)
	})
	t.Run("should not let sub-account B consume sub-account A deposits", func(t *testing.T) {
		events := pe(
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.SubAccount, ev.OpType = e.SubA, contracts.OpDeposit
				ev.TokensIn, ev.AmountsIn = addrs(e.USDC), ints(1000)
				ev.TokensOut, ev.AmountsOut = addrs(), ints()
				ev.Timestamp, ev.BlockNumber = e.HourAgo, 1000
			}),
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.SubAccount, ev.OpType = e.SubB, contracts.OpWithdraw
				ev.TokensIn, ev.AmountsIn = addrs(), ints()
				ev.TokensOut, ev.AmountsOut = addrs(e.USDC), ints(1000)
				ev.Timestamp, ev.BlockNumber, ev.LogIndex = e.HourAgo+100, 1001, 1
			}),
		)
		wantNoBalance(t, build(events, nil, e.SubB, e.Now, nil), e.USDC)
	})
}

func TestEdgeSameBlockEventOrdering(t *testing.T) {
	swap := func(logIndex int64) ProtocolExecutionEvent {
		return eProtocol(func(ev *ProtocolExecutionEvent) {
			ev.TokensOut, ev.AmountsOut = addrs(e.WETH), ints(500)
			ev.Timestamp, ev.BlockNumber, ev.LogIndex = e.HourAgo, 1000, logIndex
		})
	}
	transfer := func(logIndex, spending int64) TransferExecutedEvent {
		return eTransfer(func(ev *TransferExecutedEvent) {
			ev.Token, ev.Amount, ev.SpendingCost = e.WETH, big.NewInt(200), big.NewInt(spending)
			ev.Timestamp, ev.BlockNumber, ev.LogIndex = e.HourAgo, 1000, logIndex
		})
	}
	t.Run("should process events by logIndex within same block", func(t *testing.T) {
		s := build(pe(swap(0)), te(transfer(1, 10)), e.SubA, e.Now, nil)
		wantBalance(t, s, e.WETH, 300)
	})
	t.Run("should handle transfer before swap in same block (no acquired to consume)", func(t *testing.T) {
		s := build(pe(swap(5)), te(transfer(0, 100)), e.SubA, e.Now, nil)
		wantBalance(t, s, e.WETH, 500)
	})
}

func TestEdgeDepositWithdrawalMatchingEdgeCases(t *testing.T) {
	t.Run("should handle partial withdrawal against deposit", func(t *testing.T) {
		s := build(pe(
			eDeposit(e.USDC, 1000, e.HourAgo, 1000),
			eWithdraw(e.Aave, e.USDC, 400, e.HourAgo+100, 1001),
		), nil, e.SubA, e.Now, nil)
		wantBalance(t, s, e.USDC, 400)
	})
	t.Run("should handle withdrawal exceeding deposit (yield)", func(t *testing.T) {
		s := build(pe(
			eDeposit(e.USDC, 1000, e.HourAgo, 1000),
			eWithdraw(e.Aave, e.USDC, 1050, e.HourAgo+100, 1001),
		), nil, e.SubA, e.Now, nil)
		wantBalance(t, s, e.USDC, 1000)
	})
	t.Run("should not match withdrawal from different protocol", func(t *testing.T) {
		s := build(pe(
			eDeposit(e.USDC, 1000, e.HourAgo, 1000),
			eWithdraw(e.Uni, e.USDC, 1000, e.HourAgo+100, 1001),
		), nil, e.SubA, e.Now, nil)
		wantNoBalance(t, s, e.USDC)
	})
	t.Run("should not match withdrawal of different token", func(t *testing.T) {
		s := build(pe(
			eDeposit(e.USDC, 1000, e.HourAgo, 1000),
			eWithdraw(e.Aave, e.WETH, 500, e.HourAgo+100, 1001),
		), nil, e.SubA, e.Now, nil)
		wantNoBalance(t, s, e.WETH)
	})
	t.Run("should handle multiple deposits consumed by single withdrawal (FIFO)", func(t *testing.T) {
		first := eDeposit(e.USDC, 300, e.HourAgo, 1000)
		first.SpendingCost = big.NewInt(30)
		second := eDeposit(e.USDC, 700, e.HourAgo+50, 1001)
		second.SpendingCost = big.NewInt(70)
		s := build(pe(first, second, eWithdraw(e.Aave, e.USDC, 500, e.HourAgo+100, 1002)), nil, e.SubA, e.Now, nil)
		wantBalance(t, s, e.USDC, 500)
	})
}

func TestEdgeRapidOperationSequences(t *testing.T) {
	t.Run("should correctly track spending across many rapid swaps", func(t *testing.T) {
		const totalSwaps = 50
		var events []ProtocolExecutionEvent
		for i := int64(0); i < totalSwaps; i++ {
			events = append(events, eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.TokensIn, ev.AmountsIn = addrs(e.USDC), ints(100)
				ev.TokensOut, ev.AmountsOut = addrs(e.WETH), ints(50)
				ev.SpendingCost = big.NewInt(10)
				ev.Timestamp, ev.BlockNumber, ev.LogIndex = e.HourAgo+i, 1000+i, 0
			}))
		}
		s := build(events, nil, e.SubA, e.Now, nil)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, totalSwaps*10)
		wantBalance(t, s, e.WETH, totalSwaps*50)
	})
	t.Run("should correctly track acquired balance through swap chain", func(t *testing.T) {
		s := build(pe(
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.TokensIn, ev.AmountsIn = addrs(e.USDC), ints(1000)
				ev.TokensOut, ev.AmountsOut = addrs(e.WETH), ints(500)
				ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(100), e.HourAgo, 1000
			}),
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.TokensIn, ev.AmountsIn = addrs(e.WETH), ints(500)
				ev.TokensOut, ev.AmountsOut = addrs(e.DAI), ints(490)
				ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(0), e.HourAgo+10, 1001
			}),
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.TokensIn, ev.AmountsIn = addrs(e.DAI), ints(490)
				ev.TokensOut, ev.AmountsOut = addrs(e.LINK), ints(100)
				ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(0), e.HourAgo+20, 1002
			}),
		), nil, e.SubA, e.Now, nil)
		wantNoBalance(t, s, e.WETH)
		wantNoBalance(t, s, e.DAI)
		wantBalance(t, s, e.LINK, 100)
		if q := queueOf(t, s, e.LINK); q[0].OriginalTimestamp != e.HourAgo {
			t.Fatalf("LINK timestamp = %d, want inherited %d", q[0].OriginalTimestamp, e.HourAgo)
		}
	})
}

func TestEdgeFIFOQueueStress(t *testing.T) {
	t.Run("should handle queue with many small entries", func(t *testing.T) {
		q := Queue{}
		for i := int64(0); i < 100; i++ {
			AddToQueue(&q, big.NewInt(10), e.Now-(100-i))
		}
		wantLen(t, "queue", q, 100)
		wantInt(t, "balance", ValidQueueBalance(q, e.Now, windowDuration), 1000)
		_, remaining := ConsumeFromQueue(&q, big.NewInt(550), e.Now, windowDuration)
		wantInt(t, "remaining", remaining, 0)
		wantLen(t, "queue", q, 45)
		wantInt(t, "balance", ValidQueueBalance(q, e.Now, windowDuration), 450)
	})
	t.Run("should handle consuming exact queue total", func(t *testing.T) {
		q := entries(100, e.HourAgo, 200, e.HourAgo+10, 300, e.HourAgo+20)
		consumed, remaining := ConsumeFromQueue(&q, big.NewInt(600), e.Now, windowDuration)
		wantInt(t, "remaining", remaining, 0)
		wantLen(t, "queue", q, 0)
		wantLen(t, "consumed", consumed, 3)
	})
	t.Run("should handle consuming more than queue total", func(t *testing.T) {
		q := entries(100, e.HourAgo)
		_, remaining := ConsumeFromQueue(&q, big.NewInt(999), e.Now, windowDuration)
		wantInt(t, "remaining", remaining, 899)
		wantLen(t, "queue", q, 0)
	})
	t.Run("should handle consuming zero amount", func(t *testing.T) {
		q := entries(100, e.HourAgo)
		consumed, remaining := ConsumeFromQueue(&q, big.NewInt(0), e.Now, windowDuration)
		wantInt(t, "remaining", remaining, 0)
		wantLen(t, "consumed", consumed, 0)
		wantLen(t, "queue", q, 1)
	})
}

func TestEdgeComplexMultiOperationSequences(t *testing.T) {
	t.Run("should handle swap → deposit → withdraw → transfer sequence", func(t *testing.T) {
		s := build(
			pe(
				eProtocol(func(ev *ProtocolExecutionEvent) {
					ev.TokensIn, ev.AmountsIn = addrs(e.USDC), ints(1000)
					ev.TokensOut, ev.AmountsOut = addrs(e.WETH), ints(500)
					ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(100), e.HourAgo, 1000
				}),
				eProtocol(func(ev *ProtocolExecutionEvent) {
					ev.OpType, ev.Target = contracts.OpDeposit, e.Aave
					ev.TokensIn, ev.AmountsIn = addrs(e.WETH), ints(300)
					ev.TokensOut, ev.AmountsOut = addrs(), ints()
					ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(0), e.HourAgo+10, 1001
				}),
				// WITHDRAW keeps the default spendingCost (100), which is ignored.
				eWithdraw(e.Aave, e.WETH, 300, e.HourAgo+20, 1002),
			),
			te(eTransfer(func(ev *TransferExecutedEvent) {
				ev.Token, ev.Amount, ev.SpendingCost = e.WETH, big.NewInt(100), big.NewInt(5)
				ev.Timestamp, ev.BlockNumber = e.HourAgo+30, 1003
			})),
			e.SubA, e.Now, nil)
		wantBalance(t, s, e.WETH, 400)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 105)
	})
	t.Run("should handle CLAIM without prior deposit (rewards)", func(t *testing.T) {
		s := build(pe(eProtocol(func(ev *ProtocolExecutionEvent) {
			ev.OpType, ev.Target = contracts.OpClaim, e.Aave
			ev.TokensIn, ev.AmountsIn = addrs(), ints()
			ev.TokensOut, ev.AmountsOut = addrs(e.LINK), ints(50)
			ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(0), e.HourAgo, 1000
		})), nil, e.SubA, e.Now, nil)
		wantNoBalance(t, s, e.LINK)
	})
	t.Run("should handle CLAIM with prior deposit (matched reward)", func(t *testing.T) {
		deposit := eDeposit(e.LINK, 1000, e.HourAgo, 1000)
		deposit.SpendingCost = big.NewInt(50)
		s := build(pe(deposit, eProtocol(func(ev *ProtocolExecutionEvent) {
			ev.OpType, ev.Target = contracts.OpClaim, e.Aave
			ev.TokensIn, ev.AmountsIn = addrs(), ints()
			ev.TokensOut, ev.AmountsOut = addrs(e.LINK), ints(20)
			ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(0), e.HourAgo+100, 1001
		})), nil, e.SubA, e.Now, nil)
		wantBalance(t, s, e.LINK, 20)
	})
}

func TestEdgeEmptyAndZeroEdgeCases(t *testing.T) {
	t.Run("should handle no events", func(t *testing.T) {
		s := build(nil, nil, e.SubA, e.Now, nil)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 0)
		if s.AcquiredBalances.Len() != 0 {
			t.Fatalf("acquiredBalances.size = %d", s.AcquiredBalances.Len())
		}
		wantLen(t, "depositRecords", s.DepositRecords, 0)
		wantLen(t, "spendingRecords", s.SpendingRecords, 0)
	})
	t.Run("should handle events with zero amounts", func(t *testing.T) {
		s := build(pe(eProtocol(func(ev *ProtocolExecutionEvent) {
			ev.TokensIn, ev.AmountsIn = addrs(e.USDC), ints(0)
			ev.TokensOut, ev.AmountsOut = addrs(e.WETH), ints(0)
			ev.SpendingCost, ev.Timestamp = big.NewInt(0), e.HourAgo
		})), nil, e.SubA, e.Now, nil)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 0)
		wantNoBalance(t, s, e.WETH)
	})
	t.Run("should handle events with empty token arrays", func(t *testing.T) {
		s := build(pe(eProtocol(func(ev *ProtocolExecutionEvent) {
			ev.TokensIn, ev.AmountsIn = addrs(), ints()
			ev.TokensOut, ev.AmountsOut = addrs(), ints()
			ev.SpendingCost, ev.Timestamp = big.NewInt(50), e.HourAgo
		})), nil, e.SubA, e.Now, nil)
		wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 50)
		if s.AcquiredBalances.Len() != 0 {
			t.Fatalf("acquiredBalances.size = %d", s.AcquiredBalances.Len())
		}
	})
	t.Run("should handle case-insensitive token addresses", func(t *testing.T) {
		// In Go addresses are bytes, so case can't matter; kept for coverage.
		upper := addr("0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		lowerTok := addr("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		s := build(pe(
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.TokensOut, ev.AmountsOut = addrs(upper), ints(100)
				ev.Timestamp, ev.BlockNumber = e.HourAgo, 1000
			}),
			eProtocol(func(ev *ProtocolExecutionEvent) {
				ev.TokensIn, ev.AmountsIn = addrs(lowerTok), ints(50)
				ev.TokensOut, ev.AmountsOut = addrs(e.USDC), ints(25)
				ev.SpendingCost, ev.Timestamp, ev.BlockNumber = big.NewInt(0), e.HourAgo+10, 1001
			}),
		), nil, e.SubA, e.Now, nil)
		wantBalance(t, s, lowerTok, 50)
	})
}
