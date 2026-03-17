import { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import type { Address, Hex } from "viem";
import type { MultisubToolConfig } from "../toolkit";

/**
 * LangChain tool for executing DeFi protocol interactions through Multisub.
 * The agent provides a target protocol address and encoded calldata.
 * All spending limits and allowlists are enforced on-chain.
 */
export class MultisubExecuteTool extends StructuredTool {
  name = "multisub_execute";
  description = `Execute a DeFi operation (swap, deposit, withdraw, claim, approve) through the Multisub vault.
Provide the target protocol address and the encoded calldata.
The on-chain module enforces spending limits, protocol allowlists, and recipient validation.
Returns the transaction hash on success.`;

  schema = z.object({
    target: z
      .string()
      .describe("The protocol contract address to call (must be whitelisted)"),
    data: z.string().describe("The ABI-encoded calldata for the protocol call"),
  });

  private config: MultisubToolConfig;

  constructor(config: MultisubToolConfig) {
    super();
    this.config = config;
  }

  async _call(input: { target: string; data: string }): Promise<string> {
    try {
      // Check budget before executing
      const budget = await this.config.client.getRemainingBudget(
        this.config.moduleAddress,
        this.config.agentAccount.address,
      );

      if (budget.remainingAllowance === 0n) {
        return "Error: No spending budget remaining. Cannot execute.";
      }

      const { txHash } = await this.config.client.executeAsAgent(
        this.config.moduleAddress,
        input.target as Address,
        input.data as Hex,
        this.config.agentAccount,
      );

      return `Transaction executed successfully. Hash: ${txHash}`;
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error);
      return `Execution failed: ${message}`;
    }
  }
}
