// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {OneInchParser} from "../src/parsers/OneInchParser.sol";
import {IUniswapV2Pair} from "../src/interfaces/IUniswapV2Pair.sol";

/**
 * @title MockUniswapV2Pair
 * @notice Mock Uniswap V2 pair for testing unoswapTo output token extraction
 */
contract MockUniswapV2Pair is IUniswapV2Pair {
    address public override token0;
    address public override token1;

    constructor(address _token0, address _token1) {
        token0 = _token0;
        token1 = _token1;
    }
}

    /**
     * @title OneInchParserTest
     * @notice Tests for the 1inch AggregationRouterV6 calldata parser
     */
    contract OneInchParserTest is Test {
        OneInchParser public parser;

        // Test addresses
        address constant ONE_INCH_ROUTER = 0x111111125421cA6dc452d289314280a0f8842A65;
        address constant USDC = 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48;
        address constant WETH = 0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2;
        address constant DAI = 0x6B175474E89094C44Da98b954EedeAC495271d0F;
        address constant USER = address(0x1234);
        address constant EXECUTOR = address(0xABCD);
        address constant CLIPPER_EXCHANGE = address(0xCCCC);
        address constant DEFAULT_RECIPIENT = address(0x5678);

        function setUp() public {
            parser = new OneInchParser();
        }

        // ============ Selector Constants Tests ============

        function testSelectors() public view {
            assertEq(parser.SWAP_SELECTOR(), bytes4(0x12aa3caf), "Swap selector mismatch");
            assertEq(parser.UNOSWAP_TO_SELECTOR(), bytes4(0xf78dc253), "UnoswapTo selector mismatch");
            assertEq(parser.CLIPPER_SWAP_TO_SELECTOR(), bytes4(0x093d4fa5), "ClipperSwapTo selector mismatch");
        }

        function testSupportsSelector() public view {
            assertTrue(parser.supportsSelector(parser.SWAP_SELECTOR()), "Should support swap");
            assertTrue(parser.supportsSelector(parser.UNOSWAP_TO_SELECTOR()), "Should support unoswapTo");
            assertTrue(parser.supportsSelector(parser.CLIPPER_SWAP_TO_SELECTOR()), "Should support clipperSwapTo");

            // Unsupported selectors
            assertFalse(parser.supportsSelector(bytes4(0xdeadbeef)), "Should not support unknown selector");
            assertFalse(parser.supportsSelector(bytes4(0x00000000)), "Should not support zero selector");
            // uniswapV3SwapTo is declared as a constant but not supported in any parser function
            assertFalse(
                parser.supportsSelector(bytes4(0xbc80f1a8)), "Should not support uniswapV3SwapTo in supportsSelector"
            );
        }

        // ============ Helper: Build swap() calldata ============

        /// @dev Builds swap(address executor, SwapDescription desc, bytes data, bytes permit) calldata.
        ///      SwapDescription is: (srcToken, dstToken, srcReceiver, dstReceiver, amount, minReturnAmount, flags)
        function _buildSwapCalldata(
            address srcToken,
            address dstToken,
            address srcReceiver,
            address dstReceiver,
            uint256 amount,
            uint256 minReturnAmount,
            uint256 flags
        ) internal view returns (bytes memory) {
            // The swap function signature uses dynamic types for desc, data, and permit.
            // We need to manually encode the calldata with proper ABI offsets.
            // swap(address executor, SwapDescription desc, bytes data, bytes permit)
            //
            // Layout after selector:
            //   [0x00] executor (static)
            //   [0x20] offset to desc (dynamic - tuple)
            //   [0x40] offset to data (dynamic - bytes)
            //   [0x60] offset to permit (dynamic - bytes)
            //   [desc offset] srcToken, dstToken, srcReceiver, dstReceiver, amount, minReturnAmount, flags (7 * 32 = 224 bytes)
            //   [data offset] length + bytes
            //   [permit offset] length + bytes
            //
            // The parser reads descOffset at position 32 (after executor), then loads the struct offset
            // and reads fields from there.

            // Build the full calldata manually
            bytes memory result = abi.encodePacked(
                parser.SWAP_SELECTOR(),
                // executor (32 bytes)
                abi.encode(EXECUTOR),
                // offset to desc tuple: starts at 4 * 32 = 128 from start of params
                abi.encode(uint256(128)),
                // offset to data bytes: 128 + 224 = 352 from start of params
                abi.encode(uint256(352)),
                // offset to permit bytes: 352 + 32 = 384 from start of params
                abi.encode(uint256(384)),
                // SwapDescription struct fields (7 words = 224 bytes)
                abi.encode(srcToken),
                abi.encode(dstToken),
                abi.encode(srcReceiver),
                abi.encode(dstReceiver),
                abi.encode(amount),
                abi.encode(minReturnAmount),
                abi.encode(flags),
                // data bytes (length 0)
                abi.encode(uint256(0)),
                // permit bytes (length 0)
                abi.encode(uint256(0))
            );

            return result;
        }

        // ============ swap() Tests ============

        function testSwapExtractInputTokens() public view {
            bytes memory data = _buildSwapCalldata(USDC, WETH, address(0), USER, 1000e6, 0.5e18, 0);

            address[] memory tokens = parser.extractInputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 input token");
            assertEq(tokens[0], USDC, "Input token should be USDC");
        }

        function testSwapExtractInputTokensWETH() public view {
            bytes memory data = _buildSwapCalldata(WETH, USDC, address(0), USER, 1e18, 900e6, 0);

            address[] memory tokens = parser.extractInputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 input token");
            assertEq(tokens[0], WETH, "Input token should be WETH");
        }

        function testSwapExtractInputAmounts() public view {
            bytes memory data = _buildSwapCalldata(USDC, WETH, address(0), USER, 1000e6, 0.5e18, 0);

            uint256[] memory amounts = parser.extractInputAmounts(ONE_INCH_ROUTER, data);
            assertEq(amounts.length, 1, "Should have 1 input amount");
            assertEq(amounts[0], 1000e6, "Input amount should be 1000e6");
        }

        function testSwapExtractInputAmountsLargeValue() public view {
            uint256 largeAmount = 1_000_000e18;
            bytes memory data = _buildSwapCalldata(DAI, WETH, address(0), USER, largeAmount, 500e18, 0);

            uint256[] memory amounts = parser.extractInputAmounts(ONE_INCH_ROUTER, data);
            assertEq(amounts.length, 1, "Should have 1 input amount");
            assertEq(amounts[0], largeAmount, "Input amount should be 1_000_000e18");
        }

        function testSwapExtractOutputTokens() public view {
            bytes memory data = _buildSwapCalldata(USDC, WETH, address(0), USER, 1000e6, 0.5e18, 0);

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 output token");
            assertEq(tokens[0], WETH, "Output token should be WETH");
        }

        function testSwapExtractOutputTokensReverse() public view {
            bytes memory data = _buildSwapCalldata(WETH, DAI, address(0), USER, 1e18, 2000e18, 0);

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 output token");
            assertEq(tokens[0], DAI, "Output token should be DAI");
        }

        function testSwapExtractRecipient() public view {
            bytes memory data = _buildSwapCalldata(USDC, WETH, address(0), USER, 1000e6, 0.5e18, 0);

            address recipient = parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
            assertEq(recipient, USER, "Recipient should be USER (dstReceiver)");
        }

        function testSwapExtractRecipientDifferentAddress() public view {
            address customRecipient = address(0x9999);
            bytes memory data = _buildSwapCalldata(USDC, WETH, address(0), customRecipient, 1000e6, 0.5e18, 0);

            address recipient = parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
            assertEq(recipient, customRecipient, "Recipient should be custom address");
        }

        function testSwapGetOperationType() public view {
            bytes memory data = _buildSwapCalldata(USDC, WETH, address(0), USER, 1000e6, 0.5e18, 0);

            uint8 opType = parser.getOperationType(data);
            assertEq(opType, 1, "Swap should be SWAP (1)");
        }

        // ============ unoswapTo() Tests ============

        function _buildUnoswapToCalldata(
            address to,
            address srcToken,
            uint256 amount,
            uint256 minReturn,
            uint256[] memory pools
        ) internal view returns (bytes memory) {
            return abi.encodeWithSelector(parser.UNOSWAP_TO_SELECTOR(), to, srcToken, amount, minReturn, pools);
        }

        function testUnoswapToExtractInputTokens() public view {
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(0xDEAD)));

            bytes memory data = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);

            address[] memory tokens = parser.extractInputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 input token");
            assertEq(tokens[0], USDC, "Input token should be USDC");
        }

        function testUnoswapToExtractInputTokensWETH() public view {
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(0xDEAD)));

            bytes memory data = _buildUnoswapToCalldata(USER, WETH, 2e18, 4000e6, pools);

            address[] memory tokens = parser.extractInputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 input token");
            assertEq(tokens[0], WETH, "Input token should be WETH");
        }

        function testUnoswapToExtractInputAmounts() public view {
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(0xDEAD)));

            bytes memory data = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);

            uint256[] memory amounts = parser.extractInputAmounts(ONE_INCH_ROUTER, data);
            assertEq(amounts.length, 1, "Should have 1 input amount");
            assertEq(amounts[0], 500e6, "Input amount should be 500e6");
        }

        function testUnoswapToExtractRecipient() public view {
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(0xDEAD)));

            bytes memory data = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);

            address recipient = parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
            assertEq(recipient, USER, "Recipient should be USER (to param)");
        }

        function testUnoswapToExtractRecipientDifferentAddress() public view {
            address customRecipient = address(0x7777);
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(0xDEAD)));

            bytes memory data = _buildUnoswapToCalldata(customRecipient, USDC, 500e6, 0.25e18, pools);

            address recipient = parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
            assertEq(recipient, customRecipient, "Recipient should be custom address");
        }

        function testUnoswapToGetOperationType() public view {
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(0xDEAD)));

            bytes memory data = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);

            uint8 opType = parser.getOperationType(data);
            assertEq(opType, 1, "UnoswapTo should be SWAP (1)");
        }

        /// @notice extractOutputTokens for unoswapTo requires on-chain pool query.
        ///         We test with a mock Uniswap V2 pair to verify the logic.
        function testUnoswapToExtractOutputTokensWithMock_ZeroForOne() public {
            // Deploy mock pair where token0=USDC, token1=WETH
            MockUniswapV2Pair mockPair = new MockUniswapV2Pair(USDC, WETH);

            // Pool encoding: bits 0-159 = pool address, bit 255 = direction flag
            // direction 0 (bit 255 = 0) means zeroForOne: token0 -> token1, output = token1
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(mockPair))); // bit 255 = 0

            bytes memory data = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 output token");
            assertEq(tokens[0], WETH, "Output should be WETH (token1) for zeroForOne direction");
        }

        function testUnoswapToExtractOutputTokensWithMock_OneForZero() public {
            // Deploy mock pair where token0=USDC, token1=WETH
            MockUniswapV2Pair mockPair = new MockUniswapV2Pair(USDC, WETH);

            // direction 1 (bit 255 = 1) means oneForZero: token1 -> token0, output = token0
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(mockPair))) | (uint256(1) << 255);

            bytes memory data = _buildUnoswapToCalldata(USER, WETH, 1e18, 2000e6, pools);

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 output token");
            assertEq(tokens[0], USDC, "Output should be USDC (token0) for oneForZero direction");
        }

        function testUnoswapToExtractOutputTokensMultiHop() public {
            // Multi-hop: USDC -> DAI -> WETH
            // Only the LAST pool determines the output token
            MockUniswapV2Pair mockPair1 = new MockUniswapV2Pair(USDC, DAI);
            MockUniswapV2Pair mockPair2 = new MockUniswapV2Pair(DAI, WETH);

            uint256[] memory pools = new uint256[](2);
            pools[0] = uint256(uint160(address(mockPair1))); // first hop, direction doesn't matter for output
            pools[1] = uint256(uint160(address(mockPair2))); // last hop, bit 255 = 0 => output = token1 = WETH

            bytes memory data = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 output token");
            assertEq(tokens[0], WETH, "Output should be WETH from last pool");
        }

        function testUnoswapToExtractOutputTokensEmptyPools() public view {
            uint256[] memory pools = new uint256[](0);
            bytes memory data = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 0, "Should return empty array for empty pools");
        }

        // ============ clipperSwapTo() Tests ============

        function _buildClipperSwapToCalldata(
            address clipperExchange,
            address recipient,
            address srcToken,
            address dstToken,
            uint256 inputAmount,
            uint256 outputAmount,
            uint256 goodUntil,
            bytes32 r,
            bytes32 vs
        ) internal view returns (bytes memory) {
            return abi.encodeWithSelector(
                parser.CLIPPER_SWAP_TO_SELECTOR(),
                clipperExchange,
                recipient,
                srcToken,
                dstToken,
                inputAmount,
                outputAmount,
                goodUntil,
                r,
                vs
            );
        }

        function testClipperSwapToExtractInputTokens() public view {
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, USDC, WETH, 1000e6, 0.5e18, block.timestamp + 300, bytes32(0), bytes32(0)
            );

            address[] memory tokens = parser.extractInputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 input token");
            assertEq(tokens[0], USDC, "Input token should be USDC");
        }

        function testClipperSwapToExtractInputTokensReverse() public view {
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, WETH, DAI, 2e18, 4000e18, block.timestamp + 300, bytes32(0), bytes32(0)
            );

            address[] memory tokens = parser.extractInputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 input token");
            assertEq(tokens[0], WETH, "Input token should be WETH");
        }

        function testClipperSwapToExtractInputAmounts() public view {
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, USDC, WETH, 1000e6, 0.5e18, block.timestamp + 300, bytes32(0), bytes32(0)
            );

            uint256[] memory amounts = parser.extractInputAmounts(ONE_INCH_ROUTER, data);
            assertEq(amounts.length, 1, "Should have 1 input amount");
            assertEq(amounts[0], 1000e6, "Input amount should be 1000e6");
        }

        function testClipperSwapToExtractInputAmountsLargeValue() public view {
            uint256 largeAmount = 50_000_000e6; // 50M USDC
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE,
                USER,
                USDC,
                WETH,
                largeAmount,
                25_000e18,
                block.timestamp + 300,
                bytes32(0),
                bytes32(0)
            );

            uint256[] memory amounts = parser.extractInputAmounts(ONE_INCH_ROUTER, data);
            assertEq(amounts.length, 1, "Should have 1 input amount");
            assertEq(amounts[0], largeAmount, "Input amount should match large value");
        }

        function testClipperSwapToExtractOutputTokens() public view {
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, USDC, WETH, 1000e6, 0.5e18, block.timestamp + 300, bytes32(0), bytes32(0)
            );

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 output token");
            assertEq(tokens[0], WETH, "Output token should be WETH");
        }

        function testClipperSwapToExtractOutputTokensReverse() public view {
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, WETH, USDC, 1e18, 2000e6, block.timestamp + 300, bytes32(0), bytes32(0)
            );

            address[] memory tokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(tokens.length, 1, "Should have 1 output token");
            assertEq(tokens[0], USDC, "Output token should be USDC");
        }

        function testClipperSwapToExtractRecipient() public view {
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, USDC, WETH, 1000e6, 0.5e18, block.timestamp + 300, bytes32(0), bytes32(0)
            );

            address recipient = parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
            assertEq(recipient, USER, "Recipient should be USER");
        }

        function testClipperSwapToExtractRecipientDifferentAddress() public view {
            address customRecipient = address(0xBBBB);
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE,
                customRecipient,
                USDC,
                WETH,
                1000e6,
                0.5e18,
                block.timestamp + 300,
                bytes32(0),
                bytes32(0)
            );

            address recipient = parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
            assertEq(recipient, customRecipient, "Recipient should be custom address");
        }

        function testClipperSwapToGetOperationType() public view {
            bytes memory data = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, USDC, WETH, 1000e6, 0.5e18, block.timestamp + 300, bytes32(0), bytes32(0)
            );

            uint8 opType = parser.getOperationType(data);
            assertEq(opType, 1, "ClipperSwapTo should be SWAP (1)");
        }

        // ============ Edge Cases: InvalidCalldata ============

        function testExtractInputTokensRevertsOnEmptyCalldata() public {
            bytes memory data = new bytes(0);

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractInputTokens(ONE_INCH_ROUTER, data);
        }

        function testExtractInputTokensRevertsOnShortCalldata() public {
            bytes memory data = new bytes(3); // Less than 4 bytes

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractInputTokens(ONE_INCH_ROUTER, data);
        }

        function testExtractInputAmountsRevertsOnEmptyCalldata() public {
            bytes memory data = new bytes(0);

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractInputAmounts(ONE_INCH_ROUTER, data);
        }

        function testExtractOutputTokensRevertsOnEmptyCalldata() public {
            bytes memory data = new bytes(0);

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractOutputTokens(ONE_INCH_ROUTER, data);
        }

        function testExtractRecipientRevertsOnEmptyCalldata() public {
            bytes memory data = new bytes(0);

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
        }

        function testGetOperationTypeRevertsOnEmptyCalldata() public {
            bytes memory data = new bytes(0);

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.getOperationType(data);
        }

        function testSwapRevertsOnTooShortCalldata() public {
            // swap selector + only a few bytes, less than MIN_SWAP_LENGTH (292)
            bytes memory data = abi.encodePacked(parser.SWAP_SELECTOR(), uint256(0), uint256(0));

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractInputTokens(ONE_INCH_ROUTER, data);
        }

        function testSwapExtractInputAmountsRevertsOnTooShortCalldata() public {
            bytes memory data = abi.encodePacked(parser.SWAP_SELECTOR(), uint256(0), uint256(0));

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractInputAmounts(ONE_INCH_ROUTER, data);
        }

        function testSwapExtractOutputTokensRevertsOnTooShortCalldata() public {
            bytes memory data = abi.encodePacked(parser.SWAP_SELECTOR(), uint256(0), uint256(0));

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractOutputTokens(ONE_INCH_ROUTER, data);
        }

        function testSwapExtractRecipientRevertsOnTooShortCalldata() public {
            bytes memory data = abi.encodePacked(parser.SWAP_SELECTOR(), uint256(0), uint256(0));

            vm.expectRevert(OneInchParser.InvalidCalldata.selector);
            parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
        }

        // ============ Edge Cases: UnsupportedSelector ============

        function testExtractInputTokensRevertsOnUnsupportedSelector() public {
            bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

            vm.expectRevert(OneInchParser.UnsupportedSelector.selector);
            parser.extractInputTokens(ONE_INCH_ROUTER, data);
        }

        function testExtractInputAmountsRevertsOnUnsupportedSelector() public {
            bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

            vm.expectRevert(OneInchParser.UnsupportedSelector.selector);
            parser.extractInputAmounts(ONE_INCH_ROUTER, data);
        }

        function testExtractOutputTokensRevertsOnUnsupportedSelector() public {
            bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

            vm.expectRevert(OneInchParser.UnsupportedSelector.selector);
            parser.extractOutputTokens(ONE_INCH_ROUTER, data);
        }

        function testExtractRecipientRevertsOnUnsupportedSelector() public {
            bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

            vm.expectRevert(OneInchParser.UnsupportedSelector.selector);
            parser.extractRecipient(ONE_INCH_ROUTER, data, DEFAULT_RECIPIENT);
        }

        function testGetOperationTypeReturnsZeroForUnsupportedSelector() public view {
            bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

            uint8 opType = parser.getOperationType(data);
            assertEq(opType, 0, "Unknown selector should return 0 (UNKNOWN)");
        }

        // ============ Cross-selector consistency ============

        function testAllSelectorsReturnSwapOperationType() public view {
            // swap
            bytes memory swapData = _buildSwapCalldata(USDC, WETH, address(0), USER, 1000e6, 0.5e18, 0);
            assertEq(parser.getOperationType(swapData), 1, "swap should be SWAP (1)");

            // unoswapTo
            uint256[] memory pools = new uint256[](1);
            pools[0] = uint256(uint160(address(0xDEAD)));
            bytes memory unoswapData = _buildUnoswapToCalldata(USER, USDC, 500e6, 0.25e18, pools);
            assertEq(parser.getOperationType(unoswapData), 1, "unoswapTo should be SWAP (1)");

            // clipperSwapTo
            bytes memory clipperData = _buildClipperSwapToCalldata(
                CLIPPER_EXCHANGE, USER, USDC, WETH, 1000e6, 0.5e18, block.timestamp + 300, bytes32(0), bytes32(0)
            );
            assertEq(parser.getOperationType(clipperData), 1, "clipperSwapTo should be SWAP (1)");
        }

        // ============ swap() with non-zero flags ============

        function testSwapWithNonZeroFlags() public view {
            uint256 flags = 4; // partial fill flag
            bytes memory data = _buildSwapCalldata(USDC, WETH, address(0), USER, 1000e6, 0.5e18, flags);

            // Parser should still extract tokens/amounts correctly regardless of flags
            address[] memory inputTokens = parser.extractInputTokens(ONE_INCH_ROUTER, data);
            assertEq(inputTokens[0], USDC, "Input token should be USDC with non-zero flags");

            uint256[] memory amounts = parser.extractInputAmounts(ONE_INCH_ROUTER, data);
            assertEq(amounts[0], 1000e6, "Amount should be correct with non-zero flags");

            address[] memory outputTokens = parser.extractOutputTokens(ONE_INCH_ROUTER, data);
            assertEq(outputTokens[0], WETH, "Output token should be WETH with non-zero flags");
        }
    }
