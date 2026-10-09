/**
 * Run the TypeScript oracle once, the way `oracle run --once` runs the Go
 * one: one Safe value check, then one spending refresh. This is the TS side
 * of scripts/fork-diff.sh.
 *
 * The configuration comes from the environment only: no .env file is read.
 *
 * Usage (from oracle/):
 *   npx tsx scripts/ts-run-once.ts
 */
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const src = (file: string) =>
  pathToFileURL(join(import.meta.dirname, "..", "src", file)).href;

// config.ts calls dotenv.config() at import time, which reads .env from the
// CWD. Run from an empty directory so a local oracle/.env can't leak in.
process.chdir(mkdtempSync(join(tmpdir(), "oracle-ts-run-once-")));

const safeValue = await import(src("safe-value.ts"));
const spending = await import(src("spending-oracle.ts"));
await safeValue.runOnce();
await spending.runOnce();
process.exit(0);
