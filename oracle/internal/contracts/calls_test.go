package contracts_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
)

var (
	moduleAddr   = common.HexToAddress("0x16D44268C4e3a567c5ff87cCCa10Fe46f8a656Bb")
	registryAddr = common.HexToAddress("0xB19a2AAf5472b1e49e35eEa14f4c0dD25D89Ab96")
	safeAddr     = common.HexToAddress("0x6E7692fFE42ca2A3FA2b08611AA7e79A2AaA8e8C")
	subAccount   = common.HexToAddress("0x4573A0C9428e56B0d4EEfcb43DCd99299BBD2e6c")
	tokenAddr    = common.HexToAddress("0x036CbD53842c5426634e7929541eC2318f3dCF7e")
	feedAddr     = common.HexToAddress("0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165")
)

func TestModuleReads(t *testing.T) {
	ctx := context.Background()
	f := contractstest.New().
		Return(moduleAddr, &contracts.ModuleABI, "getSafeValue", big.NewInt(255), big.NewInt(1700000000), big.NewInt(7)).
		Return(moduleAddr, &contracts.ModuleABI, "getSubAccountLimits", big.NewInt(500), big.NewInt(0), big.NewInt(86400)).
		Return(moduleAddr, &contracts.ModuleABI, "getSpendingAllowance", big.NewInt(25)).
		Return(moduleAddr, &contracts.ModuleABI, "getAcquiredBalance", big.NewInt(42)).
		Return(moduleAddr, &contracts.ModuleABI, "allowanceVersion", big.NewInt(3)).
		Return(moduleAddr, &contracts.ModuleABI, "acquiredBalanceVersion", big.NewInt(4)).
		Return(moduleAddr, &contracts.ModuleABI, "avatar", safeAddr).
		Return(moduleAddr, &contracts.ModuleABI, "authorizedOracle", common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")).
		On(moduleAddr, &contracts.ModuleABI, "getSubaccountsByRole", func(args []any) ([]any, error) {
			if args[0].(uint16) == contracts.DefiTransferRole {
				return []any{[]common.Address{subAccount}}, nil
			}
			return []any{[]common.Address{}}, nil
		}).
		On(moduleAddr, &contracts.ModuleABI, "getTokenBalances", func(args []any) ([]any, error) {
			tokens := args[0].([]common.Address)
			out := make([]*big.Int, len(tokens))
			for i := range tokens {
				out[i] = big.NewInt(int64(100 + i))
			}
			return []any{out}, nil
		})
	m := contracts.NewModule(moduleAddr, f)

	sv, err := m.GetSafeValue(ctx)
	if err != nil || sv.TotalValueUSD.Int64() != 255 || sv.LastUpdated.Int64() != 1700000000 || sv.UpdateCount.Int64() != 7 {
		t.Fatalf("GetSafeValue = %+v, %v", sv, err)
	}
	limits, err := m.GetSubAccountLimits(ctx, subAccount)
	if err != nil || limits.MaxSpendingBps.Int64() != 500 || limits.MaxSpendingUSD.Sign() != 0 || limits.WindowDuration.Int64() != 86400 {
		t.Fatalf("GetSubAccountLimits = %+v, %v", limits, err)
	}
	checkInt := func(name string, got *big.Int, err error, want int64) {
		t.Helper()
		if err != nil || got.Int64() != want {
			t.Errorf("%s = %v, %v; want %d", name, got, err, want)
		}
	}
	v, err := m.GetSpendingAllowance(ctx, subAccount)
	checkInt("GetSpendingAllowance", v, err, 25)
	v, err = m.GetAcquiredBalance(ctx, subAccount, tokenAddr)
	checkInt("GetAcquiredBalance", v, err, 42)
	v, err = m.AllowanceVersion(ctx, subAccount)
	checkInt("AllowanceVersion", v, err, 3)
	v, err = m.AcquiredBalanceVersion(ctx, subAccount, tokenAddr)
	checkInt("AcquiredBalanceVersion", v, err, 4)

	if a, err := m.Avatar(ctx); err != nil || a != safeAddr {
		t.Errorf("Avatar = %s, %v", a.Hex(), err)
	}
	if a, err := m.AuthorizedOracle(ctx); err != nil || a.Hex() != "0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E" {
		t.Errorf("AuthorizedOracle = %s, %v", a.Hex(), err)
	}
	subs, err := m.GetSubaccountsByRole(ctx, contracts.DefiTransferRole)
	if err != nil || len(subs) != 1 || subs[0] != subAccount {
		t.Errorf("GetSubaccountsByRole(2) = %v, %v", subs, err)
	}
	subs, err = m.GetSubaccountsByRole(ctx, contracts.DefiExecuteRole)
	if err != nil || len(subs) != 0 {
		t.Errorf("GetSubaccountsByRole(1) = %v, %v", subs, err)
	}
	bals, err := m.GetTokenBalances(ctx, []common.Address{tokenAddr, safeAddr})
	if err != nil || len(bals) != 2 || bals[0].Int64() != 100 || bals[1].Int64() != 101 {
		t.Errorf("GetTokenBalances = %v, %v", bals, err)
	}

	// Reads default to latest; At pins a block.
	m.At(big.NewInt(99)).Avatar(ctx)
	calls := f.Calls()
	if calls[0].Block != nil || calls[len(calls)-1].Block.Int64() != 99 {
		t.Errorf("block pinning not honored: first=%v last=%v", calls[0].Block, calls[len(calls)-1].Block)
	}
}

func TestLegacyAbsoluteMaxSpendingBps(t *testing.T) {
	ctx := context.Background()

	// Old deployment: the getter exists.
	old := contractstest.New().Return(moduleAddr, &contracts.LegacyModuleABI, "absoluteMaxSpendingBps", big.NewInt(2000))
	if v, err := contracts.NewModule(moduleAddr, old).AbsoluteMaxSpendingBps(ctx); err != nil || v.Int64() != 2000 {
		t.Fatalf("old deployment: %v, %v", v, err)
	}

	// Current deployment: no such function -> the call reverts without data.
	current := contractstest.New()
	_, err := contracts.NewModule(moduleAddr, current).AbsoluteMaxSpendingBps(ctx)
	var rev *contractstest.RevertError
	if !errors.As(err, &rev) {
		t.Fatalf("current deployment should revert, got %v", err)
	}
}

func TestEmptyReturnDataIsAnError(t *testing.T) {
	// e.g. calling an address without code: viem raises "returned no data".
	f := contractstest.New().On(tokenAddr, &contracts.ERC20ABI, "decimals", func([]any) ([]any, error) { return nil, nil })
	if _, err := contracts.NewERC20(tokenAddr, f).Decimals(context.Background()); err == nil {
		t.Fatal("expected an unpack error for empty return data")
	}
}

func TestRegistryAndExternalReads(t *testing.T) {
	ctx := context.Background()
	vault := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	pair := common.HexToAddress("0x00000000000000000000000000000000000000a2")
	f := contractstest.New().
		Return(registryAddr, &contracts.RegistryABI, "getActiveModules", []common.Address{moduleAddr}).
		Return(tokenAddr, &contracts.ERC20ABI, "decimals", uint8(6)).
		Return(tokenAddr, &contracts.ERC20ABI, "symbol", "USDC").
		Return(tokenAddr, &contracts.ERC20ABI, "balanceOf", big.NewInt(84161345)).
		Return(feedAddr, &contracts.ChainlinkABI, "latestRoundData", big.NewInt(1), big.NewInt(99988720), big.NewInt(2), big.NewInt(3), big.NewInt(1)).
		Return(feedAddr, &contracts.ChainlinkABI, "decimals", uint8(8)).
		On(vault, &contracts.ERC4626ABI, "convertToAssets", func(args []any) ([]any, error) {
			return []any{new(big.Int).Mul(args[0].(*big.Int), big.NewInt(2))}, nil
		}).
		Return(pair, &contracts.UniswapV2PairABI, "totalSupply", big.NewInt(1000)).
		Return(pair, &contracts.UniswapV2PairABI, "getReserves", big.NewInt(5000), big.NewInt(7000), uint32(123))

	mods, err := contracts.NewRegistry(registryAddr, f).GetActiveModules(ctx)
	if err != nil || len(mods) != 1 || mods[0] != moduleAddr {
		t.Fatalf("GetActiveModules = %v, %v", mods, err)
	}
	tok := contracts.NewERC20(tokenAddr, f)
	if d, err := tok.Decimals(ctx); err != nil || d != 6 {
		t.Errorf("Decimals = %d, %v", d, err)
	}
	if s, err := tok.Symbol(ctx); err != nil || s != "USDC" {
		t.Errorf("Symbol = %q, %v", s, err)
	}
	if b, err := tok.BalanceOf(ctx, safeAddr); err != nil || b.Int64() != 84161345 {
		t.Errorf("BalanceOf = %v, %v", b, err)
	}
	feed := contracts.NewPriceFeed(feedAddr, f)
	if rd, err := feed.LatestRoundData(ctx); err != nil || rd.Answer.Int64() != 99988720 || rd.UpdatedAt.Int64() != 3 {
		t.Errorf("LatestRoundData = %+v, %v", rd, err)
	}
	if d, err := feed.Decimals(ctx); err != nil || d != 8 {
		t.Errorf("feed Decimals = %d, %v", d, err)
	}
	if a, err := contracts.NewVault(vault, f).ConvertToAssets(ctx, big.NewInt(21)); err != nil || a.Int64() != 42 {
		t.Errorf("ConvertToAssets = %v, %v", a, err)
	}
	p := contracts.NewPair(pair, f)
	if s, err := p.TotalSupply(ctx); err != nil || s.Int64() != 1000 {
		t.Errorf("TotalSupply = %v, %v", s, err)
	}
	if r, err := p.GetReserves(ctx); err != nil || r.Reserve0.Int64() != 5000 || r.Reserve1.Int64() != 7000 || r.BlockTimestampLast != 123 {
		t.Errorf("GetReserves = %+v, %v", r, err)
	}
}

func TestPackOracleWrites(t *testing.T) {
	tokens := []common.Address{tokenAddr}
	data, err := contracts.PackBatchUpdate(subAccount, big.NewInt(1), big.NewInt(2), tokens, []*big.Int{big.NewInt(3)}, []*big.Int{big.NewInt(4)})
	if err != nil {
		t.Fatal(err)
	}
	m := contracts.ModuleABI.Methods["batchUpdate"]
	args, err := m.Inputs.Unpack(data[4:])
	if err != nil {
		t.Fatal(err)
	}
	if args[0].(common.Address) != subAccount || args[1].(*big.Int).Int64() != 1 || args[2].(*big.Int).Int64() != 2 ||
		args[3].([]common.Address)[0] != tokenAddr || args[4].([]*big.Int)[0].Int64() != 3 || args[5].([]*big.Int)[0].Int64() != 4 {
		t.Fatalf("batchUpdate args round-trip = %v", args)
	}

	data, err = contracts.PackUpdateSafeValue(big.NewInt(12345))
	if err != nil {
		t.Fatal(err)
	}
	args, _ = contracts.ModuleABI.Methods["updateSafeValue"].Inputs.Unpack(data[4:])
	if args[0].(*big.Int).Int64() != 12345 {
		t.Fatalf("updateSafeValue arg = %v", args[0])
	}
}
