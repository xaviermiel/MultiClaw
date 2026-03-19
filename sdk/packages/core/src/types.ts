import type { Address, Hash, TransactionReceipt } from "viem";

// ============ Operation Types (mirrors Solidity enum) ============

export enum OperationType {
  UNKNOWN = 0,
  SWAP = 1,
  DEPOSIT = 2,
  WITHDRAW = 3,
  CLAIM = 4,
  APPROVE = 5,
}

// ============ Role Constants ============

export const DEFI_EXECUTE_ROLE = 1 as const;
export const DEFI_TRANSFER_ROLE = 2 as const;

// ============ Vault Creation ============

export interface VaultConfig {
  /** The Safe address that will own the vault */
  safe: Address;
  /** The oracle address authorized to update spending state */
  oracle: Address;
  /** The AI agent's EOA address */
  agentAddress: Address;
  /** Role to grant: 1=EXECUTE, 2=TRANSFER */
  roleId: number;
  /** Spending limit in basis points (e.g., 500 = 5%). 0 if using USD mode */
  maxSpendingBps: bigint;
  /** Spending limit in USD, 18 decimals (e.g., 1000e18). 0 if using BPS mode */
  maxSpendingUSD: bigint;
  /** Rolling window duration in seconds (e.g., 86400 = 24h) */
  windowDuration: bigint;
  /** Protocol addresses the agent can interact with */
  allowedProtocols: Address[];
  /** Protocol addresses for parser registration */
  parserProtocols: Address[];
  /** Parser contract addresses (parallel to parserProtocols) */
  parserAddresses: Address[];
  /** Function selectors to register */
  selectors: `0x${string}`[];
  /** OperationType for each selector (parallel to selectors) */
  selectorTypes: number[];
  /** Token addresses for Chainlink price feeds */
  priceFeedTokens: Address[];
  /** Chainlink price feed addresses (parallel to priceFeedTokens) */
  priceFeedAddresses: Address[];
}

export interface VaultDeployment {
  /** The deployed module address */
  module: Address;
  /** The Safe address */
  safe: Address;
  /** Transaction hash of the deployment */
  txHash: Hash;
  /** Transaction receipt */
  receipt: TransactionReceipt;
}

// ============ Vault Status ============

export interface BudgetInfo {
  /** Remaining spending allowance in USD (18 decimals) */
  remainingAllowance: bigint;
  /** Max spending in basis points (0 if USD mode) */
  maxSpendingBps: bigint;
  /** Max spending in USD, 18 decimals (0 if BPS mode) */
  maxSpendingUSD: bigint;
  /** Window duration in seconds */
  windowDuration: bigint;
  /** Total Safe value in USD (18 decimals) */
  safeValueUSD: bigint;
  /** Computed max allowance for current window */
  maxAllowance: bigint;
  /** Percentage of budget used (0-100) */
  usedPercentage: number;
}

export interface VaultStatus {
  /** Module address */
  module: Address;
  /** Safe (avatar) address */
  safe: Address;
  /** Whether the module is paused */
  isPaused: boolean;
  /** Authorized oracle address */
  oracle: Address;
  /** Safe total value in USD (18 decimals) */
  safeValueUSD: bigint;
  /** Timestamp of last Safe value update */
  safeValueLastUpdated: bigint;
  /** Number of Safe value updates */
  safeValueUpdateCount: bigint;
  /** List of sub-accounts with EXECUTE role */
  executeAgents: Address[];
  /** List of sub-accounts with TRANSFER role */
  transferAgents: Address[];
}

// ============ Transaction History ============

export interface ProtocolExecution {
  /** Sub-account (agent) that executed */
  subAccount: Address;
  /** Protocol address called */
  target: Address;
  /** Operation type */
  opType: OperationType;
  /** Input token addresses */
  tokensIn: Address[];
  /** Input amounts */
  amountsIn: bigint[];
  /** Output token addresses */
  tokensOut: Address[];
  /** Output amounts */
  amountsOut: bigint[];
  /** USD spending cost (18 decimals) */
  spendingCost: bigint;
  /** Block number */
  blockNumber: bigint;
  /** Transaction hash */
  txHash: Hash;
}

export interface TransferExecution {
  /** Sub-account (agent) that transferred */
  subAccount: Address;
  /** Token transferred */
  token: Address;
  /** Recipient address */
  recipient: Address;
  /** Amount transferred */
  amount: bigint;
  /** USD spending cost (18 decimals) */
  spendingCost: bigint;
  /** Block number */
  blockNumber: bigint;
  /** Transaction hash */
  txHash: Hash;
}

// ============ Client Config ============

export interface MultiClawClientConfig {
  /** Chain name: 'base' | 'baseSepolia' */
  chain: "base" | "baseSepolia";
  /** Custom RPC URL (overrides default for chain) */
  rpcUrl?: string;
  /** Custom contract addresses (overrides defaults for chain) */
  addresses?: Partial<ChainAddresses>;
}

export interface ChainAddresses {
  agentVaultFactory: Address;
  presetRegistry: Address;
  moduleRegistry: Address;
}
