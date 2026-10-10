package spending

// Behaviours of the TS buildSubAccountState that its own test suite never
// pinned down but that the port must reproduce exactly. Each "want" string
// is the output of the real TS implementation for the same input, rendered
// in the same format (see dumpState).

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

var q = struct {
	Sub, Target, A, B, X, Y, Z, Zero common.Address
	Now, Hour                        int64
}{
	Sub:    addr("0x1234567890123456789012345678901234567890"),
	Target: addr("0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa"),
	A:      addr("0x1111111111111111111111111111111111111111"),
	B:      addr("0x2222222222222222222222222222222222222222"),
	X:      addr("0x3333333333333333333333333333333333333333"),
	Y:      addr("0x4444444444444444444444444444444444444444"),
	Z:      addr("0x5555555555555555555555555555555555555555"),
	Zero:   common.Address{},
	Now:    1700100000,
	Hour:   1700100000 - 3600,
}

func qEvent(op contracts.OperationType, override func(ev *ProtocolExecutionEvent)) ProtocolExecutionEvent {
	ev := ProtocolExecutionEvent{
		SubAccount: q.Sub, Target: q.Target, OpType: op,
		TokensIn: addrs(), AmountsIn: ints(), TokensOut: addrs(), AmountsOut: ints(),
		SpendingCost: big.NewInt(0), Timestamp: q.Hour, BlockNumber: 1000,
	}
	if override != nil {
		override(&ev)
	}
	return ev
}

// dumpState renders a state exactly like the TS helper used to produce the
// expected strings (address prefixes, H/N for HOUR_AGO/NOW timestamps).
func dumpState(s *SubAccountState) string {
	short := func(a common.Address) string { return strings.ToLower(a.Hex())[:4] }
	ts := func(t int64) string {
		switch t {
		case q.Hour:
			return "H"
		case q.Now:
			return "N"
		}
		return fmt.Sprint(t)
	}
	out := struct {
		Deposits []string `json:"deposits"`
		Balances []string `json:"balances"`
		Queues   []string `json:"queues"`
		Spending string   `json:"spending"`
	}{Deposits: []string{}, Balances: []string{}, Queues: []string{}, Spending: s.TotalSpendingInWindow.String()}
	for _, d := range s.DepositRecords {
		tokenOut := "0x"
		if d.HasTokenOut {
			tokenOut = short(d.TokenOut)
		}
		out.Deposits = append(out.Deposits, fmt.Sprintf("%s:%s/%s->%s:%s/%s@%s", short(d.TokenIn), d.AmountIn, d.RemainingAmount,
			tokenOut, d.AmountOut, d.RemainingOutputAmount, ts(d.OriginalAcquisitionTimestamp)))
	}
	for _, k := range s.AcquiredBalances.Keys() {
		v, _ := s.AcquiredBalances.Get(k)
		out.Balances = append(out.Balances, fmt.Sprintf("%s=%s", short(k), v))
	}
	for _, k := range s.AcquiredQueues.Keys() {
		qu, _ := s.AcquiredQueues.Get(k)
		parts := make([]string, len(*qu))
		for i, e := range *qu {
			parts[i] = fmt.Sprintf("%s@%s", e.Amount, ts(e.OriginalTimestamp))
		}
		out.Queues = append(out.Queues, fmt.Sprintf("%s:[%s]", short(k), strings.Join(parts, ",")))
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // JSON.stringify doesn't escape "->"
	_ = enc.Encode(out)
	return strings.TrimSuffix(b.String(), "\n")
}

func TestTSQuirks(t *testing.T) {
	cases := []struct {
		name   string
		events []ProtocolExecutionEvent
		cache  TokenPriceCache
		want   string
	}{
		{
			// amountsOut[1] is 0n (falsy), so the second deposit record pairs
			// tokensOut[1] with amountsOut[0].
			name: "zero amountOut falls back to amountsOut[0]",
			events: pe(qEvent(contracts.OpDeposit, func(ev *ProtocolExecutionEvent) {
				ev.TokensIn, ev.AmountsIn = addrs(q.A, q.B), ints(10, 20)
				ev.TokensOut, ev.AmountsOut = addrs(q.X, q.Y), ints(5, 0)
			})),
			want: `{"deposits":["0x11:10/10->0x33:5/5@H","0x22:20/20->0x44:5/5@H"],"balances":["0x33=5"],"queues":["0x11:[]","0x22:[]","0x33:[5@H]"],"spending":"0"}`,
		},
		{
			name: "deposit without output token skips the receipt burn",
			events: pe(
				qEvent(contracts.OpDeposit, func(ev *ProtocolExecutionEvent) {
					ev.TokensIn, ev.AmountsIn = addrs(q.A), ints(100)
				}),
				qEvent(contracts.OpWithdraw, func(ev *ProtocolExecutionEvent) {
					ev.TokensOut, ev.AmountsOut = addrs(q.A), ints(50)
					ev.Timestamp, ev.BlockNumber = q.Now, 1001
				}),
			),
			want: `{"deposits":["0x11:100/50->0x:0/0@H"],"balances":["0x11=50"],"queues":["0x11:[50@H]"],"spending":"0"}`,
		},
		{
			// A real zero-address output token is not "no output token".
			name: "zero-address output token is burned on withdrawal",
			events: pe(
				qEvent(contracts.OpDeposit, func(ev *ProtocolExecutionEvent) {
					ev.TokensIn, ev.AmountsIn = addrs(q.A), ints(100)
					ev.TokensOut, ev.AmountsOut = addrs(q.Zero), ints(100)
				}),
				qEvent(contracts.OpWithdraw, func(ev *ProtocolExecutionEvent) {
					ev.TokensOut, ev.AmountsOut = addrs(q.A), ints(50)
					ev.Timestamp, ev.BlockNumber = q.Now, 1001
				}),
			),
			want: `{"deposits":["0x11:100/50->0x00:100/50@H"],"balances":["0x11=50","0x00=50"],"queues":["0x11:[50@H]","0x00:[50@H]"],"spending":"0"}`,
		},
		{
			// Only X is priced: X gets the whole input, Y an equal third, and
			// Z (last) the negative remainder. Withdrawing 100 then matches
			// 90 + 10 = 100 although only 90 was deposited (pre-existing
			// over-attribution, kept for parity).
			name: "partially priced single-input multi-output deposit over-allocates",
			events: pe(
				qEvent(contracts.OpDeposit, func(ev *ProtocolExecutionEvent) {
					ev.TokensIn, ev.AmountsIn = addrs(q.A), ints(90)
					ev.TokensOut, ev.AmountsOut = addrs(q.X, q.Y, q.Z), ints(10, 10, 10)
				}),
				qEvent(contracts.OpWithdraw, func(ev *ProtocolExecutionEvent) {
					ev.TokensOut, ev.AmountsOut = addrs(q.A), ints(100)
					ev.Timestamp, ev.BlockNumber = q.Now, 1001
				}),
			),
			cache: TokenPriceCache{q.X: {PriceUSD: bn.Precision, Decimals: 0}},
			want:  `{"deposits":["0x11:90/0->0x33:10/0@H","0x11:30/20->0x44:10/7@H","0x11:-30/-30->0x55:10/10@H"],"balances":["0x11=100","0x44=7","0x55=10"],"queues":["0x11:[90@H,10@H]","0x33:[]","0x44:[7@H]","0x55:[10@H]"],"spending":"0"}`,
		},
		{
			name: "REPAY, APPROVE and UNKNOWN events are ignored",
			events: pe(
				qEvent(contracts.OpSwap, func(ev *ProtocolExecutionEvent) {
					ev.TokensIn, ev.AmountsIn = addrs(q.A), ints(1)
					ev.TokensOut, ev.AmountsOut = addrs(q.X), ints(100)
					ev.SpendingCost = big.NewInt(10)
				}),
				qEvent(contracts.OpRepay, func(ev *ProtocolExecutionEvent) {
					ev.TokensIn, ev.AmountsIn = addrs(q.X), ints(50)
					ev.SpendingCost, ev.BlockNumber = big.NewInt(7), 1001
				}),
				qEvent(contracts.OpApprove, func(ev *ProtocolExecutionEvent) {
					ev.TokensIn, ev.AmountsIn = addrs(q.X), ints(50)
					ev.SpendingCost, ev.BlockNumber = big.NewInt(3), 1002
				}),
				qEvent(contracts.OpUnknown, func(ev *ProtocolExecutionEvent) {
					ev.TokensOut, ev.AmountsOut = addrs(q.X), ints(9)
					ev.SpendingCost, ev.BlockNumber = big.NewInt(4), 1003
				}),
			),
			want: `{"deposits":[],"balances":["0x33=100"],"queues":["0x11:[]","0x33:[100@H]"],"spending":"10"}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := dumpState(build(c.events, nil, q.Sub, q.Now, c.cache))
			if got != c.want {
				t.Fatalf("\n got: %s\nwant: %s", got, c.want)
			}
		})
	}
}

// ============ Go-specific safety properties ============

func TestConsumedEntriesDoNotAliasTheQueue(t *testing.T) {
	queue := entries(100, q.Hour)
	consumed, _ := ConsumeFromQueue(&queue, big.NewInt(30), q.Now, windowDuration)
	consumed[0].Amount.SetInt64(999) // a caller mutating its copy...
	wantInt(t, "queue[0].amount", queue[0].Amount, 70)

	queue = entries(100, q.Hour)
	consumed, _ = ConsumeFromQueue(&queue, big.NewInt(100), q.Now, windowDuration)
	consumed[0].Amount.SetInt64(1)
	added := Queue{}
	AddToQueue(&added, consumed[0].Amount, q.Hour)
	consumed[0].Amount.SetInt64(2) // ...never changes what was stored
	wantInt(t, "added[0].amount", added[0].Amount, 1)
}

func TestBuildDoesNotMutateInputs(t *testing.T) {
	events := pe(
		qEvent(contracts.OpSwap, func(ev *ProtocolExecutionEvent) {
			ev.TokensIn, ev.AmountsIn = addrs(q.A), ints(10)
			ev.TokensOut, ev.AmountsOut = addrs(q.X), ints(100)
			ev.SpendingCost = big.NewInt(5)
		}),
		qEvent(contracts.OpDeposit, func(ev *ProtocolExecutionEvent) {
			ev.TokensIn, ev.AmountsIn = addrs(q.X, q.B), ints(60, 40)
			ev.TokensOut, ev.AmountsOut = addrs(q.Y), ints(50)
			ev.BlockNumber = 1001
		}),
		qEvent(contracts.OpWithdraw, func(ev *ProtocolExecutionEvent) {
			ev.TokensOut, ev.AmountsOut = addrs(q.X, q.B), ints(70, 40)
			ev.Timestamp, ev.BlockNumber = q.Now, 1002
		}),
	)
	transfers := te(TransferExecutedEvent{SubAccount: q.Sub, Token: q.X, Amount: big.NewInt(5), SpendingCost: big.NewInt(1), Timestamp: q.Now, BlockNumber: 1003})
	before := fmt.Sprintf("%v %v", eventsString(events), transfers[0].Amount)

	s := build(events, transfers, q.Sub, q.Now, nil)
	// Mutating the result must not reach back into the inputs either.
	for _, d := range s.DepositRecords {
		d.AmountIn.SetInt64(-1)
		d.AmountOut.SetInt64(-1)
	}
	for _, r := range s.SpendingRecords {
		r.Amount.SetInt64(-1)
	}
	if after := fmt.Sprintf("%v %v", eventsString(events), transfers[0].Amount); after != before {
		t.Fatalf("inputs changed:\nbefore %s\nafter  %s", before, after)
	}
}

func eventsString(events []ProtocolExecutionEvent) string {
	var b strings.Builder
	for _, ev := range events {
		fmt.Fprintf(&b, "%v%v%v%v%v;", ev.AmountsIn, ev.AmountsOut, ev.SpendingCost, ev.TokensIn, ev.TokensOut)
	}
	return b.String()
}

func TestOrderedMapKeepsInsertionOrder(t *testing.T) {
	om := NewOrderedMap[common.Address, int]()
	om.Set(q.Z, 1)
	om.Set(q.A, 2)
	om.Set(q.Z, 3) // existing key keeps its position
	keys := om.Keys()
	if len(keys) != 2 || keys[0] != q.Z || keys[1] != q.A || om.Len() != 2 {
		t.Fatalf("keys = %v", keys)
	}
	if v, _ := om.Get(q.Z); v != 3 {
		t.Fatalf("value = %d", v)
	}
	keys[0] = q.B // Keys returns a copy
	if om.Keys()[0] != q.Z {
		t.Fatal("Keys must return a copy")
	}
}

func TestTokenValueUSD(t *testing.T) {
	cache := TokenPriceCache{
		q.X: {PriceUSD: units(3000, 18), Decimals: 18},
		q.Y: {PriceUSD: big.NewInt(0), Decimals: 6},
		// The TS computed BigInt(10 ** 24) = 999999999999999983222784n here;
		// the port divides by the exact power of ten (deliberate deviation).
		q.Z: {PriceUSD: units(2, 18), Decimals: 24},
	}
	if v, ok := TokenValueUSD(q.X, units(2, 18), cache); !ok || v.Cmp(units(6000, 18)) != 0 {
		t.Fatalf("X value = %v, %v", v, ok)
	}
	if _, ok := TokenValueUSD(q.Y, big.NewInt(5), cache); ok {
		t.Fatal("zero price must be 'no price'")
	}
	if _, ok := TokenValueUSD(q.A, big.NewInt(5), cache); ok {
		t.Fatal("unknown token must be 'no price'")
	}
	if v, ok := TokenValueUSD(q.Z, units(3, 24), cache); !ok || v.Cmp(units(6, 18)) != 0 {
		t.Fatalf("Z value = %v, %v (want exactly 6e18)", v, ok)
	}
}

func TestMalformedEventsPanicOnlyWhenProcessed(t *testing.T) {
	bad := func(op contracts.OperationType) []ProtocolExecutionEvent {
		return pe(qEvent(op, func(ev *ProtocolExecutionEvent) {
			ev.TokensIn, ev.AmountsIn = addrs(q.A, q.B), ints(1)
		}))
	}
	for _, op := range []contracts.OperationType{contracts.OpSwap, contracts.OpDeposit, contracts.OpWithdraw, contracts.OpClaim} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s with mismatched arrays should panic", op)
				}
			}()
			build(bad(op), nil, q.Sub, q.Now, nil)
		}()
	}
	// Ignored op types never touch the arrays (as in the TS).
	for _, op := range []contracts.OperationType{contracts.OpApprove, contracts.OpRepay, contracts.OpUnknown} {
		build(bad(op), nil, q.Sub, q.Now, nil)
	}
}
