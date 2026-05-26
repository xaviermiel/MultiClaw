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
      agentVaultFactory: "0x389623997Bc006dA3BdBbE18d7Be04dACF4f09Ff",
      presetRegistry: "0x49991a970BEBb0FBE7b472636dF2302c83B3bdD6",
      moduleRegistry: "0x9889390D3ac31f6Ec97833D18Df4C4E689157251",
    },
  },
  baseSepolia: {
    chain: baseSepolia,
    defaultRpcUrl: "https://sepolia.base.org",
    addresses: {
      agentVaultFactory: "0xa4D6FdE6f8F6f873BB00d5059541B657468E6179",
      presetRegistry: "0x33c487FEf63198c3d88E0F27EC1529bA1f978F60",
      moduleRegistry: "0x8694D31eCE22F827fd4353C2948B33B0CcCaE76C",
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
