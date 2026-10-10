package spending

// Port of the TS oracle's spending-oracle.test.ts (55 cases). Subtest names
// are the TS describe/it names. NOW is fixed (the TS used the wall clock; no
// assertion depends on absolute time).

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

var m = struct {
	Sub, Other, Aave, Uniswap          common.Address
	LINK, ALINK, WETH, USDC, Recipient common.Address
	LP, ReceiptA, ReceiptB             common.Address
	Now, HourAgo, DayAgo, TwoDaysAgo   int64
}{
	Sub:        addr("0x1234567890123456789012345678901234567890"),
	Other:      addr("0x9999999999999999999999999999999999999999"),
	Aave:       addr("0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa"),
	Uniswap:    addr("0xBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBb"),
	LINK:       addr("0x1111111111111111111111111111111111111111"),
	ALINK:      addr("0x2222222222222222222222222222222222222222"),
	WETH:       addr("0x3333333333333333333333333333333333333333"),
	USDC:       addr("0x4444444444444444444444444444444444444444"),
	Recipient:  addr("0x5555555555555555555555555555555555555555"),
	LP:         addr("0x6666666666666666666666666666666666666666"),
	ReceiptA:   addr("0x7777777777777777777777777777777777777777"),
	ReceiptB:   addr("0x8888888888888888888888888888888888888888"),
	Now:        1700100000,
	HourAgo:    1700100000 - 3600,
	DayAgo:     1700100000 - oneDay,
	TwoDaysAgo: 1700100000 - 2*oneDay,
}

// mProtocol mirrors createProtocolEvent (defaults + overrides).
func mProtocol(override func(e *ProtocolExecutionEvent)) ProtocolExecutionEvent {
	e := ProtocolExecutionEvent{
		SubAccount: m.Sub, Target: m.Aave, OpType: contracts.OpSwap,
		TokensIn: addrs(m.LINK), AmountsIn: ints(100),
		TokensOut: addrs(m.WETH), AmountsOut: ints(50),
		SpendingCost: big.NewInt(10), Timestamp: m.HourAgo, BlockNumber: 1000, LogIndex: 0,
	}
	if override != nil {
		override(&e)
	}
	return e
}

// mTransfer mirrors createTransferEvent (defaults + overrides).
func mTransfer(override func(e *TransferExecutedEvent)) TransferExecutedEvent {
	e := TransferExecutedEvent{
		SubAccount: m.Sub, Token: m.LINK, Recipient: m.Recipient,
		Amount: big.NewInt(50), SpendingCost: big.NewInt(5),
		Timestamp: m.HourAgo, BlockNumber: 1000, LogIndex: 0,
	}
	if override != nil {
		override(&e)
	}
	return e
}

func pe(events ...ProtocolExecutionEvent) []ProtocolExecutionEvent { return events }
func te(events ...TransferExecutedEvent) []TransferExecutedEvent   { return events }

// ============ FIFO Queue Tests ============

func TestFIFOQueueOperations(t *testing.T) {
	t.Run("addToQueue", func(t *testing.T) {
		t.Run("should add entry to empty queue", func(t *testing.T) {
			q := Queue{}
			AddToQueue(&q, big.NewInt(100), m.Now)
			wantLen(t, "queue", q, 1)
			wantInt(t, "queue[0].amount", q[0].Amount, 100)
			if q[0].OriginalTimestamp != m.Now {
				t.Fatalf("timestamp = %d", q[0].OriginalTimestamp)
			}
		})
		t.Run("should add multiple entries preserving order", func(t *testing.T) {
			q := Queue{}
			AddToQueue(&q, big.NewInt(100), m.HourAgo)
			AddToQueue(&q, big.NewInt(200), m.Now)
			wantLen(t, "queue", q, 2)
			wantInt(t, "queue[0].amount", q[0].Amount, 100)
			wantInt(t, "queue[1].amount", q[1].Amount, 200)
			if q[0].OriginalTimestamp != m.HourAgo || q[1].OriginalTimestamp != m.Now {
				t.Fatalf("timestamps = %d, %d", q[0].OriginalTimestamp, q[1].OriginalTimestamp)
			}
		})
		t.Run("should not add zero or negative amounts", func(t *testing.T) {
			q := Queue{}
			AddToQueue(&q, big.NewInt(0), m.Now)
			AddToQueue(&q, big.NewInt(-100), m.Now)
			wantLen(t, "queue", q, 0)
		})
	})

	t.Run("consumeFromQueue", func(t *testing.T) {
		t.Run("should consume from oldest entry first (FIFO)", func(t *testing.T) {
			q := entries(100, m.TwoDaysAgo, 200, m.HourAgo, 300, m.Now)
			consumed, remaining := ConsumeFromQueue(&q, big.NewInt(150), m.Now, windowDuration)
			wantLen(t, "consumed", consumed, 1)
			wantInt(t, "consumed[0].amount", consumed[0].Amount, 150)
			if consumed[0].OriginalTimestamp != m.HourAgo {
				t.Fatalf("consumed timestamp = %d", consumed[0].OriginalTimestamp)
			}
			wantInt(t, "remaining", remaining, 0)
			wantLen(t, "queue", q, 2)
			wantInt(t, "queue[0].amount", q[0].Amount, 50)
			if q[0].OriginalTimestamp != m.HourAgo {
				t.Fatalf("queue[0] timestamp = %d", q[0].OriginalTimestamp)
			}
			wantInt(t, "queue[1].amount", q[1].Amount, 300)
		})
		t.Run("should consume from multiple entries when needed", func(t *testing.T) {
			q := entries(100, m.HourAgo, 200, m.Now)
			consumed, remaining := ConsumeFromQueue(&q, big.NewInt(150), m.Now, windowDuration)
			wantLen(t, "consumed", consumed, 2)
			wantInt(t, "consumed[0].amount", consumed[0].Amount, 100)
			wantInt(t, "consumed[1].amount", consumed[1].Amount, 50)
			wantInt(t, "remaining", remaining, 0)
			wantLen(t, "queue", q, 1)
			wantInt(t, "queue[0].amount", q[0].Amount, 150)
		})
		t.Run("should skip expired entries", func(t *testing.T) {
			q := entries(100, m.TwoDaysAgo, 200, m.HourAgo)
			consumed, remaining := ConsumeFromQueue(&q, big.NewInt(50), m.Now, windowDuration)
			wantLen(t, "consumed", consumed, 1)
			wantInt(t, "consumed[0].amount", consumed[0].Amount, 50)
			if consumed[0].OriginalTimestamp != m.HourAgo {
				t.Fatalf("consumed timestamp = %d", consumed[0].OriginalTimestamp)
			}
			wantInt(t, "remaining", remaining, 0)
		})
		t.Run("should return remaining when queue is exhausted", func(t *testing.T) {
			q := entries(100, m.HourAgo)
			consumed, remaining := ConsumeFromQueue(&q, big.NewInt(150), m.Now, windowDuration)
			wantLen(t, "consumed", consumed, 1)
			wantInt(t, "consumed[0].amount", consumed[0].Amount, 100)
			wantInt(t, "remaining", remaining, 50)
			wantLen(t, "queue", q, 0)
		})
		t.Run("should handle partial consumption of entry", func(t *testing.T) {
			q := entries(100, m.HourAgo)
			consumed, remaining := ConsumeFromQueue(&q, big.NewInt(30), m.Now, windowDuration)
			wantLen(t, "consumed", consumed, 1)
			wantInt(t, "consumed[0].amount", consumed[0].Amount, 30)
			wantInt(t, "remaining", remaining, 0)
			wantLen(t, "queue", q, 1)
			wantInt(t, "queue[0].amount", q[0].Amount, 70)
		})
		t.Run("should handle empty queue", func(t *testing.T) {
			q := Queue{}
			consumed, remaining := ConsumeFromQueue(&q, big.NewInt(100), m.Now, windowDuration)
			wantLen(t, "consumed", consumed, 0)
			wantInt(t, "remaining", remaining, 100)
		})
	})

	t.Run("getValidQueueBalance", func(t *testing.T) {
		t.Run("should sum only non-expired entries", func(t *testing.T) {
			q := entries(100, m.TwoDaysAgo, 200, m.HourAgo, 300, m.Now)
			wantInt(t, "balance", ValidQueueBalance(q, m.Now, windowDuration), 500)
		})
		t.Run("should return 0 for empty queue", func(t *testing.T) {
			wantInt(t, "balance", ValidQueueBalance(Queue{}, m.Now, windowDuration), 0)
		})
		t.Run("should return 0 when all entries expired", func(t *testing.T) {
			q := entries(100, m.TwoDaysAgo, 200, m.TwoDaysAgo-3600)
			wantInt(t, "balance", ValidQueueBalance(q, m.Now, windowDuration), 0)
		})
	})

	t.Run("pruneExpiredEntries", func(t *testing.T) {
		t.Run("should remove expired entries from front of queue", func(t *testing.T) {
			q := entries(100, m.TwoDaysAgo, 200, m.TwoDaysAgo-3600, 300, m.HourAgo, 400, m.Now)
			PruneExpiredEntries(&q, m.Now, windowDuration)
			wantLen(t, "queue", q, 2)
			wantInt(t, "queue[0].amount", q[0].Amount, 300)
			wantInt(t, "queue[1].amount", q[1].Amount, 400)
		})
		t.Run("should handle queue with no expired entries", func(t *testing.T) {
			q := entries(100, m.HourAgo, 200, m.Now)
			PruneExpiredEntries(&q, m.Now, windowDuration)
			wantLen(t, "queue", q, 2)
		})
		t.Run("should handle empty queue", func(t *testing.T) {
			q := Queue{}
			PruneExpiredEntries(&q, m.Now, windowDuration)
			wantLen(t, "queue", q, 0)
		})
		t.Run("should remove expired entries from unsorted queue (middle/end)", func(t *testing.T) {
			q := entries(100, m.HourAgo, 200, m.TwoDaysAgo, 300, m.Now, 400, m.TwoDaysAgo-3600, 500, m.HourAgo-60)
			PruneExpiredEntries(&q, m.Now, windowDuration)
			wantLen(t, "queue", q, 3)
			wantInt(t, "queue[0].amount", q[0].Amount, 100)
			wantInt(t, "queue[1].amount", q[1].Amount, 300)
			wantInt(t, "queue[2].amount", q[2].Amount, 500)
		})
		t.Run("should handle queue where all entries are expired", func(t *testing.T) {
			q := entries(100, m.TwoDaysAgo, 200, m.TwoDaysAgo-3600)
			PruneExpiredEntries(&q, m.Now, windowDuration)
			wantLen(t, "queue", q, 0)
		})
	})
}

// ============ State Building Tests ============

func TestBuildSubAccountState(t *testing.T) {
	t.Run("SWAP operations", func(t *testing.T) {
		t.Run("should mark output tokens as newly acquired for non-acquired input", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType = contracts.OpSwap
				e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
				e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(50)
				e.Timestamp = m.HourAgo
			})), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.WETH, 50)
		})
		t.Run("should inherit timestamp when swapping acquired tokens", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(50)
					e.Timestamp = m.HourAgo
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(50)
					e.TokensOut, e.AmountsOut = addrs(m.USDC), ints(100)
					e.Timestamp, e.BlockNumber, e.LogIndex = m.Now, 1001, 1
				}),
			), nil, m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.WETH)
			wantBalance(t, s, m.USDC, 100)
			if q := queueOf(t, s, m.USDC); q[0].OriginalTimestamp != m.HourAgo {
				t.Fatalf("USDC inherited timestamp = %d, want %d", q[0].OriginalTimestamp, m.HourAgo)
			}
		})
		t.Run("should track spending cost for SWAP", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.SpendingCost = big.NewInt(100)
				e.Timestamp = m.HourAgo
			})), nil, m.Sub, m.Now, nil)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 100)
			wantLen(t, "spendingRecords", s.SpendingRecords, 1)
		})
		t.Run("should not track spending outside window", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.SpendingCost = big.NewInt(100)
				e.Timestamp = m.TwoDaysAgo
			})), nil, m.Sub, m.Now, nil)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 0)
		})
	})

	t.Run("DEPOSIT operations", func(t *testing.T) {
		t.Run("should track deposit record with input and output tokens", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpDeposit, m.Aave
				e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
				e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
				e.SpendingCost, e.Timestamp = big.NewInt(10), m.HourAgo
			})), nil, m.Sub, m.Now, nil)
			wantLen(t, "depositRecords", s.DepositRecords, 1)
			d := s.DepositRecords[0]
			if d.TokenIn != m.LINK || !d.HasTokenOut || d.TokenOut != m.ALINK {
				t.Fatalf("deposit tokens = %s -> %s (has=%v)", d.TokenIn.Hex(), d.TokenOut.Hex(), d.HasTokenOut)
			}
			wantInt(t, "amountIn", d.AmountIn, 100)
			wantInt(t, "amountOut", d.AmountOut, 100)
			wantInt(t, "remainingAmount", d.RemainingAmount, 100)
			wantInt(t, "remainingOutputAmount", d.RemainingOutputAmount, 100)
			wantBalance(t, s, m.ALINK, 100)
		})
		t.Run("should inherit timestamp from acquired input for deposit output", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.Timestamp, e.BlockNumber = m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1001
				}),
			), nil, m.Sub, m.Now, nil)
			if q := queueOf(t, s, m.ALINK); q[0].OriginalTimestamp != m.HourAgo {
				t.Fatalf("aLINK timestamp = %d", q[0].OriginalTimestamp)
			}
			if got := s.DepositRecords[0].OriginalAcquisitionTimestamp; got != m.HourAgo {
				t.Fatalf("originalAcquisitionTimestamp = %d", got)
			}
		})
	})

	t.Run("WITHDRAW operations", func(t *testing.T) {
		t.Run("should match withdraw to deposit and inherit original timestamp", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(10), m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.HourAgo+100, 1001
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1002
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 100)
			if q := queueOf(t, s, m.LINK); q[0].OriginalTimestamp != m.HourAgo {
				t.Fatalf("LINK timestamp = %d", q[0].OriginalTimestamp)
			}
		})
		t.Run("should consume deposit output tokens (aTokens) on withdraw", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
					e.Timestamp, e.BlockNumber = m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.Timestamp, e.BlockNumber = m.Now, 1001
				}),
			), nil, m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.ALINK)
			wantBalance(t, s, m.LINK, 100)
		})
		t.Run("should NOT mark unmatched withdraw as acquired", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpWithdraw, m.Aave
				e.TokensIn, e.AmountsIn = addrs(), ints()
				e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
				e.Timestamp = m.Now
			})), nil, m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.LINK)
		})
		t.Run("should handle partial withdraw matching", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
					e.Timestamp, e.BlockNumber = m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(50)
					e.Timestamp, e.BlockNumber = m.Now, 1001
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 50)
			wantBalance(t, s, m.ALINK, 50)
			wantInt(t, "remainingAmount", s.DepositRecords[0].RemainingAmount, 50)
		})
	})

	t.Run("CLAIM operations", func(t *testing.T) {
		deposit := mProtocol(func(e *ProtocolExecutionEvent) {
			e.OpType, e.Target = contracts.OpDeposit, m.Aave
			e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
			e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
			e.Timestamp, e.BlockNumber = m.HourAgo, 1000
		})
		t.Run("should mark CLAIM as acquired if matching deposit exists", func(t *testing.T) {
			s := build(pe(deposit, mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpClaim, m.Aave
				e.TokensIn, e.AmountsIn = addrs(), ints()
				e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(10)
				e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1001
			})), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.WETH, 10)
		})
		t.Run("should NOT mark CLAIM as acquired if no matching deposit", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpClaim, m.Aave
				e.TokensIn, e.AmountsIn = addrs(), ints()
				e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(10)
				e.Timestamp = m.Now
			})), nil, m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.WETH)
		})
		t.Run("should inherit oldest deposit timestamp for CLAIM", func(t *testing.T) {
			s := build(pe(deposit, mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpClaim, m.Aave
				e.TokensIn, e.AmountsIn = addrs(), ints()
				e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(10)
				e.Timestamp, e.BlockNumber = m.Now, 1001
			})), nil, m.Sub, m.Now, nil)
			if q := queueOf(t, s, m.WETH); q[0].OriginalTimestamp != m.HourAgo {
				t.Fatalf("WETH timestamp = %d", q[0].OriginalTimestamp)
			}
		})
	})

	t.Run("Transfer operations", func(t *testing.T) {
		t.Run("should consume acquired tokens on transfer", func(t *testing.T) {
			s := build(
				pe(mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.Timestamp, e.BlockNumber = m.HourAgo, 1000
				})),
				te(mTransfer(func(e *TransferExecutedEvent) {
					e.Token, e.Amount, e.SpendingCost = m.LINK, big.NewInt(30), big.NewInt(5)
					e.Timestamp, e.BlockNumber = m.Now, 1001
				})),
				m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 70)
		})
		t.Run("should track spending cost for transfers", func(t *testing.T) {
			s := build(nil, te(mTransfer(func(e *TransferExecutedEvent) {
				e.Token, e.Amount, e.SpendingCost = m.LINK, big.NewInt(50), big.NewInt(100)
				e.Timestamp = m.HourAgo
			})), m.Sub, m.Now, nil)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 100)
		})
	})

	t.Run("Multi-token operations", func(t *testing.T) {
		t.Run("should handle multi-token DEPOSIT (LP positions)", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpDeposit, m.Uniswap
				e.TokensIn, e.AmountsIn = addrs(m.LINK, m.WETH), ints(100, 50)
				e.TokensOut, e.AmountsOut = addrs(m.USDC), ints(1000)
				e.Timestamp = m.HourAgo
			})), nil, m.Sub, m.Now, nil)
			wantLen(t, "depositRecords", s.DepositRecords, 2)
			wantBalance(t, s, m.USDC, 1000)
		})
		t.Run("should handle multi-token output (WITHDRAW from LP)", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Uniswap
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.USDC), ints(1000)
					e.Timestamp, e.BlockNumber = m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Uniswap
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK, m.WETH), ints(100, 50)
					e.Timestamp, e.BlockNumber = m.Now, 1001
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 100)
			wantNoBalance(t, s, m.WETH)
		})
	})

	t.Run("Expiry handling", func(t *testing.T) {
		t.Run("should not count expired acquired tokens in balance", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
				e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
				e.Timestamp, e.BlockNumber = m.TwoDaysAgo, 1000
			})), nil, m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.LINK)
		})
		t.Run("should handle mix of expired and valid acquired tokens", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.Timestamp, e.BlockNumber = m.TwoDaysAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.USDC), ints(50)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(50)
					e.Timestamp, e.BlockNumber = m.HourAgo, 1001
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 50)
		})
	})

	t.Run("Chronological ordering", func(t *testing.T) {
		t.Run("should process events in timestamp order", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(50)
					e.TokensOut, e.AmountsOut = addrs(m.USDC), ints(50)
					e.Timestamp, e.BlockNumber, e.LogIndex = m.Now, 1001, 1
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.Timestamp, e.BlockNumber, e.LogIndex = m.HourAgo, 1000, 0
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 50)
			wantBalance(t, s, m.USDC, 50)
		})
		t.Run("should use log index for same-block ordering", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(50)
					e.TokensOut, e.AmountsOut = addrs(m.USDC), ints(50)
					e.Timestamp, e.BlockNumber, e.LogIndex = m.HourAgo, 1000, 1
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.Timestamp, e.BlockNumber, e.LogIndex = m.HourAgo, 1000, 0
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 50)
			wantBalance(t, s, m.USDC, 50)
		})
	})

	t.Run("Edge cases", func(t *testing.T) {
		t.Run("should handle empty events", func(t *testing.T) {
			s := build(nil, nil, m.Sub, m.Now, nil)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 0)
			if s.AcquiredBalances.Len() != 0 {
				t.Fatalf("acquiredBalances.size = %d", s.AcquiredBalances.Len())
			}
			wantLen(t, "depositRecords", s.DepositRecords, 0)
		})
		t.Run("should filter events by subaccount", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.SubAccount = m.Sub
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.SubAccount = m.Other
					e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(200)
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 100)
			wantNoBalance(t, s, m.WETH)
		})
		t.Run("should handle zero amounts gracefully", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(0)
				e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(0)
			})), nil, m.Sub, m.Now, nil)
			if s.AcquiredBalances.Len() != 0 {
				t.Fatalf("acquiredBalances.size = %d", s.AcquiredBalances.Len())
			}
		})
	})
}

// ============ Mixed Acquisition Tests ============

func TestMixedAcquisitionScenarios(t *testing.T) {
	t.Run("should proportionally split output when mixing acquired and non-acquired input", func(t *testing.T) {
		s := build(pe(
			mProtocol(func(e *ProtocolExecutionEvent) {
				e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
				e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(50)
				e.Timestamp, e.BlockNumber = m.HourAgo, 1000
			}),
			mProtocol(func(e *ProtocolExecutionEvent) {
				e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
				e.TokensOut, e.AmountsOut = addrs(m.USDC), ints(200)
				e.Timestamp, e.BlockNumber = m.Now, 1001
			}),
		), nil, m.Sub, m.Now, nil)
		q := queueOf(t, s, m.USDC)
		wantLen(t, "usdcQueue", q, 2)
		wantBalance(t, s, m.USDC, 200)
		// Tightened: 50% inherits HOUR_AGO, 50% is newly acquired at NOW.
		if q[0].Amount.Int64() != 100 || q[0].OriginalTimestamp != m.HourAgo || q[1].Amount.Int64() != 100 || q[1].OriginalTimestamp != m.Now {
			t.Fatalf("usdcQueue = [%s@%d, %s@%d]", q[0].Amount, q[0].OriginalTimestamp, q[1].Amount, q[1].OriginalTimestamp)
		}
	})
}

// ============ Complex Scenario Tests ============

func TestComplexRealWorldScenarios(t *testing.T) {
	t.Run("DeFi yield farming scenario", func(t *testing.T) {
		t.Run("should track full deposit -> earn rewards -> withdraw cycle", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(5)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(50), m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.HourAgo+60, 1001
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpClaim, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(5)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.HourAgo+120, 1002
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(105)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1003
				}),
			), nil, m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.ALINK)
			wantBalance(t, s, m.LINK, 100)
			wantBalance(t, s, m.WETH, 5)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 50)
		})
	})

	t.Run("Multiple deposits and partial withdrawals", func(t *testing.T) {
		t.Run("should handle FIFO matching for multiple deposits", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(50)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(50)
					e.Timestamp, e.BlockNumber = m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(75)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(75)
					e.Timestamp, e.BlockNumber = m.HourAgo+60, 1001
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(80)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1002
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 80)
			wantBalance(t, s, m.ALINK, 45)
			wantInt(t, "depositRecords[0].remainingAmount", s.DepositRecords[0].RemainingAmount, 0)
			wantInt(t, "depositRecords[1].remainingAmount", s.DepositRecords[1].RemainingAmount, 45)
		})
		t.Run("should handle withdrawal when output tokens have expired", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.ALINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.TwoDaysAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(50)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1001
				}),
			), nil, m.Sub, m.Now, nil)
			wantInt(t, "remainingAmount", s.DepositRecords[0].RemainingAmount, 50)
			// Reduced only by actual queue consumption (0: the aLINK expired).
			wantInt(t, "remainingOutputAmount", s.DepositRecords[0].RemainingOutputAmount, 100)
			wantInt(t, "LINK balance ?? 0", balanceOrZero(s, m.LINK), 0)
			wantInt(t, "aLINK balance ?? 0", balanceOrZero(s, m.ALINK), 0)
		})
		t.Run("should handle multi-token LP deposits without double-counting output", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpDeposit, m.Uniswap
				e.TokensIn, e.AmountsIn = addrs(m.USDC, m.WETH), ints(1000, 1)
				e.TokensOut, e.AmountsOut = addrs(m.LP), ints(500)
				e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(100), m.HourAgo, 1000
			})), nil, m.Sub, m.Now, nil)
			wantLen(t, "depositRecords", s.DepositRecords, 2)
			usdc, weth := depositByTokenIn(t, s, m.USDC), depositByTokenIn(t, s, m.WETH)
			wantInt(t, "usdc.amountOut", usdc.AmountOut, 250)
			wantInt(t, "weth.amountOut", weth.AmountOut, 250)
			wantInt(t, "usdc.remainingOutputAmount", usdc.RemainingOutputAmount, 250)
			wantInt(t, "weth.remainingOutputAmount", weth.RemainingOutputAmount, 250)
			total := bn.Zero()
			for _, d := range s.DepositRecords {
				total = bn.Add(total, d.RemainingOutputAmount)
			}
			wantInt(t, "total remainingOutputAmount", total, 500)
			wantBalance(t, s, m.LP, 500)
		})
		t.Run("should correctly consume LP tokens on withdrawal after multi-token deposit", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Uniswap
					e.TokensIn, e.AmountsIn = addrs(m.USDC, m.WETH), ints(1000, 1)
					e.TokensOut, e.AmountsOut = addrs(m.LP), ints(500)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(100), m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Uniswap
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.USDC), ints(1000)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now-60, 1001
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Uniswap
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(1)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1002
				}),
			), nil, m.Sub, m.Now, nil)
			wantInt(t, "LP balance ?? 0", balanceOrZero(s, m.LP), 0)
			for i, d := range s.DepositRecords {
				if d.RemainingAmount.Sign() != 0 || d.RemainingOutputAmount.Sign() != 0 {
					t.Fatalf("deposit %d remaining = %s / %s", i, d.RemainingAmount, d.RemainingOutputAmount)
				}
			}
			wantBalance(t, s, m.USDC, 1000)
			wantBalance(t, s, m.WETH, 1)
		})
		t.Run("should handle single-input multi-output deposits with separate deposit records", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.OpType, e.Target = contracts.OpDeposit, m.Aave
				e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
				e.TokensOut, e.AmountsOut = addrs(m.ReceiptA, m.ReceiptB), ints(50, 30)
				e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(10), m.HourAgo, 1000
			})), nil, m.Sub, m.Now, nil)
			wantLen(t, "depositRecords", s.DepositRecords, 2)
			a, b := depositByTokenOut(t, s, m.ReceiptA), depositByTokenOut(t, s, m.ReceiptB)
			wantInt(t, "a.amountIn", a.AmountIn, 50)
			wantInt(t, "b.amountIn", b.AmountIn, 50)
			wantInt(t, "a.remainingAmount", a.RemainingAmount, 50)
			wantInt(t, "b.remainingAmount", b.RemainingAmount, 50)
			wantInt(t, "a.amountOut", a.AmountOut, 50)
			wantInt(t, "b.amountOut", b.AmountOut, 30)
			wantInt(t, "a.remainingOutputAmount", a.RemainingOutputAmount, 50)
			wantInt(t, "b.remainingOutputAmount", b.RemainingOutputAmount, 30)
			wantBalance(t, s, m.ReceiptA, 50)
			wantBalance(t, s, m.ReceiptB, 30)
		})
		t.Run("should correctly match withdrawals to single-input multi-output deposit records", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpDeposit, m.Aave
					e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.ReceiptA, m.ReceiptB), ints(50, 30)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(10), m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.OpType, e.Target = contracts.OpWithdraw, m.Aave
					e.TokensIn, e.AmountsIn = addrs(), ints()
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(50)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now-60, 1001
				}),
			), nil, m.Sub, m.Now, nil)
			wantInt(t, "recordA.remainingAmount", depositByTokenOut(t, s, m.ReceiptA).RemainingAmount, 0)
			wantInt(t, "recordB.remainingAmount", depositByTokenOut(t, s, m.ReceiptB).RemainingAmount, 50)
			wantBalance(t, s, m.LINK, 50)
		})
	})

	t.Run("USD-weighted ratio calculation", func(t *testing.T) {
		acquireWETH := mProtocol(func(e *ProtocolExecutionEvent) {
			e.TokensIn, e.AmountsIn = addrs(m.LINK), ints(100)
			e.TokensOut, e.AmountsOut = addrs(m.WETH), bigs(units(1, 18))
			e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.HourAgo, 1000
		})
		mixedSwap := mProtocol(func(e *ProtocolExecutionEvent) {
			e.TokensIn, e.AmountsIn = addrs(m.WETH, m.USDC), bigs(units(1, 18), units(1000, 6))
			e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(4000)
			e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(0), m.Now, 1001
		})

		t.Run("should use USD-weighted ratio for mixed multi-token swaps", func(t *testing.T) {
			cache := TokenPriceCache{
				m.WETH: {PriceUSD: units(3000, 18), Decimals: 18},
				m.USDC: {PriceUSD: units(1, 18), Decimals: 6},
			}
			s := build(pe(acquireWETH, mixedSwap), nil, m.Sub, m.Now, cache)
			wantInt(t, "LINK balance", balanceOrZero(s, m.LINK), 4000)
			q := queueOf(t, s, m.LINK)
			wantLen(t, "linkQueue", q, 2)
			wantInt(t, "linkQueue[0].amount", q[0].Amount, 3000)
			wantInt(t, "linkQueue[1].amount", q[1].Amount, 1000)
			if q[0].OriginalTimestamp != m.HourAgo || q[1].OriginalTimestamp != m.Now {
				t.Fatalf("timestamps = %d, %d", q[0].OriginalTimestamp, q[1].OriginalTimestamp)
			}
		})
		t.Run("should fall back to amount-weighted ratio when price cache is empty", func(t *testing.T) {
			s := build(pe(acquireWETH, mixedSwap), nil, m.Sub, m.Now, nil)
			q := queueOf(t, s, m.LINK)
			wantLen(t, "linkQueue", q, 2)
			// TS asserted only > 3900. Exact: ratio = 1e18*1e18/(1e18+1e9).
			wantInt(t, "linkQueue[0].amount", q[0].Amount, 3999)
			wantInt(t, "linkQueue[1].amount", q[1].Amount, 1)
			if q[0].OriginalTimestamp != m.HourAgo || q[1].OriginalTimestamp != m.Now {
				t.Fatalf("timestamps = %d, %d", q[0].OriginalTimestamp, q[1].OriginalTimestamp)
			}
		})
	})

	t.Run("Transfer scenarios", func(t *testing.T) {
		t.Run("should properly track transfers of acquired tokens", func(t *testing.T) {
			s := build(
				pe(mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(5)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(50), m.HourAgo, 1000
				})),
				te(
					mTransfer(func(e *TransferExecutedEvent) {
						e.Token, e.Amount, e.SpendingCost = m.LINK, big.NewInt(30), big.NewInt(10)
						e.Timestamp, e.BlockNumber = m.HourAgo+60, 1001
					}),
					mTransfer(func(e *TransferExecutedEvent) {
						e.Token, e.Amount, e.SpendingCost = m.LINK, big.NewInt(20), big.NewInt(5)
						e.Timestamp, e.BlockNumber = m.Now, 1002
					}),
				),
				m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 50)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 65)
		})
		t.Run("should handle transfer of non-acquired tokens", func(t *testing.T) {
			s := build(nil, te(mTransfer(func(e *TransferExecutedEvent) {
				e.Token, e.Amount, e.SpendingCost = m.LINK, big.NewInt(100), big.NewInt(50)
				e.Timestamp = m.Now
			})), m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.LINK)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 50)
		})
	})

	t.Run("Window boundary scenarios", func(t *testing.T) {
		t.Run("should handle events inside vs outside window", func(t *testing.T) {
			s := build(pe(
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
					e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(50)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(25), m.HourAgo, 1000
				}),
				mProtocol(func(e *ProtocolExecutionEvent) {
					e.TokensIn, e.AmountsIn = addrs(m.USDC), ints(100)
					e.TokensOut, e.AmountsOut = addrs(m.WETH), ints(1)
					e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(100), m.TwoDaysAgo, 999
				}),
			), nil, m.Sub, m.Now, nil)
			wantBalance(t, s, m.LINK, 50)
			wantNoBalance(t, s, m.WETH)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 25)
		})
		t.Run("should handle events far outside window", func(t *testing.T) {
			s := build(pe(mProtocol(func(e *ProtocolExecutionEvent) {
				e.TokensIn, e.AmountsIn = addrs(m.WETH), ints(1)
				e.TokensOut, e.AmountsOut = addrs(m.LINK), ints(100)
				e.SpendingCost, e.Timestamp, e.BlockNumber = big.NewInt(50), m.TwoDaysAgo, 1000
			})), nil, m.Sub, m.Now, nil)
			wantNoBalance(t, s, m.LINK)
			wantInt(t, "totalSpendingInWindow", s.TotalSpendingInWindow, 0)
		})
	})
}
