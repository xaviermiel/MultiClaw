---
sidebar_position: 1
title: MultiClawClient
---

# MultiClawClient

The main entry point for the `@multiclaw/core` SDK. Provides methods for deploying vaults, executing operations, and reading on-chain state.

## Installation

```bash
npm install @multiclaw/core viem
```

## Constructor

```typescript
import { MultiClawClient } from "@multiclaw/core";

const client = new MultiClawClient({
  chain: "baseSepolia", // "base" | "baseSepolia"
  rpcUrl?: string, // optional RPC override
  addresses?: ChainAddresses, // optional address override
});
```

| Parameter   | Type                      | Description                                          |
| ----------- | ------------------------- | ---------------------------------------------------- |
| `chain`     | `"base" \| "baseSepolia"` | Target chain                                         |
| `rpcUrl`    | `string`                  | Custom RPC URL (overrides default)                   |
| `addresses` | `ChainAddresses`          | Custom contract addresses (overrides chain defaults) |

---

## Vault deployment

### `createAgentVault(config, account)`

Deploy a fully configured Agent Vault in a single transaction.

```typescript
const vault = await client.createAgentVault(
  {
    safe: "0x...",
    oracle: "0x...",
    agentAddress: "0x...",
    roleId: 1,
    maxSpendingBps: 500,
    maxSpendingUSD: 0n,
    windowDuration: 86400,
    allowedProtocols: ["0x..."],
    parserProtocols: ["0x..."],
    parserAddresses: ["0x..."],
    selectors: ["0x617ba037"],
    selectorTypes: [2],
    priceFeedTokens: ["0x..."],
    priceFeedAddresses: ["0x..."],
  },
  account,
);
```

**Returns:** `VaultDeployment`

```typescript
{
  module: Address;
  safe: Address;
  txHash: Hash;
  receipt: TransactionReceipt;
}
```

---

## Agent operations

### `executeAsAgent(moduleAddress, target, data, account)`

Execute a DeFi operation through the module. Requires `DEFI_EXECUTE_ROLE`.

```typescript
const result = await client.executeAsAgent(
  "0xMODULE",
  "0xAAVE_POOL", // must be whitelisted
  encodedCalldata, // ABI-encoded function call
  agentAccount,
);
```

| Parameter       | Type      | Description                                      |
| --------------- | --------- | ------------------------------------------------ |
| `moduleAddress` | `Address` | The deployed module                              |
| `target`        | `Address` | Protocol contract (must be in agent's allowlist) |
| `data`          | `Hex`     | ABI-encoded calldata                             |
| `account`       | `Account` | Agent signer (must have DEFI_EXECUTE_ROLE)       |

**Returns:** `{ txHash: Hash, receipt: TransactionReceipt }`

### `executeAsAgentWithValue(moduleAddress, target, data, value, account)`

Same as `executeAsAgent` but sends ETH value with the call.

```typescript
const result = await client.executeAsAgentWithValue(
  "0xMODULE",
  "0xWETH",
  encodedCalldata,
  parseEther("0.1"), // 0.1 ETH
  agentAccount,
);
```

### `transferAsAgent(moduleAddress, token, recipient, amount, account)`

Transfer tokens from the Safe. Requires `DEFI_TRANSFER_ROLE`.

```typescript
const result = await client.transferAsAgent(
  "0xMODULE",
  "0xUSDC",
  "0xRECIPIENT",
  1000_000000n, // 1000 USDC (6 decimals)
  agentAccount,
);
```

To send **native ETH**, pass the zero address as `token` and the amount in wei:

```typescript
import { zeroAddress, parseEther } from "viem";

await client.transferAsAgent(
  "0xMODULE",
  zeroAddress, // native ETH
  "0xRECIPIENT",
  parseEther("0.5"),
  agentAccount,
);
```

---

## Read operations

### `getRemainingBudget(moduleAddress, subAccount)`

Get the agent's current spending budget status.

```typescript
const budget = await client.getRemainingBudget("0xMODULE", "0xAGENT");
```

**Returns:** [`BudgetInfo`](./types#budgetinfo)

```typescript
{
  remainingAllowance: bigint, // USD remaining (18 decimals)
  maxSpendingBps: bigint, // 0 if USD mode
  maxSpendingUSD: bigint, // 0 if BPS mode
  windowDuration: bigint,
  safeValueUSD: bigint,
  maxAllowance: bigint, // computed budget for current window
  usedPercentage: number, // 0–100
}
```

### `getAcquiredBalance(moduleAddress, subAccount, token)`

Get the free-to-use balance for a specific token.

```typescript
const acquired = await client.getAcquiredBalance(
  "0xMODULE",
  "0xAGENT",
  "0xWETH",
);
// Returns: bigint (token smallest unit)
```

### `getVaultStatus(moduleAddress)`

Get aggregate vault information.

```typescript
const status = await client.getVaultStatus("0xMODULE");
```

**Returns:** [`VaultStatus`](./types#vaultstatus)

```typescript
{
  module: Address,
  safe: Address,
  isPaused: boolean,
  oracle: Address,
  safeValueUSD: bigint,
  safeValueLastUpdated: bigint,
  safeValueUpdateCount: bigint,
  executeAgents: Address[],
  transferAgents: Address[],
}
```

### `getTransactionHistory(moduleAddress, subAccount?, fromBlock?)`

Get `ProtocolExecution` events for a module or specific agent.

```typescript
const txs = await client.getTransactionHistory("0xMODULE", "0xAGENT");
```

**Returns:** [`ProtocolExecution[]`](./types#protocolexecution)

### `getTransferHistory(moduleAddress, subAccount?, fromBlock?)`

Get `TransferExecuted` events for a module or specific agent.

```typescript
const transfers = await client.getTransferHistory("0xMODULE");
```

**Returns:** [`TransferExecution[]`](./types#transferexecution)

---

## Owner operations

### `revokeAgent(moduleAddress, agentAddress, account)`

Revoke all roles from an agent. Must be called by the Safe owner.

```typescript
await client.revokeAgent("0xMODULE", "0xAGENT", ownerAccount);
```

### `pauseVault(moduleAddress, account)`

Emergency pause — blocks all agent operations immediately.

```typescript
await client.pauseVault("0xMODULE", ownerAccount);
```

### `unpauseVault(moduleAddress, account)`

Resume operations after a pause.

```typescript
await client.unpauseVault("0xMODULE", ownerAccount);
```
