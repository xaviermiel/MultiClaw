// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import {AaveV3Parser} from "../src/parsers/AaveV3Parser.sol";
import {UniswapV3Parser} from "../src/parsers/UniswapV3Parser.sol";
import {UniswapV4Parser} from "../src/parsers/UniswapV4Parser.sol";
import {UniversalRouterParser} from "../src/parsers/UniversalRouterParser.sol";
import {MorphoBlueParser} from "../src/parsers/MorphoBlueParser.sol";
import {MorphoParser} from "../src/parsers/MorphoParser.sol";
import {MerklParser} from "../src/parsers/MerklParser.sol";
import {OneInchParser} from "../src/parsers/OneInchParser.sol";
import {ParaswapParser} from "../src/parsers/ParaswapParser.sol";
import {KyberSwapParser} from "../src/parsers/KyberSwapParser.sol";

/**
 * @title DeployParsersBase
 * @notice Deploys the 10 stateless calldata parsers standalone (no module/Safe wiring).
 * @dev In the AgentVaultFactory clone model, parsers are not registered to a single
 *      module here. Instead their addresses are stored in PresetRegistry templates
 *      (see CreatePresets.s.sol) and copied into each freshly-cloned module at vault
 *      creation. Parsers are pure decoders with no constructor args and no state, so a
 *      single deployment per chain is reused by every vault.
 *
 *      The 4 parsers referenced by the standard presets are deployed first and their
 *      addresses printed as ready-to-paste env exports for CreatePresets.s.sol. The
 *      remaining 6 give full protocol coverage for custom / future presets.
 *
 * Key selection:
 *   - chainid 8453 (Base mainnet) -> MAINNET_DEPLOYER_PK
 *   - any other chain             -> DEPLOYER_PRIVATE_KEY
 *
 * Usage:
 *   forge script script/DeployParsersBase.s.sol --rpc-url $BASE_RPC_URL --broadcast --verify -vvvv
 */
contract DeployParsersBase is Script {
    uint256 constant CHAIN_BASE_MAINNET = 8453;

    function _deployerKey() internal view returns (uint256) {
        if (block.chainid == CHAIN_BASE_MAINNET) return vm.envUint("MAINNET_DEPLOYER_PK");
        return vm.envUint("DEPLOYER_PRIVATE_KEY");
    }

    function run() external {
        uint256 deployerKey = _deployerKey();

        console.log("=== Deploy Parsers ===");
        console.log("Chain ID:", block.chainid);
        console.log("Deployer:", vm.addr(deployerKey));
        console.log("");

        vm.startBroadcast(deployerKey);

        // ── Preset-referenced parsers (required by CreatePresets.s.sol) ──────────
        AaveV3Parser aaveParser = new AaveV3Parser();
        UniswapV3Parser uniV3Parser = new UniswapV3Parser();
        UniversalRouterParser universalParser = new UniversalRouterParser();
        MorphoBlueParser morphoBlueParser = new MorphoBlueParser();

        // ── Extra protocol coverage (custom / future presets) ────────────────────
        UniswapV4Parser uniV4Parser = new UniswapV4Parser();
        MorphoParser morphoParser = new MorphoParser();
        MerklParser merklParser = new MerklParser();
        OneInchParser oneInchParser = new OneInchParser();
        ParaswapParser paraswapParser = new ParaswapParser();
        KyberSwapParser kyberSwapParser = new KyberSwapParser();

        vm.stopBroadcast();

        console.log("--- Preset-referenced parsers ---");
        console.log("AaveV3Parser:         ", address(aaveParser));
        console.log("UniswapV3Parser:      ", address(uniV3Parser));
        console.log("UniversalRouterParser:", address(universalParser));
        console.log("MorphoBlueParser:     ", address(morphoBlueParser));
        console.log("");
        console.log("--- Extra coverage ---");
        console.log("UniswapV4Parser:      ", address(uniV4Parser));
        console.log("MorphoParser:         ", address(morphoParser));
        console.log("MerklParser:          ", address(merklParser));
        console.log("OneInchParser:        ", address(oneInchParser));
        console.log("ParaswapParser:       ", address(paraswapParser));
        console.log("KyberSwapParser:      ", address(kyberSwapParser));
        console.log("");
        console.log("=== Paste these into .env before running CreatePresets.s.sol ===");
        console.log(string.concat("export AAVE_PARSER_ADDRESS=", vm.toString(address(aaveParser))));
        console.log(string.concat("export UNISWAP_V3_PARSER_ADDRESS=", vm.toString(address(uniV3Parser))));
        console.log(string.concat("export UNIVERSAL_PARSER_ADDRESS=", vm.toString(address(universalParser))));
        console.log(string.concat("export MORPHO_BLUE_PARSER_ADDRESS=", vm.toString(address(morphoBlueParser))));
    }
}
