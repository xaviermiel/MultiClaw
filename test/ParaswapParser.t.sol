// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {ParaswapParser} from "../src/parsers/ParaswapParser.sol";

/**
 * @title ParaswapParserTest
 * @notice Tests for the Paraswap AugustusSwapper V5/V6 parser
 */
contract ParaswapParserTest is Test {
    ParaswapParser public parser;

    // Test addresses
    address constant AUGUSTUS_V6 = 0x6A000F20005980200259B80c5102003040001068;
    address constant AUGUSTUS_V5 = 0xDEF171Fe48CF0115B1d80b88dc8eAB59176FEe57;
    address constant USDC = 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48;
    address constant WETH = 0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2;
    address constant DAI = 0x6B175474e89094c44da98B954EedeaCB5BE3D86E;
    address constant EXECUTOR = address(0xABCD);
    address constant BENEFICIARY = address(0xBEEF);
    address constant PARTNER = address(0xCAFE);
    address constant DEFAULT_RECIPIENT = address(0x9999);

    uint256 constant FROM_AMOUNT = 1000e6;
    uint256 constant TO_AMOUNT = 5e17;
    uint256 constant QUOTED_AMOUNT = 4.9e17;
    bytes32 constant METADATA = bytes32(uint256(0x123456));

    function setUp() public {
        parser = new ParaswapParser();
    }

    // ============ Selector Tests ============

    function testSelectors() public view {
        assertEq(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR(), bytes4(0xe3ead59e), "swapExactAmountIn selector mismatch");
        assertEq(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR(), bytes4(0x4c1ca4e9), "swapExactAmountOut selector mismatch");
        assertEq(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR(), bytes4(0x54840d1a), "swapExactInUniV2 selector mismatch");
        assertEq(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR(), bytes4(0x876a02f6), "swapExactInUniV3 selector mismatch");
        assertEq(parser.SIMPLE_SWAP_SELECTOR(), bytes4(0x54e3f31b), "simpleSwap selector mismatch");
        assertEq(parser.MULTI_SWAP_SELECTOR(), bytes4(0xa94e78ef), "multiSwap selector mismatch");
        assertEq(parser.MEGA_SWAP_SELECTOR(), bytes4(0x46c67b6d), "megaSwap selector mismatch");
    }

    function testSupportsSelector() public view {
        assertTrue(parser.supportsSelector(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR()), "Should support swapExactAmountIn");
        assertTrue(
            parser.supportsSelector(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR()), "Should support swapExactAmountOut"
        );
        assertTrue(
            parser.supportsSelector(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR()), "Should support swapExactInUniV2"
        );
        assertTrue(
            parser.supportsSelector(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR()), "Should support swapExactInUniV3"
        );
        assertTrue(parser.supportsSelector(parser.SIMPLE_SWAP_SELECTOR()), "Should support simpleSwap");
        assertTrue(parser.supportsSelector(parser.MULTI_SWAP_SELECTOR()), "Should support multiSwap");
        assertTrue(parser.supportsSelector(parser.MEGA_SWAP_SELECTOR()), "Should support megaSwap");
        assertFalse(parser.supportsSelector(bytes4(0xdeadbeef)), "Should not support unknown");
    }

    // ============ Helper: V6 SwapData Calldata Builders ============

    /// @dev Builds calldata for swapExactAmountIn / swapExactAmountOut
    ///      Layout: selector(4) | executor(32) | swapDataOffset(32) | ... | SwapData(7 * 32 = 224)
    ///      The offset at position 36 points to the start of SwapData relative to after the selector.
    function _buildV6SwapExactCalldata(bytes4 selector) internal pure returns (bytes memory) {
        // We manually encode: selector + executor + offset + SwapData fields
        // offset = 64 (pointing past executor(32) + offset(32) to SwapData start)
        return abi.encodePacked(
            selector,
            // executor (padded to 32 bytes)
            bytes32(uint256(uint160(EXECUTOR))),
            // offset to swapData (relative to start of params = after selector)
            // executor occupies slot 0 (offset 0), swapDataOffset occupies slot 1 (offset 32)
            // SwapData starts at slot 2 (offset 64)
            uint256(64),
            // SwapData struct fields (7 fields, each 32 bytes):
            // [0] srcToken
            bytes32(uint256(uint160(USDC))),
            // [1] destToken
            bytes32(uint256(uint160(WETH))),
            // [2] fromAmount
            uint256(FROM_AMOUNT),
            // [3] toAmount
            uint256(TO_AMOUNT),
            // [4] quotedAmount
            uint256(QUOTED_AMOUNT),
            // [5] metadata
            METADATA,
            // [6] beneficiary
            bytes32(uint256(uint160(BENEFICIARY)))
        );
    }

    /// @dev Builds calldata for swapExactAmountInOnUniswapV2 / V3
    ///      Layout: selector(4) | swapDataOffset(32) | ... | SwapData(7 * 32 = 224)
    ///      No executor prefix - swapData is the first parameter.
    function _buildV6UniswapSwapCalldata(bytes4 selector) internal pure returns (bytes memory) {
        // offset = 32 (pointing past the offset slot itself to SwapData start)
        return abi.encodePacked(
            selector,
            // offset to swapData (first param slot)
            // swapDataOffset occupies slot 0 (offset 0), SwapData starts at slot 1 (offset 32)
            uint256(32),
            // SwapData struct fields (7 fields):
            bytes32(uint256(uint160(USDC))),
            bytes32(uint256(uint160(WETH))),
            uint256(FROM_AMOUNT),
            uint256(TO_AMOUNT),
            uint256(QUOTED_AMOUNT),
            METADATA,
            bytes32(uint256(uint160(BENEFICIARY)))
        );
    }

    // ============ Helper: V5 Calldata Builders ============

    /// @dev Builds calldata for simpleSwap(SimpleData)
    ///      SimpleData: (fromToken, toToken, fromAmount, toAmount, expectedAmount,
    ///                   callees[], exchangeData, startIndexes[], values[], beneficiary, partner, feePercent, permit, deadline, uuid)
    ///      The beneficiary is at field index 9 (offset 288 from struct start).
    function _buildSimpleSwapCalldata() internal pure returns (bytes memory) {
        // We need enough data up to beneficiary (field 9, offset 288) + 32 = 320 from struct start
        // Plus selector(4) + dataOffset(32) = 36 before struct
        // Total minimum: 356 bytes

        // We encode: selector + dataOffset + struct fields up to at least beneficiary
        return abi.encodePacked(
            bytes4(0x54e3f31b), // SIMPLE_SWAP_SELECTOR
            // offset to SimpleData (32 bytes past the offset slot)
            uint256(32),
            // SimpleData fields:
            // [0] fromToken (offset 0)
            bytes32(uint256(uint160(USDC))),
            // [1] toToken (offset 32)
            bytes32(uint256(uint160(WETH))),
            // [2] fromAmount (offset 64)
            uint256(FROM_AMOUNT),
            // [3] toAmount (offset 96)
            uint256(TO_AMOUNT),
            // [4] expectedAmount (offset 128)
            uint256(QUOTED_AMOUNT),
            // [5] callees[] offset (offset 160) - point to empty array after beneficiary
            uint256(448),
            // [6] exchangeData offset (offset 192)
            uint256(480),
            // [7] startIndexes[] offset (offset 224)
            uint256(512),
            // [8] values[] offset (offset 256)
            uint256(544),
            // [9] beneficiary (offset 288)
            bytes32(uint256(uint160(BENEFICIARY))),
            // [10] partner (offset 320)
            bytes32(uint256(uint160(PARTNER))),
            // [11] feePercent (offset 352)
            uint256(0),
            // [12] permit offset (offset 384)
            uint256(576),
            // [13] deadline (offset 416)
            uint256(1700000000),
            // callees[] (empty)
            uint256(0),
            // exchangeData (empty)
            uint256(0),
            // startIndexes[] (empty)
            uint256(0),
            // values[] (empty)
            uint256(0),
            // permit (empty)
            uint256(0)
        );
    }

    /// @dev Builds calldata for multiSwap(MultiSwapData) / megaSwap(MegaSwapData)
    ///      Struct: (fromToken, toToken, fromAmount, toAmount, beneficiary, ...)
    ///      beneficiary at field 4 (offset 128 from struct start).
    function _buildMultiMegaSwapCalldata(bytes4 selector) internal pure returns (bytes memory) {
        return abi.encodePacked(
            selector,
            // offset to data struct
            uint256(32),
            // Struct fields:
            // [0] fromToken (offset 0)
            bytes32(uint256(uint160(USDC))),
            // [1] toToken (offset 32)
            bytes32(uint256(uint160(WETH))),
            // [2] fromAmount (offset 64)
            uint256(FROM_AMOUNT),
            // [3] toAmount (offset 96)
            uint256(TO_AMOUNT),
            // [4] beneficiary (offset 128)
            bytes32(uint256(uint160(BENEFICIARY))),
            // padding to ensure enough length
            uint256(0)
        );
    }

    // ============ V6 swapExactAmountIn Tests ============

    function testSwapExactAmountInExtractInputTokens() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR());

        address[] memory tokens = parser.extractInputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testSwapExactAmountInExtractInputAmounts() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR());

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V6, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], FROM_AMOUNT, "Input amount should match fromAmount");
    }

    function testSwapExactAmountInExtractOutputTokens() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR());

        address[] memory tokens = parser.extractOutputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testSwapExactAmountInExtractRecipient() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR());

        address recipient = parser.extractRecipient(AUGUSTUS_V6, data, DEFAULT_RECIPIENT);
        assertEq(recipient, BENEFICIARY, "Recipient should be beneficiary");
    }

    function testSwapExactAmountInGetOperationType() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR());

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 1, "swapExactAmountIn should be SWAP (1)");
    }

    // ============ V6 swapExactAmountOut Tests ============

    function testSwapExactAmountOutExtractInputTokens() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR());

        address[] memory tokens = parser.extractInputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testSwapExactAmountOutExtractInputAmounts() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR());

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V6, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], FROM_AMOUNT, "Input amount should match fromAmount");
    }

    function testSwapExactAmountOutExtractOutputTokens() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR());

        address[] memory tokens = parser.extractOutputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testSwapExactAmountOutExtractRecipient() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR());

        address recipient = parser.extractRecipient(AUGUSTUS_V6, data, DEFAULT_RECIPIENT);
        assertEq(recipient, BENEFICIARY, "Recipient should be beneficiary");
    }

    function testSwapExactAmountOutGetOperationType() public view {
        bytes memory data = _buildV6SwapExactCalldata(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR());

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 1, "swapExactAmountOut should be SWAP (1)");
    }

    // ============ V6 swapExactAmountInOnUniswapV2 Tests ============

    function testSwapExactInUniswapV2ExtractInputTokens() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR());

        address[] memory tokens = parser.extractInputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testSwapExactInUniswapV2ExtractInputAmounts() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR());

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V6, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], FROM_AMOUNT, "Input amount should match fromAmount");
    }

    function testSwapExactInUniswapV2ExtractOutputTokens() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR());

        address[] memory tokens = parser.extractOutputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testSwapExactInUniswapV2ExtractRecipient() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR());

        address recipient = parser.extractRecipient(AUGUSTUS_V6, data, DEFAULT_RECIPIENT);
        assertEq(recipient, BENEFICIARY, "Recipient should be beneficiary");
    }

    function testSwapExactInUniswapV2GetOperationType() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR());

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 1, "swapExactInUniV2 should be SWAP (1)");
    }

    // ============ V6 swapExactAmountInOnUniswapV3 Tests ============

    function testSwapExactInUniswapV3ExtractInputTokens() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR());

        address[] memory tokens = parser.extractInputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testSwapExactInUniswapV3ExtractInputAmounts() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR());

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V6, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], FROM_AMOUNT, "Input amount should match fromAmount");
    }

    function testSwapExactInUniswapV3ExtractOutputTokens() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR());

        address[] memory tokens = parser.extractOutputTokens(AUGUSTUS_V6, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testSwapExactInUniswapV3ExtractRecipient() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR());

        address recipient = parser.extractRecipient(AUGUSTUS_V6, data, DEFAULT_RECIPIENT);
        assertEq(recipient, BENEFICIARY, "Recipient should be beneficiary");
    }

    function testSwapExactInUniswapV3GetOperationType() public view {
        bytes memory data = _buildV6UniswapSwapCalldata(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR());

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 1, "swapExactInUniV3 should be SWAP (1)");
    }

    // ============ V5 simpleSwap Tests ============

    function testSimpleSwapExtractInputTokens() public view {
        bytes memory data = _buildSimpleSwapCalldata();

        address[] memory tokens = parser.extractInputTokens(AUGUSTUS_V5, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testSimpleSwapExtractInputAmounts() public view {
        bytes memory data = _buildSimpleSwapCalldata();

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V5, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], FROM_AMOUNT, "Input amount should match fromAmount");
    }

    function testSimpleSwapExtractOutputTokens() public view {
        bytes memory data = _buildSimpleSwapCalldata();

        address[] memory tokens = parser.extractOutputTokens(AUGUSTUS_V5, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testSimpleSwapExtractRecipient() public view {
        bytes memory data = _buildSimpleSwapCalldata();

        address recipient = parser.extractRecipient(AUGUSTUS_V5, data, DEFAULT_RECIPIENT);
        assertEq(recipient, BENEFICIARY, "Recipient should be beneficiary");
    }

    function testSimpleSwapGetOperationType() public view {
        bytes memory data = _buildSimpleSwapCalldata();

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 1, "simpleSwap should be SWAP (1)");
    }

    // ============ V5 multiSwap Tests ============

    function testMultiSwapExtractInputTokens() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MULTI_SWAP_SELECTOR());

        address[] memory tokens = parser.extractInputTokens(AUGUSTUS_V5, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testMultiSwapExtractInputAmounts() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MULTI_SWAP_SELECTOR());

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V5, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], FROM_AMOUNT, "Input amount should match fromAmount");
    }

    function testMultiSwapExtractOutputTokens() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MULTI_SWAP_SELECTOR());

        address[] memory tokens = parser.extractOutputTokens(AUGUSTUS_V5, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testMultiSwapExtractRecipient() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MULTI_SWAP_SELECTOR());

        address recipient = parser.extractRecipient(AUGUSTUS_V5, data, DEFAULT_RECIPIENT);
        assertEq(recipient, BENEFICIARY, "Recipient should be beneficiary");
    }

    function testMultiSwapGetOperationType() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MULTI_SWAP_SELECTOR());

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 1, "multiSwap should be SWAP (1)");
    }

    // ============ V5 megaSwap Tests ============

    function testMegaSwapExtractInputTokens() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MEGA_SWAP_SELECTOR());

        address[] memory tokens = parser.extractInputTokens(AUGUSTUS_V5, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testMegaSwapExtractInputAmounts() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MEGA_SWAP_SELECTOR());

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V5, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], FROM_AMOUNT, "Input amount should match fromAmount");
    }

    function testMegaSwapExtractOutputTokens() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MEGA_SWAP_SELECTOR());

        address[] memory tokens = parser.extractOutputTokens(AUGUSTUS_V5, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testMegaSwapExtractRecipient() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MEGA_SWAP_SELECTOR());

        address recipient = parser.extractRecipient(AUGUSTUS_V5, data, DEFAULT_RECIPIENT);
        assertEq(recipient, BENEFICIARY, "Recipient should be beneficiary");
    }

    function testMegaSwapGetOperationType() public view {
        bytes memory data = _buildMultiMegaSwapCalldata(parser.MEGA_SWAP_SELECTOR());

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 1, "megaSwap should be SWAP (1)");
    }

    // ============ Operation Type: Unknown Selector ============

    function testGetOperationTypeUnknownSelector() public view {
        bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef));

        uint8 opType = parser.getOperationType(data);
        assertEq(opType, 0, "Unknown selector should return 0");
    }

    // ============ Revert Tests: UnsupportedSelector ============

    function testUnsupportedSelectorRevertsExtractInputTokens() public {
        bytes memory badData = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(ParaswapParser.UnsupportedSelector.selector);
        parser.extractInputTokens(AUGUSTUS_V6, badData);
    }

    function testUnsupportedSelectorRevertsExtractInputAmounts() public {
        bytes memory badData = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(ParaswapParser.UnsupportedSelector.selector);
        parser.extractInputAmounts(AUGUSTUS_V6, badData);
    }

    function testUnsupportedSelectorRevertsExtractOutputTokens() public {
        bytes memory badData = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(ParaswapParser.UnsupportedSelector.selector);
        parser.extractOutputTokens(AUGUSTUS_V6, badData);
    }

    function testUnsupportedSelectorRevertsExtractRecipient() public {
        bytes memory badData = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(ParaswapParser.UnsupportedSelector.selector);
        parser.extractRecipient(AUGUSTUS_V6, badData, DEFAULT_RECIPIENT);
    }

    // ============ Revert Tests: InvalidCalldata (too short) ============

    function testInvalidCalldataRevertsOnEmptyData() public {
        bytes memory emptyData = "";

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V6, emptyData);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputAmounts(AUGUSTUS_V6, emptyData);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(AUGUSTUS_V6, emptyData);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractRecipient(AUGUSTUS_V6, emptyData, DEFAULT_RECIPIENT);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.getOperationType(emptyData);
    }

    function testInvalidCalldataRevertsOnShortData() public {
        bytes memory shortData = hex"e3ea";

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V6, shortData);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputAmounts(AUGUSTUS_V6, shortData);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(AUGUSTUS_V6, shortData);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractRecipient(AUGUSTUS_V6, shortData, DEFAULT_RECIPIENT);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.getOperationType(shortData);
    }

    function testInvalidCalldataSwapExactAmountInTooShort() public {
        // Valid selector but not enough data for swapExactAmountIn (needs 292 bytes)
        bytes memory data = abi.encodeWithSelector(parser.SWAP_EXACT_AMOUNT_IN_SELECTOR(), uint256(0));

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V6, data);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputAmounts(AUGUSTUS_V6, data);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(AUGUSTUS_V6, data);

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractRecipient(AUGUSTUS_V6, data, DEFAULT_RECIPIENT);
    }

    function testInvalidCalldataSwapExactAmountOutTooShort() public {
        bytes memory data = abi.encodeWithSelector(parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR(), uint256(0));

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V6, data);
    }

    function testInvalidCalldataUniswapV2TooShort() public {
        bytes memory data = abi.encodeWithSelector(parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR(), uint256(0));

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V6, data);
    }

    function testInvalidCalldataUniswapV3TooShort() public {
        bytes memory data = abi.encodeWithSelector(parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR(), uint256(0));

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V6, data);
    }

    function testInvalidCalldataSimpleSwapTooShort() public {
        // simpleSwap needs 356 bytes minimum
        bytes memory data = abi.encodeWithSelector(parser.SIMPLE_SWAP_SELECTOR(), uint256(0));

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V5, data);
    }

    function testInvalidCalldataMultiSwapTooShort() public {
        // multiSwap needs 196 bytes minimum
        bytes memory data = abi.encodeWithSelector(parser.MULTI_SWAP_SELECTOR(), uint256(0));

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V5, data);
    }

    function testInvalidCalldataMegaSwapTooShort() public {
        // megaSwap needs 196 bytes minimum
        bytes memory data = abi.encodeWithSelector(parser.MEGA_SWAP_SELECTOR(), uint256(0));

        vm.expectRevert(ParaswapParser.InvalidCalldata.selector);
        parser.extractInputTokens(AUGUSTUS_V5, data);
    }

    // ============ Cross-Selector Consistency Tests ============

    function testAllV6SelectorsReturnSwapOpType() public view {
        bytes4[4] memory v6Selectors = [
            parser.SWAP_EXACT_AMOUNT_IN_SELECTOR(),
            parser.SWAP_EXACT_AMOUNT_OUT_SELECTOR(),
            parser.SWAP_EXACT_IN_UNISWAP_V2_SELECTOR(),
            parser.SWAP_EXACT_IN_UNISWAP_V3_SELECTOR()
        ];

        for (uint256 i = 0; i < v6Selectors.length; i++) {
            bytes memory data = abi.encodeWithSelector(v6Selectors[i]);
            assertEq(parser.getOperationType(data), 1, "All V6 selectors should be SWAP (1)");
        }
    }

    function testAllV5SelectorsReturnSwapOpType() public view {
        bytes4[3] memory v5Selectors =
            [parser.SIMPLE_SWAP_SELECTOR(), parser.MULTI_SWAP_SELECTOR(), parser.MEGA_SWAP_SELECTOR()];

        for (uint256 i = 0; i < v5Selectors.length; i++) {
            bytes memory data = abi.encodeWithSelector(v5Selectors[i]);
            assertEq(parser.getOperationType(data), 1, "All V5 selectors should be SWAP (1)");
        }
    }

    // ============ Edge Case: Different Token Pairs ============

    function testSwapExactAmountInWithDaiToWeth() public view {
        // Build calldata with DAI -> WETH instead of USDC -> WETH
        bytes memory data = abi.encodePacked(
            parser.SWAP_EXACT_AMOUNT_IN_SELECTOR(),
            bytes32(uint256(uint160(EXECUTOR))),
            uint256(64),
            bytes32(uint256(uint160(DAI))),
            bytes32(uint256(uint160(WETH))),
            uint256(500e18),
            uint256(TO_AMOUNT),
            uint256(QUOTED_AMOUNT),
            METADATA,
            bytes32(uint256(uint160(BENEFICIARY)))
        );

        address[] memory inputTokens = parser.extractInputTokens(AUGUSTUS_V6, data);
        assertEq(inputTokens[0], DAI, "Input token should be DAI");

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V6, data);
        assertEq(amounts[0], 500e18, "Input amount should be 500e18");

        address[] memory outputTokens = parser.extractOutputTokens(AUGUSTUS_V6, data);
        assertEq(outputTokens[0], WETH, "Output token should be WETH");
    }

    function testMultiSwapWithWethToUsdc() public view {
        // Build calldata with WETH -> USDC
        bytes memory data = abi.encodePacked(
            parser.MULTI_SWAP_SELECTOR(),
            uint256(32),
            bytes32(uint256(uint160(WETH))),
            bytes32(uint256(uint160(USDC))),
            uint256(1e18),
            uint256(2000e6),
            bytes32(uint256(uint160(BENEFICIARY))),
            uint256(0)
        );

        address[] memory inputTokens = parser.extractInputTokens(AUGUSTUS_V5, data);
        assertEq(inputTokens[0], WETH, "Input token should be WETH");

        address[] memory outputTokens = parser.extractOutputTokens(AUGUSTUS_V5, data);
        assertEq(outputTokens[0], USDC, "Output token should be USDC");

        uint256[] memory amounts = parser.extractInputAmounts(AUGUSTUS_V5, data);
        assertEq(amounts[0], 1e18, "Input amount should be 1e18");
    }
}
