package safevalue

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

const now = 1_700_000_000

// fakeSender records what the service sends.
type fakeSender struct {
	sent    []*ethx.SentTx
	status  uint64
	sendErr error
	panics  bool
}

func newFakeSender() *fakeSender { return &fakeSender{status: types.ReceiptStatusSuccessful} }

func (f *fakeSender) Address() common.Address { return oracle }

func (f *fakeSender) Send(_ context.Context, to common.Address, data []byte) (*ethx.SentTx, error) {
	if f.panics {
		panic("boom")
	}
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	tx := &ethx.SentTx{Hash: common.BigToHash(big.NewInt(int64(len(f.sent) + 1))), From: oracle, To: to, Data: data}
	f.sent = append(f.sent, tx)
	return tx, nil
}

func (f *fakeSender) Wait(context.Context, *ethx.SentTx) (*types.Receipt, error) {
	return &types.Receipt{Status: f.status, BlockNumber: big.NewInt(4242), GasUsed: 31337}, nil
}

// serviceChain is a module (ours) whose Safe holds `value` worth of ETH
// at $1 (so wei = USD with 18 decimals), with the given stored value.
func serviceChain(value *big.Int, stored *big.Int, lastUpdated int64) *fakeChain {
	c := newFakeChain()
	c.balances[safe] = value
	c.Return(module, &contracts.ModuleABI, "authorizedOracle", oracle)
	c.Return(module, &contracts.ModuleABI, "avatar", safe)
	c.Return(module, &contracts.ModuleABI, "getSafeValue", stored, big.NewInt(lastUpdated), big.NewInt(3))
	feed(c.Fake, ethFeed, e(1, 8), 8)
	return c
}

func newTestService(c *fakeChain, sender ethx.Sender, l *logs) *Service {
	cfg := &config.Config{
		Chain:                        config.Chain{ETHPriceFeed: ethFeed},
		ModuleAddress:                ptr(module),
		SafeValueChangeThresholdBps:  big.NewInt(100),
		SafeValueMaxStalenessSeconds: 2700,
	}
	s := NewService(cfg, c, sender, l.logf)
	s.Now = func() time.Time { return time.Unix(now, 0) }
	return s
}

func TestUpdateDecision(t *testing.T) {
	stored := e(1000, 18)
	fresh := int64(now - 100)
	cases := []struct {
		name        string
		value       *big.Int
		stored      *big.Int
		lastUpdated int64
		write       bool
		wantLog     string
	}{
		{"zero value is never written", big.NewInt(0), stored, 0, false, "Skipping write - total value is 0"},
		{"exactly +1% stays", e(1010, 18), stored, fresh, false, "  Diff: $10 (100bps, threshold: 100bps)"},
		{"just over +1% writes", bn.Add(e(1010, 18), big.NewInt(1)), stored, fresh, true, "Updating due to value change >1% (diff: $10.000000000000000001)"},
		{"exactly -1% stays", e(990, 18), stored, fresh, false, "Skipping update - value change within threshold:"},
		{"just over -1% writes", bn.Sub(e(990, 18), big.NewInt(1)), stored, fresh, true, "Updating due to value change >1%"},
		{"unchanged but exactly max age stays", stored, stored, now - 2700, false, "  Time since update: 2700s (max: 2700s)"},
		{"unchanged but stale writes", stored, stored, now - 2701, true, "Updating due to staleness (2701s > 2700s)"},
		{"first value writes", big.NewInt(1), big.NewInt(0), fresh, true, "Updating due to value change >1%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := serviceChain(tc.value, tc.stored, tc.lastUpdated)
			sender := newFakeSender()
			l := &logs{}
			newTestService(c, sender, l).RunOnce(context.Background())

			if got := len(sender.sent) == 1; got != tc.write {
				t.Fatalf("wrote %d transactions, want write=%v; logs %q", len(sender.sent), tc.write, l.lines)
			}
			if !l.has(tc.wantLog) {
				t.Fatalf("logs %q lack %q", l.lines, tc.wantLog)
			}
			if tc.write {
				want, _ := contracts.PackUpdateSafeValue(tc.value)
				if tx := sender.sent[0]; tx.To != module || !bytes.Equal(tx.Data, want) {
					t.Fatalf("sent %x to %s", tx.Data, tx.To.Hex())
				}
				if !l.has("Transaction confirmed in block 4242") {
					t.Fatalf("logs %q", l.lines)
				}
			}
		})
	}
}

func TestUnreadableOnChainValueWritesAsIfStale(t *testing.T) {
	c := serviceChain(e(5, 18), nil, 0)
	c.Revert(module, &contracts.ModuleABI, "getSafeValue", nil)
	sender := newFakeSender()
	l := &logs{}
	newTestService(c, sender, l).RunOnce(context.Background())
	if len(sender.sent) != 1 || !l.has("Error reading on-chain safe value") || !l.has("Updating due to staleness (1700000000s > 2700s)") {
		t.Fatalf("%d sent, logs %q", len(sender.sent), l.lines)
	}
}

func TestRevertedWriteIsReportedAsAFailure(t *testing.T) {
	c := serviceChain(e(5, 18), big.NewInt(0), 0)
	moduleErr := argumentlessError(t)
	c.Revert(module, &contracts.ModuleABI, "updateSafeValue", moduleErr.ID[:4])
	sender := newFakeSender()
	sender.status = types.ReceiptStatusFailed
	l := &logs{}
	newTestService(c, sender, l).RunOnce(context.Background())
	for _, want := range []string{
		"Error writing safe value: transaction",
		"reverted on-chain in block 4242 (gasUsed=31337, reason=" + moduleErr.Name + ")",
		"Error processing module " + module.Hex(),
		"=== Safe Value Monitor: Complete ===",
	} {
		if !l.has(want) {
			t.Errorf("logs %q lack %q", l.lines, want)
		}
	}
	if l.has("Transaction confirmed") {
		t.Fatal("a reverted transaction was logged as confirmed")
	}
}

func TestModulesAreIsolated(t *testing.T) {
	other, foreign := addr(0x40e), addr(0x40f)
	c := serviceChain(e(5, 18), big.NewInt(0), 0)
	c.Return(foreign, &contracts.ModuleABI, "authorizedOracle", addr(0xbad))
	c.Return(other, &contracts.ModuleABI, "authorizedOracle", oracle)
	registry := addr(0x5e9)
	c.Return(registry, &contracts.RegistryABI, "getActiveModules", []common.Address{other, foreign, module})

	sender := newFakeSender()
	sender.sendErr = errors.New("insufficient funds for gas")
	l := &logs{}
	s := newTestService(c, sender, l)
	s.registry = ptr(registry)
	s.RunOnce(context.Background())

	// other: no avatar() -> error; foreign: skipped; module: send fails.
	for _, want := range []string{
		"Found 3 active modules in registry",
		"Error processing module " + other.Hex() + ": read the Safe of module",
		"Skipping module " + foreign.Hex(),
		"Error writing safe value: insufficient funds for gas",
		"=== Safe Value Monitor: Complete ===",
	} {
		if !l.has(want) {
			t.Errorf("logs %q lack %q", l.lines, want)
		}
	}

	panicking := newFakeSender()
	panicking.panics = true
	l = &logs{}
	newTestService(serviceChain(e(5, 18), big.NewInt(0), 0), panicking, l).RunOnce(context.Background())
	if !l.has("Error processing module "+module.Hex()+": panic: boom") || !l.has("=== Safe Value Monitor: Complete ===") {
		t.Fatalf("logs %q", l.lines)
	}
}

func TestDryRunNeverSignsButLogsTheWrite(t *testing.T) {
	c := serviceChain(e(5, 18), big.NewInt(0), 0)
	c.Return(module, &contracts.ModuleABI, "updateSafeValue")
	l := &logs{}
	newTestService(c, ethx.NewDryRunSender(c, oracle, 0, l.logf), l).RunOnce(context.Background())
	for _, want := range []string{
		"Writing Safe value to chain: 5000000000000000000 ($5 USD) for module " + module.Hex(),
		"DRY RUN: not sending updateSafeValue to " + module.Hex() + " (36 bytes of calldata); at block 777: simulation ok",
		"Transaction confirmed in block 777",
	} {
		if !l.has(want) {
			t.Errorf("logs %q lack %q", l.lines, want)
		}
	}
}

func TestDryRunReportsAWriteThatWouldRevert(t *testing.T) {
	moduleErr := argumentlessError(t)
	c := serviceChain(e(5, 18), big.NewInt(0), 0)
	c.Revert(module, &contracts.ModuleABI, "updateSafeValue", moduleErr.ID[:4])
	l := &logs{}
	newTestService(c, ethx.NewDryRunSender(c, oracle, 0, l.logf), l).RunOnce(context.Background())
	for _, want := range []string{
		"at block 777: would revert: " + moduleErr.Name,
		"Error writing safe value: transaction ",
		"reverted on-chain in block 777 (gasUsed=0, reason=" + moduleErr.Name + ")",
	} {
		if !l.has(want) {
			t.Errorf("logs %q lack %q", l.lines, want)
		}
	}
	if l.has("Transaction confirmed") {
		t.Fatalf("a write that would revert was reported as confirmed: %q", l.lines)
	}
}

// argumentlessError returns a module error without inputs (the first by
// name, so runs are reproducible).
func argumentlessError(t *testing.T) abi.Error {
	t.Helper()
	for _, name := range slices.Sorted(maps.Keys(contracts.ModuleABI.Errors)) {
		if e := contracts.ModuleABI.Errors[name]; len(e.Inputs) == 0 {
			return e
		}
	}
	t.Fatal("no module error without inputs")
	return abi.Error{}
}

// go-ethereum's packer would write a total above 2^256-1 modulo 2^256; viem
// threw, so the TS wrote nothing.
func TestATotalAboveUint256IsNotWritten(t *testing.T) {
	sender := newFakeSender()
	l := &logs{}
	newTestService(serviceChain(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(0), 0), sender, l).RunOnce(context.Background())
	if len(sender.sent) != 0 || !l.has("Error processing module "+module.Hex()+": updateSafeValue: ") || !l.has("out of the uint256 range") {
		t.Fatalf("sent %d; logs %q", len(sender.sent), l.lines)
	}
}

// A shutdown during the valuation makes the reads left count as 0: the
// partial total must not be compared, logged or written.
func TestShutdownDuringTheValuationWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := serviceChain(e(5, 18), e(100, 18), now-100)
	c.On(module, &contracts.ModuleABI, "getSafeValue", func([]any) ([]any, error) {
		cancel()
		return []any{e(100, 18), big.NewInt(now - 100), big.NewInt(3)}, nil
	})
	sender := newFakeSender() // ignores ctx
	l := &logs{}
	newTestService(c, sender, l).RunOnce(ctx)
	if len(sender.sent) != 0 || l.has("Total USD Value") || l.has("Writing Safe value") {
		t.Fatalf("sent %d; logs %q", len(sender.sent), l.lines)
	}
}

func TestOnlyTheModulesOfTheOraclesAccessPolicyAreValued(t *testing.T) {
	stranger := addr(0x40e)
	c := serviceChain(e(5, 18), big.NewInt(0), 0)
	c.Return(stranger, &contracts.ModuleABI, "authorizedOracle", oracle)
	registry := addr(0x5e9)
	c.Return(registry, &contracts.RegistryABI, "getActiveModules", []common.Address{stranger, module})
	// The oracle restricted itself: the stranger's module, which names it too, isn't served.
	c.On(registry, &contracts.RegistryABI, "getModulesServedBy", func(args []any) ([]any, error) {
		if args[0].(common.Address) != oracle {
			t.Errorf("policy of %s read, want the oracle's", args[0])
		}
		return []any{[]common.Address{module}}, nil
	})

	sender := newFakeSender()
	l := &logs{}
	s := newTestService(c, sender, l)
	s.registry = ptr(registry)
	s.RunOnce(context.Background())
	if !l.has("Found 1 active modules in registry served by this oracle") || len(sender.sent) != 1 || sender.sent[0].To != module {
		t.Fatalf("sent %+v; logs %q", sender.sent, l.lines)
	}
	for _, call := range c.Calls() {
		if call.To == stranger {
			t.Fatalf("read the stranger's module: %+v", call)
		}
	}
}
