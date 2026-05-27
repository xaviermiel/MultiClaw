import type { Address } from "viem";

/**
 * Subgraph-backed history. Reads ProtocolExecution / TransferExecuted entities
 * for a given agent (subAccount) from the MultiClaw subgraph, ordered newest
 * first. The Graph stores addresses as lowercase Bytes, so the `subAccount`
 * filter must be lowercased.
 */

const HISTORY_QUERY = `query History($agent: Bytes!, $first: Int!) {
  protocolExecutions(
    where: { subAccount: $agent }
    orderBy: blockNumber
    orderDirection: desc
    first: $first
  ) {
    target
    opType
    spendingCost
    blockNumber
    transactionHash
  }
  transferExecuteds(
    where: { subAccount: $agent }
    orderBy: blockNumber
    orderDirection: desc
    first: $first
  ) {
    token
    recipient
    amount
    spendingCost
    blockNumber
    transactionHash
  }
}`;

export interface ProtocolExecutionRow {
  target: string;
  opType: number;
  spendingCost: string;
  blockNumber: string;
  transactionHash: string;
}

export interface TransferExecutedRow {
  token: string;
  recipient: string;
  amount: string;
  spendingCost: string;
  blockNumber: string;
  transactionHash: string;
}

export interface HistoryResult {
  protocolExecutions: ProtocolExecutionRow[];
  transferExecuteds: TransferExecutedRow[];
}

interface GraphQLResponse {
  data?: HistoryResult;
  errors?: { message: string }[];
}

export async function fetchHistory(
  subgraphUrl: string,
  agent: Address,
  first = 50,
): Promise<HistoryResult> {
  const res = await fetch(subgraphUrl, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      query: HISTORY_QUERY,
      variables: { agent: agent.toLowerCase(), first },
    }),
  });

  if (!res.ok) {
    throw new Error(`Subgraph HTTP ${res.status}: ${await res.text()}`);
  }

  const json = (await res.json()) as GraphQLResponse;
  if (json.errors && json.errors.length > 0) {
    throw new Error(
      `Subgraph query error: ${json.errors.map((e) => e.message).join("; ")}`,
    );
  }
  if (!json.data) {
    throw new Error("Subgraph returned no data");
  }
  return json.data;
}
