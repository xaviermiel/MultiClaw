---
sidebar_position: 3
title: Chain Configuration
---

# Chain Configuration

MultiClaw is deployed on Base. The SDK includes built-in chain configurations with contract addresses and default RPC URLs.

## Supported chains

| Chain        | Chain ID | Status              |
| ------------ | -------- | ------------------- |
| Base Sepolia | 84532    | Deployed and tested |
| Base         | 8453     | Coming soon         |

## Default addresses

### Base Sepolia

```typescript
{
  agentVaultFactory: "0xa4D6FdE6f8F6f873BB00d5059541B657468E6179",
  presetRegistry: "0x33c487FEf63198c3d88E0F27EC1529bA1f978F60",
  moduleRegistry: "0x8694D31eCE22F827fd4353C2948B33B0CcCaE76C",
}
```

### Base (mainnet)

Addresses will be populated after mainnet deployment.

## Custom RPC

Override the default RPC URL:

```typescript
const client = new MultiClawClient({
  chain: "baseSepolia",
  rpcUrl: "https://your-custom-rpc.com",
});
```

## Custom addresses

Override contract addresses (useful for local development with Anvil):

```typescript
const client = new MultiClawClient({
  chain: "baseSepolia",
  addresses: {
    agentVaultFactory: "0x...",
    presetRegistry: "0x...",
    moduleRegistry: "0x...",
  },
});
```

## Protocol addresses (Base Sepolia)

These are the DeFi protocol contracts deployed on Base Sepolia that MultiClaw parsers support:

| Protocol              | Address                                      |
| --------------------- | -------------------------------------------- |
| Aave V3 Pool          | `0x8bAB6d1b75f19e9eD9fCe8b9BD338844fF79aE27` |
| Uniswap V3 SwapRouter | `0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4` |
| Universal Router      | `0x492E6456D9528771018DeB9E87ef7750EF184104` |

## Token addresses (Base Sepolia)

| Token | Address                                      |
| ----- | -------------------------------------------- |
| USDC  | `0x036CbD53842c5426634e7929541eC2318f3dCF7e` |

For a complete list of deployed parsers, price feeds, and token addresses, see the [deployment records](https://github.com/xaviermiel/MultiClaw).
