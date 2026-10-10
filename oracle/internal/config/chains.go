package config

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// TokenType selects how a token is valued by the safe-value oracle.
type TokenType string

// Token types (same strings as the TypeScript TokenConfigSchema).
const (
	TokenERC20       TokenType = "erc20"
	TokenAaveAToken  TokenType = "aave-atoken"
	TokenMorphoVault TokenType = "morpho-vault"
	TokenUniswapV2LP TokenType = "uniswap-v2-lp"
)

// Token is a token tracked for Safe valuation and USD price lookups.
type Token struct {
	Address   common.Address
	PriceFeed common.Address
	Symbol    string
	Type      TokenType

	// UnderlyingAsset is required for morpho-vault tokens.
	UnderlyingAsset *common.Address
	// Token0/Token1 and their feeds are required for uniswap-v2-lp tokens.
	Token0, Token1         *common.Address
	PriceFeed0, PriceFeed1 *common.Address
}

// Chain is a per-chain preset (port of CHAIN_CONFIGS in the TS config.ts).
type Chain struct {
	Name               string
	ID                 uint64
	DefaultRPCURL      string
	BlockTimeSeconds   int64
	ConfirmationBlocks int64
	ETHPriceFeed       common.Address
	Tokens             []Token
}

// mustAddress parses a hardcoded address with ParseAddress's checksum rules
// (a typo guard for the tables below) and panics on failure.
func mustAddress(s string) common.Address {
	addr, err := ParseAddress(s)
	if err != nil {
		panic("config: " + err.Error())
	}
	return addr
}

func erc20(address, feed, symbol string) Token {
	return Token{Address: mustAddress(address), PriceFeed: mustAddress(feed), Symbol: symbol, Type: TokenERC20}
}

func aToken(address, feed, symbol string) Token {
	return Token{Address: mustAddress(address), PriceFeed: mustAddress(feed), Symbol: symbol, Type: TokenAaveAToken}
}

// ============ Chainlink Sepolia Price Feeds ============
// Source: https://docs.chain.link/data-feeds/price-feeds/addresses
const (
	sepoliaETHUSD  = "0x694AA1769357215DE4FAC081bf1f309aDC325306"
	sepoliaBTCUSD  = "0x1b44F3514812d835EB1BDB0acB33d3fA3351Ee43"
	sepoliaLINKUSD = "0xc59E3633BAAC79493d908e63626716e204A45EdF"
	sepoliaUSDCUSD = "0xA2F78ab2355fe2f984D808B5CeE7FD0A93D5270E"
	sepoliaDAIUSD  = "0x14866185B1962B63C3Ea9E03Bc1da838bab34C19"
	sepoliaEURUSD  = "0x1a81afB8146aeFfCFc5E50e8479e826E7D55b910"
)

// ============ Chainlink Base Mainnet Price Feeds ============
// Source: https://docs.chain.link/data-feeds/price-feeds/addresses?network=base
//
// LINK/USD and AAVE/USD are spelled with a valid EIP-55 checksum here. The TS
// config.ts has the same bytes with broken capitalization
// (0x17CAb8FE31cA45e4684181bCB02e26f877aEEaEF, 0x978B06bB4bDf7cc3650B70B7aF36CF6E95Cf0dBE),
// which viem rejects as invalid, so the TS oracle could never price LINK or
// AAVE on Base mainnet.
const (
	baseETHUSD   = "0x71041dddad3595F9CEd3DcCFBe3D1F4b0a16Bb70"
	baseBTCUSD   = "0x07DA0E54543a844a80ABE69c8A12F22B3aA59f9D"
	baseLINKUSD  = "0x17CAB8Fe31cA45e4684181BcB02e26f877aeeaef"
	baseUSDCUSD  = "0x7e860098F58bBFC8648a4311b374B1D669a2bc6B"
	baseDAIUSD   = "0x591e79239a7d679378eC8c847e5038150364C78F"
	baseAAVEUSD  = "0x978B06bb4bDF7cc3650B70B7af36Cf6E95cF0dBe"
	baseCBETHUSD = "0xd7818272B9e248357d13057AAb0B417aF31E817d"
)

// ============ Chainlink Base Sepolia Price Feeds ============
// Source: bgd-labs/aave-address-book AaveV3BaseSepolia + Chainlink docs
const (
	baseSepoliaETHUSD  = "0x4aDC67696bA383F43DD60A9e78F2C97Fbbfc7cb1"
	baseSepoliaBTCUSD  = "0x0FB99723Aee6f420beAD13e6bBB79b7E6F034298"
	baseSepoliaLINKUSD = "0xb113F5A928BCfF189C998ab20d753a47F9dE5A61"
	baseSepoliaUSDCUSD = "0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165"
	baseSepoliaUSDTUSD = "0x3ec8593F930EA45ea58c968260e6e9FF53FC934f"
)

// chainOrder is the order the TS listed supported chains in error messages.
var chainOrder = []string{"sepolia", "base", "base-sepolia"}

// chains holds the presets. Token lists keep the TS order (it determines the
// order of the safe-value log lines and of the getTokenBalances call), and the
// TS price-feed choices are kept verbatim, including the odd ones (e.g. USDT
// and EURC priced with the USDC feed on some testnets).
var chains = map[string]Chain{
	"sepolia": {
		Name:               "sepolia",
		ID:                 11155111,
		DefaultRPCURL:      "https://ethereum-sepolia-rpc.publicnode.com",
		BlockTimeSeconds:   12,
		ConfirmationBlocks: 12,
		ETHPriceFeed:       mustAddress(sepoliaETHUSD),
		Tokens: []Token{
			// Underlying tokens (Aave V3 Sepolia)
			erc20("0xC558DBdd856501FCd9aaF1E62eae57A9F0629a3c", sepoliaETHUSD, "WETH"),
			erc20("0x29f2D40B0605204364af54EC677bD022dA425d03", sepoliaBTCUSD, "WBTC"),
			erc20("0x94a9D9AC8a22534E3FaCa9F4e7F2E2cf85d5E4C8", sepoliaUSDCUSD, "USDC"),
			erc20("0xFF34B3d4Aee8ddCd6F9AFFFB6Fe49bD371b8a357", sepoliaDAIUSD, "DAI"),
			erc20("0xaA8E23Fb1079EA71e0a56F48a2aA51851D8433D0", sepoliaUSDCUSD, "USDT"),
			erc20("0xf8Fb3713D459D7C1018BD0A49D19b4C44290EBE5", sepoliaLINKUSD, "LINK"),
			erc20("0x88541670E55cC00bEEFD87eB59EDd1b7C511AC9a", sepoliaLINKUSD, "AAVE"),
			erc20("0x6d906e526a4e2Ca02097BA9d0caA3c382F52278E", sepoliaEURUSD, "EURS"),
			// aTokens (1:1 with underlying, use same price feeds)
			aToken("0x5b071b590a59395fE4025A0Ccc1FcC931AAc1830", sepoliaETHUSD, "aWETH"),
			aToken("0x1804Bf30507dc2EB3bDEbbbdd859991EAeF6EefF", sepoliaBTCUSD, "aWBTC"),
			aToken("0x16dA4541aD1807f4443d92D26044C1147406EB80", sepoliaUSDCUSD, "aUSDC"),
			aToken("0x29598b72eb5CeBd806C5dCD549490FdA35B13cD8", sepoliaDAIUSD, "aDAI"),
			aToken("0xAF0F6e8b0Dc5c913bbF4d14c22B4E78Dd14310B6", sepoliaUSDCUSD, "aUSDT"),
			aToken("0x3FfAf50D4F4E96eB78f2407c090b72e86eCaed24", sepoliaLINKUSD, "aLINK"),
			aToken("0x6b8558764d3b7572136F17174Cb9aB1DDc7E1259", sepoliaLINKUSD, "aAAVE"),
			aToken("0xB20691021F9AcED8631eDaa3c0Cd2949EB45662D", sepoliaEURUSD, "aEURS"),
			// Circle USDC (different from Aave USDC on Sepolia)
			erc20("0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238", sepoliaUSDCUSD, "USDC (Circle)"),
			erc20("0x08210F9170F89Ab7658F0B5E3fF39b0E03C594D4", sepoliaEURUSD, "EURC"),
		},
	},
	"base": {
		Name:               "base",
		ID:                 8453,
		DefaultRPCURL:      "https://mainnet.base.org",
		BlockTimeSeconds:   2,
		ConfirmationBlocks: 3,
		ETHPriceFeed:       mustAddress(baseETHUSD),
		Tokens: []Token{
			// Underlying tokens
			erc20("0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", baseUSDCUSD, "USDC"),
			erc20("0x4200000000000000000000000000000000000006", baseETHUSD, "WETH"),
			erc20("0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb", baseDAIUSD, "DAI"),
			erc20("0x2Ae3F1Ec7F1F5012CFEab0185bfc7aa3cf0DEc22", baseCBETHUSD, "cbETH"),
			erc20("0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf", baseBTCUSD, "cbBTC"),
			erc20("0x88Fb150BDc53A65fe94Dea0c9BA0a6dAf8C6e196", baseLINKUSD, "LINK"),
			erc20("0xcfA132E353cB4E398080B9700609bb008eceB125", baseAAVEUSD, "AAVE"),
			erc20("0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA", baseUSDCUSD, "USDbC"),
			// Aave V3 aTokens on Base (1:1 with underlying)
			aToken("0x4e65fE4DbA92790696d040ac24Aa414708F5c0AB", baseUSDCUSD, "aBasUSDC"),
			aToken("0xD4a0e0b9149BCee3C920d2E00b5dE09138fd8bb7", baseETHUSD, "aBasWETH"),
			aToken("0x0a1d576f3eFeF75b330424287a95A366e8281D54", baseDAIUSD, "aBasDAI"),
			aToken("0xcf3D55c10DB69f28fD1A75Bd73f3D8A2d9c595ad", baseCBETHUSD, "aBascbETH"),
		},
	},
	"base-sepolia": {
		Name:               "base-sepolia",
		ID:                 84532,
		DefaultRPCURL:      "https://sepolia.base.org",
		BlockTimeSeconds:   2,
		ConfirmationBlocks: 3,
		ETHPriceFeed:       mustAddress(baseSepoliaETHUSD),
		Tokens: []Token{
			// Underlying tokens
			erc20("0x4200000000000000000000000000000000000006", baseSepoliaETHUSD, "WETH"),
			erc20("0x036CbD53842c5426634e7929541eC2318f3dCF7e", baseSepoliaUSDCUSD, "USDC"),
			erc20("0xba50Cd2A20f6DA35D788639E581bca8d0B5d4D5f", baseSepoliaUSDCUSD, "USDC (Aave)"),
			erc20("0x0a215D8ba66387DCA84B284D18c3B4ec3de6E54a", baseSepoliaUSDTUSD, "USDT"),
			erc20("0x54114591963CF60EF3aA63bEfD6eC263D98145a4", baseSepoliaBTCUSD, "WBTC"),
			erc20("0x810D46F9a9027E28F9B01F75E2bdde839dA61115", baseSepoliaLINKUSD, "LINK"),
			erc20("0xD171b9694f7A2597Ed006D41f7509aaD4B485c4B", baseSepoliaETHUSD, "cbETH"),
			erc20("0x808456652fdb597867f38412077A9182bf77359F", baseSepoliaUSDCUSD, "EURC"),
			// Aave V3 aTokens (1:1 with underlying)
			aToken("0x73a5bB60b0B0fc35710DDc0ea9c407031E31Bdbb", baseSepoliaETHUSD, "aBasSepWETH"),
			aToken("0x10F1A9D11CDf50041f3f8cB7191CBE2f31750ACC", baseSepoliaUSDCUSD, "aBasSepUSDC"),
			aToken("0xcE3CAae5Ed17A7AafCEEbc897DE843fA6CC0c018", baseSepoliaUSDTUSD, "aBasSepUSDT"),
			aToken("0x47Db195BAf46898302C06c31bCF46c01C64ACcF9", baseSepoliaBTCUSD, "aBasSepWBTC"),
			aToken("0x0aD46dE765522399d7b25B438b230A894d72272B", baseSepoliaLINKUSD, "aBasSepLINK"),
			aToken("0x9Fd6d1DBAd7c052e0c43f46df36eEc6a68814B63", baseSepoliaETHUSD, "aBasSepcbETH"),
		},
	},
}

// ChainByName returns the preset for name.
func ChainByName(name string) (Chain, error) {
	c, ok := chains[name]
	if !ok {
		return Chain{}, fmt.Errorf("unknown chain: %s. Supported: %s", name, strings.Join(chainOrder, ", "))
	}
	return c, nil
}

// SupportedChains lists the preset names in TS order.
func SupportedChains() []string { return append([]string(nil), chainOrder...) }

// TokenByAddress finds the preset token with the given address.
func (c Chain) TokenByAddress(addr common.Address) (Token, bool) {
	for _, t := range c.Tokens {
		if t.Address == addr {
			return t, true
		}
	}
	return Token{}, false
}
