#!/usr/bin/env npx tsx
/**
 * Update chain config addresses after deployment.
 *
 * Usage:
 *   CHAIN=base \
 *   AGENT_VAULT_FACTORY=0x... \
 *   PRESET_REGISTRY=0x... \
 *   MODULE_REGISTRY=0x... \
 *   npx tsx scripts/update-addresses.ts
 *
 * Or read from Foundry broadcast artifacts:
 *   CHAIN=baseSepolia \
 *   BROADCAST_DIR=../../../broadcast \
 *   npx tsx scripts/update-addresses.ts
 */

import { readFileSync, writeFileSync } from "fs";
import { resolve } from "path";

const chain = process.env.CHAIN;
if (!chain) {
  console.error("CHAIN env var required (base | baseSepolia)");
  process.exit(1);
}

const factory = process.env.AGENT_VAULT_FACTORY;
const presetReg = process.env.PRESET_REGISTRY;
const moduleReg = process.env.MODULE_REGISTRY;

if (!factory || !presetReg || !moduleReg) {
  console.error(
    "Required env vars: AGENT_VAULT_FACTORY, PRESET_REGISTRY, MODULE_REGISTRY",
  );
  process.exit(1);
}

const chainsFile = resolve(import.meta.dirname, "../src/chains.ts");
let content = readFileSync(chainsFile, "utf-8");

// Replace the placeholder addresses for the specified chain
const chainBlockRegex = new RegExp(
  `(${chain}:\\s*\\{[\\s\\S]*?addresses:\\s*\\{[\\s\\S]*?)agentVaultFactory:\\s*"0x[0-9a-fA-F]+"`,
  "m",
);
content = content.replace(chainBlockRegex, `$1agentVaultFactory: "${factory}"`);

const presetRegex = new RegExp(
  `(${chain}:\\s*\\{[\\s\\S]*?addresses:\\s*\\{[\\s\\S]*?)presetRegistry:\\s*"0x[0-9a-fA-F]+"`,
  "m",
);
content = content.replace(presetRegex, `$1presetRegistry: "${presetReg}"`);

const moduleRegex = new RegExp(
  `(${chain}:\\s*\\{[\\s\\S]*?addresses:\\s*\\{[\\s\\S]*?)moduleRegistry:\\s*"0x[0-9a-fA-F]+"`,
  "m",
);
content = content.replace(moduleRegex, `$1moduleRegistry: "${moduleReg}"`);

writeFileSync(chainsFile, content);
console.log(`Updated ${chainsFile} for chain: ${chain}`);
console.log(`  agentVaultFactory: ${factory}`);
console.log(`  presetRegistry:    ${presetReg}`);
console.log(`  moduleRegistry:    ${moduleReg}`);
