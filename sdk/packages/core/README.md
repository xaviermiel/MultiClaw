# @multisub/core

TypeScript SDK for [Multisub](https://multisubs.xyz) — on-chain guardrails for AI agents managing crypto funds.

## Installation

```bash
npm install @multisub/core viem
```

## Quick Start

```typescript
import { MultisubClient } from "@multisub/core";
import { privateKeyToAccount } from "viem/accounts";

// 1. Create client
const client = new MultisubClient({ chain: "base" });

// 2. Check agent's remaining budget
const budget = await client.getRemainingBudget(moduleAddress, agentAddress);
console.log(
  `$${formatEther(budget.remainingAllowance)} remaining (${budget.usedPercentage}% used)`,
);

// 3. Execute a DeFi operation as the agent
const agent = privateKeyToAccount("0x...");
const { txHash } = await client.executeAsAgent(
  moduleAddress,
  uniswapRouter,
  swapCalldata,
  agent,
);
```

## API Reference

### Constructor

```typescript
const client = new MultisubClient({
  chain: "base", // 'base' | 'baseSepolia'
  rpcUrl: "https://...", // Optional: override default RPC
  addresses: {
    // Optional: override contract addresses
    agentVaultFactory: "0x...",
    presetRegistry: "0x...",
    moduleRegistry: "0x...",
  },
});
```

### Agent Operations

These methods are called by the AI agent's signer key.

#### `executeAsAgent(moduleAddress, target, data, account)`

Execute a DeFi protocol interaction (swap, deposit, withdraw, claim, approve).

```typescript
const { txHash, receipt } = await client.executeAsAgent(
  moduleAddress,
  "0x...uniswapRouter", // Must be whitelisted for this agent
  encodedSwapCalldata, // ABI-encoded calldata
  agentAccount, // Agent's signer
);
```

#### `executeAsAgentWithValue(moduleAddress, target, data, value, account)`

Same as above but with ETH value (for native ETH swaps/deposits).

#### `transferAsAgent(moduleAddress, token, recipient, amount, account)`

Transfer tokens from the Safe. Requires `DEFI_TRANSFER_ROLE`.

```typescript
const { txHash } = await client.transferAsAgent(
  moduleAddress,
  usdcAddress,
  recipientAddress,
  1000000n, // 1 USDC (6 decimals)
  agentAccount,
);
```

### Read Operations

These methods are read-only and don't require a signer.

#### `getRemainingBudget(moduleAddress, subAccount)`

Get the agent's spending budget status.

```typescript
const budget = await client.getRemainingBudget(moduleAddress, agentAddress);
// budget.remainingAllowance  - USD remaining (18 decimals)
// budget.maxAllowance        - Maximum USD allowance
// budget.usedPercentage      - 0-100
// budget.safeValueUSD        - Total Safe value
// budget.maxSpendingBps      - Limit in basis points (500 = 5%)
// budget.windowDuration      - Rolling window in seconds
```

#### `getAcquiredBalance(moduleAddress, subAccount, token)`

Get the agent's acquired (free-to-use) balance for a token.

```typescript
const acquired = await client.getAcquiredBalance(
  moduleAddress,
  agentAddress,
  wethAddress,
);
// Acquired tokens can be spent without deducting from the spending budget
```

#### `getVaultStatus(moduleAddress)`

Get comprehensive vault status.

```typescript
const status = await client.getVaultStatus(moduleAddress);
// status.safe              - Safe address
// status.isPaused          - Emergency pause state
// status.oracle            - Oracle address
// status.safeValueUSD      - Total USD value
// status.executeAgents     - Agents with EXECUTE role
// status.transferAgents    - Agents with TRANSFER role
```

#### `getTransactionHistory(moduleAddress, subAccount?, fromBlock?)`

Get `ProtocolExecution` events for an agent.

```typescript
const txs = await client.getTransactionHistory(moduleAddress, agentAddress);
for (const tx of txs) {
  console.log(
    `${tx.opType}: ${tx.tokensIn} -> ${tx.tokensOut}, cost: $${formatEther(tx.spendingCost)}`,
  );
}
```

#### `getTransferHistory(moduleAddress, subAccount?, fromBlock?)`

Get `TransferExecuted` events for an agent.

### Owner Operations

These methods are called by the Safe owner (module owner) for administration.

#### `revokeAgent(moduleAddress, agentAddress, account)`

Instantly revoke all roles from an agent.

```typescript
await client.revokeAgent(moduleAddress, agentAddress, safeOwnerAccount);
```

#### `pauseVault(moduleAddress, account)`

Emergency pause — blocks all agent operations.

```typescript
await client.pauseVault(moduleAddress, safeOwnerAccount);
```

#### `unpauseVault(moduleAddress, account)`

Resume operations after a pause.

### Vault Creation

#### `createAgentVault(config, account)`

Deploy a fully-configured vault via `AgentVaultFactory`. Caller must be the factory owner.

```typescript
import { type VaultConfig, DEFI_EXECUTE_ROLE } from "@multisub/core";

const config: VaultConfig = {
  safe: safeAddress,
  oracle: oracleAddress,
  agentAddress: agentEOA,
  roleId: DEFI_EXECUTE_ROLE,
  maxSpendingBps: 500n, // 5%
  windowDuration: 86400n, // 24h
  allowedProtocols: [uniswapRouter, aavePool],
  parserProtocols: [uniswapRouter, aavePool],
  parserAddresses: [uniswapParser, aaveParser],
  selectors: ["0x3593564c"], // execute(bytes,bytes[],uint256)
  selectorTypes: [1], // SWAP
  priceFeedTokens: [usdcAddress, wethAddress],
  priceFeedAddresses: [usdcFeed, ethFeed],
};

const { module, txHash } = await client.createAgentVault(
  config,
  factoryOwnerAccount,
);
```

## ABIs

Contract ABIs are exported for direct use with viem/ethers:

```typescript
import {
  DeFiInteractorModuleAbi,
  AgentVaultFactoryAbi,
  PresetRegistryAbi,
  ModuleRegistryAbi,
} from "@multisub/core/abi";
```

## Framework Adapters

- **LangChain**: `@multisub/langchain` — StructuredTool adapters
- **Eliza**: `@multisub/eliza` — Eliza plugin with DeFi actions
- **GOAT**: `@multisub/goat` — GOAT SDK plugin

See each package's documentation for integration guides.

## How Security Works

The SDK is a thin wrapper around smart contract calls. **All security is enforced on-chain:**

- The agent's key can only call `executeOnProtocol()` and `transferToken()`
- Every call passes through a 10-layer defense stack (role check, oracle freshness, target allowlist, parser validation, recipient guard, spending limit, acquired balance, approve cap, Safe execution)
- Even if the agent is fully compromised (jailbroken, prompt-injected), it cannot exceed its spending budget, interact with non-whitelisted protocols, or redirect funds to an attacker

The SDK makes it easy to use these guardrails. It does not add or bypass any security rules.
