# Vault Guardian — MultiClaw agent template for Pinata

A Pinata-hosted OpenClaw agent that **guards** a MultiClaw vault. It monitors the
owner's positions and, on a credible threat (exploit, drain, emergency pause,
abnormal oracle), pulls the funds **back to the owner — and nowhere else.**

The "nowhere else" is enforced **on-chain**: the Safe's recipient whitelist is
locked to the owner's address. So even if this agent is prompt-injected,
jailbroken, or its key is stolen, the worst it can do is return your funds to
**you**. That is the entire pitch: a low-trust agent you can safely hand on-chain
authority, because its blast radius is "moves your money to you."

This is the **circuit-breaker** member of the MultiClaw agent family. It shares
the same `multiclaw-vault` skill as the [Capped Trader](../pinata-agent-template/)
— same primitive, a different bounded role.

## How it differs from the Capped Trader

|                           | Capped Trader               | Vault Guardian                       |
| ------------------------- | --------------------------- | ------------------------------------ |
| Goal                      | Trade within a spending cap | Protect; withdraw to owner on threat |
| On-chain bound            | Spending cap (USD/window)   | Recipient whitelist = owner only     |
| Default behavior          | Act within budget           | **Observe; do nothing**              |
| Worst case if compromised | Spends up to the cap        | Returns funds to the owner           |

## Shared skill (`multiclaw-vault`)

Both templates use the **same** skill — pin it once with
`pinata agents skills create`, then reference the **same CID** in both manifests
(`skills[0].cid`). The skill source is canonical in the Capped Trader template;
this guardian repo references it by CID. One primitive, many roles.

## Capabilities & one remaining follow-up

1. **Read the recipient whitelist.** ✅ Done. The skill's `whitelist` command
   (SDK `getRecipientWhitelist`, `@multiclaw/core@0.1.2`) reports whether the
   whitelist is enforced, checks `OWNER_ADDRESS` membership, and — with
   `SUBGRAPH_URL` set — enumerates the full allowed set and computes
   `ownerIsSoleRecipient`. That flag is the guardian's startup safety check
   (BOOTSTRAP step 2).
2. **Withdraw a protocol position.** ⏳ Follow-up. Sweeping loose tokens to the
   owner works today via the skill's `transfer` command. Pulling a position _out
   of_ a protocol (Aave/Morpho `withdraw`) needs the `executeAsAgent(module,
target, calldata, account)` path — the same protocol-execution follow-up the
   trader template flags for swaps. Build the protocol withdraw calldata, the SDK
   sends it.

## Prerequisites

1. A MultiClaw vault on Base mainnet configured for guarding: agent holds the
   transfer role, recipient whitelist **enabled**, `OWNER_ADDRESS` the **only**
   allowed recipient. See `BOOTSTRAP.md` §0.
2. `@multiclaw/core` on npm (`^0.1.2`, for the `whitelist` reader).
3. An ETH-funded agent key (gas). The Safe holds the positions to protect.

## Deploy

```bash
# pin the shared skill once (reuse the Capped Trader's skill source), get a CID
pinata agents skills create <path-to>/multiclaw-vault
#   → paste the SAME CID into this manifest.json skills[0].cid (and the trader's)

pinata agents templates validate .
pinata agents create --template <this-repo-url>
#   set secrets: ANTHROPIC_API_KEY, AGENT_PRIVATE_KEY, VAULT_MODULE_ADDRESS,
#   OWNER_ADDRESS, SUBGRAPH_URL (to enumerate the whitelist / history).
```

See the [Capped Trader README](../pinata-agent-template/README.md) for the
subgraph strategy and SDK-dependency notes — they apply identically here.
