// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/AgentVaultFactory.sol";
import "../src/ModuleRegistry.sol";

/**
 * @title DeployAgentVaultFactory
 * @notice Deploy the AgentVaultFactory and authorize it in the Registry
 *
 * Environment variables:
 *   - DEPLOYER_PRIVATE_KEY: Private key of deployer (must be Registry owner)
 *   - REGISTRY_ADDRESS: Address of the deployed ModuleRegistry
 *   - FACTORY_OWNER: (Optional) Owner address, defaults to deployer
 *
 * Usage:
 *   REGISTRY_ADDRESS=0x... \
 *   forge script script/DeployAgentVaultFactory.s.sol --rpc-url $RPC_URL --broadcast --private-key $DEPLOYER_PRIVATE_KEY
 */
contract DeployAgentVaultFactory is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);
        address registryAddress = vm.envAddress("REGISTRY_ADDRESS");
        address factoryOwner = vm.envOr("FACTORY_OWNER", deployer);

        console.log("=== Deploy AgentVaultFactory ===");
        console.log("Chain ID:", block.chainid);
        console.log("Deployer:", deployer);
        console.log("Owner:", factoryOwner);
        console.log("Registry:", registryAddress);

        ModuleRegistry registry = ModuleRegistry(registryAddress);

        // Verify deployer is registry owner
        require(registry.owner() == deployer, "Deployer must be registry owner to authorize factory");

        vm.startBroadcast(deployerPrivateKey);

        // 1. Deploy the factory
        AgentVaultFactory factory = new AgentVaultFactory(factoryOwner, registryAddress);
        console.log("\n1. AgentVaultFactory deployed at:", address(factory));

        // 2. Authorize the factory in the registry
        registry.authorizeFactory(address(factory));
        console.log("2. Factory authorized in registry");

        vm.stopBroadcast();

        // Verify
        require(registry.authorizedFactories(address(factory)), "Factory not authorized");

        console.log("\n=== Deployment Complete ===");
        console.log("AgentVaultFactory:", address(factory));
        console.log("\nNext steps:");
        console.log("1. Create presets with factory.createPreset(...)");
        console.log("2. Deploy vaults with factory.deployVault(...) or factory.deployVaultFromPreset(...)");
        console.log("3. Users must enable the module on their Safe via Safe multisig tx");
    }
}
