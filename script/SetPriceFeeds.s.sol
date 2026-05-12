// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/DeFiInteractorModule.sol";
import "./utils/SafeTxHelper.sol";

/**
 * @title SetPriceFeeds
 * @notice Set Chainlink price feeds for tokens. Branches the token + feed list
 *         by `block.chainid` so the same script targets Ethereum Sepolia and
 *         Base mainnet.
 * @dev Executes via Safe transaction since Safe is the module owner.
 *
 * Supported chains:
 *   - 11155111 → Ethereum Sepolia (Aave V3 testnet token set, 19 entries)
 *   - 8453     → Base mainnet (Chainlink Base feeds + canonical tokens)
 *
 * Environment variables:
 *   - SAFE_ADDRESS: The Safe multisig address (owner of the module)
 *   - DEFI_MODULE_ADDRESS: The deployed DeFiInteractorModule address
 *   - DEPLOYER_PRIVATE_KEY: Private key of Safe owner
 *
 * Usage:
 *   SAFE_ADDRESS=0x... DEFI_MODULE_ADDRESS=0x... \
 *   forge script script/SetPriceFeeds.s.sol --rpc-url $RPC_URL --broadcast --private-key $DEPLOYER_PRIVATE_KEY
 */
contract SetPriceFeeds is Script, SafeTxHelper {
    uint256 constant CHAIN_ETH_SEPOLIA = 11155111;
    uint256 constant CHAIN_BASE_MAINNET = 8453;

    function run() external {
        address safe = vm.envAddress("SAFE_ADDRESS");
        address module = vm.envAddress("DEFI_MODULE_ADDRESS");
        uint256 deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");

        console.log("=== Set Token Price Feeds ===");
        console.log("Chain ID:", block.chainid);
        console.log("Safe:", safe);
        console.log("Module:", module);

        (address[] memory tokens, address[] memory feeds) = _buildFeedList();

        vm.startBroadcast(deployerPrivateKey);

        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("setTokenPriceFeeds(address[],address[])", tokens, feeds),
            deployerPrivateKey
        );

        vm.stopBroadcast();

        console.log("Set price feeds for %s tokens", tokens.length);
    }

    function _buildFeedList() internal view returns (address[] memory, address[] memory) {
        if (block.chainid == CHAIN_ETH_SEPOLIA) return _ethSepoliaFeeds();
        if (block.chainid == CHAIN_BASE_MAINNET) return _baseMainnetFeeds();
        revert("SetPriceFeeds: unsupported chain. Add the network's feed list before running.");
    }

    // ── Ethereum Sepolia ──────────────────────────────────────────────────────
    function _ethSepoliaFeeds() internal pure returns (address[] memory, address[] memory) {
        // Chainlink Sepolia feeds
        address ETH_USD = 0x694AA1769357215DE4FAC081bf1f309aDC325306;
        address BTC_USD = 0x1b44F3514812d835EB1BDB0acB33d3fA3351Ee43;
        address LINK_USD = 0xc59E3633BAAC79493d908e63626716e204A45EdF;
        address USDC_USD = 0xA2F78ab2355fe2f984D808B5CeE7FD0A93D5270E;
        address DAI_USD = 0x14866185B1962B63C3Ea9E03Bc1da838bab34C19;
        address EUR_USD = 0x1a81afB8146aeFfCFc5E50e8479e826E7D55b910;

        // Aave V3 Sepolia underlying tokens
        address DAI = 0xFF34B3d4Aee8ddCd6F9AFFFB6Fe49bD371b8a357;
        address USDC = 0x94a9D9AC8a22534E3FaCa9F4e7F2E2cf85d5E4C8;
        address USDT = 0xaA8E23Fb1079EA71e0a56F48a2aA51851D8433D0;
        address WETH = 0xC558DBdd856501FCd9aaF1E62eae57A9F0629a3c;
        address WBTC = 0x29f2D40B0605204364af54EC677bD022dA425d03;
        address LINK = 0xf8Fb3713D459D7C1018BD0A49D19b4C44290EBE5;
        address AAVE = 0x88541670E55cC00bEEFD87eB59EDd1b7C511AC9a;
        address EURS = 0x6d906e526a4e2Ca02097BA9d0caA3c382F52278E;

        // Aave V3 Sepolia aTokens (1:1 with underlying, use same price feeds)
        address aDAI = 0x29598b72eb5CeBd806C5dCD549490FdA35B13cD8;
        address aUSDC = 0x16dA4541aD1807f4443d92D26044C1147406EB80;
        address aUSDT = 0xAF0F6e8b0Dc5c913bbF4d14c22B4E78Dd14310B6;
        address aWETH = 0x5b071b590a59395fE4025A0Ccc1FcC931AAc1830;
        address aWBTC = 0x1804Bf30507dc2EB3bDEbbbdd859991EAeF6EefF;
        address aLINK = 0x3FfAf50D4F4E96eB78f2407c090b72e86eCaed24;
        address aAAVE = 0x6b8558764d3b7572136F17174Cb9aB1DDc7E1259;
        address aEURS = 0xB20691021F9AcED8631eDaa3c0Cd2949EB45662D;

        // Other tokens
        address USDC_CIRCLE = 0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238;
        address EURC = 0x08210F9170F89Ab7658F0B5E3fF39b0E03C594D4;

        address[] memory tokens = new address[](19);
        address[] memory feeds = new address[](19);

        // Native ETH (for swaps with value)
        tokens[0] = address(0);
        feeds[0] = ETH_USD;

        // Underlying (8)
        tokens[1] = WETH;
        feeds[1] = ETH_USD;
        tokens[2] = WBTC;
        feeds[2] = BTC_USD;
        tokens[3] = USDC;
        feeds[3] = USDC_USD;
        tokens[4] = DAI;
        feeds[4] = DAI_USD;
        tokens[5] = USDT; // proxy: USDC/USD on testnet
        feeds[5] = USDC_USD;
        tokens[6] = LINK;
        feeds[6] = LINK_USD;
        tokens[7] = AAVE; // proxy: LINK/USD on testnet
        feeds[7] = LINK_USD;
        tokens[8] = EURS;
        feeds[8] = EUR_USD;

        // aTokens (8)
        tokens[9] = aWETH;
        feeds[9] = ETH_USD;
        tokens[10] = aWBTC;
        feeds[10] = BTC_USD;
        tokens[11] = aUSDC;
        feeds[11] = USDC_USD;
        tokens[12] = aDAI;
        feeds[12] = DAI_USD;
        tokens[13] = aUSDT;
        feeds[13] = USDC_USD;
        tokens[14] = aLINK;
        feeds[14] = LINK_USD;
        tokens[15] = aAAVE;
        feeds[15] = LINK_USD;
        tokens[16] = aEURS;
        feeds[16] = EUR_USD;

        // Other (2)
        tokens[17] = USDC_CIRCLE;
        feeds[17] = USDC_USD;
        tokens[18] = EURC;
        feeds[18] = EUR_USD;

        return (tokens, feeds);
    }

    // ── Base mainnet ─────────────────────────────────────────────────────────
    // Token + feed pairs mirror lib/priceFeeds.ts BASE_MAINNET. Verify each
    // Chainlink proxy against https://data.chain.link/base before broadcast —
    // Chainlink occasionally migrates aggregator proxies.
    function _baseMainnetFeeds() internal pure returns (address[] memory, address[] memory) {
        // Chainlink Base mainnet USD aggregators
        address ETH_USD = 0x71041dddad3595F9CEd3DcCFBe3D1F4b0a16Bb70;
        address BTC_USD = 0x64c911996D3c6aC71f9b455B1E8E7266BcbD848F;
        address USDC_USD = 0x7e860098F58bBFC8648a4311b374B1D669a2bc6B;
        address USDT_USD = 0xf19d560eB8d2ADf07BD6D13ed03e1D11215721F9;
        address DAI_USD = 0x591e79239a7d679378eC8c847e5038150364C78F;
        address CBETH_USD = 0xd7818272B9e248357d13057AAb0B417aF31E817d;
        address LINK_USD = 0x17CAb8FE31E32f08326e5E27412894e49B0f9D65;
        address AAVE_USD = 0x3d6774EF702A10b20FCa8Ed40FC022f7E4938e07;
        address EUR_USD = 0xc91D87E81faB8f93699ECf7Ee9B44D11e1D53F0F;

        // Canonical token addresses on Base mainnet
        address WETH = 0x4200000000000000000000000000000000000006;
        address USDC = 0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913; // Circle native
        address USDbC = 0xd9aAEc86B65D86f6A7B5B1b0c42FFA531710b6CA; // bridged USDC
        address USDT = 0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2;
        address DAI = 0x50c5725949A6F0c72E6C4a641F24049A917DB0Cb;
        address cbETH = 0x2Ae3F1Ec7F1F5012CFEab0185bfc7aa3cf0DEc22;
        address cbBTC = 0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf;
        address LINK = 0x88Fb150BDc53A65fe94Dea0c9BA0a6dAf8C6e196;
        address AAVE = 0x63706e401c06ac8513145b7687A14804d17f814b;
        address EURC = 0x60a3E35Cc302bFA44Cb288Bc5a4F316Fdb1adb42;

        address[] memory tokens = new address[](11);
        address[] memory feeds = new address[](11);

        // Native ETH for Universal Router swaps with value
        tokens[0] = address(0);
        feeds[0] = ETH_USD;
        tokens[1] = WETH;
        feeds[1] = ETH_USD;
        tokens[2] = USDC;
        feeds[2] = USDC_USD;
        tokens[3] = USDbC;
        feeds[3] = USDC_USD;
        tokens[4] = USDT;
        feeds[4] = USDT_USD;
        tokens[5] = DAI;
        feeds[5] = DAI_USD;
        tokens[6] = cbETH;
        feeds[6] = CBETH_USD;
        tokens[7] = cbBTC;
        feeds[7] = BTC_USD;
        tokens[8] = LINK;
        feeds[8] = LINK_USD;
        tokens[9] = AAVE;
        feeds[9] = AAVE_USD;
        tokens[10] = EURC;
        feeds[10] = EUR_USD;

        return (tokens, feeds);
    }
}
