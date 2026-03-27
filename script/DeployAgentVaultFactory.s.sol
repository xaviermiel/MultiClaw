// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/AgentVaultFactory.sol";
import "../src/DeFiInteractorModule.sol";
import "../src/PresetRegistry.sol";
import "../src/ModuleRegistry.sol";

/**
 * @title DeployAgentVaultFactory
 * @notice Deploy DeFiInteractorModule implementation + PresetRegistry + AgentVaultFactory
 *         with cross-chain address consistency via CREATE2 deterministic deployment.
 *
 * All three contracts are deployed with fixed CREATE2 salts so that a Safe at the same
 * address on different chains will get the same vault (module clone) address everywhere.
 *
 * Registry and PresetRegistry are set post-deployment via setters so that chain-specific
 * addresses don't affect the factory's CREATE2 address.
 *
 * Environment variables:
 *   - DEPLOYER_PRIVATE_KEY: Private key of deployer (must be Registry owner)
 *   - REGISTRY_ADDRESS: Address of the deployed ModuleRegistry on this chain
 *   - FACTORY_OWNER: (Optional) Owner address, defaults to deployer
 *
 * Usage:
 *   forge script script/DeployAgentVaultFactory.s.sol --rpc-url $BASE_SEPOLIA_RPC_URL --broadcast --verify
 */
contract DeployAgentVaultFactory is Script {
    // Fixed salts — changing these changes the deployed addresses everywhere
    bytes32 constant IMPL_SALT = keccak256("multiclaw.DeFiInteractorModule.v1");
    bytes32 constant PRESET_REGISTRY_SALT = keccak256("multiclaw.PresetRegistry.v1");
    bytes32 constant FACTORY_SALT = keccak256("multiclaw.AgentVaultFactory.v1");

    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);
        address registryAddress = vm.envAddress("REGISTRY_ADDRESS");
        address factoryOwner = vm.envOr("FACTORY_OWNER", deployer);

        console.log("=== Deploy AgentVaultFactory (cross-chain deterministic) ===");
        console.log("Chain ID:  ", block.chainid);
        console.log("Deployer:  ", deployer);
        console.log("Owner:     ", factoryOwner);
        console.log("Registry:  ", registryAddress);

        ModuleRegistry registry = ModuleRegistry(registryAddress);
        require(registry.owner() == deployer, "Deployer must be registry owner to authorize factory");

        vm.startBroadcast(deployerPrivateKey);

        // 1. Deploy implementation with CREATE2 — address is identical on every chain.
        //    address(1) placeholder args are ignored; proxies call initialize() instead.
        DeFiInteractorModule impl =
            new DeFiInteractorModule{salt: IMPL_SALT}(address(1), address(1), address(1));
        console.log("\n1. DeFiInteractorModule implementation:", address(impl));

        // 2. Deploy PresetRegistry with CREATE2
        PresetRegistry presetRegistry = new PresetRegistry{salt: PRESET_REGISTRY_SALT}(factoryOwner);
        console.log("2. PresetRegistry:                      ", address(presetRegistry));

        // 3. Deploy AgentVaultFactory with CREATE2.
        //    Pass address(0) for registry/presetRegistry so chain-specific addresses
        //    don't influence the factory's CREATE2 address — we set them via setters below.
        AgentVaultFactory factory =
            new AgentVaultFactory{salt: FACTORY_SALT}(factoryOwner, address(0), address(0), address(impl));
        console.log("3. AgentVaultFactory:                   ", address(factory));

        // 4. Wire up chain-specific addresses (does not affect CREATE2 address)
        factory.setRegistry(registryAddress);
        factory.setPresetRegistry(address(presetRegistry));

        // 5. Authorize the factory in the registry
        registry.authorizeFactory(address(factory));
        console.log("4. Registry + PresetRegistry wired, factory authorized");

        vm.stopBroadcast();

        // Sanity checks
        require(address(factory.registry()) == registryAddress, "Registry not set");
        require(address(factory.presetRegistry()) == address(presetRegistry), "PresetRegistry not set");
        require(registry.authorizedFactories(address(factory)), "Factory not authorized");

        console.log("\n=== Deployment Complete ===");
        console.log("DeFiInteractorModule implementation:", address(impl));
        console.log("PresetRegistry:                     ", address(presetRegistry));
        console.log("AgentVaultFactory:                  ", address(factory));
    }
}
