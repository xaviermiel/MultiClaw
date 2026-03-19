import { z } from "zod";
import dotenv from "dotenv";
import { sepolia, base, baseSepolia, type Chain } from "viem/chains";

dotenv.config();

// Token configuration for safe-value calculation
export const TokenConfigSchema = z.object({
  address: z.string(),
  priceFeedAddress: z.string(),
  symbol: z.string(),
  type: z
    .enum(["erc20", "aave-atoken", "morpho-vault", "uniswap-v2-lp"])
    .optional()
    .default("erc20"),
  underlyingAsset: z.string().optional(),
  token0: z.string().optional(),
  token1: z.string().optional(),
  priceFeed0: z.string().optional(),
  priceFeed1: z.string().optional(),
});

export type TokenConfig = z.infer<typeof TokenConfigSchema>;

// ============ Chain Definitions ============

interface ChainConfig {
  chain: Chain;
  defaultRpcUrl: string;
  // Block time in seconds (used to compute default blocksToLookBack)
  blockTimeSeconds: number;
  // Reorg protection
  confirmationBlocks: number;
  // ETH/USD Chainlink price feed address for native ETH valuation
  ethPriceFeedAddress: string;
  tokens: TokenConfig[];
}

// ============ Chainlink Sepolia Price Feeds ============
// Source: https://docs.chain.link/data-feeds/price-feeds/addresses
const CHAINLINK_SEPOLIA = {
  ETH_USD: "0x694AA1769357215DE4FAC081bf1f309aDC325306",
  BTC_USD: "0x1b44F3514812d835EB1BDB0acB33d3fA3351Ee43",
  LINK_USD: "0xc59E3633BAAC79493d908e63626716e204A45EdF",
  USDC_USD: "0xA2F78ab2355fe2f984D808B5CeE7FD0A93D5270E",
  DAI_USD: "0x14866185B1962B63C3Ea9E03Bc1da838bab34C19",
  EUR_USD: "0x1a81afB8146aeFfCFc5E50e8479e826E7D55b910",
};

// ============ Aave V3 Sepolia Token Addresses ============
// Source: https://github.com/bgd-labs/aave-address-book/blob/main/src/AaveV3Sepolia.sol
const AAVE_SEPOLIA_TOKENS = {
  // Underlying tokens
  DAI: "0xFF34B3d4Aee8ddCd6F9AFFFB6Fe49bD371b8a357",
  USDC: "0x94a9D9AC8a22534E3FaCa9F4e7F2E2cf85d5E4C8",
  USDT: "0xaA8E23Fb1079EA71e0a56F48a2aA51851D8433D0",
  WETH: "0xC558DBdd856501FCd9aaF1E62eae57A9F0629a3c",
  WBTC: "0x29f2D40B0605204364af54EC677bD022dA425d03",
  LINK: "0xf8Fb3713D459D7C1018BD0A49D19b4C44290EBE5",
  AAVE: "0x88541670E55cC00bEEFD87eB59EDd1b7C511AC9a",
  EURS: "0x6d906e526a4e2Ca02097BA9d0caA3c382F52278E",
  // aTokens
  aDAI: "0x29598b72eb5CeBd806C5dCD549490FdA35B13cD8",
  aUSDC: "0x16dA4541aD1807f4443d92D26044C1147406EB80",
  aUSDT: "0xAF0F6e8b0Dc5c913bbF4d14c22B4E78Dd14310B6",
  aWETH: "0x5b071b590a59395fE4025A0Ccc1FcC931AAc1830",
  aWBTC: "0x1804Bf30507dc2EB3bDEbbbdd859991EAeF6EefF",
  aLINK: "0x3FfAf50D4F4E96eB78f2407c090b72e86eCaed24",
  aAAVE: "0x6b8558764d3b7572136F17174Cb9aB1DDc7E1259",
  aEURS: "0xB20691021F9AcED8631eDaa3c0Cd2949EB45662D",
};

const OTHER_SEPOLIA_TOKENS = {
  USDC_CIRCLE: "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238",
  EURC: "0x08210F9170F89Ab7658F0B5E3fF39b0E03C594D4",
};

// ============ Chainlink Base Mainnet Price Feeds ============
// Source: https://docs.chain.link/data-feeds/price-feeds/addresses?network=base
const CHAINLINK_BASE = {
  ETH_USD: "0x71041dddad3595F9CEd3DcCFBe3D1F4b0a16Bb70",
  BTC_USD: "0x07DA0E54543a844a80ABE69c8A12F22B3aA59f9D",
  LINK_USD: "0x17CAb8FE31cA45e4684181bCB02e26f877aEEaEF",
  USDC_USD: "0x7e860098F58bBFC8648a4311b374B1D669a2bc6B",
  DAI_USD: "0x591e79239a7d679378eC8c847e5038150364C78F",
  AAVE_USD: "0x978B06bB4bDf7cc3650B70B7aF36CF6E95Cf0dBE",
  cbETH_USD: "0xd7818272B9e248357d13057AAb0B417aF31E817d",
};

// ============ Base Mainnet Token Addresses ============
// Source: https://github.com/bgd-labs/aave-address-book/blob/main/src/AaveV3Base.sol
const BASE_TOKENS = {
  // Underlying tokens
  USDC: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913",
  WETH: "0x4200000000000000000000000000000000000006",
  DAI: "0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb",
  cbETH: "0x2Ae3F1Ec7F1F5012CFEab0185bfc7aa3cf0DEc22",
  cbBTC: "0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf",
  LINK: "0x88Fb150BDc53A65fe94Dea0c9BA0a6dAf8C6e196",
  AAVE: "0xcfA132E353cB4E398080B9700609bb008eceB125",
  USDbC: "0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA",
  // Aave V3 aTokens on Base
  aBasUSDC: "0x4e65fE4DbA92790696d040ac24Aa414708F5c0AB",
  aBasWETH: "0xD4a0e0b9149BCee3C920d2E00b5dE09138fd8bb7",
  aBasDAI: "0x0a1d576f3eFeF75b330424287a95A366e8281D54",
  aBascbETH: "0xcf3D55c10DB69f28fD1A75Bd73f3D8A2d9c595ad",
  aBasUSDbC: "0x0a1d576f3eFeF75b330424287a95A366e8281D54",
};

// ============ Chainlink Base Sepolia Price Feeds ============
const CHAINLINK_BASE_SEPOLIA = {
  ETH_USD: "0x4aDC67D04f6Ff2B21933cF4D6919e65E1afFdcB1",
  USDC_USD: "0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165",
};

const BASE_SEPOLIA_TOKENS = {
  USDC: "0x036CbD53842c5426634e7929541eC2318f3dCF7e",
  WETH: "0x4200000000000000000000000000000000000006",
};

// ============ Per-Chain Configurations ============

const CHAIN_CONFIGS: Record<string, ChainConfig> = {
  sepolia: {
    chain: sepolia,
    defaultRpcUrl: "https://ethereum-sepolia-rpc.publicnode.com",
    blockTimeSeconds: 12,
    confirmationBlocks: 12,
    ethPriceFeedAddress: CHAINLINK_SEPOLIA.ETH_USD,
    tokens: [
      // Underlying tokens
      {
        address: AAVE_SEPOLIA_TOKENS.WETH,
        priceFeedAddress: CHAINLINK_SEPOLIA.ETH_USD,
        symbol: "WETH",
        type: "erc20",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.WBTC,
        priceFeedAddress: CHAINLINK_SEPOLIA.BTC_USD,
        symbol: "WBTC",
        type: "erc20",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.USDC,
        priceFeedAddress: CHAINLINK_SEPOLIA.USDC_USD,
        symbol: "USDC",
        type: "erc20",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.DAI,
        priceFeedAddress: CHAINLINK_SEPOLIA.DAI_USD,
        symbol: "DAI",
        type: "erc20",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.USDT,
        priceFeedAddress: CHAINLINK_SEPOLIA.USDC_USD,
        symbol: "USDT",
        type: "erc20",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.LINK,
        priceFeedAddress: CHAINLINK_SEPOLIA.LINK_USD,
        symbol: "LINK",
        type: "erc20",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.AAVE,
        priceFeedAddress: CHAINLINK_SEPOLIA.LINK_USD,
        symbol: "AAVE",
        type: "erc20",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.EURS,
        priceFeedAddress: CHAINLINK_SEPOLIA.EUR_USD,
        symbol: "EURS",
        type: "erc20",
      },
      // aTokens (1:1 with underlying, use same price feeds)
      {
        address: AAVE_SEPOLIA_TOKENS.aWETH,
        priceFeedAddress: CHAINLINK_SEPOLIA.ETH_USD,
        symbol: "aWETH",
        type: "aave-atoken",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.aWBTC,
        priceFeedAddress: CHAINLINK_SEPOLIA.BTC_USD,
        symbol: "aWBTC",
        type: "aave-atoken",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.aUSDC,
        priceFeedAddress: CHAINLINK_SEPOLIA.USDC_USD,
        symbol: "aUSDC",
        type: "aave-atoken",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.aDAI,
        priceFeedAddress: CHAINLINK_SEPOLIA.DAI_USD,
        symbol: "aDAI",
        type: "aave-atoken",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.aUSDT,
        priceFeedAddress: CHAINLINK_SEPOLIA.USDC_USD,
        symbol: "aUSDT",
        type: "aave-atoken",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.aLINK,
        priceFeedAddress: CHAINLINK_SEPOLIA.LINK_USD,
        symbol: "aLINK",
        type: "aave-atoken",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.aAAVE,
        priceFeedAddress: CHAINLINK_SEPOLIA.LINK_USD,
        symbol: "aAAVE",
        type: "aave-atoken",
      },
      {
        address: AAVE_SEPOLIA_TOKENS.aEURS,
        priceFeedAddress: CHAINLINK_SEPOLIA.EUR_USD,
        symbol: "aEURS",
        type: "aave-atoken",
      },
      // Circle USDC (different from Aave USDC on Sepolia)
      {
        address: OTHER_SEPOLIA_TOKENS.USDC_CIRCLE,
        priceFeedAddress: CHAINLINK_SEPOLIA.USDC_USD,
        symbol: "USDC (Circle)",
        type: "erc20",
      },
      {
        address: OTHER_SEPOLIA_TOKENS.EURC,
        priceFeedAddress: CHAINLINK_SEPOLIA.EUR_USD,
        symbol: "EURC",
        type: "erc20",
      },
    ] as TokenConfig[],
  },

  base: {
    chain: base,
    defaultRpcUrl: "https://mainnet.base.org",
    blockTimeSeconds: 2,
    confirmationBlocks: 3,
    ethPriceFeedAddress: CHAINLINK_BASE.ETH_USD,
    tokens: [
      // Underlying tokens
      {
        address: BASE_TOKENS.USDC,
        priceFeedAddress: CHAINLINK_BASE.USDC_USD,
        symbol: "USDC",
        type: "erc20",
      },
      {
        address: BASE_TOKENS.WETH,
        priceFeedAddress: CHAINLINK_BASE.ETH_USD,
        symbol: "WETH",
        type: "erc20",
      },
      {
        address: BASE_TOKENS.DAI,
        priceFeedAddress: CHAINLINK_BASE.DAI_USD,
        symbol: "DAI",
        type: "erc20",
      },
      {
        address: BASE_TOKENS.cbETH,
        priceFeedAddress: CHAINLINK_BASE.cbETH_USD,
        symbol: "cbETH",
        type: "erc20",
      },
      {
        address: BASE_TOKENS.cbBTC,
        priceFeedAddress: CHAINLINK_BASE.BTC_USD,
        symbol: "cbBTC",
        type: "erc20",
      },
      {
        address: BASE_TOKENS.LINK,
        priceFeedAddress: CHAINLINK_BASE.LINK_USD,
        symbol: "LINK",
        type: "erc20",
      },
      {
        address: BASE_TOKENS.AAVE,
        priceFeedAddress: CHAINLINK_BASE.AAVE_USD,
        symbol: "AAVE",
        type: "erc20",
      },
      {
        address: BASE_TOKENS.USDbC,
        priceFeedAddress: CHAINLINK_BASE.USDC_USD,
        symbol: "USDbC",
        type: "erc20",
      },
      // Aave V3 aTokens on Base (1:1 with underlying)
      {
        address: BASE_TOKENS.aBasUSDC,
        priceFeedAddress: CHAINLINK_BASE.USDC_USD,
        symbol: "aBasUSDC",
        type: "aave-atoken",
      },
      {
        address: BASE_TOKENS.aBasWETH,
        priceFeedAddress: CHAINLINK_BASE.ETH_USD,
        symbol: "aBasWETH",
        type: "aave-atoken",
      },
      {
        address: BASE_TOKENS.aBasDAI,
        priceFeedAddress: CHAINLINK_BASE.DAI_USD,
        symbol: "aBasDAI",
        type: "aave-atoken",
      },
      {
        address: BASE_TOKENS.aBascbETH,
        priceFeedAddress: CHAINLINK_BASE.cbETH_USD,
        symbol: "aBascbETH",
        type: "aave-atoken",
      },
    ] as TokenConfig[],
  },

  "base-sepolia": {
    chain: baseSepolia,
    defaultRpcUrl: "https://sepolia.base.org",
    blockTimeSeconds: 2,
    confirmationBlocks: 3,
    ethPriceFeedAddress: CHAINLINK_BASE_SEPOLIA.ETH_USD,
    tokens: [
      {
        address: BASE_SEPOLIA_TOKENS.USDC,
        priceFeedAddress: CHAINLINK_BASE_SEPOLIA.USDC_USD,
        symbol: "USDC",
        type: "erc20",
      },
      {
        address: BASE_SEPOLIA_TOKENS.WETH,
        priceFeedAddress: CHAINLINK_BASE_SEPOLIA.ETH_USD,
        symbol: "WETH",
        type: "erc20",
      },
    ] as TokenConfig[],
  },
};

// ============ Chain Selection ============

const selectedChain = process.env.CHAIN || "sepolia";
const chainConfig = CHAIN_CONFIGS[selectedChain];
if (!chainConfig) {
  throw new Error(
    `Unknown chain: ${selectedChain}. Supported: ${Object.keys(CHAIN_CONFIGS).join(", ")}`,
  );
}

// Compute default blocksToLookBack based on chain block time
// 24h worth of blocks = 86400 / blockTimeSeconds
const defaultBlocksToLookBack = Math.floor(
  86400 / chainConfig.blockTimeSeconds,
);
// 30 days of blocks for historical lookback
const defaultMaxHistoricalBlocks = defaultBlocksToLookBack * 30;

// Parse RPC URLs from environment (comma-separated for fallback support)
function parseRpcUrls(): string[] {
  const primary = process.env.RPC_URL || chainConfig.defaultRpcUrl;
  const fallbacks =
    process.env.RPC_FALLBACK_URLS?.split(",")
      .map((u) => u.trim())
      .filter(Boolean) || [];
  return [primary, ...fallbacks];
}

// Main configuration
export const config = {
  // Selected chain name
  chainName: selectedChain,

  // Primary RPC URL (first in the list)
  rpcUrl: parseRpcUrls()[0],
  // All RPC URLs including fallbacks
  rpcUrls: parseRpcUrls(),
  privateKey: process.env.PRIVATE_KEY as `0x${string}`,
  moduleAddress: process.env.MODULE_ADDRESS as `0x${string}`,
  // Optional registry address for multi-module support
  registryAddress: process.env.REGISTRY_ADDRESS as `0x${string}` | undefined,

  // Cron schedules
  safeValueCron: process.env.SAFE_VALUE_CRON || "0 */10 * * * *", // Every 30 minutes
  spendingOracleCron: process.env.SPENDING_ORACLE_CRON || "0 */2 * * * *", // Every 5 minutes

  // Polling interval: shorter for faster chains (Base ~2s blocks vs Sepolia ~12s)
  pollIntervalMs: parseInt(
    process.env.POLL_INTERVAL_MS ||
      String(chainConfig.blockTimeSeconds * 2 * 1000),
  ),
  blocksToLookBack: parseInt(
    process.env.BLOCKS_TO_LOOK_BACK || String(defaultBlocksToLookBack),
  ),
  windowDurationSeconds: parseInt(
    process.env.WINDOW_DURATION_SECONDS || "86400",
  ),

  // Reorg protection
  confirmationBlocks: parseInt(
    process.env.CONFIRMATION_BLOCKS || String(chainConfig.confirmationBlocks),
  ),

  // Maximum blocks per log query to prevent RPC timeouts
  maxBlocksPerQuery: parseInt(process.env.MAX_BLOCKS_PER_QUERY || "5000"),

  // Maximum total blocks to search for historical tokens
  maxHistoricalBlocks: parseInt(
    process.env.MAX_HISTORICAL_BLOCKS || String(defaultMaxHistoricalBlocks),
  ),

  // Gas
  gasLimit: BigInt(process.env.GAS_LIMIT || "500000"),

  // Chain (viem chain object)
  chain: chainConfig.chain,

  // ETH/USD price feed address (chain-specific)
  ethPriceFeedAddress: chainConfig.ethPriceFeedAddress,

  // Tokens to track for safe value calculation (chain-specific)
  tokens: chainConfig.tokens,
};

// Validate required config
export function validateConfig() {
  if (!config.privateKey) {
    throw new Error("PRIVATE_KEY environment variable is required");
  }
  if (!config.moduleAddress) {
    throw new Error("MODULE_ADDRESS environment variable is required");
  }
}
