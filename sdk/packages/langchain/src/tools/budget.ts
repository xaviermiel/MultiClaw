import { StructuredTool } from "@langchain/core/tools";
import { z } from "zod";
import { formatEther } from "viem";
import type { MultisubToolConfig } from "../toolkit";

/**
 * LangChain tool for checking the agent's remaining spending budget.
 * Read-only — does not require a signer.
 */
export class MultisubBudgetTool extends StructuredTool {
  name = "multisub_check_budget";
  description = `Check the remaining spending budget for this agent.
Returns the remaining USD allowance, maximum allowance, and percentage used.
Use this before executing operations to verify there is sufficient budget.`;

  schema = z.object({});

  private config: MultisubToolConfig;

  constructor(config: MultisubToolConfig) {
    super();
    this.config = config;
  }

  async _call(_input: Record<string, never>): Promise<string> {
    try {
      const budget = await this.config.client.getRemainingBudget(
        this.config.moduleAddress,
        this.config.agentAccount.address,
      );

      const remaining = formatEther(budget.remainingAllowance);
      const max = formatEther(budget.maxAllowance);
      const safeValue = formatEther(budget.safeValueUSD);

      return [
        `Spending Budget Status:`,
        `  Remaining: $${remaining} USD`,
        `  Maximum:   $${max} USD (${budget.maxSpendingBps} bps of Safe value)`,
        `  Used:      ${budget.usedPercentage.toFixed(1)}%`,
        `  Safe Value: $${safeValue} USD`,
        `  Window:    ${Number(budget.windowDuration) / 3600}h rolling`,
      ].join("\n");
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error);
      return `Failed to check budget: ${message}`;
    }
  }
}
