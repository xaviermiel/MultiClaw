// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/AgentVaultFactory.sol";
import "../src/DeFiInteractorModule.sol";
import "../src/ModuleRegistry.sol";

/**
 * @title RedeployAgentVaultFactory
 * @notice Redeploy AgentVaultFactory with the updated VaultConfig struct (includes recipient whitelist).
 *         The old factory (v1 salt) stays in place but is deauthorized from the registry.
 *         The new factory (v2 salt) is authorized.
 *
 * Environment variables:
 *   - DEPLOYER_PRIVATE_KEY: Private key of the deployer (must be registry owner + factory owner)
 *   - REGISTRY_ADDRESS:             ModuleRegistry address
 *   - PRESET_REGISTRY_ADDRESS:      PresetRegistry address
 *   - AGENT_VAULT_FACTORY_ADDRESS:  Old factory address (to deauthorize)
 *
 * Usage:
 *   forge script script/RedeployAgentVaultFactory.s.sol \
 *     --rpc-url $BASE_SEPOLIA_RPC_URL --broadcast --verify \
 *     --private-key $DEPLOYER_PRIVATE_KEY
 */
contract RedeployAgentVaultFactory is Script {
    // v2 salts — keep v1 salts unchanged so old addresses are unaffected
    bytes32 constant FACTORY_SALT = keccak256("multiclaw.AgentVaultFactory.v2");

    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);
        address registryAddress = vm.envAddress("REGISTRY_ADDRESS");
        address presetRegistryAddress = vm.envAddress("PRESET_REGISTRY_ADDRESS");
        address oldFactoryAddress = vm.envAddress("AGENT_VAULT_FACTORY_ADDRESS");
        // The new implementation we deployed in UpdateFactoryImplementation.s.sol
        address implAddress = AgentVaultFactory(oldFactoryAddress).implementation();

        ModuleRegistry registry = ModuleRegistry(registryAddress);

        console.log("=== Redeploy AgentVaultFactory (v2 - recipient whitelist ABI) ===");
        console.log("Chain ID:         ", block.chainid);
        console.log("Deployer:         ", deployer);
        console.log("Registry:         ", registryAddress);
        console.log("PresetRegistry:   ", presetRegistryAddress);
        console.log("Implementation:   ", implAddress);
        console.log("Old factory:      ", oldFactoryAddress);
        require(registry.owner() == deployer, "Deployer must be registry owner");

        vm.startBroadcast(deployerPrivateKey);

        // 1. Deploy new factory with v2 salt
        AgentVaultFactory newFactory = new AgentVaultFactory{salt: FACTORY_SALT}(
            deployer, address(0), address(0), implAddress
        );
        console.log("\n1. New AgentVaultFactory deployed:", address(newFactory));

        // 2. Wire up registry and preset registry
        newFactory.setRegistry(registryAddress);
        newFactory.setPresetRegistry(presetRegistryAddress);
        console.log("2. Registry + PresetRegistry wired");

        // 3. Authorize new factory in registry
        registry.authorizeFactory(address(newFactory));
        console.log("3. New factory authorized in registry");

        // 4. Deauthorize old factory
        registry.deauthorizeFactory(oldFactoryAddress);
        console.log("4. Old factory deauthorized");

        vm.stopBroadcast();

        // Sanity checks
        require(address(newFactory.registry()) == registryAddress, "Registry not set");
        require(address(newFactory.presetRegistry()) == presetRegistryAddress, "PresetRegistry not set");
        require(newFactory.implementation() == implAddress, "Implementation not set");
        require(registry.authorizedFactories(address(newFactory)), "New factory not authorized");
        require(!registry.authorizedFactories(oldFactoryAddress), "Old factory still authorized");

        console.log("\n=== Done ===");
        console.log("New AgentVaultFactory:", address(newFactory));
        console.log("\nUpdate AGENT_VAULT_FACTORY_ADDRESS in .env and oracle config to:", address(newFactory));
    }
}
