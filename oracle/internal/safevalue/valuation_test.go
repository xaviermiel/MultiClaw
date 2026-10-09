package safevalue

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
)

// fakeChain is a contractstest.Fake with native balances and a block number.
type fakeChain struct {
	*contractstest.Fake
	balances   map[common.Address]*big.Int
	balanceErr error
}

func newFakeChain() *fakeChain {
	return &fakeChain{Fake: contractstest.New(), balances: map[common.Address]*big.Int{}}
}

func (f *fakeChain) BalanceAt(_ context.Context, a common.Address, _ *big.Int) (*big.Int, error) {
	if f.balanceErr != nil {
		return nil, f.balanceErr
	}
	if b, ok := f.balances[a]; ok {
		return b, nil
	}
	return big.NewInt(0), nil
}

func (f *fakeChain) BlockNumber(context.Context) (uint64, error) { return 777, nil }

// logs collects log lines.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logs) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.lines, func(line string) bool { return strings.Contains(line, s) })
}

func addr(n int64) common.Address { return common.BigToAddress(big.NewInt(n)) }

func ptr(a common.Address) *common.Address { return &a }

func e(v, decimals int64) *big.Int { return bn.Mul(big.NewInt(v), bn.Pow10(uint(decimals))) }

func parse(s string) *big.Int { return bn.MustParse(s) }

func feed(f *contractstest.Fake, at common.Address, answer *big.Int, decimals uint8) {
	f.Return(at, &contracts.ChainlinkABI, "latestRoundData",
		big.NewInt(1), answer, big.NewInt(0), big.NewInt(0), big.NewInt(1))
	f.Return(at, &contracts.ChainlinkABI, "decimals", decimals)
}

func decimals(f *contractstest.Fake, token common.Address, d uint8) {
	f.Return(token, &contracts.ERC20ABI, "decimals", d)
}

var (
	module = addr(0x40d)
	safe   = addr(0x5afe)
	oracle = common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")

	ethFeed  = addr(0xfee0)
	usdcFeed = addr(0xfee1)
	wethFeed = addr(0xfee2)
)

// TestSafeValueMatchesTheTSFormulas values every token type; the expected
// values were computed independently with the TS formulas (Python integers,
// truncating division).
func TestSafeValueMatchesTheTSFormulas(t *testing.T) {
	var (
		usdc, aweth, empty      = addr(0xa1), addr(0xa2), addr(0xa3)
		vault, pair, noDecimals = addr(0xa4), addr(0xa5), addr(0xa6)
		negative, tiny, broken  = addr(0xa7), addr(0xa8), addr(0xa9)
		weth                    = addr(0xaa)
	)
	chain := config.Chain{ETHPriceFeed: ethFeed, Tokens: []config.Token{
		{Address: usdc, PriceFeed: usdcFeed, Symbol: "USDC", Type: config.TokenERC20},
		{Address: aweth, PriceFeed: wethFeed, Symbol: "aWETH", Type: config.TokenAaveAToken},
		{Address: empty, PriceFeed: usdcFeed, Symbol: "EMPTY", Type: config.TokenERC20},
		{Address: vault, PriceFeed: usdcFeed, Symbol: "mUSDC", Type: config.TokenMorphoVault, UnderlyingAsset: ptr(usdc)},
		{Address: pair, Symbol: "LP", Type: config.TokenUniswapV2LP,
			Token0: ptr(usdc), Token1: ptr(weth), PriceFeed0: ptr(usdcFeed), PriceFeed1: ptr(wethFeed)},
		{Address: noDecimals, PriceFeed: addr(0xfee3), Symbol: "NODEC"},
		{Address: negative, PriceFeed: addr(0xfee4), Symbol: "NEG", Type: config.TokenERC20},
		{Address: tiny, PriceFeed: addr(0xfee5), Symbol: "TINY", Type: config.TokenERC20},
		{Address: broken, PriceFeed: addr(0xfee6), Symbol: "BROKEN", Type: config.TokenERC20},
	}}

	c := newFakeChain()
	c.balances[safe] = e(15, 17)
	c.Return(module, &contracts.ModuleABI, "avatar", safe)
	c.Return(module, &contracts.ModuleABI, "getTokenBalances", []*big.Int{
		big.NewInt(1234567891), bn.Add(e(5, 17), big.NewInt(1)), big.NewInt(0), e(1, 18),
		bn.Add(e(10, 18), big.NewInt(1)), e(7, 18), e(3, 18), big.NewInt(3), e(5, 18),
	})
	feed(c.Fake, ethFeed, big.NewInt(200012345678), 8)
	feed(c.Fake, usdcFeed, big.NewInt(99987654), 8)
	feed(c.Fake, wethFeed, big.NewInt(345678901234), 8)
	feed(c.Fake, addr(0xfee3), big.NewInt(250000000), 8)
	feed(c.Fake, addr(0xfee4), big.NewInt(-100000000), 8) // negative answers count, as in the TS
	feed(c.Fake, addr(0xfee5), big.NewInt(-7), 8)
	c.Revert(addr(0xfee6), &contracts.ChainlinkABI, "latestRoundData", nil)
	decimals(c.Fake, usdc, 6)
	decimals(c.Fake, aweth, 18)
	decimals(c.Fake, weth, 18)
	decimals(c.Fake, negative, 18)
	decimals(c.Fake, tiny, 18)
	decimals(c.Fake, broken, 18)
	c.Return(vault, &contracts.ERC4626ABI, "convertToAssets", big.NewInt(1050000))
	c.Return(pair, &contracts.UniswapV2PairABI, "totalSupply", e(1000, 18))
	c.Return(pair, &contracts.UniswapV2PairABI, "getReserves",
		bn.Add(e(5000, 6), big.NewInt(7)), bn.Add(e(2, 18), big.NewInt(3)), uint32(0))

	l := &logs{}
	got, err := NewValuer(c, chain, l.logf).SafeValue(context.Background(), module)
	if err != nil {
		t.Fatal(err)
	}
	// eth 3000185185170000000000 + usdc 1234415471248177140000
	// + aweth 1728394506170000003456 + morpho 1049870367000000000
	// + lp 119129607246800000000 + nodec 17500000000000000000
	// + neg -3000000000000000000 + tiny 0 (-21e-8 truncated toward zero)
	// + broken 0
	if want := parse("6097674640201977143456"); got.Cmp(want) != 0 {
		t.Fatalf("safe value = %s, want %s", got, want)
	}
	for _, want := range []string{
		"Monitoring Safe: " + safe.Hex() + " (module: " + module.Hex() + ")",
		"  ETH: balance=1.5, price=2000.12345678 USD",
		"  Value: $3000.18518517 USD",
		"Processing aave-atoken: aWETH (" + aweth.Hex() + ")",
		"  USDC: balance=1234.567891, price=0.99987654 USD",
		"  Balance: 0, skipping",
		"  Morpho: 1000000000000000000 shares = 1050000 underlying assets",
		"  Uniswap V2 LP: owns 50000000 token0, 20000000000000000 token1",
		"Error getting decimals for " + strings.ToLower(noDecimals.Hex()),
		"  NEG: balance=3, price=-1 USD",
		"Error getting price from " + addr(0xfee6).Hex(),
		"  BROKEN: balance=5, price=0 USD",
	} {
		if !l.has(want) {
			t.Errorf("logs lack %q", want)
		}
	}
}

func TestSafeValueEdgeCases(t *testing.T) {
	ctx := context.Background()
	token := addr(0xa1)
	erc20 := config.Token{Address: token, PriceFeed: usdcFeed, Symbol: "USDC", Type: config.TokenERC20}
	withSafe := func() *fakeChain {
		c := newFakeChain()
		c.Return(module, &contracts.ModuleABI, "avatar", safe)
		feed(c.Fake, usdcFeed, e(1, 8), 8)
		feed(c.Fake, ethFeed, e(2000, 8), 8)
		decimals(c.Fake, token, 6)
		return c
	}

	t.Run("unreadable Safe address is an error", func(t *testing.T) {
		_, err := NewValuer(newFakeChain(), config.Chain{}, nil).SafeValue(ctx, module)
		if err == nil || !strings.Contains(err.Error(), "read the Safe of module") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no ETH feed skips the tokens too", func(t *testing.T) {
		c := withSafe()
		c.balances[safe] = e(1, 18)
		c.Return(module, &contracts.ModuleABI, "getTokenBalances", []*big.Int{e(5, 6)})
		l := &logs{}
		got, err := NewValuer(c, config.Chain{Tokens: []config.Token{erc20}}, l.logf).SafeValue(ctx, module)
		if err != nil || got.Sign() != 0 || !l.has("No ETH price feed configured, skipping native ETH valuation") {
			t.Fatalf("value %v (%v), logs %q", got, err, l.lines)
		}
	})

	t.Run("failed reads count as zero", func(t *testing.T) {
		c := withSafe()
		c.balanceErr = errors.New("rpc down")
		c.Revert(module, &contracts.ModuleABI, "getTokenBalances", nil)
		l := &logs{}
		got, err := NewValuer(c, config.Chain{ETHPriceFeed: ethFeed, Tokens: []config.Token{erc20}}, l.logf).SafeValue(ctx, module)
		if err != nil || got.Sign() != 0 {
			t.Fatalf("value %v (%v)", got, err)
		}
		for _, want := range []string{"Error getting native ETH balance: rpc down", "Error batch fetching balances", "  Balance: 0, skipping"} {
			if !l.has(want) {
				t.Errorf("logs %q lack %q", l.lines, want)
			}
		}
	})

	t.Run("no tokens configured", func(t *testing.T) {
		c := withSafe()
		c.balances[safe] = e(1, 18)
		l := &logs{}
		got, err := NewValuer(c, config.Chain{ETHPriceFeed: ethFeed}, l.logf).SafeValue(ctx, module)
		if err != nil || got.Cmp(e(2000, 18)) != 0 || !l.has("No ERC20 tokens configured for safe value calculation") {
			t.Fatalf("value %v (%v), logs %q", got, err, l.lines)
		}
	})

	t.Run("Morpho and Uniswap V2 failures count as zero", func(t *testing.T) {
		vault, unconfigured, emptyPair, missingPair := addr(0xb1), addr(0xb2), addr(0xb3), addr(0xb4)
		c := withSafe()
		c.Return(module, &contracts.ModuleABI, "getTokenBalances", []*big.Int{e(1, 18), e(1, 18), e(1, 18), e(1, 18)})
		c.Revert(vault, &contracts.ERC4626ABI, "convertToAssets", nil)
		c.Return(emptyPair, &contracts.UniswapV2PairABI, "totalSupply", big.NewInt(0))
		c.Return(emptyPair, &contracts.UniswapV2PairABI, "getReserves", e(1, 6), e(1, 18), uint32(0))
		lp := func(at common.Address) config.Token {
			return config.Token{Address: at, Symbol: "LP", Type: config.TokenUniswapV2LP,
				Token0: ptr(token), Token1: ptr(token), PriceFeed0: ptr(usdcFeed), PriceFeed1: ptr(usdcFeed)}
		}
		chain := config.Chain{Tokens: []config.Token{
			{Address: vault, PriceFeed: usdcFeed, Symbol: "mV", Type: config.TokenMorphoVault, UnderlyingAsset: ptr(token)},
			{Address: unconfigured, PriceFeed: usdcFeed, Symbol: "mX", Type: config.TokenMorphoVault},
			lp(emptyPair),
			{Address: missingPair, Symbol: "LP2", Type: config.TokenUniswapV2LP},
		}}
		l := &logs{}
		got, err := NewValuer(c, chain, l.logf).SafeValue(ctx, module)
		if err != nil || got.Sign() != 0 {
			t.Fatalf("value %v (%v)", got, err)
		}
		for _, want := range []string{
			"Error calculating Morpho value",
			"Morpho vault mX missing underlyingAsset config",
			"Error calculating Uniswap V2 LP value: RangeError: Division by zero",
			"Uniswap V2 LP LP2 missing token/priceFeed config",
		} {
			if !l.has(want) {
				t.Errorf("logs %q lack %q", l.lines, want)
			}
		}
	})
}
