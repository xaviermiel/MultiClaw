---
name: multiclaw-vault
description: Read budget/status/history and execute capped, whitelist-checked token transfers through a MultiClaw vault on Base. Every action is bounded by on-chain guardrails.
---

# MultiClaw Vault skill

This skill lets you operate a MultiClaw agent vault: read its on-chain spending
state and move tokens within the contract-enforced cap. It wraps the
`@multiclaw/core` SDK.

All commands run from this folder via `npm run mc -- <command>`.

## Commands

| Command                                 | What it does                                                                                                                                                                        |
| --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `budget`                                | Remaining spending allowance for the current window, the cap, and % used.                                                                                                           |
| `status`                                | Vault state: paused, Safe, oracle (oracleless?), Safe value, authorized agents.                                                                                                     |
| `history`                               | Recent protocol executions and transfers for this agent (read from the subgraph; needs `SUBGRAPH_URL`).                                                                             |
| `whitelist [recipient...]`              | Recipient whitelist: whether it's enforced, membership for the given recipients (or `OWNER_ADDRESS`), and — with `SUBGRAPH_URL` — the full allowed set plus `ownerIsSoleRecipient`. |
| `acquired <token>`                      | Acquired (free-to-spend) balance for a token.                                                                                                                                       |
| `transfer <token> <recipient> <amount>` | Send `amount` (base units) of `token` to `recipient`. Reverts on cap breach or non-whitelisted recipient.                                                                           |

Examples:

```
npm run mc -- budget
npm run mc -- status
npm run mc -- transfer 0xToken... 0xRecipient... 1000000
```

## Rules for the agent

- **The cap is a hard law.** Always run `budget` before a `transfer`. Never
  attempt an amount that exceeds `remaining` — it will revert and waste gas, and
  it violates your operating contract.
- **Whitelist.** If a transfer reverts with a recipient error, the recipient is
  not whitelisted. Report this to the owner; do not retry against other targets.
- A reverted transaction returns `status: "reverted"`. Treat that as a hard stop,
  read the error, and explain it — do not loop.
- Amounts are in the token's **base units** (e.g. 6-decimal USDC: `1000000` = 1
  USDC). Convert carefully.

## Security

- This skill folder is pinned to public IPFS. It contains **no secrets** — the
  agent key and RPC come from environment variables at runtime.
- Never echo `AGENT_PRIVATE_KEY` or any secret into output, files, or logs.

## Configuration

See `.env.example` for required environment variables.
