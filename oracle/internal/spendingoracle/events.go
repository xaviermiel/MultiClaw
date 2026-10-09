package spendingoracle

import (
	"context"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/sync/errgroup"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/spending"
)

// polledEvents are a module's events in a polled block range.
type polledEvents struct {
	protocol  []spending.ProtocolExecutionEvent
	transfers []spending.TransferExecutedEvent
	// limitsSet counts SubAccountLimitsSet logs; limitsSetSubAccounts holds
	// the subaccount of each that names one.
	limitsSet            int
	limitsSetSubAccounts []common.Address
}

func (e *polledEvents) count() int { return len(e.protocol) + len(e.transfers) + e.limitsSet }

// subAccounts lists the subaccounts the events concern, in first-seen order:
// protocol events, then transfers, then limit changes (the TS Set order).
func (e *polledEvents) subAccounts() []common.Address {
	var out []common.Address
	seen := map[common.Address]bool{}
	add := func(a common.Address) {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, ev := range e.protocol {
		add(ev.SubAccount)
	}
	for _, ev := range e.transfers {
		add(ev.SubAccount)
	}
	for _, a := range e.limitsSetSubAccounts {
		add(a)
	}
	return out
}

// newEvents reads a module's events of every subaccount in [from, to].
func (s *Service) newEvents(ctx context.Context, module common.Address, from, to uint64) (*polledEvents, error) {
	var ev polledEvents
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		ev.protocol, err = s.protocolEvents(gctx, module, from, to, nil)
		return err
	})
	g.Go(func() (err error) {
		ev.transfers, err = s.transferEvents(gctx, module, from, to, nil)
		return err
	})
	g.Go(func() error {
		logs, err := s.filterLogs(gctx, module, contracts.SubAccountLimitsSetTopic, nil, from, to)
		if err != nil {
			return err
		}
		ev.limitsSet = len(logs)
		for _, l := range logs {
			// Only the subaccount topic matters, as in the TS.
			if sub, err := contracts.SubAccountFromLimitsSet(l); err == nil {
				ev.limitsSetSubAccounts = append(ev.limitsSetSubAccounts, sub)
			}
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return &ev, nil
}

// filterLogs reads a module's logs with topic0 in [from, to], optionally
// only those of one subaccount, in chunks of MAX_BLOCKS_PER_QUERY blocks.
// (The TS asked for the whole range at once.)
func (s *Service) filterLogs(ctx context.Context, module common.Address, topic common.Hash, sub *common.Address, from, to uint64) ([]types.Log, error) {
	q := ethereum.FilterQuery{Addresses: []common.Address{module}, Topics: [][]common.Hash{{topic}}}
	if sub != nil {
		q.Topics = append(q.Topics, []common.Hash{contracts.AddressTopic(*sub)})
	}
	return ethx.FilterLogsChunked(ctx, s.chain, q, from, to, uint64(s.cfg.MaxBlocksPerQuery))
}

// protocolEvents reads ProtocolExecution events with their block
// timestamps. A malformed event fails the query, as in the TS.
func (s *Service) protocolEvents(ctx context.Context, module common.Address, from, to uint64, sub *common.Address) ([]spending.ProtocolExecutionEvent, error) {
	logs, err := s.filterLogs(ctx, module, contracts.ProtocolExecutionTopic, sub, from, to)
	if err != nil {
		return nil, err
	}
	events := make([]spending.ProtocolExecutionEvent, len(logs))
	blocks := make([]uint64, len(logs))
	for i, l := range logs {
		d, err := contracts.DecodeProtocolExecution(l)
		if err != nil {
			return nil, err
		}
		events[i] = spending.ProtocolExecutionEvent{
			SubAccount:   d.SubAccount,
			Target:       d.Target,
			OpType:       d.OpType,
			TokensIn:     d.TokensIn,
			AmountsIn:    d.AmountsIn,
			TokensOut:    d.TokensOut,
			AmountsOut:   d.AmountsOut,
			SpendingCost: d.SpendingCost,
			BlockNumber:  int64(d.BlockNumber),
			LogIndex:     int64(d.LogIndex),
		}
		blocks[i] = d.BlockNumber
	}
	times, err := s.blockTimes(ctx, blocks)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Timestamp = times[blocks[i]]
	}
	return events, nil
}

// transferEvents reads TransferExecuted events with their block timestamps.
func (s *Service) transferEvents(ctx context.Context, module common.Address, from, to uint64, sub *common.Address) ([]spending.TransferExecutedEvent, error) {
	logs, err := s.filterLogs(ctx, module, contracts.TransferExecutedTopic, sub, from, to)
	if err != nil {
		return nil, err
	}
	events := make([]spending.TransferExecutedEvent, len(logs))
	blocks := make([]uint64, len(logs))
	for i, l := range logs {
		d, err := contracts.DecodeTransferExecuted(l)
		if err != nil {
			return nil, err
		}
		events[i] = spending.TransferExecutedEvent{
			SubAccount:   d.SubAccount,
			Token:        d.Token,
			Recipient:    d.Recipient,
			Amount:       d.Amount,
			SpendingCost: d.SpendingCost,
			BlockNumber:  int64(d.BlockNumber),
			LogIndex:     int64(d.LogIndex),
		}
		blocks[i] = d.BlockNumber
	}
	times, err := s.blockTimes(ctx, blocks)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Timestamp = times[blocks[i]]
	}
	return events, nil
}

// blockTimes reads the timestamp of each distinct block, recording its hash
// for reorg detection. Any failure fails the whole read.
func (s *Service) blockTimes(ctx context.Context, blocks []uint64) (map[uint64]int64, error) {
	times := map[uint64]int64{}
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallelReads)
	seen := map[uint64]bool{}
	for _, n := range blocks {
		if seen[n] {
			continue
		}
		seen[n] = true
		g.Go(func() error {
			info, err := s.chain.BlockInfo(gctx, n)
			if err != nil {
				return err
			}
			s.blocks.record(n, info.Hash)
			mu.Lock()
			times[n] = int64(info.Timestamp)
			mu.Unlock()
			return nil
		})
	}
	return times, g.Wait()
}
