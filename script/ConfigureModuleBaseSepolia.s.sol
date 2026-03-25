// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/DeFiInteractorModule.sol";
import "../src/parsers/AaveV3Parser.sol";
import "../src/parsers/UniswapV3Parser.sol";
import "../src/parsers/UniswapV4Parser.sol";
import "../src/parsers/UniversalRouterParser.sol";
import "./utils/SafeTxHelper.sol";

/**
 * @title ConfigureModuleBaseSepolia
 * @notice Full module configuration for Base Sepolia: parsers, selectors, price feeds, sub-accounts
 * @dev Deploys parsers, registers them, registers selectors, sets Chainlink price feeds,
 *      and configures sub-accounts with roles and spending limits.
 *
 * Environment variables:
 *   - SAFE_ADDRESS: The Safe multisig address (owner of the module)
 *   - DEFI_MODULE_ADDRESS: The deployed DeFiInteractorModule address
 *   - DEPLOYER_PRIVATE_KEY: Private key of Safe owner
 *   - SUB_ACCOUNT_ADDRESS: First sub-account (deployer/owner)
 *   - SUB_ACCOUNT_ADDRESS_1: Second sub-account (AI agent)
 */
contract ConfigureModuleBaseSepolia is Script, SafeTxHelper {
    // ============ Base Sepolia Protocol Addresses ============

    // Aave V3 (from bgd-labs/aave-address-book AaveV3BaseSepolia)
    address constant AAVE_V3_POOL = 0x8bAB6d1b75f19e9eD9fCe8b9BD338844fF79aE27;
    address constant AAVE_V3_REWARDS = 0x71B448405c803A3982aBa448133133D2DEAFBE5F;

    // Uniswap V3 (from official Uniswap Base Sepolia deployments)
    address constant UNISWAP_V3_SWAP_ROUTER_02 = 0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4;
    address constant NONFUNGIBLE_POSITION_MANAGER = 0x27F971cb582BF9E50F397e4d29a5C7A34f11faA2;

    // Uniswap V4
    address constant UNISWAP_V4_POSITION_MANAGER = 0x4B2C77d209D3405F41a037Ec6c77F7F5b8e2ca80;

    // Universal Router (shared V3 + V4)
    address constant UNIVERSAL_ROUTER = 0x492E6456D9528771018DeB9E87ef7750EF184104;

    // ============ Chainlink Price Feeds (Base Sepolia) ============

    address constant ETH_USD_FEED = 0x4aDC67696bA383F43DD60A9e78F2C97Fbbfc7cb1;
    address constant BTC_USD_FEED = 0x0FB99723Aee6f420beAD13e6bBB79b7E6F034298;
    address constant LINK_USD_FEED = 0xb113F5A928BCfF189C998ab20d753a47F9dE5A61;
    address constant USDC_USD_FEED = 0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165;
    address constant USDT_USD_FEED = 0x3ec8593F930EA45ea58c968260e6e9FF53FC934f;

    // ============ Tokens (Base Sepolia) ============

    // Underlying tokens
    address constant WETH = 0x4200000000000000000000000000000000000006;
    address constant USDC = 0xba50Cd2A20f6DA35D788639E581bca8d0B5d4D5f; // Aave USDC
    address constant USDC_CIRCLE = 0x036CbD53842c5426634e7929541eC2318f3dCF7e;
    address constant USDT = 0x0a215D8ba66387DCA84B284D18c3B4ec3de6E54a;
    address constant WBTC = 0x54114591963CF60EF3aA63bEfD6eC263D98145a4;
    address constant LINK = 0x810D46F9a9027E28F9B01F75E2bdde839dA61115;
    address constant cbETH = 0xD171b9694f7A2597Ed006D41f7509aaD4B485c4B;

    // Aave aTokens (Base Sepolia)
    address constant aWETH = 0x73a5bB60b0B0fc35710DDc0ea9c407031E31Bdbb;
    address constant aUSDC = 0x10F1A9D11CDf50041f3f8cB7191CBE2f31750ACC;
    address constant aUSDT = 0xcE3CAae5Ed17A7AafCEEbc897DE843fA6CC0c018;
    address constant aWBTC = 0x47Db195BAf46898302C06c31bCF46c01C64ACcF9;
    address constant aLINK = 0x0aD46dE765522399d7b25B438b230A894d72272B;
    address constant acbETH = 0x9Fd6d1DBAd7c052e0c43f46df36eEc6a68814B63;

    // ============ Selectors ============

    // ERC20
    bytes4 constant APPROVE_SELECTOR = 0x095ea7b3;

    // Aave V3 Pool
    bytes4 constant AAVE_SUPPLY = 0x617ba037;
    bytes4 constant AAVE_WITHDRAW = 0x69328dec;
    bytes4 constant AAVE_BORROW = 0xa415bcad;
    bytes4 constant AAVE_REPAY = 0x573ade81;

    // Aave V3 Rewards
    bytes4 constant AAVE_CLAIM_REWARDS = 0x236300dc;
    bytes4 constant AAVE_CLAIM_REWARDS_ON_BEHALF = 0x33028b99;
    bytes4 constant AAVE_CLAIM_ALL_REWARDS = 0xbb492bf5;
    bytes4 constant AAVE_CLAIM_ALL_ON_BEHALF = 0x9ff55db9;

    // Uniswap V3 SwapRouter02 selectors
    bytes4 constant EXACT_INPUT_SINGLE_V2 = 0x04e45aaf;
    bytes4 constant EXACT_INPUT_V2 = 0xb858183f;
    bytes4 constant EXACT_OUTPUT_SINGLE_V2 = 0x5023b4df;
    bytes4 constant EXACT_OUTPUT_V2 = 0x09b81346;

    // NonfungiblePositionManager
    bytes4 constant NPM_MINT = 0x88316456;
    bytes4 constant NPM_INCREASE_LIQUIDITY = 0x219f5d17;
    bytes4 constant NPM_DECREASE_LIQUIDITY = 0x0c49ccbe;
    bytes4 constant NPM_COLLECT = 0xfc6f7865;

    // Uniswap V4
    bytes4 constant MODIFY_LIQUIDITIES = 0xdd46508f;

    // Universal Router
    bytes4 constant UNIVERSAL_EXECUTE = 0x3593564c;

    function run() external {
        address safe = vm.envAddress("SAFE_ADDRESS");
        address module = vm.envAddress("DEFI_MODULE_ADDRESS");
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address subAccount1 = vm.envAddress("SUB_ACCOUNT_ADDRESS_1");

        DeFiInteractorModule defiModule = DeFiInteractorModule(module);

        console.log("=== Configure Module for Base Sepolia ===");
        console.log("Safe:", safe);
        console.log("Module:", module);
        console.log("Sub-account 1 (AI agent):", subAccount1);

        vm.startBroadcast(deployerPrivateKey);

        // ============ 1. Deploy Parsers ============
        console.log("\n--- 1. Deploying Parsers ---");

        AaveV3Parser aaveParser = new AaveV3Parser();
        console.log("AaveV3Parser:", address(aaveParser));

        UniswapV3Parser uniV3Parser = new UniswapV3Parser();
        console.log("UniswapV3Parser:", address(uniV3Parser));

        UniswapV4Parser uniV4Parser = new UniswapV4Parser();
        console.log("UniswapV4Parser:", address(uniV4Parser));

        UniversalRouterParser universalParser = new UniversalRouterParser();
        console.log("UniversalRouterParser:", address(universalParser));

        // ============ 2. Register Parsers ============
        console.log("\n--- 2. Registering Parsers ---");

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
        console.log("Aave V3 Pool & Rewards -> AaveV3Parser");

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
        console.log("Uniswap V3 SwapRouter02 & NPM -> UniswapV3Parser");

        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature(
                "registerParser(address,address)", UNISWAP_V4_POSITION_MANAGER, address(uniV4Parser)
            ),
            deployerPrivateKey
        );
        console.log("Uniswap V4 PositionManager -> UniswapV4Parser");

        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", UNIVERSAL_ROUTER, address(universalParser)),
            deployerPrivateKey
        );
        console.log("Universal Router -> UniversalRouterParser");

        // ============ 3. Register Selectors ============
        console.log("\n--- 3. Registering Selectors ---");

        // ERC20 approve -> APPROVE (5)
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", APPROVE_SELECTOR, uint8(5)),
            deployerPrivateKey
        );
        console.log("approve -> APPROVE");

        // Aave V3 Pool
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_SUPPLY, uint8(2)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_WITHDRAW, uint8(3)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_BORROW, uint8(3)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_REPAY, uint8(2)),
            deployerPrivateKey
        );
        console.log("Aave supply/repay -> DEPOSIT, withdraw/borrow -> WITHDRAW");

        // Aave V3 Rewards -> CLAIM (4)
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_CLAIM_REWARDS, uint8(4)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_CLAIM_REWARDS_ON_BEHALF, uint8(4)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_CLAIM_ALL_REWARDS, uint8(4)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", AAVE_CLAIM_ALL_ON_BEHALF, uint8(4)),
            deployerPrivateKey
        );
        console.log("Aave claim* -> CLAIM");

        // Uniswap V3 SwapRouter02 -> SWAP (1)
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", EXACT_INPUT_SINGLE_V2, uint8(1)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", EXACT_INPUT_V2, uint8(1)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", EXACT_OUTPUT_SINGLE_V2, uint8(1)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", EXACT_OUTPUT_V2, uint8(1)),
            deployerPrivateKey
        );
        console.log("SwapRouter02 exactInput*/exactOutput* -> SWAP");

        // NonfungiblePositionManager
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", NPM_MINT, uint8(2)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", NPM_INCREASE_LIQUIDITY, uint8(2)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", NPM_DECREASE_LIQUIDITY, uint8(3)),
            deployerPrivateKey
        );
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", NPM_COLLECT, uint8(4)),
            deployerPrivateKey
        );
        console.log("NPM mint/increase -> DEPOSIT, decrease -> WITHDRAW, collect -> CLAIM");

        // Uniswap V4 -> DEPOSIT (2) (parser handles dynamic classification)
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", MODIFY_LIQUIDITIES, uint8(2)),
            deployerPrivateKey
        );
        console.log("V4 modifyLiquidities -> DEPOSIT");

        // Universal Router -> SWAP (1)
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerSelector(bytes4,uint8)", UNIVERSAL_EXECUTE, uint8(1)),
            deployerPrivateKey
        );
        console.log("Universal Router execute -> SWAP");

        // ============ 4. Set Price Feeds ============
        console.log("\n--- 4. Setting Price Feeds ---");

        // 15 tokens: native ETH + 7 underlying + 6 aTokens + 1 Circle USDC
        address[] memory tokens = new address[](14);
        address[] memory feeds = new address[](14);

        // Native ETH
        tokens[0] = address(0);
        feeds[0] = ETH_USD_FEED;

        // Underlying tokens
        tokens[1] = WETH;
        feeds[1] = ETH_USD_FEED;
        tokens[2] = USDC;
        feeds[2] = USDC_USD_FEED;
        tokens[3] = USDT;
        feeds[3] = USDT_USD_FEED;
        tokens[4] = WBTC;
        feeds[4] = BTC_USD_FEED;
        tokens[5] = LINK;
        feeds[5] = LINK_USD_FEED;
        tokens[6] = cbETH;
        feeds[6] = ETH_USD_FEED; // cbETH uses ETH/USD as proxy
        tokens[7] = USDC_CIRCLE;
        feeds[7] = USDC_USD_FEED;

        // Aave aTokens (1:1 with underlying)
        tokens[8] = aWETH;
        feeds[8] = ETH_USD_FEED;
        tokens[9] = aUSDC;
        feeds[9] = USDC_USD_FEED;
        tokens[10] = aUSDT;
        feeds[10] = USDT_USD_FEED;
        tokens[11] = aWBTC;
        feeds[11] = BTC_USD_FEED;
        tokens[12] = aLINK;
        feeds[12] = LINK_USD_FEED;
        tokens[13] = acbETH;
        feeds[13] = ETH_USD_FEED;

        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("setTokenPriceFeeds(address[],address[])", tokens, feeds),
            deployerPrivateKey
        );
        console.log("Set price feeds for 14 tokens");

        // ============ 5. Configure Sub-Account 1 (AI agent) ============
        // Note: Sub-account 0 (deployer) is the authorizedOracle and cannot be a sub-account
        console.log("\n--- 5. Configuring Sub-Account 1 (AI agent) ---");

        // Grant DEFI_EXECUTE_ROLE
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("grantRole(address,uint16)", subAccount1, defiModule.DEFI_EXECUTE_ROLE()),
            deployerPrivateKey
        );
        // Grant DEFI_TRANSFER_ROLE
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("grantRole(address,uint16)", subAccount1, defiModule.DEFI_TRANSFER_ROLE()),
            deployerPrivateKey
        );
        console.log("Granted DEFI_EXECUTE_ROLE + DEFI_TRANSFER_ROLE");

        // Set spending limits: 5% BPS mode, 1 day window (tighter for AI agent)
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature(
                "setSubAccountLimits(address,uint256,uint256,uint256)",
                subAccount1,
                uint256(500),
                uint256(0),
                uint256(1 days)
            ),
            deployerPrivateKey
        );
        console.log("Set limits: 500 bps (5%), 1 day window");

        // Whitelist protocols for sub-account 1
        address[] memory protocols = new address[](6);
        protocols[0] = AAVE_V3_POOL;
        protocols[1] = AAVE_V3_REWARDS;
        protocols[2] = UNISWAP_V3_SWAP_ROUTER_02;
        protocols[3] = NONFUNGIBLE_POSITION_MANAGER;
        protocols[4] = UNISWAP_V4_POSITION_MANAGER;
        protocols[5] = UNIVERSAL_ROUTER;

        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("setAllowedAddresses(address,address[],bool)", subAccount1, protocols, true),
            deployerPrivateKey
        );
        console.log("Whitelisted 6 protocols");

        vm.stopBroadcast();

        console.log("\n=== Configuration Complete ===");
        console.log("Parsers deployed: 4 (Aave, UniV3, UniV4, UniversalRouter)");
        console.log("Protocols registered: 6");
        console.log("Selectors registered: 18");
        console.log("Price feeds set: 14 tokens");
        console.log("Sub-accounts configured: 1 (AI agent)");
    }
}
