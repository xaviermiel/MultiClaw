---
sidebar_position: 1
title: Agent Vaults
---

# Agent Vaults

An **Agent Vault** is a Safe multisig paired with a `DeFiInteractorModule` that gives an AI agent controlled access to DeFi protocols. The Safe holds the funds; the module enforces what the agent can do with them.

## Components

```
┌─────────────────────────────────────┐
│  Safe Multisig (holds all funds)    │
│                                     │
│  ┌─────────────────────────────┐    │
│  │  DeFiInteractorModule       │    │
│  │  - Role-based access        │    │
│  │  - Spending limits          │    │
│  │  - Protocol allowlists      │    │
│  │  - Calldata parsing         │    │
│  │  - Oracle state             │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
         ↑                    ↑
     AI Agent            Safe Owner
   (limited ops)       (full control)
```

### Safe

A [Safe](https://safe.global) (formerly Gnosis Safe) multisig wallet. It holds all tokens and ETH. The agent never has direct access to the Safe's private keys — it operates exclusively through the module.

### DeFiInteractorModule

A custom [Zodiac](https://zodiac.wiki) module that acts as a gatekeeper. When the agent wants to execute a transaction, it calls the module, which:

1. Validates the agent has the right role
2. Checks the operation against 12 security layers
3. If approved, calls `execTransactionFromModule()` on the Safe

The module is **owned by the Safe** — the Safe owner retains full control to reconfigure, pause, or remove the module at any time.

## Roles

Each agent is assigned one or more roles that determine what operations it can perform:

| Role ID | Name                 | Permissions                                                                       |
| ------- | -------------------- | --------------------------------------------------------------------------------- |
| 1       | `DEFI_EXECUTE_ROLE`  | Execute DeFi operations (swap, deposit, withdraw, claim) on whitelisted protocols |
| 2       | `DEFI_TRANSFER_ROLE` | Transfer tokens from the Safe to allowed recipients                               |
| 3       | `DEFI_REPAY_ROLE`    | Repay debt positions (no spending cost)                                           |

Roles are per-agent (sub-account). An agent can hold multiple roles. The Safe owner grants and revokes roles via `grantRole()` and `revokeRole()`.

## Sub-accounts

Each agent address is a **sub-account** with its own:

- **Spending limits** — max BPS or USD per rolling window
- **Protocol allowlist** — which contracts this agent can interact with
- **Spending allowance** — current remaining budget (managed by oracle)
- **Acquired balances** — tokens received from operations (free to re-use)

This means a single Safe can have multiple agents with different permissions and budgets.

## Deployment

Agent Vaults are deployed through the `AgentVaultFactory`:

1. Factory deploys a `DeFiInteractorModule` clone (ERC-1167 minimal proxy)
2. Factory configures roles, spending limits, protocol allowlists, parsers, selectors, and price feeds
3. Factory transfers module ownership to the Safe
4. Factory registers the module in the `ModuleRegistry`
5. Safe owner enables the module (one multisig transaction)

The factory is **permissionless** — no approval needed to deploy.

## Ownership model

```
Deployment:   Factory (temporary owner) → configures → transfers to Safe
Runtime:      Safe (owner) → can reconfigure, pause, revoke, remove
Agent:        Limited executor → can only call module functions matching its role
Oracle:       Constrained updater → can only update spending state within bounds
```

The Safe owner can always:

- Pause/unpause the module (emergency stop)
- Grant or revoke agent roles
- Change spending limits
- Add or remove protocol allowlists
- Update the oracle address (or set to `address(0)` for oracleless mode)
- Remove the module entirely

## Oracleless mode

Vaults can be deployed without an oracle (`oracle = address(0)`). In this mode:

- Spending is governed solely by on-chain cumulative tracking against a fixed USD limit
- No oracle freshness checks — agents can operate without any off-chain dependency
- Only Tier 1 acquired balance works (swap outputs marked on-chain)
- BPS-based spending limits are not available (they require portfolio valuation)
- The Safe owner can switch to oracle mode later via `setAuthorizedOracle()`

This is ideal for public-facing agents, payment bots, or any vault where minimizing trust surface matters more than dynamic portfolio-based limits. See [Security Model — Oracleless mode](../security#oracleless-mode) for the full analysis.
