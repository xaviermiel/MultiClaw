---
sidebar_position: 2
title: Getting Started
---

# Getting Started

Deploy an AI Agent Vault and execute your first guarded DeFi operation in under 5 minutes.

## Prerequisites

- Node.js 18+
- An Ethereum wallet with funds on Base (or Base Sepolia for testing)
- A [Safe](https://app.safe.global) multisig deployed on Base

## 1. Install the SDK

```bash
npm install @multiclaw/core viem
```

## 2. Create a client

```typescript
import { MultiClawClient } from "@multiclaw/core";

const client = new MultiClawClient({
  chain: "baseSepolia", // or "base" for mainnet
});
```

## 3. Deploy an Agent Vault

An Agent Vault is a `DeFiInteractorModule` configured and attached to your Safe. The `AgentVaultFactory` handles deployment, configuration, and ownership transfer in a single transaction.

The `oracle` parameter is the address of the oracle service that tracks spending. The MultiClaw oracle auto-discovers new vaults via the `ModuleRegistry` — you don't need to run your own. Use the shared oracle address provided by the MultiClaw team. If you need a custom oracle (e.g., self-hosted), see the `oracle/` directory in the repo.

```typescript
import { privateKeyToAccount } from "viem/accounts";

const deployer = privateKeyToAccount("0xYOUR_PRIVATE_KEY");

const vault = await client.createAgentVault(
  {
    safe: "0xYOUR_SAFE_ADDRESS",
    oracle: "0xORACLE_ADDRESS",
    agentAddress: "0xAGENT_WALLET",
    roleId: 1, // DEFI_EXECUTE_ROLE
    maxSpendingBps: 500n, // 5% of Safe value per window
    maxSpendingUSD: 0n, // 0 = use BPS mode
    windowDuration: 86400n, // 24 hours
    allowedProtocols: ["0xAAVE_V3_POOL"],
    parserProtocols: ["0xAAVE_V3_POOL"],
    parserAddresses: ["0xAAVE_PARSER"],
    selectors: ["0x617ba037", "0x69328dec"], // supply, withdraw
    selectorTypes: [2, 3], // DEPOSIT, WITHDRAW
    priceFeedTokens: ["0xUSDC", "0xWETH"],
    priceFeedAddresses: ["0xUSDC_FEED", "0xWETH_FEED"],
  },
  deployer,
);

console.log("Module deployed at:", vault.module);
```

After deployment, enable the module on your Safe (one multisig transaction):

```typescript
// In the Safe UI: Settings > Modules > Add Module > paste vault.module
```

## 4. Execute a guarded operation

The agent can now interact with whitelisted protocols within its spending budget:

```typescript
import { encodeFunctionData } from "viem";

const agent = privateKeyToAccount("0xAGENT_PRIVATE_KEY");

// Encode an Aave V3 supply call
const calldata = encodeFunctionData({
  abi: aaveV3PoolAbi,
  functionName: "supply",
  args: [usdcAddress, 100_000000n, safeAddress, 0],
});

const result = await client.executeAsAgent(
  vault.module,
  "0xAAVE_V3_POOL",
  calldata,
  agent,
);

console.log("Transaction hash:", result.txHash);
```

## 5. Check the agent's budget

```typescript
const budget = await client.getRemainingBudget(vault.module, agent.address);

console.log("Remaining:", budget.remainingAllowance);
console.log("Used:", budget.usedPercentage, "%");
```

## Using a preset

Instead of configuring everything manually, deploy from a preset template:

```typescript
const vault = await client.createAgentVault(
  {
    safe: "0xYOUR_SAFE",
    oracle: "0xORACLE",
    agentAddress: "0xAGENT",
    presetId: 0, // DeFi Trader preset
    priceFeedTokens: ["0xUSDC", "0xWETH"],
    priceFeedAddresses: ["0xUSDC_FEED", "0xWETH_FEED"],
  },
  deployer,
);
```

Available presets:

| ID  | Name          | Protocols         | Budget  |
| --- | ------------- | ----------------- | ------- |
| 0   | DeFi Trader   | Uniswap + Aave V3 | 5%/day  |
| 1   | Yield Farmer  | Aave V3           | 10%/day |
| 2   | Payment Agent | Transfer only     | 1%/day  |

---

## Agent operation examples

The examples below show how an agent uses its roles to interact with the Safe through the module. All operations are guarded by the 12 on-chain security layers.

### Common setup

```typescript
import { MultiClawClient } from "@multiclaw/core";
import { encodeFunctionData, parseUnits, type Address, type Hex } from "viem";
import { privateKeyToAccount } from "viem/accounts";

const client = new MultiClawClient({ chain: "baseSepolia" });
const agent = privateKeyToAccount("0xAGENT_PRIVATE_KEY");

const MODULE: Address = "0xMODULE_ADDRESS";
const SAFE: Address = "0xSAFE_ADDRESS";

// Base Sepolia addresses
const USDC: Address = "0x036CbD53842c5426634e7929541eC2318f3dCF7e";
const WETH: Address = "0x4200000000000000000000000000000000000006";
const AAVE_POOL: Address = "0x07eA79F68B2B3df564D0A34F8e19D9B1e339814b";
const UNISWAP_ROUTER: Address = "0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4";
```

### Swap tokens on Uniswap V3

Requires `DEFI_EXECUTE_ROLE`. Costs spending budget for the input tokens. Output tokens are automatically marked as **acquired** (free to re-use).

```typescript
const swapData = encodeFunctionData({
  abi: [
    {
      name: "exactInputSingle",
      type: "function",
      inputs: [
        {
          name: "params",
          type: "tuple",
          components: [
            { name: "tokenIn", type: "address" },
            { name: "tokenOut", type: "address" },
            { name: "fee", type: "uint24" },
            { name: "recipient", type: "address" },
            { name: "amountIn", type: "uint256" },
            { name: "amountOutMinimum", type: "uint256" },
            { name: "sqrtPriceLimitX96", type: "uint160" },
          ],
        },
      ],
    },
  ],
  functionName: "exactInputSingle",
  args: [
    {
      tokenIn: USDC,
      tokenOut: WETH,
      fee: 500, // 0.05% pool
      recipient: SAFE, // output MUST go to the Safe
      amountIn: parseUnits("100", 6), // 100 USDC
      amountOutMinimum: 0n,
      sqrtPriceLimitX96: 0n,
    },
  ],
});

const { txHash } = await client.executeAsAgent(
  MODULE,
  UNISWAP_ROUTER,
  swapData,
  agent,
);
console.log("Swap tx:", txHash);
```

### Deposit into Aave V3

Requires `DEFI_EXECUTE_ROLE`. Costs spending budget (unless the tokens are acquired from a prior swap).

```typescript
const supplyData = encodeFunctionData({
  abi: [
    {
      name: "supply",
      type: "function",
      inputs: [
        { name: "asset", type: "address" },
        { name: "amount", type: "uint256" },
        { name: "onBehalfOf", type: "address" },
        { name: "referralCode", type: "uint16" },
      ],
    },
  ],
  functionName: "supply",
  args: [USDC, parseUnits("500", 6), SAFE, 0],
});

const { txHash } = await client.executeAsAgent(
  MODULE,
  AAVE_POOL,
  supplyData,
  agent,
);
console.log("Deposit tx:", txHash);
```

### Withdraw from Aave V3

Requires `DEFI_EXECUTE_ROLE`. **Free** — withdrawals don't cost spending budget.

```typescript
const withdrawData = encodeFunctionData({
  abi: [
    {
      name: "withdraw",
      type: "function",
      inputs: [
        { name: "asset", type: "address" },
        { name: "amount", type: "uint256" },
        { name: "to", type: "address" },
      ],
    },
  ],
  functionName: "withdraw",
  args: [USDC, parseUnits("500", 6), SAFE], // must withdraw to Safe
});

const { txHash } = await client.executeAsAgent(
  MODULE,
  AAVE_POOL,
  withdrawData,
  agent,
);
console.log("Withdraw tx:", txHash);
```

### Approve a protocol to spend tokens

Requires `DEFI_EXECUTE_ROLE`. The spender must be in the agent's allowlist. The approval amount is capped by the agent's remaining budget.

```typescript
const approveData = encodeFunctionData({
  abi: [
    {
      name: "approve",
      type: "function",
      inputs: [
        { name: "spender", type: "address" },
        { name: "amount", type: "uint256" },
      ],
    },
  ],
  functionName: "approve",
  args: [UNISWAP_ROUTER, parseUnits("1000", 6)], // approve 1000 USDC
});

// target is the TOKEN contract, not the protocol
const { txHash } = await client.executeAsAgent(
  MODULE,
  USDC, // target = token address
  approveData,
  agent,
);
console.log("Approve tx:", txHash);
```

### Transfer tokens from the Safe

Requires `DEFI_TRANSFER_ROLE` (separate from execute). Costs spending budget for original tokens; acquired tokens are free to transfer.

```typescript
const { txHash } = await client.transferAsAgent(
  MODULE,
  USDC,
  "0xRECIPIENT_ADDRESS",
  parseUnits("50", 6), // 50 USDC
  agent,
);
console.log("Transfer tx:", txHash);
```

### Check budget and acquired balance

No role required — these are read-only.

```typescript
// Remaining spending budget
const budget = await client.getRemainingBudget(MODULE, agent.address);
console.log(`Budget: $${Number(budget.remainingAllowance) / 1e18} remaining`);
console.log(`Used: ${budget.usedPercentage.toFixed(1)}%`);
console.log(`Safe value: $${Number(budget.safeValueUSD) / 1e18}`);

// Acquired balance for a specific token (free to use)
const acquiredWETH = await client.getAcquiredBalance(
  MODULE,
  agent.address,
  WETH,
);
console.log(`Acquired WETH: ${acquiredWETH}`);

// Full vault status
const status = await client.getVaultStatus(MODULE);
console.log(`Paused: ${status.isPaused}`);
console.log(`Execute agents: ${status.executeAgents.length}`);
console.log(`Transfer agents: ${status.transferAgents.length}`);
```

### Error handling

On-chain guardrails revert with descriptive errors. Wrap calls in try/catch:

```typescript
try {
  await client.executeAsAgent(MODULE, target, data, agent);
} catch (error) {
  const msg = error instanceof Error ? error.message : String(error);

  if (msg.includes("Unauthorized")) {
    console.error("Agent lacks the required role");
  } else if (msg.includes("ExceedsSpendingAllowance")) {
    console.error("Operation exceeds remaining budget");
  } else if (msg.includes("OracleDataStale")) {
    console.error("Oracle data is older than 60 minutes — wait for refresh");
  } else if (msg.includes("TargetNotAllowed")) {
    console.error("Protocol is not in the agent's allowlist");
  } else if (msg.includes("RecipientNotSafe")) {
    console.error("Output recipient must be the Safe address");
  } else {
    console.error("Transaction failed:", msg);
  }
}
```

### Operation cost summary

| Operation                   | Role needed |  Costs budget?  |      Output acquired?       |
| --------------------------- | ----------- | :-------------: | :-------------------------: |
| Swap (Uniswap, 1inch, etc.) | EXECUTE     |       Yes       |  Yes (on-chain, trustless)  |
| Deposit (Aave, Morpho)      | EXECUTE     |       Yes       |   Tracked for withdrawal    |
| Withdraw (Aave, Morpho)     | EXECUTE     |       No        | Conditional (oracle grants) |
| Claim rewards (Merkl, Aave) | EXECUTE     |       No        | Conditional (oracle grants) |
| Approve                     | EXECUTE     | No (but capped) |             N/A             |
| Repay debt                  | REPAY       |       No        |             N/A             |
| Transfer tokens             | TRANSFER    |       Yes       |             N/A             |

---

## Next steps

- [Understand Agent Vaults](./concepts/agent-vaults) — how vaults, roles, and ownership work
- [Guardrails deep dive](./concepts/guardrails) — the 12 security layers protecting your funds
- [SDK Reference](./sdk/client) — full `MultiClawClient` API
- [Framework Guides](./frameworks/langchain) — integrate with LangChain, Eliza, or GOAT
