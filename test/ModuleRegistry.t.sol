// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {ModuleRegistry} from "../src/ModuleRegistry.sol";
import {IModuleRegistry} from "../src/interfaces/IModuleRegistry.sol";

/**
 * @title ModuleRegistryTest
 * @notice Unit tests for ModuleRegistry contract
 */
contract ModuleRegistryTest is Test {
    ModuleRegistry public registry;

    address public owner;
    address public factory;
    address public module1;
    address public module2;
    address public safe1;
    address public safe2;
    address public oracle;

    event ModuleRegistered(address indexed module, address indexed safe, address indexed oracle);
    event ModuleDeactivated(address indexed module);
    event ModuleReactivated(address indexed module);
    event ModuleRemoved(address indexed module);
    event FactoryAuthorized(address indexed factory);
    event FactoryDeauthorized(address indexed factory);

    function setUp() public {
        owner = address(this);
        factory = makeAddr("factory");
        module1 = makeAddr("module1");
        module2 = makeAddr("module2");
        safe1 = makeAddr("safe1");
        safe2 = makeAddr("safe2");
        oracle = makeAddr("oracle");

        registry = new ModuleRegistry(owner);
    }

    // ============ Constructor Tests ============

    function testConstructor() public view {
        assertEq(registry.owner(), owner);
    }

    // ============ Factory Authorization Tests ============

    function testAuthorizeFactory() public {
        vm.expectEmit(true, false, false, false);
        emit FactoryAuthorized(factory);

        registry.authorizeFactory(factory);
        assertTrue(registry.authorizedFactories(factory));
    }

    function testAuthorizeFactoryRevertsOnZeroAddress() public {
        vm.expectRevert(ModuleRegistry.InvalidAddress.selector);
        registry.authorizeFactory(address(0));
    }

    function testAuthorizeFactoryOnlyOwner() public {
        vm.prank(makeAddr("notOwner"));
        vm.expectRevert();
        registry.authorizeFactory(factory);
    }

    function testDeauthorizeFactory() public {
        registry.authorizeFactory(factory);

        vm.expectEmit(true, false, false, false);
        emit FactoryDeauthorized(factory);

        registry.deauthorizeFactory(factory);
        assertFalse(registry.authorizedFactories(factory));
    }

    // ============ Module Registration Tests ============

    function testRegisterModule() public {
        vm.expectEmit(true, true, true, false);
        emit ModuleRegistered(module1, safe1, oracle);

        registry.registerModule(module1, safe1, oracle);

        assertTrue(registry.isRegistered(module1));
        assertEq(registry.safeToModule(safe1), module1);

        IModuleRegistry.ModuleInfo memory info = registry.moduleInfo(module1);
        assertEq(info.safeAddress, safe1);
        assertEq(info.authorizedOracle, oracle);
        assertTrue(info.isActive);
    }

    function testRegisterModuleRevertsOnZeroAddress() public {
        vm.expectRevert(ModuleRegistry.InvalidAddress.selector);
        registry.registerModule(address(0), safe1, oracle);

        vm.expectRevert(ModuleRegistry.InvalidAddress.selector);
        registry.registerModule(module1, address(0), oracle);
    }

    function testRegisterModuleAllowsZeroOracle() public {
        // oracle = address(0) is valid for oracleless modules
        registry.registerModule(module1, safe1, address(0));
        assertTrue(registry.isRegistered(module1));
    }

    function testRegisterModuleRevertsOnDuplicate() public {
        registry.registerModule(module1, safe1, oracle);

        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.ModuleAlreadyRegistered.selector, module1));
        registry.registerModule(module1, safe2, oracle);
    }

    function testRegisterModuleRevertsIfSafeAlreadyHasModule() public {
        registry.registerModule(module1, safe1, oracle);

        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.SafeAlreadyHasModule.selector, safe1, module1));
        registry.registerModule(module2, safe1, oracle);
    }

    function testRegisterModuleOnlyOwner() public {
        vm.prank(makeAddr("notOwner"));
        vm.expectRevert();
        registry.registerModule(module1, safe1, oracle);
    }

    function testRegisterModuleFromFactory() public {
        registry.authorizeFactory(factory);

        vm.expectEmit(true, true, true, false);
        emit ModuleRegistered(module1, safe1, oracle);

        vm.prank(factory);
        registry.registerModuleFromFactory(module1, safe1, oracle);

        assertTrue(registry.isRegistered(module1));
    }

    function testRegisterModuleFromFactoryRevertsIfUnauthorized() public {
        vm.prank(factory);
        vm.expectRevert(ModuleRegistry.OnlyAuthorizedFactory.selector);
        registry.registerModuleFromFactory(module1, safe1, oracle);
    }

    // ============ Module Management Tests ============

    function testDeactivateModule() public {
        registry.registerModule(module1, safe1, oracle);

        vm.expectEmit(true, false, false, false);
        emit ModuleDeactivated(module1);

        registry.deactivateModule(module1);

        IModuleRegistry.ModuleInfo memory info = registry.moduleInfo(module1);
        assertFalse(info.isActive);
    }

    function testDeactivateModuleRevertsIfNotRegistered() public {
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.ModuleNotRegistered.selector, module1));
        registry.deactivateModule(module1);
    }

    function testDeactivateModuleOnlyOwner() public {
        registry.registerModule(module1, safe1, oracle);
        vm.prank(makeAddr("notOwner"));
        vm.expectRevert();
        registry.deactivateModule(module1);
    }

    function testReactivateModule() public {
        registry.registerModule(module1, safe1, oracle);
        registry.deactivateModule(module1);

        vm.expectEmit(true, false, false, false);
        emit ModuleReactivated(module1);

        registry.reactivateModule(module1);

        IModuleRegistry.ModuleInfo memory info = registry.moduleInfo(module1);
        assertTrue(info.isActive);
    }

    function testReactivateModuleOnlyOwner() public {
        registry.registerModule(module1, safe1, oracle);
        registry.deactivateModule(module1);
        vm.prank(makeAddr("notOwner"));
        vm.expectRevert();
        registry.reactivateModule(module1);
    }

    function testRemoveModule() public {
        registry.registerModule(module1, safe1, oracle);

        vm.expectEmit(true, false, false, false);
        emit ModuleRemoved(module1);

        registry.removeModule(module1);

        assertFalse(registry.isRegistered(module1));
        assertEq(registry.safeToModule(safe1), address(0));
    }

    function testRemoveModuleOnlyOwner() public {
        registry.registerModule(module1, safe1, oracle);
        vm.prank(makeAddr("notOwner"));
        vm.expectRevert();
        registry.removeModule(module1);
    }

    function testRemoveModuleAllowsReregistration() public {
        registry.registerModule(module1, safe1, oracle);
        registry.removeModule(module1);

        // Can now register a new module for the same Safe
        registry.registerModule(module2, safe1, oracle);
        assertEq(registry.safeToModule(safe1), module2);
    }

    function testRemoveModuleCompactsArray() public {
        // Register 3 modules
        address module3 = makeAddr("module3");
        address safe3 = makeAddr("safe3");
        registry.registerModule(module1, safe1, oracle);
        registry.registerModule(module2, safe2, oracle);
        registry.registerModule(module3, safe3, oracle);

        assertEq(registry.getTotalModuleCount(), 3);

        // Remove module1 (swap-and-pop: module3 takes module1's slot)
        registry.removeModule(module1);

        assertEq(registry.getTotalModuleCount(), 2);
        assertFalse(registry.isRegistered(module1));
        assertTrue(registry.isRegistered(module2));
        assertTrue(registry.isRegistered(module3));

        // Active modules should be module2 and module3
        address[] memory active = registry.getActiveModules();
        assertEq(active.length, 2);
    }

    function testRemoveLastModuleInArray() public {
        registry.registerModule(module1, safe1, oracle);
        registry.registerModule(module2, safe2, oracle);

        // Remove last element (no swap needed)
        registry.removeModule(module2);

        assertEq(registry.getTotalModuleCount(), 1);
        assertTrue(registry.isRegistered(module1));
        assertFalse(registry.isRegistered(module2));
    }

    function testRemoveSingleModule() public {
        registry.registerModule(module1, safe1, oracle);

        registry.removeModule(module1);

        assertEq(registry.getTotalModuleCount(), 0);
        assertFalse(registry.isRegistered(module1));
    }

    // ============ View Function Tests ============

    function testGetActiveModules() public {
        registry.registerModule(module1, safe1, oracle);
        registry.registerModule(module2, safe2, oracle);

        address[] memory active = registry.getActiveModules();
        assertEq(active.length, 2);
        assertEq(active[0], module1);
        assertEq(active[1], module2);
    }

    function testGetActiveModulesExcludesDeactivated() public {
        registry.registerModule(module1, safe1, oracle);
        registry.registerModule(module2, safe2, oracle);
        registry.deactivateModule(module1);

        address[] memory active = registry.getActiveModules();
        assertEq(active.length, 1);
        assertEq(active[0], module2);
    }

    function testGetActiveModulesExcludesRemoved() public {
        registry.registerModule(module1, safe1, oracle);
        registry.registerModule(module2, safe2, oracle);
        registry.removeModule(module1);

        address[] memory active = registry.getActiveModules();
        assertEq(active.length, 1);
        assertEq(active[0], module2);
    }

    function testGetActiveModulesPaginated() public {
        // Register 5 modules
        for (uint256 i = 0; i < 5; i++) {
            address module = makeAddr(string(abi.encodePacked("module", i)));
            address safe = makeAddr(string(abi.encodePacked("safe", i)));
            registry.registerModule(module, safe, oracle);
        }

        // Get first 2
        (address[] memory page1, uint256 total1) = registry.getActiveModulesPaginated(0, 2);
        assertEq(page1.length, 2);
        assertEq(total1, 5);

        // Get next 2
        (address[] memory page2, uint256 total2) = registry.getActiveModulesPaginated(2, 2);
        assertEq(page2.length, 2);
        assertEq(total2, 5);

        // Get last 1
        (address[] memory page3, uint256 total3) = registry.getActiveModulesPaginated(4, 2);
        assertEq(page3.length, 1);
        assertEq(total3, 5);
    }

    function testGetActiveModulesPaginatedWithOffset() public {
        // Register 3 modules
        registry.registerModule(module1, safe1, oracle);
        registry.registerModule(module2, safe2, oracle);
        address module3 = makeAddr("module3");
        address safe3 = makeAddr("safe3");
        registry.registerModule(module3, safe3, oracle);

        // Skip first, get next 2
        (address[] memory modules, uint256 total) = registry.getActiveModulesPaginated(1, 2);
        assertEq(modules.length, 2);
        assertEq(modules[0], module2);
        assertEq(modules[1], module3);
        assertEq(total, 3);
    }

    function testGetModuleForSafe() public {
        registry.registerModule(module1, safe1, oracle);

        assertEq(registry.getModuleForSafe(safe1), module1);
        assertEq(registry.getModuleForSafe(safe2), address(0));
    }

    function testGetActiveModuleCount() public {
        assertEq(registry.getActiveModuleCount(), 0);

        registry.registerModule(module1, safe1, oracle);
        assertEq(registry.getActiveModuleCount(), 1);

        registry.registerModule(module2, safe2, oracle);
        assertEq(registry.getActiveModuleCount(), 2);

        registry.deactivateModule(module1);
        assertEq(registry.getActiveModuleCount(), 1);
    }

    function testGetTotalModuleCount() public {
        assertEq(registry.getTotalModuleCount(), 0);

        registry.registerModule(module1, safe1, oracle);
        assertEq(registry.getTotalModuleCount(), 1);

        registry.deactivateModule(module1);
        assertEq(registry.getTotalModuleCount(), 1); // Still counts deactivated

        registry.removeModule(module1);
        assertEq(registry.getTotalModuleCount(), 0); // Removed via swap-and-pop
    }
}
