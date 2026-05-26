// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/ModuleRegistry.sol";

/**
 * @title DeployRegistry
 * @notice Deploy ModuleRegistry contract
 * @dev The registry tracks all deployed DeFiInteractorModules
 *
 * Environment variables:
 *   - DEPLOYER_PRIVATE_KEY: Private key of deployer (MultiClaw team EOA)
 *   - REGISTRY_OWNER: (Optional) Owner address, defaults to deployer
 *
 * Usage:
 *   forge script script/DeployRegistry.s.sol --rpc-url $RPC_URL --broadcast --private-key $DEPLOYER_PRIVATE_KEY
 */
contract DeployRegistry is Script {
    /// @dev Mainnet broadcasts use a dedicated deployer key; other chains keep the default.
    function _deployerKey() internal view returns (uint256) {
        if (block.chainid == 8453) return vm.envUint("MAINNET_DEPLOYER_PK");
        return vm.envUint("DEPLOYER_PRIVATE_KEY");
    }

    function run() external {
        uint256 deployerPrivateKey = _deployerKey();
        address deployer = vm.addr(deployerPrivateKey);
        address owner = vm.envOr("REGISTRY_OWNER", deployer);

        console.log("=== Deploy ModuleRegistry ===");
        console.log("Chain ID:", block.chainid);
        console.log("Deployer:", deployer);
        console.log("Owner:", owner);

        vm.startBroadcast(deployerPrivateKey);

        ModuleRegistry registry = new ModuleRegistry(owner);

        vm.stopBroadcast();

        console.log("\n=== Deployment Complete ===");
        console.log("ModuleRegistry:", address(registry));
        console.log("\nNext steps:");
        console.log("1. Run DeployFactory.s.sol with REGISTRY_ADDRESS set to:", address(registry));
    }
}
