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
    address public deployer;

    event ModuleRegistered(address indexed module, address indexed safe, address indexed oracle);
    event ModuleDeactivated(address indexed module);
    event ModuleReactivated(address indexed module);
    event ModuleRemoved(address indexed module);
    event FactoryAuthorized(address indexed factory);
    event FactoryDeauthorized(address indexed factory);
    event OracleManagerSet(address indexed oracle, address indexed manager);
    event OracleRestrictionSet(address indexed oracle, bool restricted);
    event OracleDeployerTrustSet(address indexed oracle, address indexed deployer, bool trusted);
    event OracleModuleApprovalSet(address indexed oracle, address indexed module, bool approved);

    function setUp() public {
        owner = address(this);
        factory = makeAddr("factory");
        module1 = makeAddr("module1");
        module2 = makeAddr("module2");
        safe1 = makeAddr("safe1");
        safe2 = makeAddr("safe2");
        oracle = makeAddr("oracle");
        deployer = makeAddr("deployer");

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
        registry.registerModuleFromFactory(module1, safe1, oracle, deployer);

        assertTrue(registry.isRegistered(module1));
        assertEq(registry.moduleDeployer(module1), deployer);
        assertEq(registry.getDeployerModules(deployer).length, 1);
        assertEq(registry.getDeployerModules(deployer)[0], module1);
    }

    function testRegisterModuleFromFactoryRevertsIfUnauthorized() public {
        vm.prank(factory);
        vm.expectRevert(ModuleRegistry.OnlyAuthorizedFactory.selector);
        registry.registerModuleFromFactory(module1, safe1, oracle, deployer);
    }

    function testRegisterModuleRecordsNoDeployer() public {
        registry.registerModule(module1, safe1, oracle);
        assertEq(registry.moduleDeployer(module1), address(0));
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

    // ============ Oracle Access Policy Tests ============

    function _one(address a) internal pure returns (address[] memory list) {
        list = new address[](1);
        list[0] = a;
    }

    function _two(address a, address b) internal pure returns (address[] memory list) {
        list = new address[](2);
        list[0] = a;
        list[1] = b;
    }

    /// @dev module1 is deployed by `deployer`, module2 by a stranger (both through the factory),
    ///      module3 is registered by the owner (no deployer)
    function _registerPolicyModules() internal returns (address module3) {
        module3 = makeAddr("module3");
        registry.authorizeFactory(factory);
        vm.startPrank(factory);
        registry.registerModuleFromFactory(module1, safe1, oracle, deployer);
        registry.registerModuleFromFactory(module2, safe2, oracle, makeAddr("stranger"));
        vm.stopPrank();
        registry.registerModule(module3, makeAddr("safe3"), oracle);
    }

    function _restrict(address oracle_) internal {
        vm.prank(oracle_);
        registry.setOracleRestricted(oracle_, true);
    }

    function testOraclesAreOpenByDefault() public {
        address module3 = _registerPolicyModules();

        assertFalse(registry.oracleRestricted(oracle));
        assertTrue(registry.isServedBy(module1, oracle));
        assertTrue(registry.isServedBy(module2, oracle));
        assertTrue(registry.isServedBy(module3, oracle));
        assertEq(registry.getModulesServedBy(oracle), registry.getActiveModules());
        // Not registered: not served
        assertFalse(registry.isServedBy(makeAddr("unknown"), oracle));
    }

    function testRestrictedOracleServesApprovedModulesAndTrustedDeployers() public {
        address module3 = _registerPolicyModules();
        _restrict(oracle);
        vm.prank(oracle);
        registry.setTrustedDeployers(oracle, _one(deployer), true);

        assertTrue(registry.isServedBy(module1, oracle));
        assertFalse(registry.isServedBy(module2, oracle)); // a stranger's
        assertFalse(registry.isServedBy(module3, oracle)); // no deployer
        assertEq(registry.getModulesServedBy(oracle), _one(module1));

        // An approved module is served whoever deployed it; approved ones are listed first
        vm.prank(oracle);
        registry.setApprovedModules(oracle, _one(module3), true);
        assertTrue(registry.isServedBy(module3, oracle));
        assertEq(registry.getModulesServedBy(oracle), _two(module3, module1));

        // A trusted deployer's new module is served without another transaction
        address module4 = makeAddr("module4");
        vm.prank(factory);
        registry.registerModuleFromFactory(module4, makeAddr("safe4"), oracle, deployer);
        assertTrue(registry.isServedBy(module4, oracle));
        address[] memory served = registry.getModulesServedBy(oracle);
        assertEq(served.length, 3);
        assertEq(served[2], module4);

        // Other oracles keep their own (default) policy
        address otherOracle = makeAddr("otherOracle");
        assertTrue(registry.isServedBy(module2, otherOracle));
        assertEq(registry.getModulesServedBy(otherOracle), registry.getActiveModules());
    }

    function testServedModulesAreListedOnce() public {
        _registerPolicyModules();
        _restrict(oracle);
        vm.startPrank(oracle);
        registry.setTrustedDeployers(oracle, _one(deployer), true);
        registry.setApprovedModules(oracle, _one(module1), true); // also deployed by `deployer`
        vm.stopPrank();
        assertEq(registry.getModulesServedBy(oracle), _one(module1));
    }

    function testInactiveAndRemovedModulesAreNotServed() public {
        address module3 = _registerPolicyModules();

        // Open
        registry.deactivateModule(module2);
        assertFalse(registry.isServedBy(module2, oracle));

        // Restricted
        _restrict(oracle);
        vm.startPrank(oracle);
        registry.setTrustedDeployers(oracle, _one(deployer), true);
        registry.setApprovedModules(oracle, _two(module2, module3), true);
        vm.stopPrank();
        assertEq(registry.getModulesServedBy(oracle), _two(module3, module1));

        registry.deactivateModule(module1);
        assertFalse(registry.isServedBy(module1, oracle));
        assertEq(registry.getModulesServedBy(oracle), _one(module3));
        registry.reactivateModule(module1);
        assertTrue(registry.isServedBy(module1, oracle));

        // A removed module loses its deployer: registered again by the owner, it is not served
        // through that deployer any more
        registry.removeModule(module1);
        registry.removeModule(module3);
        assertFalse(registry.isServedBy(module1, oracle));
        assertEq(registry.getModulesServedBy(oracle).length, 0);
        registry.registerModule(module1, safe1, oracle);
        assertEq(registry.moduleDeployer(module1), address(0));
        assertFalse(registry.isServedBy(module1, oracle));
        assertEq(registry.getModulesServedBy(oracle).length, 0);
    }

    function testModulesCanBeApprovedBeforeTheyAreRegistered() public {
        _restrict(oracle);
        vm.prank(oracle);
        registry.setApprovedModules(oracle, _one(module1), true);
        assertFalse(registry.isServedBy(module1, oracle));
        assertEq(registry.getModulesServedBy(oracle).length, 0);

        registry.registerModule(module1, safe1, oracle);
        assertTrue(registry.isServedBy(module1, oracle));
        assertEq(registry.getModulesServedBy(oracle), _one(module1));
    }

    function testTrustAndApprovalCanBeWithdrawn() public {
        address module3 = _registerPolicyModules();
        _restrict(oracle);
        vm.startPrank(oracle);
        registry.setTrustedDeployers(oracle, _one(deployer), true);
        registry.setApprovedModules(oracle, _one(module3), true);
        assertEq(registry.getTrustedDeployers(oracle), _one(deployer));
        assertEq(registry.getApprovedModules(oracle), _one(module3));
        assertTrue(registry.isTrustedDeployer(oracle, deployer));
        assertTrue(registry.isApprovedModule(oracle, module3));
        assertFalse(registry.isTrustedDeployer(makeAddr("otherOracle"), deployer));

        registry.setTrustedDeployers(oracle, _one(deployer), false);
        registry.setApprovedModules(oracle, _one(module3), false);
        vm.stopPrank();
        assertFalse(registry.isServedBy(module1, oracle));
        assertFalse(registry.isServedBy(module3, oracle));
        assertEq(registry.getTrustedDeployers(oracle).length, 0);
        assertEq(registry.getApprovedModules(oracle).length, 0);
        assertFalse(registry.isTrustedDeployer(oracle, deployer));
        assertFalse(registry.isApprovedModule(oracle, module3));
        assertEq(registry.getModulesServedBy(oracle).length, 0);

        // Opening the oracle again serves every active module
        vm.prank(oracle);
        registry.setOracleRestricted(oracle, false);
        assertEq(registry.getModulesServedBy(oracle), registry.getActiveModules());
    }

    function testOnlyTheOracleOrItsManagerSetsItsPolicy() public {
        address manager = makeAddr("manager");
        address stranger = makeAddr("stranger");

        vm.prank(stranger);
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.NotOracleManager.selector, oracle, stranger));
        registry.setOracleRestricted(oracle, true);
        // Not even the registry owner
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.NotOracleManager.selector, oracle, owner));
        registry.setOracleRestricted(oracle, true);
        vm.prank(stranger);
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.NotOracleManager.selector, oracle, stranger));
        registry.setTrustedDeployers(oracle, _one(stranger), true);
        vm.prank(stranger);
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.NotOracleManager.selector, oracle, stranger));
        registry.setApprovedModules(oracle, _one(module1), true);
        // Nobody has a policy for address(0), the oracle of oracleless modules
        vm.prank(stranger);
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.NotOracleManager.selector, address(0), stranger));
        registry.setOracleRestricted(address(0), true);

        // The oracle designates a manager, which sets the policy
        vm.expectEmit(true, true, false, false);
        emit OracleManagerSet(oracle, manager);
        vm.prank(oracle);
        registry.setOracleManager(manager);
        assertEq(registry.oracleManager(oracle), manager);
        vm.startPrank(manager);
        registry.setOracleRestricted(oracle, true);
        registry.setTrustedDeployers(oracle, _one(deployer), true);
        registry.setApprovedModules(oracle, _one(module1), true);
        vm.stopPrank();
        assertTrue(registry.oracleRestricted(oracle));

        // The oracle still can
        vm.prank(oracle);
        registry.setOracleRestricted(oracle, false);
        assertFalse(registry.oracleRestricted(oracle));

        // A manager manages only its oracle's policy
        address otherOracle = makeAddr("otherOracle");
        vm.prank(manager);
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.NotOracleManager.selector, otherOracle, manager));
        registry.setOracleRestricted(otherOracle, true);

        // A removed manager can't any more
        vm.prank(oracle);
        registry.setOracleManager(address(0));
        vm.prank(manager);
        vm.expectRevert(abi.encodeWithSelector(ModuleRegistry.NotOracleManager.selector, oracle, manager));
        registry.setOracleRestricted(oracle, true);
    }

    function testPolicyChangesEmitEvents() public {
        vm.startPrank(oracle);
        vm.expectEmit(true, false, false, true);
        emit OracleRestrictionSet(oracle, true);
        registry.setOracleRestricted(oracle, true);

        vm.expectEmit(true, true, false, true);
        emit OracleDeployerTrustSet(oracle, deployer, true);
        registry.setTrustedDeployers(oracle, _one(deployer), true);

        vm.expectEmit(true, true, false, true);
        emit OracleModuleApprovalSet(oracle, module1, false);
        registry.setApprovedModules(oracle, _one(module1), false);
        vm.stopPrank();
    }

    function testPolicyRejectsZeroAddresses() public {
        vm.startPrank(oracle);
        vm.expectRevert(ModuleRegistry.InvalidAddress.selector);
        registry.setTrustedDeployers(oracle, _two(deployer, address(0)), true);
        vm.expectRevert(ModuleRegistry.InvalidAddress.selector);
        registry.setApprovedModules(oracle, _one(address(0)), true);
        vm.stopPrank();
    }
}
