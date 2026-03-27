// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {AgentVaultFactory} from "../src/AgentVaultFactory.sol";
import {PresetRegistry} from "../src/PresetRegistry.sol";
import {ModuleRegistry} from "../src/ModuleRegistry.sol";
import {DeFiInteractorModule} from "../src/DeFiInteractorModule.sol";
import {IModuleRegistry} from "../src/interfaces/IModuleRegistry.sol";
import {MockSafe} from "./mocks/MockSafe.sol";
import {MockChainlinkPriceFeed} from "./mocks/MockChainlinkPriceFeed.sol";

/**
 * @title AgentVaultFactoryTest
 * @notice Tests for AgentVaultFactory + PresetRegistry
 * @dev Covers: constructor, configuration, preset CRUD, custom deployment,
 *      preset-based deployment, validation, registry integration, edge cases.
 */
contract AgentVaultFactoryTest is Test {
    AgentVaultFactory public factory;
    PresetRegistry public presetRegistry;
    ModuleRegistry public registry;
    MockSafe public safe1;
    MockSafe public safe2;

    address public owner;
    address public oracle;
    address public agent;
    address public protocol1;
    address public protocol2;
    address public parser1;
    address public parser2;
    MockChainlinkPriceFeed public priceFeed;

    event AgentVaultCreated(address indexed safe, address indexed agentAddress, address module, uint256 presetId);
    event PresetCreated(uint256 indexed presetId, string name);

    function setUp() public {
        owner = address(this);
        oracle = makeAddr("oracle");
        agent = makeAddr("agent");
        protocol1 = makeAddr("protocol1");
        protocol2 = makeAddr("protocol2");
        parser1 = makeAddr("parser1");
        parser2 = makeAddr("parser2");

        // Deploy mock Safes
        address[] memory safeOwners = new address[](1);
        safeOwners[0] = owner;
        safe1 = new MockSafe(safeOwners, 1);
        safe2 = new MockSafe(safeOwners, 1);

        // Deploy mock price feed
        priceFeed = new MockChainlinkPriceFeed(100000000000, 8); // $1000 with 8 decimals

        // Deploy registry, implementation, and factory
        registry = new ModuleRegistry(owner);
        presetRegistry = new PresetRegistry(owner);
        DeFiInteractorModule impl = new DeFiInteractorModule(owner, owner, owner);
        factory = new AgentVaultFactory(owner, address(registry), address(presetRegistry), address(impl));

        // Authorize factory in registry
        registry.authorizeFactory(address(factory));
    }

    // ============ Helper Functions ============

    function _buildConfig(address safe) internal view returns (AgentVaultFactory.VaultConfig memory) {
        address[] memory allowedProtocols = new address[](2);
        allowedProtocols[0] = protocol1;
        allowedProtocols[1] = protocol2;

        address[] memory parserProtocols = new address[](1);
        parserProtocols[0] = protocol1;
        address[] memory parserAddresses = new address[](1);
        parserAddresses[0] = parser1;

        bytes4[] memory selectors = new bytes4[](2);
        selectors[0] = bytes4(0x12345678); // mock selector
        selectors[1] = bytes4(0xabcdef01); // mock selector
        uint8[] memory selectorTypes = new uint8[](2);
        selectorTypes[0] = 1; // SWAP
        selectorTypes[1] = 2; // DEPOSIT

        address[] memory priceFeedTokens = new address[](1);
        priceFeedTokens[0] = protocol1; // reusing address as token for simplicity
        address[] memory priceFeedAddresses = new address[](1);
        priceFeedAddresses[0] = address(priceFeed);

        return AgentVaultFactory.VaultConfig({
            safe: safe,
            oracle: oracle,
            agentAddress: agent,
            roleId: 1, // DEFI_EXECUTE_ROLE
            maxSpendingBps: 500, // 5%
            maxSpendingUSD: 0, // BPS mode
            windowDuration: 1 days,
            allowedProtocols: allowedProtocols,
            parserProtocols: parserProtocols,
            parserAddresses: parserAddresses,
            selectors: selectors,
            selectorTypes: selectorTypes,
            priceFeedTokens: priceFeedTokens,
            priceFeedAddresses: priceFeedAddresses
        });
    }

    function _createDefiTraderPreset() internal returns (uint256 presetId) {
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

        return presetRegistry.createPreset(
            "DeFi Trader",
            1, // DEFI_EXECUTE_ROLE
            500, // 5%
            0, // BPS mode
            1 days,
            protocols,
            parserProtos,
            parserAddrs,
            sels,
            selTypes
        );
    }

    // ============ Constructor Tests ============

    function testConstructor() public view {
        assertEq(factory.owner(), owner);
        assertEq(address(factory.registry()), address(registry));
        assertEq(address(factory.presetRegistry()), address(presetRegistry));
    }

    function testConstructorWithoutRegistries() public {
        DeFiInteractorModule impl = new DeFiInteractorModule(owner, owner, owner);
        AgentVaultFactory f = new AgentVaultFactory(owner, address(0), address(0), address(impl));
        assertEq(address(f.registry()), address(0));
        assertEq(address(f.presetRegistry()), address(0));
    }

    // ============ Configuration Tests ============

    function testSetRegistry() public {
        ModuleRegistry newRegistry = new ModuleRegistry(owner);
        factory.setRegistry(address(newRegistry));
        assertEq(address(factory.registry()), address(newRegistry));
    }

    function testSetRegistryRevertsOnZeroAddress() public {
        vm.expectRevert(AgentVaultFactory.InvalidAddress.selector);
        factory.setRegistry(address(0));
    }

    function testSetRegistryOnlyOwner() public {
        vm.prank(agent);
        vm.expectRevert();
        factory.setRegistry(makeAddr("newRegistry"));
    }

    function testSetPresetRegistry() public {
        PresetRegistry newPr = new PresetRegistry(owner);
        factory.setPresetRegistry(address(newPr));
        assertEq(address(factory.presetRegistry()), address(newPr));
    }

    function testSetPresetRegistryRevertsOnZero() public {
        vm.expectRevert(AgentVaultFactory.InvalidAddress.selector);
        factory.setPresetRegistry(address(0));
    }

    // ============ PresetRegistry Tests ============

    function testCreatePreset() public {
        vm.expectEmit(true, false, false, true);
        emit PresetCreated(0, "DeFi Trader");

        uint256 presetId = _createDefiTraderPreset();
        assertEq(presetId, 0);
        assertEq(presetRegistry.presetCount(), 1);

        (string memory name, uint16 roleId, uint256 maxSpendingBps, uint256 maxSpendingUSD, uint256 windowDuration) =
            presetRegistry.getPreset(presetId);
        assertEq(name, "DeFi Trader");
        assertEq(roleId, 1);
        assertEq(maxSpendingBps, 500);
        assertEq(maxSpendingUSD, 0);
        assertEq(windowDuration, 1 days);
    }

    function testCreateMultiplePresets() public {
        uint256 id0 = _createDefiTraderPreset();
        assertEq(id0, 0);

        // Create a second preset
        address[] memory empty = new address[](0);
        bytes4[] memory emptySel = new bytes4[](0);
        uint8[] memory emptyType = new uint8[](0);
        uint256 id1 =
            presetRegistry.createPreset("Payment Agent", 2, 100, 0, 1 days, empty, empty, empty, emptySel, emptyType);
        assertEq(id1, 1);
        assertEq(presetRegistry.presetCount(), 2);
    }

    function testGetPresetProtocols() public {
        uint256 presetId = _createDefiTraderPreset();
        address[] memory protocols = presetRegistry.getPresetProtocols(presetId);
        assertEq(protocols.length, 2);
        assertEq(protocols[0], protocol1);
        assertEq(protocols[1], protocol2);
    }

    function testGetPresetRevertsOnInvalidId() public {
        vm.expectRevert(abi.encodeWithSelector(PresetRegistry.PresetNotFound.selector, 999));
        presetRegistry.getPreset(999);
    }

    function testCreatePresetOnlyOwner() public {
        address[] memory empty = new address[](0);
        bytes4[] memory emptySel = new bytes4[](0);
        uint8[] memory emptyType = new uint8[](0);

        vm.prank(agent);
        vm.expectRevert();
        presetRegistry.createPreset("test", 1, 500, 0, 1 days, empty, empty, empty, emptySel, emptyType);
    }

    function testCreatePresetRevertsOnArrayMismatch() public {
        address[] memory oneAddr = new address[](1);
        oneAddr[0] = protocol1;
        address[] memory empty = new address[](0);
        bytes4[] memory emptySel = new bytes4[](0);
        uint8[] memory emptyType = new uint8[](0);

        // parserProtocols.length != parserAddresses.length
        vm.expectRevert(PresetRegistry.ArrayLengthMismatch.selector);
        presetRegistry.createPreset("bad", 1, 500, 0, 1 days, empty, oneAddr, empty, emptySel, emptyType);
    }

    // ============ Deploy Vault Tests ============

    function testDeployVault() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));

        address module = factory.deployVault(config);
        assertTrue(module != address(0));

        // Verify module configuration
        DeFiInteractorModule m = DeFiInteractorModule(module);

        // Owner transferred to Safe
        assertEq(m.owner(), address(safe1));

        // Avatar is Safe
        assertEq(m.avatar(), address(safe1));

        // Oracle set correctly
        assertEq(m.authorizedOracle(), oracle);

        // Role granted
        assertTrue(m.hasRole(agent, 1)); // DEFI_EXECUTE_ROLE

        // Spending limits configured
        (uint256 maxBps, uint256 maxUSD, uint256 windowDur) = m.getSubAccountLimits(agent);
        assertEq(maxBps, 500);
        assertEq(maxUSD, 0);
        assertEq(windowDur, 1 days);

        // Allowed addresses set
        assertTrue(m.allowedAddresses(agent, protocol1));
        assertTrue(m.allowedAddresses(agent, protocol2));

        // Parser registered
        assertEq(address(m.protocolParsers(protocol1)), parser1);

        // Selectors registered
        assertEq(uint8(m.selectorType(bytes4(0x12345678))), 1); // SWAP
        assertEq(uint8(m.selectorType(bytes4(0xabcdef01))), 2); // DEPOSIT

        // Price feed set
        assertEq(address(m.tokenPriceFeeds(protocol1)), address(priceFeed));
    }

    function testDeployVaultRegistersInRegistry() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        address module = factory.deployVault(config);

        // Registered in registry
        assertTrue(registry.isRegistered(module));
        assertEq(registry.safeToModule(address(safe1)), module);

        IModuleRegistry.ModuleInfo memory info = registry.moduleInfo(module);
        assertEq(info.safeAddress, address(safe1));
        assertEq(info.authorizedOracle, oracle);
        assertTrue(info.isActive);
    }

    function testDeployVaultEmitsEvent() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));

        // We can't predict exact module address easily, so just check indexed params
        vm.expectEmit(true, true, false, false);
        emit AgentVaultCreated(address(safe1), agent, address(0), 0);

        factory.deployVault(config);
    }

    function testDeployVaultTracksDeployments() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        address module = factory.deployVault(config);

        address[] memory modules = factory.getDeployedModules(address(safe1));
        assertEq(modules.length, 1);
        assertEq(modules[0], module);
        assertEq(factory.getNonce(address(safe1)), 1);
    }

    function testDeployVaultForMultipleSafes() public {
        AgentVaultFactory.VaultConfig memory config1 = _buildConfig(address(safe1));
        AgentVaultFactory.VaultConfig memory config2 = _buildConfig(address(safe2));

        address module1 = factory.deployVault(config1);
        address module2 = factory.deployVault(config2);

        assertTrue(module1 != module2);
        assertTrue(registry.isRegistered(module1));
        assertTrue(registry.isRegistered(module2));
    }

    function testDeployVaultDeterministicAddress() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));

        address predicted = factory.computeModuleAddress(address(safe1));
        address module = factory.deployVault(config);

        assertEq(module, predicted);
    }

    // ============ Deploy From Preset Tests ============

    function testDeployVaultFromPreset() public {
        uint256 presetId = _createDefiTraderPreset();

        address[] memory priceFeedTokens = new address[](1);
        priceFeedTokens[0] = protocol1;
        address[] memory priceFeedAddrs = new address[](1);
        priceFeedAddrs[0] = address(priceFeed);

        address module =
            factory.deployVaultFromPreset(address(safe1), oracle, agent, presetId, priceFeedTokens, priceFeedAddrs);
        assertTrue(module != address(0));

        DeFiInteractorModule m = DeFiInteractorModule(module);

        // Verify preset was applied
        assertEq(m.owner(), address(safe1));
        assertTrue(m.hasRole(agent, 1));
        (uint256 maxBps,,) = m.getSubAccountLimits(agent);
        assertEq(maxBps, 500);
        assertTrue(m.allowedAddresses(agent, protocol1));
        assertTrue(m.allowedAddresses(agent, protocol2));
        assertEq(address(m.protocolParsers(protocol1)), parser1);
        assertEq(uint8(m.selectorType(bytes4(0x12345678))), 1);
        assertEq(address(m.tokenPriceFeeds(protocol1)), address(priceFeed));
    }

    function testDeployFromPresetRevertsOnInvalidPreset() public {
        address[] memory empty = new address[](0);

        vm.expectRevert(abi.encodeWithSelector(PresetRegistry.PresetNotFound.selector, 999));
        factory.deployVaultFromPreset(address(safe1), oracle, agent, 999, empty, empty);
    }

    function testDeployFromPresetRevertsIfNoPresetRegistry() public {
        DeFiInteractorModule impl = new DeFiInteractorModule(owner, owner, owner);
        AgentVaultFactory factoryNoPr = new AgentVaultFactory(owner, address(registry), address(0), address(impl));
        registry.authorizeFactory(address(factoryNoPr));

        address[] memory empty = new address[](0);

        vm.expectRevert(AgentVaultFactory.PresetRegistryNotSet.selector);
        factoryNoPr.deployVaultFromPreset(address(safe1), oracle, agent, 0, empty, empty);
    }

    // ============ Validation Tests ============

    function testDeployVaultRevertsOnZeroSafe() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        config.safe = address(0);

        vm.expectRevert(AgentVaultFactory.InvalidAddress.selector);
        factory.deployVault(config);
    }

    function testDeployVaultRevertsOnZeroOracle() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        config.oracle = address(0);

        vm.expectRevert(AgentVaultFactory.InvalidAddress.selector);
        factory.deployVault(config);
    }

    function testDeployVaultRevertsOnZeroAgent() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        config.agentAddress = address(0);

        vm.expectRevert(AgentVaultFactory.InvalidAddress.selector);
        factory.deployVault(config);
    }

    function testDeployVaultRevertsOnOracleEqualsSafe() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        config.oracle = address(safe1);

        vm.expectRevert(AgentVaultFactory.InvalidConfig.selector);
        factory.deployVault(config);
    }

    function testDeployVaultRevertsOnArrayMismatch() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        // Make parser arrays mismatched
        config.parserAddresses = new address[](0);

        vm.expectRevert(AgentVaultFactory.ArrayLengthMismatch.selector);
        factory.deployVault(config);
    }

    function testDeployVaultPermissionless() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));

        // Anyone can deploy a vault — not restricted to owner
        address anyone = makeAddr("anyone");
        vm.prank(anyone);
        address module = factory.deployVault(config);

        assertTrue(module != address(0));

        // Module owner is the Safe, not the deployer
        DeFiInteractorModule m = DeFiInteractorModule(module);
        assertEq(m.owner(), address(safe1));
        assertTrue(registry.isRegistered(module));
    }

    function testDeployVaultRevertsIfSafeAlreadyHasModule() public {
        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        factory.deployVault(config);

        // Second deployment should revert
        vm.expectRevert();
        factory.deployVault(config);
    }

    // ============ No Registry Tests ============

    function testDeployVaultWithoutRegistry() public {
        DeFiInteractorModule impl = new DeFiInteractorModule(owner, owner, owner);
        AgentVaultFactory factoryNoReg = new AgentVaultFactory(owner, address(0), address(presetRegistry), address(impl));

        AgentVaultFactory.VaultConfig memory config = _buildConfig(address(safe1));
        address module = factoryNoReg.deployVault(config);

        assertTrue(module != address(0));
        // Module is deployed and configured but not registered
        assertFalse(registry.isRegistered(module));

        // But it's tracked in the factory
        address[] memory modules = factoryNoReg.getDeployedModules(address(safe1));
        assertEq(modules.length, 1);
        assertEq(modules[0], module);
    }

    // ============ Minimal Config Tests ============

    function testDeployVaultMinimalConfig() public {
        // Deploy with zero-length optional arrays (no parsers, no selectors, no price feeds)
        AgentVaultFactory.VaultConfig memory config;
        config.safe = address(safe1);
        config.oracle = oracle;
        config.agentAddress = agent;
        config.roleId = 2; // DEFI_TRANSFER_ROLE
        config.maxSpendingBps = 100; // 1%
        config.maxSpendingUSD = 0;
        config.windowDuration = 1 days;
        config.allowedProtocols = new address[](0);
        config.parserProtocols = new address[](0);
        config.parserAddresses = new address[](0);
        config.selectors = new bytes4[](0);
        config.selectorTypes = new uint8[](0);
        config.priceFeedTokens = new address[](0);
        config.priceFeedAddresses = new address[](0);

        address module = factory.deployVault(config);
        DeFiInteractorModule m = DeFiInteractorModule(module);

        assertEq(m.owner(), address(safe1));
        assertTrue(m.hasRole(agent, 2)); // DEFI_TRANSFER_ROLE
        assertFalse(m.hasRole(agent, 1)); // Does NOT have EXECUTE role
    }

    // ============ Update Preset Tests ============

    function testUpdatePreset() public {
        uint256 presetId = _createDefiTraderPreset();

        address[] memory newProtocols = new address[](1);
        newProtocols[0] = protocol1;
        address[] memory emptyAddr = new address[](0);
        bytes4[] memory emptySel = new bytes4[](0);
        uint8[] memory emptyType = new uint8[](0);

        presetRegistry.updatePreset(
            presetId,
            "Updated DeFi Trader",
            1,
            1000, // updated to 10%
            0, // BPS mode
            2 days, // updated window
            newProtocols,
            emptyAddr,
            emptyAddr,
            emptySel,
            emptyType
        );

        (string memory name,, uint256 maxBps, uint256 maxUSD, uint256 window) = presetRegistry.getPreset(presetId);
        assertEq(name, "Updated DeFi Trader");
        assertEq(maxBps, 1000);
        assertEq(maxUSD, 0);
        assertEq(window, 2 days);
    }

    function testUpdatePresetRevertsOnInvalidId() public {
        address[] memory empty = new address[](0);
        bytes4[] memory emptySel = new bytes4[](0);
        uint8[] memory emptyType = new uint8[](0);

        vm.expectRevert(abi.encodeWithSelector(PresetRegistry.PresetNotFound.selector, 0));
        presetRegistry.updatePreset(0, "bad", 1, 500, 0, 1 days, empty, empty, empty, emptySel, emptyType);
    }
}
