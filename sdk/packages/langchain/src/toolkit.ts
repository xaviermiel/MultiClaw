import type { Address, Account } from "viem";
import { MultiClawClient } from "@multiclaw/core";
import { MultiClawExecuteTool } from "./tools/execute";
import { MultiClawTransferTool } from "./tools/transfer";
import { MultiClawBudgetTool } from "./tools/budget";

export interface MultiClawToolConfig {
  /** The MultiClawClient instance */
  client: MultiClawClient;
  /** The DeFiInteractorModule address */
  moduleAddress: Address;
  /** The agent's signer account */
  agentAccount: Account;
}

/**
 * Create all MultiClaw LangChain tools for an agent.
 *
 * @example
 * ```ts
 * const tools = createMultiClawTools({
 *   client: new MultiClawClient({ chain: 'base' }),
 *   moduleAddress: '0x...',
 *   agentAccount: privateKeyToAccount('0x...'),
 * })
 *
 * const agent = createReactAgent({ llm, tools })
 * ```
 */
export function createMultiClawTools(config: MultiClawToolConfig) {
  return [
    new MultiClawExecuteTool(config),
    new MultiClawTransferTool(config),
    new MultiClawBudgetTool(config),
  ];
}
