// Package safevalue is the Go port of the TS Safe value oracle
// (oracle/src/safe-value.ts): it values each module's Safe in USD and writes
// the value on-chain when it moved or went stale.
package safevalue

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/pricing"
)

// ChainReader is what the safe-value oracle reads from the chain.
type ChainReader interface {
	contracts.Caller
	BalanceAt(ctx context.Context, account common.Address, block *big.Int) (*big.Int, error)
}

// Valuer values Safes with the TS formulas (calculateSafeValue).
type Valuer struct {
	reader ChainReader
	chain  config.Chain
	prices *pricing.Reader
	logf   logx.Logf
}

// NewValuer returns a Valuer for the tokens of chain.
func NewValuer(r ChainReader, chain config.Chain, logf logx.Logf) *Valuer {
	if logf == nil {
		logf = logx.Discard
	}
	return &Valuer{reader: r, chain: chain, prices: pricing.NewReader(r, chain, logf), logf: logf}
}

// SafeValue returns the USD value (18 decimals) of module's Safe: its native
// ETH plus every configured token. As in the TS, failed reads count as 0 and
// are logged; only an unreadable Safe address is an error.
func (v *Valuer) SafeValue(ctx context.Context, module common.Address) (*big.Int, error) {
	total := bn.Zero()
	safe, err := contracts.NewModule(module, v.reader).Avatar(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the Safe of module %s: %w", module.Hex(), err)
	}
	v.logf("Monitoring Safe: %s (module: %s)", safe.Hex(), module.Hex())

	ethBalance, err := v.reader.BalanceAt(ctx, safe, nil)
	if err != nil {
		v.logf("Error getting native ETH balance: %v", err)
		ethBalance = bn.Zero()
	}
	if ethBalance.Sign() > 0 {
		v.logf("Processing native ETH")
		if v.chain.ETHPriceFeed == (common.Address{}) {
			// The TS returned here, leaving the tokens unvalued too.
			v.logf("No ETH price feed configured, skipping native ETH valuation")
			return total, nil
		}
		price, priceDecimals := v.prices.Price(ctx, v.chain.ETHPriceFeed)
		value := usdValue(ethBalance, price, 18, priceDecimals)
		v.logf("  ETH: balance=%s, price=%s USD", bn.FormatUnits(ethBalance, 18), bn.FormatUnits(price, int(priceDecimals)))
		v.logf("  Value: $%s USD", bn.FormatUnits(value, 18))
		total = bn.Add(total, value)
	}

	tokens := v.chain.Tokens
	if len(tokens) == 0 {
		v.logf("No ERC20 tokens configured for safe value calculation")
		return total, nil
	}
	addresses := make([]common.Address, len(tokens))
	for i, t := range tokens {
		addresses[i] = t.Address
	}
	balances, err := contracts.NewModule(module, v.reader).GetTokenBalances(ctx, addresses)
	if err != nil {
		v.logf("Error batch fetching balances: %v", err)
		balances = nil
	}

	for i, t := range tokens {
		kind := t.Type
		if kind == "" {
			kind = config.TokenERC20
		}
		v.logf("Processing %s: %s (%s)", kind, t.Symbol, t.Address.Hex())
		balance := bn.Zero()
		if i < len(balances) {
			balance = balances[i]
		}
		if balance.Sign() == 0 {
			v.logf("  Balance: 0, skipping")
			continue
		}

		var value *big.Int
		switch kind {
		case config.TokenMorphoVault:
			value = v.morphoValue(ctx, t, balance)
		case config.TokenUniswapV2LP:
			value = v.uniswapV2LPValue(ctx, t, balance)
		default: // ERC20s and aTokens (1:1 with their underlying)
			decimals := v.prices.TokenDecimals(ctx, t.Address)
			price, priceDecimals := v.prices.Price(ctx, t.PriceFeed)
			v.logf("  %s: balance=%s, price=%s USD", t.Symbol,
				bn.FormatUnits(balance, int(decimals)), bn.FormatUnits(price, int(priceDecimals)))
			value = usdValue(balance, price, decimals, priceDecimals)
		}
		v.logf("  Value: $%s USD", bn.FormatUnits(value, 18))
		total = bn.Add(total, value)
	}
	return total, nil
}

// morphoValue values Morpho vault shares through their underlying asset.
func (v *Valuer) morphoValue(ctx context.Context, t config.Token, shares *big.Int) *big.Int {
	if t.UnderlyingAsset == nil {
		v.logf("Morpho vault %s missing underlyingAsset config", t.Symbol)
		return bn.Zero()
	}
	assets, err := contracts.NewVault(t.Address, v.reader).ConvertToAssets(ctx, shares)
	if err != nil {
		v.logf("Error calculating Morpho value: %v", err)
		return bn.Zero()
	}
	v.logf("  Morpho: %s shares = %s underlying assets", shares, assets)
	decimals := v.prices.TokenDecimals(ctx, *t.UnderlyingAsset)
	price, priceDecimals := v.prices.Price(ctx, t.PriceFeed)
	return usdValue(assets, price, decimals, priceDecimals)
}

// uniswapV2LPValue values LP tokens as the owned share of both reserves.
func (v *Valuer) uniswapV2LPValue(ctx context.Context, t config.Token, lpBalance *big.Int) *big.Int {
	if t.Token0 == nil || t.Token1 == nil || t.PriceFeed0 == nil || t.PriceFeed1 == nil {
		v.logf("Uniswap V2 LP %s missing token/priceFeed config", t.Symbol)
		return bn.Zero()
	}
	pair := contracts.NewPair(t.Address, v.reader)
	totalSupply, err := pair.TotalSupply(ctx)
	if err != nil {
		v.logf("Error calculating Uniswap V2 LP value: %v", err)
		return bn.Zero()
	}
	reserves, err := pair.GetReserves(ctx)
	if err != nil {
		v.logf("Error calculating Uniswap V2 LP value: %v", err)
		return bn.Zero()
	}
	if totalSupply.Sign() == 0 {
		// The TS divided by zero here and caught the RangeError.
		v.logf("Error calculating Uniswap V2 LP value: RangeError: Division by zero")
		return bn.Zero()
	}
	owned0 := bn.Quo(bn.Mul(reserves.Reserve0, lpBalance), totalSupply)
	owned1 := bn.Quo(bn.Mul(reserves.Reserve1, lpBalance), totalSupply)
	v.logf("  Uniswap V2 LP: owns %s token0, %s token1", owned0, owned1)

	decimals0 := v.prices.TokenDecimals(ctx, *t.Token0)
	decimals1 := v.prices.TokenDecimals(ctx, *t.Token1)
	price0, priceDecimals0 := v.prices.Price(ctx, *t.PriceFeed0)
	price1, priceDecimals1 := v.prices.Price(ctx, *t.PriceFeed1)
	return bn.Add(usdValue(owned0, price0, decimals0, priceDecimals0), usdValue(owned1, price1, decimals1, priceDecimals1))
}

// usdValue is amount * price * 1e18 / 10^decimals / 10^priceDecimals: an
// 18-decimal USD value, with the TS's truncating divisions in the TS order.
func usdValue(amount, price *big.Int, decimals, priceDecimals uint8) *big.Int {
	v := bn.Mul(bn.Mul(amount, price), bn.Precision)
	v = bn.Quo(v, bn.Pow10(uint(decimals)))
	return bn.Quo(v, bn.Pow10(uint(priceDecimals)))
}
