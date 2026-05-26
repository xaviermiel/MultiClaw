# Capped Trader — MultiClaw agent template for Pinata

A Pinata-hosted OpenClaw agent that operates a **MultiClaw vault** on Base. Its
spending is bounded **on-chain**: it cannot exceed its daily USD cap or send to a
non-whitelisted recipient, regardless of prompt injection or model error. The
guardrails are a Safe Zodiac module — not a system prompt.

This scaffold targets **Base Sepolia** for testing. Flip `MULTICLAW_CHAIN=base`
once validated.

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

1. **A deployed MultiClaw vault on Base Sepolia.** Use the AgentVaultFactory
   (`0xa4D6FdE6f8F6f873BB00d5059541B657468E6179`) with the agent's EOA as
   `agentAddress`. Note the resulting `DeFiInteractorModule` address.
2. **`@multiclaw/core` published to npm.** The skill installs it as a normal
   dependency (`^0.1.0`). See "SDK dependency" below.
3. A Base Sepolia–funded agent key (gas) and the Safe funded with a test token.

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

## Deploy & test on Base Sepolia

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
#    VAULT_MODULE_ADDRESS, MULTICLAW_CHAIN=baseSepolia
```

### Smoke test (the point of the demo)

In the agent's chat or terminal:

```
npm run mc -- status      # agent listed? oraclelessMode true? not paused?
npm run mc -- budget      # note cap + remaining
npm run mc -- transfer <token> <whitelisted-recipient> <small-amount>   # confirms
npm run mc -- transfer <token> <whitelisted-recipient> <over-cap-amount> # reverts
```

The over-cap transfer **reverting on-chain** is the demonstration: the guardrail
holds even when the agent is told to break it.

## Publishing to the Pinata marketplace

Marketplace listing is **curated** — "limited to ecosystem partners"
(partnerships@pinata.cloud). You can self-deploy and share by repo URL without
listing. Use `pinata agents templates submit <repo-url>` to register your
submission; marketplace placement is the partnership conversation.

## Notes & TODO

- The skill demonstrates guardrails via **capped/whitelisted transfers**. Adding
  protocol swaps (Uniswap/Aave) means building target calldata and passing it to
  `executeAsAgent(module, target, calldata, account)` — the SDK sends it; you
  build it. Left as a follow-up.
- History reads currently use the SDK's `getLogs` path. Swapping to the deployed
  subgraph (`VITE_SUBGRAPH_URL_*`) is a cheap upgrade for richer history.
- Verify exact `manifest.json` field shapes with `pinata agents templates validate`
  before submitting — some fields here are modeled from the docs and may need
  tweaks against the live schema.
