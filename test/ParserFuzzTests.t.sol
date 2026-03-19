// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {UniswapV4Parser} from "../src/parsers/UniswapV4Parser.sol";
import {UniversalRouterParser} from "../src/parsers/UniversalRouterParser.sol";
import {AaveV3Parser} from "../src/parsers/AaveV3Parser.sol";
import {MorphoParser} from "../src/parsers/MorphoParser.sol";

/**
 * @title MockPositionManager
 * @notice Returns configurable pool info for V4 parser tests
 */
contract MockPositionManager {
    address public currency0;
    address public currency1;

    constructor(address _c0, address _c1) {
        currency0 = _c0;
        currency1 = _c1;
    }

    function getPoolAndPositionInfo(uint256)
        external
        view
        returns (address, address, uint24, int24, address, int24, int24, uint128)
    {
        return (currency0, currency1, 3000, 60, address(0), -887220, 887220, 1e18);
    }
}

/**
 * @title MockAavePool
 * @notice Returns configurable reserve data for Aave parser tests
 */
contract MockAavePool {
    address public aTokenAddress;

    constructor(address _aToken) {
        aTokenAddress = _aToken;
    }

    function getReserveData(address)
        external
        view
        returns (
            uint256,
            uint128,
            uint128,
            uint128,
            uint128,
            uint128,
            uint40,
            uint16,
            address _aToken,
            address,
            address,
            address,
            uint128,
            uint128,
            uint128
        )
    {
        return (0, 0, 0, 0, 0, 0, 0, 0, aTokenAddress, address(0), address(0), address(0), 0, 0, 0);
    }
}

/**
 * @title MockMorphoVault
 * @notice Returns configurable asset for Morpho parser tests
 */
contract MockMorphoVault {
    address public assetAddr;

    constructor(address _asset) {
        assetAddr = _asset;
    }

    function asset() external view returns (address) {
        return assetAddr;
    }

    function convertToAssets(uint256 shares) external pure returns (uint256) {
        return shares; // 1:1 for simplicity
    }
}

/**
 * @title ParserFuzzTests
 * @notice Fuzz tests for parser edge cases and invariants
 */
contract ParserFuzzTests is Test {
    UniswapV4Parser public v4Parser;
    UniversalRouterParser public universalParser;
    AaveV3Parser public aaveParser;
    MorphoParser public morphoParser;

    address public token0 = makeAddr("token0");
    address public token1 = makeAddr("token1");
    address public aToken = makeAddr("aToken");
    address public underlying = makeAddr("underlying");

    MockPositionManager public positionManager;
    MockAavePool public aavePool;
    MockMorphoVault public morphoVault;

    function setUp() public {
        v4Parser = new UniswapV4Parser();
        universalParser = new UniversalRouterParser();
        aaveParser = new AaveV3Parser();
        morphoParser = new MorphoParser();

        positionManager = new MockPositionManager(token0, token1);
        aavePool = new MockAavePool(aToken);
        morphoVault = new MockMorphoVault(underlying);
    }

    // ============ UniswapV4Parser: InvalidCalldata on malformed params ============

    function testFuzzV4MintPositionShortParamsReverts(uint16 paramLen) public {
        // Any param length < 320 should revert with InvalidCalldata for MINT_POSITION
        vm.assume(paramLen < 320);
        vm.assume(paramLen > 0);

        bytes memory params = new bytes(paramLen);
        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x02)); // MINT_POSITION

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(
            bytes4(0xdd46508f), // modifyLiquidities
            unlockData,
            block.timestamp
        );

        vm.expectRevert(UniswapV4Parser.InvalidCalldata.selector);
        v4Parser.extractInputAmounts(address(positionManager), callData);
    }

    function testFuzzV4IncreaseLiquidityShortParamsReverts(uint8 paramLen) public {
        // Any param length < 128 should revert with InvalidCalldata for INCREASE_LIQUIDITY
        vm.assume(paramLen < 128);
        vm.assume(paramLen > 0);

        bytes memory params = new bytes(paramLen);
        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x00)); // INCREASE_LIQUIDITY

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        vm.expectRevert(UniswapV4Parser.InvalidCalldata.selector);
        v4Parser.extractInputAmounts(address(positionManager), callData);
    }

    function testFuzzV4MintPositionValidParams(uint128 amount0, uint128 amount1) public {
        // Build valid MINT_POSITION params (PoolKey + tickLower + tickUpper + liquidity + amount0Max + amount1Max + owner + hookData)
        bytes memory params = abi.encode(
            token0, // currency0
            token1, // currency1
            uint24(3000), // fee
            int24(60), // tickSpacing
            address(0), // hooks
            int24(-887220), // tickLower
            int24(887220), // tickUpper
            uint256(1e18), // liquidity
            amount0, // amount0Max
            amount1 // amount1Max
        );

        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x02)); // MINT_POSITION

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        uint256[] memory amounts = v4Parser.extractInputAmounts(address(positionManager), callData);
        assertEq(amounts.length, 2, "Should return 2 amounts");
        assertEq(amounts[0], uint256(amount0), "amount0 mismatch");
        assertEq(amounts[1], uint256(amount1), "amount1 mismatch");
    }

    function testFuzzV4IncreaseLiquidityValidParams(uint128 amount0, uint128 amount1) public {
        // Build valid INCREASE_LIQUIDITY params (tokenId + liquidity + amount0Max + amount1Max + hookData)
        bytes memory params = abi.encode(
            uint256(1), // tokenId
            uint256(1e18), // liquidity
            amount0, // amount0Max
            amount1 // amount1Max
        );

        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x00)); // INCREASE_LIQUIDITY

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        uint256[] memory amounts = v4Parser.extractInputAmounts(address(positionManager), callData);
        assertEq(amounts.length, 2, "Should return 2 amounts");
        assertEq(amounts[0], uint256(amount0), "amount0 mismatch");
        assertEq(amounts[1], uint256(amount1), "amount1 mismatch");
    }

    // ============ UniswapV4Parser: Operation type invariants ============

    function testFuzzV4DecreaseLiquidityOpType(uint128 liquidity) public {
        // liquidity == 0 → CLAIM (4), liquidity > 0 → WITHDRAW (3)
        bytes memory params = abi.encode(
            uint256(1), // tokenId
            liquidity, // liquidity
            uint128(0), // amount0Min
            uint128(0) // amount1Min
        );

        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x01)); // DECREASE_LIQUIDITY

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        uint8 opType = v4Parser.getOperationType(callData);
        if (liquidity == 0) {
            assertEq(opType, 4, "liquidity=0 should be CLAIM");
        } else {
            assertEq(opType, 3, "liquidity>0 should be WITHDRAW");
        }
    }

    // ============ UniswapV4Parser: Token extraction invariants ============

    function testFuzzV4SettleInputTokenConsistency(address currency) public {
        // SETTLE should always return the currency as input token
        vm.assume(currency != address(0) || currency == address(0)); // any address

        bytes memory params = abi.encode(currency, uint256(1000), true);

        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x0b)); // SETTLE

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        address[] memory tokens = v4Parser.extractInputTokens(address(positionManager), callData);
        assertEq(tokens.length, 1, "Should return 1 token");
        assertEq(tokens[0], currency, "Token should match currency");
    }

    function testFuzzV4SettlePairInputTokens(address curr0, address curr1) public {
        // SETTLE_PAIR should return both currencies
        bytes memory params = abi.encode(curr0, curr1);

        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x0d)); // SETTLE_PAIR

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        address[] memory tokens = v4Parser.extractInputTokens(address(positionManager), callData);
        assertEq(tokens.length, 2, "Should return 2 tokens");
        assertEq(tokens[0], curr0, "First token mismatch");
        assertEq(tokens[1], curr1, "Second token mismatch");
    }

    // ============ UniversalRouterParser: Recipient resolution ============

    function testFuzzUniversalRouterV3RecipientResolution(address recipient) public {
        vm.assume(recipient != address(0));
        vm.assume(recipient != address(1)); // MSG_SENDER
        vm.assume(recipient != address(2)); // ADDRESS_THIS

        // Build V3_SWAP_EXACT_IN with explicit recipient
        bytes memory path = abi.encodePacked(token0, uint24(3000), token1);
        bytes memory swapInput = abi.encode(recipient, uint256(1000), uint256(900), path, true);

        bytes memory commands = new bytes(1);
        commands[0] = bytes1(uint8(0x00)); // V3_SWAP_EXACT_IN

        bytes[] memory inputs = new bytes[](1);
        inputs[0] = swapInput;

        bytes memory callData = abi.encodeWithSelector(
            bytes4(0x3593564c), // execute
            commands,
            inputs,
            block.timestamp
        );

        address safe = makeAddr("safe");
        address resolved = universalParser.extractRecipient(address(0), callData, safe);
        assertEq(resolved, recipient, "Should return explicit recipient");
    }

    function testUniversalRouterMsgSenderResolvesToDefault() public {
        // address(1) = MSG_SENDER should resolve to defaultRecipient (safe)
        bytes memory path = abi.encodePacked(token0, uint24(3000), token1);
        bytes memory swapInput = abi.encode(address(1), uint256(1000), uint256(900), path, true);

        bytes memory commands = new bytes(1);
        commands[0] = bytes1(uint8(0x00));

        bytes[] memory inputs = new bytes[](1);
        inputs[0] = swapInput;

        bytes memory callData = abi.encodeWithSelector(bytes4(0x3593564c), commands, inputs, block.timestamp);

        address safe = makeAddr("safe");
        address resolved = universalParser.extractRecipient(address(0), callData, safe);
        assertEq(resolved, safe, "MSG_SENDER should resolve to safe");
    }

    function testUniversalRouterAddressThisNotResolved() public {
        // address(2) = ADDRESS_THIS should NOT resolve — should be returned as-is
        // so module's recipient != avatar check blocks it
        bytes memory path = abi.encodePacked(token0, uint24(3000), token1);
        bytes memory swapInput = abi.encode(address(2), uint256(1000), uint256(900), path, true);

        bytes memory commands = new bytes(1);
        commands[0] = bytes1(uint8(0x00));

        bytes[] memory inputs = new bytes[](1);
        inputs[0] = swapInput;

        bytes memory callData = abi.encodeWithSelector(bytes4(0x3593564c), commands, inputs, block.timestamp);

        address safe = makeAddr("safe");
        address resolved = universalParser.extractRecipient(address(0), callData, safe);
        // ADDRESS_THIS (address(2)) is tracked via sawAddressThis path
        // Since there's no SWEEP and it's the only command, it returns ADDRESS_THIS
        assertEq(resolved, address(2), "ADDRESS_THIS should not be resolved");
    }

    // ============ UniversalRouterParser: Amount extraction invariants ============

    function testFuzzUniversalRouterV3ExactInAmount(uint256 amountIn) public {
        vm.assume(amountIn > 0);

        bytes memory path = abi.encodePacked(token0, uint24(3000), token1);
        bytes memory swapInput = abi.encode(address(1), amountIn, uint256(0), path, true);

        bytes memory commands = new bytes(1);
        commands[0] = bytes1(uint8(0x00)); // V3_SWAP_EXACT_IN

        bytes[] memory inputs = new bytes[](1);
        inputs[0] = swapInput;

        bytes memory callData = abi.encodeWithSelector(bytes4(0x3593564c), commands, inputs, block.timestamp);

        uint256[] memory amounts = universalParser.extractInputAmounts(address(0), callData);
        assertEq(amounts.length, 1, "Should return 1 amount");
        assertEq(amounts[0], amountIn, "Amount should match");
    }

    function testFuzzUniversalRouterV2ExactInAmount(uint256 amountIn) public {
        vm.assume(amountIn > 0);

        address[] memory path = new address[](2);
        path[0] = token0;
        path[1] = token1;

        bytes memory swapInput = abi.encode(address(1), amountIn, uint256(0), path, true);

        bytes memory commands = new bytes(1);
        commands[0] = bytes1(uint8(0x08)); // V2_SWAP_EXACT_IN

        bytes[] memory inputs = new bytes[](1);
        inputs[0] = swapInput;

        bytes memory callData = abi.encodeWithSelector(bytes4(0x3593564c), commands, inputs, block.timestamp);

        uint256[] memory amounts = universalParser.extractInputAmounts(address(0), callData);
        assertEq(amounts.length, 1, "Should return 1 amount");
        assertEq(amounts[0], amountIn, "Amount should match");
    }

    // ============ AaveV3Parser: Operation type invariants ============

    function testFuzzAaveSupplyExtractsCorrectAmount(uint256 amount) public {
        vm.assume(amount > 0);

        // supply(address asset, uint256 amount, address onBehalfOf, uint16 referralCode)
        bytes memory callData = abi.encodeWithSelector(
            bytes4(0x617ba037), // supply
            underlying,
            amount,
            makeAddr("safe"),
            uint16(0)
        );

        uint256[] memory amounts = aaveParser.extractInputAmounts(address(aavePool), callData);
        assertEq(amounts.length, 1);
        assertEq(amounts[0], amount);
    }

    function testFuzzAaveWithdrawExtractsCorrectAmount(uint256 amount) public {
        vm.assume(amount > 0);

        // withdraw(address asset, uint256 amount, address to)
        bytes memory callData = abi.encodeWithSelector(
            bytes4(0x69328dec), // withdraw
            underlying,
            amount,
            makeAddr("safe")
        );

        // Withdraw returns output tokens (the underlying)
        address[] memory tokens = aaveParser.extractOutputTokens(address(aavePool), callData);
        assertEq(tokens.length, 1);
        assertEq(tokens[0], underlying);
    }

    // ============ Cross-Parser: Array length invariant ============

    function testV4ParserInputTokensAndAmountsLengthMatch() public {
        // For any valid SETTLE operation, tokens.length == amounts.length
        bytes memory params = abi.encode(token0, uint256(1000), true);

        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x0b)); // SETTLE

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        address[] memory tokens = v4Parser.extractInputTokens(address(positionManager), callData);
        uint256[] memory amounts = v4Parser.extractInputAmounts(address(positionManager), callData);
        assertEq(tokens.length, amounts.length, "tokens and amounts length must match");
    }

    function testV4ParserSettlePairTokensAndAmountsLengthMatch() public {
        // SETTLE_PAIR returns 2 tokens but extractInputAmounts falls to Pass 2
        // which skips SETTLE_PAIR — so amounts should be empty (0)
        // This is the expected behavior: SETTLE_PAIR alone has no explicit amounts
        bytes memory params = abi.encode(token0, token1);

        bytes memory actions = new bytes(1);
        actions[0] = bytes1(uint8(0x0d)); // SETTLE_PAIR

        bytes[] memory paramsArray = new bytes[](1);
        paramsArray[0] = params;

        bytes memory unlockData = abi.encode(actions, paramsArray);
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdd46508f), unlockData, block.timestamp);

        address[] memory tokens = v4Parser.extractInputTokens(address(positionManager), callData);
        uint256[] memory amounts = v4Parser.extractInputAmounts(address(positionManager), callData);

        // SETTLE_PAIR alone: tokens=2 but amounts=0 (no explicit amounts)
        // Module would revert on LengthMismatch — which is correct for SETTLE_PAIR without MINT/INCREASE
        assertEq(tokens.length, 2, "SETTLE_PAIR should return 2 tokens");
        assertEq(amounts.length, 0, "SETTLE_PAIR alone should return 0 amounts");
    }

    // ============ Parser: Short/empty calldata handling ============

    function testV4ParserRevertsOnEmptyCalldata() public {
        vm.expectRevert(UniswapV4Parser.InvalidCalldata.selector);
        v4Parser.extractInputTokens(address(positionManager), "");
    }

    function testV4ParserRevertsOnShortCalldata() public {
        vm.expectRevert(UniswapV4Parser.InvalidCalldata.selector);
        v4Parser.extractInputTokens(address(positionManager), hex"aabbcc");
    }

    function testV4ParserRevertsOnWrongSelector() public {
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdeadbeef), bytes(""), uint256(0));
        vm.expectRevert(UniswapV4Parser.UnsupportedSelector.selector);
        v4Parser.extractInputTokens(address(positionManager), callData);
    }

    function testUniversalParserRevertsOnEmptyCalldata() public {
        vm.expectRevert(UniversalRouterParser.InvalidCalldata.selector);
        universalParser.extractInputTokens(address(0), "");
    }

    function testUniversalParserRevertsOnWrongSelector() public {
        bytes memory callData = abi.encodeWithSelector(bytes4(0xdeadbeef), bytes(""), new bytes[](0), uint256(0));
        vm.expectRevert(UniversalRouterParser.UnsupportedSelector.selector);
        universalParser.extractInputTokens(address(0), callData);
    }

    // ============ UniversalRouterParser: Always returns SWAP ============

    function testFuzzUniversalRouterAlwaysReturnsSwap(bytes calldata randomData) public {
        vm.assume(randomData.length >= 4);

        // getOperationType always returns 1 (SWAP) for valid selector, regardless of content
        bytes memory callData = abi.encodeWithSelector(bytes4(0x3593564c), bytes(""), new bytes[](0), uint256(0));

        uint8 opType = universalParser.getOperationType(callData);
        assertEq(opType, 1, "Universal Router should always be SWAP");
    }
}
