# Safe Value Monitor (CRE Workflow)

Monitors and updates the USD value of a Safe's portfolio on-chain. Used by the DeFiInteractorModule for spending limit calculations.

## Overview

The Safe Value Monitor:
- Periodically fetches token balances from the Safe
- Gets USD prices from Chainlink price feeds
- Calculates total portfolio value
- Updates the on-chain `safeValue` in the DeFiInteractorModule

## Configuration

### Config Schema

```json
{
  "schedule": "*/30 * * * * *",
  "moduleAddress": "0x...",
  "chainSelectorName": "ethereum-testnet-sepolia",
  "gasLimit": "500000",
  "proxyAddress": "0x...",
  "tokens": [
    {
      "address": "0x...",
      "priceFeedAddress": "0x...",
      "symbol": "USDC",
      "type": "erc20"
    }
  ]
}
```

### Configuration Options

| Field | Type | Description |
|-------|------|-------------|
| `schedule` | `string` | Cron expression for update frequency (e.g., `*/30 * * * * *` for every 30 seconds) |
| `moduleAddress` | `string` | DeFiInteractorModule contract address |
| `chainSelectorName` | `string` | Chain identifier (e.g., `ethereum-testnet-sepolia`, `ethereum-mainnet`) |
| `gasLimit` | `string` | Gas limit for update transactions |
| `proxyAddress` | `string` | CRE proxy contract address |
| `tokens` | `Token[]` | Token configurations to track |

### Token Configuration

| Field | Type | Description |
|-------|------|-------------|
| `address` | `string` | Token contract address |
| `priceFeedAddress` | `string` | Chainlink price feed address for USD price |
| `symbol` | `string` | Token symbol (for logging) |
| `type` | `string` | Token type: `erc20`, `aToken`, `morphoVault`, etc. |

## Supported Token Types

| Type | Description |
|------|-------------|
| `erc20` | Standard ERC20 tokens |
| `aToken` | Aave interest-bearing tokens |
| `morphoVault` | Morpho vault shares |
| `uniswapLP` | Uniswap LP positions |

## Running Locally

```bash
# Install dependencies
bun install

# Simulate the workflow
cre workflow simulate ./safe-value

# Select cron trigger when prompted
```

## Config Files

| File | Purpose |
|------|---------|
| `config.safe-monitor.json` | Default/template configuration |
| `config.sepolia.json` | Sepolia testnet configuration |
| `config.ethereum-mainnet.json` | Ethereum mainnet configuration |
| `config.arbitrum-mainnet.json` | Arbitrum mainnet configuration |

## Integration with Spending Oracle

The Safe Value Monitor provides the portfolio value used by the Spending Oracle to calculate spending limits:

```
Portfolio Value ($100,000) × Max Spending (5%) = Daily Limit ($5,000)
```

Both workflows should be configured to monitor the same module for consistent behavior.
