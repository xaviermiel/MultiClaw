// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/PresetRegistry.sol";

/**
 * @title CreatePresets
 * @notice Populates the PresetRegistry with the 3 standard presets expected by the interface.
 *
 * Preset IDs (0-indexed, order of creation):
 *   0 — DeFi Trader    (Uniswap V3, Universal Router, Aave V3)
 *   1 — Yield Farmer   (Aave V3 supply/withdraw/repay)
 *   2 — Payment Agent  (transfer only, no DeFi)
 *
 * Parser addresses are supplied via environment variables so presets do not
 * silently point at stale parser deployments.
 *
 * Usage:
 *   forge script script/CreatePresets.s.sol --rpc-url $BASE_SEPOLIA_RPC_URL --broadcast
 */
contract CreatePresets is Script {
    // ── Protocol addresses (Base Sepolia) ────────────────────────────────────
    address constant AAVE_V3_POOL        = 0x8bAB6d1b75f19e9eD9fCe8b9BD338844fF79aE27;
    address constant AAVE_V3_REWARDS     = 0x71B448405c803A3982aBa448133133D2DEAFBE5F;
    address constant UNISWAP_V3_ROUTER   = 0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4;
    address constant UNIVERSAL_ROUTER    = 0x492E6456D9528771018DeB9E87ef7750EF184104;
    address constant MORPHO_BLUE         = 0xBBBBBbbBBb9cC5e90e3b3Af64bdAF62C37EEFFCb;

    // ── Role IDs ─────────────────────────────────────────────────────────────
    uint16 constant DEFI_EXECUTE_ROLE  = 1;
    uint16 constant DEFI_TRANSFER_ROLE = 2;

    // ── Selector operation types ──────────────────────────────────────────────
    // UNKNOWN=0, SWAP=1, DEPOSIT=2, WITHDRAW=3, CLAIM=4, APPROVE=5, REPAY=6
    uint8 constant SWAP    = 1;
    uint8 constant DEPOSIT = 2;
    uint8 constant WITHDRAW = 3;
    uint8 constant CLAIM   = 4;
    uint8 constant APPROVE = 5;
    uint8 constant REPAY   = 6;

    function run() external {
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address presetRegistryAddress = vm.envAddress("PRESET_REGISTRY_ADDRESS");
        address aaveParser = vm.envAddress("AAVE_PARSER_ADDRESS");
        address uniswapV3Parser = vm.envAddress("UNISWAP_V3_PARSER_ADDRESS");
        address universalParser = vm.envAddress("UNIVERSAL_PARSER_ADDRESS");
        address morphoBlueParser = vm.envAddress("MORPHO_BLUE_PARSER_ADDRESS");

        vm.startBroadcast(deployerPrivateKey);

        PresetRegistry registry = PresetRegistry(presetRegistryAddress);

        // ── Preset 0: DeFi Trader ─────────────────────────────────────────────
        {
            address[] memory protocols = new address[](4);
            protocols[0] = AAVE_V3_POOL;
            protocols[1] = AAVE_V3_REWARDS;
            protocols[2] = UNISWAP_V3_ROUTER;
            protocols[3] = UNIVERSAL_ROUTER;

            address[] memory parserProtocols = new address[](4);
            parserProtocols[0] = AAVE_V3_POOL;
            parserProtocols[1] = AAVE_V3_REWARDS;
            parserProtocols[2] = UNISWAP_V3_ROUTER;
            parserProtocols[3] = UNIVERSAL_ROUTER;

            address[] memory parserAddresses = new address[](4);
            parserAddresses[0] = aaveParser;
            parserAddresses[1] = aaveParser;
            parserAddresses[2] = uniswapV3Parser;
            parserAddresses[3] = universalParser;

            bytes4[] memory selectors = new bytes4[](11);
            selectors[0]  = 0x095ea7b3; // ERC20 approve
            selectors[1]  = 0x617ba037; // Aave supply      -> DEPOSIT
            selectors[2]  = 0x69328dec; // Aave withdraw     -> WITHDRAW
            selectors[3]  = 0xa415bcad; // Aave borrow       -> WITHDRAW
            selectors[4]  = 0x573ade81; // Aave repay        -> REPAY
            selectors[5]  = 0x236300dc; // claimRewards      -> CLAIM
            selectors[6]  = 0xbb492bf5; // claimAllRewards   -> CLAIM
            selectors[7]  = 0x04e45aaf; // exactInputSingle  -> SWAP
            selectors[8]  = 0xb858183f; // exactInput        -> SWAP
            selectors[9]  = 0x5023b4df; // exactOutputSingle -> SWAP
            selectors[10] = 0x3593564c; // Universal execute -> SWAP

            uint8[] memory selectorTypes = new uint8[](11);
            selectorTypes[0]  = APPROVE;
            selectorTypes[1]  = DEPOSIT;
            selectorTypes[2]  = WITHDRAW;
            selectorTypes[3]  = WITHDRAW;
            selectorTypes[4]  = REPAY;
            selectorTypes[5]  = CLAIM;
            selectorTypes[6]  = CLAIM;
            selectorTypes[7]  = SWAP;
            selectorTypes[8]  = SWAP;
            selectorTypes[9]  = SWAP;
            selectorTypes[10] = SWAP;

            registry.createPreset(
                "DeFi Trader",
                DEFI_EXECUTE_ROLE,
                500,  // 5% maxSpendingBps
                0,    // no USD cap
                1 days,
                protocols,
                parserProtocols,
                parserAddresses,
                selectors,
                selectorTypes
            );
            console.log("Preset 0 created: DeFi Trader");
        }

        // ── Preset 1: Yield Farmer ────────────────────────────────────────────
        {
            address[] memory protocols = new address[](3);
            protocols[0] = AAVE_V3_POOL;
            protocols[1] = AAVE_V3_REWARDS;
            protocols[2] = MORPHO_BLUE;

            address[] memory parserProtocols = new address[](3);
            parserProtocols[0] = AAVE_V3_POOL;
            parserProtocols[1] = AAVE_V3_REWARDS;
            parserProtocols[2] = MORPHO_BLUE;

            address[] memory parserAddresses = new address[](3);
            parserAddresses[0] = aaveParser;
            parserAddresses[1] = aaveParser;
            parserAddresses[2] = morphoBlueParser;

            bytes4[] memory selectors = new bytes4[](12);
            selectors[0]  = 0x095ea7b3; // approve
            // Aave V3
            selectors[1]  = 0x617ba037; // Aave supply            -> DEPOSIT
            selectors[2]  = 0x69328dec; // Aave withdraw           -> WITHDRAW
            selectors[3]  = 0xa415bcad; // Aave borrow             -> WITHDRAW
            selectors[4]  = 0x573ade81; // Aave repay              -> REPAY
            selectors[5]  = 0x236300dc; // claimRewards            -> CLAIM
            selectors[6]  = 0xbb492bf5; // claimAllRewards         -> CLAIM
            // Morpho Blue
            selectors[7]  = 0xa99aad89; // Morpho supply           -> DEPOSIT
            selectors[8]  = 0x5c2bea49; // Morpho withdraw         -> WITHDRAW
            selectors[9]  = 0x20b76e81; // Morpho repay            -> REPAY
            selectors[10] = 0x238d6579; // Morpho supplyCollateral  -> DEPOSIT
            selectors[11] = 0x8720316d; // Morpho withdrawCollateral -> WITHDRAW

            uint8[] memory selectorTypes = new uint8[](12);
            selectorTypes[0]  = APPROVE;
            // Aave V3
            selectorTypes[1]  = DEPOSIT;
            selectorTypes[2]  = WITHDRAW;
            selectorTypes[3]  = WITHDRAW;
            selectorTypes[4]  = REPAY;
            selectorTypes[5]  = CLAIM;
            selectorTypes[6]  = CLAIM;
            // Morpho Blue
            selectorTypes[7]  = DEPOSIT;   // supply
            selectorTypes[8]  = WITHDRAW;  // withdraw
            selectorTypes[9]  = REPAY;     // repay
            selectorTypes[10] = DEPOSIT;   // supplyCollateral
            selectorTypes[11] = WITHDRAW;  // withdrawCollateral

            registry.createPreset(
                "Yield Farmer",
                DEFI_EXECUTE_ROLE,
                1000, // 10% maxSpendingBps
                0,
                1 days,
                protocols,
                parserProtocols,
                parserAddresses,
                selectors,
                selectorTypes
            );
            console.log("Preset 1 created: Yield Farmer");
        }

        // ── Preset 2: Payment Agent ───────────────────────────────────────────
        {
            address[] memory empty = new address[](0);
            bytes4[] memory emptySelectors = new bytes4[](0);
            uint8[] memory emptyTypes = new uint8[](0);

            registry.createPreset(
                "Payment Agent",
                DEFI_TRANSFER_ROLE,
                100,  // 1% maxSpendingBps
                0,
                1 days,
                empty,
                empty,
                empty,
                emptySelectors,
                emptyTypes
            );
            console.log("Preset 2 created: Payment Agent");
        }

        vm.stopBroadcast();

        require(registry.presetCount() == 3, "Expected 3 presets");
        console.log("\nDone. PresetRegistry now has", registry.presetCount(), "presets.");
    }
}
