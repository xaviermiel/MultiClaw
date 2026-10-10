package ethx

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
)

// LogFilterer executes eth_getLogs.
type LogFilterer interface {
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
}

// FilterLogsChunked runs q over the inclusive block range [from, to] in
// consecutive chunks of at most chunkSize blocks and concatenates the results
// (so they stay in chain order). q's FromBlock/ToBlock/BlockHash are ignored.
// Any chunk failure fails the whole query, like the single unbounded query
// the TS oracle issued. An empty range (from > to) returns no logs.
func FilterLogsChunked(ctx context.Context, f LogFilterer, q ethereum.FilterQuery, from, to, chunkSize uint64) ([]types.Log, error) {
	if chunkSize == 0 {
		return nil, fmt.Errorf("chunk size must be positive")
	}
	var all []types.Log
	for start := from; start <= to; {
		end := to
		if to-start >= chunkSize {
			end = start + chunkSize - 1
		}
		cq := q
		cq.BlockHash = nil
		cq.FromBlock = new(big.Int).SetUint64(start)
		cq.ToBlock = new(big.Int).SetUint64(end)
		logs, err := f.FilterLogs(ctx, cq)
		if err != nil {
			return nil, fmt.Errorf("getLogs blocks %d-%d: %w", start, end, err)
		}
		all = append(all, logs...)
		if end == to {
			break // also avoids overflow when to == MaxUint64
		}
		start = end + 1
	}
	return all, nil
}

// SubClamped returns a-b, or 0 when b > a (block ranges never go negative;
// the TS computed negative block numbers on young chains).
func SubClamped(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}
