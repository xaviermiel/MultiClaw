/**
 * Dump the TypeScript oracle's resolved default config for every supported chain.
 *
 * The Go config tests compare the Go chain presets against this output, so the
 * hand-ported token/price-feed tables can't silently drift from src/config.ts.
 *
 * Usage (from oracle/):
 *   npx tsx scripts/dump-ts-config.ts > internal/config/testdata/ts-config.json
 */
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const CHAINS = ["sepolia", "base", "base-sepolia"];

// Every variable config.ts reads; cleared so the dump reflects pure defaults.
const CONFIG_ENV_VARS = [
  "CHAIN",
  "RPC_URL",
  "RPC_FALLBACK_URLS",
  "PRIVATE_KEY",
  "MODULE_ADDRESS",
  "REGISTRY_ADDRESS",
  "SAFE_VALUE_CRON",
  "SPENDING_ORACLE_CRON",
  "POLL_INTERVAL_MS",
  "BLOCKS_TO_LOOK_BACK",
  "WINDOW_DURATION_SECONDS",
  "CONFIRMATION_BLOCKS",
  "MAX_BLOCKS_PER_QUERY",
  "MAX_HISTORICAL_BLOCKS",
  "GAS_LIMIT",
];

const configUrl = pathToFileURL(
  join(import.meta.dirname, "..", "src", "config.ts"),
).href;

// config.ts calls dotenv.config() at import time, which reads .env from the
// CWD. Run from an empty directory so a local oracle/.env can't leak in.
process.chdir(mkdtempSync(join(tmpdir(), "oracle-ts-config-")));
for (const name of CONFIG_ENV_VARS) delete process.env[name];

const dump: Record<string, unknown> = {};
for (const chain of CHAINS) {
  process.env.CHAIN = chain;
  // config.ts resolves everything at import time; a distinct URL per chain
  // forces a fresh module instance.
  const { config } = await import(`${configUrl}?chain=${chain}`);
  dump[chain] = {
    chainId: config.chain.id,
    rpcUrls: config.rpcUrls,
    safeValueCron: config.safeValueCron,
    spendingOracleCron: config.spendingOracleCron,
    pollIntervalMs: config.pollIntervalMs,
    blocksToLookBack: config.blocksToLookBack,
    confirmationBlocks: config.confirmationBlocks,
    maxBlocksPerQuery: config.maxBlocksPerQuery,
    maxHistoricalBlocks: config.maxHistoricalBlocks,
    gasLimit: config.gasLimit.toString(),
    ethPriceFeedAddress: config.ethPriceFeedAddress,
    tokens: config.tokens,
  };
}

console.log(JSON.stringify(dump, null, 2));
