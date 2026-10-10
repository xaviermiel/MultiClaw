package spendingoracle

import (
	"cmp"
	"context"
	"errors"
	"math/big"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/schedule"
)

// historyRanges returns the [from, to] ranges of the AcquiredBalanceUpdated
// queries made so far.
func historyRanges(c *fakeChain) [][2]uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out [][2]uint64
	for _, q := range c.queries {
		if q.Topics[0][0] == contracts.AcquiredBalanceUpdatedTopic {
			out = append(out, [2]uint64{q.FromBlock.Uint64(), q.ToBlock.Uint64()})
		}
	}
	return out
}

func TestHistoricalTokensScan(t *testing.T) {
	ctx := context.Background()

	t.Run("inclusive chunks reach the head", func(t *testing.T) {
		env := newTestEnv(t)
		// 102 = 2 * (50 + 1): the TS's last chunk ended at head-1 and
		// stopped there.
		env.svc.cfg.MaxHistoricalBlocks = 102
		if _, err := env.svc.historicalTokens(ctx, module, subA); err != nil {
			t.Fatal(err)
		}
		want := [][2]uint64{{898, 948}, {949, 999}, {1000, 1000}}
		if got := historyRanges(env.chain); !slices.Equal(got, want) {
			t.Fatalf("ranges %v, want %v", got, want)
		}
		env.wantLogs("Querying historical tokens from block 898 to 1000 (102 blocks, max 50 per query)")
	})

	t.Run("failed chunks are retried in halves, failed halves next time", func(t *testing.T) {
		env := newTestEnv(t)
		env.svc.cfg.MaxBlocksPerQuery = 400
		env.svc.cfg.MaxHistoricalBlocks = 1000
		env.chain.addLog(acquiredLog(t, subA, tokenX, e18(1), 500)) // in the half that fails
		env.chain.addLog(acquiredLog(t, subA, tokenY, e18(1), 700))
		env.chain.addLog(acquiredLog(t, subA, tokenZ, e18(1), 900))
		env.chain.addLog(acquiredLog(t, subA, tokenY, e18(2), 950)) // seen already
		env.chain.failLogs = func(q ethereum.FilterQuery) error {
			if from := q.FromBlock.Uint64(); from == 401 && (q.ToBlock.Uint64() == 801 || q.ToBlock.Uint64() == 601) {
				return errRPC
			}
			return nil
		}
		got, err := env.svc.historicalTokens(ctx, module, subA)
		if err != nil || !slices.Equal(got, []common.Address{tokenY, tokenZ}) {
			t.Fatalf("tokens %v (%v)", got, err)
		}
		env.wantLogs(
			"Query chunk failed (blocks 401-801), trying smaller range: rpc: rate limited",
			"Sub-chunk query failed (blocks 401-601), will retry next time",
			"Historical token query complete: 2 unique tokens found in 3 events (2 queries)",
		)

		env.chain.failLogs = nil
		before := len(historyRanges(env.chain))
		got, err = env.svc.historicalTokens(ctx, module, subA)
		// In chain order of their first event: X is now first.
		if err != nil || !slices.Equal(got, []common.Address{tokenX, tokenY, tokenZ}) {
			t.Fatalf("tokens %v (%v)", got, err)
		}
		// The failed half, then the blocks unconfirmed at the last scan.
		want := [][2]uint64{{401, 601}, {999, 1000}}
		if got := historyRanges(env.chain)[before:]; !slices.Equal(got, want) {
			t.Fatalf("ranges %v, want %v", got, want)
		}
	})

	t.Run("chunks under 200 blocks are not split", func(t *testing.T) {
		env := newTestEnv(t)
		env.svc.cfg.MaxBlocksPerQuery = 150
		env.svc.cfg.MaxHistoricalBlocks = 150
		env.chain.failLogs = func(ethereum.FilterQuery) error { return errRPC }
		if got, err := env.svc.historicalTokens(ctx, module, subA); err != nil || len(got) != 0 {
			t.Fatalf("tokens %v (%v)", got, err)
		}
		env.wantLogs("Chunk too small to retry, will retry blocks 850-1000 next time")

		env.chain.failLogs = nil
		before := len(historyRanges(env.chain))
		if _, err := env.svc.historicalTokens(ctx, module, subA); err != nil {
			t.Fatal(err)
		}
		// The failed chunk and the unconfirmed blocks, in one query.
		if got := historyRanges(env.chain)[before:]; !slices.Equal(got, [][2]uint64{{850, 1000}}) {
			t.Fatalf("ranges %v", got)
		}
	})
}

func TestHistoricalTokensOnlyQueryNewAndUnconfirmedBlocks(t *testing.T) {
	env := newTestEnv(t) // 2 confirmations, 50-block chunks, 500 blocks of history
	ctx := context.Background()
	env.chain.addLog(acquiredLog(t, subA, tokenX, e18(1), 600))
	env.chain.addLog(acquiredLog(t, subA, tokenY, e18(1), 1000)) // unconfirmed
	if got, err := env.svc.historicalTokens(ctx, module, subA); err != nil || !slices.Equal(got, []common.Address{tokenX, tokenY}) {
		t.Fatalf("tokens %v (%v)", got, err)
	}

	// Block 1000 is replaced: its event moves to block 1005.
	env.chain.removeLogs(func(l types.Log) bool { return l.BlockNumber == 1000 })
	env.chain.addLog(acquiredLog(t, subA, tokenZ, e18(1), 999))
	env.chain.addLog(acquiredLog(t, subA, tokenY, e18(1), 1005))
	env.chain.setHead(1010)
	before := len(historyRanges(env.chain))
	got, err := env.svc.historicalTokens(ctx, module, subA)
	if err != nil || !slices.Equal(got, []common.Address{tokenX, tokenZ, tokenY}) {
		t.Fatalf("tokens %v (%v)", got, err)
	}
	if got := historyRanges(env.chain)[before:]; !slices.Equal(got, [][2]uint64{{999, 1010}}) {
		t.Fatalf("ranges %v", got)
	}
	env.wantLogs("Historical token query complete: 3 unique tokens found in 3 events (1 queries)")

	// Block 600 leaves the range.
	env.chain.setHead(1101)
	if got, _ := env.svc.historicalTokens(ctx, module, subA); !slices.Equal(got, []common.Address{tokenZ, tokenY}) {
		t.Fatalf("tokens %v", got)
	}

	// Every subaccount has its own history.
	before = len(historyRanges(env.chain))
	if got, _ := env.svc.historicalTokens(ctx, module, subB); len(got) != 0 {
		t.Fatalf("tokens %v", got)
	}
	if got := historyRanges(env.chain)[before]; got != [2]uint64{601, 651} {
		t.Fatalf("first range %v, want a full scan", got)
	}

	// An interrupted scan starts over.
	env.chain.setHead(1200)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := env.svc.historicalTokens(cancelled, module, subA); err == nil {
		t.Fatal("no error")
	}
	if _, ok := env.svc.history[updateKey{module, subA}]; ok {
		t.Fatal("kept a partly updated history")
	}

	// An hour later, the history is read again from scratch.
	env.svc.historicalTokens(ctx, module, subB)
	env.svc.Now = func() time.Time { return time.Unix(now, 0).Add(time.Hour) }
	before = len(historyRanges(env.chain))
	env.svc.historicalTokens(ctx, module, subB)
	if got := historyRanges(env.chain)[before]; got != [2]uint64{700, 750} {
		t.Fatalf("first range %v, want a full scan", got)
	}
}

func TestHistoricalTokensOnAChainYoungerThanTheConfirmationDepth(t *testing.T) {
	env := newTestEnv(t) // 2 confirmations
	ctx := context.Background()
	env.chain.setHead(1)
	if _, err := env.svc.historicalTokens(ctx, module, subA); err != nil {
		t.Fatal(err)
	}
	// Block 0 was not confirmed yet: it is read again.
	env.chain.addLog(acquiredLog(t, subA, tokenX, e18(1), 0))
	env.chain.setHead(5)
	before := len(historyRanges(env.chain))
	if got, _ := env.svc.historicalTokens(ctx, module, subA); !slices.Equal(got, []common.Address{tokenX}) {
		t.Fatalf("tokens %v", got)
	}
	if got := historyRanges(env.chain)[before:]; !slices.Equal(got, [][2]uint64{{0, 5}}) {
		t.Fatalf("ranges %v", got)
	}
}

// fullScanTokens is what a full scan of [from, to] finds: sub's tokens in
// chain order of their first AcquiredBalanceUpdated event.
func fullScanTokens(c *fakeChain, sub common.Address, from, to uint64) []common.Address {
	logs, _ := c.FilterLogs(context.Background(), ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(from),
		ToBlock:   new(big.Int).SetUint64(to),
		Topics:    [][]common.Hash{{contracts.AcquiredBalanceUpdatedTopic}, {topic(sub)}},
	})
	var tokens []common.Address
	for _, l := range logs {
		if token := common.BytesToAddress(l.Topics[2].Bytes()); !slices.Contains(tokens, token) {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// checkHistory checks h's invariants after a scan.
func checkHistory(t *testing.T, h *tokenHistory) {
	t.Helper()
	if h.from > h.next {
		t.Fatalf("from %d > next %d", h.from, h.next)
	}
	for i, e := range h.events {
		if e.block < h.from {
			t.Fatalf("event at block %d before the start %d", e.block, h.from)
		}
		if i > 0 && cmp.Or(cmp.Compare(h.events[i-1].block, e.block), cmp.Compare(h.events[i-1].index, e.index)) >= 0 {
			t.Fatalf("events out of order or duplicated: %v", h.events)
		}
	}
	for i, g := range h.gaps {
		if g.from > g.to || g.from < h.from || g.to >= h.next || (i > 0 && h.gaps[i-1].to+1 >= g.from) {
			t.Fatalf("gaps %v in [%d, %d)", h.gaps, h.from, h.next)
		}
	}
}

// TestHistoricalTokensMatchAFullScan checks the incremental scans against a
// new oracle's full scan, on random histories: heads moving forward, a
// little back or past the whole range, unconfirmed blocks replaced between
// scans, and failing queries.
func TestHistoricalTokensMatchAFullScan(t *testing.T) {
	ctx := context.Background()
	tokens := []common.Address{tokenX, tokenY, tokenZ, common.HexToAddress("0x4444"), common.HexToAddress("0x5555")}
	subs := []common.Address{subA, subB}
	for seed := range 50 {
		rng := rand.New(rand.NewPCG(uint64(seed), 1))
		env := newTestEnv(t)
		cfg := env.svc.cfg
		cfg.MaxBlocksPerQuery = []int64{7, 50, 250}[rng.IntN(3)]
		cfg.ConfirmationBlocks = rng.Int64N(4)
		cfg.MaxHistoricalBlocks = []int64{0, 40, 300, 1000}[rng.IntN(4)]
		chunk, conf := uint64(cfg.MaxBlocksPerQuery), uint64(cfg.ConfirmationBlocks)
		addEvents := func(lo, hi uint64, n int) {
			for range n {
				sub := subs[rng.IntN(len(subs))]
				env.chain.addLog(acquiredLog(t, sub, tokens[rng.IntN(len(tokens))], e18(1), lo+rng.Uint64N(hi-lo+1)))
			}
		}
		head := rng.Uint64N(1500)
		if rng.IntN(3) == 0 {
			head = rng.Uint64N(4) // younger than the confirmation depth
		}
		addEvents(0, head, rng.IntN(60))
		// Blocks before unconfirmed were confirmed at a scan: they never
		// change.
		unconfirmed := uint64(0)
		failed := map[common.Address]bool{}
		for step := range 60 {
			prev := head
			switch r := rng.IntN(10); {
			case r == 0:
				head = ethx.SubClamped(head, rng.Uint64N(conf+3))
			case r == 1:
				head += uint64(cfg.MaxHistoricalBlocks) + 1 + rng.Uint64N(100)
			default:
				head += rng.Uint64N(2*chunk + 5)
			}
			env.chain.setHead(head)
			// Replace some unconfirmed blocks.
			if rng.IntN(2) == 0 {
				env.chain.removeLogs(func(l types.Log) bool { return l.BlockNumber >= unconfirmed && rng.IntN(2) == 0 })
			}
			if unconfirmed <= head {
				addEvents(unconfirmed, head, rng.IntN(6))
			}
			failing := rng.IntN(4) == 0
			for _, sub := range subs {
				if failing {
					env.chain.failLogs = func(ethereum.FilterQuery) error {
						if rng.IntN(3) == 0 {
							return errRPC
						}
						return nil
					}
				}
				before := len(historyRanges(env.chain))
				got, err := env.svc.historicalTokens(ctx, module, sub)
				if err != nil {
					t.Fatal(err)
				}
				queries := len(historyRanges(env.chain)) - before
				env.chain.failLogs = nil

				fresh := New(cfg, env.chain, env.sender, nil)
				fresh.Now = env.svc.Now
				want, err := fresh.historicalTokens(ctx, module, sub)
				if err != nil {
					t.Fatal(err)
				}
				earliest := ethx.SubClamped(head, uint64(cfg.MaxHistoricalBlocks))
				if spec := fullScanTokens(env.chain, sub, earliest, head); !slices.Equal(want, spec) {
					t.Fatalf("seed %d step %d: full scan %v, want %v", seed, step, want, spec)
				}
				switch {
				case !failing && !slices.Equal(got, want):
					t.Fatalf("seed %d step %d (head %d, conf %d, chunk %d): tokens %v, want %v",
						seed, step, head, conf, chunk, got, want)
				case failing && slices.ContainsFunc(got, func(a common.Address) bool { return !slices.Contains(want, a) }):
					t.Fatalf("seed %d step %d: tokens %v not in %v", seed, step, got, want)
				}
				// Without failures, only the new and the unconfirmed blocks
				// are queried.
				if step > 0 && !failing && !failed[sub] && head >= prev {
					if limit := (head - prev + conf + chunk) / (chunk + 1); uint64(queries) > limit {
						t.Fatalf("seed %d step %d: %d queries from head %d to %d, want at most %d",
							seed, step, queries, prev, head, limit)
					}
				}
				failed[sub] = failing
				checkHistory(t, env.svc.history[updateKey{module, sub}])
			}
			unconfirmed = max(unconfirmed, ethx.SubClamped(head+1, conf))
		}
	}
}

func TestPollProcessesAffectedSubaccountsOnce(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.chain.addLog(protocolLog(t, subA, contracts.OpSwap, []common.Address{tokenX}, []*big.Int{e18(1)},
		[]common.Address{tokenY}, []*big.Int{e18(2)}, e18(1), 950))
	env.chain.addLog(transferLog(t, subB, tokenX, e18(1), e18(1), 960))
	env.chain.addLog(limitsSetLog(t, subC, 970))
	env.chain.addLog(limitsSetLog(t, module, 971))
	env.chain.addLog(transferLog(t, subA, tokenX, e18(1), e18(1), 980)) // subA again

	env.svc.Poll(ctx)
	if got := env.sender.subAccounts(); !slices.Equal(got, []common.Address{subA, subB, subC}) {
		t.Fatalf("processed %v; logs:\n%s", got, env.logs)
	}
	env.wantLogs(
		"Polling blocks 899 to 998 (100 blocks, 2 confirmation depth)",
		"Found 1 protocol events, 2 transfer events, 2 limits-set events",
		"Skipping "+lower(module)+" - this is the module address, not a subaccount",
		"Transaction results: 3 confirmed, 0 failed",
	)
	if env.svc.lastProcessedBlock != 998 {
		t.Fatalf("lastProcessedBlock = %d", env.svc.lastProcessedBlock)
	}

	// Nothing new: nothing logged, nothing sent.
	env.logs.reset()
	env.svc.Poll(ctx)
	if env.logs.String() != "" || len(env.sender.sent()) != 3 {
		t.Fatalf("idle poll logged:\n%s", env.logs)
	}

	// New blocks without events.
	env.chain.setHead(1010)
	env.svc.Poll(ctx)
	env.wantLogs("Polling blocks 999 to 1008 (10 blocks, 2 confirmation depth)")
	if env.svc.lastProcessedBlock != 1008 || len(env.sender.sent()) != 3 {
		t.Fatalf("lastProcessedBlock = %d, %d batches", env.svc.lastProcessedBlock, len(env.sender.sent()))
	}
}

func TestPollDoesNotAdvancePastAFailedModule(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	module2 := common.HexToAddress("0x4d0D0000000000000000000000000000000000e2")
	newModuleState().install(env.chain, module2)
	reg := registry
	env.svc.cfg.RegistryAddress = &reg
	env.chain.Return(registry, &contracts.RegistryABI, "getActiveModules", []common.Address{module, module2})
	env.chain.addLog(transferLog(t, subA, tokenX, e18(1), e18(1), 950))
	env.chain.failLogs = func(q ethereum.FilterQuery) error {
		if q.Addresses[0] == module2 {
			return errRPC
		}
		return nil
	}

	env.svc.Poll(ctx)
	env.wantLogs(
		"Error polling for events: getLogs blocks 899-948: rpc: rate limited",
		// Unlike the TS, the transaction already sent is still awaited.
		"Transaction results: 1 confirmed, 0 failed",
	)
	if env.svc.lastProcessedBlock != 898 {
		t.Fatalf("lastProcessedBlock = %d, want 898 (unchanged)", env.svc.lastProcessedBlock)
	}

	env.chain.failLogs = nil
	env.logs.reset()
	env.svc.Poll(ctx)
	env.wantLogs("Polling blocks 899 to 998")
	if env.svc.lastProcessedBlock != 998 {
		t.Fatalf("lastProcessedBlock = %d", env.svc.lastProcessedBlock)
	}
}

func TestReorgRewindsToTheOldestChangedBlock(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.chain.addLog(transferLog(t, subA, tokenX, e18(1), e18(1), 950))
	env.chain.addLog(transferLog(t, subB, tokenX, e18(1), e18(1), 960))
	env.svc.Poll(ctx)
	if env.svc.lastProcessedBlock != 998 {
		t.Fatalf("lastProcessedBlock = %d", env.svc.lastProcessedBlock)
	}

	env.chain.reorg(950, 960)
	env.logs.reset()
	before := len(historyRanges(env.chain))
	env.svc.Poll(ctx)
	env.wantLogs(
		"Reorg detected at block 960",
		"Reorg detected at block 950",
		// Both blocks in one poll (the TS needed one poll per block).
		"Reorg detected! Rewinding from block 998 to 949",
		"Polling blocks 950 to 998 (49 blocks, 2 confirmation depth)",
		"Found 0 protocol events, 2 transfer events, 0 limits-set events",
	)
	if env.svc.lastProcessedBlock != 998 {
		t.Fatalf("lastProcessedBlock = %d", env.svc.lastProcessedBlock)
	}
	// The acquired-token histories were read again from scratch.
	if got := historyRanges(env.chain)[before]; got != [2]uint64{500, 550} {
		t.Fatalf("first history range %v, want a full scan", got)
	}

	// The new hashes are recorded: no reorg on the next poll.
	env.logs.reset()
	env.svc.Poll(ctx)
	if env.logs.has("Reorg") {
		t.Fatalf("logs:\n%s", env.logs)
	}
}

func TestPollAndRefreshShareOneLock(t *testing.T) {
	env := newTestEnv(t)
	env.svc.mu.Lock()
	env.svc.Poll(context.Background())
	if env.logs.String() != "" {
		t.Fatalf("a skipped poll logged:\n%s", env.logs)
	}
	env.svc.Refresh(context.Background())
	env.svc.mu.Unlock()
	if env.logs.String() != "Skipping cron refresh - another operation is running" {
		t.Fatalf("logs:\n%s", env.logs)
	}
}

func TestLastUpdateIsRecordedOnlyOnConfirmedSuccess(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA}
	env.module.allowance[subA] = e18(1000) // = 10% of $10,000: no change

	env.sender.revert = func(sentBatch) bool { return true }
	env.svc.Refresh(ctx)
	env.wantLogs(
		"Preparing batch update: subAccount="+lower(subA),
		"REVERTED on-chain in block 1001 ("+lower(subA)+") — gasUsed=50000, reason=execution reverted",
		"failed for "+lower(subA)+": transaction reverted on-chain",
		"Transaction results: 0 confirmed, 1 failed",
	)
	if _, ok := env.svc.lastUpdate[updateKey{module, subA}]; ok {
		t.Fatal("a reverted batch recorded the last update")
	}

	env.sender.revert = nil
	env.svc.Refresh(ctx) // still stale: sent again
	if len(env.sender.sent()) != 2 || env.svc.lastUpdate[updateKey{module, subA}] != now {
		t.Fatalf("%d batches, last update %d", len(env.sender.sent()), env.svc.lastUpdate[updateKey{module, subA}])
	}
	env.logs.reset()
	env.svc.Refresh(ctx) // fresh and unchanged
	if len(env.sender.sent()) != 2 {
		t.Fatalf("sent a batch with nothing to update; logs:\n%s", env.logs)
	}
	env.wantLogs("Skipping batch update - no changes needed:")
}

func TestDryRunRecordsOnlyUpdatesThatWouldSucceed(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA}
	env.svc.sender = ethx.NewDryRunSender(env.chain, oracle, 0, env.logs.logf)

	// batchUpdate has no handler: its simulation reverts, and production
	// would have mined a reverting transaction.
	env.svc.Refresh(ctx)
	env.wantLogs("DRY RUN: not sending batchUpdate", "would revert: ", "REVERTED on-chain", "Transaction results: 0 confirmed, 1 failed")
	if _, ok := env.svc.lastUpdate[updateKey{module, subA}]; ok {
		t.Fatal("a batch that would revert recorded the last update")
	}

	env.chain.Return(module, &contracts.ModuleABI, "batchUpdate")
	env.logs.reset()
	env.svc.Refresh(ctx)
	env.wantLogs("simulation ok", "Transaction results: 1 confirmed, 0 failed")
	if env.svc.lastUpdate[updateKey{module, subA}] != now {
		t.Fatalf("last update %d", env.svc.lastUpdate[updateKey{module, subA}])
	}
}

func TestRefreshVisitsEverySubaccountOnce(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	other := common.HexToAddress("0x4d0D0000000000000000000000000000000000e2")
	broken := common.HexToAddress("0x4d0D0000000000000000000000000000000000e3")
	env.chain.Return(other, &contracts.ModuleABI, "authorizedOracle", common.HexToAddress("0xbad"))
	env.chain.Return(broken, &contracts.ModuleABI, "authorizedOracle", oracle) // its roles revert
	reg := registry
	env.svc.cfg.RegistryAddress = &reg
	env.chain.Return(registry, &contracts.RegistryABI, "getActiveModules", []common.Address{other, broken, module})
	env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA, subB}
	env.module.roles[contracts.DefiTransferRole] = []common.Address{subB, subC}
	env.module.roles[contracts.DefiRepayRole] = []common.Address{subA, module}

	env.svc.Refresh(ctx)
	if got := env.sender.subAccounts(); !slices.Equal(got, []common.Address{subA, subB, subC}) {
		t.Fatalf("processed %v; logs:\n%s", got, env.logs)
	}
	env.wantLogs(
		"=== Spending Oracle: Periodic Refresh ===",
		"Processing 3 module(s)",
		"Skipping module "+other.Hex(),
		// The TS abandoned the whole refresh here.
		"Error processing module "+broken.Hex()+": getSubaccountsByRole(1)",
		"Found 4 active subaccounts",
		"Processing subaccount: "+lower(subA),
		"Skipping "+lower(module)+" - this is the module address, not a subaccount",
		"Transaction results: 3 confirmed, 0 failed",
		"=== Periodic Refresh Complete ===",
	)
}

func TestProcessSubaccountBuildsStateFromItsEvents(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	feedX, feedY := common.HexToAddress("0xfee1"), common.HexToAddress("0xfee2")
	env.svc = New(&config.Config{
		Chain: config.Chain{Tokens: []config.Token{
			{Address: tokenX, PriceFeed: feedX, Symbol: "X", Type: config.TokenERC20},
			{Address: tokenY, PriceFeed: feedY, Symbol: "Y", Type: config.TokenERC20},
		}},
		ModuleAddress:                     env.svc.cfg.ModuleAddress,
		BlocksToLookBack:                  100,
		ConfirmationBlocks:                2,
		MaxBlocksPerQuery:                 50,
		MaxHistoricalBlocks:               500,
		AllowanceIncreaseThresholdBps:     big.NewInt(200),
		SpendingOracleMaxStalenessSeconds: 2700,
	}, env.chain, env.sender, env.logs.logf)
	env.svc.Now = func() time.Time { return time.Unix(now, 0) }
	for _, f := range []common.Address{feedX, feedY} {
		env.chain.Return(f, &contracts.ChainlinkABI, "latestRoundData", big.NewInt(1), big.NewInt(1e8), big.NewInt(0), big.NewInt(0), big.NewInt(1))
		env.chain.Return(f, &contracts.ChainlinkABI, "decimals", uint8(8))
	}
	env.module.limits[2] = big.NewInt(100) // a 100s window

	// 200s ago: outside the window. 20s ago: inside.
	env.chain.addLog(protocolLog(t, subA, contracts.OpSwap, []common.Address{tokenZ}, []*big.Int{e18(9)},
		[]common.Address{tokenX}, []*big.Int{e18(9)}, e18(9), 900))
	env.chain.addLog(protocolLog(t, subA, contracts.OpSwap, []common.Address{tokenX}, []*big.Int{e18(100)},
		[]common.Address{tokenY}, []*big.Int{e18(50)}, e18(10), 990))
	env.chain.addLog(protocolLog(t, subB, contracts.OpSwap, []common.Address{tokenX}, []*big.Int{e18(1)},
		[]common.Address{tokenZ}, []*big.Int{e18(1)}, e18(500), 991)) // another subaccount

	var pending []pendingTx
	if err := env.svc.processSubaccount(ctx, module, subA, 998, &pending); err != nil {
		t.Fatal(err)
	}
	env.svc.waitPending(ctx, pending)
	sent := env.sender.sent()
	if len(sent) != 1 {
		t.Fatalf("%d batches; logs:\n%s", len(sent), env.logs)
	}
	// $1000 limit - $10 spent in the window; tokenY acquired 20s ago.
	if b := sent[0]; b.newAllowance.Cmp(e18(990)) != 0 || !slices.Equal(b.tokens, []common.Address{tokenY}) || b.balances[0].Cmp(e18(50)) != 0 {
		t.Fatalf("batch %+v; logs:\n%s", b, env.logs)
	}
	env.wantLogs("Built price cache for 2 tokens", "Processing 2 events for "+lower(subA))

	// Its event queries are filtered on the subaccount, over 2x the lookback.
	for _, q := range env.chain.queries {
		if q.Topics[0][0] == contracts.ProtocolExecutionTopic && (len(q.Topics) < 2 || q.Topics[1][0] != topic(subA) || q.FromBlock.Uint64() < 798) {
			t.Fatalf("query %+v", q)
		}
	}
}

func TestRunRefreshesFirstThenPolls(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	env.svc.Run(ctx, schedule.Interval(time.Hour))
	refreshed, polled := env.logs.index("=== Periodic Refresh Complete ==="), env.logs.index("Polling blocks 899 to 998")
	if refreshed < 0 || polled < refreshed {
		t.Fatalf("logs:\n%s", env.logs)
	}
	if ctx.Err() == nil {
		t.Fatal("Run returned before its context ended")
	}
}

// cancelOnQueryFor cancels ctx when sub's own events are first queried.
func cancelOnQueryFor(env *testEnv, sub common.Address, cancel context.CancelFunc) {
	env.chain.failLogs = func(q ethereum.FilterQuery) error {
		if len(q.Topics) > 1 && q.Topics[1][0] == topic(sub) {
			cancel()
		}
		return nil
	}
}

func TestShutdownInterruptsRefresh(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cancelAt common.Address
		wantSent []common.Address
	}{
		{"during the first subaccount", subA, nil},
		// The last one: the refresh used to log its completion.
		{"during the last subaccount", subB, []common.Address{subA}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA, subB}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelOnQueryFor(env, tc.cancelAt, cancel)

			env.svc.Refresh(ctx)
			if got := env.sender.subAccounts(); !slices.Equal(got, tc.wantSent) {
				t.Fatalf("sent %v, want %v; logs:\n%s", got, tc.wantSent, env.logs)
			}
			env.wantLogs("Error in periodic refresh: context canceled")
			if env.logs.has("=== Periodic Refresh Complete ===") {
				t.Fatalf("an interrupted refresh logged its completion:\n%s", env.logs)
			}
			if tc.cancelAt == subA && env.logs.has("Processing subaccount: "+lower(subB)) {
				t.Fatalf("processed a subaccount after the shutdown:\n%s", env.logs)
			}
		})
	}
}

func TestShutdownInterruptsPoll(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cancelAt common.Address
	}{
		{"during the first subaccount", subA},
		{"during the last subaccount", subB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.chain.addLog(transferLog(t, subA, tokenX, e18(1), e18(1), 950))
			env.chain.addLog(transferLog(t, subB, tokenX, e18(1), e18(1), 960))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelOnQueryFor(env, tc.cancelAt, cancel)

			env.svc.Poll(ctx)
			env.wantLogs("Error polling for events: context canceled")
			// The blocks were not all processed.
			if env.svc.lastProcessedBlock != 898 {
				t.Fatalf("lastProcessedBlock = %d, want 898 (unchanged)", env.svc.lastProcessedBlock)
			}
			if tc.cancelAt != subA {
				return
			}
			for _, q := range env.chain.queries {
				if len(q.Topics) > 1 && q.Topics[1][0] == topic(subB) {
					t.Fatalf("processed a subaccount after the shutdown:\n%s", env.logs)
				}
			}
		})
	}
}

func TestReorgNeverMovesThePollForward(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA}
	env.svc.Poll(ctx) // up to block 998

	// A refresh records block 1050, above the polled blocks; subB's event
	// at 1020 is the next poll's to find.
	env.chain.setHead(1100)
	env.chain.addLog(transferLog(t, subA, tokenX, e18(1), e18(1), 1050))
	env.chain.addLog(transferLog(t, subB, tokenX, e18(1), e18(1), 1020))
	env.svc.Refresh(ctx)
	env.chain.reorg(1050)
	env.logs.reset()
	env.svc.Poll(ctx)
	// Rewinding to 1049 would skip blocks 999 to 1049.
	env.wantLogs("Reorg detected at block 1050", "Polling blocks 999 to 1098")
	if !slices.Contains(env.sender.subAccounts(), subB) {
		t.Fatalf("subB's event was skipped; logs:\n%s", env.logs)
	}
}

func TestReorgThatRemovesAnEventRewindsOnce(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.chain.addLog(transferLog(t, subA, tokenX, e18(1), e18(1), 950))
	env.svc.Poll(ctx)

	// The reorg drops the event: block 950 is read no more, so its old hash
	// must be forgotten, or every poll finds the same reorg again.
	env.chain.reorg(950)
	env.chain.removeLogs(func(l types.Log) bool { return l.BlockNumber == 950 })
	env.svc.Poll(ctx)
	env.wantLogs("Reorg detected at block 950")
	env.logs.reset()
	env.svc.Poll(ctx)
	if env.logs.has("Reorg") {
		t.Fatalf("the reorg was detected again:\n%s", env.logs)
	}
}

func TestReorgCheckCoversTwiceTheConfirmationDepth(t *testing.T) {
	// CONFIRMATION_BLOCKS is 2: the newest 4 recorded blocks are checked,
	// as in the TS.
	for _, tc := range []struct {
		reorged  uint64
		detected bool
	}{{960, true}, {950, false}} {
		env := newTestEnv(t)
		ctx := context.Background()
		for _, b := range []uint64{950, 960, 970, 980, 990} {
			env.chain.addLog(transferLog(t, subA, tokenX, e18(1), e18(1), b))
		}
		env.svc.Poll(ctx)
		env.chain.reorg(tc.reorged)
		env.logs.reset()
		env.svc.Poll(ctx)
		if got := env.logs.has("Reorg detected at block"); got != tc.detected {
			t.Fatalf("reorg of block %d detected: %v, want %v; logs:\n%s", tc.reorged, got, tc.detected, env.logs)
		}
	}
}

func TestAWaitFailureDoesNotRecordTheLastUpdate(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA}
	env.sender.waitErr = errors.New("transaction 0xabc not mined within 2m0s")
	env.svc.Refresh(ctx)
	env.wantLogs("failed for "+lower(subA)+": transaction 0xabc not mined within 2m0s", "Transaction results: 0 confirmed, 1 failed")
	if _, ok := env.svc.lastUpdate[updateKey{module, subA}]; ok {
		t.Fatal("an unconfirmed batch recorded the last update")
	}
}

func TestLastUpdateIsWhenTheBatchWasPrepared(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA}
	clock := int64(now)
	env.svc.Now = func() time.Time { return time.Unix(clock, 0) }
	env.sender.onWait = func() { clock += 100 } // the receipt takes 100s
	env.svc.Refresh(ctx)
	if got := env.svc.lastUpdate[updateKey{module, subA}]; got != now {
		t.Fatalf("last update %d, want %d (when the values were read)", got, now)
	}
}

func TestProcessSubaccountReadsTwiceTheLookback(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// Block 850 is outside one BLOCKS_TO_LOOK_BACK (100) of block 998, but
	// within two: its acquisition still counts.
	env.chain.addLog(protocolLog(t, subA, contracts.OpSwap, []common.Address{tokenZ}, []*big.Int{e18(5)},
		[]common.Address{tokenX}, []*big.Int{e18(5)}, e18(5), 850))
	var pending []pendingTx
	if err := env.svc.processSubaccount(ctx, module, subA, 998, &pending); err != nil {
		t.Fatal(err)
	}
	env.svc.waitPending(ctx, pending)
	sent := env.sender.sent()
	if len(sent) != 1 || !slices.Equal(sent[0].tokens, []common.Address{tokenX}) || sent[0].balances[0].Cmp(e18(5)) != 0 {
		t.Fatalf("batches %+v; logs:\n%s", sent, env.logs)
	}
	from := uint64(headBlock)
	for _, q := range env.chain.queries {
		if q.Topics[0][0] == contracts.ProtocolExecutionTopic {
			from = min(from, q.FromBlock.Uint64())
		}
	}
	if from != 798 {
		t.Fatalf("protocol events read from block %d, want 798", from)
	}
}

func TestWindowsBeyondInt64CoverEveryEvent(t *testing.T) {
	// The on-chain window is a uint256 with no upper bound.
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, window := range []*big.Int{new(big.Int).Lsh(big.NewInt(1), 63), new(big.Int).Lsh(big.NewInt(1), 64), maxUint256} {
		env := newTestEnv(t)
		ctx := context.Background()
		env.module.limits[2] = window
		env.chain.addLog(transferLog(t, subA, tokenX, e18(10), e18(10), 900))
		var pending []pendingTx
		if err := env.svc.processSubaccount(ctx, module, subA, 998, &pending); err != nil {
			t.Fatal(err)
		}
		env.svc.waitPending(ctx, pending)
		// 10% of $10,000, less the $10 spent.
		if sent := env.sender.sent(); len(sent) != 1 || sent[0].newAllowance.Cmp(e18(990)) != 0 {
			t.Fatalf("window %s: batches %+v; logs:\n%s", window, sent, env.logs)
		}
	}
}

func TestAPanicFailsOnlyItsSubaccountOrPoll(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.module.roles[contracts.DefiExecuteRole] = []common.Address{subA, subB}
	// A panic in a goroutine reading subA's events.
	env.chain.failLogs = func(q ethereum.FilterQuery) error {
		if q.Topics[0][0] == contracts.ProtocolExecutionTopic && len(q.Topics) > 1 && q.Topics[1][0] == topic(subA) {
			panic("boom")
		}
		return nil
	}
	env.svc.Refresh(ctx)
	env.wantLogs("Error processing "+lower(subA)+": panic: boom", "=== Periodic Refresh Complete ===")
	if got := env.sender.subAccounts(); !slices.Equal(got, []common.Address{subB}) {
		t.Fatalf("processed %v; logs:\n%s", got, env.logs)
	}

	// A panic in a goroutine of the poll's event query, then in the poll
	// itself.
	env.chain.failLogs = func(q ethereum.FilterQuery) error {
		if len(q.Topics) == 1 {
			panic("boom in a query")
		}
		return nil
	}
	env.svc.Poll(ctx)
	env.wantLogs("Error polling for events: panic: boom in a query")
	env.chain.failLogs = nil
	env.chain.On(module, &contracts.ModuleABI, "authorizedOracle", func([]any) ([]any, error) { panic("boom in the poll") })
	env.svc.Poll(ctx)
	env.wantLogs("Error polling for events: panic: boom in the poll")
	if env.svc.lastProcessedBlock != 898 { // where the first poll started
		t.Fatalf("lastProcessedBlock = %d after failed polls", env.svc.lastProcessedBlock)
	}
	env.chain.Return(module, &contracts.ModuleABI, "authorizedOracle", oracle)
	env.svc.Poll(ctx)
	if env.svc.lastProcessedBlock != 998 {
		t.Fatalf("lastProcessedBlock = %d after a poll that worked", env.svc.lastProcessedBlock)
	}
}

func TestHistoricalTokensInterruptedDuringTheLastChunk(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The shutdown makes the scan's last chunk fail.
	env.chain.failLogs = func(q ethereum.FilterQuery) error {
		if q.ToBlock.Uint64() == headBlock {
			cancel()
			return context.Canceled
		}
		return nil
	}
	if _, err := env.svc.historicalTokens(ctx, module, subA); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if _, ok := env.svc.history[updateKey{module, subA}]; ok {
		t.Fatal("kept an incomplete history")
	}
}
