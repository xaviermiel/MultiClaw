// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Script, console} from "forge-std/Script.sol";
import {MockFreshPriceFeed} from "../test/mocks/MockFreshPriceFeed.sol";

/**
 * @title DeployMockFreshPriceFeed
 * @notice Deploys MockFreshPriceFeed contracts that wrap Chainlink feeds with fresh timestamps
 *
 * Usage:
 *   # Deploy for a single feed
 *   forge script script/DeployMockFreshPriceFeed.s.sol --sig "deploySingle(address)" <UNDERLYING_FEED> \
 *     --rpc-url $RPC_URL --broadcast --private-key $PRIVATE_KEY
 *
 *   # Deploy for ETH/USD and BTC/USD on Sepolia
 *   forge script script/DeployMockFreshPriceFeed.s.sol --sig "deploySepoliaFeeds()" \
 *     --rpc-url $RPC_URL --broadcast --private-key $PRIVATE_KEY
 */
contract DeployMockFreshPriceFeed is Script {
    // Chainlink Sepolia Price Feeds
    address constant SEPOLIA_ETH_USD = 0x694AA1769357215DE4FAC081bf1f309aDC325306;
    address constant SEPOLIA_BTC_USD = 0x1b44F3514812d835EB1BDB0acB33d3fA3351Ee43;
    address constant SEPOLIA_LINK_USD = 0xc59E3633BAAC79493d908e63626716e204A45EdF;
    address constant SEPOLIA_USDC_USD = 0xA2F78ab2355fe2f984D808B5CeE7FD0A93D5270E;
    address constant SEPOLIA_DAI_USD = 0x14866185B1962B63C3Ea9E03Bc1da838bab34C19;
    address constant SEPOLIA_EUR_USD = 0x1a81afB8146aeFfCFc5E50e8479e826E7D55b910;

    function deploySingle(address underlyingFeed) public returns (address) {
        vm.startBroadcast();
        MockFreshPriceFeed mock = new MockFreshPriceFeed(underlyingFeed);
        vm.stopBroadcast();

        console.log("MockFreshPriceFeed deployed at:", address(mock));
        console.log("  Underlying feed:", underlyingFeed);

        return address(mock);
    }

    function deploySepoliaFeeds() public {
        vm.startBroadcast();

        MockFreshPriceFeed ethUsd = new MockFreshPriceFeed(SEPOLIA_ETH_USD);
        MockFreshPriceFeed btcUsd = new MockFreshPriceFeed(SEPOLIA_BTC_USD);
        MockFreshPriceFeed linkUsd = new MockFreshPriceFeed(SEPOLIA_LINK_USD);
        MockFreshPriceFeed usdcUsd = new MockFreshPriceFeed(SEPOLIA_USDC_USD);
        MockFreshPriceFeed daiUsd = new MockFreshPriceFeed(SEPOLIA_DAI_USD);
        MockFreshPriceFeed eurUsd = new MockFreshPriceFeed(SEPOLIA_EUR_USD);

        vm.stopBroadcast();

        console.log("\n=== MockFreshPriceFeed Deployments (Sepolia) ===\n");
        console.log("ETH/USD Mock:", address(ethUsd));
        console.log("BTC/USD Mock:", address(btcUsd));
        console.log("LINK/USD Mock:", address(linkUsd));
        console.log("USDC/USD Mock:", address(usdcUsd));
        console.log("DAI/USD Mock:", address(daiUsd));
        console.log("EUR/USD Mock:", address(eurUsd));
        console.log("\n=== Update your config with these addresses ===");
    }

    function run() public {
        deploySepoliaFeeds();
    }
}
