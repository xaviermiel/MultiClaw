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

- Set `spendingAllowance` up to `absoluteMaxSpendingBps` of Safe value (default: 20%)
- Grant acquired balance up to `maxOracleAcquiredBps` of Safe value (default: 20%)
- Inflate `safeValue` (but only affects future windows, not current cumulative cap)

**What a compromised oracle CANNOT do:**

- Reset `cumulativeSpent` (incremented only by execution functions, on-chain)
- Overwrite state with stale data (version counters enforce ordering)
- Grant unlimited acquired balance (capped per window)
- Bypass the cumulative spending cap

### Maximum damage per window

```
Worst case = absoluteMaxSpendingBps + maxOracleAcquiredBps
           = 20% + 20%
           = 40% of Safe value per rolling window
```

With default settings, a compromised oracle **combined with** a compromised agent (or a publicly usable agent) can extract at most **40% of Safe value per 24-hour window**. A compromised oracle alone cannot extract anything — it has no ability to submit transactions through the module.

Both caps are **configurable by the Safe owner** via `setAbsoluteMaxSpendingBps()` and `setMaxOracleAcquiredBps()`. Lowering them directly reduces worst-case exposure. For example, setting both to 10% limits maximum damage to 20% per window instead of 40%.

### Mitigation layers

| Layer                      | What it protects                     | On-chain? |
| -------------------------- | ------------------------------------ | --------- |
| `cumulativeSpent` counter  | Hard spending cap per window         | Yes       |
| `windowSafeValue` snapshot | Locks Safe value at window start     | Yes       |
| Tier 1 swap marking        | Trustless acquired balance for swaps | Yes       |
| `maxOracleAcquiredBps`     | Caps oracle-granted acquired tokens  | Yes       |
| Version counters           | Prevents stale oracle writes         | Yes       |
| `absoluteMaxSpendingBps`   | Global spending backstop             | Yes       |
| Per-account USD cap        | Fixed dollar limit per agent         | Yes       |

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

## Design principles

1. **Defense in depth** — 12 layers, not one
2. **On-chain enforcement** — rules in smart contracts, not prompts
3. **Bounded trust** — every actor has hard limits
4. **Fail-safe defaults** — operations freeze when oracle is stale
5. **Owner sovereignty** — Safe owner can always pause, revoke, or remove
