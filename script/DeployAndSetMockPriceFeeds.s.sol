// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Script, console} from "forge-std/Script.sol";
import {MockFreshPriceFeed} from "../test/mocks/MockFreshPriceFeed.sol";
import "./utils/SafeTxHelper.sol";

/**
 * @title DeployAndSetMockPriceFeeds
 * @notice Deploys MockFreshPriceFeed contracts and sets them in the DeFiInteractorModule
 *
 * Usage:
 *   SAFE_ADDRESS=0x6E7692fFE42ca2A3FA2b08611AA7e79A2AaA8e8C \
 *   DEFI_MODULE_ADDRESS=0xDFF3cBa01F63152446E442133B664baE5A42bf39 \
 *   forge script script/DeployAndSetMockPriceFeeds.s.sol \
 *     --rpc-url $RPC_URL --broadcast --private-key $PRIVATE_KEY
 */
contract DeployAndSetMockPriceFeeds is Script, SafeTxHelper {
    // ============ Chainlink Sepolia Price Feeds (underlying) ============
    address constant CHAINLINK_ETH_USD = 0x694AA1769357215DE4FAC081bf1f309aDC325306;
    address constant CHAINLINK_BTC_USD = 0x1b44F3514812d835EB1BDB0acB33d3fA3351Ee43;
    address constant CHAINLINK_LINK_USD = 0xc59E3633BAAC79493d908e63626716e204A45EdF;
    address constant CHAINLINK_EUR_USD = 0x1a81afB8146aeFfCFc5E50e8479e826E7D55b910;

    // Existing mock for stablecoins (returns $1.00 with fresh timestamp)
    address constant MOCK_USDC_USD = 0xDd317fAbDD4884a1C3f87e53c119D2d58609e209;

    // ============ Token Addresses ============
    // Native ETH
    address constant NATIVE_ETH = address(0);

    // WETH variants
    address constant WETH_UNISWAP = 0xfFf9976782d46CC05630D1f6eBAb18b2324d6B14;
    address constant WETH_AAVE = 0xC558DBdd856501FCd9aaF1E62eae57A9F0629a3c;

    // Other tokens
    address constant WBTC = 0x29f2D40B0605204364af54EC677bD022dA425d03;
    address constant LINK = 0xf8Fb3713D459D7C1018BD0A49D19b4C44290EBE5;
    address constant AAVE = 0x88541670E55cC00bEEFD87eB59EDd1b7C511AC9a;
    address constant EURS = 0x6d906e526a4e2Ca02097BA9d0caA3c382F52278E;
    address constant EURC = 0x08210F9170F89Ab7658F0B5E3fF39b0E03C594D4;

    // Stablecoins (use mock USDC feed)
    address constant USDC_AAVE = 0x94a9D9AC8a22534E3FaCa9F4e7F2E2cf85d5E4C8;
    address constant USDC_CIRCLE = 0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238;
    address constant USDT = 0xaA8E23Fb1079EA71e0a56F48a2aA51851D8433D0;
    address constant DAI = 0xFF34B3d4Aee8ddCd6F9AFFFB6Fe49bD371b8a357;

    // aTokens
    address constant aWETH = 0x5b071b590a59395fE4025A0Ccc1FcC931AAc1830;
    address constant aWBTC = 0x1804Bf30507dc2EB3bDEbbbdd859991EAeF6EefF;
    address constant aUSDC = 0x16dA4541aD1807f4443d92D26044C1147406EB80;
    address constant aDAI = 0x29598b72eb5CeBd806C5dCD549490FdA35B13cD8;
    address constant aUSDT = 0xAF0F6e8b0Dc5c913bbF4d14c22B4E78Dd14310B6;
    address constant aLINK = 0x3FfAf50D4F4E96eB78f2407c090b72e86eCaed24;
    address constant aAAVE = 0x6b8558764d3b7572136F17174Cb9aB1DDc7E1259;
    address constant aEURS = 0xB20691021F9AcED8631eDaa3c0Cd2949EB45662D;

    function run() external {
        address safe = vm.envAddress("SAFE_ADDRESS");
        address module = vm.envAddress("DEFI_MODULE_ADDRESS");
        uint256 privateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");

        console.log("=== Deploy and Set Mock Price Feeds ===");
        console.log("Safe:", safe);
        console.log("Module:", module);

        vm.startBroadcast(privateKey);

        // ============ Step 1: Deploy MockFreshPriceFeed contracts ============
        console.log("\n--- Deploying MockFreshPriceFeed contracts ---");

        MockFreshPriceFeed mockEthUsd = new MockFreshPriceFeed(CHAINLINK_ETH_USD);
        console.log("Mock ETH/USD:", address(mockEthUsd));

        MockFreshPriceFeed mockBtcUsd = new MockFreshPriceFeed(CHAINLINK_BTC_USD);
        console.log("Mock BTC/USD:", address(mockBtcUsd));

        MockFreshPriceFeed mockLinkUsd = new MockFreshPriceFeed(CHAINLINK_LINK_USD);
        console.log("Mock LINK/USD:", address(mockLinkUsd));

        MockFreshPriceFeed mockEurUsd = new MockFreshPriceFeed(CHAINLINK_EUR_USD);
        console.log("Mock EUR/USD:", address(mockEurUsd));

        // ============ Step 2: Build token -> feed arrays ============
        // Total: 20 tokens
        address[] memory tokens = new address[](20);
        address[] memory feeds = new address[](20);

        // ETH/USD feed tokens (5)
        tokens[0] = NATIVE_ETH;
        feeds[0] = address(mockEthUsd);
        tokens[1] = WETH_UNISWAP;
        feeds[1] = address(mockEthUsd);
        tokens[2] = WETH_AAVE;
        feeds[2] = address(mockEthUsd);
        tokens[3] = aWETH;
        feeds[3] = address(mockEthUsd);

        // BTC/USD feed tokens (2)
        tokens[4] = WBTC;
        feeds[4] = address(mockBtcUsd);
        tokens[5] = aWBTC;
        feeds[5] = address(mockBtcUsd);

        // LINK/USD feed tokens (4) - also used for AAVE
        tokens[6] = LINK;
        feeds[6] = address(mockLinkUsd);
        tokens[7] = AAVE;
        feeds[7] = address(mockLinkUsd);
        tokens[8] = aLINK;
        feeds[8] = address(mockLinkUsd);
        tokens[9] = aAAVE;
        feeds[9] = address(mockLinkUsd);

        // EUR/USD feed tokens (3)
        tokens[10] = EURS;
        feeds[10] = address(mockEurUsd);
        tokens[11] = EURC;
        feeds[11] = address(mockEurUsd);
        tokens[12] = aEURS;
        feeds[12] = address(mockEurUsd);

        // Stablecoins using existing Mock USDC feed (7)
        tokens[13] = USDC_CIRCLE;
        feeds[13] = MOCK_USDC_USD;
        tokens[14] = USDC_AAVE;
        feeds[14] = MOCK_USDC_USD;
        tokens[15] = USDT;
        feeds[15] = MOCK_USDC_USD;
        tokens[16] = aUSDC;
        feeds[16] = MOCK_USDC_USD;
        tokens[17] = aUSDT;
        feeds[17] = MOCK_USDC_USD;
        tokens[18] = DAI;
        feeds[18] = MOCK_USDC_USD;
        tokens[19] = aDAI;
        feeds[19] = MOCK_USDC_USD;

        // ============ Step 3: Set price feeds via Safe transaction ============
        console.log("\n--- Setting price feeds in module via Safe ---");

        _executeSafeTx(
            safe, module, abi.encodeWithSignature("setTokenPriceFeeds(address[],address[])", tokens, feeds), privateKey
        );

        vm.stopBroadcast();

        // ============ Summary ============
        console.log("\n=== Deployment Summary ===");
        console.log("Mock ETH/USD:  ", address(mockEthUsd));
        console.log("Mock BTC/USD:  ", address(mockBtcUsd));
        console.log("Mock LINK/USD: ", address(mockLinkUsd));
        console.log("Mock EUR/USD:  ", address(mockEurUsd));
        console.log("Mock USDC/USD: ", MOCK_USDC_USD, "(existing)");
        console.log("\nTotal tokens configured:", tokens.length);
        console.log("\n=== Done ===");
    }
}
