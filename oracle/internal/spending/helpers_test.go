package spending

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// Shared helpers for the ported vitest suites (state_test.go is
// spending-oracle.test.ts, state_edge_test.go is spending-oracle-edge.test.ts).

const (
	oneDay         int64 = 86400
	windowDuration       = oneDay
)

func addr(s string) common.Address { return common.HexToAddress(s) }

func addrs(as ...common.Address) []common.Address {
	if as == nil {
		return []common.Address{}
	}
	return as
}

func ints(xs ...int64) []*big.Int {
	out := make([]*big.Int, len(xs))
	for i, x := range xs {
		out[i] = big.NewInt(x)
	}
	return out
}

func bigs(xs ...*big.Int) []*big.Int { return xs }

// units returns v * 10^decimals (viem's parseUnits for integer amounts).
func units(v int64, decimals uint) *big.Int { return bn.Mul(big.NewInt(v), bn.Pow10(decimals)) }

func build(events []ProtocolExecutionEvent, transfers []TransferExecutedEvent, sub common.Address, now int64, cache TokenPriceCache) *SubAccountState {
	return BuildSubAccountState(events, transfers, sub, now, windowDuration, cache, logx.Discard)
}

// wantBalance asserts acquiredBalances.get(token) === want.
func wantBalance(t *testing.T, s *SubAccountState, token common.Address, want int64) {
	t.Helper()
	got, ok := s.AcquiredBalances.Get(token)
	if !ok {
		t.Fatalf("acquired balance of %s is undefined, want %d", token.Hex(), want)
	}
	if got.Cmp(big.NewInt(want)) != 0 {
		t.Fatalf("acquired balance of %s = %s, want %d", token.Hex(), got, want)
	}
}

// wantNoBalance asserts acquiredBalances.get(token) is undefined.
func wantNoBalance(t *testing.T, s *SubAccountState, token common.Address) {
	t.Helper()
	if got, ok := s.AcquiredBalances.Get(token); ok {
		t.Fatalf("acquired balance of %s = %s, want undefined", token.Hex(), got)
	}
}

// balanceOrZero mirrors `acquiredBalances.get(token) ?? 0n`.
func balanceOrZero(s *SubAccountState, token common.Address) *big.Int {
	if v, ok := s.AcquiredBalances.Get(token); ok {
		return v
	}
	return bn.Zero()
}

func wantInt(t *testing.T, what string, got *big.Int, want int64) {
	t.Helper()
	if got.Cmp(big.NewInt(want)) != 0 {
		t.Fatalf("%s = %s, want %d", what, got, want)
	}
}

func wantLen[T any](t *testing.T, what string, xs []T, want int) {
	t.Helper()
	if len(xs) != want {
		t.Fatalf("len(%s) = %d, want %d", what, len(xs), want)
	}
}

// queueOf returns acquiredQueues.get(token), failing if undefined.
func queueOf(t *testing.T, s *SubAccountState, token common.Address) Queue {
	t.Helper()
	q, ok := s.AcquiredQueues.Get(token)
	if !ok {
		t.Fatalf("queue of %s is undefined", token.Hex())
	}
	return *q
}

func depositByTokenIn(t *testing.T, s *SubAccountState, token common.Address) *DepositRecord {
	t.Helper()
	for _, d := range s.DepositRecords {
		if d.TokenIn == token {
			return d
		}
	}
	t.Fatalf("no deposit record with tokenIn %s", token.Hex())
	return nil
}

func depositByTokenOut(t *testing.T, s *SubAccountState, token common.Address) *DepositRecord {
	t.Helper()
	for _, d := range s.DepositRecords {
		if d.HasTokenOut && d.TokenOut == token {
			return d
		}
	}
	t.Fatalf("no deposit record with tokenOut %s", token.Hex())
	return nil
}

func entries(pairs ...int64) Queue {
	if len(pairs)%2 != 0 {
		panic("entries takes (amount, timestamp) pairs")
	}
	q := Queue{}
	for i := 0; i < len(pairs); i += 2 {
		q = append(q, AcquiredEntry{Amount: big.NewInt(pairs[i]), OriginalTimestamp: pairs[i+1]})
	}
	return q
}
