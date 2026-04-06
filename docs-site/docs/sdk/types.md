---
sidebar_position: 2
title: Types
---

# Types

TypeScript type definitions exported by `@multiclaw/core`.

## Constants

```typescript
export const DEFI_EXECUTE_ROLE = 1;
export const DEFI_TRANSFER_ROLE = 2;
```

:::note
The contracts also define `DEFI_REPAY_ROLE = 3` for debt repayment operations (no spending cost). This is not yet exported from the SDK — use the raw value `3` if needed.
:::

## Enums

### `OperationType`

```typescript
enum OperationType {
  UNKNOWN = 0,
  SWAP = 1,
  DEPOSIT = 2,
  WITHDRAW = 3,
  CLAIM = 4,
  APPROVE = 5,
  // REPAY = 6 — defined in contract, not yet in SDK enum
}
```

## Interfaces

### `VaultConfig`

Configuration for deploying a new Agent Vault.

```typescript
interface VaultConfig {
  safe: Address;
  oracle: Address;
  agentAddress: Address;
  roleId: number; // 1 = EXECUTE, 2 = TRANSFER
  maxSpendingBps: bigint; // basis points (0n for USD mode)
  maxSpendingUSD: bigint; // 18 decimals (0n for BPS mode)
  windowDuration: bigint; // seconds
  allowedProtocols: Address[];
  parserProtocols: Address[];
  parserAddresses: Address[];
  selectors: Hex[];
  selectorTypes: number[]; // OperationType values
  priceFeedTokens: Address[];
  priceFeedAddresses: Address[];
}
```

### `VaultDeployment`

Returned after a successful vault deployment.

```typescript
interface VaultDeployment {
  module: Address;
  safe: Address;
  txHash: Hash;
  receipt: TransactionReceipt;
}
```

### `BudgetInfo`

Spending budget status for an agent.

```typescript
interface BudgetInfo {
  remainingAllowance: bigint; // USD remaining, 18 decimals
  maxSpendingBps: bigint; // 0 if USD mode
  maxSpendingUSD: bigint; // 0 if BPS mode
  windowDuration: bigint; // seconds
  safeValueUSD: bigint; // current Safe value, 18 decimals
  maxAllowance: bigint; // computed max for current window
  usedPercentage: number; // 0–100
}
```

### `VaultStatus`

Aggregate vault status.

```typescript
interface VaultStatus {
  module: Address;
  safe: Address;
  isPaused: boolean;
  oracle: Address;
  safeValueUSD: bigint;
  safeValueLastUpdated: bigint;
  safeValueUpdateCount: bigint;
  executeAgents: Address[]; // agents with DEFI_EXECUTE_ROLE
  transferAgents: Address[]; // agents with DEFI_TRANSFER_ROLE
}
```

### `ProtocolExecution`

A DeFi operation event.

```typescript
interface ProtocolExecution {
  subAccount: Address;
  target: Address;
  opType: OperationType;
  tokensIn: Address[];
  amountsIn: bigint[];
  tokensOut: Address[];
  amountsOut: bigint[];
  spendingCost: bigint; // USD, 18 decimals
  blockNumber: bigint;
  txHash: Hash;
}
```

### `TransferExecution`

A token transfer event.

```typescript
interface TransferExecution {
  subAccount: Address;
  token: Address;
  recipient: Address;
  amount: bigint;
  spendingCost: bigint; // USD, 18 decimals
  blockNumber: bigint;
  txHash: Hash;
}
```

### `MultiClawClientConfig`

Constructor config for `MultiClawClient`.

```typescript
interface MultiClawClientConfig {
  chain: "base" | "baseSepolia";
  rpcUrl?: string; // custom RPC (overrides default)
  addresses?: Partial<ChainAddresses>; // custom contract addresses
}
```

### `ChainAddresses`

Contract addresses for a specific chain.

```typescript
interface ChainAddresses {
  agentVaultFactory: Address;
  presetRegistry: Address;
  moduleRegistry: Address;
}
```
