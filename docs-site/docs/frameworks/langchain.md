---
sidebar_position: 1
title: LangChain
---

# LangChain Integration

The `@multiclaw/langchain` adapter provides LangChain-compatible tools that let your agent execute guarded DeFi operations, transfer tokens, and check budget through natural language.

## Installation

```bash
npm install @multiclaw/core @multiclaw/langchain viem langchain
```

## Setup

```typescript
import { MultiClawClient } from "@multiclaw/core";
import { createMultiClawTools } from "@multiclaw/langchain";
import { privateKeyToAccount } from "viem/accounts";

const client = new MultiClawClient({ chain: "baseSepolia" });
const agentAccount = privateKeyToAccount("0xAGENT_PRIVATE_KEY");

const tools = createMultiClawTools({
  client,
  moduleAddress: "0xMODULE_ADDRESS",
  agentAccount,
});
```

## Available tools

`createMultiClawTools()` returns three LangChain `StructuredTool` instances:

### `MultiClawExecuteTool`

Execute a DeFi operation on a whitelisted protocol.

**Input schema:**

- `target` (string) — Protocol contract address
- `calldata` (string) — ABI-encoded function calldata

**Example agent prompt:** "Deposit 100 USDC into Aave V3"

### `MultiClawTransferTool`

Transfer tokens from the Safe to a recipient.

**Input schema:**

- `token` (string) — ERC-20 token address
- `recipient` (string) — Destination address
- `amount` (string) — Amount in token smallest unit

**Example agent prompt:** "Send 50 USDC to 0x..."

### `MultiClawBudgetTool`

Check the agent's remaining spending budget.

**Input schema:** None (reads current state)

**Returns:** Remaining allowance, usage percentage, Safe value

## Usage with a ReAct agent

```typescript
import { ChatOpenAI } from "@langchain/openai";
import { createReactAgent } from "langchain/agents";

const llm = new ChatOpenAI({ model: "gpt-4o" });

const agent = createReactAgent({
  llm,
  tools,
  messageModifier:
    "You are a DeFi agent. Use MultiClaw tools to execute operations. " +
    "Always check your budget before executing large operations.",
});

const result = await agent.invoke({
  messages: [{ role: "user", content: "Check my remaining budget" }],
});
```

## How it works

Each tool wraps a `MultiClawClient` method:

1. The LLM decides which tool to call based on the user's message
2. The tool calls the corresponding SDK method (e.g., `executeAsAgent`)
3. The SDK submits the transaction through the `DeFiInteractorModule`
4. The module enforces all 12 guardrail layers on-chain
5. The tool returns the result (tx hash or budget info) to the LLM

The security boundary is on-chain. Even if the LLM is jailbroken into calling tools with malicious parameters, the smart contract rejects unauthorized operations.
