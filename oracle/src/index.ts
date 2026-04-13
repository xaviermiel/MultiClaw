/**
 * Local Oracle Entry Point
 *
 * Runs both safe-value and spending-oracle locally.
 *
 * Usage:
 *   npm start           - Run both oracles
 *   npm run safe-value  - Run only safe-value oracle
 *   npm run spending-oracle - Run only spending oracle
 */

import { startCron as startSafeValue } from "./safe-value.js";
import { start as startSpendingOracle } from "./spending-oracle.js";
import { config, validateConfig } from "./config.js";

function main() {
  console.log("===========================================");
  console.log("  MultiClaw Local Oracle");
  console.log("===========================================");
  console.log("");

  try {
    validateConfig();
  } catch (error) {
    console.error("Configuration error:", error);
    console.error("");
    console.error("Please copy .env.example to .env and configure:");
    console.error("  - PRIVATE_KEY: Private key of the authorized updater");
    console.error(
      "  - REGISTRY_ADDRESS: ModuleRegistry address for shared multi-module mode",
    );
    console.error(
      "    or MODULE_ADDRESS: DeFiInteractorModule contract address for single-module mode",
    );
    console.error("  - RPC_URL: Ethereum RPC URL");
    process.exit(1);
  }

  console.log(`Chain: ${config.chainName}`);
  console.log(`RPC URL: ${config.rpcUrl}`);
  if (config.registryAddress) {
    console.log(`Registry mode: ${config.registryAddress}`);
  } else {
    console.log(`Single-module mode: ${config.moduleAddress}`);
  }
  console.log("");

  console.log("Starting Safe Value Oracle...");
  startSafeValue();

  console.log("");
  console.log("Starting Spending Oracle...");
  startSpendingOracle();

  console.log("");
  console.log("Both oracles are now running.");
  console.log("Press Ctrl+C to stop.");
}

main();
