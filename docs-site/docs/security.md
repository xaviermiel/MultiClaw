---
sidebar_position: 7
title: Security Model
---

# Security Model

MultiClaw's security is enforced **on-chain** in smart contracts, not off-chain in prompts or middleware. This page explains the trust model, oracle constraints, and worst-case analysis.

## Why on-chain enforcement

Traditional AI agent security approaches:

| Approach                | Failure mode                                     |
| ----------------------- | ------------------------------------------------ |
| System prompts          | Jailbreaking, prompt injection                   |
| Middleware filters      | Bugs, bypasses, API key theft                    |
| Rate limiting           | Account sharing, key compromise                  |
| Off-chain rules engines | Single point of failure, can be patched/disabled |

MultiClaw's approach: the rules live in an immutable smart contract. Even if an attacker gains full control of the agent (private key + LLM access), the contract still enforces:

- Role-based access (wrong role = revert)
- Protocol allowlists (unknown target = revert)
- Spending caps (over budget = revert)
- Recipient validation (wrong recipient = revert)
- Cumulative on-chain counters (cannot be reset by oracle)

The only way to bypass the guardrails is to compromise the **Safe owner's keys** — which is a separate, well-understood security problem.

## Trust model

### What each actor can do

| Actor          | Trust level | Can do                                                  | Cannot do                                                         |
| -------------- | ----------- | ------------------------------------------------------- | ----------------------------------------------------------------- |
| **Safe owner** | Full trust  | Reconfigure, pause, revoke, remove module               | N/A (full control)                                                |
| **Agent**      | Constrained | Execute operations within role + budget + allowlist     | Exceed budget, access non-whitelisted protocols, redirect outputs |
| **Oracle**     | Bounded     | Update spending allowance, acquired balance, Safe value | Reset cumulative counters, exceed oracle budget caps              |
| **Parser**     | Stateless   | Decode calldata, extract tokens/amounts/recipient       | Modify state, access funds                                        |

### Oracle compromise analysis

The oracle is an off-chain service (EOA) that updates spending state on the module. Its power is **bounded by on-chain constraints**.

**Important:** A compromised oracle alone is not enough to extract funds — it can only update state, not execute transactions. The worst-case scenario requires **both** the oracle **and** the agent to be compromised simultaneously (or the agent to be publicly accessible, e.g., in a "Break the Vault" challenge where anyone can instruct it). In that combined scenario, the attacker can maximize spending within the on-chain bounds below:

**What a compromised oracle CAN do:**

- Set `spendingAllowance` up to the sub-account's configured cap (`maxSpendingBps * safeValue` or `maxSpendingUSD`)
- Grant acquired balance up to `maxOracleAcquiredBps` of Safe value (default: 20%)
- Inflate `safeValue` (but only affects future windows, not current cumulative cap)

**What a compromised oracle CANNOT do:**

- Reset `cumulativeSpent` (incremented only by execution functions, on-chain)
- Overwrite state with stale data (version counters enforce ordering)
- Grant unlimited acquired balance (capped per window)
- Bypass the cumulative spending cap

### Maximum damage per window

```
Spending budget ceiling = per-account (maxSpendingBps × safeValue OR maxSpendingUSD)
                        + maxOracleAcquiredBps (default 20% of Safe value)
```

**This is the maximum spending budget the on-chain caps allow — not the amount an attacker can extract.** Even with both the oracle and the agent fully compromised, the attacker is still constrained to the agent's normal scope of operations. They cannot escape the other 11 guardrail layers:

- They can only call protocols on the agent's **allowlist**. Random target contracts revert.
- They can only execute **registered operation types** (swap / deposit / withdraw / approve / repay).
- The **recipient** of every swap, deposit, and withdrawal must be the Safe itself — never an attacker address. Parsers extract the recipient from calldata and the module reverts otherwise.
- **Approve calls** are capped against the same spending budget, with the spender forced to be on the allowlist.
- The **role check** still applies — the agent's identity is fixed to its granted role.

In practical terms: the per-account cap plus the 20% oracle-acquired budget is the size of the daily "budget bucket" that hostile inputs can drain — but only by performing operations the agent was already configured to perform. For a yield-farming agent restricted to Aave V3 supply/withdraw with the Safe as recipient, the worst case is that the attacker repeatedly supplies and withdraws — annoying but not value-extracting. For a payment agent with a small recipient allowlist, the worst case is that the attacker exhausts the daily budget paying out to those exact addresses. In every case, no funds reach an address the operator did not explicitly authorize.

The ceiling matters most when the agent has very broad permissions (e.g. swap authority across many DEXs with arbitrary token outputs). For tightly-scoped agents, the on-chain budget is rarely the binding constraint — the allowlist and recipient checks are.

Per-account spending is configured via `setSubAccountLimits()`; the oracle-acquired budget is configured via `setMaxOracleAcquiredBps()`. Lowering either shrinks the budget bucket further. For maximum determinism, switch to oracleless mode and the ceiling becomes a fixed USD amount you set yourself.

### Mitigation layers

| Layer                      | What it protects                                                 | On-chain? |
| -------------------------- | ---------------------------------------------------------------- | --------- |
| `cumulativeSpent` counter  | Hard spending cap per window                                     | Yes       |
| `windowSafeValue` snapshot | Locks Safe value at window start                                 | Yes       |
| Tier 1 swap marking        | Trustless acquired balance for swaps                             | Yes       |
| `maxOracleAcquiredBps`     | Caps oracle-granted acquired tokens                              | Yes       |
| Version counters           | Prevents stale oracle writes                                     | Yes       |
| Per-account allowance cap  | `maxSpendingBps × safeValue` or `maxSpendingUSD` per sub-account | Yes       |

## Recovery from compromise

### Agent key compromised

1. Call `revokeRole(agent, DEFI_EXECUTE_ROLE)` — immediate, blocks all operations
2. Optionally `pause()` the module for extra safety
3. Deploy a new agent with a fresh key

### Oracle key compromised

1. Call `setAuthorizedOracle(newOracleAddress)` — rotates the oracle
2. Previous oracle can no longer update state
3. Agent operations freeze after 60 minutes (oracle staleness check)
4. Maximum damage during the window is bounded by the caps above

### Module vulnerability

1. `pause()` — immediate emergency stop
2. Disable the module on the Safe (Settings > Modules > Remove)
3. Funds remain safe in the Safe — the module never holds funds

## Audit status

The contracts have undergone an internal security review. See the [Security Audit document](https://github.com/xaviermiel/MultiClaw/blob/main/docs/SECURITY_AUDIT.md) for findings and remediation status. An external audit is planned before mainnet launch.

## Oracleless mode

For vaults that want **zero off-chain trust**, MultiClaw supports oracleless mode. Deploy with `oracle = address(0)` and the module operates with no oracle dependency at all.

### How it works

| Feature                                       | Normal mode                                            | Oracleless mode                           |
| --------------------------------------------- | ------------------------------------------------------ | ----------------------------------------- |
| Oracle freshness check                        | Required (60 min)                                      | Skipped                                   |
| Safe value updates                            | Oracle-driven                                          | Not needed                                |
| Spending limit mode                           | BPS or USD                                             | **USD only**                              |
| `spendingAllowance` check                     | Oracle sets it                                         | Skipped — only `cumulativeSpent` enforced |
| Tier 1 acquired (swaps)                       | On-chain                                               | On-chain (unchanged)                      |
| Tier 2 acquired (withdraw/claim)              | Oracle grants                                          | Disabled                                  |
| Spending budget ceiling                       | Per-account cap + `maxOracleAcquiredBps` (default 20%) | `maxSpendingUSD` (fixed, deterministic)   |
| Other guardrails (allowlist, recipient, role) | Always enforced                                        | Always enforced                           |

### Trade-offs

- **No BPS mode** — percentage-of-portfolio limits require a portfolio valuation, which needs an oracle. Only fixed USD limits are available.
- **No Tier 2 acquired balance** — tokens received from withdrawals and claims are not automatically marked as acquired (only swap outputs are, via on-chain Tier 1 marking). This means withdrawals and claims use spending budget when the tokens are later re-used.
- **Simpler trust model** — the worst-case damage is exactly `maxSpendingUSD` per window. No oracle compromise risk. Fully deterministic.

### When to use oracleless mode

- Public-facing agents (e.g., "Break the Vault" challenges) where minimizing trust surface is critical
- Simple transfer or payment agents with fixed dollar budgets
- Any vault where eliminating oracle dependency outweighs the convenience of BPS-based limits

### Switching modes

The Safe owner can switch between modes at any time:

```solidity
// Enter oracleless mode
module.setAuthorizedOracle(address(0));

// Return to oracle mode
module.setAuthorizedOracle(newOracleAddress);
```

## Design principles

1. **Defense in depth** — 12 layers, not one
2. **On-chain enforcement** — rules in smart contracts, not prompts
3. **Bounded trust** — every actor has hard limits
4. **Fail-safe defaults** — operations freeze when oracle is stale
5. **Owner sovereignty** — Safe owner can always pause, revoke, or remove
