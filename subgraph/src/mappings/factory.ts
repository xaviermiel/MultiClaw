import { AgentVaultCreated as AgentVaultCreatedEvent } from "../../generated/AgentVaultFactory/AgentVaultFactory";
import { AgentVaultCreated, Vault } from "../../generated/schema";
import { DeFiInteractorModule } from "../../generated/templates";

export function handleAgentVaultCreated(event: AgentVaultCreatedEvent): void {
  DeFiInteractorModule.create(event.params.module);

  let vault = new Vault(event.params.module);
  vault.safe = event.params.safe;
  vault.agentAddress = event.params.agentAddress;
  vault.module = event.params.module;
  vault.presetId = event.params.presetId;
  vault.createdAt = event.block.timestamp;
  vault.createdAtBlock = event.block.number;
  vault.save();

  let entity = new AgentVaultCreated(
    event.transaction.hash.concatI32(event.logIndex.toI32()),
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
