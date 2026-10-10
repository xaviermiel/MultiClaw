package spendingoracle

import (
	"cmp"
	"context"
	"math"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

// minRetryChunk is the smallest half-range a failed history chunk is
// retried with.
const minRetryChunk = 100

// historyRescanInterval is how often a subaccount's history is scanned again
// from scratch. That corrects what the incremental scans can miss: a reorg
// deeper than the confirmation depth that the poll did not detect, or a node
// that returned incomplete logs without an error.
const historyRescanInterval = time.Hour

// blockRange is an inclusive range of blocks.
type blockRange struct{ from, to uint64 }

// acquiredEvent is an AcquiredBalanceUpdated event's position and token.
type acquiredEvent struct {
	block uint64
	index uint
	token common.Address
}

// tokenHistory is what the scans learned of a subaccount's
// AcquiredBalanceUpdated events. Blocks [from, next) were read, except the
// failed ranges in gaps, and are not read again. Blocks from next up were
// unconfirmed (within CONFIRMATION_BLOCKS of the head) when last read, so
// the next scan reads them again. events is in chain order.
type tokenHistory struct {
	from, next uint64
	gaps       []blockRange
	events     []acquiredEvent
	createdAt  time.Time
}

// historicalTokens returns, in chain order of their first event, the tokens
// whose acquired balance was set for sub in the last MAX_HISTORICAL_BLOCKS
// blocks (AcquiredBalanceUpdated events): candidates for clearing stale
// on-chain balances. It returns what queryHistoricalAcquiredTokens did, but
// the TS read the whole range on every call (some 260 queries per subaccount
// and refresh on Base); this keeps the events and only queries the blocks
// that are new or were unconfirmed at the last scan, and the ranges that
// failed (which the TS skipped until its next scan). The history is read
// again from scratch every hour and after a reorg. As in the TS, ranges are
// queried in inclusive chunks of MAX_BLOCKS_PER_QUERY+1 blocks and a failed
// chunk is retried in halves (if at least 100 blocks). Unlike the TS, the
// head block is always scanned.
func (s *Service) historicalTokens(ctx context.Context, module, sub common.Address) ([]common.Address, error) {
	current, err := s.chain.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	chunk := uint64(s.cfg.MaxBlocksPerQuery)
	earliest := ethx.SubClamped(current, uint64(s.cfg.MaxHistoricalBlocks))
	key := updateKey{module, sub}
	h := s.history[key]
	if h == nil || s.Now().Sub(h.createdAt) >= historyRescanInterval {
		h = &tokenHistory{from: earliest, next: earliest, createdAt: s.Now()}
		s.history[key] = h
	}
	h.setStart(earliest)
	h.events = slices.DeleteFunc(h.events, func(e acquiredEvent) bool { return e.block >= h.next })
	var ranges []blockRange
	for _, g := range h.gaps {
		ranges = appendRange(ranges, g)
	}
	if h.next <= current {
		ranges = appendRange(ranges, blockRange{h.next, current})
	}

	var failed []blockRange
	queries := 0
	scan := func(from, to uint64) error {
		logs, err := s.chain.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(from),
			ToBlock:   new(big.Int).SetUint64(to),
			Addresses: []common.Address{module},
			Topics:    [][]common.Hash{{contracts.AcquiredBalanceUpdatedTopic}, {contracts.AddressTopic(sub)}},
		})
		if err != nil {
			return err
		}
		for _, l := range logs {
			// A log that doesn't decode is skipped. (viem kept a log whose
			// data didn't decode, and the TS used its token topic; the
			// module never emits such logs.)
			if d, err := contracts.DecodeAcquiredBalanceUpdated(l); err == nil {
				h.events = append(h.events, acquiredEvent{block: l.BlockNumber, index: l.Index, token: d.Token})
			}
		}
		return nil
	}
	for _, r := range ranges {
		s.logf("Querying historical tokens from block %d to %d (%d blocks, max %d per query)",
			r.from, r.to, r.to-r.from, chunk)
		for from := r.from; from <= r.to; {
			if err := ctx.Err(); err != nil {
				delete(s.history, key) // partly updated
				return nil, err
			}
			to := min(from+chunk, r.to)
			if err := scan(from, to); err == nil {
				queries++
			} else {
				s.logf("Query chunk failed (blocks %d-%d), trying smaller range: %v", from, to, err)
				if half := chunk / 2; half >= minRetryChunk {
					for subFrom := from; subFrom <= to; {
						subTo := min(subFrom+half, to)
						if err := scan(subFrom, subTo); err != nil {
							s.logf("Sub-chunk query failed (blocks %d-%d), will retry next time: %v", subFrom, subTo, err)
							failed = appendRange(failed, blockRange{subFrom, subTo})
						}
						subFrom = subTo + 1
					}
				} else {
					s.logf("Chunk too small to retry, will retry blocks %d-%d next time", from, to)
					failed = appendRange(failed, blockRange{from, to})
				}
			}
			from = to + 1
		}
	}
	// A shutdown during the last chunk made it fail: the scan is incomplete.
	if err := ctx.Err(); err != nil {
		delete(s.history, key)
		return nil, err
	}
	// The first unconfirmed block; failed ranges from there are read again
	// anyway.
	h.next = max(h.from, ethx.SubClamped(current+1, uint64(s.cfg.ConfirmationBlocks)))
	h.gaps = clipRanges(failed, 0, h.next)
	slices.SortFunc(h.events, func(a, b acquiredEvent) int {
		return cmp.Or(cmp.Compare(a.block, b.block), cmp.Compare(a.index, b.index))
	})

	var tokens []common.Address
	seen := map[common.Address]bool{}
	events := 0
	for _, e := range h.events {
		if e.block > current {
			break // read when the head was higher
		}
		events++
		if !seen[e.token] {
			seen[e.token] = true
			tokens = append(tokens, e.token)
		}
	}
	s.logf("Historical token query complete: %d unique tokens found in %d events (%d queries)",
		len(tokens), events, queries)
	return tokens, nil
}

// setStart moves the start of the history to block start: earlier events
// and gaps are dropped, and if the history started later (the head moved
// back), the blocks in between become a gap.
func (h *tokenHistory) setStart(start uint64) {
	if start < h.from {
		h.gaps = append([]blockRange{{start, h.from - 1}}, h.gaps...)
	}
	h.events = slices.DeleteFunc(h.events, func(e acquiredEvent) bool { return e.block < start })
	h.gaps = clipRanges(h.gaps, start, math.MaxUint64)
	h.from = start
	h.next = max(h.next, start)
}

// appendRange appends r to the ranges rs, which end before it, merging it
// with the last one when they touch.
func appendRange(rs []blockRange, r blockRange) []blockRange {
	if n := len(rs); n > 0 && rs[n-1].to+1 >= r.from {
		rs[n-1].to = max(rs[n-1].to, r.to)
		return rs
	}
	return append(rs, r)
}

// clipRanges returns the parts of the ranges rs within [lo, end).
func clipRanges(rs []blockRange, lo, end uint64) []blockRange {
	var out []blockRange
	for _, r := range rs {
		if r.to >= lo && r.from < end {
			out = append(out, blockRange{max(r.from, lo), min(r.to, end-1)})
		}
	}
	return out
}
