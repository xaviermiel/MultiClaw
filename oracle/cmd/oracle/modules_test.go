package main

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

var (
	testOracle = common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")
	testModule = common.HexToAddress("0x4d0D0000000000000000000000000000000000e1")
	testSafe   = common.HexToAddress("0x5AFE000000000000000000000000000000000000")
	testSub    = common.HexToAddress("0xAAaa0000000000000000000000000000000000a1")
)

// installSafeValueModule makes testModule (ours) a module whose Safe holds
// 1 ETH at $2000, no tokens, and no stored value yet.
func installSafeValueModule(node *fakeRPC, chain config.Chain) {
	node.balance = big.NewInt(1e18)
	m := &contracts.ModuleABI
	node.Return(testModule, m, "authorizedOracle", testOracle).
		Return(testModule, m, "avatar", testSafe).
		Return(testModule, m, "getSafeValue", big.NewInt(0), big.NewInt(0), big.NewInt(0)).
		On(testModule, m, "getTokenBalances", func(args []any) ([]any, error) {
			balances := make([]*big.Int, len(args[0].([]common.Address)))
			for i := range balances {
				balances[i] = big.NewInt(0)
			}
			return []any{balances}, nil
		}).
		Return(testModule, m, "updateSafeValue").
		Return(chain.ETHPriceFeed, &contracts.ChainlinkABI, "latestRoundData",
			big.NewInt(1), big.NewInt(2000e8), big.NewInt(0), big.NewInt(0), big.NewInt(1)).
		Return(chain.ETHPriceFeed, &contracts.ChainlinkABI, "decimals", uint8(8))
}

// installSpendingModule gives testModule one subaccount (testSub) with a 10%
// daily limit on a $10,000 Safe, no events and nothing on-chain yet.
func installSpendingModule(node *fakeRPC) {
	m := &contracts.ModuleABI
	tenThousand := new(big.Int).Mul(big.NewInt(10_000), big.NewInt(1e18))
	node.Return(testModule, m, "authorizedOracle", testOracle).
		On(testModule, m, "getSubaccountsByRole", func(args []any) ([]any, error) {
			if args[0].(uint16) == contracts.DefiExecuteRole {
				return []any{[]common.Address{testSub}}, nil
			}
			return []any{[]common.Address{}}, nil
		}).
		Return(testModule, m, "getSubAccountLimits", big.NewInt(1000), big.NewInt(0), big.NewInt(86400)).
		Return(testModule, m, "getSafeValue", tenThousand, big.NewInt(1_700_000_000), big.NewInt(1)).
		Return(testModule, m, "getSpendingAllowance", big.NewInt(0)).
		Return(testModule, m, "allowanceVersion", big.NewInt(3)).
		Return(testModule, m, "batchUpdate")
}
