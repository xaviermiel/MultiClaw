---
sidebar_position: 4
title: Acquired Balance Model
---

# Acquired Balance Model

The Acquired Balance Model is how MultiClaw distinguishes between **original tokens** (already in the Safe) and **acquired tokens** (received from operations). This prevents double-counting when an agent swaps or deposits and then re-uses the output.

## The problem

Without this model, a swap would be counted as spending twice:

1. Agent swaps 1,000 USDC for 0.5 ETH — costs $1,000 of spending budget
2. Agent deposits 0.5 ETH into Aave — costs another ~$1,000 of budget
3. Total budget consumed: $2,000 for what is effectively one position

The Acquired Balance Model solves this: the 0.5 ETH received from the swap is marked as **acquired**, so depositing it is **free**.

## How it works

### Tier 1: On-chain (trustless)

When a **swap** executes successfully, the module immediately marks the output tokens as acquired on-chain:

```
acquiredBalance[agent][tokenOut] += amountOut
```

No oracle involvement — this is fully trustless. The output amount is extracted from the parser's calldata decoding.

### Tier 2: Oracle-granted (bounded)

For **withdrawals** and **claims**, the oracle observes the operation events and grants acquired balance off-chain:

```solidity
module.updateAcquiredBalance(agent, token, expectedVersion, newBalance);
```

This is bounded by `maxOracleAcquiredBps` (default: 20% of Safe value per window). Even a compromised oracle cannot grant unlimited acquired tokens.

### Version counters

Each acquired balance has a version counter for optimistic concurrency:

```solidity
mapping(address => mapping(address => uint256)) public acquiredBalanceVersion;
```

The oracle must provide `expectedVersion` when updating. If another update happened in between, the call reverts — preventing stale overwrites.

## Spending deduction

When an agent uses tokens in an operation, acquired balance is deducted first:

```
costUSD = totalValueUSD
if acquiredBalance[agent][tokenIn] >= amountIn:
    costUSD = 0  // entirely free
else:
    costUSD -= acquiredValueUSD  // partial deduction
    acquiredBalance[agent][tokenIn] = 0
```

Only the net new spending is charged against the agent's budget.

## Expiry

Acquired tokens expire after the spending window resets (default: 24 hours). When the window resets:

1. `cumulativeSpent` resets to zero
2. Acquired balances from the previous window become "original" tokens
3. Using them now costs spending budget

This prevents gaming where an agent accumulates acquired balance indefinitely.

## Example walkthrough

Starting state: Safe has 10,000 USDC. Agent has 5% budget = $500.

| Step | Operation                   | Cost                  | Acquired                 | Budget Left |
| ---- | --------------------------- | --------------------- | ------------------------ | ----------- |
| 1    | Swap 500 USDC for 0.25 ETH  | $500                  | 0.25 ETH acquired        | $0          |
| 2    | Deposit 0.25 ETH into Aave  | $0 (acquired)         | aETH tracked             | $0          |
| 3    | Withdraw 0.25 ETH from Aave | $0 (withdraw is free) | 0.25 ETH (oracle grants) | $0          |
| 4    | Swap 0.25 ETH for 500 USDC  | $0 (acquired ETH)     | 500 USDC acquired        | $0          |

The agent executed 4 operations but only consumed $500 of budget — the initial swap. All subsequent operations used acquired tokens.

## Oracle compromise protection

A compromised oracle alone cannot extract funds — it can only update state, not execute transactions. Combined with a compromised agent, the on-chain spending budget cap is:

```
spending budget ceiling = per-account (maxSpendingBps × safeValue OR maxSpendingUSD)
                        + maxOracleAcquiredBps (default 20%)
```

**This is the size of the spending budget bucket — not what an attacker walks away with.** The agent is still bound by every other guardrail: protocol allowlist, registered operation types, recipient validation (must be the Safe), approve cap. An attacker can only perform operations the agent was already configured to perform — they cannot redirect funds to addresses outside the allowlist.

Per-account spending is configured via `setSubAccountLimits()`; the oracle-acquired budget is configured via `setMaxOracleAcquiredBps()`. Lowering either shrinks the budget bucket further. For maximum determinism, switch to oracleless mode and the ceiling becomes a fixed USD amount.
