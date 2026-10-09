// Package pricing reads Chainlink USD prices and token decimals. It ports the
// TS price helpers of the spending oracle (getPriceFeedForToken,
// getTokenDecimals, getChainlinkPriceUSD, buildTokenPriceCache).
package pricing

import (
	"context"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/sync/errgroup"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/spending"
)

// DefaultDecimals is assumed for tokens whose decimals() can't be read.
const DefaultDecimals = 18

// maxParallelTokens bounds the concurrent reads of BuildPriceCache.
const maxParallelTokens = 8

// Reader reads prices for the tokens of a chain preset.
type Reader struct {
	caller contracts.Caller
	chain  config.Chain
	logf   logx.Logf
}

// NewReader returns a Reader for chain's tokens.
func NewReader(c contracts.Caller, chain config.Chain, logf logx.Logf) *Reader {
	if logf == nil {
		logf = logx.Discard
	}
	return &Reader{caller: c, chain: chain, logf: logf}
}

// FeedForToken returns the price feed configured for token: that of its first
// preset entry, if it has one.
func (r *Reader) FeedForToken(token common.Address) (common.Address, bool) {
	t, ok := r.chain.TokenByAddress(token)
	if !ok || t.PriceFeed == (common.Address{}) {
		return common.Address{}, false
	}
	return t.PriceFeed, true
}

// TokenDecimals returns token's decimals, or DefaultDecimals when they can't
// be read.
func (r *Reader) TokenDecimals(ctx context.Context, token common.Address) uint8 {
	decimals, err := contracts.NewERC20(token, r.caller).Decimals(ctx)
	if err != nil {
		r.logf("Error getting decimals for %s: %v", strings.ToLower(token.Hex()), err)
		return DefaultDecimals
	}
	return decimals
}

// PriceUSD returns feed's latest answer scaled to 18 decimals, or 0 when it
// can't be read. The answer is not checked for sign or staleness (callers
// decide). Unlike the TS, which threw on feeds with more than 18 decimals,
// those are scaled down.
func (r *Reader) PriceUSD(ctx context.Context, feed common.Address) *big.Int {
	f := contracts.NewPriceFeed(feed, r.caller)
	round, err := f.LatestRoundData(ctx)
	if err == nil {
		var decimals uint8
		if decimals, err = f.Decimals(ctx); err == nil {
			return ScaleTo18(round.Answer, decimals)
		}
	}
	r.logf("Error getting price from %s: %v", feed.Hex(), err)
	return bn.Zero()
}

// ScaleTo18 rescales a fixed-point value with the given decimals to 18
// decimals, truncating toward zero when scaling down.
func ScaleTo18(v *big.Int, decimals uint8) *big.Int {
	if decimals <= 18 {
		return bn.Mul(v, bn.Pow10(uint(18-decimals)))
	}
	return bn.Quo(v, bn.Pow10(uint(decimals-18)))
}

// BuildPriceCache prices tokens for USD-weighted spending accounting. Tokens
// without a configured feed, or whose price isn't positive, are left out.
func (r *Reader) BuildPriceCache(ctx context.Context, tokens []common.Address) spending.TokenPriceCache {
	cache := spending.TokenPriceCache{}
	var mu sync.Mutex
	var g errgroup.Group
	g.SetLimit(maxParallelTokens)
	for _, token := range tokens {
		feed, ok := r.FeedForToken(token)
		if !ok {
			continue
		}
		g.Go(func() error {
			price := r.PriceUSD(ctx, feed)
			if price.Sign() <= 0 {
				return nil
			}
			info := spending.TokenPriceInfo{PriceUSD: price, Decimals: r.TokenDecimals(ctx, token)}
			mu.Lock()
			cache[token] = info
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait() // the goroutines never fail: errors leave a token out
	return cache
}
