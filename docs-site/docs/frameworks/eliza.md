---
sidebar_position: 2
title: Eliza
---

# Eliza Integration

The `@multiclaw/eliza` adapter provides an Eliza plugin with 4 actions for guarded DeFi operations.

## Installation

```bash
npm install @multiclaw/core @multiclaw/eliza viem
```

## Setup

```typescript
import { MultiClawClient } from "@multiclaw/core";
import { createMultiClawElizaPlugin } from "@multiclaw/eliza";
import { privateKeyToAccount } from "viem/accounts";

const plugin = createMultiClawElizaPlugin({
  client: new MultiClawClient({ chain: "baseSepolia" }),
  moduleAddress: "0xMODULE_ADDRESS",
  agentAccount: privateKeyToAccount("0xAGENT_PRIVATE_KEY"),
});
```

## Register the plugin

```typescript
eliza.registerPlugin(plugin);
```

## Available actions

| Action                   | Description                                        |
| ------------------------ | -------------------------------------------------- |
| `MULTICLAW_EXECUTE`      | Execute a DeFi operation on a whitelisted protocol |
| `MULTICLAW_TRANSFER`     | Transfer tokens from the Safe                      |
| `MULTICLAW_CHECK_BUDGET` | Check remaining spending budget                    |
| `MULTICLAW_VAULT_STATUS` | Get vault status (paused, Safe value, agents)      |

## Plugin structure

```typescript
const plugin = {
  name: "multiclaw",
  description: "On-chain guardrails for AI agent DeFi operations",
  actions: [
    /* MULTICLAW_EXECUTE */
    /* MULTICLAW_TRANSFER */
    /* MULTICLAW_CHECK_BUDGET */
    /* MULTICLAW_VAULT_STATUS */
  ],
};
```

Each action follows the Eliza action interface with `name`, `description`, `examples`, `validate`, and `handler` methods.
