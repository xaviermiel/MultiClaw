package spendingoracle

import (
	"context"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/spending"
)

func limits(bps, usd int64) contracts.SubAccountLimits {
	return contracts.SubAccountLimits{MaxSpendingBps: big.NewInt(bps), MaxSpendingUSD: e18(usd), WindowDuration: big.NewInt(86400)}
}

func TestAllowance(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name     string
		limits   contracts.SubAccountLimits
		spent    *big.Int
		legacy   *big.Int // absoluteMaxSpendingBps; nil when the getter reverts
		want     *big.Int
		wantLogs []string
	}{
		{
			name: "BPS mode", limits: limits(1000, 0), spent: e18(250), want: e18(750),
			wantLogs: []string{"Allowance: safeValue=10000, mode=BPS, max=1000, spent=250, new=750"},
		},
		{
			name: "USD mode", limits: limits(1000, 300), spent: e18(250), want: e18(50),
			wantLogs: []string{"Allowance: safeValue=10000, mode=USD, max=300, spent=250, new=50"},
		},
		{name: "overspent floors at 0", limits: limits(1000, 0), spent: e18(1001), want: big.NewInt(0)},
		{
			name: "legacy cap below the limit", limits: limits(1000, 0), spent: e18(250), legacy: big.NewInt(500), want: e18(250),
			wantLogs: []string{"Allowance capped by absoluteMaxSpendingBps (500bps): 1000 -> 500", "mode=BPS, max=500, spent=250, new=250"},
		},
		{name: "legacy cap above the limit", limits: limits(1000, 0), spent: big.NewInt(0), legacy: big.NewInt(2000), want: e18(1000)},
		{name: "legacy cap applies in USD mode too", limits: limits(0, 900), spent: big.NewInt(0), legacy: big.NewInt(100), want: e18(100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.module.absoluteMaxBps = tc.legacy
			got, err := env.svc.allowance(ctx, module, tc.limits, tc.spent)
			if err != nil || got.Cmp(tc.want) != 0 {
				t.Fatalf("allowance = %v (%v), want %s", got, err, tc.want)
			}
			env.wantLogs(tc.wantLogs...)
		})
	}
}

func acquiredMap(pairs ...any) *spending.OrderedMap[common.Address, *big.Int] {
	m := spending.NewOrderedMap[common.Address, *big.Int]()
	for i := 0; i < len(pairs); i += 2 {
		m.Set(pairs[i].(common.Address), pairs[i+1].(*big.Int))
	}
	return m
}

func TestBatchDecision(t *testing.T) {
	const fresh = now - 100
	cases := []struct {
		name       string
		onChain    *big.Int
		newValue   *big.Int
		lastUpdate int64 // 0: never (a restart)
		acquired   *spending.OrderedMap[common.Address, *big.Int]
		onChainX   *big.Int
		wantReason string // "" when no batch is due
		wantLog    string
	}{
		{name: "any decrease", onChain: e18(1000), newValue: bn.Sub(e18(1000), big.NewInt(1)), lastUpdate: fresh,
			wantReason: "allowance decreased"},
		{name: "exactly +2% is within threshold", onChain: e18(1000), newValue: e18(1020), lastUpdate: fresh,
			wantLog: "  Allowance increase within threshold: 1000 -> 1020 (+200bps, threshold: 200bps)"},
		{name: "over +2%", onChain: e18(1000), newValue: bn.Add(e18(1020), big.NewInt(1)), lastUpdate: fresh,
			wantReason: "allowance increased >2%"},
		{name: "any increase from 0", onChain: big.NewInt(0), newValue: big.NewInt(1), lastUpdate: fresh,
			wantReason: "allowance increased >2%"},
		{name: "unchanged", onChain: e18(5), newValue: e18(5), lastUpdate: fresh,
			wantLog: "  Allowance unchanged: 5"},
		{name: "exactly max age", onChain: e18(5), newValue: e18(5), lastUpdate: now - 2700,
			wantLog: "  Staleness: 2700s (max: 2700s)"},
		{name: "stale", onChain: e18(5), newValue: e18(5), lastUpdate: now - 2701,
			wantReason: "stale (2701s > 2700s)"},
		{name: "first update after a restart", onChain: e18(5), newValue: e18(5),
			wantReason: "stale (1700000000s > 2700s)"},
		{name: "acquired balance changed", onChain: e18(5), newValue: e18(5), lastUpdate: fresh,
			acquired: acquiredMap(tokenX, e18(3)), onChainX: e18(2), wantReason: "acquired changed"},
		{name: "acquired balance unchanged", onChain: e18(5), newValue: e18(5), lastUpdate: fresh,
			acquired: acquiredMap(tokenX, e18(3)), onChainX: e18(3), wantLog: "  Acquired tokens: 1 (no changes)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			env.module.allowance[subA] = tc.onChain
			if tc.onChainX != nil {
				env.module.acquired[[2]common.Address{subA, tokenX}] = tc.onChainX
			}
			if tc.lastUpdate != 0 {
				env.svc.lastUpdate[updateKey{module, subA}] = tc.lastUpdate
			}
			acquired := tc.acquired
			if acquired == nil {
				acquired = acquiredMap()
			}
			b, err := env.svc.prepareBatch(context.Background(), module, subA, tc.newValue, acquired)
			if err != nil {
				t.Fatal(err)
			}
			if (b != nil) != (tc.wantReason != "") {
				t.Fatalf("batch = %v, want reason %q; logs:\n%s", b, tc.wantReason, env.logs)
			}
			if tc.wantReason != "" {
				env.wantLogs("Preparing batch update: subAccount="+lower(subA), "  Reason: "+tc.wantReason)
			} else {
				env.wantLogs("Skipping batch update - no changes needed:", tc.wantLog)
			}
		})
	}
}

func TestBatchTokensKeepStateOrderAndClearStaleBalances(t *testing.T) {
	env := newTestEnv(t)
	tokenW := common.HexToAddress("0x4444444444444444444444444444444444444444")
	env.module.allowance[subA] = e18(1000)
	env.module.allowanceVersion = 7
	env.module.acquired[[2]common.Address{subA, tokenX}] = e18(5) // unchanged
	env.module.acquired[[2]common.Address{subA, tokenZ}] = e18(9) // no longer supported by events
	// History: Z, X and W (W's on-chain balance is already 0).
	env.chain.addLog(acquiredLog(t, subA, tokenZ, e18(9), 900))
	env.chain.addLog(acquiredLog(t, subA, tokenX, e18(5), 910))
	env.chain.addLog(acquiredLog(t, subA, tokenW, e18(1), 920))
	env.chain.addLog(acquiredLog(t, subB, tokenY, e18(1), 930)) // another subaccount
	env.svc.lastUpdate[updateKey{module, subA}] = now - 100

	ctx := context.Background()
	b, err := env.svc.prepareBatch(ctx, module, subA, e18(1000), acquiredMap(tokenY, e18(7), tokenX, e18(5)))
	if err != nil || b == nil {
		t.Fatalf("batch %v (%v)", b, err)
	}
	if _, err := env.svc.submit(ctx, b); err != nil {
		t.Fatal(err)
	}
	sent := env.sender.sent()[0]
	if !slices.Equal(sent.tokens, []common.Address{tokenY, tokenX, tokenZ}) {
		t.Fatalf("tokens %v", sent.tokens)
	}
	wantBalances := []*big.Int{e18(7), e18(5), big.NewInt(0)}
	wantVersions := []*big.Int{big.NewInt(0x22), big.NewInt(0x11), big.NewInt(0x33)}
	for i := range wantBalances {
		if sent.balances[i].Cmp(wantBalances[i]) != 0 || sent.tokenVersions[i].Cmp(wantVersions[i]) != 0 {
			t.Fatalf("balances %v versions %v", sent.balances, sent.tokenVersions)
		}
	}
	if sent.module != module || sent.subAccount != subA || sent.allowanceVersion.Int64() != 7 || sent.newAllowance.Cmp(e18(1000)) != 0 {
		t.Fatalf("batch %+v", sent)
	}
	env.wantLogs(
		"  Clearing stale acquired balance for "+lower(tokenZ)+": 9000000000000000000 -> 0",
		"  Reason: acquired changed",
		"  Tokens: 3",
		"Transaction submitted: ",
	)
	if env.logs.has("Clearing stale acquired balance for " + lower(tokenW)) {
		t.Fatal("cleared a token whose on-chain balance is already 0")
	}
}
