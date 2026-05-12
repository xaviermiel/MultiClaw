import { Bytes } from "@graphprotocol/graph-ts";
import {
  ProtocolExecution as ProtocolExecutionEvent,
  TransferExecuted as TransferExecutedEvent,
  AcquiredBalanceUpdated as AcquiredBalanceUpdatedEvent,
  AllowedRecipientsSet as AllowedRecipientsSetEvent,
  RecipientWhitelistToggled as RecipientWhitelistToggledEvent,
} from "../../generated/templates/DeFiInteractorModule/DeFiInteractorModule";
import {
  ProtocolExecution,
  TransferExecuted,
  AcquiredBalanceUpdated,
  AllowedRecipientsSet,
  RecipientWhitelistToggled,
} from "../../generated/schema";

export function handleProtocolExecution(event: ProtocolExecutionEvent): void {
  let entity = new ProtocolExecution(
    event.transaction.hash.concatI32(event.logIndex.toI32())
  );
  entity.subAccount = event.params.subAccount;
  entity.target = event.params.target;
  entity.opType = event.params.opType;
  entity.tokensIn = event.params.tokensIn.map<Bytes>((a) => a as Bytes);
  entity.amountsIn = event.params.amountsIn;
  entity.tokensOut = event.params.tokensOut.map<Bytes>((a) => a as Bytes);
  entity.amountsOut = event.params.amountsOut;
  entity.spendingCost = event.params.spendingCost;
  entity.blockNumber = event.block.number;
  entity.blockTimestamp = event.block.timestamp;
  entity.transactionHash = event.transaction.hash;
  entity.save();
}

export function handleTransferExecuted(event: TransferExecutedEvent): void {
  let entity = new TransferExecuted(
    event.transaction.hash.concatI32(event.logIndex.toI32())
  );
  entity.subAccount = event.params.subAccount;
  entity.token = event.params.token;
  entity.recipient = event.params.recipient;
  entity.amount = event.params.amount;
  entity.spendingCost = event.params.spendingCost;
  entity.blockNumber = event.block.number;
  entity.blockTimestamp = event.block.timestamp;
  entity.transactionHash = event.transaction.hash;
  entity.save();
}

export function handleAcquiredBalanceUpdated(
  event: AcquiredBalanceUpdatedEvent
): void {
  let entity = new AcquiredBalanceUpdated(
    event.transaction.hash.concatI32(event.logIndex.toI32())
  );
  entity.subAccount = event.params.subAccount;
  entity.token = event.params.token;
  entity.newBalance = event.params.newBalance;
  entity.blockNumber = event.block.number;
  entity.blockTimestamp = event.block.timestamp;
  entity.transactionHash = event.transaction.hash;
  entity.save();
}

export function handleAllowedRecipientsSet(
  event: AllowedRecipientsSetEvent
): void {
  let entity = new AllowedRecipientsSet(
    event.transaction.hash.concatI32(event.logIndex.toI32())
  );
  entity.subAccount = event.params.subAccount;
  entity.recipients = event.params.recipients.map<Bytes>((a) => a as Bytes);
  entity.allowed = event.params.allowed;
  entity.blockNumber = event.block.number;
  entity.blockTimestamp = event.block.timestamp;
  entity.transactionHash = event.transaction.hash;
  entity.save();
}

export function handleRecipientWhitelistToggled(
  event: RecipientWhitelistToggledEvent
): void {
  let entity = new RecipientWhitelistToggled(
    event.transaction.hash.concatI32(event.logIndex.toI32())
  );
  entity.subAccount = event.params.subAccount;
  entity.enabled = event.params.enabled;
  entity.blockNumber = event.block.number;
  entity.blockTimestamp = event.block.timestamp;
  entity.transactionHash = event.transaction.hash;
  entity.save();
}
