// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {PresetRegistry} from "../src/PresetRegistry.sol";

/**
 * @title PresetRegistryTest
 * @notice Unit tests for PresetRegistry contract
 * @dev Covers: constructor, create/update/read presets, access control,
 *      array validation, sequential IDs, full data round-trip.
 */
contract PresetRegistryTest is Test {
    PresetRegistry public registry;

    address public owner;
    address public notOwner;
    address public protocol1;
    address public protocol2;
    address public parser1;
    address public parser2;

    event PresetCreated(uint256 indexed presetId, string name);
    event PresetUpdated(uint256 indexed presetId, string name);

    function setUp() public {
        owner = address(this);
        notOwner = makeAddr("notOwner");
        protocol1 = makeAddr("protocol1");
        protocol2 = makeAddr("protocol2");
        parser1 = makeAddr("parser1");
        parser2 = makeAddr("parser2");

        registry = new PresetRegistry(owner);
    }

    // ============ Helpers ============

    /// @dev Creates a DeFi Trader preset with 2 protocols, 1 parser, 1 selector
    function _createDeFiTraderPreset() internal returns (uint256) {
        address[] memory protocols = new address[](2);
        protocols[0] = protocol1;
        protocols[1] = protocol2;

        address[] memory parserProtos = new address[](1);
        parserProtos[0] = protocol1;
        address[] memory parserAddrs = new address[](1);
        parserAddrs[0] = parser1;

        bytes4[] memory sels = new bytes4[](1);
        sels[0] = bytes4(0x12345678);
        uint8[] memory selTypes = new uint8[](1);
        selTypes[0] = 1; // SWAP

        return
            registry.createPreset("DeFi Trader", 1, 500, 1 days, protocols, parserProtos, parserAddrs, sels, selTypes);
    }

    /// @dev Creates a Payment Agent preset (transfer-only, no protocols)
    function _createPaymentAgentPreset() internal returns (uint256) {
        address[] memory empty = new address[](0);
        bytes4[] memory emptySel = new bytes4[](0);
        uint8[] memory emptyType = new uint8[](0);

        return registry.createPreset("Payment Agent", 2, 100, 1 days, empty, empty, empty, emptySel, emptyType);
    }

    /// @dev Shorthand for empty arrays
    function _emptyAddresses() internal pure returns (address[] memory) {
        return new address[](0);
    }

    function _emptySelectors() internal pure returns (bytes4[] memory) {
        return new bytes4[](0);
    }

    function _emptyUint8s() internal pure returns (uint8[] memory) {
        return new uint8[](0);
    }

    // ============ Constructor Tests ============

    function testConstructor() public view {
        assertEq(registry.owner(), owner);
        assertEq(registry.presetCount(), 0);
    }

    // ============ Create Preset Tests ============

    function testCreatePreset() public {
        vm.expectEmit(true, false, false, true);
        emit PresetCreated(0, "DeFi Trader");

        uint256 presetId = _createDeFiTraderPreset();

        assertEq(presetId, 0);
        assertEq(registry.presetCount(), 1);
        assertTrue(registry.presetExists(presetId));
    }

    function testCreatePresetStoresAllFields() public {
        uint256 presetId = _createDeFiTraderPreset();

        (string memory name, uint16 roleId, uint256 maxSpendingBps, uint256 windowDuration) =
            registry.getPreset(presetId);

        assertEq(name, "DeFi Trader");
        assertEq(roleId, 1);
        assertEq(maxSpendingBps, 500);
        assertEq(windowDuration, 1 days);
    }

    function testCreatePresetStoresProtocols() public {
        uint256 presetId = _createDeFiTraderPreset();

        address[] memory protocols = registry.getPresetProtocols(presetId);
        assertEq(protocols.length, 2);
        assertEq(protocols[0], protocol1);
        assertEq(protocols[1], protocol2);
    }

    function testCreatePresetStoresFullConfig() public {
        uint256 presetId = _createDeFiTraderPreset();

        (
            uint16 roleId,
            uint256 maxSpendingBps,
            uint256 windowDuration,
            address[] memory allowedProtocols,
            address[] memory parserProtocols,
            address[] memory parserAddresses,
            bytes4[] memory selectors,
            uint8[] memory selectorTypes
        ) = registry.getPresetFull(presetId);

        assertEq(roleId, 1);
        assertEq(maxSpendingBps, 500);
        assertEq(windowDuration, 1 days);
        assertEq(allowedProtocols.length, 2);
        assertEq(allowedProtocols[0], protocol1);
        assertEq(parserProtocols.length, 1);
        assertEq(parserProtocols[0], protocol1);
        assertEq(parserAddresses.length, 1);
        assertEq(parserAddresses[0], parser1);
        assertEq(selectors.length, 1);
        assertEq(selectors[0], bytes4(0x12345678));
        assertEq(selectorTypes.length, 1);
        assertEq(selectorTypes[0], 1);
    }

    function testCreateMultiplePresetsSequentialIds() public {
        uint256 id0 = _createDeFiTraderPreset();
        uint256 id1 = _createPaymentAgentPreset();
        uint256 id2 = _createDeFiTraderPreset(); // duplicate name is fine

        assertEq(id0, 0);
        assertEq(id1, 1);
        assertEq(id2, 2);
        assertEq(registry.presetCount(), 3);
    }

    function testCreatePresetWithEmptyArrays() public {
        uint256 presetId = _createPaymentAgentPreset();

        (
            ,,,
            address[] memory allowedProtocols,
            address[] memory parserProtocols,
            address[] memory parserAddresses,
            bytes4[] memory selectors,
            uint8[] memory selectorTypes
        ) = registry.getPresetFull(presetId);

        assertEq(allowedProtocols.length, 0);
        assertEq(parserProtocols.length, 0);
        assertEq(parserAddresses.length, 0);
        assertEq(selectors.length, 0);
        assertEq(selectorTypes.length, 0);
    }

    function testCreatePresetWithMultipleParsersAndSelectors() public {
        address[] memory protocols = new address[](2);
        protocols[0] = protocol1;
        protocols[1] = protocol2;

        address[] memory parserProtos = new address[](2);
        parserProtos[0] = protocol1;
        parserProtos[1] = protocol2;
        address[] memory parserAddrs = new address[](2);
        parserAddrs[0] = parser1;
        parserAddrs[1] = parser2;

        bytes4[] memory sels = new bytes4[](3);
        sels[0] = bytes4(0x11111111);
        sels[1] = bytes4(0x22222222);
        sels[2] = bytes4(0x33333333);
        uint8[] memory selTypes = new uint8[](3);
        selTypes[0] = 1; // SWAP
        selTypes[1] = 2; // DEPOSIT
        selTypes[2] = 3; // WITHDRAW

        uint256 presetId = registry.createPreset(
            "Yield Farmer", 1, 1000, 1 days, protocols, parserProtos, parserAddrs, sels, selTypes
        );

        (
            ,,,,
            address[] memory storedParserProtos,
            address[] memory storedParserAddrs,
            bytes4[] memory storedSels,
            uint8[] memory storedSelTypes
        ) = registry.getPresetFull(presetId);

        assertEq(storedParserProtos.length, 2);
        assertEq(storedParserAddrs.length, 2);
        assertEq(storedParserAddrs[1], parser2);
        assertEq(storedSels.length, 3);
        assertEq(storedSelTypes[2], 3);
    }

    // ============ Create Preset — Access Control ============

    function testCreatePresetOnlyOwner() public {
        vm.prank(notOwner);
        vm.expectRevert();
        registry.createPreset(
            "test",
            1,
            500,
            1 days,
            _emptyAddresses(),
            _emptyAddresses(),
            _emptyAddresses(),
            _emptySelectors(),
            _emptyUint8s()
        );
    }

    // ============ Create Preset — Array Validation ============

    function testCreatePresetRevertsOnParserArrayMismatch() public {
        address[] memory oneAddr = new address[](1);
        oneAddr[0] = protocol1;

        vm.expectRevert(PresetRegistry.ArrayLengthMismatch.selector);
        registry.createPreset(
            "bad", 1, 500, 1 days, _emptyAddresses(), oneAddr, _emptyAddresses(), _emptySelectors(), _emptyUint8s()
        );
    }

    function testCreatePresetRevertsOnSelectorArrayMismatch() public {
        bytes4[] memory oneSel = new bytes4[](1);
        oneSel[0] = bytes4(0x12345678);

        vm.expectRevert(PresetRegistry.ArrayLengthMismatch.selector);
        registry.createPreset(
            "bad", 1, 500, 1 days, _emptyAddresses(), _emptyAddresses(), _emptyAddresses(), oneSel, _emptyUint8s()
        );
    }

    // ============ Update Preset Tests ============

    function testUpdatePreset() public {
        uint256 presetId = _createDeFiTraderPreset();

        address[] memory newProtocols = new address[](1);
        newProtocols[0] = protocol2;

        vm.expectEmit(true, false, false, true);
        emit PresetUpdated(presetId, "Updated Trader");

        registry.updatePreset(
            presetId,
            "Updated Trader",
            2, // changed role
            1000, // increased spending
            2 days, // longer window
            newProtocols,
            _emptyAddresses(),
            _emptyAddresses(),
            _emptySelectors(),
            _emptyUint8s()
        );

        (string memory name, uint16 roleId, uint256 maxBps, uint256 window) = registry.getPreset(presetId);
        assertEq(name, "Updated Trader");
        assertEq(roleId, 2);
        assertEq(maxBps, 1000);
        assertEq(window, 2 days);

        // Protocols replaced entirely (not appended)
        address[] memory protocols = registry.getPresetProtocols(presetId);
        assertEq(protocols.length, 1);
        assertEq(protocols[0], protocol2);
    }

    function testUpdatePresetRevertsOnNonExistent() public {
        vm.expectRevert(abi.encodeWithSelector(PresetRegistry.PresetNotFound.selector, 0));
        registry.updatePreset(
            0,
            "bad",
            1,
            500,
            1 days,
            _emptyAddresses(),
            _emptyAddresses(),
            _emptyAddresses(),
            _emptySelectors(),
            _emptyUint8s()
        );
    }

    function testUpdatePresetRevertsOnParserMismatch() public {
        uint256 presetId = _createDeFiTraderPreset();

        address[] memory oneAddr = new address[](1);
        oneAddr[0] = protocol1;

        vm.expectRevert(PresetRegistry.ArrayLengthMismatch.selector);
        registry.updatePreset(
            presetId,
            "bad",
            1,
            500,
            1 days,
            _emptyAddresses(),
            oneAddr,
            _emptyAddresses(),
            _emptySelectors(),
            _emptyUint8s()
        );
    }

    function testUpdatePresetOnlyOwner() public {
        uint256 presetId = _createDeFiTraderPreset();

        vm.prank(notOwner);
        vm.expectRevert();
        registry.updatePreset(
            presetId,
            "hack",
            1,
            500,
            1 days,
            _emptyAddresses(),
            _emptyAddresses(),
            _emptyAddresses(),
            _emptySelectors(),
            _emptyUint8s()
        );
    }

    // ============ View Function Tests ============

    function testPresetExistsReturnsFalseForNonExistent() public view {
        assertFalse(registry.presetExists(0));
        assertFalse(registry.presetExists(999));
    }

    function testPresetExistsReturnsTrueAfterCreate() public {
        uint256 presetId = _createDeFiTraderPreset();
        assertTrue(registry.presetExists(presetId));
    }

    function testGetPresetRevertsOnNonExistent() public {
        vm.expectRevert(abi.encodeWithSelector(PresetRegistry.PresetNotFound.selector, 42));
        registry.getPreset(42);
    }

    function testGetPresetProtocolsRevertsOnNonExistent() public {
        vm.expectRevert(abi.encodeWithSelector(PresetRegistry.PresetNotFound.selector, 0));
        registry.getPresetProtocols(0);
    }

    function testGetPresetFullRevertsOnNonExistent() public {
        vm.expectRevert(abi.encodeWithSelector(PresetRegistry.PresetNotFound.selector, 0));
        registry.getPresetFull(0);
    }

    // ============ Preset Count Tests ============

    function testPresetCountStartsAtZero() public view {
        assertEq(registry.presetCount(), 0);
    }

    function testPresetCountIncrements() public {
        _createDeFiTraderPreset();
        assertEq(registry.presetCount(), 1);

        _createPaymentAgentPreset();
        assertEq(registry.presetCount(), 2);
    }

    // ============ Edge Case: Update Preserves Existence ============

    function testUpdateDoesNotResetExists() public {
        uint256 presetId = _createDeFiTraderPreset();
        assertTrue(registry.presetExists(presetId));

        // Update with empty arrays
        registry.updatePreset(
            presetId,
            "Empty",
            1,
            0,
            1 hours,
            _emptyAddresses(),
            _emptyAddresses(),
            _emptyAddresses(),
            _emptySelectors(),
            _emptyUint8s()
        );

        // Still exists
        assertTrue(registry.presetExists(presetId));

        (string memory name,,,) = registry.getPreset(presetId);
        assertEq(name, "Empty");
    }
}
