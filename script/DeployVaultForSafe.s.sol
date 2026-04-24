// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/AgentVaultFactory.sol";

/**
 * @title DeployVaultForSafe
 * @notice Deploy a DeFiInteractorModule vault for a given Safe + agent using a preset.
 *
 * Usage:
 *   forge script script/DeployVaultForSafe.s.sol --rpc-url $BASE_SEPOLIA_RPC_URL --broadcast
 */
contract DeployVaultForSafe is Script {
    address constant FACTORY = 0x83CaA00d363aCA3cb68274D991Eb1f6B226F70FF;
    address constant ORACLE = 0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E;

    address constant SAFE = 0x6c9410Fcdedda7a0dA572eB613b1ad5372592BB7;
    address constant AGENT = 0xf6808f2c2A5BE8410D921a81b4Ef4d3Ff83d4E2b;

    uint256 constant PRESET_ID = 0; // DeFi Trader

    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");

        AgentVaultFactory factory = AgentVaultFactory(FACTORY);

        address predicted = factory.computeModuleAddress(SAFE);
        console.log("Predicted module address:", predicted);

        address[] memory priceFeedTokens = new address[](0);
        address[] memory priceFeedAddresses = new address[](0);
        // Only consumed when the preset enables recipient whitelisting (e.g. Payment Agent).
        // For DeFi Trader / Yield Farmer presets, leave empty.
        address[] memory allowedRecipients = new address[](0);

        vm.startBroadcast(deployerPrivateKey);

        address module = factory.deployVaultFromPreset(
            SAFE, ORACLE, AGENT, PRESET_ID, priceFeedTokens, priceFeedAddresses, allowedRecipients
        );

        vm.stopBroadcast();

        console.log("Module deployed at:", module);
        require(module == predicted, "Address mismatch");
    }
}
