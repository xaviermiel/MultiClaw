# MultiClaw — On-Chain Guardrails for AI Agents

> Give your AI agent a wallet with hard spending limits. Unhackable by design.

[![Solidity](https://img.shields.io/badge/solidity-0.8.24-blue)]()
[![Tests](https://img.shields.io/badge/tests-450%2B%20passing-brightgreen)]()
[![License](https://img.shields.io/badge/license-MIT-blue)]()
[![Base](https://img.shields.io/badge/chain-Base-0052FF)]()

## The Problem

AI agents need wallets to operate on-chain. But giving an agent an unrestricted private key is a security disaster — one jailbreak, prompt injection, or plugin exploit and your funds are gone.

## The Solution

MultiClaw wraps a [Safe](https://safe.global/) multisig with a permission layer that enforces spending limits, protocol whitelists, and role-based access **at the smart contract level**. Even if your agent is 100% compromised, the on-chain guardrails hold.

**Deployment is permissionless** — anyone can deploy an agent vault via the factory contracts. No approval needed.

## Quick Start

### Using the SDK

```bash
npm install @multiclaw/core
```

```typescript
import { MultiClawClient } from "@multiclaw/core";

// 1. Deploy a vault (permissionless — anyone can call)
const client = new MultiClawClient({ chain: "base", signer: ownerWallet });
const vault = await client.createAgentVault({
  preset: "defi-trader", // DeFi Trader, Yield Farmer, Payment Agent, or Custom
  agentSigner: "0xAgent...",
  maxSpendingBps: 500, // 5% of Safe per 24h (BPS mode)
  // OR: maxSpendingUSD: 1000n * 10n**18n,  // $1000/day fixed (USD mode)
});

// 2. Agent operates within guardrails
const agent = new MultiClawAgent({
  chain: "base",
  signer: agentWallet,
  moduleAddress: vault.module,
});

await agent.executeOnProtocol("0xUniRouter...", swapCalldata);
const budget = await agent.getRemainingBudget();
```

### Using Foundry (Direct Contract Interaction)

```bash
git clone <repository-url> && cd MultiClaw
forge install && forge build

# Deploy a vault via AgentVaultFactory (permissionless — anyone can call)
# The factory is already deployed on Base at the address in sdk/packages/core/src/chains.ts
cast send $AGENT_VAULT_FACTORY "deployVaultFromPreset(address,address,address,uint256,address[],address[])" \
  $SAFE_ADDRESS $ORACLE_ADDRESS $AGENT_ADDRESS $PRESET_ID "[]" "[]" \
  --rpc-url $BASE_RPC_URL --private-key $PRIVATE_KEY
```

## What the Agent Can Do (Within Guardrails)

- Swap tokens on Uniswap, 1inch, KyberSwap, Paraswap
- Supply/withdraw on Aave V3, Morpho Blue, Morpho MetaMorpho vaults
- Repay protocol debt on Aave V3, Morpho Blue (if granted REPAY role — improves Safe health)
- Claim rewards from Merkl
- Transfer tokens to specified recipients (if granted TRANSFER role)

## What the Agent Cannot Do (Even If Fully Compromised)

- Spend more than its rolling 24h budget
- Interact with non-whitelisted protocols
- Approve tokens to unknown contracts
- Redirect swap/withdrawal output to an attacker address
- Bypass limits via any prompt injection, jailbreak, or plugin exploit

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  DEPLOYMENT (permissionless, single transaction)                │
│                                                                 │
│  AgentVaultFactory.deployVault(config)                          │
│    → Deploy DeFiInteractorModule (CREATE2, deterministic)       │
│    → Configure: roles, limits, allowlists, parsers, feeds       │
│    → Transfer ownership to Safe                                 │
│    → Register in ModuleRegistry                                 │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────┐
│  RUNTIME (on-chain enforcement)                                 │
│                                                                 │
│  Safe Multisig ◄── DeFiInteractorModule                        │
│  (holds funds)     │                                            │
│                    ├─ Role Check         (agent authorized?)    │
│                    ├─ Oracle Freshness   (data < 60 min?)       │
│                    ├─ Target Allowlist   (protocol whitelisted?)│
│                    ├─ Recipient Guard    (output → Safe only)   │
│                    ├─ Spending Limit     (within USD budget?)   │
│                    ├─ Acquired Balance   (free tokens first)    │
│                    └─ Approve Cap        (spender whitelisted?) │
│                                                                 │
│  10 Protocol Parsers: Aave V3, Morpho, Uniswap V3/V4,         │
│    Universal Router, 1inch, KyberSwap, Paraswap, Merkl         │
└─────────────────────────────────────────────────────────────────┘
```

## Permissionless Deployment

Both `ModuleFactory` and `AgentVaultFactory` are **permissionless** — anyone can deploy a module or vault without requiring approval from the MultiClaw team. Security is preserved because:

1. **Module ownership transfers to the Safe** after deployment — only the Safe can configure it
2. **The deployer has no special privileges** over the deployed module
3. **The Safe must separately enable the module** (requires Safe multisig transaction)
4. **Registry tracks all deployments** for oracle discovery

### Template Presets

| Preset        | Protocols            | Budget       | Role         |
| ------------- | -------------------- | ------------ | ------------ |
| DeFi Trader   | Uniswap + Aave V3    | 5%/day       | EXECUTE      |
| Yield Farmer  | Morpho + Aave V3     | 10%/day      | EXECUTE      |
| Payment Agent | None (transfer only) | 1%/day       | TRANSFER     |
| Custom        | User-defined         | User-defined | User-defined |

### Dual-Mode Spending Limits

Each sub-account's spending limit can be configured in one of two modes:

- **BPS mode** (`maxSpendingBps`): Percentage of Safe value per window (e.g., 500 = 5%). Scales with portfolio.
- **USD mode** (`maxSpendingUSD`): Fixed dollar amount per window (e.g., $1,000/day). Does not scale.

Exactly one must be set. The `absoluteMaxSpendingBps` hard cap (20%) still applies in both modes — even a fixed $1M limit cannot exceed 20% of the Safe's value.

## Acquired Balance Model

The spending limit mechanism distinguishes between:

- **Original tokens** (in Safe at start of window) → using them **costs spending**
- **Acquired tokens** (received from DeFi operations) → **free to use** for 24h

This lets agents chain operations (swap → deposit → withdraw) without burning budget on every step.

| Operation    | Costs Spending?     | Output Acquired? |
| ------------ | ------------------- | ---------------- |
| **Swap**     | Yes (original only) | Yes              |
| **Deposit**  | Yes (original only) | No               |
| **Withdraw** | No (FREE)           | Conditional\*    |
| **Claim**    | No (FREE)           | Conditional\*    |
| **Approve**  | No (capped)         | N/A              |
| **Repay**    | No (REPAY role\*\*) | N/A              |
| **Transfer** | Always              | N/A              |

\* Only if matched to a deposit by the same agent on the same protocol.
\*\* Requires `DEFI_REPAY_ROLE` (3). Improves Safe health factor — no spending check needed.

## Security Model

```
┌──────────────────────────────────────────────────────────────────┐
│ Layer 1   ROLE CHECK          Does this key have the right role? │
│ Layer 2   ORACLE FRESHNESS    Is spending state < 60 min old?    │
│ Layer 3   OPERATION CLASSIFY  Is this selector registered?       │
│ Layer 4   TARGET ALLOWLIST    Is this protocol whitelisted?      │
│ Layer 5   PARSER EXTRACTION   Decode tokens, amounts, recipient  │
│ Layer 6   RECIPIENT GUARD     Output goes to Safe, not attacker  │
│ Layer 7   SPENDING LIMIT      USD cost fits in remaining budget  │
│ Layer 8   CUMULATIVE CAP      On-chain per-window spending limit │
│ Layer 9   ACQUIRED BALANCE    Free tokens deducted first         │
│ Layer 10  SWAP MARKING        Swap outputs auto-marked acquired  │
│ Layer 11  APPROVE CAP         Spender whitelisted, amount capped │
│ Layer 12  SAFE EXECUTION      execTransactionFromModule()        │
└──────────────────────────────────────────────────────────────────┘
```

Every transaction must pass all applicable layers. Failure at any layer = revert.

### Oracle Compromise Protection

Even if the oracle key is compromised, on-chain cumulative counters limit damage:

| Protection                   | Mechanism                                                         | Default |
| ---------------------------- | ----------------------------------------------------------------- | ------- |
| **Cumulative spending cap**  | `cumulativeSpent` tracked on-chain, oracle cannot reset           | 20%     |
| **Safe value snapshot**      | `windowSafeValue` frozen at window start, inflation has no effect | —       |
| **Oracle acquired budget**   | `cumulativeOracleGrantedUSD` caps oracle's acquired grants        | 20%     |
| **Swap marking (trustless)** | Swap outputs auto-marked acquired on-chain, no oracle needed      | —       |
| **Per-account USD cap**      | USD-mode sub-accounts capped by `maxSpendingUSD`                  | —       |
| **Version counters**         | Oracle must pass expected version; stale writes are skipped       | —       |

Max damage per window: `absoluteMaxSpendingBps + maxOracleAcquiredBps` (default 40%). See [`oracle/ORACLE_SECURITY.md`](./oracle/ORACLE_SECURITY.md).

## Agent Framework Integrations

| Framework      | Package                | Description                                     |
| -------------- | ---------------------- | ----------------------------------------------- |
| LangChain      | `@multiclaw/langchain` | StructuredTool adapter                          |
| Eliza          | `@multiclaw/eliza`     | Plugin with SWAP, DEPOSIT, CHECK_BUDGET actions |
| GOAT           | `@multiclaw/goat`      | GOAT SDK wrapper                                |
| Raw TypeScript | `@multiclaw/core`      | Direct viem-based contract interactions         |

## Emergency Controls

| Control                      | Purpose                            |
| ---------------------------- | ---------------------------------- |
| `pause()`                    | Freeze all module operations       |
| `revokeRole()`               | Remove agent permissions instantly |
| `unregisterSelector()`       | Block specific operation types     |
| `setAllowedAddresses(false)` | Remove protocol from whitelist     |

## Oracle Integration

Two off-chain services maintain the module's spending state:

1. **Spending Oracle** — monitors events, tracks spending allowances & acquired balances per agent
2. **Safe Value Monitor** — periodically values the Safe's portfolio (ERC20, Aave, Morpho, Uniswap positions)

See [`oracle/README.md`](./oracle/README.md) for details.

## Development

```bash
forge build          # Compile contracts
forge test           # Run all tests (450+)
forge test --gas-report  # With gas reporting
make deploy-base-sepolia # Deploy to Base Sepolia
```

## Documentation

- [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md) — Full system architecture and contract reference
- [`docs/SPENDING_LIMIT_MECHANISM.md`](./docs/SPENDING_LIMIT_MECHANISM.md) — Deep dive into the Acquired Balance Model
- [`docs/SPENDING_LIMIT_OVERVIEW.md`](./docs/SPENDING_LIMIT_OVERVIEW.md) — Quick overview of spending limits
- [`docs/TODO.md`](./docs/TODO.md) — Development roadmap

## License

MIT License - see [LICENSE](./LICENSE)

## Disclaimer

**Use at your own risk.** Smart contracts may contain vulnerabilities. This software has not been audited.

---

**Built by DUSA LABS — On-chain guardrails for AI agents**
