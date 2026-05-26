import type { Address } from "viem";

/**
 * Chainlink price-feed registry per chain, keyed by the same chain names as
 * {@link CHAIN_CONFIGS} (`base`, `baseSepolia`).
 *
 * Use this to populate the `priceFeedTokens` / `priceFeedAddresses` arrays of a
 * {@link VaultConfig} (for `createAgentVault`) or the matching params of
 * `createAgentVaultFromPreset`. Feeds seed the module's `tokenPriceFeeds`
 * mapping at vault creation so the module can value tokens for USD-mode
 * spending limits — which oracleless vaults require.
 *
 * Feed addresses are Chainlink USD aggregator proxies. The native-ETH entry
 * (`address(0)`) is accepted by `setTokenPriceFeeds`; do not filter it out or
 * swap paths that route through native ETH will fail to value.
 *
 * Addresses verified against the live chain. Re-check Base mainnet feeds
 * against https://data.chain.link/base before a first deploy — Chainlink
 * occasionally migrates an aggregator proxy.
 */
export interface PriceFeedEntry {
  /** Token address (`address(0)` = native ETH). */
  token: Address;
  /** Chainlink USD aggregator proxy. */
  feed: Address;
}

// Base Sepolia (84532) — testnet token set including Aave V3 aTokens.
const BASE_SEPOLIA: PriceFeedEntry[] = [
  // Native ETH (address(0)) -> ETH/USD
  {
    token: "0x0000000000000000000000000000000000000000",
    feed: "0x4aDC67696bA383F43DD60A9e78F2C97Fbbfc7cb1",
  },
  // WETH -> ETH/USD
  {
    token: "0x4200000000000000000000000000000000000006",
    feed: "0x4aDC67696bA383F43DD60A9e78F2C97Fbbfc7cb1",
  },
  // USDC (Circle) -> USDC/USD
  {
    token: "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
    feed: "0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165",
  },
  // USDC (Aave) -> USDC/USD
  {
    token: "0xba50Cd2A20f6DA35D788639E581bca8d0B5d4D5f",
    feed: "0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165",
  },
  // USDT -> USDT/USD
  {
    token: "0x0a215D8ba66387DCA84B284D18c3B4ec3de6E54a",
    feed: "0x3ec8593F930EA45ea58c968260e6e9FF53FC934f",
  },
  // WBTC -> BTC/USD
  {
    token: "0x54114591963CF60EF3aA63bEfD6eC263D98145a4",
    feed: "0x0FB99723Aee6f420beAD13e6bBB79b7E6F034298",
  },
  // LINK -> LINK/USD
  {
    token: "0x810D46F9a9027E28F9B01F75E2bdde839dA61115",
    feed: "0xb113F5A928BCfF189C998ab20d753a47F9dE5A61",
  },
  // cbETH -> ETH/USD (pegged)
  {
    token: "0xD171b9694f7A2597Ed006D41f7509aaD4B485c4B",
    feed: "0x4aDC67696bA383F43DD60A9e78F2C97Fbbfc7cb1",
  },
  // EURC -> USDC/USD (proxy: no native EUR/USD feed on Base Sepolia)
  {
    token: "0x808456652fdb597867f38412077A9182bf77359F",
    feed: "0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165",
  },
  // Aave V3 aTokens (1:1 with underlying, reuse underlying feed)
  // aWETH -> ETH/USD
  {
    token: "0x73a5bB60b0B0fc35710DDc0ea9c407031E31Bdbb",
    feed: "0x4aDC67696bA383F43DD60A9e78F2C97Fbbfc7cb1",
  },
  // aUSDC -> USDC/USD
  {
    token: "0x10F1A9D11CDf50041f3f8cB7191CBE2f31750ACC",
    feed: "0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165",
  },
  // aUSDT -> USDT/USD
  {
    token: "0xcE3CAae5Ed17A7AafCEEbc897DE843fA6CC0c018",
    feed: "0x3ec8593F930EA45ea58c968260e6e9FF53FC934f",
  },
  // aWBTC -> BTC/USD
  {
    token: "0x47Db195BAf46898302C06c31bCF46c01C64ACcF9",
    feed: "0x0FB99723Aee6f420beAD13e6bBB79b7E6F034298",
  },
  // aLINK -> LINK/USD
  {
    token: "0x0aD46dE765522399d7b25B438b230A894d72272B",
    feed: "0xb113F5A928BCfF189C998ab20d753a47F9dE5A61",
  },
  // acbETH -> ETH/USD
  {
    token: "0x9Fd6d1DBAd7c052e0c43f46df36eEc6a68814B63",
    feed: "0x4aDC67696bA383F43DD60A9e78F2C97Fbbfc7cb1",
  },
];

// Base mainnet (8453) — canonical tokens + Chainlink USD aggregators.
// Mirrors the interface repo's priceFeeds.ts BASE_MAINNET. All 11 pairs
// verified on-chain (correct asset pair, 8 decimals).
const BASE_MAINNET: PriceFeedEntry[] = [
  // Native ETH (address(0)) -> ETH/USD
  {
    token: "0x0000000000000000000000000000000000000000",
    feed: "0x71041dddad3595F9CEd3DcCFBe3D1F4b0a16Bb70",
  },
  // WETH -> ETH/USD
  {
    token: "0x4200000000000000000000000000000000000006",
    feed: "0x71041dddad3595F9CEd3DcCFBe3D1F4b0a16Bb70",
  },
  // USDC (Circle native) -> USDC/USD
  {
    token: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
    feed: "0x7e860098F58bBFC8648a4311b374B1D669a2bc6B",
  },
  // USDbC (bridged USDC) -> USDC/USD
  {
    token: "0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA",
    feed: "0x7e860098F58bBFC8648a4311b374B1D669a2bc6B",
  },
  // USDT -> USDT/USD
  {
    token: "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2",
    feed: "0xf19d560eB8d2ADf07BD6D13ed03e1D11215721F9",
  },
  // DAI -> DAI/USD
  {
    token: "0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb",
    feed: "0x591e79239a7d679378eC8c847e5038150364C78F",
  },
  // cbETH -> cbETH/USD
  {
    token: "0x2Ae3F1Ec7F1F5012CFEab0185bfc7aa3cf0DEc22",
    feed: "0xd7818272B9e248357d13057AAb0B417aF31E817d",
  },
  // cbBTC -> BTC/USD
  {
    token: "0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf",
    feed: "0x64c911996D3c6aC71f9b455B1E8E7266BcbD848F",
  },
  // LINK -> LINK/USD
  {
    token: "0x88Fb150BDc53A65fe94Dea0c9BA0a6dAf8C6e196",
    feed: "0x17CAb8FE31E32f08326e5E27412894e49B0f9D65",
  },
  // AAVE -> AAVE/USD
  {
    token: "0x63706e401c06ac8513145b7687A14804d17f814b",
    feed: "0x3d6774EF702A10b20FCa8Ed40FC022f7E4938e07",
  },
  // EURC -> EUR/USD
  {
    token: "0x60a3E35Cc302bFA44Cb288Bc5a4F316Fdb1adb42",
    feed: "0xc91D87E81faB8f93699ECf7Ee9B44D11e1D53F0F",
  },
];

const PRICE_FEEDS_BY_CHAIN: Record<string, PriceFeedEntry[]> = {
  base: BASE_MAINNET,
  baseSepolia: BASE_SEPOLIA,
};

/**
 * Returns the price-feed registry for a chain, or `undefined` if none is
 * configured. Treat `undefined` as "no feeds to seed" — the vault can still be
 * created, but USD-mode spending and price-dependent paths will revert until
 * feeds are added by the Safe owner.
 *
 * @param chainName - `base` or `baseSepolia` (same keys as {@link CHAIN_CONFIGS}).
 */
export function getPriceFeeds(chainName: string): PriceFeedEntry[] | undefined {
  return PRICE_FEEDS_BY_CHAIN[chainName];
}

/**
 * Convenience: parallel `tokens[]` / `feeds[]` arrays in the shape that
 * `deployVault` (`VaultConfig.priceFeedTokens` / `priceFeedAddresses`) and
 * `createAgentVaultFromPreset` expect. Returns empty arrays for unknown chains.
 *
 * @param chainName - `base` or `baseSepolia` (same keys as {@link CHAIN_CONFIGS}).
 */
export function getPriceFeedArrays(chainName: string): {
  tokens: Address[];
  feeds: Address[];
} {
  const entries = PRICE_FEEDS_BY_CHAIN[chainName] ?? [];
  return {
    tokens: entries.map((e) => e.token),
    feeds: entries.map((e) => e.feed),
  };
}
