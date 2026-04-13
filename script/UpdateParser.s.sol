// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "../src/DeFiInteractorModule.sol";
import "../src/parsers/AaveV3Parser.sol";
import "../src/parsers/UniswapV3Parser.sol";
import "../src/parsers/UniswapV4Parser.sol";
import "../src/parsers/UniversalRouterParser.sol";
import "../src/parsers/MorphoParser.sol";
import "../src/parsers/MerklParser.sol";
import "./utils/SafeTxHelper.sol";

/**
 * @title UpdateParser
 * @notice Redeploy a parser and update its registration on the module
 * @dev Executes via Safe transaction since Safe is the module owner
 *
 * Environment variables:
 *   - SAFE_ADDRESS: The Safe multisig address (owner of the module)
 *   - DEFI_MODULE_ADDRESS: The deployed DeFiInteractorModule address
 *   - DEPLOYER_PRIVATE_KEY: Private key of Safe owner
 *   - PARSER_TYPE: Which parser to update (aave, uniswapv3, uniswapv4, universal, morpho, merkl)
 *   - PROTOCOL_ADDRESS: (Optional) Additional protocol address to register the parser for
 *
 * Usage:
 *   SAFE_ADDRESS=0x... DEFI_MODULE_ADDRESS=0x... PARSER_TYPE=uniswapv3 \
 *   forge script script/UpdateParser.s.sol --rpc-url $RPC_URL --broadcast --private-key $DEPLOYER_PRIVATE_KEY
 */
contract UpdateParser is Script, SafeTxHelper {
    // ============ Protocol Addresses ============
    // Ethereum Sepolia
    address constant SEPOLIA_AAVE_V3_POOL = 0x6Ae43d3271ff6888e7Fc43Fd7321a503ff738951;
    address constant SEPOLIA_AAVE_V3_REWARDS = 0x8164Cc65827dcFe994AB23944CBC90e0aa80bFcb;
    address constant SEPOLIA_UNISWAP_V3_ROUTER = 0x3bFA4769FB09eefC5a80d6E87c3B9C650f7Ae48E;
    address constant SEPOLIA_NONFUNGIBLE_POSITION_MANAGER = 0x1238536071E1c677A632429e3655c799b22cDA52;
    address constant SEPOLIA_UNISWAP_V4_POSITION_MANAGER = 0x429ba70129df741B2Ca2a85BC3A2a3328e5c09b4;
    address constant SEPOLIA_UNIVERSAL_ROUTER = 0x3fC91A3afd70395Cd496C647d5a6CC9D4B2b7FAD;
    address constant SEPOLIA_PANCAKESWAP_UNIVERSAL_ROUTER = 0x55D32fa7Da7290838347bc97cb7fAD4992672255;
    address constant SEPOLIA_UNISWAP_UNIVERSAL_ROUTER_V2 = 0x3A9D48AB9751398BbFa63ad67599Bb04e4BdF98b;
    address constant SEPOLIA_MERKL_DISTRIBUTOR = 0x3Ef3D8bA38EBe18DB133cEc108f4D14CE00Dd9Ae;

    // Base Sepolia
    address constant BASE_SEPOLIA_AAVE_V3_POOL = 0x8bAB6d1b75f19e9eD9fCe8b9BD338844fF79aE27;
    address constant BASE_SEPOLIA_AAVE_V3_REWARDS = 0x71B448405c803A3982aBa448133133D2DEAFBE5F;
    address constant BASE_SEPOLIA_UNISWAP_V3_ROUTER = 0x94cC0AaC535CCDB3C01d6787D6413C739ae12bc4;
    address constant BASE_SEPOLIA_NONFUNGIBLE_POSITION_MANAGER = 0x27F971cb582BF9E50F397e4d29a5C7A34f11faA2;
    address constant BASE_SEPOLIA_UNISWAP_V4_POSITION_MANAGER = 0x4B2C77d209D3405F41a037Ec6c77F7F5b8e2ca80;
    address constant BASE_SEPOLIA_UNIVERSAL_ROUTER = 0x492E6456D9528771018DeB9E87ef7750EF184104;

    address safe;
    address module;
    uint256 deployerPrivateKey;

    function run() external {
        safe = vm.envAddress("SAFE_ADDRESS");
        module = vm.envAddress("DEFI_MODULE_ADDRESS");
        deployerPrivateKey = vm.envUint("DEPLOYER_PRIVATE_KEY");
        string memory parserType = vm.envString("PARSER_TYPE");
        address extraProtocol = vm.envOr("PROTOCOL_ADDRESS", address(0));

        console.log("=== Update Parser ===");
        console.log("Safe:", safe);
        console.log("Module:", module);
        console.log("Parser type:", parserType);
        console.log("Chain ID:", block.chainid);

        vm.startBroadcast(deployerPrivateKey);

        bytes32 parserTypeHash = keccak256(bytes(parserType));

        if (parserTypeHash == keccak256("aave")) {
            _updateAaveParser(extraProtocol);
        } else if (parserTypeHash == keccak256("uniswapv3")) {
            _updateUniswapV3Parser(extraProtocol);
        } else if (parserTypeHash == keccak256("uniswapv4")) {
            _updateUniswapV4Parser(extraProtocol);
        } else if (parserTypeHash == keccak256("universal")) {
            _updateUniversalRouterParser(extraProtocol);
        } else if (parserTypeHash == keccak256("morpho")) {
            _updateMorphoParser(extraProtocol);
        } else if (parserTypeHash == keccak256("merkl")) {
            _updateMerklParser(extraProtocol);
        } else {
            revert("Unknown parser type. Use: aave, uniswapv3, uniswapv4, universal, morpho, merkl");
        }

        vm.stopBroadcast();
    }

    function _updateAaveParser(address extraProtocol) internal {
        console.log("\nDeploying new AaveV3Parser...");
        AaveV3Parser parser = new AaveV3Parser();
        console.log("Deployed at:", address(parser));

        console.log("Registering for Aave V3 Pool...");
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", _aavePool(), address(parser)),
            deployerPrivateKey
        );

        console.log("Registering for Aave V3 Rewards...");
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", _aaveRewards(), address(parser)),
            deployerPrivateKey
        );

        if (extraProtocol != address(0)) {
            console.log("Registering for extra protocol:", extraProtocol);
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature("registerParser(address,address)", extraProtocol, address(parser)),
                deployerPrivateKey
            );
        }

        console.log("\n=== AaveV3Parser Updated ===");
        console.log("New parser:", address(parser));
    }

    function _updateUniswapV3Parser(address extraProtocol) internal {
        console.log("\nDeploying new UniswapV3Parser...");
        UniswapV3Parser parser = new UniswapV3Parser();
        console.log("Deployed at:", address(parser));

        console.log("Registering for Uniswap V3 Router...");
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", _uniswapV3Router(), address(parser)),
            deployerPrivateKey
        );

        console.log("Registering for NonfungiblePositionManager...");
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature(
                "registerParser(address,address)", _nonfungiblePositionManager(), address(parser)
            ),
            deployerPrivateKey
        );

        if (extraProtocol != address(0)) {
            console.log("Registering for extra protocol:", extraProtocol);
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature("registerParser(address,address)", extraProtocol, address(parser)),
                deployerPrivateKey
            );
        }

        console.log("\n=== UniswapV3Parser Updated ===");
        console.log("New parser:", address(parser));
    }

    function _updateUniswapV4Parser(address extraProtocol) internal {
        console.log("\nDeploying new UniswapV4Parser...");
        UniswapV4Parser parser = new UniswapV4Parser();
        console.log("Deployed at:", address(parser));

        console.log("Registering for Uniswap V4 PositionManager...");
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature(
                "registerParser(address,address)", _uniswapV4PositionManager(), address(parser)
            ),
            deployerPrivateKey
        );

        if (extraProtocol != address(0)) {
            console.log("Registering for extra protocol:", extraProtocol);
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature("registerParser(address,address)", extraProtocol, address(parser)),
                deployerPrivateKey
            );
        }

        console.log("\n=== UniswapV4Parser Updated ===");
        console.log("New parser:", address(parser));
    }

    function _updateUniversalRouterParser(address extraProtocol) internal {
        console.log("\nDeploying new UniversalRouterParser...");
        UniversalRouterParser parser = new UniversalRouterParser();
        console.log("Deployed at:", address(parser));

        console.log("Registering for Universal Router...");
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", _universalRouter(), address(parser)),
            deployerPrivateKey
        );

        if (block.chainid == 11155111) {
            console.log("Registering for PancakeSwap Universal Router...");
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature(
                    "registerParser(address,address)", SEPOLIA_PANCAKESWAP_UNIVERSAL_ROUTER, address(parser)
                ),
                deployerPrivateKey
            );

            console.log("Registering for Uniswap Universal Router V2...");
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature(
                    "registerParser(address,address)", SEPOLIA_UNISWAP_UNIVERSAL_ROUTER_V2, address(parser)
                ),
                deployerPrivateKey
            );
        }

        if (extraProtocol != address(0)) {
            console.log("Registering for extra protocol:", extraProtocol);
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature("registerParser(address,address)", extraProtocol, address(parser)),
                deployerPrivateKey
            );
        }

        console.log("\n=== UniversalRouterParser Updated ===");
        console.log("New parser:", address(parser));
    }

    function _updateMorphoParser(address extraProtocol) internal {
        console.log("\nDeploying new MorphoParser...");
        MorphoParser parser = new MorphoParser();
        console.log("Deployed at:", address(parser));

        if (extraProtocol != address(0)) {
            console.log("Registering for Morpho vault:", extraProtocol);
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature("registerParser(address,address)", extraProtocol, address(parser)),
                deployerPrivateKey
            );
        } else {
            console.log("Note: Specify PROTOCOL_ADDRESS to register for a specific vault");
        }

        console.log("\n=== MorphoParser Updated ===");
        console.log("New parser:", address(parser));
    }

    function _updateMerklParser(address extraProtocol) internal {
        console.log("\nDeploying new MerklParser...");
        MerklParser parser = new MerklParser();
        console.log("Deployed at:", address(parser));

        console.log("Registering for Merkl Distributor...");
        _executeSafeTx(
            safe,
            module,
            abi.encodeWithSignature("registerParser(address,address)", _merklDistributor(), address(parser)),
            deployerPrivateKey
        );

        if (extraProtocol != address(0)) {
            console.log("Registering for extra protocol:", extraProtocol);
            _executeSafeTx(
                safe,
                module,
                abi.encodeWithSignature("registerParser(address,address)", extraProtocol, address(parser)),
                deployerPrivateKey
            );
        }

        console.log("\n=== MerklParser Updated ===");
        console.log("New parser:", address(parser));
    }

    function _aavePool() internal view returns (address) {
        if (block.chainid == 84532) return BASE_SEPOLIA_AAVE_V3_POOL;
        return SEPOLIA_AAVE_V3_POOL;
    }

    function _aaveRewards() internal view returns (address) {
        if (block.chainid == 84532) return BASE_SEPOLIA_AAVE_V3_REWARDS;
        return SEPOLIA_AAVE_V3_REWARDS;
    }

    function _uniswapV3Router() internal view returns (address) {
        if (block.chainid == 84532) return BASE_SEPOLIA_UNISWAP_V3_ROUTER;
        return SEPOLIA_UNISWAP_V3_ROUTER;
    }

    function _nonfungiblePositionManager() internal view returns (address) {
        if (block.chainid == 84532) return BASE_SEPOLIA_NONFUNGIBLE_POSITION_MANAGER;
        return SEPOLIA_NONFUNGIBLE_POSITION_MANAGER;
    }

    function _uniswapV4PositionManager() internal view returns (address) {
        if (block.chainid == 84532) return BASE_SEPOLIA_UNISWAP_V4_POSITION_MANAGER;
        return SEPOLIA_UNISWAP_V4_POSITION_MANAGER;
    }

    function _universalRouter() internal view returns (address) {
        if (block.chainid == 84532) return BASE_SEPOLIA_UNIVERSAL_ROUTER;
        return SEPOLIA_UNIVERSAL_ROUTER;
    }

    function _merklDistributor() internal view returns (address) {
        if (block.chainid == 84532) {
            revert("Merkl parser update not configured for Base Sepolia");
        }
        return SEPOLIA_MERKL_DISTRIBUTOR;
    }
}
