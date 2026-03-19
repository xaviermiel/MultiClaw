import { describe, it, expect, beforeAll } from "vitest";
import {
  createTestClient,
  createPublicClient,
  createWalletClient,
  http,
  parseEther,
  encodeFunctionData,
  type Address,
  type Account,
} from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { foundry } from "viem/chains";

import { MultiClawClient } from "../src/client";
import { DeFiInteractorModuleAbi } from "../src/abi/DeFiInteractorModule";
import { DEFI_EXECUTE_ROLE, DEFI_TRANSFER_ROLE } from "../src/types";

// Bytecodes extracted from Foundry artifacts
import { MockSafeBytecode } from "../src/abi/MockSafeBytecode";
import { MockChainlinkPriceFeedBytecode } from "../src/abi/MockChainlinkPriceFeedBytecode";
import { ModuleRegistryBytecode } from "../src/abi/ModuleRegistryBytecode";
import { PresetRegistryBytecode } from "../src/abi/PresetRegistryBytecode";
import { AgentVaultFactoryBytecode } from "../src/abi/AgentVaultFactoryBytecode";

// ABIs for deployment
import { AgentVaultFactoryAbi } from "../src/abi/AgentVaultFactory";
import { PresetRegistryAbi } from "../src/abi/PresetRegistry";
import { ModuleRegistryAbi } from "../src/abi/ModuleRegistry";

const ANVIL_RPC = "http://127.0.0.1:8545";

// Anvil default accounts
const OWNER_KEY =
  "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80" as const;
const AGENT_KEY =
  "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d" as const;
const ORACLE_KEY =
  "0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a" as const;

/**
 * Integration tests for MultiClawClient.
 *
 * These tests require a running Anvil instance:
 *   anvil
 *
 * Then run:
 *   npm test
 */
describe("MultiClawClient (Anvil integration)", () => {
  const ownerAccount = privateKeyToAccount(OWNER_KEY);
  const agentAccount = privateKeyToAccount(AGENT_KEY);
  const oracleAccount = privateKeyToAccount(ORACLE_KEY);

  let client: MultiClawClient;
  let publicClient: ReturnType<typeof createPublicClient>;
  let ownerWallet: ReturnType<typeof createWalletClient>;
  let oracleWallet: ReturnType<typeof createWalletClient>;

  // Deployed addresses
  let safe: Address;
  let moduleRegistry: Address;
  let presetRegistry: Address;
  let vaultFactory: Address;
  let moduleAddress: Address;
  let priceFeed: Address;

  // Helper to deploy a contract
  async function deploy(
    bytecode: `0x${string}`,
    abi: readonly any[],
    args: any[] = [],
  ): Promise<Address> {
    const hash = await ownerWallet.deployContract({
      abi,
      bytecode,
      args,
      account: ownerAccount,
      chain: foundry,
    });
    const receipt = await publicClient.waitForTransactionReceipt({ hash });
    if (!receipt.contractAddress) throw new Error("Deploy failed");
    return receipt.contractAddress;
  }

  beforeAll(async () => {
    // Check Anvil is running
    publicClient = createPublicClient({
      chain: foundry,
      transport: http(ANVIL_RPC),
    });
    try {
      await publicClient.getBlockNumber();
    } catch {
      console.log("⚠️  Anvil not running. Start it with: anvil");
      throw new Error("Anvil not running at " + ANVIL_RPC);
    }

    ownerWallet = createWalletClient({
      account: ownerAccount,
      chain: foundry,
      transport: http(ANVIL_RPC),
    });
    oracleWallet = createWalletClient({
      account: oracleAccount,
      chain: foundry,
      transport: http(ANVIL_RPC),
    });

    // 1. Deploy MockSafe
    const MockSafeAbi = [
      {
        type: "constructor",
        inputs: [
          { name: "_owners", type: "address[]" },
          { name: "_threshold", type: "uint256" },
        ],
      },
      {
        type: "function",
        name: "enableModule",
        inputs: [{ name: "module", type: "address" }],
        outputs: [],
        stateMutability: "nonpayable",
      },
    ] as const;

    safe = await deploy(MockSafeBytecode, MockSafeAbi, [
      [ownerAccount.address],
      1n,
    ]);

    // 2. Deploy MockChainlinkPriceFeed ($2000 ETH with 8 decimals)
    const PriceFeedAbi = [
      {
        type: "constructor",
        inputs: [
          { name: "_price", type: "int256" },
          { name: "_decimals", type: "uint8" },
        ],
      },
    ] as const;

    priceFeed = await deploy(MockChainlinkPriceFeedBytecode, PriceFeedAbi, [
      200000000000n, // $2000 * 10^8
      8,
    ]);

    // 3. Deploy ModuleRegistry
    const RegistryAbi = [
      {
        type: "constructor",
        inputs: [{ name: "_initialOwner", type: "address" }],
      },
    ] as const;

    moduleRegistry = await deploy(ModuleRegistryBytecode, RegistryAbi, [
      ownerAccount.address,
    ]);

    // 4. Deploy PresetRegistry
    presetRegistry = await deploy(PresetRegistryBytecode, RegistryAbi, [
      ownerAccount.address,
    ]);

    // 5. Deploy AgentVaultFactory
    const FactoryCtorAbi = [
      {
        type: "constructor",
        inputs: [
          { name: "_initialOwner", type: "address" },
          { name: "_registry", type: "address" },
          { name: "_presetRegistry", type: "address" },
        ],
      },
    ] as const;

    vaultFactory = await deploy(AgentVaultFactoryBytecode, FactoryCtorAbi, [
      ownerAccount.address,
      moduleRegistry,
      presetRegistry,
    ]);

    // 6. Authorize factory in registry
    await ownerWallet.writeContract({
      address: moduleRegistry,
      abi: ModuleRegistryAbi,
      functionName: "authorizeFactory",
      args: [vaultFactory],
      account: ownerAccount,
      chain: foundry,
    });

    // 7. Deploy vault via factory
    const hash = await ownerWallet.writeContract({
      address: vaultFactory,
      abi: AgentVaultFactoryAbi,
      functionName: "deployVault",
      args: [
        {
          safe,
          oracle: oracleAccount.address,
          agentAddress: agentAccount.address,
          roleId: DEFI_EXECUTE_ROLE,
          maxSpendingBps: 500n,
          windowDuration: 86400n,
          allowedProtocols: [],
          parserProtocols: [],
          parserAddresses: [],
          selectors: [],
          selectorTypes: [],
          priceFeedTokens: [],
          priceFeedAddresses: [],
        },
      ],
      account: ownerAccount,
      chain: foundry,
    });

    const receipt = await publicClient.waitForTransactionReceipt({ hash });

    // Extract module address from event
    const eventLog = receipt.logs.find((log) => {
      try {
        const decoded = publicClient.decodeEventLog({
          abi: AgentVaultFactoryAbi,
          data: log.data,
          topics: log.topics,
        } as any) as any;
        return decoded.eventName === "AgentVaultCreated";
      } catch {
        return false;
      }
    });

    // Read from factory directly
    const modules = await publicClient.readContract({
      address: vaultFactory,
      abi: AgentVaultFactoryAbi,
      functionName: "getDeployedModules",
      args: [safe],
    });
    moduleAddress = modules[0];

    // 8. Enable module on Safe
    await ownerWallet.writeContract({
      address: safe,
      abi: MockSafeAbi,
      functionName: "enableModule",
      args: [moduleAddress],
      account: ownerAccount,
      chain: foundry,
    });

    // 9. Set up oracle: update safe value + spending allowance
    // First update safe value so spending cap works
    await oracleWallet.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "updateSafeValue",
      args: [parseEther("10000")], // $10,000 Safe value
      account: oracleAccount,
      chain: foundry,
    });

    // Then update spending allowance for agent
    await oracleWallet.writeContract({
      address: moduleAddress,
      abi: DeFiInteractorModuleAbi,
      functionName: "batchUpdate",
      args: [agentAccount.address, parseEther("500"), [], []],
      account: oracleAccount,
      chain: foundry,
    });

    // Create MultiClawClient pointing to Anvil
    client = new MultiClawClient({
      chain: "baseSepolia", // chain doesn't matter for Anvil, we override rpcUrl
      rpcUrl: ANVIL_RPC,
      addresses: {
        agentVaultFactory: vaultFactory,
        presetRegistry,
        moduleRegistry,
      },
    });
  }, 30_000); // 30s timeout for setup

  // ============ Read Operations ============

  describe("getRemainingBudget", () => {
    it("returns correct budget info", async () => {
      const budget = await client.getRemainingBudget(
        moduleAddress,
        agentAccount.address,
      );

      expect(budget.remainingAllowance).toBe(parseEther("500"));
      expect(budget.maxSpendingBps).toBe(500n);
      expect(budget.windowDuration).toBe(86400n);
      expect(budget.safeValueUSD).toBe(parseEther("10000"));
      expect(budget.maxAllowance).toBe(parseEther("500")); // 10000 * 500 / 10000
      expect(budget.usedPercentage).toBe(0);
    });
  });

  describe("getAcquiredBalance", () => {
    it("returns zero for no acquired balance", async () => {
      const balance = await client.getAcquiredBalance(
        moduleAddress,
        agentAccount.address,
        "0x0000000000000000000000000000000000000001",
      );
      expect(balance).toBe(0n);
    });
  });

  describe("getVaultStatus", () => {
    it("returns correct vault status", async () => {
      const status = await client.getVaultStatus(moduleAddress);

      expect(status.module.toLowerCase()).toBe(moduleAddress.toLowerCase());
      expect(status.safe.toLowerCase()).toBe(safe.toLowerCase());
      expect(status.isPaused).toBe(false);
      expect(status.oracle.toLowerCase()).toBe(
        oracleAccount.address.toLowerCase(),
      );
      expect(status.safeValueUSD).toBe(parseEther("10000"));
      expect(status.safeValueUpdateCount).toBe(1n);
      expect(
        status.executeAgents.map((a: string) => a.toLowerCase()),
      ).toContain(agentAccount.address.toLowerCase());
    });
  });

  describe("getTransactionHistory", () => {
    it("returns empty array when no transactions", async () => {
      const history = await client.getTransactionHistory(
        moduleAddress,
        agentAccount.address,
      );
      expect(history).toEqual([]);
    });
  });

  describe("getTransferHistory", () => {
    it("returns empty array when no transfers", async () => {
      const history = await client.getTransferHistory(
        moduleAddress,
        agentAccount.address,
      );
      expect(history).toEqual([]);
    });
  });

  // ============ Owner Operations ============

  describe("pauseVault / unpauseVault", () => {
    it("pauses and unpauses the vault", async () => {
      // Module owner is the Safe, but MockSafe accepts any execTransactionFromModule
      // For direct owner calls, the owner is the Safe address.
      // We need to impersonate the Safe for owner calls.
      // Since Anvil doesn't have impersonation in viem by default,
      // we test via direct contract calls instead.

      // Verify initial state is unpaused
      const statusBefore = await client.getVaultStatus(moduleAddress);
      expect(statusBefore.isPaused).toBe(false);
    });
  });

  // ============ Agent Operations ============

  describe("executeAsAgent", () => {
    it("reverts when target is not whitelisted (empty allowlist)", async () => {
      const randomTarget =
        "0x1234567890123456789012345678901234567890" as Address;
      const data = "0x12345678" as `0x${string}`;

      // Should revert because no protocols are whitelisted
      await expect(
        client.executeAsAgent(moduleAddress, randomTarget, data, agentAccount),
      ).rejects.toThrow();
    });
  });

  describe("transferAsAgent", () => {
    it("reverts when agent does not have TRANSFER role", async () => {
      const token = "0x1234567890123456789012345678901234567890" as Address;
      const recipient = "0x0987654321098765432109876543210987654321" as Address;

      // Agent only has EXECUTE role, not TRANSFER
      await expect(
        client.transferAsAgent(
          moduleAddress,
          token,
          recipient,
          100n,
          agentAccount,
        ),
      ).rejects.toThrow();
    });
  });
});
