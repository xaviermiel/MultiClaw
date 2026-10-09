package spendingoracle

import (
	"context"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
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

	t.Run("failed chunks are retried in halves, failed halves skipped", func(t *testing.T) {
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
			"Sub-chunk query failed (blocks 401-601), skipping",
			"Historical token query complete: 2 unique tokens found in 3 events (2 queries)",
		)
	})

	t.Run("chunks under 200 blocks are not split", func(t *testing.T) {
		env := newTestEnv(t)
		env.svc.cfg.MaxBlocksPerQuery = 150
		env.svc.cfg.MaxHistoricalBlocks = 150
		env.chain.failLogs = func(ethereum.FilterQuery) error { return errRPC }
		if got, err := env.svc.historicalTokens(ctx, module, subA); err != nil || len(got) != 0 {
			t.Fatalf("tokens %v (%v)", got, err)
		}
		env.wantLogs("Chunk too small to retry, skipping blocks 850-1000")
	})
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
	if !env.logs.has("=== Periodic Refresh Complete ===") || !env.logs.has("Polling blocks 899 to 998") {
		t.Fatalf("logs:\n%s", env.logs)
	}
	if ctx.Err() == nil {
		t.Fatal("Run returned before its context ended")
	}
}
