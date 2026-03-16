# Spending Oracle (CRE Workflow)

Off-chain oracle for the DeFiInteractorModule spending limit system. Monitors blockchain events and updates on-chain spending allowances.

## Overview

The spending oracle implements the **Acquired Balance Model**:
- Tracks spending in rolling 24-hour windows
- Distinguishes between original tokens (costs spending) and acquired tokens (free to use)
- Uses FIFO queues for acquired balance tracking with timestamp inheritance
- Matches deposits to withdrawals for acquired status determination

## Configuration

### Config Schema

```json
{
  "moduleAddresses": ["0x...", "0x..."],
  "registryAddress": "0x...",
  "chainSelectorName": "ethereum-testnet-sepolia",
  "gasLimit": "500000",
  "proxyAddress": "0x...",
  "refreshSchedule": "*/5 * * * *",
  "windowDurationSeconds": 86400,
  "blocksToLookBack": 7200,
  "maxBlocksPerQuery": 5000,
  "maxHistoricalBlocks": 72000,
  "tokens": [
    {
      "address": "0x...",
      "priceFeedAddress": "0x...",
      "symbol": "USDC"
    }
  ]
}
```

### Configuration Options

| Field | Type | Description |
|-------|------|-------------|
| `moduleAddresses` | `string[]` | Array of module addresses to monitor via log triggers (instant) |
| `registryAddress` | `string` | Optional registry address for discovering new modules via cron |
| `chainSelectorName` | `string` | Chain identifier (e.g., `ethereum-testnet-sepolia`) |
| `gasLimit` | `string` | Gas limit for update transactions |
| `proxyAddress` | `string` | CRE proxy contract address |
| `refreshSchedule` | `string` | Cron expression for periodic refresh (e.g., `*/5 * * * *`) |
| `windowDurationSeconds` | `number` | Rolling window duration (default: 86400 = 24 hours) |
| `blocksToLookBack` | `number` | Blocks to query for events (~24h worth) |
| `maxBlocksPerQuery` | `number` | Max blocks per RPC log query (prevents timeouts) |
| `maxHistoricalBlocks` | `number` | Max total blocks for historical token discovery |
| `tokens` | `Token[]` | Token configurations with price feed addresses |

## Multi-Module Support

The oracle supports monitoring multiple DeFiInteractorModule instances:

### Instant Event Processing
Modules listed in `moduleAddresses` get real-time log triggers:
- ProtocolExecution events processed immediately
- TransferExecuted events processed immediately

### Registry Discovery
If `registryAddress` is configured:
- Cron job queries the ModuleRegistry for active modules
- New modules are discovered and processed on each cron cycle
- Provides complete coverage even for dynamically deployed modules

### Recommended Setup

1. **Known modules**: Add to `moduleAddresses` for instant event processing
2. **Dynamic modules**: Configure `registryAddress` for automatic discovery
3. **Hybrid**: Use both for instant processing of known modules + discovery of new ones

```json
{
  "moduleAddresses": [
    "0x1111...",
    "0x2222..."
  ],
  "registryAddress": "0x3333..."
}
```

## Triggers

| Trigger | Purpose |
|---------|---------|
| `logTrigger` (ProtocolExecution) | Instant update on swaps, deposits, withdrawals |
| `logTrigger` (TransferExecuted) | Instant update on token transfers |
| `cronTrigger` | Periodic refresh + registry discovery |

## Running Locally

```bash
# Install dependencies
bun install

# Simulate the workflow
cre workflow simulate ./spending-oracle

# Select trigger type when prompted
```

## State Management

The oracle is stateless - state is reconstructed from blockchain events on each invocation:
1. Query historical ProtocolExecution events
2. Query historical TransferExecuted events
3. Build FIFO queues for acquired balances
4. Calculate spending in rolling window
5. Derive new spending allowance
6. Push batch update to contract

## Events Monitored

- `ProtocolExecution(address subAccount, address target, uint8 opType, address[] tokensIn, uint256[] amountsIn, address[] tokensOut, uint256[] amountsOut, uint256 spendingCost)`
- `TransferExecuted(address subAccount, address token, address recipient, uint256 amount, uint256 spendingCost)`
- `AcquiredBalanceUpdated(address subAccount, address token, uint256 amount)`
