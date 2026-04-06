---
sidebar_position: 2
title: Guardrails
---

# 12-Layer Guardrails

Every transaction an AI agent attempts must pass through 12 security layers enforced on-chain by the `DeFiInteractorModule`. Failure at any layer causes the transaction to revert.

## The layers

### 1. Role check

Is the agent authorized? The module checks `subAccountRoles[msg.sender][roleId]`. Only agents with the correct role can call `executeOnProtocol()` (requires `DEFI_EXECUTE_ROLE`) or `transferToken()` (requires `DEFI_TRANSFER_ROLE`).

### 2. Oracle freshness

Is the oracle data recent? If `lastOracleUpdate[subAccount]` is older than 60 minutes, all operations are blocked. This prevents agents from operating with stale spending data.

### 3. Operation classification

Is the function selector registered? Each selector (e.g., `0x617ba037` for Aave `supply`) is mapped to an `OperationType` (SWAP, DEPOSIT, WITHDRAW, CLAIM, APPROVE, REPAY). Unknown selectors are rejected.

### 4. Target allowlist

Is the protocol whitelisted for this agent? Each sub-account has its own `allowedAddresses` mapping. The target contract must be explicitly allowed.

### 5. Parser extraction

Decode the calldata. If the target has a registered `ICalldataParser`, the module calls `parseCalldata()` to extract:

- `tokensIn` / `amountsIn` — what the Safe is sending
- `tokensOut` / `amountsOut` — what the Safe will receive
- `recipient` — where the output goes

### 6. Recipient guard

Does the output go to the Safe? For swaps, deposits, and withdrawals, the module verifies that `recipient == safeAddress`. If a parser extracts a different recipient, the transaction reverts — preventing an attacker from redirecting funds.

### 7. Spending limit

Does the operation fit within the remaining budget? The module estimates the USD value of `tokensIn` using Chainlink price feeds, then checks if `spendingAllowance[subAccount] >= costUSD`.

Acquired tokens (from prior operations) are deducted first — they don't cost spending.

### 8. Cumulative cap

On-chain cumulative spending check. Even if the oracle sets a generous allowance, `cumulativeSpent[subAccount]` tracks total spending within the current window. The module enforces:

```
cumulativeSpent + costUSD <= windowSafeValue * absoluteMaxSpendingBps / 10000
```

This is the hard safety backstop (default: 20% of Safe value per window). The oracle **cannot** reset this counter.

### 9. Acquired balance tracking

Mark received tokens as "acquired." For swap operations, the module records `acquiredBalance[subAccount][tokenOut] += amountOut` on-chain. These tokens are free to use in subsequent operations within the same window.

### 10. Swap output marking (Tier 1)

Trustless, on-chain acquired marking. When a swap executes, the module marks the output tokens as acquired immediately — no oracle involvement. This is **Tier 1** (fully trustless).

### 11. Approve cap

For `approve()` calls: the spender must be whitelisted, and the approval amount is capped by the agent's remaining spending allowance. This prevents unlimited token approvals.

### 12. Safe execution

Final step: `execTransactionFromModule()`. The module calls the Safe, which executes the actual transaction. The module uses `nonReentrant` to prevent reentrancy attacks.

## Why on-chain matters

Traditional AI agent security relies on prompt engineering or middleware filters. These are **off-chain** — they can be bypassed through:

- Prompt injection / jailbreaking
- Plugin vulnerabilities
- API key compromise
- Middleware bugs

MultiClaw's guardrails are **on-chain smart contract logic**. Even if an attacker gains full control of the agent (its private key and LLM), the smart contract still enforces every limit. The attacker cannot:

- Bypass the role check (wrong role = revert)
- Interact with non-whitelisted protocols (target check = revert)
- Exceed the spending limit (cumulative cap = revert)
- Redirect outputs to their address (recipient guard = revert)

The only way to circumvent the guardrails is to compromise the Safe owner's keys — which is a separate, well-understood security problem with established solutions (hardware wallets, multi-sig, social recovery).
