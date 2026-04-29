// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/AgentVaultFactory.sol";
import "../src/DeFiInteractorModule.sol";

/**
 * @title UpdateFactoryImplementation
 * @notice Deploy a new DeFiInteractorModule implementation and point AgentVaultFactory to it.
 *
 * Environment variables:
 *   - DEPLOYER_PRIVATE_KEY: Private key of the factory owner
 *   - AGENT_VAULT_FACTORY_ADDRESS: Address of the deployed AgentVaultFactory
 *
 * Usage:
 *   forge script script/UpdateFactoryImplementation.s.sol \
 *     --rpc-url $BASE_SEPOLIA_RPC_URL --broadcast --verify \
 *     --private-key $DEPLOYER_PRIVATE_KEY
 */
contract UpdateFactoryImplementation is Script {
    bytes32 constant IMPL_SALT = keccak256("multiclaw.DeFiInteractorModule.v2");

    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);
        address factoryAddress = vm.envAddress("AGENT_VAULT_FACTORY_ADDRESS");

        AgentVaultFactory factory = AgentVaultFactory(factoryAddress);

        console.log("=== Update AgentVaultFactory Implementation ===");
        console.log("Factory:        ", factoryAddress);
        console.log("Factory owner:  ", factory.owner());
        console.log("Deployer:       ", deployer);
        console.log("Old impl:       ", factory.implementation());
        require(factory.owner() == deployer, "Deployer is not factory owner");

        vm.startBroadcast(deployerPrivateKey);

        // Deploy new implementation — placeholder args, clones call initialize() instead
        DeFiInteractorModule newImpl =
            new DeFiInteractorModule{salt: IMPL_SALT}(address(1), address(1), address(1));
        console.log("\nNew implementation deployed:", address(newImpl));

        // Point factory to new implementation
        factory.setImplementation(address(newImpl));
        console.log("Factory implementation updated.");

        vm.stopBroadcast();

        require(factory.implementation() == address(newImpl), "Implementation not updated");
        console.log("\n=== Done ===");
        console.log("New impl:       ", address(newImpl));
    }
}
