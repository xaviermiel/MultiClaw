import { base, baseSepolia, type Chain } from "viem/chains";
import type { ChainAddresses } from "./types";

interface ChainConfig {
  chain: Chain;
  defaultRpcUrl: string;
  addresses: ChainAddresses;
}

/**
 * Chain-specific configurations.
 * Contract addresses are populated after deployment.
 * Use `0x0` placeholders until deployed — the client will throw if you
 * try to use a function that requires an undeployed contract.
 */
export const CHAIN_CONFIGS: Record<string, ChainConfig> = {
  base: {
    chain: base,
    defaultRpcUrl: "https://mainnet.base.org",
    addresses: {
      // TODO: fill after Base mainnet deployment
      agentVaultFactory: "0x0000000000000000000000000000000000000000",
      presetRegistry: "0x0000000000000000000000000000000000000000",
      moduleRegistry: "0x0000000000000000000000000000000000000000",
    },
  },
  baseSepolia: {
    chain: baseSepolia,
    defaultRpcUrl: "https://sepolia.base.org",
    addresses: {
      // TODO: fill after Base Sepolia deployment
      agentVaultFactory: "0x0000000000000000000000000000000000000000",
      presetRegistry: "0x0000000000000000000000000000000000000000",
      moduleRegistry: "0x0000000000000000000000000000000000000000",
    },
  },
};

export function getChainConfig(chainName: string): ChainConfig {
  const config = CHAIN_CONFIGS[chainName];
  if (!config) {
    throw new Error(
      `Unknown chain: ${chainName}. Supported: ${Object.keys(CHAIN_CONFIGS).join(", ")}`,
    );
  }
  return config;
}
