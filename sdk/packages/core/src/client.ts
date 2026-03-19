import {
  createPublicClient,
  createWalletClient,
  http,
  getContract,
  parseEventLogs,
  type Address,
  type PublicClient,
  type WalletClient,
  type Account,
  type Transport,
  type Chain,
  type GetContractReturnType,
  type Hex,
} from "viem";
import { DeFiInteractorModuleAbi } from "./abi/DeFiInteractorModule";
import { AgentVaultFactoryAbi } from "./abi/AgentVaultFactory";
import { getChainConfig } from "./chains";
import type {
  MultiClawClientConfig,
  ChainAddresses,
  VaultConfig,
  VaultDeployment,
  BudgetInfo,
  VaultStatus,
  ProtocolExecution,
  TransferExecution,
  OperationType,
} from "./types";
import { DEFI_EXECUTE_ROLE, DEFI_TRANSFER_ROLE } from "./types";

/**
 * MultiClawClient — the primary SDK entry point.
 *
 * Provides typed methods for all MultiClaw contract interactions:
 * - Vault creation (via AgentVaultFactory)
 * - Agent operations (executeOnProtocol, transferToken)
 * - Read operations (budget, status, history)
 * - Owner operations (revoke, pause)
 *
 * @example
 * ```ts
 * import { MultiClawClient } from '@multiclaw/core'
 *
 * const client = new MultiClawClient({ chain: 'base' })
 *
 * // Read agent's remaining budget
 * const budget = await client.getRemainingBudget(moduleAddress, agentAddress)
 * console.log(`${budget.usedPercentage}% used`)
 *
 * // Execute a DeFi operation as agent
 * const receipt = await client.executeAsAgent(moduleAddress, target, calldata, agentAccount)
 * ```
 */
export class MultiClawClient {
  readonly publicClient: PublicClient<Transport, Chain>;
  readonly chain: Chain;
  readonly addresses: ChainAddresses;

  constructor(config: MultiClawClientConfig) {
    const chainConfig = getChainConfig(config.chain);
    this.chain = chainConfig.chain;

    this.addresses = {
      ...chainConfig.addresses,
      ...config.addresses,
    };

    this.publicClient = createPublicClient({
      chain: this.chain,
      transport: http(config.rpcUrl ?? chainConfig.defaultRpcUrl),
    });
  }

  // ============ Vault Creation (Owner) ============

  /**
   * Deploy a fully-configured agent vault via AgentVaultFactory.
   * Caller must be the factory owner.
   *
   * @param config - Full vault configuration
   * @param account - The factory owner's account (signer)
   * @returns The deployed module address, Safe address, tx hash, and receipt
   */
  async createAgentVault(
    config: VaultConfig,
    account: Account,
  ): Promise<VaultDeployment> {
    const walletClient = this._walletClient(account);

    const hash = await walletClient.writeContract({
      address: this.addresses.agentVaultFactory,
      abi: AgentVaultFactoryAbi,
      functionName: "deployVault",
      args: [config],
      account,
      chain: this.chain,
    });

    const receipt = await this.publicClient.waitForTransactionReceipt({ hash });

    // Extract module address from AgentVaultCreated event
    const logs = parseEventLogs({
      abi: AgentVaultFactoryAbi,
      logs: receipt.logs,
      eventName: "AgentVaultCreated",
    });

    const module = logs[0]?.args?.module as Address;
    if (!module) {
      throw new Error("AgentVaultCreated event not found in receipt");
    }

    return {
      module,
      safe: config.safe,
      txHash: hash,
      receipt,
    };
  }

  // ============ Agent Operations ============

  /**
   * Execute a DeFi protocol interaction as an agent.
   * Calls `executeOnProtocol(target, data)` on the module.
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param target - The protocol address to call (must be whitelisted)
   * @param data - The calldata to execute
   * @param account - The agent's signer account
   */
  async executeAsAgent(
    moduleAddress: Address,
    target: Address,
    data: Hex,
    account: Account,
  ): Promise<{
    txHash: `0x${string}`;
    receipt: import("viem").TransactionReceipt;
  }> {
    const walletClient = this._walletClient(account);

    const hash = await walletClient.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "executeOnProtocol",
      args: [target, data],
      account,
      chain: this.chain,
    });

    const receipt = await this.publicClient.waitForTransactionReceipt({ hash });
    return { txHash: hash, receipt };
  }

  /**
   * Execute a DeFi protocol interaction with ETH value.
   * Calls `executeOnProtocolWithValue(target, data)` on the module.
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param target - The protocol address to call
   * @param data - The calldata to execute
   * @param value - ETH value to send (in wei)
   * @param account - The agent's signer account
   */
  async executeAsAgentWithValue(
    moduleAddress: Address,
    target: Address,
    data: Hex,
    value: bigint,
    account: Account,
  ): Promise<{
    txHash: `0x${string}`;
    receipt: import("viem").TransactionReceipt;
  }> {
    const walletClient = this._walletClient(account);

    const hash = await walletClient.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "executeOnProtocolWithValue",
      args: [target, data],
      value,
      account,
      chain: this.chain,
    });

    const receipt = await this.publicClient.waitForTransactionReceipt({ hash });
    return { txHash: hash, receipt };
  }

  /**
   * Transfer tokens from the Safe as an agent.
   * Calls `transferToken(token, recipient, amount)` on the module.
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param token - Token address to transfer
   * @param recipient - Recipient address
   * @param amount - Amount to transfer (in token's smallest unit)
   * @param account - The agent's signer account
   */
  async transferAsAgent(
    moduleAddress: Address,
    token: Address,
    recipient: Address,
    amount: bigint,
    account: Account,
  ): Promise<{
    txHash: `0x${string}`;
    receipt: import("viem").TransactionReceipt;
  }> {
    const walletClient = this._walletClient(account);

    const hash = await walletClient.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "transferToken",
      args: [token, recipient, amount],
      account,
      chain: this.chain,
    });

    const receipt = await this.publicClient.waitForTransactionReceipt({ hash });
    return { txHash: hash, receipt };
  }

  // ============ Read Operations ============

  /**
   * Get the remaining spending budget for an agent.
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param subAccount - The agent's address
   * @returns Budget info including remaining allowance, limits, and usage percentage
   */
  async getRemainingBudget(
    moduleAddress: Address,
    subAccount: Address,
  ): Promise<BudgetInfo> {
    const module = this._moduleContract(moduleAddress);

    const [remainingAllowance, limits, safeValue] = await Promise.all([
      module.read.getSpendingAllowance([subAccount]),
      module.read.getSubAccountLimits([subAccount]),
      module.read.getSafeValue(),
    ]);

    const [maxSpendingBps, maxSpendingUSD, windowDuration] = limits;
    const [safeValueUSD] = safeValue;

    // Dual-mode: USD mode uses fixed amount, BPS mode calculates from Safe value
    const maxAllowance =
      maxSpendingUSD > 0n
        ? maxSpendingUSD
        : safeValueUSD > 0n
          ? (safeValueUSD * maxSpendingBps) / 10000n
          : 0n;

    const usedPercentage =
      maxAllowance > 0n
        ? Number(
            ((maxAllowance - remainingAllowance) * 10000n) / maxAllowance,
          ) / 100
        : 0;

    return {
      remainingAllowance,
      maxSpendingBps,
      maxSpendingUSD,
      windowDuration,
      safeValueUSD,
      maxAllowance,
      usedPercentage: Math.min(100, Math.max(0, usedPercentage)),
    };
  }

  /**
   * Get the acquired (free-to-use) balance for an agent on a specific token.
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param subAccount - The agent's address
   * @param token - The token address
   * @returns The acquired balance amount
   */
  async getAcquiredBalance(
    moduleAddress: Address,
    subAccount: Address,
    token: Address,
  ): Promise<bigint> {
    const module = this._moduleContract(moduleAddress);
    return module.read.getAcquiredBalance([subAccount, token]);
  }

  /**
   * Get comprehensive vault status.
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @returns Vault status including paused state, oracle, Safe value, agents
   */
  async getVaultStatus(moduleAddress: Address): Promise<VaultStatus> {
    const module = this._moduleContract(moduleAddress);

    const [safe, oracle, isPaused, safeValue, executeAgents, transferAgents] =
      await Promise.all([
        module.read.avatar(),
        module.read.authorizedOracle(),
        module.read.paused(),
        module.read.getSafeValue(),
        module.read.getSubaccountsByRole([DEFI_EXECUTE_ROLE]),
        module.read.getSubaccountsByRole([DEFI_TRANSFER_ROLE]),
      ]);

    const [safeValueUSD, safeValueLastUpdated, safeValueUpdateCount] =
      safeValue;

    return {
      module: moduleAddress,
      safe,
      isPaused,
      oracle,
      safeValueUSD,
      safeValueLastUpdated,
      safeValueUpdateCount,
      executeAgents: [...executeAgents],
      transferAgents: [...transferAgents],
    };
  }

  /**
   * Get transaction history for an agent (ProtocolExecution events).
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param subAccount - The agent's address (optional — all agents if omitted)
   * @param fromBlock - Block to start searching from (default: latest 10000 blocks)
   * @returns Array of protocol execution events
   */
  async getTransactionHistory(
    moduleAddress: Address,
    subAccount?: Address,
    fromBlock?: bigint,
  ): Promise<ProtocolExecution[]> {
    const currentBlock = await this.publicClient.getBlockNumber();
    const startBlock =
      fromBlock ?? (currentBlock > 10000n ? currentBlock - 10000n : 0n);

    const logs = await this.publicClient.getLogs({
      address: moduleAddress,
      event: {
        type: "event",
        name: "ProtocolExecution",
        inputs: [
          { type: "address", name: "subAccount", indexed: true },
          { type: "address", name: "target", indexed: true },
          { type: "uint8", name: "opType" },
          { type: "address[]", name: "tokensIn" },
          { type: "uint256[]", name: "amountsIn" },
          { type: "address[]", name: "tokensOut" },
          { type: "uint256[]", name: "amountsOut" },
          { type: "uint256", name: "spendingCost" },
        ],
      },
      args: subAccount ? { subAccount } : undefined,
      fromBlock: startBlock,
      toBlock: currentBlock,
    });

    return logs.map((log) => ({
      subAccount: log.args.subAccount!,
      target: log.args.target!,
      opType: (log.args.opType ?? 0) as OperationType,
      tokensIn: [...(log.args.tokensIn ?? [])],
      amountsIn: [...(log.args.amountsIn ?? [])],
      tokensOut: [...(log.args.tokensOut ?? [])],
      amountsOut: [...(log.args.amountsOut ?? [])],
      spendingCost: log.args.spendingCost ?? 0n,
      blockNumber: log.blockNumber ?? 0n,
      txHash: log.transactionHash!,
    }));
  }

  /**
   * Get transfer history for an agent (TransferExecuted events).
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param subAccount - The agent's address (optional)
   * @param fromBlock - Block to start from
   */
  async getTransferHistory(
    moduleAddress: Address,
    subAccount?: Address,
    fromBlock?: bigint,
  ): Promise<TransferExecution[]> {
    const currentBlock = await this.publicClient.getBlockNumber();
    const startBlock =
      fromBlock ?? (currentBlock > 10000n ? currentBlock - 10000n : 0n);

    const logs = await this.publicClient.getLogs({
      address: moduleAddress,
      event: {
        type: "event",
        name: "TransferExecuted",
        inputs: [
          { type: "address", name: "subAccount", indexed: true },
          { type: "address", name: "token", indexed: true },
          { type: "address", name: "recipient", indexed: true },
          { type: "uint256", name: "amount" },
          { type: "uint256", name: "spendingCost" },
        ],
      },
      args: subAccount ? { subAccount } : undefined,
      fromBlock: startBlock,
      toBlock: currentBlock,
    });

    return logs.map((log) => ({
      subAccount: log.args.subAccount!,
      token: log.args.token!,
      recipient: log.args.recipient!,
      amount: log.args.amount ?? 0n,
      spendingCost: log.args.spendingCost ?? 0n,
      blockNumber: log.blockNumber ?? 0n,
      txHash: log.transactionHash!,
    }));
  }

  // ============ Owner Operations ============

  /**
   * Revoke all roles from an agent. Caller must be the Safe (module owner).
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param agentAddress - The agent to revoke
   * @param account - The Safe owner's account
   */
  async revokeAgent(
    moduleAddress: Address,
    agentAddress: Address,
    account: Account,
  ): Promise<{
    txHash: `0x${string}`;
    receipt: import("viem").TransactionReceipt;
  }> {
    const walletClient = this._walletClient(account);

    // Revoke both roles (revoke is a no-op if the role isn't assigned)
    const hash1 = await walletClient.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "revokeRole",
      args: [agentAddress, DEFI_EXECUTE_ROLE],
      account,
      chain: this.chain,
    });
    await this.publicClient.waitForTransactionReceipt({ hash: hash1 });

    const hash2 = await walletClient.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "revokeRole",
      args: [agentAddress, DEFI_TRANSFER_ROLE],
      account,
      chain: this.chain,
    });
    const receipt = await this.publicClient.waitForTransactionReceipt({
      hash: hash2,
    });

    return { txHash: hash2, receipt };
  }

  /**
   * Pause the module. Blocks all agent operations. Caller must be the Safe (module owner).
   *
   * @param moduleAddress - The DeFiInteractorModule address
   * @param account - The Safe owner's account
   */
  async pauseVault(
    moduleAddress: Address,
    account: Account,
  ): Promise<{
    txHash: `0x${string}`;
    receipt: import("viem").TransactionReceipt;
  }> {
    const walletClient = this._walletClient(account);

    const hash = await walletClient.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "pause",
      args: [],
      account,
      chain: this.chain,
    });

    const receipt = await this.publicClient.waitForTransactionReceipt({ hash });
    return { txHash: hash, receipt };
  }

  /**
   * Unpause the module. Caller must be the Safe (module owner).
   */
  async unpauseVault(
    moduleAddress: Address,
    account: Account,
  ): Promise<{
    txHash: `0x${string}`;
    receipt: import("viem").TransactionReceipt;
  }> {
    const walletClient = this._walletClient(account);

    const hash = await walletClient.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "unpause",
      args: [],
      account,
      chain: this.chain,
    });

    const receipt = await this.publicClient.waitForTransactionReceipt({ hash });
    return { txHash: hash, receipt };
  }

  // ============ Internal ============

  private _walletClient(
    account: Account,
  ): WalletClient<Transport, Chain, Account> {
    return createWalletClient({
      account,
      chain: this.chain,
      transport: http(this.publicClient.transport.url as string | undefined),
    });
  }

  private _moduleContract(
    moduleAddress: Address,
  ): GetContractReturnType<
    typeof DeFiInteractorModuleAbi,
    typeof this.publicClient
  > {
    return getContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      client: this.publicClient,
    });
  }
}
