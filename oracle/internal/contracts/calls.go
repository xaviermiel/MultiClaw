package contracts

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Caller executes eth_call. The RPC client implements it; tests use fakes.
type Caller interface {
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

// bound is a contract address plus the ABI and block used for reads.
type bound struct {
	Address common.Address
	caller  Caller
	abi     *abi.ABI
	// block is the block to read at; nil means latest.
	block *big.Int
}

// call packs method(args), executes it and unpacks the outputs. RPC errors are
// returned unwrapped so callers can inspect revert data.
func (b bound) call(ctx context.Context, method string, args ...any) ([]any, error) {
	data, err := b.abi.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	to := b.Address
	out, err := b.caller.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, b.block)
	if err != nil {
		return nil, err
	}
	vals, err := b.abi.Unpack(method, out)
	if err != nil {
		return nil, fmt.Errorf("%s on %s: unpack: %w", method, b.Address.Hex(), err)
	}
	return vals, nil
}

func one[T any](vals []any, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	if len(vals) != 1 {
		return zero, fmt.Errorf("expected 1 output, got %d", len(vals))
	}
	v, ok := vals[0].(T)
	if !ok {
		return zero, fmt.Errorf("unexpected output type %T", vals[0])
	}
	return v, nil
}

// ============ DeFiInteractorModule ============

// Module wraps the DeFiInteractorModule read functions the oracle uses.
type Module struct{ bound }

// NewModule binds a module at address.
func NewModule(address common.Address, c Caller) Module {
	return Module{bound{Address: address, caller: c, abi: &ModuleABI}}
}

// At returns a copy reading at the given block (nil = latest).
func (m Module) At(block *big.Int) Module {
	m.block = block
	return m
}

// SafeValue is the result of getSafeValue().
type SafeValue struct {
	TotalValueUSD *big.Int
	LastUpdated   *big.Int
	UpdateCount   *big.Int
}

// GetSafeValue calls getSafeValue().
func (m Module) GetSafeValue(ctx context.Context) (SafeValue, error) {
	vals, err := m.call(ctx, "getSafeValue")
	if err != nil {
		return SafeValue{}, err
	}
	return SafeValue{TotalValueUSD: vals[0].(*big.Int), LastUpdated: vals[1].(*big.Int), UpdateCount: vals[2].(*big.Int)}, nil
}

// SubAccountLimits is the result of getSubAccountLimits(subAccount).
type SubAccountLimits struct {
	MaxSpendingBps *big.Int
	MaxSpendingUSD *big.Int
	WindowDuration *big.Int
}

// GetSubAccountLimits calls getSubAccountLimits(subAccount).
func (m Module) GetSubAccountLimits(ctx context.Context, subAccount common.Address) (SubAccountLimits, error) {
	vals, err := m.call(ctx, "getSubAccountLimits", subAccount)
	if err != nil {
		return SubAccountLimits{}, err
	}
	return SubAccountLimits{MaxSpendingBps: vals[0].(*big.Int), MaxSpendingUSD: vals[1].(*big.Int), WindowDuration: vals[2].(*big.Int)}, nil
}

// GetSpendingAllowance calls getSpendingAllowance(subAccount).
func (m Module) GetSpendingAllowance(ctx context.Context, subAccount common.Address) (*big.Int, error) {
	return one[*big.Int](m.call(ctx, "getSpendingAllowance", subAccount))
}

// GetAcquiredBalance calls getAcquiredBalance(subAccount, token).
func (m Module) GetAcquiredBalance(ctx context.Context, subAccount, token common.Address) (*big.Int, error) {
	return one[*big.Int](m.call(ctx, "getAcquiredBalance", subAccount, token))
}

// AllowanceVersion calls allowanceVersion(subAccount).
func (m Module) AllowanceVersion(ctx context.Context, subAccount common.Address) (*big.Int, error) {
	return one[*big.Int](m.call(ctx, "allowanceVersion", subAccount))
}

// AcquiredBalanceVersion calls acquiredBalanceVersion(subAccount, token).
func (m Module) AcquiredBalanceVersion(ctx context.Context, subAccount, token common.Address) (*big.Int, error) {
	return one[*big.Int](m.call(ctx, "acquiredBalanceVersion", subAccount, token))
}

// Avatar calls avatar() (the Safe).
func (m Module) Avatar(ctx context.Context) (common.Address, error) {
	return one[common.Address](m.call(ctx, "avatar"))
}

// AuthorizedOracle calls authorizedOracle().
func (m Module) AuthorizedOracle(ctx context.Context) (common.Address, error) {
	return one[common.Address](m.call(ctx, "authorizedOracle"))
}

// GetSubaccountsByRole calls getSubaccountsByRole(roleId).
func (m Module) GetSubaccountsByRole(ctx context.Context, roleID uint16) ([]common.Address, error) {
	return one[[]common.Address](m.call(ctx, "getSubaccountsByRole", roleID))
}

// GetTokenBalances calls getTokenBalances(tokens) (the Safe's balances).
func (m Module) GetTokenBalances(ctx context.Context, tokens []common.Address) ([]*big.Int, error) {
	return one[[]*big.Int](m.call(ctx, "getTokenBalances", tokens))
}

// AbsoluteMaxSpendingBps calls the absoluteMaxSpendingBps() getter that only
// deployments from before April 20 have. On current deployments it fails.
func (m Module) AbsoluteMaxSpendingBps(ctx context.Context) (*big.Int, error) {
	legacy := m.bound
	legacy.abi = &LegacyModuleABI
	return one[*big.Int](legacy.call(ctx, "absoluteMaxSpendingBps"))
}

// ============ ModuleRegistry ============

// Registry wraps the ModuleRegistry read functions the oracle uses.
type Registry struct{ bound }

// NewRegistry binds a registry at address.
func NewRegistry(address common.Address, c Caller) Registry {
	return Registry{bound{Address: address, caller: c, abi: &RegistryABI}}
}

// GetActiveModules calls getActiveModules().
func (r Registry) GetActiveModules(ctx context.Context) ([]common.Address, error) {
	return one[[]common.Address](r.call(ctx, "getActiveModules"))
}

// ============ ERC20 ============

// ERC20 wraps balanceOf/decimals/symbol.
type ERC20 struct{ bound }

// NewERC20 binds a token at address.
func NewERC20(address common.Address, c Caller) ERC20 {
	return ERC20{bound{Address: address, caller: c, abi: &ERC20ABI}}
}

// At returns a copy reading at the given block (nil = latest).
func (t ERC20) At(block *big.Int) ERC20 {
	t.block = block
	return t
}

// BalanceOf calls balanceOf(account).
func (t ERC20) BalanceOf(ctx context.Context, account common.Address) (*big.Int, error) {
	return one[*big.Int](t.call(ctx, "balanceOf", account))
}

// Decimals calls decimals().
func (t ERC20) Decimals(ctx context.Context) (uint8, error) {
	return one[uint8](t.call(ctx, "decimals"))
}

// Symbol calls symbol().
func (t ERC20) Symbol(ctx context.Context) (string, error) {
	return one[string](t.call(ctx, "symbol"))
}

// ============ Chainlink ============

// PriceFeed wraps a Chainlink aggregator.
type PriceFeed struct{ bound }

// NewPriceFeed binds a feed at address.
func NewPriceFeed(address common.Address, c Caller) PriceFeed {
	return PriceFeed{bound{Address: address, caller: c, abi: &ChainlinkABI}}
}

// At returns a copy reading at the given block (nil = latest).
func (f PriceFeed) At(block *big.Int) PriceFeed {
	f.block = block
	return f
}

// RoundData is the result of latestRoundData().
type RoundData struct {
	RoundID         *big.Int
	Answer          *big.Int
	StartedAt       *big.Int
	UpdatedAt       *big.Int
	AnsweredInRound *big.Int
}

// LatestRoundData calls latestRoundData().
func (f PriceFeed) LatestRoundData(ctx context.Context) (RoundData, error) {
	vals, err := f.call(ctx, "latestRoundData")
	if err != nil {
		return RoundData{}, err
	}
	return RoundData{
		RoundID:         vals[0].(*big.Int),
		Answer:          vals[1].(*big.Int),
		StartedAt:       vals[2].(*big.Int),
		UpdatedAt:       vals[3].(*big.Int),
		AnsweredInRound: vals[4].(*big.Int),
	}, nil
}

// Decimals calls decimals().
func (f PriceFeed) Decimals(ctx context.Context) (uint8, error) {
	return one[uint8](f.call(ctx, "decimals"))
}

// ============ ERC4626 (Morpho vaults) ============

// Vault wraps an ERC4626 vault.
type Vault struct{ bound }

// NewVault binds a vault at address.
func NewVault(address common.Address, c Caller) Vault {
	return Vault{bound{Address: address, caller: c, abi: &ERC4626ABI}}
}

// ConvertToAssets calls convertToAssets(shares).
func (v Vault) ConvertToAssets(ctx context.Context, shares *big.Int) (*big.Int, error) {
	return one[*big.Int](v.call(ctx, "convertToAssets", shares))
}

// ============ Uniswap V2 pair ============

// Pair wraps a Uniswap V2 pair.
type Pair struct{ bound }

// NewPair binds a pair at address.
func NewPair(address common.Address, c Caller) Pair {
	return Pair{bound{Address: address, caller: c, abi: &UniswapV2PairABI}}
}

// TotalSupply calls totalSupply().
func (p Pair) TotalSupply(ctx context.Context) (*big.Int, error) {
	return one[*big.Int](p.call(ctx, "totalSupply"))
}

// Reserves is the result of getReserves().
type Reserves struct {
	Reserve0, Reserve1 *big.Int
	BlockTimestampLast uint32
}

// GetReserves calls getReserves().
func (p Pair) GetReserves(ctx context.Context) (Reserves, error) {
	vals, err := p.call(ctx, "getReserves")
	if err != nil {
		return Reserves{}, err
	}
	return Reserves{Reserve0: vals[0].(*big.Int), Reserve1: vals[1].(*big.Int), BlockTimestampLast: vals[2].(uint32)}, nil
}

// ============ Oracle writes (calldata only; sending is the tx sender's job) ============

// PackBatchUpdate encodes batchUpdate(...).
func PackBatchUpdate(subAccount common.Address, expectedAllowanceVersion, newAllowance *big.Int,
	tokens []common.Address, expectedTokenVersions, balances []*big.Int) ([]byte, error) {
	values := append([]*big.Int{expectedAllowanceVersion, newAllowance}, expectedTokenVersions...)
	if err := checkUint256(append(values, balances...)...); err != nil {
		return nil, fmt.Errorf("batchUpdate: %w", err)
	}
	return ModuleABI.Pack("batchUpdate", subAccount, expectedAllowanceVersion, newAllowance, tokens, expectedTokenVersions, balances)
}

// PackUpdateSafeValue encodes updateSafeValue(totalValueUSD).
func PackUpdateSafeValue(totalValueUSD *big.Int) ([]byte, error) {
	if err := checkUint256(totalValueUSD); err != nil {
		return nil, fmt.Errorf("updateSafeValue: %w", err)
	}
	return ModuleABI.Pack("updateSafeValue", totalValueUSD)
}

var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// checkUint256 rejects values a uint256 cannot hold, which go-ethereum's
// packer would silently wrap modulo 2^256 (viem's encoder threw, so the TS
// never sent them).
func checkUint256(values ...*big.Int) error {
	for _, v := range values {
		if v == nil || v.Sign() < 0 || v.Cmp(maxUint256) > 0 {
			return fmt.Errorf("%v is out of the uint256 range", v)
		}
	}
	return nil
}
