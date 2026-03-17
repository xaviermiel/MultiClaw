import { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import type { Address } from "viem";
import type { MultisubToolConfig } from "../toolkit";

/**
 * LangChain tool for transferring tokens from the Safe via Multisub.
 * Requires the agent to have DEFI_TRANSFER_ROLE.
 */
export class MultisubTransferTool extends StructuredTool {
  name = "multisub_transfer";
  description = `Transfer tokens from the Multisub vault to a recipient.
Requires DEFI_TRANSFER_ROLE. Spending limits are enforced for non-acquired tokens.
Provide the token address, recipient address, and amount (in token's smallest unit).`;

  schema = z.object({
    token: z.string().describe("The ERC20 token address to transfer"),
    recipient: z.string().describe("The recipient address"),
    amount: z
      .string()
      .describe(
        "The amount to transfer (in smallest unit, e.g., wei for ETH, 1e6 for USDC)",
      ),
  });

  private config: MultisubToolConfig;

  constructor(config: MultisubToolConfig) {
    super();
    this.config = config;
  }

  async _call(input: {
    token: string;
    recipient: string;
    amount: string;
  }): Promise<string> {
    try {
      const { txHash } = await this.config.client.transferAsAgent(
        this.config.moduleAddress,
        input.token as Address,
        input.recipient as Address,
        BigInt(input.amount),
        this.config.agentAccount,
      );

      return `Transfer executed successfully. Hash: ${txHash}`;
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error);
      return `Transfer failed: ${message}`;
    }
  }
}
