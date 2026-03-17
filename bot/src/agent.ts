import { MultisubClient } from "@multisub/core";
import { privateKeyToAccount } from "viem/accounts";
import { formatEther, type Address, type Hex } from "viem";

// Initialize the Multisub client and agent account
const chain = (process.env.CHAIN as "base" | "baseSepolia") || "base";
const moduleAddress = process.env.MODULE_ADDRESS as Address;

if (!process.env.AGENT_PRIVATE_KEY) {
  throw new Error("AGENT_PRIVATE_KEY env var required");
}
if (!moduleAddress) {
  throw new Error("MODULE_ADDRESS env var required");
}

export const agentAccount = privateKeyToAccount(
  process.env.AGENT_PRIVATE_KEY as `0x${string}`,
);

export const multisubClient = new MultisubClient({
  chain,
  rpcUrl: process.env.RPC_URL,
});

/**
 * Execute a DeFi operation through Multisub guardrails.
 * Returns a human-readable result string.
 */
export async function executeOperation(
  target: string,
  calldata: string,
): Promise<string> {
  try {
    const budget = await multisubClient.getRemainingBudget(
      moduleAddress,
      agentAccount.address,
    );

    if (budget.remainingAllowance === 0n) {
      return "Cannot execute: no spending budget remaining.";
    }

    const { txHash } = await multisubClient.executeAsAgent(
      moduleAddress,
      target as Address,
      calldata as Hex,
      agentAccount,
    );

    return `Executed successfully. Transaction: ${txHash}`;
  } catch (error: unknown) {
    const message = error instanceof Error ? error.message : String(error);
    return `Execution failed: ${message}`;
  }
}

/**
 * Get the agent's current budget status as a readable string.
 */
export async function getBudgetStatus(): Promise<string> {
  try {
    const budget = await multisubClient.getRemainingBudget(
      moduleAddress,
      agentAccount.address,
    );
    return [
      `Remaining budget: $${formatEther(budget.remainingAllowance)} USD`,
      `Maximum: $${formatEther(budget.maxAllowance)} USD`,
      `Used: ${budget.usedPercentage.toFixed(1)}%`,
      `Safe value: $${formatEther(budget.safeValueUSD)} USD`,
    ].join("\n");
  } catch (error: unknown) {
    return `Could not fetch budget: ${error instanceof Error ? error.message : String(error)}`;
  }
}

/**
 * Get vault status for the stats endpoint.
 */
export async function getVaultStats() {
  try {
    const status = await multisubClient.getVaultStatus(moduleAddress);
    return {
      balance: `$${(Number(status.safeValueUSD) / 1e18).toLocaleString()} USD`,
      safeAddress: status.safe,
      isPaused: status.isPaused,
      agentCount: status.executeAgents.length + status.transferAgents.length,
    };
  } catch {
    return {
      balance: "Unknown",
      safeAddress: "0x...",
      isPaused: false,
      agentCount: 0,
    };
  }
}
