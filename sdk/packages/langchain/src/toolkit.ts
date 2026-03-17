import type { Address, Account } from "viem";
import { MultisubClient } from "@multisub/core";
import { MultisubExecuteTool } from "./tools/execute";
import { MultisubTransferTool } from "./tools/transfer";
import { MultisubBudgetTool } from "./tools/budget";

export interface MultisubToolConfig {
  /** The MultisubClient instance */
  client: MultisubClient;
  /** The DeFiInteractorModule address */
  moduleAddress: Address;
  /** The agent's signer account */
  agentAccount: Account;
}

/**
 * Create all Multisub LangChain tools for an agent.
 *
 * @example
 * ```ts
 * const tools = createMultisubTools({
 *   client: new MultisubClient({ chain: 'base' }),
 *   moduleAddress: '0x...',
 *   agentAccount: privateKeyToAccount('0x...'),
 * })
 *
 * const agent = createReactAgent({ llm, tools })
 * ```
 */
export function createMultisubTools(config: MultisubToolConfig) {
  return [
    new MultisubExecuteTool(config),
    new MultisubTransferTool(config),
    new MultisubBudgetTool(config),
  ];
}
