// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {KyberSwapParser} from "../src/parsers/KyberSwapParser.sol";

/**
 * @title KyberSwapParserTest
 * @notice Comprehensive tests for the KyberSwap MetaAggregationRouterV2 parser
 * @dev The parser uses inline assembly to navigate ABI-encoded structs from calldata.
 *      The helpers in this test manually construct calldata matching the exact byte layout
 *      that the real KyberSwap MetaAggregationRouterV2 would produce.
 *
 *      swap / swapGeneric: selector + abi.encode(SwapExecutionParams)
 *        SwapExecutionParams head (5 slots): callTarget | approveTarget | targetData_offset | desc_offset | clientData_offset
 *        desc head (11 slots): srcToken | dstToken | srcReceivers_off | srcAmounts_off | feeReceivers_off | feeAmounts_off | dstReceiver | amount | minReturnAmount | flags | permit_off
 *
 *      swapSimpleMode: selector + abi.encode(address caller, SwapDescription desc, bytes executorData, bytes clientData)
 *        Outer head (4 slots): caller | desc_offset | executorData_offset | clientData_offset
 *        desc head is the same 11-slot layout.
 */
contract KyberSwapParserTest is Test {
    KyberSwapParser public parser;

    // Selector constants (matching KyberSwapParser)
    bytes4 constant SWAP_SEL = 0xe21fd0e9;
    bytes4 constant SWAP_SIMPLE_MODE_SEL = 0x8af033fb;
    bytes4 constant SWAP_GENERIC_SEL = 0x59e50fed;

    // Test addresses (realistic mainnet-style)
    address constant KYBER_ROUTER = 0x6131B5fae19EA4f9D964eAc0408E4408b66337b5;
    address constant USDC = 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48;
    address constant WETH = 0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2;
    address constant DAI = 0x6B175474E89094C44Da98b954EedeAC495271d0F;
    address constant USER = address(0xBEEF);
    address constant RECIPIENT = address(0xCAFE);
    address constant CALL_TARGET = address(0xAA);
    address constant APPROVE_TARGET = address(0xBB);

    function setUp() public {
        parser = new KyberSwapParser();
    }

    // ============ Calldata construction helpers ============

    /**
     * @dev Builds the SwapDescription tuple head + tail.
     *      Head (11 x 32-byte slots):
     *        [0]  srcToken          (address)
     *        [1]  dstToken          (address)
     *        [2]  srcReceivers off  -> dynamic array offset from desc head
     *        [3]  srcAmounts off    -> dynamic array offset from desc head
     *        [4]  feeReceivers off  -> dynamic array offset from desc head
     *        [5]  feeAmounts off    -> dynamic array offset from desc head
     *        [6]  dstReceiver       (address)
     *        [7]  amount            (uint256)
     *        [8]  minReturnAmount   (uint256)
     *        [9]  flags             (uint256)
     *        [10] permit off        -> dynamic bytes offset from desc head
     *      Tail: dynamic array/bytes data (we use minimal placeholders).
     *
     *      Head size = 11 * 32 = 352. Offsets point past the head into the tail.
     */
    function _buildSwapDescriptionBytes(
        address srcToken,
        address dstToken,
        address dstReceiver,
        uint256 amount,
        uint256 minReturnAmount
    ) internal pure returns (bytes memory) {
        // Head size = 11 * 32 = 352
        // Tail layout (all at offset 352+):
        //   offset 352: srcReceivers  -> length(1) + 1 element = 64 bytes
        //   offset 416: srcAmounts    -> length(1) + 1 element = 64 bytes
        //   offset 480: feeReceivers  -> length(0) = 32 bytes
        //   offset 512: feeAmounts    -> length(0) = 32 bytes
        //   offset 544: permit        -> length(0) = 32 bytes

        uint256 headSize = 352;
        uint256 srcReceiversOff = headSize; // 352
        uint256 srcAmountsOff = srcReceiversOff + 64; // 416
        uint256 feeReceiversOff = srcAmountsOff + 64; // 480
        uint256 feeAmountsOff = feeReceiversOff + 32; // 512
        uint256 permitOff = feeAmountsOff + 32; // 544

        return abi.encodePacked(
            // Head (11 slots)
            bytes32(uint256(uint160(srcToken))), // [0] srcToken
            bytes32(uint256(uint160(dstToken))), // [1] dstToken
            bytes32(srcReceiversOff), // [2] srcReceivers offset
            bytes32(srcAmountsOff), // [3] srcAmounts offset
            bytes32(feeReceiversOff), // [4] feeReceivers offset
            bytes32(feeAmountsOff), // [5] feeAmounts offset
            bytes32(uint256(uint160(dstReceiver))), // [6] dstReceiver
            bytes32(amount), // [7] amount
            bytes32(minReturnAmount), // [8] minReturnAmount
            bytes32(uint256(0)), // [9] flags
            bytes32(permitOff), // [10] permit offset
            // Tail
            // srcReceivers: length=1, element=0xCC
            bytes32(uint256(1)),
            bytes32(uint256(uint160(address(0xCC)))),
            // srcAmounts: length=1, element=amount
            bytes32(uint256(1)),
            bytes32(amount),
            // feeReceivers: length=0
            bytes32(uint256(0)),
            // feeAmounts: length=0
            bytes32(uint256(0)),
            // permit: length=0
            bytes32(uint256(0))
        );
    }

    /**
     * @dev Builds calldata for swap(SwapExecutionParams execution) or swapGeneric(SwapExecutionParams execution).
     *
     *      ABI layout from the perspective of the parser:
     *        data[0..3]:   selector
     *        data[4..35]:  offset to execution tuple (= 32, since it's the sole param)
     *        data[36..]:   execution tuple
     *
     *      Execution tuple head (5 slots, starting at execution offset):
     *        [0] callTarget           (address, static)
     *        [1] approveTarget        (address, static)
     *        [2] targetData offset    (relative to execution start)
     *        [3] desc offset          (relative to execution start) -- this is what the parser reads at +96
     *        [4] clientData offset    (relative to execution start)
     *
     *      Execution tail: targetData bytes, desc tuple, clientData bytes
     */
    function _buildSwapCalldata(
        bytes4 sel,
        address srcToken,
        address dstToken,
        address dstReceiver,
        uint256 amount,
        uint256 minReturnAmount
    ) internal pure returns (bytes memory) {
        bytes memory descBytes = _buildSwapDescriptionBytes(srcToken, dstToken, dstReceiver, amount, minReturnAmount);

        // Execution head = 5 slots = 160 bytes
        uint256 execHeadSize = 160;

        // targetData at offset execHeadSize (160)
        // targetData = length(4) + 4 bytes padded to 32 = 64 bytes total (length slot + data slot)
        uint256 targetDataOff = execHeadSize;
        uint256 targetDataSize = 64; // 32 (length) + 32 (padded 4 bytes)

        // desc at offset after targetData
        uint256 descOff = targetDataOff + targetDataSize; // 224

        // clientData at offset after desc
        uint256 clientDataOff = descOff + descBytes.length;

        return abi.encodePacked(
            sel,
            // Outer abi.encode: offset to execution tuple = 32
            bytes32(uint256(32)),
            // === Execution tuple ===
            // Head (5 slots)
            bytes32(uint256(uint160(CALL_TARGET))), // [0] callTarget
            bytes32(uint256(uint160(APPROVE_TARGET))), // [1] approveTarget
            bytes32(targetDataOff), // [2] targetData offset
            bytes32(descOff), // [3] desc offset (parser reads this at execOffset+96)
            bytes32(clientDataOff), // [4] clientData offset
            // Tail
            // targetData (bytes): length + data
            bytes32(uint256(4)), // length = 4
            bytes4(0xaabbccdd),
            bytes28(0), // data padded to 32 bytes
            // desc (tuple): inline tuple data
            descBytes,
            // clientData (bytes): length=0
            bytes32(uint256(0))
        );
    }

    /// @dev Convenience wrappers for swap and swapGeneric
    function _buildSwapCalldata(
        address srcToken,
        address dstToken,
        address dstReceiver,
        uint256 amount,
        uint256 minReturnAmount
    ) internal pure returns (bytes memory) {
        return _buildSwapCalldata(SWAP_SEL, srcToken, dstToken, dstReceiver, amount, minReturnAmount);
    }

    function _buildSwapGenericCalldata(
        address srcToken,
        address dstToken,
        address dstReceiver,
        uint256 amount,
        uint256 minReturnAmount
    ) internal pure returns (bytes memory) {
        return _buildSwapCalldata(SWAP_GENERIC_SEL, srcToken, dstToken, dstReceiver, amount, minReturnAmount);
    }

    /**
     * @dev Builds calldata for swapSimpleMode(address caller, SwapDescription desc, bytes executorData, bytes clientData).
     *
     *      ABI layout:
     *        data[0..3]:    selector
     *        data[4..35]:   caller (address, static)
     *        data[36..67]:  desc offset     -- parser reads calldataload(data.offset + 36)
     *        data[68..99]:  executorData offset
     *        data[100..131]: clientData offset
     *        data[132..]:   dynamic data (desc tuple, executorData bytes, clientData bytes)
     *
     *      The parser: descOffset = data.offset + 4 + calldataload(data.offset + 36)
     *      Then reads desc fields from descOffset.
     */
    function _buildSwapSimpleModeCalldata(
        address srcToken,
        address dstToken,
        address dstReceiver,
        uint256 amount,
        uint256 minReturnAmount
    ) internal pure returns (bytes memory) {
        bytes memory descBytes = _buildSwapDescriptionBytes(srcToken, dstToken, dstReceiver, amount, minReturnAmount);

        // Outer head: 4 slots (caller is static, desc/executorData/clientData are offsets)
        uint256 outerHeadSize = 128; // 4 * 32

        // desc starts right after outer head
        uint256 descOff = outerHeadSize;
        // executorData starts after desc
        uint256 executorDataOff = descOff + descBytes.length;
        // clientData starts after executorData (length=2 -> 32 + 32 = 64 bytes)
        uint256 clientDataOff = executorDataOff + 64;

        return abi.encodePacked(
            SWAP_SIMPLE_MODE_SEL,
            // Outer head (4 slots)
            bytes32(uint256(uint160(USER))), // caller (static)
            bytes32(descOff), // desc offset (from start of params, i.e. after selector)
            bytes32(executorDataOff), // executorData offset
            bytes32(clientDataOff), // clientData offset
            // Tail
            // desc (tuple)
            descBytes,
            // executorData (bytes): length=2 + padded data
            bytes32(uint256(2)),
            bytes2(0x1122),
            bytes30(0),
            // clientData (bytes): length=0
            bytes32(uint256(0))
        );
    }

    // ============ Selector Tests ============

    function testSelectors() public view {
        assertEq(parser.SWAP_SELECTOR(), bytes4(0xe21fd0e9), "SWAP selector mismatch");
        assertEq(parser.SWAP_SIMPLE_MODE_SELECTOR(), bytes4(0x8af033fb), "SWAP_SIMPLE_MODE selector mismatch");
        assertEq(parser.SWAP_GENERIC_SELECTOR(), bytes4(0x59e50fed), "SWAP_GENERIC selector mismatch");
    }

    function testSupportsSelector() public view {
        assertTrue(parser.supportsSelector(parser.SWAP_SELECTOR()), "Should support swap");
        assertTrue(parser.supportsSelector(parser.SWAP_SIMPLE_MODE_SELECTOR()), "Should support swapSimpleMode");
        assertTrue(parser.supportsSelector(parser.SWAP_GENERIC_SELECTOR()), "Should support swapGeneric");
        assertFalse(parser.supportsSelector(bytes4(0xdeadbeef)), "Should not support unknown selector");
        assertFalse(parser.supportsSelector(bytes4(0x00000000)), "Should not support zero selector");
    }

    // ============ swap() Tests ============

    function testSwapExtractInputTokens() public view {
        bytes memory data = _buildSwapCalldata(USDC, WETH, RECIPIENT, 1000e6, 0.5e18);

        address[] memory tokens = parser.extractInputTokens(KYBER_ROUTER, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], USDC, "Input token should be USDC");
    }

    function testSwapExtractInputAmounts() public view {
        bytes memory data = _buildSwapCalldata(USDC, WETH, RECIPIENT, 1000e6, 0.5e18);

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], 1000e6, "Input amount should be 1000e6");
    }

    function testSwapExtractOutputTokens() public view {
        bytes memory data = _buildSwapCalldata(USDC, WETH, RECIPIENT, 1000e6, 0.5e18);

        address[] memory tokens = parser.extractOutputTokens(KYBER_ROUTER, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], WETH, "Output token should be WETH");
    }

    function testSwapExtractRecipient() public view {
        bytes memory data = _buildSwapCalldata(USDC, WETH, RECIPIENT, 1000e6, 0.5e18);

        address recipient = parser.extractRecipient(KYBER_ROUTER, data, address(0));
        assertEq(recipient, RECIPIENT, "Recipient should be RECIPIENT");
    }

    function testSwapGetOperationType() public view {
        bytes memory data = _buildSwapCalldata(USDC, WETH, RECIPIENT, 1000e6, 0.5e18);
        assertEq(parser.getOperationType(data), 1, "swap should be SWAP (1)");
    }

    // ============ swapGeneric() Tests ============

    function testSwapGenericExtractInputTokens() public view {
        bytes memory data = _buildSwapGenericCalldata(WETH, DAI, RECIPIENT, 2e18, 5000e18);

        address[] memory tokens = parser.extractInputTokens(KYBER_ROUTER, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], WETH, "Input token should be WETH");
    }

    function testSwapGenericExtractInputAmounts() public view {
        bytes memory data = _buildSwapGenericCalldata(WETH, DAI, RECIPIENT, 2e18, 5000e18);

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], 2e18, "Input amount should be 2e18");
    }

    function testSwapGenericExtractOutputTokens() public view {
        bytes memory data = _buildSwapGenericCalldata(WETH, DAI, RECIPIENT, 2e18, 5000e18);

        address[] memory tokens = parser.extractOutputTokens(KYBER_ROUTER, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], DAI, "Output token should be DAI");
    }

    function testSwapGenericExtractRecipient() public view {
        bytes memory data = _buildSwapGenericCalldata(WETH, DAI, RECIPIENT, 2e18, 5000e18);

        address recipient = parser.extractRecipient(KYBER_ROUTER, data, address(0));
        assertEq(recipient, RECIPIENT, "Recipient should be RECIPIENT");
    }

    function testSwapGenericGetOperationType() public view {
        bytes memory data = _buildSwapGenericCalldata(WETH, DAI, RECIPIENT, 2e18, 5000e18);
        assertEq(parser.getOperationType(data), 1, "swapGeneric should be SWAP (1)");
    }

    // ============ swapSimpleMode() Tests ============

    function testSwapSimpleModeExtractInputTokens() public view {
        bytes memory data = _buildSwapSimpleModeCalldata(DAI, USDC, RECIPIENT, 5000e18, 4900e6);

        address[] memory tokens = parser.extractInputTokens(KYBER_ROUTER, data);
        assertEq(tokens.length, 1, "Should have 1 input token");
        assertEq(tokens[0], DAI, "Input token should be DAI");
    }

    function testSwapSimpleModeExtractInputAmounts() public view {
        bytes memory data = _buildSwapSimpleModeCalldata(DAI, USDC, RECIPIENT, 5000e18, 4900e6);

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts.length, 1, "Should have 1 input amount");
        assertEq(amounts[0], 5000e18, "Input amount should be 5000e18");
    }

    function testSwapSimpleModeExtractOutputTokens() public view {
        bytes memory data = _buildSwapSimpleModeCalldata(DAI, USDC, RECIPIENT, 5000e18, 4900e6);

        address[] memory tokens = parser.extractOutputTokens(KYBER_ROUTER, data);
        assertEq(tokens.length, 1, "Should have 1 output token");
        assertEq(tokens[0], USDC, "Output token should be USDC");
    }

    function testSwapSimpleModeExtractRecipient() public view {
        bytes memory data = _buildSwapSimpleModeCalldata(DAI, USDC, RECIPIENT, 5000e18, 4900e6);

        address recipient = parser.extractRecipient(KYBER_ROUTER, data, address(0));
        assertEq(recipient, RECIPIENT, "Recipient should be RECIPIENT");
    }

    function testSwapSimpleModeGetOperationType() public view {
        bytes memory data = _buildSwapSimpleModeCalldata(DAI, USDC, RECIPIENT, 5000e18, 4900e6);
        assertEq(parser.getOperationType(data), 1, "swapSimpleMode should be SWAP (1)");
    }

    // ============ Different Token Pairs ============

    function testSwapDifferentTokenPair() public view {
        bytes memory data = _buildSwapCalldata(DAI, WETH, USER, 10_000e18, 3e18);

        address[] memory inputTokens = parser.extractInputTokens(KYBER_ROUTER, data);
        assertEq(inputTokens[0], DAI, "Input should be DAI");

        address[] memory outputTokens = parser.extractOutputTokens(KYBER_ROUTER, data);
        assertEq(outputTokens[0], WETH, "Output should be WETH");

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts[0], 10_000e18, "Amount should be 10000 DAI");

        address recipient = parser.extractRecipient(KYBER_ROUTER, data, address(0));
        assertEq(recipient, USER, "Recipient should be USER");
    }

    function testSwapSimpleModeDifferentRecipient() public view {
        address customRecipient = address(0xDEAD);
        bytes memory data = _buildSwapSimpleModeCalldata(WETH, USDC, customRecipient, 1e18, 2000e6);

        address recipient = parser.extractRecipient(KYBER_ROUTER, data, address(0));
        assertEq(recipient, customRecipient, "Recipient should be customRecipient");
    }

    // ============ Large Amount Tests ============

    function testSwapLargeAmount() public view {
        uint256 largeAmount = type(uint128).max;
        bytes memory data = _buildSwapCalldata(USDC, WETH, RECIPIENT, largeAmount, 1);

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts[0], largeAmount, "Should handle large amounts");
    }

    function testSwapSimpleModeLargeAmount() public view {
        uint256 largeAmount = type(uint128).max;
        bytes memory data = _buildSwapSimpleModeCalldata(DAI, USDC, RECIPIENT, largeAmount, 1);

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts[0], largeAmount, "Should handle large amounts in simple mode");
    }

    // ============ Operation Type for Unknown Selector ============

    function testGetOperationTypeUnknownSelector() public view {
        bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));
        assertEq(parser.getOperationType(data), 0, "Unknown selector should return 0 (UNKNOWN)");
    }

    // ============ Edge Case: InvalidCalldata ============

    function testExtractInputTokensRevertsOnEmptyData() public {
        bytes memory data = hex"";
        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputTokens(KYBER_ROUTER, data);
    }

    function testExtractInputTokensRevertsOnShortData() public {
        bytes memory data = hex"e21f";
        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputTokens(KYBER_ROUTER, data);
    }

    function testExtractInputAmountsRevertsOnEmptyData() public {
        bytes memory data = hex"";
        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputAmounts(KYBER_ROUTER, data);
    }

    function testExtractOutputTokensRevertsOnEmptyData() public {
        bytes memory data = hex"";
        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(KYBER_ROUTER, data);
    }

    function testExtractRecipientRevertsOnEmptyData() public {
        bytes memory data = hex"";
        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractRecipient(KYBER_ROUTER, data, address(0));
    }

    function testGetOperationTypeRevertsOnEmptyData() public {
        bytes memory data = hex"";
        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.getOperationType(data);
    }

    // ============ Edge Case: Selector-only calldata (too short for swap) ============

    function testSwapSelectorRevertsOnTooShortCalldata() public {
        // Only selector + small payload, far below MIN_SWAP_LENGTH (420)
        bytes memory data = abi.encodePacked(SWAP_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputTokens(KYBER_ROUTER, data);
    }

    function testSwapGenericSelectorRevertsOnTooShortCalldata() public {
        bytes memory data = abi.encodePacked(SWAP_GENERIC_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputAmounts(KYBER_ROUTER, data);
    }

    function testSwapSimpleModeSelectorRevertsOnTooShortCalldata() public {
        // Only selector + small payload, below MIN_SWAP_SIMPLE_LENGTH (324)
        bytes memory data = abi.encodePacked(SWAP_SIMPLE_MODE_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputTokens(KYBER_ROUTER, data);
    }

    // ============ Edge Case: UnsupportedSelector ============

    function testUnsupportedSelectorRevertsExtractInputTokens() public {
        bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(KyberSwapParser.UnsupportedSelector.selector);
        parser.extractInputTokens(KYBER_ROUTER, data);
    }

    function testUnsupportedSelectorRevertsExtractInputAmounts() public {
        bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(KyberSwapParser.UnsupportedSelector.selector);
        parser.extractInputAmounts(KYBER_ROUTER, data);
    }

    function testUnsupportedSelectorRevertsExtractOutputTokens() public {
        bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(KyberSwapParser.UnsupportedSelector.selector);
        parser.extractOutputTokens(KYBER_ROUTER, data);
    }

    function testUnsupportedSelectorRevertsExtractRecipient() public {
        bytes memory data = abi.encodeWithSelector(bytes4(0xdeadbeef), uint256(100));

        vm.expectRevert(KyberSwapParser.UnsupportedSelector.selector);
        parser.extractRecipient(KYBER_ROUTER, data, address(0));
    }

    // ============ Edge Case: Min return amount preserved ============

    function testSwapMinReturnAmountDoesNotAffectInputAmount() public view {
        uint256 inputAmount = 1000e6;
        uint256 minReturn = 999e6;
        bytes memory data = _buildSwapCalldata(USDC, DAI, RECIPIENT, inputAmount, minReturn);

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts[0], inputAmount, "Should extract input amount, not minReturn");
    }

    function testSwapSimpleModeMinReturnAmountDoesNotAffectInputAmount() public view {
        uint256 inputAmount = 5000e18;
        uint256 minReturn = 4990e6;
        bytes memory data = _buildSwapSimpleModeCalldata(DAI, USDC, RECIPIENT, inputAmount, minReturn);

        uint256[] memory amounts = parser.extractInputAmounts(KYBER_ROUTER, data);
        assertEq(amounts[0], inputAmount, "Should extract input amount, not minReturn");
    }

    // ============ All three selectors produce consistent results ============

    function testAllSelectorsConsistentResults() public view {
        address srcToken = USDC;
        address dstToken = WETH;
        address dstReceiver = RECIPIENT;
        uint256 amount = 1000e6;
        uint256 minReturn = 0.5e18;

        bytes memory swapData = _buildSwapCalldata(srcToken, dstToken, dstReceiver, amount, minReturn);
        bytes memory genericData = _buildSwapGenericCalldata(srcToken, dstToken, dstReceiver, amount, minReturn);
        bytes memory simpleData = _buildSwapSimpleModeCalldata(srcToken, dstToken, dstReceiver, amount, minReturn);

        // Input tokens
        assertEq(parser.extractInputTokens(KYBER_ROUTER, swapData)[0], srcToken, "swap input token");
        assertEq(parser.extractInputTokens(KYBER_ROUTER, genericData)[0], srcToken, "generic input token");
        assertEq(parser.extractInputTokens(KYBER_ROUTER, simpleData)[0], srcToken, "simple input token");

        // Input amounts
        assertEq(parser.extractInputAmounts(KYBER_ROUTER, swapData)[0], amount, "swap input amount");
        assertEq(parser.extractInputAmounts(KYBER_ROUTER, genericData)[0], amount, "generic input amount");
        assertEq(parser.extractInputAmounts(KYBER_ROUTER, simpleData)[0], amount, "simple input amount");

        // Output tokens
        assertEq(parser.extractOutputTokens(KYBER_ROUTER, swapData)[0], dstToken, "swap output token");
        assertEq(parser.extractOutputTokens(KYBER_ROUTER, genericData)[0], dstToken, "generic output token");
        assertEq(parser.extractOutputTokens(KYBER_ROUTER, simpleData)[0], dstToken, "simple output token");

        // Recipients
        assertEq(parser.extractRecipient(KYBER_ROUTER, swapData, address(0)), dstReceiver, "swap recipient");
        assertEq(parser.extractRecipient(KYBER_ROUTER, genericData, address(0)), dstReceiver, "generic recipient");
        assertEq(parser.extractRecipient(KYBER_ROUTER, simpleData, address(0)), dstReceiver, "simple recipient");

        // Operation types
        assertEq(parser.getOperationType(swapData), 1, "swap opType");
        assertEq(parser.getOperationType(genericData), 1, "generic opType");
        assertEq(parser.getOperationType(simpleData), 1, "simple opType");
    }

    // ============ InvalidCalldata for all functions with swap-length check ============

    function testSwapTooShortRevertsExtractOutputTokens() public {
        bytes memory data = abi.encodePacked(SWAP_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(KYBER_ROUTER, data);
    }

    function testSwapTooShortRevertsExtractRecipient() public {
        bytes memory data = abi.encodePacked(SWAP_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractRecipient(KYBER_ROUTER, data, address(0));
    }

    function testSwapSimpleModeTooShortRevertsExtractOutputTokens() public {
        bytes memory data = abi.encodePacked(SWAP_SIMPLE_MODE_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(KYBER_ROUTER, data);
    }

    function testSwapSimpleModeTooShortRevertsExtractRecipient() public {
        bytes memory data = abi.encodePacked(SWAP_SIMPLE_MODE_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractRecipient(KYBER_ROUTER, data, address(0));
    }

    function testSwapSimpleModeTooShortRevertsExtractAmounts() public {
        bytes memory data = abi.encodePacked(SWAP_SIMPLE_MODE_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputAmounts(KYBER_ROUTER, data);
    }

    function testSwapGenericTooShortRevertsExtractOutputTokens() public {
        bytes memory data = abi.encodePacked(SWAP_GENERIC_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(KYBER_ROUTER, data);
    }

    function testSwapGenericTooShortRevertsExtractRecipient() public {
        bytes memory data = abi.encodePacked(SWAP_GENERIC_SEL, uint256(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractRecipient(KYBER_ROUTER, data, address(0));
    }

    // ============ Edge Case: exactly 3 bytes (less than 4 for selector) ============

    function testThreeBytesRevertsAllFunctions() public {
        bytes memory data = hex"e21fd0";

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputTokens(KYBER_ROUTER, data);

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractInputAmounts(KYBER_ROUTER, data);

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractOutputTokens(KYBER_ROUTER, data);

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.extractRecipient(KYBER_ROUTER, data, address(0));

        vm.expectRevert(KyberSwapParser.InvalidCalldata.selector);
        parser.getOperationType(data);
    }
}
