# Bootstrap

First-run checklist for the Capped Trader agent.

## 0. Get a vault (`VAULT_MODULE_ADDRESS`)

This is set up by the **owner before the agent runs** — the agent never deploys
its own vault. `VAULT_MODULE_ADDRESS` is the `DeFiInteractorModule` address of a
MultiClaw vault in which **this agent's address is authorized**.

First, get the agent's address from its key (so you can authorize it):

```
cast wallet address --private-key $AGENT_PRIVATE_KEY
```

Then obtain a module address one of three ways:

**A. Deploy via the frontend wizard (easiest).** In the MultiClaw interface,
create a vault, set this agent's address as the agent, pick a preset + USD cap,
and deploy. The wizard shows the deployed **module address** — that's your
`VAULT_MODULE_ADDRESS`.

**B. Deploy via the SDK.** `createAgentVaultFromPreset` returns the module
address. It deploys through the same `AgentVaultFactory` as the wizard, so the
vault is indexed by the subgraph and shows up in the frontend identically — just
ensure the SDK and frontend point at the same factory address.

```ts
import { MultiClawClient } from "@multiclaw/core";
const client = new MultiClawClient({ chain: "base" });
const { module } = await client.createAgentVaultFromPreset(
  {
    safe, // the Safe that owns the vault
    oracle: "0x0000000000000000000000000000000000000000", // oracleless
    agentAddress, // the agent's address from above
    presetId: 0n, // 0=DeFi Trader, 1=Yield Farmer, 2=Payment Agent
    priceFeedTokens,
    priceFeedAddresses, // from getPriceFeedArrays("base")
    allowedRecipients: [],
  },
  ownerAccount, // the deployer's signer
);
console.log(module); // → VAULT_MODULE_ADDRESS
```

**C. Reuse an existing vault.** If a vault for this agent already exists, find
its module address by querying the subgraph (`Vault` entity, filter by
`agentAddress`) or by reading `AgentVaultCreated` events from the factory on the
block explorer. The factory is `0x389623997Bc006dA3BdBbE18d7Be04dACF4f09Ff` on
Base mainnet (`0xa4D6FdE6f8F6f873BB00d5059541B657468E6179` on Base Sepolia).

Deploy on the chain matching `MULTICLAW_CHAIN`. Once you have the module address,
set it as the `VAULT_MODULE_ADDRESS` secret and continue.

## 1. Verify configuration

Confirm these secrets are set (Pinata Secrets Vault):

- `ANTHROPIC_API_KEY` — LLM provider
- `AGENT_PRIVATE_KEY` — your agent signer
- `VAULT_MODULE_ADDRESS` — the DeFiInteractorModule you operate
- `MULTICLAW_CHAIN` — `base` (default) or `baseSepolia`

## 2. Sanity-check the vault

Run, from `workspace/skills/multiclaw-vault`:

```
npm run mc -- status
```

Confirm:

- `paused` is `false`
- your agent address appears under `executeAgents` or `transferAgents`
- `oraclelessMode` is `true` (Option B vaults run oracleless)

If your address is **not** listed, the owner has not yet authorized this agent on
the module. Stop and ask them to grant the role before doing anything else.

## 3. Read your budget

```
npm run mc -- budget
```

Note the `cap` and `remaining` values. This is your hard ceiling for the window.

## 4. Operating loop

For any value-moving request:

1. `budget` → confirm headroom
2. act (`transfer …`) only if it fits
3. report cost + remaining budget back to the owner

If a task needs more than the cap, ask the owner to raise the limit. Never seek a
workaround.
