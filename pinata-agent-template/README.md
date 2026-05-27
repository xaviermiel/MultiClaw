# Capped Trader — MultiClaw agent template for Pinata

A Pinata-hosted OpenClaw agent that operates a **MultiClaw vault** on Base. Its
spending is bounded **on-chain**: it cannot exceed its daily USD cap or send to a
non-whitelisted recipient, regardless of prompt injection or model error. The
guardrails are a Safe Zodiac module — not a system prompt.

This scaffold targets **Base mainnet** by default. To rehearse
on testnet first, set `MULTICLAW_CHAIN=baseSepolia` and point `VAULT_MODULE_ADDRESS`
/ `SUBGRAPH_URL` at a Base Sepolia vault and subgraph.

## Layout

```
pinata-agent-template/
├── manifest.json        # Pinata template spec (agent, model, secrets, skills, tasks, routes)
├── SOUL.md              # agent persona / prime directive
├── BOOTSTRAP.md         # first-run checklist
└── workspace/
    └── skills/
        └── multiclaw-vault/   # the skill: reads + capped transfers via @multiclaw/core
            ├── SKILL.md
            ├── .env.example
            ├── package.json
            ├── tsconfig.json
            └── src/{client,index}.ts
```

## Prerequisites

1. **A deployed MultiClaw vault on Base mainnet.** Use the AgentVaultFactory
   (`0x389623997Bc006dA3BdBbE18d7Be04dACF4f09Ff`) with the agent's EOA as
   `agentAddress`. Note the resulting `DeFiInteractorModule` address.
   (Base Sepolia factory: `0xa4D6FdE6f8F6f873BB00d5059541B657468E6179`.)
2. **`@multiclaw/core` published to npm.** The skill installs it as a normal
   dependency (`^0.1.1`). See "SDK dependency" below.
3. An ETH-funded agent key (gas) and the Safe funded with the token to trade.

## SDK dependency

The skill depends on `@multiclaw/core`. The agent runs in an isolated container
and the skill folder is pinned to public IPFS, so the dependency must resolve
from a public registry — i.e. the SDK must be **published to npm**. Once it is,
`npm install` in `scripts.build` (see `manifest.json`) resolves it with no extra
steps.

Local fallback for pre-publish testing only: pack the workspace SDK
(`cd sdk/packages/core && npm run build && npm pack`) and
`npm install <tarball>` inside the skill folder. Don't ship a template that
relies on the tarball — publish first.

## Deploy & test on Base mainnet

```bash
# 1. install the Pinata CLI and authenticate (see docs.pinata.cloud/agents)
# 2. publish the skill (pins to IPFS, returns a CID)
pinata agents skills create workspace/skills/multiclaw-vault
#    → paste the returned CID into manifest.json skills[0].cid

# 3. validate the template structure
pinata agents templates validate .

# 4. deploy the agent from this repo
pinata agents create --template <this-repo-url>
#    set secrets when prompted: ANTHROPIC_API_KEY, AGENT_PRIVATE_KEY,
#    VAULT_MODULE_ADDRESS, SUBGRAPH_URL (for history). MULTICLAW_CHAIN
#    defaults to base; set it to baseSepolia only for a testnet rehearsal.
```

### Smoke test

⚠️ For tests, use a tiny amount, or run the rehearsal on `baseSepolia` first.

In the agent's chat or terminal:

```
npm run mc -- status      # agent listed? oraclelessMode true? not paused?
npm run mc -- budget      # note cap + remaining (window-aware)
npm run mc -- transfer <token> <whitelisted-recipient> <small-amount>   # confirms
npm run mc -- transfer <token> <whitelisted-recipient> <over-cap-amount> # reverts
```

The over-cap transfer **reverting on-chain** is the demonstration: the guardrail
holds even when the agent is told to break it.

## Notes

- The skill demonstrates guardrails via **capped/whitelisted transfers**. Adding
  protocol swaps (Uniswap/Aave) means building target calldata and passing it to
  `executeAsAgent(module, target, calldata, account)` — the SDK sends it; you
  build it. Left as a follow-up.
- History reads are **subgraph-backed** (`SUBGRAPH_URL`), not `eth_getLogs` — this
  avoids the public RPC's 2000-block `eth_getLogs` cap and scales better. See
  "Subgraph strategy" below.

## Subgraph strategy

The `history` command (and the `daily-budget-report` task, which calls it) reads
from a MultiClaw subgraph via `SUBGRAPH_URL`. `budget`, `status`, `acquired`, and
`transfer` work without it — only history depends on it.

A few things to know:

- **It is not a secret.** The subgraph indexes on-chain events
  (`AgentVaultCreated`, `ProtocolExecution`, `TransferExecuted`, …) that are
  already public. The endpoint is read-only; exposing it leaks nothing private.
- **Bring your own — don't share one endpoint across deployments.** The concern
  is quota, not secrecy: a single shared endpoint means every deployment's
  queries hit the same query cap. Each operator should point `SUBGRAPH_URL` at
  their own subgraph so query volume (and cost) stays isolated.
- **Where to get one.** Deploy the MultiClaw subgraph (`https://github.com/xaviermiel/MultiClaw/tree/main/subgraph`) 
  to The Graph Studio — its free tier (100k queries/mo) is ample for a single agent. 
  Studio hands you a query URL like `https://api.studio.thegraph.com/query/<id>/<name>/<version>`. 
  Migrate to The Graph's decentralized network only if query volume outgrows the free tier.
- **Leave it empty if you don't need history.** With `SUBGRAPH_URL` unset, the
  `history` command reports a clear "missing env var" error and everything else
  keeps working.
- Verify exact `manifest.json` field shapes with `pinata agents templates validate`
  before submitting — some fields here are modeled from the docs and may need
  tweaks against the live schema.
