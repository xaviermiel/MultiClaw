package spendingoracle

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

// minRetryChunk is the smallest half-range a failed history chunk is
// retried with.
const minRetryChunk = 100

// historicalTokens returns, in discovery order, the tokens whose acquired
// balance was set for sub in the last MAX_HISTORICAL_BLOCKS blocks
// (AcquiredBalanceUpdated events): candidates for clearing stale on-chain
// balances. Port of queryHistoricalAcquiredTokens: the range is scanned in
// inclusive chunks of MAX_BLOCKS_PER_QUERY+1 blocks; a failed chunk is
// retried in halves (if at least 100 blocks), and halves that fail are
// skipped. Unlike the TS, the head block is always scanned.
func (s *Service) historicalTokens(ctx context.Context, module, sub common.Address) ([]common.Address, error) {
	current, err := s.chain.BlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	chunk := uint64(s.cfg.MaxBlocksPerQuery)
	earliest := ethx.SubClamped(current, uint64(s.cfg.MaxHistoricalBlocks))
	s.logf("Querying historical tokens from block %d to %d (%d blocks, max %d per query)",
		earliest, current, current-earliest, chunk)

	var tokens []common.Address
	seen := map[common.Address]bool{}
	totalLogs, queries := 0, 0
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
			// viem returned undecodable logs without arguments, which the
			// TS then skipped.
			if d, err := contracts.DecodeAcquiredBalanceUpdated(l); err == nil && !seen[d.Token] {
				seen[d.Token] = true
				tokens = append(tokens, d.Token)
			}
		}
		totalLogs += len(logs)
		return nil
	}

	for from := earliest; from <= current; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		to := min(from+chunk, current)
		if err := scan(from, to); err == nil {
			queries++
		} else {
			s.logf("Query chunk failed (blocks %d-%d), trying smaller range: %v", from, to, err)
			if half := chunk / 2; half >= minRetryChunk {
				for subFrom := from; subFrom <= to; {
					subTo := min(subFrom+half, to)
					if err := scan(subFrom, subTo); err != nil {
						s.logf("Sub-chunk query failed (blocks %d-%d), skipping: %v", subFrom, subTo, err)
					}
					subFrom = subTo + 1
				}
			} else {
				s.logf("Chunk too small to retry, skipping blocks %d-%d", from, to)
			}
		}
		from = to + 1
	}
	s.logf("Historical token query complete: %d unique tokens found in %d events (%d queries)",
		len(tokens), totalLogs, queries)
	return tokens, nil
}
