---
slug: /
sidebar_position: 1
title: Introduction
---

# MultiClaw

**On-chain guardrails for AI agents.** MultiClaw wraps a [Safe](https://safe.global) multisig with a permission layer that enforces spending limits, protocol whitelists, and role-based access at the smart contract level.

Even if an agent is fully compromised — jailbreak, prompt injection, plugin exploit — the on-chain guardrails prevent it from:

- Spending more than its rolling budget
- Interacting with non-whitelisted protocols
- Approving tokens to unknown contracts
- Redirecting swap or withdrawal output to an attacker's address

## How it works

```
AI Agent  ──>  DeFiInteractorModule  ──>  Safe Multisig  ──>  DeFi Protocol
                 (12-layer checks)        (holds funds)
```

1. An AI agent calls the **DeFiInteractorModule** to execute a DeFi operation
2. The module enforces **12 layers of security checks** — role, oracle freshness, operation type, target allowlist, recipient validation, spending limits, and more
3. If all checks pass, the module executes the transaction through the **Safe** using `execTransactionFromModule`
4. If any check fails, the transaction reverts — the agent cannot bypass it

## Key features

- **Permissionless deployment** — anyone can deploy an Agent Vault via factory contracts, no approval needed
- **Dual spending limit modes** — percentage of Safe value (BPS) or fixed USD amount
- **Acquired Balance Model** — tokens received from operations are free to re-use, preventing double-counting
- **Oracle compromise protection** — a compromised oracle cannot redirect funds; even combined with a compromised agent, the attacker is bound to the agent's allowlist and the Safe is the only valid recipient
- **10 protocol parsers** — Aave V3, Morpho, Uniswap V3/V4, 1inch, KyberSwap, Paraswap, Merkl
- **Framework adapters** — LangChain, Eliza, GOAT integrations out of the box

## Quick links

| Resource                                   | Description                                        |
| ------------------------------------------ | -------------------------------------------------- |
| [Getting Started](./getting-started)       | Deploy your first Agent Vault in 5 minutes         |
| [Concepts](./concepts/agent-vaults)        | Understand vaults, guardrails, and spending limits |
| [SDK Reference](./sdk/client)              | `@multiclaw/core` API documentation                |
| [Framework Guides](./frameworks/langchain) | LangChain, Eliza, GOAT integration                 |
| [Smart Contracts](./contracts/module)      | Solidity contract reference                        |
| [Security Model](./security)               | Trust model and oracle compromise analysis         |

## Architecture overview

| Component              | Role                                                               |
| ---------------------- | ------------------------------------------------------------------ |
| `DeFiInteractorModule` | Core Zodiac module — 12-layer security enforcement                 |
| `AgentVaultFactory`    | One-transaction vault deployment (ERC-1167 clones)                 |
| `PresetRegistry`       | Template configurations (DeFi Trader, Yield Farmer, Payment Agent) |
| `ModuleRegistry`       | Central index for oracle module discovery                          |
| 10 Protocol Parsers    | Calldata decoders for supported DeFi protocols                     |
| Spending Oracle        | Off-chain service tracking spending in rolling windows             |
| Safe Value Oracle      | Portfolio valuation via Chainlink price feeds                      |

## Deployed on

- **Base Sepolia** (testnet) — fully deployed and tested
- **Base mainnet** — coming soon
