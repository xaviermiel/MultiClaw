import type { Address, Account, Hex } from "viem";
import { MultisubClient, type BudgetInfo } from "@multisub/core";
import { formatEther } from "viem";

/**
 * Eliza plugin configuration.
 */
export interface MultisubElizaPluginConfig {
  client: MultisubClient;
  moduleAddress: Address;
  agentAccount: Account;
}

/**
 * Action handler type compatible with Eliza's plugin system.
 */
export interface ElizaAction {
  name: string;
  description: string;
  handler: (params: Record<string, string>) => Promise<string>;
}

/**
 * Create the Multisub plugin for Eliza.
 *
 * Registers the following actions:
 * - MULTISUB_EXECUTE: Execute a DeFi operation
 * - MULTISUB_TRANSFER: Transfer tokens from the vault
 * - MULTISUB_CHECK_BUDGET: Check remaining spending budget
 * - MULTISUB_VAULT_STATUS: Get vault status
 *
 * @example
 * ```ts
 * import { createMultisubElizaPlugin } from '@multisub/eliza'
 *
 * const plugin = createMultisubElizaPlugin({
 *   client: new MultisubClient({ chain: 'base' }),
 *   moduleAddress: '0x...',
 *   agentAccount: privateKeyToAccount('0x...'),
 * })
 *
 * // Register with Eliza
 * eliza.registerPlugin(plugin)
 * ```
 */
export function createMultisubElizaPlugin(config: MultisubElizaPluginConfig) {
  const { client, moduleAddress, agentAccount } = config;

  const actions: ElizaAction[] = [
    {
      name: "MULTISUB_EXECUTE",
      description:
        "Execute a DeFi operation (swap, deposit, withdraw) through the vault",
      handler: async (params) => {
        try {
          const { txHash } = await client.executeAsAgent(
            moduleAddress,
            params.target as Address,
            params.data as Hex,
            agentAccount,
          );
          return `Executed successfully. Tx: ${txHash}`;
        } catch (error: unknown) {
          return `Execution failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      },
    },
    {
      name: "MULTISUB_TRANSFER",
      description: "Transfer tokens from the vault to a recipient",
      handler: async (params) => {
        try {
          const { txHash } = await client.transferAsAgent(
            moduleAddress,
            params.token as Address,
            params.recipient as Address,
            BigInt(params.amount),
            agentAccount,
          );
          return `Transfer successful. Tx: ${txHash}`;
        } catch (error: unknown) {
          return `Transfer failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      },
    },
    {
      name: "MULTISUB_CHECK_BUDGET",
      description: "Check remaining spending budget for the agent",
      handler: async () => {
        try {
          const budget = await client.getRemainingBudget(
            moduleAddress,
            agentAccount.address,
          );
          return [
            `Budget: $${formatEther(budget.remainingAllowance)} / $${formatEther(budget.maxAllowance)} USD`,
            `Used: ${budget.usedPercentage.toFixed(1)}%`,
            `Safe value: $${formatEther(budget.safeValueUSD)}`,
          ].join(" | ");
        } catch (error: unknown) {
          return `Budget check failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      },
    },
    {
      name: "MULTISUB_VAULT_STATUS",
      description: "Get the current vault status (paused, oracle, agents)",
      handler: async () => {
        try {
          const status = await client.getVaultStatus(moduleAddress);
          return [
            `Vault ${moduleAddress}`,
            `Paused: ${status.isPaused}`,
            `Safe: ${status.safe}`,
            `Oracle: ${status.oracle}`,
            `Value: $${formatEther(status.safeValueUSD)}`,
            `Execute agents: ${status.executeAgents.length}`,
            `Transfer agents: ${status.transferAgents.length}`,
          ].join(" | ");
        } catch (error: unknown) {
          return `Status check failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      },
    },
  ];

  return {
    name: "multisub",
    description: "On-chain guardrails for AI agent DeFi operations",
    actions,
  };
}
