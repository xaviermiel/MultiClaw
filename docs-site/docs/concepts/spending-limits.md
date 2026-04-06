---
sidebar_position: 3
title: Spending Limits
---

# Spending Limits

MultiClaw enforces spending limits in rolling time windows. Each agent (sub-account) has its own limit, tracked independently.

## Dual-mode limits

Each sub-account uses exactly one of two spending limit modes:

### BPS mode (`maxSpendingBps`)

Budget as a **percentage of Safe value** per window.

```
Budget = safeValueUSD * maxSpendingBps / 10000
```

- `500` BPS = 5% of Safe value per 24h
- Scales automatically as the portfolio grows or shrinks
- Best for: agents managing a proportional share of a portfolio

### USD mode (`maxSpendingUSD`)

Budget as a **fixed dollar amount** per window (18 decimals).

```
Budget = maxSpendingUSD (fixed)
```

- `1000e18` = $1,000 per 24h regardless of Safe value
- Does not scale — useful for capping absolute exposure
- Best for: payment agents, fixed-budget operations

Exactly one must be non-zero. Setting both or neither reverts.

## Rolling windows

Spending is tracked in rolling windows (default: 24 hours). The window resets when `block.timestamp >= windowStart + windowDuration`:

1. `cumulativeSpent` resets to zero
2. `windowSafeValue` snapshots the current Safe value
3. Fresh budget is available

The oracle updates `spendingAllowance` within each window based on operations observed on-chain.

## Hard safety cap

The `absoluteMaxSpendingBps` (default: 2000 = 20%) is a global backstop enforced on-chain:

```
cumulativeSpent + newCost <= windowSafeValue * absoluteMaxSpendingBps / 10000
```

This cap **cannot be exceeded** regardless of what the oracle reports. Even if the oracle is compromised and sets a high `spendingAllowance`, the cumulative on-chain counter blocks overspending.

## How spending is calculated

When an agent executes an operation:

1. The parser extracts `tokensIn` and `amountsIn` from the calldata
2. For each token, the module looks up the Chainlink price feed
3. USD value is computed: `amountIn * price / 10^decimals`
4. Acquired balance for that token is deducted first (free tokens)
5. Remaining cost is charged against `spendingAllowance`
6. `cumulativeSpent` is incremented on-chain

### Cost by operation type

| Operation |      Costs Spending?       |        Output Acquired?         |
| --------- | :------------------------: | :-----------------------------: |
| Swap      | Yes (original tokens only) |               Yes               |
| Deposit   | Yes (original tokens only) | Tracked for withdrawal matching |
| Withdraw  |         No (free)          |           Conditional           |
| Claim     |         No (free)          |           Conditional           |
| Approve   |      No (but capped)       |               N/A               |
| Repay     |      No (REPAY role)       |               N/A               |
| Transfer  |   Yes (original tokens)    |               N/A               |

## Configuration

The Safe owner configures limits via `setSubAccountLimits()`:

```solidity
module.setSubAccountLimits(
    agentAddress,   // sub-account
    500,            // maxSpendingBps (5%) — set to 0 for USD mode
    0,              // maxSpendingUSD — set to 0 for BPS mode
    86400           // windowDuration (24 hours)
);
```

The owner can also adjust the global safety caps:

```solidity
module.setAbsoluteMaxSpendingBps(3000);  // raise hard cap to 30%
module.setMaxOracleAcquiredBps(1500);    // lower oracle budget to 15%
```
