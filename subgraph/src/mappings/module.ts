import { Address, Bytes } from "@graphprotocol/graph-ts";
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
  Account,
  AcquiredBalance,
} from "../../generated/schema";
import { BigInt } from "@graphprotocol/graph-ts";

function loadOrCreateAccount(
  subAccount: Bytes,
  module: Address,
  timestamp: BigInt,
): Account {
  let account = Account.load(subAccount);
  if (account == null) {
    account = new Account(subAccount);
    account.vault = module;
    account.module = module;
    account.cumulativeSpentIndexed = BigInt.zero();
    account.totalTxs = 0;
    account.firstActivityAt = timestamp;
    account.lastActivityAt = timestamp;
    account.recipientWhitelistEnabled = false;
  }
  return account as Account;
}

export function handleProtocolExecution(event: ProtocolExecutionEvent): void {
  let entity = new ProtocolExecution(
    event.transaction.hash.concatI32(event.logIndex.toI32()),
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

  let account = loadOrCreateAccount(
    event.params.subAccount,
    event.address,
    event.block.timestamp,
  );
  account.cumulativeSpentIndexed = account.cumulativeSpentIndexed.plus(
    event.params.spendingCost,
  );
  account.totalTxs = account.totalTxs + 1;
  account.lastActivityAt = event.block.timestamp;
  account.save();
}

export function handleTransferExecuted(event: TransferExecutedEvent): void {
  let entity = new TransferExecuted(
    event.transaction.hash.concatI32(event.logIndex.toI32()),
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

  let account = loadOrCreateAccount(
    event.params.subAccount,
    event.address,
    event.block.timestamp,
  );
  account.cumulativeSpentIndexed = account.cumulativeSpentIndexed.plus(
    event.params.spendingCost,
  );
  account.totalTxs = account.totalTxs + 1;
  account.lastActivityAt = event.block.timestamp;
  account.save();
}

export function handleAcquiredBalanceUpdated(
  event: AcquiredBalanceUpdatedEvent,
): void {
  let entity = new AcquiredBalanceUpdated(
    event.transaction.hash.concatI32(event.logIndex.toI32()),
  );
  entity.subAccount = event.params.subAccount;
  entity.token = event.params.token;
  entity.newBalance = event.params.newBalance;
  entity.blockNumber = event.block.number;
  entity.blockTimestamp = event.block.timestamp;
  entity.transactionHash = event.transaction.hash;
  entity.save();

  let account = loadOrCreateAccount(
    event.params.subAccount,
    event.address,
    event.block.timestamp,
  );
  account.lastActivityAt = event.block.timestamp;
  account.save();

  let balanceId = event.params.subAccount.concat(event.params.token);
  let balance = AcquiredBalance.load(balanceId);
  if (balance == null) {
    balance = new AcquiredBalance(balanceId);
    balance.account = event.params.subAccount;
    balance.subAccount = event.params.subAccount;
    balance.token = event.params.token;
  }
  balance.amount = event.params.newBalance;
  balance.updatedAt = event.block.timestamp;
  balance.updatedAtBlock = event.block.number;
  balance.save();
}

export function handleAllowedRecipientsSet(
  event: AllowedRecipientsSetEvent,
): void {
  let entity = new AllowedRecipientsSet(
    event.transaction.hash.concatI32(event.logIndex.toI32()),
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
  event: RecipientWhitelistToggledEvent,
): void {
  let account = loadOrCreateAccount(
    event.params.subAccount,
    event.address,
    event.block.timestamp,
  );
  account.recipientWhitelistEnabled = event.params.enabled;
  account.lastActivityAt = event.block.timestamp;
  account.save();
}
