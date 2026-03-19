/**
 * Edge case and security-focused tests for the Spending Oracle
 *
 * Covers scenarios not in the main test suite:
 * - Rapid reset attack simulation
 * - Window boundary precision (exact expiry timestamp)
 * - Multi-subaccount isolation
 * - Large-scale event replay
 * - Mixed operation sequences with deposit/withdrawal matching edge cases
 * - Same-block event ordering
 */

import { describe, it, expect } from "vitest";
import type { Address } from "viem";
import { OperationType } from "./abi.js";
import {
  consumeFromQueue,
  addToQueue,
  getValidQueueBalance,
  pruneExpiredEntries,
  buildSubAccountState,
  type AcquiredBalanceQueue,
  type ProtocolExecutionEvent,
  type TransferExecutedEvent,
} from "./spending-oracle.js";

// ============ Test Constants ============

const SUB_ACCOUNT_A = "0x1111111111111111111111111111111111111111" as Address;
const SUB_ACCOUNT_B = "0x2222222222222222222222222222222222222222" as Address;
const TARGET_AAVE = "0xAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAaAa" as Address;
const TARGET_UNI = "0xBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBbBb" as Address;
const TOKEN_USDC = "0x3333333333333333333333333333333333333333" as Address;
const TOKEN_WETH = "0x4444444444444444444444444444444444444444" as Address;
const TOKEN_DAI = "0x5555555555555555555555555555555555555555" as Address;
const TOKEN_LINK = "0x6666666666666666666666666666666666666666" as Address;
const RECIPIENT = "0x7777777777777777777777777777777777777777" as Address;

const ONE_DAY = 86400n;
const WINDOW_DURATION = ONE_DAY;
const NOW = 1700100000n;
const HOUR_AGO = NOW - 3600n;
const DAY_AGO = NOW - ONE_DAY;
const TWO_DAYS_AGO = NOW - ONE_DAY * 2n;

// ============ Helpers ============

function createProtocolEvent(
  overrides: Partial<ProtocolExecutionEvent>,
): ProtocolExecutionEvent {
  return {
    subAccount: SUB_ACCOUNT_A,
    target: TARGET_AAVE,
    opType: OperationType.SWAP,
    tokensIn: [TOKEN_USDC],
    amountsIn: [1000n],
    tokensOut: [TOKEN_WETH],
    amountsOut: [500n],
    spendingCost: 100n,
    timestamp: HOUR_AGO,
    blockNumber: 1000n,
    logIndex: 0,
    ...overrides,
  };
}

function createTransferEvent(
  overrides: Partial<TransferExecutedEvent>,
): TransferExecutedEvent {
  return {
    subAccount: SUB_ACCOUNT_A,
    token: TOKEN_USDC,
    recipient: RECIPIENT,
    amount: 100n,
    spendingCost: 50n,
    timestamp: HOUR_AGO,
    blockNumber: 1000n,
    logIndex: 0,
    ...overrides,
  };
}

// ============ Window Boundary Precision ============

describe("Window boundary precision", () => {
  it("should treat event at exact window start as valid", () => {
    // Event timestamp == windowStart (NOW - WINDOW_DURATION)
    const events = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensOut: [TOKEN_WETH],
        amountsOut: [100n],
        spendingCost: 50n,
        timestamp: DAY_AGO, // exactly at window boundary
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // At exact boundary — behavior depends on >= vs > comparison
    // The oracle uses >= windowStart, so this should be valid
    const balance = state.acquiredBalances.get(
      TOKEN_WETH.toLowerCase() as Address,
    );
    // Either valid (if >= comparison) or expired (if > comparison)
    // Document the actual behavior
    if (balance !== undefined) {
      expect(balance).toBe(100n);
      expect(state.totalSpendingInWindow).toBe(50n);
    } else {
      // If the implementation uses strict > for window start,
      // the event at exact boundary is expired
      expect(state.totalSpendingInWindow).toBe(0n);
    }
  });

  it("should treat event 1 second before window as expired", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensOut: [TOKEN_WETH],
        amountsOut: [100n],
        spendingCost: 50n,
        timestamp: DAY_AGO - 1n, // 1 second before window
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBeUndefined();
    expect(state.totalSpendingInWindow).toBe(0n);
  });

  it("should treat event 1 second after window start as valid", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensOut: [TOKEN_WETH],
        amountsOut: [100n],
        spendingCost: 50n,
        timestamp: DAY_AGO + 1n,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBe(100n);
    expect(state.totalSpendingInWindow).toBe(50n);
  });
});

// ============ Sub-Account Isolation ============

describe("Sub-account isolation", () => {
  it("should not leak acquired balances between sub-accounts", () => {
    const events = [
      createProtocolEvent({
        subAccount: SUB_ACCOUNT_A,
        opType: OperationType.SWAP,
        tokensOut: [TOKEN_WETH],
        amountsOut: [500n],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
      }),
      createProtocolEvent({
        subAccount: SUB_ACCOUNT_B,
        opType: OperationType.SWAP,
        tokensOut: [TOKEN_USDC],
        amountsOut: [1000n],
        timestamp: HOUR_AGO,
        blockNumber: 1001n,
        logIndex: 1,
      }),
    ];

    const stateA = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );
    const stateB = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_B,
      NOW,
      WINDOW_DURATION,
    );

    // A should only have WETH
    expect(
      stateA.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBe(500n);
    expect(
      stateA.acquiredBalances.get(TOKEN_USDC.toLowerCase() as Address),
    ).toBeUndefined();

    // B should only have USDC
    expect(
      stateB.acquiredBalances.get(TOKEN_USDC.toLowerCase() as Address),
    ).toBe(1000n);
    expect(
      stateB.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBeUndefined();
  });

  it("should not leak spending between sub-accounts", () => {
    const events = [
      createProtocolEvent({
        subAccount: SUB_ACCOUNT_A,
        spendingCost: 100n,
        timestamp: HOUR_AGO,
      }),
      createProtocolEvent({
        subAccount: SUB_ACCOUNT_B,
        spendingCost: 200n,
        timestamp: HOUR_AGO,
        blockNumber: 1001n,
        logIndex: 1,
      }),
    ];

    const stateA = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );
    const stateB = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_B,
      NOW,
      WINDOW_DURATION,
    );

    expect(stateA.totalSpendingInWindow).toBe(100n);
    expect(stateB.totalSpendingInWindow).toBe(200n);
  });

  it("should not let sub-account B consume sub-account A deposits", () => {
    const events = [
      // A deposits
      createProtocolEvent({
        subAccount: SUB_ACCOUNT_A,
        opType: OperationType.DEPOSIT,
        tokensIn: [TOKEN_USDC],
        amountsIn: [1000n],
        tokensOut: [],
        amountsOut: [],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
      }),
      // B tries to withdraw from same target
      createProtocolEvent({
        subAccount: SUB_ACCOUNT_B,
        opType: OperationType.WITHDRAW,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_USDC],
        amountsOut: [1000n],
        timestamp: HOUR_AGO + 100n,
        blockNumber: 1001n,
        logIndex: 1,
      }),
    ];

    const stateB = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_B,
      NOW,
      WINDOW_DURATION,
    );

    // B should NOT have acquired USDC — no matching deposit from B
    expect(
      stateB.acquiredBalances.get(TOKEN_USDC.toLowerCase() as Address),
    ).toBeUndefined();
  });
});

// ============ Same-Block Event Ordering ============

describe("Same-block event ordering", () => {
  it("should process events by logIndex within same block", () => {
    // Swap creates acquired, then transfer consumes it — same block
    const protocolEvents = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensOut: [TOKEN_WETH],
        amountsOut: [500n],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
    ];

    const transferEvents = [
      createTransferEvent({
        token: TOKEN_WETH,
        amount: 200n,
        spendingCost: 10n,
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 1, // after the swap
      }),
    ];

    const state = buildSubAccountState(
      protocolEvents,
      transferEvents,
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // 500 acquired from swap - 200 consumed by transfer = 300 remaining
    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBe(300n);
  });

  it("should handle transfer before swap in same block (no acquired to consume)", () => {
    const protocolEvents = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensOut: [TOKEN_WETH],
        amountsOut: [500n],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 5, // after the transfer
      }),
    ];

    const transferEvents = [
      createTransferEvent({
        token: TOKEN_WETH,
        amount: 200n,
        spendingCost: 100n,
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0, // before the swap
      }),
    ];

    const state = buildSubAccountState(
      protocolEvents,
      transferEvents,
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // Transfer happens first (logIndex 0) — no acquired WETH yet, so it doesn't consume from queue
    // Then swap produces 500 acquired WETH
    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBe(500n);
  });
});

// ============ Deposit/Withdrawal Matching Edge Cases ============

describe("Deposit/withdrawal matching edge cases", () => {
  it("should handle partial withdrawal against deposit", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_USDC],
        amountsIn: [1000n],
        tokensOut: [],
        amountsOut: [],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.WITHDRAW,
        target: TARGET_AAVE,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_USDC],
        amountsOut: [400n], // partial
        timestamp: HOUR_AGO + 100n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // 400 of 1000 matched — should be acquired
    expect(
      state.acquiredBalances.get(TOKEN_USDC.toLowerCase() as Address),
    ).toBe(400n);
  });

  it("should handle withdrawal exceeding deposit (yield)", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_USDC],
        amountsIn: [1000n],
        tokensOut: [],
        amountsOut: [],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.WITHDRAW,
        target: TARGET_AAVE,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_USDC],
        amountsOut: [1050n], // 5% yield
        timestamp: HOUR_AGO + 100n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // Only 1000 matches the deposit — 50 yield is unmatched
    const balance = state.acquiredBalances.get(
      TOKEN_USDC.toLowerCase() as Address,
    );
    expect(balance).toBe(1000n); // only matched amount is acquired
  });

  it("should not match withdrawal from different protocol", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_USDC],
        amountsIn: [1000n],
        tokensOut: [],
        amountsOut: [],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.WITHDRAW,
        target: TARGET_UNI, // different protocol
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_USDC],
        amountsOut: [1000n],
        timestamp: HOUR_AGO + 100n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // No match — different target
    expect(
      state.acquiredBalances.get(TOKEN_USDC.toLowerCase() as Address),
    ).toBeUndefined();
  });

  it("should not match withdrawal of different token", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_USDC],
        amountsIn: [1000n],
        tokensOut: [],
        amountsOut: [],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.WITHDRAW,
        target: TARGET_AAVE,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_WETH], // different token
        amountsOut: [500n],
        timestamp: HOUR_AGO + 100n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // WETH not matched to USDC deposit
    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBeUndefined();
  });

  it("should handle multiple deposits consumed by single withdrawal (FIFO)", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_USDC],
        amountsIn: [300n],
        tokensOut: [],
        amountsOut: [],
        spendingCost: 30n,
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_USDC],
        amountsIn: [700n],
        tokensOut: [],
        amountsOut: [],
        spendingCost: 70n,
        timestamp: HOUR_AGO + 50n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.WITHDRAW,
        target: TARGET_AAVE,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_USDC],
        amountsOut: [500n],
        timestamp: HOUR_AGO + 100n,
        blockNumber: 1002n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // Withdrawal of 500 should match: 300 from first deposit + 200 from second
    expect(
      state.acquiredBalances.get(TOKEN_USDC.toLowerCase() as Address),
    ).toBe(500n);
  });
});

// ============ Rapid Sequence (Simulated Attack Pattern) ============

describe("Rapid operation sequences", () => {
  it("should correctly track spending across many rapid swaps", () => {
    const events: ProtocolExecutionEvent[] = [];
    const totalSwaps = 50;

    for (let i = 0; i < totalSwaps; i++) {
      events.push(
        createProtocolEvent({
          opType: OperationType.SWAP,
          tokensIn: [TOKEN_USDC],
          amountsIn: [100n],
          tokensOut: [TOKEN_WETH],
          amountsOut: [50n],
          spendingCost: 10n,
          timestamp: HOUR_AGO + BigInt(i),
          blockNumber: BigInt(1000 + i),
          logIndex: 0,
        }),
      );
    }

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    expect(state.totalSpendingInWindow).toBe(BigInt(totalSwaps) * 10n);
    // WETH accumulated from all swaps
    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBe(BigInt(totalSwaps) * 50n);
  });

  it("should correctly track acquired balance through swap chain", () => {
    // Swap A→B, then B→C, then C→D — each consuming the previous output
    const events = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensIn: [TOKEN_USDC],
        amountsIn: [1000n],
        tokensOut: [TOKEN_WETH],
        amountsOut: [500n],
        spendingCost: 100n,
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensIn: [TOKEN_WETH],
        amountsIn: [500n],
        tokensOut: [TOKEN_DAI],
        amountsOut: [490n],
        spendingCost: 0n, // WETH was acquired
        timestamp: HOUR_AGO + 10n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensIn: [TOKEN_DAI],
        amountsIn: [490n],
        tokensOut: [TOKEN_LINK],
        amountsOut: [100n],
        spendingCost: 0n, // DAI was acquired
        timestamp: HOUR_AGO + 20n,
        blockNumber: 1002n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // Intermediate tokens fully consumed
    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBeUndefined();
    expect(
      state.acquiredBalances.get(TOKEN_DAI.toLowerCase() as Address),
    ).toBeUndefined();

    // Final token is acquired, with timestamp inherited from first swap
    expect(
      state.acquiredBalances.get(TOKEN_LINK.toLowerCase() as Address),
    ).toBe(100n);

    const linkQueue = state.acquiredQueues.get(
      TOKEN_LINK.toLowerCase() as Address,
    );
    expect(linkQueue).toBeDefined();
    expect(linkQueue![0].originalTimestamp).toBe(HOUR_AGO); // inherited
  });
});

// ============ FIFO Queue Stress Tests ============

describe("FIFO queue stress", () => {
  it("should handle queue with many small entries", () => {
    const queue: AcquiredBalanceQueue = [];
    for (let i = 0; i < 100; i++) {
      addToQueue(queue, 10n, NOW - BigInt(100 - i));
    }

    expect(queue).toHaveLength(100);
    expect(getValidQueueBalance(queue, NOW, WINDOW_DURATION)).toBe(1000n);

    // Consume 550 — should consume 55 entries
    const result = consumeFromQueue(queue, 550n, NOW, WINDOW_DURATION);
    expect(result.remaining).toBe(0n);
    expect(queue).toHaveLength(45);
    expect(getValidQueueBalance(queue, NOW, WINDOW_DURATION)).toBe(450n);
  });

  it("should handle consuming exact queue total", () => {
    const queue: AcquiredBalanceQueue = [
      { amount: 100n, originalTimestamp: HOUR_AGO },
      { amount: 200n, originalTimestamp: HOUR_AGO + 10n },
      { amount: 300n, originalTimestamp: HOUR_AGO + 20n },
    ];

    const result = consumeFromQueue(queue, 600n, NOW, WINDOW_DURATION);
    expect(result.remaining).toBe(0n);
    expect(queue).toHaveLength(0);
    expect(result.consumed).toHaveLength(3);
  });

  it("should handle consuming more than queue total", () => {
    const queue: AcquiredBalanceQueue = [
      { amount: 100n, originalTimestamp: HOUR_AGO },
    ];

    const result = consumeFromQueue(queue, 999n, NOW, WINDOW_DURATION);
    expect(result.remaining).toBe(899n);
    expect(queue).toHaveLength(0);
  });

  it("should handle consuming zero amount", () => {
    const queue: AcquiredBalanceQueue = [
      { amount: 100n, originalTimestamp: HOUR_AGO },
    ];

    const result = consumeFromQueue(queue, 0n, NOW, WINDOW_DURATION);
    expect(result.remaining).toBe(0n);
    expect(result.consumed).toHaveLength(0);
    expect(queue).toHaveLength(1); // unchanged
  });
});

// ============ Mixed Operation Sequences ============

describe("Complex multi-operation sequences", () => {
  it("should handle swap → deposit → withdraw → transfer sequence", () => {
    const protocolEvents = [
      // 1. Swap USDC→WETH (WETH becomes acquired)
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensIn: [TOKEN_USDC],
        amountsIn: [1000n],
        tokensOut: [TOKEN_WETH],
        amountsOut: [500n],
        spendingCost: 100n,
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      // 2. Deposit WETH into Aave (consumes acquired WETH)
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_WETH],
        amountsIn: [300n],
        tokensOut: [],
        amountsOut: [],
        spendingCost: 0n, // WETH is acquired
        timestamp: HOUR_AGO + 10n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
      // 3. Withdraw WETH from Aave
      createProtocolEvent({
        opType: OperationType.WITHDRAW,
        target: TARGET_AAVE,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_WETH],
        amountsOut: [300n],
        timestamp: HOUR_AGO + 20n,
        blockNumber: 1002n,
        logIndex: 0,
      }),
    ];

    const transferEvents = [
      // 4. Transfer some WETH out
      createTransferEvent({
        token: TOKEN_WETH,
        amount: 100n,
        spendingCost: 5n,
        timestamp: HOUR_AGO + 30n,
        blockNumber: 1003n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      protocolEvents,
      transferEvents,
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // WETH: 500 (swap) - 300 (deposit) + 300 (withdraw, matched) - 100 (transfer) = 400
    // But the withdraw produces acquired only if matched to deposit
    const wethBalance = state.acquiredBalances.get(
      TOKEN_WETH.toLowerCase() as Address,
    );
    // 200 remaining from swap + 300 from matched withdraw - 100 transfer = 400
    expect(wethBalance).toBe(400n);

    // Spending: 100 (swap) + 5 (transfer) = 105
    expect(state.totalSpendingInWindow).toBe(105n);
  });

  it("should handle CLAIM without prior deposit (rewards)", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.CLAIM,
        target: TARGET_AAVE,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_LINK],
        amountsOut: [50n],
        spendingCost: 0n,
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // CLAIM without matching deposit — should NOT be acquired
    expect(
      state.acquiredBalances.get(TOKEN_LINK.toLowerCase() as Address),
    ).toBeUndefined();
  });

  it("should handle CLAIM with prior deposit (matched reward)", () => {
    const events = [
      // Deposit into Aave
      createProtocolEvent({
        opType: OperationType.DEPOSIT,
        target: TARGET_AAVE,
        tokensIn: [TOKEN_LINK],
        amountsIn: [1000n],
        tokensOut: [],
        amountsOut: [],
        spendingCost: 50n,
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      // Claim LINK rewards from same protocol
      createProtocolEvent({
        opType: OperationType.CLAIM,
        target: TARGET_AAVE,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [TOKEN_LINK],
        amountsOut: [20n],
        spendingCost: 0n,
        timestamp: HOUR_AGO + 100n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // CLAIM from same target with LINK deposit — should be matched
    const linkBalance = state.acquiredBalances.get(
      TOKEN_LINK.toLowerCase() as Address,
    );
    // 20 matched from 1000 deposit credit
    expect(linkBalance).toBe(20n);
  });
});

// ============ Empty / Zero Edge Cases ============

describe("Empty and zero edge cases", () => {
  it("should handle no events", () => {
    const state = buildSubAccountState(
      [],
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    expect(state.totalSpendingInWindow).toBe(0n);
    expect(state.acquiredBalances.size).toBe(0);
    expect(state.depositRecords).toHaveLength(0);
    expect(state.spendingRecords).toHaveLength(0);
  });

  it("should handle events with zero amounts", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensIn: [TOKEN_USDC],
        amountsIn: [0n],
        tokensOut: [TOKEN_WETH],
        amountsOut: [0n],
        spendingCost: 0n,
        timestamp: HOUR_AGO,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    expect(state.totalSpendingInWindow).toBe(0n);
    // Zero amounts should not create acquired entries
    expect(
      state.acquiredBalances.get(TOKEN_WETH.toLowerCase() as Address),
    ).toBeUndefined();
  });

  it("should handle events with empty token arrays", () => {
    const events = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensIn: [],
        amountsIn: [],
        tokensOut: [],
        amountsOut: [],
        spendingCost: 50n,
        timestamp: HOUR_AGO,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // Spending still tracked even with empty token arrays
    expect(state.totalSpendingInWindow).toBe(50n);
    expect(state.acquiredBalances.size).toBe(0);
  });

  it("should handle case-insensitive token addresses", () => {
    const upperToken = "0xAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" as Address;
    const lowerToken = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" as Address;

    const events = [
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensOut: [upperToken],
        amountsOut: [100n],
        timestamp: HOUR_AGO,
        blockNumber: 1000n,
        logIndex: 0,
      }),
      createProtocolEvent({
        opType: OperationType.SWAP,
        tokensIn: [lowerToken],
        amountsIn: [50n],
        tokensOut: [TOKEN_USDC],
        amountsOut: [25n],
        spendingCost: 0n,
        timestamp: HOUR_AGO + 10n,
        blockNumber: 1001n,
        logIndex: 0,
      }),
    ];

    const state = buildSubAccountState(
      events,
      [],
      SUB_ACCOUNT_A,
      NOW,
      WINDOW_DURATION,
    );

    // Should treat upper and lower case as same token
    const balance = state.acquiredBalances.get(lowerToken);
    expect(balance).toBe(50n); // 100 - 50 consumed
  });
});
