// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/parsers/AaveV3Parser.sol";
import "../src/parsers/UniswapV3Parser.sol";
import "../src/parsers/UniswapV4Parser.sol";
import "../src/parsers/UniversalRouterParser.sol";
import "./utils/SafeTxHelper.sol";

/**
 * @title RepairBaseSepoliaParsers
 * @notice Deploys and binds the core Base Sepolia parsers for a live module.
 * @dev This is a targeted repair script for partially configured modules where
 *      execution reverts because parser registrations are missing on-chain.
 */
contract RepairBaseSepoliaParsers is Script, SafeTxHelper {
    address constant AAVE_V3_POOL = 0x8bAB6d1b75f19e9eD9fCe8b9BD338844fF79aE27;
    address constant AAVE_V3_REWARDS = 0x71B448405c803A3982aBa448133133D2DEAFBE5F;
    address constant UNISWAP_V3_SWAP_ROUTER_02 = 0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4;
    address constant NONFUNGIBLE_POSITION_MANAGER = 0x27F971cb582BF9E50F397e4d29a5C7A34f11faA2;
    address constant UNISWAP_V4_POSITION_MANAGER = 0x4B2C77d209D3405F41a037Ec6c77F7F5b8e2ca80;
    address constant UNIVERSAL_ROUTER = 0x492E6456D9528771018DeB9E87ef7750EF184104;

    function run() external {
        if (block.chainid != 84532) revert("This repair script is for Base Sepolia only");

        address safe = vm.envAddress("SAFE_ADDRESS");
        address module = vm.envAddress("DEFI_MODULE_ADDRESS");
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");

        console.log("=== Repair Base Sepolia Parsers ===");
        console.log("Safe:", safe);
        console.log("Module:", module);

        vm.startBroadcast(deployerPrivateKey);

        AaveV3Parser aaveParser = new AaveV3Parser();
        UniswapV3Parser uniV3Parser = new UniswapV3Parser();
        UniswapV4Parser uniV4Parser = new UniswapV4Parser();
        UniversalRouterParser universalParser = new UniversalRouterParser();

        console.log("AaveV3Parser:", address(aaveParser));
        console.log("UniswapV3Parser:", address(uniV3Parser));
        console.log("UniswapV4Parser:", address(uniV4Parser));
        console.log("UniversalRouterParser:", address(universalParser));

        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", AAVE_V3_POOL, address(aaveParser)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", AAVE_V3_REWARDS, address(aaveParser)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", UNISWAP_V3_SWAP_ROUTER_02, address(uniV3Parser)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature(
                "registerParser(address,address)", NONFUNGIBLE_POSITION_MANAGER, address(uniV3Parser)
            ),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature(
                "registerParser(address,address)", UNISWAP_V4_POSITION_MANAGER, address(uniV4Parser)
            ),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", UNIVERSAL_ROUTER, address(universalParser)),
            deployerPrivateKey
        );

        vm.stopBroadcast();

        console.log("=== Base Sepolia Parser Repair Complete ===");
    }
}
