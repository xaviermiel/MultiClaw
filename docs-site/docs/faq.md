---
sidebar_position: 8
title: FAQ
---

# FAQ

## General

### What is MultiClaw?

MultiClaw is an on-chain security framework that gives AI agents controlled access to DeFi. It wraps a Safe multisig with a permission layer enforcing spending limits, protocol whitelists, and role-based access in smart contracts.

### How is this different from just using a multisig?

A multisig requires human approval for every transaction. MultiClaw lets an AI agent act autonomously within pre-defined guardrails — it can swap, deposit, and withdraw without human intervention, but cannot exceed its budget or interact with unapproved protocols.

### What chains are supported?

Base (Sepolia testnet now, mainnet coming soon). The contracts are chain-agnostic and work on any EVM chain with Safe and Chainlink price feeds.

### Is this audited?

An internal security review has been completed. An external audit is planned before mainnet launch. See the [Security Model](./security) page for the trust model and worst-case analysis.

## Agents

### What happens if my agent is hacked?

The on-chain guardrails still apply. A compromised agent cannot exceed its spending limit, interact with non-whitelisted protocols, or redirect funds. The Safe owner can immediately revoke the agent's role to cut off access.

### Can an agent drain the Safe?

No. Even in the worst case — both the oracle and the agent fully compromised — the attacker is still bound by every guardrail except the spending budget cap. They can only call protocols on the agent's allowlist, only execute registered operation types, and the recipient of every swap/deposit/withdrawal is forced to be the Safe itself (parsers extract it from calldata and the module reverts otherwise). The 40% figure (`absoluteMaxSpendingBps + maxOracleAcquiredBps` by default) is the size of the **spending budget bucket** — not what an attacker walks away with. For a tightly-scoped agent (e.g. Aave V3 supply/withdraw, Safe as recipient), the worst case is the attacker repeatedly cycles deposits — annoying but no value leaves the Safe. For a payment agent with a small recipient allowlist, they can at most exhaust the daily budget paying out to those exact addresses. No funds reach an address the operator did not explicitly authorize. Both budget caps are configurable; switching to oracleless mode replaces them with a fixed USD limit you set yourself.

### Can I have multiple agents on one Safe?

Yes. Each agent is a sub-account with its own roles, spending limits, and protocol allowlists. A single Safe can have different agents for different strategies.

### What AI frameworks are supported?

The SDK has adapters for [LangChain](./frameworks/langchain), [Eliza](./frameworks/eliza), and [GOAT](./frameworks/goat). You can also use the core `@multiclaw/core` SDK directly with any framework.

## Spending limits

### What's the difference between BPS and USD mode?

**BPS mode** sets the budget as a percentage of the Safe's total value (e.g., 5% per day). It scales with the portfolio. **USD mode** sets a fixed dollar amount (e.g., $1,000/day). Exactly one must be configured per agent.

### What is the Acquired Balance Model?

Tokens received from operations (e.g., ETH from a USDC swap) are marked as "acquired" and can be re-used without costing additional spending budget. This prevents double-counting when chaining operations. See [Acquired Balance Model](./concepts/acquired-balance-model).

### When does the spending window reset?

When `block.timestamp >= windowStart + windowDuration`. Default window is 24 hours. On reset, `cumulativeSpent` resets to zero and `windowSafeValue` snapshots the current portfolio value.

## Oracle

### What is the oracle?

A self-hosted Node.js service that monitors on-chain events and updates spending state on the module. It tracks spending in rolling windows, matches deposits to withdrawals for acquired balance, and periodically refreshes the Safe's portfolio value.

### What happens if the oracle goes down?

Agent operations freeze after 60 minutes (the oracle staleness threshold). The Safe and its funds are unaffected. Restart the oracle and it rebuilds state from on-chain events.

### Can the oracle steal funds?

No. The oracle can only update spending state — it cannot submit transactions through the module. A compromised oracle alone cannot extract any funds. Combined with a compromised agent, it can raise the spending budget to the hard cap (default 40% per window), but the attacker is still constrained to the agent's allowlist and recipient rules — they can only spend within the operations the agent was already configured to perform. They cannot redirect funds to attacker-controlled addresses.

### Can I run a vault without an oracle?

Yes. Deploy with `oracle = address(0)` for oracleless mode. Spending is enforced solely by on-chain cumulative tracking against a fixed USD limit (`maxSpendingUSD`). No oracle updates needed, no oracle staleness risk, no oracle compromise risk. The trade-off: only fixed USD limits are available (not percentage-of-portfolio), and only Tier 1 acquired balance works (swap outputs). See [Security Model — Oracleless mode](./security#oracleless-mode).

## Deployment

### Do I need permission to deploy a vault?

No. The `AgentVaultFactory` is permissionless — anyone can deploy. No approval, no KYC, no waitlist.

### Do I need to deploy my own Safe?

Yes. MultiClaw uses a "Bring Your Own Safe" model. Deploy a Safe via [app.safe.global](https://app.safe.global), then deploy an Agent Vault pointing to your Safe address.

### How do I enable the module on my Safe?

After the factory deploys and configures the module, go to your Safe UI: **Settings > Modules > Add Module** and paste the module address. This requires a multisig transaction.

### How much gas does deployment cost?

The factory uses ERC-1167 minimal proxy clones, so deployment is gas-efficient (~200-300k gas for the clone + configuration, compared to ~4M+ for a full contract deployment).
