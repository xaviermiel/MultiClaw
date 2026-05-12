import { AgentVaultCreated as AgentVaultCreatedEvent } from "../../generated/AgentVaultFactory/AgentVaultFactory";
import { AgentVaultCreated } from "../../generated/schema";
import { DeFiInteractorModule } from "../../generated/templates";

export function handleAgentVaultCreated(event: AgentVaultCreatedEvent): void {
  // Spin up a dynamic data source for this module instance
  DeFiInteractorModule.create(event.params.module);

  let entity = new AgentVaultCreated(
    event.transaction.hash.concatI32(event.logIndex.toI32())
  );
  entity.safe = event.params.safe;
  entity.agentAddress = event.params.agentAddress;
  entity.module = event.params.module;
  entity.presetId = event.params.presetId;
  entity.blockNumber = event.block.number;
  entity.blockTimestamp = event.block.timestamp;
  entity.transactionHash = event.transaction.hash;
  entity.save();
}
