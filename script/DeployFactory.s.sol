// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/ModuleFactory.sol";
import "../src/ModuleRegistry.sol";

/**
 * @title DeployFactory
 * @notice Deploy ModuleFactory and authorize it in the Registry
 * @dev The factory deploys DeFiInteractorModules with deterministic CREATE2 addresses
 *
 * Environment variables:
 *   - DEPLOYER_PRIVATE_KEY: Private key of deployer (MultiClaw team EOA, must be Registry owner)
 *   - REGISTRY_ADDRESS: Address of the deployed ModuleRegistry
 *   - FACTORY_OWNER: (Optional) Owner address, defaults to deployer
 *   - AUTO_REGISTER: (Optional) Whether to auto-register modules, defaults to true
 *
 * Usage:
 *   REGISTRY_ADDRESS=0x... \
 *   forge script script/DeployFactory.s.sol --rpc-url $RPC_URL --broadcast --private-key $DEPLOYER_PRIVATE_KEY
 */
contract DeployFactory is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);
        address registryAddress = vm.envAddress("REGISTRY_ADDRESS");
        address owner = vm.envOr("FACTORY_OWNER", deployer);
        bool autoRegister = vm.envOr("AUTO_REGISTER", true);

        console.log("=== Deploy ModuleFactory ===");
        console.log("Chain ID:", block.chainid);
        console.log("Deployer:", deployer);
        console.log("Owner:", owner);
        console.log("Registry:", registryAddress);
        console.log("Auto-register:", autoRegister);

        ModuleRegistry registry = ModuleRegistry(registryAddress);

        // Verify deployer is registry owner
        require(registry.owner() == deployer, "Deployer must be registry owner to authorize factory");

        vm.startBroadcast(deployerPrivateKey);

        // 1. Deploy the factory
        ModuleFactory factory = new ModuleFactory(owner, registryAddress, autoRegister);
        console.log("\n1. Factory deployed at:", address(factory));

        // 2. Authorize the factory in the registry
        registry.authorizeFactory(address(factory));
        console.log("2. Factory authorized in registry");

        vm.stopBroadcast();

        // Verify
        require(registry.authorizedFactories(address(factory)), "Factory not authorized");

        console.log("\n=== Deployment Complete ===");
        console.log("ModuleFactory:", address(factory));
        console.log("\nNext steps:");
        console.log("1. Run DeployModuleViaFactory.s.sol to deploy modules for Safes");
        console.log("2. Update CRE config with registryAddress:", registryAddress);
    }
}
