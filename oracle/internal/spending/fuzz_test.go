package spending

import (
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

// FuzzBuildSubAccountState checks the bookkeeping invariants of
// BuildSubAccountState on arbitrary well-formed event streams. Parity with
// the TS is the golden vectors' job; this looks for panics, aliasing and
// accounting bugs on inputs the generator never produced.
//
//	go test -run='^$' -fuzz=FuzzBuildSubAccountState -fuzztime=2m ./internal/spending
//
// (`make fuzz`). A plain `go test` runs the seed corpus only.
func FuzzBuildSubAccountState(f *testing.F) {
	f.Add([]byte{})
	// Deterministic pseudo-random seeds of increasing length (xorshift32).
	x := uint32(2463534242)
	for n := range 48 {
		seed := make([]byte, 16+12*n)
		for i := range seed {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			seed[i] = byte(x)
		}
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		in := decodeFuzzInput(data)
		inputs := in.String()

		state := in.build()
		if in.String() != inputs {
			t.Fatal("BuildSubAccountState modified its inputs")
		}
		got := in.render(state)
		if again := in.render(in.build()); again != got {
			t.Fatalf("not deterministic:\n%s\n%s", got, again)
		}
		checkStateInvariants(t, state, in.now, in.window)

		// Other subaccounts' events never matter.
		if own := in.render(in.ownEventsOnly().build()); own != got {
			t.Fatalf("other subaccounts' events changed the state:\n%s\n%s", got, own)
		}
		// With unique (timestamp, block, logIndex) keys, the input order
		// never matters.
		if in.uniqueKeys() {
			if rev := in.render(in.reversed().build()); rev != got {
				t.Fatalf("input order changed the state:\n%s\n%s", got, rev)
			}
		}

		// The state must not share big.Ints with the inputs.
		scribble(state)
		if in.String() != inputs {
			t.Fatal("the state aliases the inputs")
		}
	})
}

var (
	fuzzSub      = addr("0x1234567890123456789012345678901234567890")
	fuzzOtherSub = addr("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	fuzzTargets  = []common.Address{
		addr("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		addr("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}
	// The zero address is a token like any other here.
	fuzzTokens = []common.Address{
		{},
		addr("0x1111111111111111111111111111111111111111"),
		addr("0x2222222222222222222222222222222222222222"),
		addr("0x3333333333333333333333333333333333333333"),
	}
	maxUint256 = bn.Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
)

// fuzzReader turns fuzz bytes into choices, reading zeros once exhausted.
type fuzzReader struct{ data []byte }

func (r *fuzzReader) next() byte {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[0]
	r.data = r.data[1:]
	return b
}

func (r *fuzzReader) intn(n int) int { return int(r.next()) % n }

func (r *fuzzReader) amount() *big.Int {
	switch r.intn(8) {
	case 0:
		return new(big.Int)
	case 1, 2:
		return big.NewInt(int64(r.next()) + 1)
	case 3, 4:
		return bn.Mul(big.NewInt(int64(r.next())+1), bn.Pow10(uint(6*r.intn(4))))
	case 5:
		b := make([]byte, 1+r.intn(32))
		for i := range b {
			b[i] = r.next()
		}
		return new(big.Int).SetBytes(b)
	case 6:
		return bn.Sub(maxUint256, big.NewInt(int64(r.next())))
	default:
		// Small round amounts, so consumption often matches exactly.
		return big.NewInt(int64(r.intn(5)) * 25)
	}
}

type fuzzInput struct {
	events      []ProtocolExecutionEvent
	transfers   []TransferExecutedEvent
	now, window int64
	cache       TokenPriceCache
}

func decodeFuzzInput(data []byte) *fuzzInput {
	r := &fuzzReader{data: data}
	in := &fuzzInput{now: 1_700_000_000, window: []int64{3600, 86400, 7777}[r.intn(3)]}
	if mode := r.intn(3); mode > 0 { // 0: no cache, 1: some tokens priced, 2: all
		in.cache = TokenPriceCache{}
		for _, token := range fuzzTokens {
			if mode == 1 && r.intn(2) == 0 {
				continue
			}
			in.cache[token] = TokenPriceInfo{PriceUSD: r.amount(), Decimals: uint8(r.intn(40))}
		}
	}
	token := func() common.Address { return fuzzTokens[r.intn(len(fuzzTokens))] }

	for len(r.data) > 0 && len(in.events)+len(in.transfers) < 64 {
		sub := fuzzSub
		if r.intn(8) == 0 {
			sub = fuzzOtherSub
		}
		// From ~4 windows back to 1/8 window ahead of now, on a grid that
		// often lands exactly on an expiry boundary.
		ts := in.now - int64(r.intn(256)-8)*in.window/64 + int64(r.intn(3)) - 1
		block, logIndex := int64(r.intn(32)), int64(r.intn(4))

		if r.intn(6) == 0 {
			in.transfers = append(in.transfers, TransferExecutedEvent{
				SubAccount: sub, Token: token(), Recipient: fuzzTargets[0],
				Amount: r.amount(), SpendingCost: r.amount(),
				Timestamp: ts, BlockNumber: block, LogIndex: logIndex,
			})
			continue
		}
		ev := ProtocolExecutionEvent{
			SubAccount: sub, Target: fuzzTargets[r.intn(len(fuzzTargets))],
			OpType:   contracts.OperationType(r.intn(7)),
			TokensIn: []common.Address{}, AmountsIn: []*big.Int{},
			TokensOut: []common.Address{}, AmountsOut: []*big.Int{},
			SpendingCost: r.amount(),
			Timestamp:    ts, BlockNumber: block, LogIndex: logIndex,
		}
		for n := r.intn(4); n > 0; n-- {
			ev.TokensIn = append(ev.TokensIn, token())
			ev.AmountsIn = append(ev.AmountsIn, r.amount())
		}
		for n := r.intn(4); n > 0; n-- {
			ev.TokensOut = append(ev.TokensOut, token())
			ev.AmountsOut = append(ev.AmountsOut, r.amount())
		}
		in.events = append(in.events, ev)
	}
	return in
}

func (in *fuzzInput) build() *SubAccountState {
	return BuildSubAccountState(in.events, in.transfers, fuzzSub, in.now, in.window, in.cache, sprintfLogf)
}

// String renders every input value (fmt prints big.Ints by value).
func (in *fuzzInput) String() string {
	return fmt.Sprintf("%v|%v|%v", in.events, in.transfers, in.cache)
}

func (in *fuzzInput) render(s *SubAccountState) string {
	b, err := json.Marshal(renderState(s, func(a common.Address) any { return a.Hex() }))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func (in *fuzzInput) ownEventsOnly() *fuzzInput {
	out := *in
	out.events = slices.DeleteFunc(slices.Clone(in.events), func(e ProtocolExecutionEvent) bool { return e.SubAccount != fuzzSub })
	out.transfers = slices.DeleteFunc(slices.Clone(in.transfers), func(e TransferExecutedEvent) bool { return e.SubAccount != fuzzSub })
	return &out
}

func (in *fuzzInput) reversed() *fuzzInput {
	out := *in
	out.events = slices.Clone(in.events)
	slices.Reverse(out.events)
	out.transfers = slices.Clone(in.transfers)
	slices.Reverse(out.transfers)
	return &out
}

func (in *fuzzInput) uniqueKeys() bool {
	type key struct{ ts, block, logIndex int64 }
	seen := map[key]bool{}
	add := func(k key) bool {
		if seen[k] {
			return false
		}
		seen[k] = true
		return true
	}
	for _, e := range in.events {
		if !add(key{e.Timestamp, e.BlockNumber, e.LogIndex}) {
			return false
		}
	}
	for _, e := range in.transfers {
		if !add(key{e.Timestamp, e.BlockNumber, e.LogIndex}) {
			return false
		}
	}
	return true
}

func checkStateInvariants(t *testing.T, s *SubAccountState, now, window int64) {
	t.Helper()
	windowStart := now - window

	total := new(big.Int)
	for _, r := range s.SpendingRecords {
		if r.Amount.Sign() <= 0 || r.Timestamp < windowStart {
			t.Fatalf("spending record %s@%d is not positive or not in the window", r.Amount, r.Timestamp)
		}
		total.Add(total, r.Amount)
	}
	if total.Cmp(s.TotalSpendingInWindow) != 0 {
		t.Fatalf("totalSpendingInWindow = %s, records sum to %s", s.TotalSpendingInWindow, total)
	}

	for _, token := range s.AcquiredQueues.Keys() {
		q, _ := s.AcquiredQueues.Get(token)
		sum := new(big.Int)
		for _, e := range *q {
			if e.Amount.Sign() <= 0 {
				t.Fatalf("queue %s holds a non-positive entry %s", token.Hex(), e.Amount)
			}
			if e.OriginalTimestamp < windowStart {
				t.Fatalf("queue %s holds an expired entry @%d", token.Hex(), e.OriginalTimestamp)
			}
			sum.Add(sum, e.Amount)
		}
		balance, ok := s.AcquiredBalances.Get(token)
		if ok != (sum.Sign() > 0) || ok && balance.Cmp(sum) != 0 {
			t.Fatalf("acquired balance of %s = %v (present: %v), queue holds %s", token.Hex(), balance, ok, sum)
		}
	}
	for _, token := range s.AcquiredBalances.Keys() {
		if !s.AcquiredQueues.Has(token) {
			t.Fatalf("acquired balance of %s without a queue", token.Hex())
		}
	}

	for _, d := range s.DepositRecords {
		if d.RemainingAmount.Cmp(d.AmountIn) > 0 || d.RemainingOutputAmount.Cmp(d.AmountOut) > 0 {
			t.Fatalf("deposit remainder above its initial amount: in %s of %s, out %s of %s",
				d.RemainingAmount, d.AmountIn, d.RemainingOutputAmount, d.AmountOut)
		}
	}
}

// scribble overwrites every big.Int the state holds.
func scribble(s *SubAccountState) {
	poison := func(v *big.Int) { v.SetInt64(-424242) }
	for _, r := range s.SpendingRecords {
		poison(r.Amount)
	}
	poison(s.TotalSpendingInWindow)
	for _, d := range s.DepositRecords {
		poison(d.AmountIn)
		poison(d.RemainingAmount)
		poison(d.AmountOut)
		poison(d.RemainingOutputAmount)
	}
	for _, token := range s.AcquiredQueues.Keys() {
		q, _ := s.AcquiredQueues.Get(token)
		for _, e := range *q {
			poison(e.Amount)
		}
	}
	for _, token := range s.AcquiredBalances.Keys() {
		b, _ := s.AcquiredBalances.Get(token)
		poison(b)
	}
}
