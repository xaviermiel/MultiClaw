package ethx

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// rangeRecorder answers each query with one log per queried block boundary
// and records the requested ranges.
type rangeRecorder struct {
	ranges [][2]uint64
	failAt int // fail the n-th query (1-based); 0 = never
	q      []ethereum.FilterQuery
}

func (r *rangeRecorder) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	r.ranges = append(r.ranges, [2]uint64{q.FromBlock.Uint64(), q.ToBlock.Uint64()})
	r.q = append(r.q, q)
	if r.failAt == len(r.ranges) {
		return nil, errors.New("boom")
	}
	return []types.Log{{BlockNumber: q.FromBlock.Uint64()}, {BlockNumber: q.ToBlock.Uint64()}}, nil
}

func TestFilterLogsChunkedCoversRangeExactly(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	type tc struct{ from, to, chunk uint64 }
	cases := []tc{
		{0, 0, 1}, {0, 9, 10}, {0, 10, 10}, {5, 4, 3}, {100, 5099, 5000}, {100, 5100, 5000},
		{1, 1, 5000}, {0, 12, 1}, {math.MaxUint64 - 3, math.MaxUint64, 2},
	}
	for i := 0; i < 500; i++ {
		from := uint64(rng.Intn(10_000))
		cases = append(cases, tc{from, from + uint64(rng.Intn(20_000)) - 50, uint64(rng.Intn(3000) + 1)})
	}

	for _, c := range cases {
		rec := &rangeRecorder{}
		logs, err := FilterLogsChunked(context.Background(), rec, ethereum.FilterQuery{}, c.from, c.to, c.chunk)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if c.from > c.to {
			if len(rec.ranges) != 0 || len(logs) != 0 {
				t.Fatalf("%+v: empty range must not query", c)
			}
			continue
		}
		next := c.from
		for _, r := range rec.ranges {
			if r[0] != next || r[1] < r[0] || r[1]-r[0]+1 > c.chunk {
				t.Fatalf("%+v: bad chunk %v (expected start %d)", c, r, next)
			}
			next = r[1] + 1
			if r[1] == c.to {
				break
			}
		}
		if last := rec.ranges[len(rec.ranges)-1]; last[1] != c.to {
			t.Fatalf("%+v: range not fully covered, last chunk %v", c, last)
		}
		// Results stay in chunk (chain) order.
		for i := 1; i < len(logs); i++ {
			if logs[i].BlockNumber < logs[i-1].BlockNumber {
				t.Fatalf("%+v: logs out of order", c)
			}
		}
	}
}

func TestFilterLogsChunkedKeepsFilterAndFailsWhole(t *testing.T) {
	module := common.HexToAddress("0x16D44268C4e3a567c5ff87cCCa10Fe46f8a656Bb")
	q := ethereum.FilterQuery{
		Addresses: []common.Address{module},
		Topics:    [][]common.Hash{{common.Hash{1}}, {common.Hash{2}}},
		BlockHash: &common.Hash{9}, // must be ignored
	}
	rec := &rangeRecorder{failAt: 2}
	_, err := FilterLogsChunked(context.Background(), rec, q, 0, 99, 10)
	if err == nil || len(rec.ranges) != 2 {
		t.Fatalf("expected failure on 2nd chunk, got %v after %d queries", err, len(rec.ranges))
	}
	for _, sent := range rec.q {
		if sent.BlockHash != nil || sent.Addresses[0] != module || sent.Topics[1][0] != (common.Hash{2}) {
			t.Fatalf("filter not preserved: %+v", sent)
		}
	}
	if _, err := FilterLogsChunked(context.Background(), rec, q, 0, 1, 0); err == nil {
		t.Fatal("zero chunk size must be rejected")
	}
}

func TestSubClamped(t *testing.T) {
	if SubClamped(10, 3) != 7 || SubClamped(3, 10) != 0 || SubClamped(0, 0) != 0 {
		t.Fatal("SubClamped wrong")
	}
}
