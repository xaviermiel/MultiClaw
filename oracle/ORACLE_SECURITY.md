# Oracle Security: Compromise Risk & Mitigations

## 1. The Risk: Infinite Reset Attack

### Current Trust Model

The oracle EOA has three stateless setter functions on `DeFiInteractorModule`:

| Function                                           | What it controls                                 | Existing cap                                 |
| -------------------------------------------------- | ------------------------------------------------ | -------------------------------------------- |
| `updateSafeValue(uint256)`                         | USD portfolio value used to compute spending cap | None                                         |
| `updateSpendingAllowance(address, uint256)`        | USD budget for original tokens                   | `safeValue * absoluteMaxSpendingBps / 10000` |
| `updateAcquiredBalance(address, address, uint256)` | Per-token balance that bypasses spending limits  | Safe's actual `balanceOf`                    |

All three are **stateless sets** — the oracle replaces the value entirely on each call. There is no on-chain cooldown, nonce, rate limit, or per-window cumulative tracking.

### The Attack

If the oracle key is compromised, an attacker can **repeatedly reset** spending allowance and acquired balances between sub-account operations, draining the Safe in a single block:

```
// All transactions in one block (e.g., via bundled submission):

tx1: oracle.updateSafeValue(1_000_000e18)                          // inflate safeValue
tx2: oracle.batchUpdate(sub, 200_000e18, [USDC], [safeBal])        // max allowance + full acquired
tx3: sub.transferToken(USDC, attacker, safeBal)                     // drain (acquired = free)
tx4: oracle.batchUpdate(sub, 200_000e18, [WETH], [safeBal])        // reset for next token
tx5: sub.transferToken(WETH, attacker, safeBal)                     // drain next token
...repeat until Safe is empty
```

Each `batchUpdate` re-marks the remaining balance as acquired and resets the spending allowance. Each `transferToken` drains a chunk at zero spending cost. `_capToSafeBalance` passes every time because it reads the current (decreasing) balance.

### Why `absoluteMaxSpendingBps` Does Not Help

The 20% backstop only caps the `spendingAllowance` value per oracle call. But:

1. **Acquired tokens bypass spending entirely.** A token marked as acquired has zero spending cost in `_executeWithSpendingCheck`, `_executeApproveWithCap`, and `transferToken`. The oracle can mark 100% of any token as acquired.
2. **The oracle controls `safeValue`.** It can inflate `safeValue` to raise the spending cap arbitrarily before setting the allowance.
3. **Resets are unlimited.** After each drain, the oracle resets both values. The contract has no memory of how much was already spent or acquired in the current window — that tracking exists only in the off-chain oracle logic, which is the compromised component.

### Total Exposure

- **Spending path**: unbounded (oracle inflates `safeValue`, resets allowance after each spend)
- **Acquired path**: 100% of every token (oracle sets acquired to full balance, resets after each transfer)
- **Combined**: **100% of the Safe**, not the 20% the backstop suggests

---

## 2. Proposed Mitigations

### Solution 1: On-Chain Cumulative Spending Tracker

Track total USD spent per sub-account per window **inside the contract**, incremented by execution functions. The oracle cannot reset it.

#### Mechanism

```solidity
mapping(address => uint256) public windowStart;
mapping(address => uint256) public windowSafeValue;   // snapshot at window start
mapping(address => uint256) public cumulativeSpent;

// In _executeWithSpendingCheck / transferToken, after computing spendingCost:
(uint256 maxBps, uint256 windowDuration) = getSubAccountLimits(subAccount);

if (block.timestamp > windowStart[subAccount] + windowDuration) {
    // New window — snapshot safeValue, reset counter
    windowStart[subAccount] = block.timestamp;
    windowSafeValue[subAccount] = safeValue.totalValueUSD;
    cumulativeSpent[subAccount] = 0;
}

cumulativeSpent[subAccount] += spendingCost;
uint256 maxSpending = (windowSafeValue[subAccount] * maxBps) / 10000;
require(cumulativeSpent[subAccount] <= maxSpending);
```

#### What It Fixes

- **Spending reset**: the cumulative counter is only written by execution functions (`_executeWithSpendingCheck`, `transferToken`), never by the oracle. Repeated `updateSpendingAllowance` calls have no effect on the on-chain cap.
- **`safeValue` inflation**: `windowSafeValue` is snapshotted once per window. The oracle cannot inflate it mid-window to raise the spending cap.

#### What It Does Not Fix

- **Acquired balance reset**: the oracle can still mark all tokens as acquired, making every operation cost zero spending. The cumulative spending tracker only counts `spendingCost`, and acquired tokens have `spendingCost = 0`.

#### Pros

- Low complexity: ~15 lines of new contract code, 3 new storage slots per sub-account
- No price feed dependency for the cap itself (uses snapshotted `safeValue`)
- No UX impact: operations work identically, just with an irrevocable counter
- `spendingAllowance` becomes a UX hint from the oracle; real enforcement is on-chain

#### Cons

- Does **not** fix the acquired balance path: the primary drain vector remains open
- Must be combined with a solution for acquired balances to be meaningful
- If `safeValue` is stale at the moment a new window starts, the snapshot could be outdated (mitigated by the existing `maxSafeValueAge` freshness check)

---

### Solution 6: Hybrid On-Chain Acquired Marking

Issues:

- **3 of 5 deposit-capable parsers return empty `extractOutputTokens` for deposits** (UniswapV3, UniswapV4, MorphoBlue). The contract cannot track what receipt token was received.
- **Input and output tokens differ** across operations (swap USDC for DAI, LP deposit USDC+ETH withdraw in different ratios).
- **Value changes over time** (yield, impermanent loss) — deposit credit in token terms doesn't match withdrawal amounts.
- **USD valuation on-chain is expensive** and introduces price feed dependency for the credit system itself.

#### Adapted Mechanism: Two-Tier Acquired Balance

Split acquired balance management based on what the contract can verify on-chain:

**Tier 1 — On-chain (trustless, no oracle involvement):**

SWAP outputs are marked as acquired immediately by the contract. The spending cost was already charged on the input tokens. The output tokens are new value the sub-account generated.

```solidity
// In _executeWithSpendingCheck, after computing amountsOut:
if (opType == OperationType.SWAP) {
    for (uint256 i = 0; i < tokensOut.length; i++) {
        acquiredBalance[subAccount][tokensOut[i]] += amountsOut[i];
    }
}
```

This covers all DEX operations (1inch, Paraswap, KyberSwap, Universal Router, Uniswap V3/V4 swaps) — the highest-frequency operation type. No oracle needed.

**Tier 2 — Oracle-managed (with on-chain cumulative cap):**

For WITHDRAW/CLAIM outputs, the oracle marks tokens as acquired after deposit/withdrawal matching. But the oracle's power is bounded by a per-window cumulative budget that it cannot reset:

```solidity
mapping(address => uint256) public acquiredGrantWindowStart;
mapping(address => uint256) public cumulativeOracleGrantedUSD;
uint256 public maxOracleAcquiredBps = 5000; // 50% of Safe value per window

// In updateAcquiredBalance / batchUpdate, when oracle increases acquired:
uint256 increaseValueUSD = _estimateTokenValueUSD(token, newBalance)
                         - _estimateTokenValueUSD(token, oldBalance);
if (increaseValueUSD > 0) {
    if (block.timestamp > acquiredGrantWindowStart[sub] + windowDuration) {
        acquiredGrantWindowStart[sub] = block.timestamp;
        cumulativeOracleGrantedUSD[sub] = 0;
    }
    cumulativeOracleGrantedUSD[sub] += increaseValueUSD;
    uint256 maxGrant = (windowSafeValue[sub] * maxOracleAcquiredBps) / 10000;
    require(cumulativeOracleGrantedUSD[sub] <= maxGrant);
}
```

The cumulative counter only grows on oracle increases and only resets on window expiry. The oracle cannot reset it.

#### What It Fixes

- **Acquired reset attack**: after the oracle grants acquired balance and the sub-account drains, the oracle tries to re-grant — but the cumulative counter has already recorded the first grant. Repeated resets accumulate and hit the cap.
- **Swap acquired (Tier 1)**: fully trustless, oracle not involved at all.

#### What It Does Not Fix (alone)

- **Spending reset**: needs Solution 1 for that.
- The `maxOracleAcquiredBps` cap must be generous enough for legitimate withdrawal activity, which limits how much it constrains an attacker.

#### Pros

- Swaps (the most common operation) become fully trustless for acquired balance
- Oracle's acquired budget has a hard per-window cap that cannot be reset
- No per-token deposit tracking needed — avoids token mismatch problems entirely
- The cap is in USD (not per-token), so it works naturally across different tokens
- Combined with Solution 1, both the spending and acquired attack vectors are closed

#### Cons

- Tier 2 requires USD valuation on oracle updates (Chainlink reads = gas cost)
- `maxOracleAcquiredBps` must be tuned: too low blocks legitimate withdrawals, too high leaves more exposure. A sub-account that routinely moves 50% of Safe value through DeFi needs at least 50% budget
- Swap outputs marked acquired on-chain increase storage writes (one SSTORE per output token per swap)
- If oracle budget is exhausted by legitimate activity, further withdrawn tokens are treated as original — sub-account must use spending allowance for them

---

### Combined: Solution 1 + Solution 6

When both are applied together:

| Attack Vector                        | Protection                             | Enforced By                            |
| ------------------------------------ | -------------------------------------- | -------------------------------------- |
| Spending allowance reset             | On-chain cumulative counter per window | Contract execution functions           |
| `safeValue` inflation                | Window-start snapshot                  | Contract (first op in new window)      |
| Acquired balance reset (swaps)       | Marked on-chain at execution time      | Contract (`_executeWithSpendingCheck`) |
| Acquired balance reset (withdrawals) | Cumulative oracle budget per window    | Contract (`updateAcquiredBalance`)     |

**Maximum damage from compromised oracle per window:**

```
maxSpendingBps + maxOracleAcquiredBps  (e.g., 20% + 50% = 70%)
```

Configurable by the Safe owner. This is a **real** cap — enforced on-chain by cumulative counters that the oracle cannot reset. The oracle's role reduces to:

| Before                                | After                                                                          |
| ------------------------------------- | ------------------------------------------------------------------------------ |
| Full control of spending allowance    | Provides freshness attestation; spending tracked on-chain                      |
| Full control of all acquired balances | Only manages WITHDRAW/CLAIM acquired, within a capped budget                   |
| Full control of `safeValue`           | `safeValue` is snapshotted at window start; mid-window inflation has no effect |

---

## 3. Alternative: Chainlink CRE Migration

The project previously used Chainlink CRE (Computation Runtime Environment) before switching to the current self-hosted Node.js oracle. Migrating back to CRE is an alternative mitigation strategy.

### What CRE Provides

CRE runs the oracle logic as a deterministic WASM workflow on Chainlink's Decentralized Oracle Network (DON). Instead of a single EOA private key, a threshold of independent node operators must reach consensus on every update.

| Property        | Current (self-hosted EOA)        | Chainlink CRE                                |
| --------------- | -------------------------------- | -------------------------------------------- |
| Signing key     | Single private key on one server | Threshold key across N independent nodes     |
| To compromise   | Steal one private key            | Compromise M-of-N independent node operators |
| Logic execution | One Node.js process              | Same logic executed by all DON nodes         |
| Logic tamper    | SSH into server, modify code     | Need workflow owner key + CRE redeployment   |

### What CRE Fixes

- **Single-key compromise**: no single EOA to steal. Attacker must compromise a threshold of independent Chainlink nodes — orders of magnitude harder.
- **Silent logic tampering**: WASM is deployed with a verifiable hash. All nodes execute the same bytecode.
- **Infrastructure reliability**: Chainlink manages node redundancy, RPC diversity, and gas funding.

### What CRE Does Not Fix

- **Logic bugs**: if the oracle logic has a bug that computes wrong values, CRE faithfully executes that bug across all nodes and reaches consensus on the wrong result. CRE guarantees integrity of execution, not correctness of logic.
- **Workflow owner key compromise**: someone owns the CRE workflow deployment key. If compromised, malicious logic can be deployed to the DON. This is a single-key risk, shifted from the oracle EOA to the workflow deployer.
- **Supply chain attacks**: the oracle depends on npm packages, the TypeScript compiler, and the WASM toolchain. A compromised dependency gets compiled into the workflow and executed by all nodes.
- **The smart contract interface is unchanged**: `updateSpendingAllowance`, `updateAcquiredBalance`, and `updateSafeValue` still accept any value from the authorized address. CRE reduces the probability of malicious calls but doesn't add on-chain constraints.

### CRE vs On-Chain Caps

They protect against different threats:

| Threat                    | CRE                   | On-chain caps (Solutions 1 + 6) |
| ------------------------- | --------------------- | ------------------------------- |
| Oracle key theft          | Yes (no single key)   | Yes (limits damage)             |
| Oracle logic bug          | No (executes the bug) | Yes (hard cap regardless)       |
| Workflow owner compromise | No                    | Yes                             |
| Supply chain attack       | No                    | Yes                             |
| DON threshold compromise  | No                    | Yes                             |

On-chain caps provide a **mathematical guarantee from the contract alone**. CRE provides a **probabilistic guarantee** based on the difficulty of compromising the DON.

