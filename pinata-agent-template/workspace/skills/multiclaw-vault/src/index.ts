import { formatUnits, isAddress, type Address } from "viem";
import { loadContext } from "./client.js";
import {
  fetchHistory,
  type ProtocolExecutionRow,
  type TransferExecutedRow,
} from "./subgraph.js";

const USD_DECIMALS = 18;
const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";

/** Format an 18-decimal USD bigint for display. */
function usd(value: bigint): string {
  const n = Number(formatUnits(value, USD_DECIMALS));
  return `$${n.toLocaleString(undefined, { maximumFractionDigits: 2 })}`;
}

function emit(obj: Record<string, unknown>): void {
  console.log(JSON.stringify(obj, null, 2));
}

async function budget(): Promise<void> {
  const { client, agent, module } = loadContext();
  const b = await client.getRemainingBudget(module, agent.address);
  emit({
    agent: agent.address,
    module,
    remaining: usd(b.remainingAllowance),
    cap: usd(b.maxAllowance),
    spentThisWindow: usd(b.cumulativeSpent),
    usedPercentage: b.usedPercentage,
    windowSeconds: Number(b.windowDuration),
    oraclelessMode: b.oracleless,
    windowExpired: b.windowExpired,
  });
}

async function status(): Promise<void> {
  const { client, module } = loadContext();
  const s = await client.getVaultStatus(module);
  emit({
    module: s.module,
    safe: s.safe,
    paused: s.isPaused,
    oracle: s.oracle,
    oraclelessMode: s.oracle.toLowerCase() === ZERO_ADDRESS,
    safeValueUSD: usd(s.safeValueUSD),
    executeAgents: s.executeAgents,
    transferAgents: s.transferAgents,
  });
}

async function history(): Promise<void> {
  const subgraphUrl = process.env.SUBGRAPH_URL;
  if (!subgraphUrl) {
    throw new Error(
      "Missing required env var: SUBGRAPH_URL (your MultiClaw subgraph query URL)",
    );
  }
  const { agent } = loadContext();
  const data = await fetchHistory(subgraphUrl, agent.address);
  emit({
    executions: data.protocolExecutions.map((e: ProtocolExecutionRow) => ({
      target: e.target,
      opType: Number(e.opType),
      spendingCost: usd(BigInt(e.spendingCost)),
      block: Number(e.blockNumber),
      txHash: e.transactionHash,
    })),
    transfers: data.transferExecuteds.map((t: TransferExecutedRow) => ({
      token: t.token,
      recipient: t.recipient,
      amount: t.amount,
      spendingCost: usd(BigInt(t.spendingCost)),
      block: Number(t.blockNumber),
      txHash: t.transactionHash,
    })),
  });
}

async function acquired(args: string[]): Promise<void> {
  const [token] = args;
  if (!token || !isAddress(token)) {
    throw new Error("Usage: acquired <token>");
  }
  const { client, agent, module } = loadContext();
  const balance = await client.getAcquiredBalance(
    module,
    agent.address,
    token as Address,
  );
  emit({ token, acquired: balance.toString() });
}

async function transfer(args: string[]): Promise<void> {
  const [token, recipient, amount] = args;
  if (!token || !recipient || !amount) {
    throw new Error("Usage: transfer <token> <recipient> <amount(base units)>");
  }
  if (!isAddress(token) || !isAddress(recipient)) {
    throw new Error("token and recipient must be valid addresses");
  }

  const { client, agent, module } = loadContext();
  const { txHash, receipt } = await client.transferAsAgent(
    module,
    token as Address,
    recipient as Address,
    BigInt(amount),
    agent,
  );

  emit({
    status: receipt.status,
    txHash,
    note:
      receipt.status === "reverted"
        ? "Transfer reverted on-chain — likely a spending-cap breach or non-whitelisted recipient."
        : "Transfer confirmed within the on-chain cap.",
  });
}

async function main(): Promise<void> {
  const [cmd, ...rest] = process.argv.slice(2);
  switch (cmd) {
    case "budget":
      return budget();
    case "status":
      return status();
    case "history":
      return history();
    case "acquired":
      return acquired(rest);
    case "transfer":
      return transfer(rest);
    default:
      console.log(
        "Commands: budget | status | history | acquired <token> | transfer <token> <recipient> <amount>",
      );
  }
}

main().catch((err: unknown) => {
  const message = err instanceof Error ? err.message : String(err);
  console.error(`[multiclaw-vault] error: ${message}`);
  process.exitCode = 1;
});
