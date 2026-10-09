/**
 * Golden parity vectors for the Go port of buildSubAccountState.
 *
 * Runs seeded random scenarios, plus the fixed scenarios of src/fuzz-test.ts,
 * through the real TypeScript buildSubAccountState and records every input
 * with the state it produced. internal/spending/golden_test.go replays each
 * case through the Go implementation and requires the identical state.
 *
 * Usage (from oracle/):
 *   npx tsx scripts/golden/gen.ts [--check] [dir]
 *
 * dir defaults to internal/spending/testdata/golden. The output is fully
 * deterministic. With --check nothing is written: the script fails if the
 * files in dir differ from what it would write (`make golden-check`).
 *
 * File format: one JSON document per family, one case per line.
 *   addresses  every address spelling the cases use. Inputs refer to an
 *              address by index; lowercase spellings come first, and outputs
 *              refer to an address by the index of its lowercase spelling.
 *   input      subAccount, currentTimestamp, windowDuration,
 *              priceCache: null (no cache) or [[token, priceUSD, decimals]]
 *              events:    [[subAccount, target, opType, tokensIn, amountsIn,
 *                           tokensOut, amountsOut, spendingCost, timestamp,
 *                           blockNumber, logIndex]]
 *              transfers: [[subAccount, token, recipient, amount,
 *                           spendingCost, timestamp, blockNumber, logIndex]]
 *   want       spendingRecords:  [[amount, timestamp]]
 *              totalSpendingInWindow
 *              depositRecords:   [[subAccount, target, tokenIn, amountIn,
 *                                  remainingAmount, tokenOut (null for "0x"),
 *                                  amountOut, remainingOutputAmount,
 *                                  timestamp, originalAcquisitionTimestamp]]
 *              acquiredQueues:   [[token, [[amount, originalTimestamp]]]]
 *              acquiredBalances: [[token, amount]]
 *   throws     replaces want when the TS threw
 * Amounts are decimal strings; timestamps and block numbers are numbers.
 */
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { join, resolve } from "node:path";
import { getAddress, type Address } from "viem";
import type {
  ProtocolExecutionEvent,
  SubAccountState,
  TokenPriceCache,
  TransferExecutedEvent,
} from "../../src/spending-oracle.js";
import type { OperationType } from "../../src/abi.js";

const args = process.argv.slice(2);
const checkOnly = args.includes("--check");
const outDir = resolve(
  args.find((a) => !a.startsWith("--")) ??
    join(import.meta.dirname, "..", "..", "internal/spending/testdata/golden"),
);

// spending-oracle.ts logs at import time and buildSubAccountState logs every
// step; keep the console quiet.
console.log = () => {};

// config.ts (imported by spending-oracle.ts) loads .env from the CWD and
// validates CHAIN at import time. Neither affects buildSubAccountState, but
// the vectors must not depend on a local .env, so import from a directory
// that has none.
process.chdir(import.meta.dirname);
delete process.env.CHAIN;
const { buildSubAccountState } = await import("../../src/spending-oracle.js");

// ============ Addresses ============

const SUBS = [
  "0x1234567890123456789012345678901234567890",
  "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
  "0x9a8b7c6d5e4f9a8b7c6d5e4f9a8b7c6d5e4f9a8b",
];
const TARGETS = [
  "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "0xcccccccccccccccccccccccccccccccccccccccc",
];
const ZERO = "0x0000000000000000000000000000000000000000";
const TOKENS = [
  ZERO,
  "0x1111111111111111111111111111111111111111",
  "0x2222222222222222222222222222222222222222",
  "0x3333333333333333333333333333333333333333",
  "0x4444444444444444444444444444444444444444",
  "0x5555555555555555555555555555555555555555",
  "0xdddddddddddddddddddddddddddddddddddddddd",
  "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
  "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2",
  "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
];
const RECIPIENT = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef";

const BASES = [...SUBS, ...TARGETS, ...TOKENS, RECIPIENT];

/** Lowercase, EIP-55 and uppercase spellings of each address (deduplicated). */
const SPELLINGS = new Map<string, string[]>(
  BASES.map((a) => [
    a,
    [...new Set([a, getAddress(a), `0x${a.slice(2).toUpperCase()}`])],
  ]),
);

// Lowercase spellings first, so an address's lowercase index is also the
// index of its first spelling.
const ADDRESSES = [
  ...BASES,
  ...[...SPELLINGS.values()].flatMap((s) => s.slice(1)),
];
const INDEX = new Map(ADDRESSES.map((a, i) => [a, i]));

function ref(spelling: string): number {
  const i = INDEX.get(spelling);
  if (i === undefined) throw new Error(`unknown address ${spelling}`);
  return i;
}

/** Index of an address's lowercase spelling (how outputs refer to it). */
function canon(address: string): number {
  return ref(address.toLowerCase());
}

const at = (i: number) => ADDRESSES[i] as Address;

// ============ Case model ============

const OP = {
  UNKNOWN: 0,
  SWAP: 1,
  DEPOSIT: 2,
  WITHDRAW: 3,
  CLAIM: 4,
  APPROVE: 5,
  REPAY: 6,
} as const;

interface EventSpec {
  sub: number;
  target: number;
  op: number;
  tokensIn: number[];
  amountsIn: bigint[];
  tokensOut: number[];
  amountsOut: bigint[];
  cost: bigint;
  ts: bigint;
  block: bigint;
  logIndex: number;
}

interface TransferSpec {
  sub: number;
  token: number;
  recipient: number;
  amount: bigint;
  cost: bigint;
  ts: bigint;
  block: bigint;
  logIndex: number;
}

interface CaseSpec {
  name: string;
  sub: number;
  now: bigint;
  window: bigint;
  priceCache: [number, bigint, number][] | null;
  events: EventSpec[];
  transfers: TransferSpec[];
}

const str = (xs: bigint[]) => xs.map(String);

function safeNumber(x: bigint): number {
  if (
    x > BigInt(Number.MAX_SAFE_INTEGER) ||
    x < -BigInt(Number.MAX_SAFE_INTEGER)
  ) {
    throw new Error(`${x} does not fit a JSON number`);
  }
  return Number(x);
}

function toProtocolEvent(e: EventSpec): ProtocolExecutionEvent {
  return {
    subAccount: at(e.sub),
    target: at(e.target),
    opType: e.op as OperationType,
    tokensIn: e.tokensIn.map(at),
    amountsIn: [...e.amountsIn],
    tokensOut: e.tokensOut.map(at),
    amountsOut: [...e.amountsOut],
    spendingCost: e.cost,
    timestamp: e.ts,
    blockNumber: e.block,
    logIndex: e.logIndex,
  };
}

function toTransferEvent(t: TransferSpec): TransferExecutedEvent {
  return {
    subAccount: at(t.sub),
    token: at(t.token),
    recipient: at(t.recipient),
    amount: t.amount,
    spendingCost: t.cost,
    timestamp: t.ts,
    blockNumber: t.block,
    logIndex: t.logIndex,
  };
}

function renderState(s: SubAccountState) {
  return {
    spendingRecords: s.spendingRecords.map((r) => [
      String(r.amount),
      safeNumber(r.timestamp),
    ]),
    totalSpendingInWindow: String(s.totalSpendingInWindow),
    depositRecords: s.depositRecords.map((d) => [
      canon(d.subAccount),
      canon(d.target),
      canon(d.tokenIn),
      String(d.amountIn),
      String(d.remainingAmount),
      d.tokenOut === "0x" ? null : canon(d.tokenOut),
      String(d.amountOut),
      String(d.remainingOutputAmount),
      safeNumber(d.timestamp),
      safeNumber(d.originalAcquisitionTimestamp),
    ]),
    acquiredQueues: [...s.acquiredQueues].map(([token, queue]) => [
      canon(token),
      queue.map((e) => [String(e.amount), safeNumber(e.originalTimestamp)]),
    ]),
    acquiredBalances: [...s.acquiredBalances].map(([token, amount]) => [
      canon(token),
      String(amount),
    ]),
  };
}

/** Runs a case through the real TS implementation and serializes it. */
function runCase(c: CaseSpec): string {
  // Serialize the input first: it must record what the TS was given.
  const input = {
    subAccount: c.sub,
    currentTimestamp: safeNumber(c.now),
    windowDuration: safeNumber(c.window),
    priceCache:
      c.priceCache &&
      c.priceCache.map(([token, price, decimals]) => [
        token,
        String(price),
        decimals,
      ]),
    events: c.events.map((e) => [
      e.sub,
      e.target,
      e.op,
      e.tokensIn,
      str(e.amountsIn),
      e.tokensOut,
      str(e.amountsOut),
      String(e.cost),
      safeNumber(e.ts),
      safeNumber(e.block),
      e.logIndex,
    ]),
    transfers: c.transfers.map((t) => [
      t.sub,
      t.token,
      t.recipient,
      String(t.amount),
      String(t.cost),
      safeNumber(t.ts),
      safeNumber(t.block),
      t.logIndex,
    ]),
  };

  // buildTokenPriceCache keys the cache by lowercase address.
  const priceCache: TokenPriceCache | undefined =
    c.priceCache === null
      ? undefined
      : new Map(
          c.priceCache.map(([token, priceUSD, decimals]) => [
            at(token).toLowerCase() as Address,
            { priceUSD, decimals },
          ]),
        );

  let outcome: { want: ReturnType<typeof renderState> } | { throws: string };
  try {
    const state = buildSubAccountState(
      c.events.map(toProtocolEvent),
      c.transfers.map(toTransferEvent),
      at(c.sub),
      c.now,
      c.window,
      priceCache,
    );
    outcome = { want: renderState(state) };
  } catch (err) {
    outcome = {
      throws:
        err instanceof Error ? `${err.name}: ${err.message}` : String(err),
    };
  }
  return JSON.stringify({ name: c.name, input, ...outcome });
}

// ============ Seeded generator ============

/** mulberry32: a small deterministic 32-bit PRNG. */
class Rng {
  private s: number;

  constructor(seed: number) {
    this.s = seed >>> 0;
  }

  u32(): number {
    this.s = (this.s + 0x6d2b79f5) >>> 0;
    let t = this.s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return (t ^ (t >>> 14)) >>> 0;
  }

  /** Integer in [0, n). */
  int(n: number): number {
    return this.u32() % n;
  }

  /** True with probability p percent. */
  pct(p: number): boolean {
    return this.int(100) < p;
  }

  pick<T>(xs: readonly T[]): T {
    return xs[this.int(xs.length)];
  }

  /** Non-negative bigint of at most `bits` bits. */
  bits(bits: number): bigint {
    let v = 0n;
    for (let i = 0; i < bits; i += 32) v = (v << 32n) | BigInt(this.u32());
    return v & ((1n << BigInt(bits)) - 1n);
  }

  /** Bigint in [0, n), n > 0. */
  below(n: bigint): bigint {
    return this.bits(n.toString(2).length + 16) % n;
  }

  shuffle<T>(xs: T[]): T[] {
    for (let i = xs.length - 1; i > 0; i--) {
      const j = this.int(i + 1);
      [xs[i], xs[j]] = [xs[j], xs[i]];
    }
    return xs;
  }
}

const MAX_UINT256 = (1n << 256n) - 1n;

interface Knobs {
  cases: number;
  /** Events per case, inclusive range. */
  events: [number, number];
  /** Size of the case's token pool. */
  tokens: number;
  /** Candidate window durations; 0n draws one in [1h, ~2.4d). */
  windows: bigint[];
  /** Timestamps fall within this many windows before "now". */
  spanWindows: number;
  /** % of timestamps placed exactly on (or 1s around) an expiry boundary. */
  boundary: number;
  /** % of events sharing the previous event's timestamp (and maybe block/log index). */
  ties: number;
  /** Pass the events to buildSubAccountState out of chronological order. */
  shuffle: boolean;
  /** % of events that are transfers. */
  transfers: number;
  /** % of events belonging to another subaccount. */
  otherSubs: number;
  /** Spell addresses randomly (lowercase/EIP-55/uppercase) instead of EIP-55. */
  mixedCase: boolean;
  /** % of protocol events with several tokens on a side. */
  multi: number;
  /** % of multi-token sides that repeat a token. */
  duplicates: number;
  /** % of amounts that are 0. */
  zeros: number;
  /** Amounts up to and beyond 2^256-1. */
  huge: boolean;
  /** % of cases with a price cache. */
  prices: number;
  /** % of protocol events that are REPAY / APPROVE / UNKNOWN. */
  ignoredOps: number;
  /** Weights of the other protocol events: SWAP, DEPOSIT, WITHDRAW, CLAIM. */
  ops: [number, number, number, number];
  /** % of deposits shaped one input -> 3-4 outputs (value-weighted split). */
  fanOut: number;
  /** Shapes of a price cache: % empty, % partially priced (rest: all priced). */
  priceShapes: [number, number];
}

const DEFAULTS: Knobs = {
  cases: 100,
  events: [1, 30],
  tokens: 4,
  windows: [86400n],
  spanWindows: 3,
  boundary: 10,
  ties: 8,
  shuffle: false,
  transfers: 15,
  otherSubs: 5,
  mixedCase: false,
  multi: 15,
  duplicates: 15,
  zeros: 8,
  huge: false,
  prices: 40,
  ignoredOps: 8,
  ops: [35, 25, 25, 15],
  fanOut: 5,
  priceShapes: [33, 33],
};

const FAMILIES: { name: string; seed: number; knobs: Partial<Knobs> }[] = [
  {
    name: "random",
    seed: 1,
    knobs: {
      cases: 160,
      events: [0, 30],
      windows: [3600n, 86400n, 604800n, 0n],
    },
  },
  {
    name: "expiry",
    seed: 2,
    knobs: {
      cases: 60,
      events: [10, 60],
      tokens: 3,
      windows: [3600n, 86400n, 0n],
      boundary: 40,
      multi: 5,
      prices: 20,
    },
  },
  {
    name: "multi-token",
    seed: 3,
    knobs: { cases: 60, tokens: 5, multi: 70, duplicates: 30, zeros: 20 },
  },
  {
    name: "prices",
    seed: 4,
    knobs: { cases: 60, tokens: 5, multi: 50, prices: 100 },
  },
  {
    name: "huge",
    seed: 5,
    knobs: { cases: 30, huge: true, multi: 30, prices: 50 },
  },
  {
    name: "subaccounts",
    seed: 6,
    knobs: { cases: 30, otherSubs: 40, mixedCase: true, transfers: 25 },
  },
  {
    name: "ordering",
    seed: 7,
    knobs: { cases: 30, events: [5, 40], ties: 50, shuffle: true },
  },
  {
    // Single-input multi-output deposits with partially priced outputs:
    // the TS over-allocates the input, the last output's share goes
    // negative, and mixed-acquisition splits then divide negative values
    // (truncating toward zero, like BigInt).
    name: "allocation",
    seed: 8,
    knobs: {
      cases: 40,
      events: [5, 30],
      tokens: 5,
      ops: [40, 45, 10, 5],
      fanOut: 70,
      multi: 0,
      zeros: 4,
      transfers: 5,
      prices: 100,
      priceShapes: [0, 100],
    },
  },
];

interface DepositMemo {
  target: string;
  tokensIn: string[];
  amountsIn: bigint[];
  tokensOut: string[];
  amountsOut: bigint[];
}

function generateCase(name: string, rng: Rng, k: Knobs): CaseSpec {
  const pickedWindow = rng.pick(k.windows);
  const window =
    pickedWindow === 0n ? 3600n + BigInt(rng.int(200_000)) : pickedWindow;
  const now = 1_700_000_000n + BigInt(rng.int(100_000_000));
  const tokenPool = rng.shuffle([...TOKENS]).slice(0, k.tokens);
  const targetPool = rng
    .shuffle([...TARGETS])
    .slice(0, 1 + rng.int(TARGETS.length));

  const spell = (address: string): number =>
    ref(k.mixedCase ? rng.pick(SPELLINGS.get(address)!) : getAddress(address));

  // ---- amounts ----
  const seen: bigint[] = [];
  const related = (x: bigint): bigint =>
    rng.pick([
      x,
      x / 2n,
      x * 2n,
      x + 1n,
      x > 0n ? x - 1n : 0n,
      (x * 3n) / 4n,
      x / 3n,
    ]);
  const amount = (): bigint => {
    if (rng.pct(k.zeros)) return 0n;
    let v: bigint;
    const r = rng.int(100);
    if (seen.length > 0 && rng.pct(25)) {
      v = related(rng.pick(seen));
    } else if (k.huge && r < 60) {
      if (r < 25) v = rng.bits(256);
      else if (r < 40) v = MAX_UINT256 - BigInt(rng.int(1000));
      // Beyond uint256: impossible on-chain, but both sides must be
      // arbitrary-precision.
      else if (r < 50) v = MAX_UINT256 + 1n + rng.bits(64);
      else v = rng.bits(129 + rng.int(127));
    } else if (r < 30) {
      v = BigInt(1 + rng.int(1000));
    } else if (r < 80) {
      v =
        BigInt(1 + rng.int(1000)) * 10n ** BigInt(rng.pick([6, 8, 18])) +
        (rng.pct(30) ? BigInt(rng.int(1_000_000)) : 0n);
    } else {
      v = rng.bits(1 + rng.int(128));
    }
    if (v > 0n) {
      seen.push(v);
      if (seen.length > 64) seen.shift();
    }
    return v;
  };

  // ---- timestamps and log positions ----
  const n = k.events[0] + rng.int(k.events[1] - k.events[0] + 1);
  const span = window * BigInt(k.spanWindows);
  const times: bigint[] = [];
  for (let i = 0; i < n; i++) {
    let ts: bigint;
    if (times.length > 0 && rng.pct(k.ties)) {
      ts = times[times.length - 1];
    } else if (rng.pct(k.boundary)) {
      // Exactly one window after an earlier event (expiry measured from the
      // event) or one window before now (expiry measured from now), +-1s.
      const anchor =
        times.length > 0 && rng.pct(50)
          ? rng.pick(times) + window
          : now - window;
      ts = anchor + BigInt(rng.int(3) - 1);
    } else if (rng.pct(2)) {
      ts = now + BigInt(rng.int(600)); // block timestamp ahead of the oracle clock
    } else {
      ts = now - rng.below(span + 1n);
    }
    times.push(ts);
  }
  // Ties were drawn against the previous draw; keep duplicates adjacent.
  times.sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));

  let block = 5_000_000n + BigInt(rng.int(1_000_000));
  let logIndex = 0;
  const keys = times.map((ts, i) => {
    if (i > 0 && ts === times[i - 1]) {
      const r = rng.int(10);
      if (r < 4) {
        block += 1n + BigInt(rng.int(2));
        logIndex = rng.int(8);
      } else if (r < 9) {
        logIndex += 1 + rng.int(2);
      } // else: identical (timestamp, block, logIndex) key
    } else if (i > 0) {
      block += (ts - times[i - 1]) / 2n + BigInt(rng.int(3));
      // Rarely, a block number that disagrees with the timestamp order
      // (the timestamp must win).
      if (rng.pct(2) && block > 10n) block -= 1n + BigInt(rng.int(5));
      logIndex = rng.int(8);
    }
    return { ts, block, logIndex };
  });

  // ---- events ----
  const deposits: DepositMemo[] = [];
  const tokensFor = (count: number, from: string[] = tokenPool): string[] => {
    const xs: string[] = [];
    const shuffled = rng.shuffle([...from]);
    for (let i = 0; i < count; i++) {
      xs.push(i < shuffled.length ? shuffled[i] : rng.pick(from));
    }
    if (count > 1 && rng.pct(k.duplicates))
      xs[count - 1] = xs[rng.int(count - 1)];
    return xs;
  };
  const sideSize = (allowEmpty: boolean): number =>
    rng.pct(k.multi) ? 2 + rng.int(2) : allowEmpty && rng.pct(5) ? 0 : 1;
  const amounts = (count: number) => Array.from({ length: count }, amount);

  const events: EventSpec[] = [];
  const transfers: TransferSpec[] = [];

  for (const key of keys) {
    const owner = rng.pct(k.otherSubs) ? rng.pick(SUBS.slice(1)) : SUBS[0];

    if (rng.pct(k.transfers)) {
      transfers.push({
        sub: spell(owner),
        token: spell(rng.pick(tokenPool)),
        recipient: spell(RECIPIENT),
        amount: amount(),
        cost: rng.pct(70) ? amount() : 0n,
        ...key,
      });
      continue;
    }

    let op: number;
    if (rng.pct(k.ignoredOps)) {
      op = rng.pick([OP.UNKNOWN, OP.APPROVE, OP.REPAY]);
    } else {
      const [swap, deposit, withdraw] = k.ops;
      const r = rng.int(k.ops.reduce((a, b) => a + b));
      op =
        r < swap
          ? OP.SWAP
          : r < swap + deposit
            ? OP.DEPOSIT
            : r < swap + deposit + withdraw
              ? OP.WITHDRAW
              : OP.CLAIM;
    }

    let target = rng.pick(targetPool);
    let tokensIn: string[];
    let amountsIn: bigint[];
    let tokensOut: string[];
    let amountsOut: bigint[];
    let cost = 0n;

    switch (op) {
      case OP.SWAP: {
        tokensIn = tokensFor(sideSize(true));
        tokensOut = tokensFor(sideSize(true));
        amountsIn = amounts(tokensIn.length);
        amountsOut = amounts(tokensOut.length);
        if (rng.pct(60)) cost = amount();
        break;
      }
      case OP.DEPOSIT: {
        if (rng.pct(k.fanOut)) {
          tokensIn = tokensFor(1);
          tokensOut = tokensFor(3 + rng.int(2));
        } else {
          tokensIn = tokensFor(sideSize(true));
          tokensOut = rng.pct(15) ? [] : tokensFor(sideSize(false));
        }
        amountsIn = amounts(tokensIn.length);
        amountsOut = amounts(tokensOut.length);
        if (rng.pct(50)) cost = amount();
        if (owner === SUBS[0]) {
          deposits.push({ target, tokensIn, amountsIn, tokensOut, amountsOut });
        }
        break;
      }
      case OP.WITHDRAW: {
        if (deposits.length > 0 && rng.pct(75)) {
          // Mirror an earlier deposit: get (part of) its inputs back and
          // usually burn its receipt tokens.
          const d = rng.pick(deposits);
          if (rng.pct(90)) target = d.target;
          if (d.tokensIn.length > 0) {
            const picks = rng
              .shuffle(d.tokensIn.map((_, j) => j))
              .slice(0, 1 + rng.int(d.tokensIn.length));
            tokensOut = picks.map((j) => d.tokensIn[j]);
            amountsOut = picks.map((j) =>
              rng.pct(80) ? related(d.amountsIn[j]) : amount(),
            );
          } else {
            tokensOut = tokensFor(1);
            amountsOut = amounts(1);
          }
          if (rng.pct(70)) {
            tokensIn = [...d.tokensOut];
            amountsIn = d.amountsOut.map((a) =>
              rng.pct(80) ? related(a) : amount(),
            );
          } else {
            tokensIn = [];
            amountsIn = [];
          }
        } else {
          tokensOut = tokensFor(sideSize(true));
          tokensIn = rng.pct(50) ? tokensFor(sideSize(true)) : [];
          amountsOut = amounts(tokensOut.length);
          amountsIn = amounts(tokensIn.length);
        }
        if (rng.pct(20)) cost = amount();
        break;
      }
      case OP.CLAIM: {
        const d =
          deposits.length > 0 && rng.pct(60) ? rng.pick(deposits) : null;
        if (d) target = d.target;
        tokensOut =
          d && d.tokensIn.length > 0 && rng.pct(40)
            ? tokensFor(1 + rng.int(d.tokensIn.length), d.tokensIn)
            : tokensFor(sideSize(false));
        tokensIn = rng.pct(10) ? tokensFor(1) : [];
        amountsOut = amounts(tokensOut.length);
        amountsIn = amounts(tokensIn.length);
        if (rng.pct(10)) cost = amount();
        break;
      }
      default: {
        // REPAY / APPROVE / UNKNOWN: ignored by the TS, spending cost included.
        tokensIn = tokensFor(sideSize(true));
        tokensOut = tokensFor(sideSize(true));
        amountsIn = amounts(tokensIn.length);
        amountsOut = amounts(tokensOut.length);
        cost = amount();
      }
    }

    events.push({
      sub: spell(owner),
      target: spell(target),
      op,
      tokensIn: tokensIn.map(spell),
      amountsIn,
      tokensOut: tokensOut.map(spell),
      amountsOut,
      cost,
      ...key,
    });
  }

  if (k.shuffle) {
    rng.shuffle(events);
    rng.shuffle(transfers);
  }

  // ---- price cache ----
  let priceCache: CaseSpec["priceCache"] = null;
  if (rng.pct(k.prices)) {
    priceCache = [];
    const [empty, partial] = k.priceShapes;
    const r = rng.int(100);
    const mode = r < empty ? 0 : r < empty + partial ? 1 : 2; // 0: empty, 1: partial, 2: all priced
    for (const token of tokenPool) {
      if (mode === 0 || (mode === 1 && rng.pct(50))) continue;
      let price: bigint;
      if (rng.pct(10)) price = 0n;
      else if (k.huge && rng.pct(40)) price = 1n + rng.bits(256);
      else {
        price = rng.pick([
          BigInt(1 + rng.int(5000)) * 10n ** 16n,
          BigInt(1 + rng.int(100_000)) * 10n ** 18n,
          1n + rng.bits(64),
          1n,
        ]);
      }
      // Decimals stay <= 22: the TS computes 10 ** decimals as a float,
      // which is exact only up to 1e22 (the Go port is exact everywhere;
      // larger decimals are unit-tested there).
      priceCache.push([canon(token), price, rng.pick([0, 6, 8, 18, 22])]);
    }
  }

  return {
    name,
    sub: spell(SUBS[0]),
    now,
    window,
    priceCache,
    events,
    transfers,
  };
}

/** Per-case seed, so adding cases to a family keeps the earlier ones. */
function caseSeed(familySeed: number, index: number): number {
  return (
    (Math.imul(familySeed, 0x9e3779b1) ^ Math.imul(index + 1, 0x85ebca6b)) >>> 0
  );
}

// ============ Fixed scenarios of src/fuzz-test.ts ============

function fuzzTestScenarios(): CaseSpec[] {
  const [sub] = SUBS;
  const target = TARGETS[0];
  const [t1, t2, t3] = [TOKENS[1], TOKENS[2], TOKENS[3]];
  const base = 1_700_000_000n;
  const e18 = 10n ** 18n;

  type Leg = [token: string, amount: bigint];
  const ev = (s: {
    op: number;
    in?: Leg[];
    out?: Leg[];
    cost?: bigint;
    ts: bigint;
    block: bigint;
  }): EventSpec => ({
    sub: ref(sub),
    target: ref(target),
    op: s.op,
    tokensIn: (s.in ?? []).map(([token]) => ref(token)),
    amountsIn: (s.in ?? []).map(([, amount]) => amount),
    tokensOut: (s.out ?? []).map(([token]) => ref(token)),
    amountsOut: (s.out ?? []).map(([, amount]) => amount),
    cost: s.cost ?? 0n,
    ts: s.ts,
    block: s.block,
    logIndex: 0,
  });
  // SWAP 1 ETH (spending cost 0.1) for amountOut of `out`.
  const ethSwap = (out: string, amountOut: bigint, ts: bigint, block: bigint) =>
    ev({
      op: OP.SWAP,
      in: [[ZERO, e18]],
      out: [[out, amountOut]],
      cost: e18 / 10n,
      ts,
      block,
    });
  const scenario = (
    name: string,
    events: EventSpec[],
    transfers: TransferSpec[] = [],
    now = base + 3600n,
  ): CaseSpec => ({
    name: `fuzz-test/${name}`,
    sub: ref(sub),
    now,
    window: 86400n,
    priceCache: null,
    events,
    transfers,
  });
  const large = 100n * 10n ** 36n;

  return [
    scenario("Multi-token DEPOSIT", [
      ethSwap(t1, e18 / 2n, base, 1000000n),
      ethSwap(t2, e18 / 2n, base + 100n, 1000001n),
      ev({
        op: OP.DEPOSIT,
        in: [
          [t1, e18 / 4n],
          [t2, e18 / 4n],
        ],
        out: [[t3, e18 / 2n]],
        ts: base + 200n,
        block: 1000002n,
      }),
    ]),
    scenario("Multi-token WITHDRAW", [
      ethSwap(t1, e18 / 2n, base, 1000000n),
      ethSwap(t2, e18 / 2n, base + 100n, 1000001n),
      ev({
        op: OP.DEPOSIT,
        in: [
          [t1, e18 / 2n],
          [t2, e18 / 2n],
        ],
        out: [[t3, e18]],
        ts: base + 200n,
        block: 1000002n,
      }),
      ev({
        op: OP.WITHDRAW,
        in: [[t3, e18]],
        out: [
          [t1, e18 / 2n],
          [t2, e18 / 2n],
        ],
        ts: base + 300n,
        block: 1000003n,
      }),
    ]),
    scenario("Mixed acquired/non-acquired SWAP", [
      ethSwap(t1, e18 / 2n, base, 1000000n),
      ev({
        op: OP.SWAP,
        in: [[t1, e18]],
        out: [[t2, (e18 * 8n) / 10n]],
        ts: base + 100n,
        block: 1000001n,
      }),
    ]),
    scenario(
      "Expiry window handling",
      [
        ethSwap(t1, e18 / 2n, base, 1000000n),
        ev({
          op: OP.SWAP,
          in: [[ZERO, e18]],
          out: [[t1, (e18 * 3n) / 10n]],
          cost: e18 / 20n,
          ts: base + 100000n - 1000n,
          block: 1000001n,
        }),
      ],
      [],
      base + 100000n,
    ),
    scenario("Empty input arrays", [
      ev({
        op: OP.SWAP,
        out: [[t1, e18 / 2n]],
        cost: e18 / 10n,
        ts: base,
        block: 1000000n,
      }),
    ]),
    scenario("Zero amounts in arrays", [
      ev({
        op: OP.SWAP,
        in: [
          [t1, 0n],
          [t2, e18 / 2n],
        ],
        out: [[t2, (e18 * 4n) / 10n]],
        cost: e18 / 10n,
        ts: base,
        block: 1000000n,
      }),
    ]),
    scenario("Duplicate tokens in output", [
      ethSwap(t1, e18, base, 1000000n),
      ev({
        op: OP.DEPOSIT,
        in: [[t1, e18 / 2n]],
        out: [
          [t1, e18 / 5n],
          [t1, e18 / 5n],
        ],
        ts: base + 100n,
        block: 1000001n,
      }),
    ]),
    scenario(
      "Transfer consumes acquired balance",
      [ethSwap(t1, e18 / 2n, base, 1000000n)],
      [
        {
          sub: ref(sub),
          token: ref(t1),
          recipient: ref(RECIPIENT),
          amount: e18 / 5n,
          cost: e18 / 20n,
          ts: base + 100n,
          block: 1000001n,
          logIndex: 0,
        },
      ],
    ),
    scenario("CLAIM without matching deposit", [
      ev({ op: OP.CLAIM, out: [[t1, e18 / 10n]], ts: base, block: 1000000n }),
    ]),
    scenario("Large numbers handling", [
      ev({
        op: OP.SWAP,
        in: [[ZERO, large]],
        out: [[t1, large]],
        cost: large / 100n,
        ts: base,
        block: 1000000n,
      }),
    ]),
    scenario("Chained SWAP operations (timestamp inheritance)", [
      ethSwap(t1, e18 / 2n, base, 1000000n),
      ev({
        op: OP.SWAP,
        in: [[t1, e18 / 2n]],
        out: [[t2, (e18 * 4n) / 10n]],
        ts: base + 100n,
        block: 1000001n,
      }),
      ev({
        op: OP.SWAP,
        in: [[t2, (e18 * 4n) / 10n]],
        out: [[t3, (e18 * 3n) / 10n]],
        ts: base + 200n,
        block: 1000002n,
      }),
    ]),
  ];
}

// ============ Main ============

function renderFamily(family: string, lines: string[]): string {
  return (
    "{\n" +
    ` "generator": "scripts/golden/gen.ts",\n` +
    ` "family": ${JSON.stringify(family)},\n` +
    ` "addresses": ${JSON.stringify(ADDRESSES)},\n` +
    ` "cases": [\n  ${lines.join(",\n  ")}\n ]\n` +
    "}\n"
  );
}

const families: [string, CaseSpec[]][] = [
  ["fuzz-test", fuzzTestScenarios()],
  ...FAMILIES.map(({ name, seed, knobs }): [string, CaseSpec[]] => {
    const k = { ...DEFAULTS, ...knobs };
    return [
      name,
      Array.from({ length: k.cases }, (_, i) =>
        generateCase(
          `${name}/${String(i).padStart(3, "0")}`,
          new Rng(caseSeed(seed, i)),
          k,
        ),
      ),
    ];
  }),
];

const files = new Map<string, string>();
const summary: string[] = [];
for (const [family, cases] of families) {
  const lines = cases.map(runCase);
  files.set(`${family}.json`, renderFamily(family, lines));
  const throws = lines.filter((l) => l.includes('"throws":')).length;
  summary.push(
    `${family}: ${cases.length} cases${throws ? `, ${throws} throwing` : ""}`,
  );
}

if (checkOnly) {
  const current = (file: string): string | null => {
    try {
      return readFileSync(join(outDir, file), "utf8");
    } catch {
      return null;
    }
  };
  const problems = [
    ...[...files]
      .filter(([file, body]) => current(file) !== body)
      .map(([file]) => `${file}: differs from the TS output`),
    ...(existsSync(outDir) ? readdirSync(outDir) : [])
      .filter((file) => file.endsWith(".json") && !files.has(file))
      .map((file) => `${file}: not generated`),
  ];
  if (problems.length > 0) {
    process.stderr.write(
      `golden vectors in ${outDir} are stale (run make golden):\n  ${problems.join("\n  ")}\n`,
    );
    process.exit(1);
  }
} else {
  mkdirSync(outDir, { recursive: true });
  for (const [file, body] of files) writeFileSync(join(outDir, file), body);
  process.stdout.write(
    `golden vectors written to ${outDir}\n  ${summary.join("\n  ")}\n`,
  );
}
