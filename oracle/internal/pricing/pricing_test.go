package pricing

import (
	"context"
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
	"github.com/xaviermiel/MultiClaw/oracle/internal/spending"
)

// logs collects log lines; BuildPriceCache logs from several goroutines.
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

func e(v, decimals int64) *big.Int { return bn.Mul(big.NewInt(v), bn.Pow10(uint(decimals))) }

// feed registers a Chainlink feed answering answer with the given decimals.
func feed(f *contractstest.Fake, at common.Address, answer *big.Int, decimals uint8) {
	f.Return(at, &contracts.ChainlinkABI, "latestRoundData",
		big.NewInt(1), answer, big.NewInt(1700000000), big.NewInt(1700000000), big.NewInt(1))
	f.Return(at, &contracts.ChainlinkABI, "decimals", decimals)
}

func TestScaleTo18(t *testing.T) {
	cases := []struct {
		v        *big.Int
		decimals uint8
		want     *big.Int
	}{
		{e(2000, 8), 8, e(2000, 18)},
		{big.NewInt(1), 0, e(1, 18)},
		{big.NewInt(12345), 18, big.NewInt(12345)},
		{big.NewInt(12345), 20, big.NewInt(123)},
		{big.NewInt(-12345), 20, big.NewInt(-123)}, // toward zero, like BigInt
		{big.NewInt(-150), 20, big.NewInt(-1)},
		{e(-5, 8), 8, e(-5, 18)},
	}
	for _, tc := range cases {
		if got := ScaleTo18(tc.v, tc.decimals); got.Cmp(tc.want) != 0 {
			t.Errorf("ScaleTo18(%s, %d) = %s, want %s", tc.v, tc.decimals, got, tc.want)
		}
	}
}

func TestBuildPriceCache(t *testing.T) {
	var (
		usdc, weth, odd = addr(0xa1), addr(0xa2), addr(0xa3)
		negative, zero  = addr(0xa4), addr(0xa5)
		brokenRound     = addr(0xa6)
		brokenDecimals  = addr(0xa7)
		noFeed, shadow  = addr(0xa8), addr(0xa9)
		unknown         = addr(0xaa)
	)
	chain := config.Chain{Tokens: []config.Token{
		{Address: usdc, PriceFeed: addr(0xf1), Symbol: "USDC"},
		{Address: weth, PriceFeed: addr(0xf2), Symbol: "WETH"},
		{Address: odd, PriceFeed: addr(0xf3), Symbol: "ODD"},
		{Address: negative, PriceFeed: addr(0xf4), Symbol: "NEG"},
		{Address: zero, PriceFeed: addr(0xf5), Symbol: "ZERO"},
		{Address: brokenRound, PriceFeed: addr(0xf6), Symbol: "BR"},
		{Address: brokenDecimals, PriceFeed: addr(0xf7), Symbol: "BD"},
		{Address: noFeed, Symbol: "NOFEED"},
		// Only the first entry for a token counts, as in the TS.
		{Address: shadow, Symbol: "SHADOW"},
		{Address: shadow, PriceFeed: addr(0xf9), Symbol: "SHADOW2"},
	}}
	f := contractstest.New()
	feed(f, addr(0xf1), e(1, 8), 8)
	f.Return(usdc, &contracts.ERC20ABI, "decimals", uint8(6))
	feed(f, addr(0xf2), e(2500, 18), 18) // WETH's decimals() reverts: 18
	feed(f, addr(0xf3), big.NewInt(12345), 20)
	f.Return(odd, &contracts.ERC20ABI, "decimals", uint8(8))
	feed(f, addr(0xf4), e(-5, 8), 8)
	feed(f, addr(0xf5), big.NewInt(0), 8)
	f.Revert(addr(0xf6), &contracts.ChainlinkABI, "latestRoundData", nil)
	f.Return(addr(0xf7), &contracts.ChainlinkABI, "latestRoundData",
		big.NewInt(1), e(3, 8), big.NewInt(0), big.NewInt(0), big.NewInt(1))
	feed(f, addr(0xf9), e(7, 8), 8)

	var l logs
	r := NewReader(f, chain, l.logf)
	got := r.BuildPriceCache(context.Background(),
		[]common.Address{usdc, weth, odd, negative, zero, brokenRound, brokenDecimals, noFeed, shadow, unknown, usdc})

	want := spending.TokenPriceCache{
		usdc: {PriceUSD: e(1, 18), Decimals: 6},
		weth: {PriceUSD: e(2500, 18), Decimals: 18},
		odd:  {PriceUSD: big.NewInt(123), Decimals: 8},
	}
	if len(got) != len(want) {
		t.Fatalf("cache has %d tokens: %v", len(got), got)
	}
	for token, w := range want {
		g, ok := got[token]
		if !ok || g.PriceUSD.Cmp(w.PriceUSD) != 0 || g.Decimals != w.Decimals {
			t.Errorf("%s: got %+v, want %+v", token.Hex(), g, w)
		}
	}
	for _, s := range []string{
		"Error getting decimals for " + strings.ToLower(weth.Hex()),
		"Error getting price from " + addr(0xf6).Hex(),
		"Error getting price from " + addr(0xf7).Hex(),
	} {
		if !l.has(s) {
			t.Errorf("logs %q lack %q", l.lines, s)
		}
	}
	for _, c := range f.Calls() {
		if c.To == noFeed || c.To == shadow || c.To == unknown || c.To == addr(0xf9) {
			t.Errorf("called %s on %s, which has no feed", c.Method, c.To.Hex())
		}
	}
}

func TestPriceUSDAndTokenDecimalsDefaults(t *testing.T) {
	var l logs
	r := NewReader(contractstest.New(), config.Chain{}, l.logf)
	ctx := context.Background()
	if p := r.PriceUSD(ctx, addr(0xf1)); p.Sign() != 0 {
		t.Fatalf("price of an unreadable feed = %s", p)
	}
	if d := r.TokenDecimals(ctx, addr(0xa1)); d != DefaultDecimals {
		t.Fatalf("decimals of an unreadable token = %d", d)
	}
	if len(l.lines) != 2 {
		t.Fatalf("logs %q", l.lines)
	}
	// Negative answers come back as they are: the cache drops them, other
	// callers decide.
	f := contractstest.New()
	feed(f, addr(0xf1), e(-5, 8), 8)
	if p := NewReader(f, config.Chain{}, nil).PriceUSD(ctx, addr(0xf1)); p.Cmp(e(-5, 18)) != 0 {
		t.Fatalf("price = %s", p)
	}
}

func TestPriceReturnsTheRawAnswer(t *testing.T) {
	f := contractstest.New()
	feed(f, addr(0xf1), big.NewInt(-12345), 20)
	l := &logs{}
	r := NewReader(f, config.Chain{}, l.logf)
	ctx := context.Background()
	if answer, decimals := r.Price(ctx, addr(0xf1)); answer.Int64() != -12345 || decimals != 20 {
		t.Fatalf("Price = %s, %d", answer, decimals)
	}
	if answer, decimals := r.Price(ctx, addr(0xf2)); answer.Sign() != 0 || decimals != UnreadableFeedDecimals {
		t.Fatalf("Price of an unreadable feed = %s, %d", answer, decimals)
	}
	if !l.has("Error getting price from " + addr(0xf2).Hex()) {
		t.Fatalf("logs %q", l.lines)
	}
}
