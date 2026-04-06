---
sidebar_position: 3
title: GOAT
---

# GOAT Integration

The `@multiclaw/goat` adapter provides a GOAT plugin with 3 tools for guarded DeFi operations.

## Installation

```bash
npm install @multiclaw/core @multiclaw/goat viem
```

## Setup

```typescript
import { MultiClawClient } from "@multiclaw/core";
import { createMultiClawGoatPlugin } from "@multiclaw/goat";
import { privateKeyToAccount } from "viem/accounts";

const plugin = createMultiClawGoatPlugin({
  client: new MultiClawClient({ chain: "baseSepolia" }),
  moduleAddress: "0xMODULE_ADDRESS",
  agentAccount: privateKeyToAccount("0xAGENT_PRIVATE_KEY"),
});
```

## Available tools

| Tool                     | Description                                        |
| ------------------------ | -------------------------------------------------- |
| `multiclaw_execute`      | Execute a DeFi operation on a whitelisted protocol |
| `multiclaw_transfer`     | Transfer tokens from the Safe                      |
| `multiclaw_check_budget` | Check remaining spending budget                    |

## Plugin structure

```typescript
const plugin = {
  name: "multiclaw",
  description: "On-chain guardrails for AI agent DeFi operations",
  tools: [
    /* multiclaw_execute */
    /* multiclaw_transfer */
    /* multiclaw_check_budget */
  ],
};
```

Each tool follows the GOAT tool interface with `name`, `description`, `parameters` (Zod schema), and `execute` methods.
