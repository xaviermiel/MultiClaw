import type { Address, Account, Hex } from "viem";
import { MultiClawClient } from "@multiclaw/core";
import { formatEther } from "viem";

/**
 * GOAT SDK plugin configuration.
 */
export interface MultiClawGoatPluginConfig {
  client: MultiClawClient;
  moduleAddress: Address;
  agentAccount: Account;
}

/**
 * GOAT tool definition compatible with GOAT SDK's tool interface.
 */
export interface GoatTool {
  name: string;
  description: string;
  parameters: Record<
    string,
    { type: string; description: string; required?: boolean }
  >;
  handler: (params: Record<string, string>) => Promise<string>;
}

/**
 * Create the MultiClaw plugin for GOAT SDK.
 *
 * @example
 * ```ts
 * import { createMultiClawGoatPlugin } from '@multiclaw/goat'
 *
 * const plugin = createMultiClawGoatPlugin({
 *   client: new MultiClawClient({ chain: 'base' }),
 *   moduleAddress: '0x...',
 *   agentAccount: privateKeyToAccount('0x...'),
 * })
 *
 * // Register tools with GOAT
 * goat.addTools(plugin.tools)
 * ```
 */
export function createMultiClawGoatPlugin(config: MultiClawGoatPluginConfig) {
  const { client, moduleAddress, agentAccount } = config;

  const tools: GoatTool[] = [
    {
      name: "multiclaw_execute",
      description:
        "Execute a DeFi operation through the MultiClaw-protected vault",
      parameters: {
        target: {
          type: "string",
          description: "Protocol contract address (must be whitelisted)",
          required: true,
        },
        data: {
          type: "string",
          description: "ABI-encoded calldata",
          required: true,
        },
      },
      handler: async (params) => {
        try {
          const { txHash } = await client.executeAsAgent(
            moduleAddress,
            params.target as Address,
            params.data as Hex,
            agentAccount,
          );
          return `Executed. Tx: ${txHash}`;
        } catch (error: unknown) {
          return `Failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      },
    },
    {
      name: "multiclaw_transfer",
      description: "Transfer tokens from the vault",
      parameters: {
        token: {
          type: "string",
          description: "ERC20 token address",
          required: true,
        },
        recipient: {
          type: "string",
          description: "Recipient address",
          required: true,
        },
        amount: {
          type: "string",
          description: "Amount in smallest unit",
          required: true,
        },
      },
      handler: async (params) => {
        try {
          const { txHash } = await client.transferAsAgent(
            moduleAddress,
            params.token as Address,
            params.recipient as Address,
            BigInt(params.amount),
            agentAccount,
          );
          return `Transferred. Tx: ${txHash}`;
        } catch (error: unknown) {
          return `Failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      },
    },
    {
      name: "multiclaw_check_budget",
      description: "Check remaining spending budget",
      parameters: {},
      handler: async () => {
        try {
          const budget = await client.getRemainingBudget(
            moduleAddress,
            agentAccount.address,
          );
          return `$${formatEther(budget.remainingAllowance)} / $${formatEther(budget.maxAllowance)} (${budget.usedPercentage.toFixed(1)}% used)`;
        } catch (error: unknown) {
          return `Failed: ${error instanceof Error ? error.message : String(error)}`;
        }
      },
    },
  ];

  return {
    name: "multiclaw",
    description: "On-chain guardrails for AI agent DeFi operations",
    tools,
  };
}
