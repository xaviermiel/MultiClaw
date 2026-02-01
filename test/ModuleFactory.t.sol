// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {ModuleFactory} from "../src/ModuleFactory.sol";
import {ModuleRegistry} from "../src/ModuleRegistry.sol";
import {DeFiInteractorModule} from "../src/DeFiInteractorModule.sol";
import {IModuleRegistry} from "../src/interfaces/IModuleRegistry.sol";
import {MockSafe} from "./mocks/MockSafe.sol";

/**
 * @title ModuleFactoryTest
 * @notice Unit tests for ModuleFactory contract
 */
contract ModuleFactoryTest is Test {
    ModuleFactory public factory;
    ModuleRegistry public registry;
    MockSafe public safe1;
    MockSafe public safe2;

    address public owner;
    address public oracle;

    event ModuleDeployed(
        address indexed module,
        address indexed safe,
        address indexed oracle,
        bytes32 salt
    );
    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);
    event AutoRegisterToggled(bool enabled);

    function setUp() public {
        owner = address(this);
        oracle = makeAddr("oracle");

        // Deploy mock Safes
        address[] memory safeOwners = new address[](1);
        safeOwners[0] = owner;
        safe1 = new MockSafe(safeOwners, 1);
        safe2 = new MockSafe(safeOwners, 1);

        // Deploy registry and factory
        registry = new ModuleRegistry(owner);
        factory = new ModuleFactory(owner, address(registry), true);

        // Authorize factory in registry
        registry.authorizeFactory(address(factory));
    }

    // ============ Constructor Tests ============

    function testConstructor() public view {
        assertEq(factory.owner(), owner);
        assertEq(address(factory.registry()), address(registry));
        assertTrue(factory.autoRegister());
    }

    function testConstructorWithoutRegistry() public {
        ModuleFactory factoryNoRegistry = new ModuleFactory(owner, address(0), false);
        assertEq(address(factoryNoRegistry.registry()), address(0));
        assertFalse(factoryNoRegistry.autoRegister());
    }

    // ============ Configuration Tests ============

    function testSetRegistry() public {
        ModuleRegistry newRegistry = new ModuleRegistry(owner);

        vm.expectEmit(true, true, false, false);
        emit RegistryUpdated(address(registry), address(newRegistry));

        factory.setRegistry(address(newRegistry));
        assertEq(address(factory.registry()), address(newRegistry));
    }

    function testSetRegistryOnlyOwner() public {
        vm.prank(makeAddr("notOwner"));
        vm.expectRevert();
        factory.setRegistry(address(0));
    }

    function testSetAutoRegister() public {
        vm.expectEmit(false, false, false, true);
        emit AutoRegisterToggled(false);

        factory.setAutoRegister(false);
        assertFalse(factory.autoRegister());
    }

    // ============ Salt Generation Tests ============

    function testComputeSalt() public view {
        bytes32 salt0 = factory.computeSalt(address(safe1), 0);
        bytes32 salt1 = factory.computeSalt(address(safe1), 1);
        bytes32 saltOther = factory.computeSalt(address(safe2), 0);

        // Same safe, same nonce = same salt
        assertEq(salt0, keccak256(abi.encodePacked(address(safe1), uint256(0))));

        // Different nonce = different salt
        assertTrue(salt0 != salt1);

        // Different safe = different salt
        assertTrue(salt0 != saltOther);
    }

    function testComputeModuleAddress() public view {
        address predicted = factory.computeModuleAddress(address(safe1), oracle, 0);

        // Should be a valid address
        assertTrue(predicted != address(0));

        // Should be deterministic
        address predicted2 = factory.computeModuleAddress(address(safe1), oracle, 0);
        assertEq(predicted, predicted2);

        // Different nonce = different address
        address predicted3 = factory.computeModuleAddress(address(safe1), oracle, 1);
        assertTrue(predicted != predicted3);
    }

    // ============ Deployment Tests ============

    function testDeployModule() public {
        address predicted = factory.computeModuleAddress(address(safe1), oracle, 0);

        vm.expectEmit(true, true, true, true);
        emit ModuleDeployed(predicted, address(safe1), oracle, factory.computeSalt(address(safe1), 0));

        address module = factory.deployModule(address(safe1), oracle);

        assertEq(module, predicted);
        assertEq(factory.deployedModules(address(safe1)), module);
        assertEq(factory.getNonce(address(safe1)), 1);

        // Verify module was deployed correctly
        DeFiInteractorModule deployedModule = DeFiInteractorModule(module);
        assertEq(deployedModule.avatar(), address(safe1));
        assertEq(deployedModule.owner(), address(safe1));
        assertEq(deployedModule.authorizedOracle(), oracle);
    }

    function testDeployModuleAutoRegisters() public {
        address module = factory.deployModule(address(safe1), oracle);

        // Should be registered in registry
        assertTrue(registry.isRegistered(module));
        assertEq(registry.safeToModule(address(safe1)), module);

        IModuleRegistry.ModuleInfo memory info = registry.moduleInfo(module);
        assertEq(info.safeAddress, address(safe1));
        assertEq(info.authorizedOracle, oracle);
        assertTrue(info.isActive);
    }

    function testDeployModuleWithoutAutoRegister() public {
        factory.setAutoRegister(false);

        address module = factory.deployModule(address(safe1), oracle);

        // Should NOT be registered in registry
        assertFalse(registry.isRegistered(module));
    }

    function testDeployModuleRevertsOnZeroAddress() public {
        vm.expectRevert(ModuleFactory.InvalidAddress.selector);
        factory.deployModule(address(0), oracle);

        vm.expectRevert(ModuleFactory.InvalidAddress.selector);
        factory.deployModule(address(safe1), address(0));
    }

    function testDeployModuleOnlyOwner() public {
        vm.prank(makeAddr("notOwner"));
        vm.expectRevert();
        factory.deployModule(address(safe1), oracle);
    }

    function testDeployModuleWithNonce() public {
        // Deploy with nonce 5
        address predicted = factory.computeModuleAddress(address(safe1), oracle, 5);
        address module = factory.deployModuleWithNonce(address(safe1), oracle, 5);

        assertEq(module, predicted);
        assertEq(factory.getNonce(address(safe1)), 6);
    }

    function testDeployMultipleModules() public {
        address module1 = factory.deployModule(address(safe1), oracle);
        address module2 = factory.deployModule(address(safe2), oracle);

        // Different addresses
        assertTrue(module1 != module2);

        // Both registered
        assertTrue(registry.isRegistered(module1));
        assertTrue(registry.isRegistered(module2));

        // Correct Safe mappings
        assertEq(registry.safeToModule(address(safe1)), module1);
        assertEq(registry.safeToModule(address(safe2)), module2);
    }

    function testDeployModuleRevertsIfRegistryNotSet() public {
        // Create factory without registry but with auto-register enabled
        ModuleFactory factoryNoRegistry = new ModuleFactory(owner, address(0), true);

        vm.expectRevert(ModuleFactory.RegistryNotSet.selector);
        factoryNoRegistry.deployModule(address(safe1), oracle);
    }

    function testDeployModuleRevertsIfOracleIsSafe() public {
        // Oracle cannot be the Safe itself (prevents self-authorization)
        vm.expectRevert(abi.encodeWithSelector(ModuleFactory.InvalidOracleAddress.selector, address(safe1)));
        factory.deployModule(address(safe1), address(safe1));
    }

    function testDeployModuleRevertsIfOracleIsFactory() public {
        // Oracle cannot be the Factory itself
        vm.expectRevert(abi.encodeWithSelector(ModuleFactory.InvalidOracleAddress.selector, address(factory)));
        factory.deployModule(address(safe1), address(factory));
    }

    function testDeployModuleRevertsIfSafeAlreadyHasModule() public {
        // First deployment succeeds
        factory.deployModule(address(safe1), oracle);

        // Second deployment for same Safe should revert (pre-check before CREATE2)
        address existingModule = registry.safeToModule(address(safe1));
        vm.expectRevert(abi.encodeWithSelector(ModuleFactory.SafeAlreadyHasModule.selector, address(safe1), existingModule));
        factory.deployModule(address(safe1), oracle);
    }

    function testDeployModuleRevertsIfModuleAlreadyDeployed() public {
        // Deploy without auto-register to allow same nonce re-attempt
        factory.setAutoRegister(false);

        // First deployment succeeds
        address module = factory.deployModuleWithNonce(address(safe1), oracle, 0);

        // Trying to deploy with same nonce should revert (contract already exists)
        vm.expectRevert(abi.encodeWithSelector(ModuleFactory.ModuleAlreadyDeployed.selector, module));
        factory.deployModuleWithNonce(address(safe1), oracle, 0);
    }

    // ============ View Function Tests ============

    function testGetDeployedModule() public {
        (bool hasBefore, address moduleBefore) = factory.getDeployedModule(address(safe1));
        assertFalse(hasBefore);
        assertEq(moduleBefore, address(0));

        address deployed = factory.deployModule(address(safe1), oracle);

        (bool hasAfter, address moduleAfter) = factory.getDeployedModule(address(safe1));
        assertTrue(hasAfter);
        assertEq(moduleAfter, deployed);
    }

    function testGetNonce() public {
        assertEq(factory.getNonce(address(safe1)), 0);

        factory.deployModule(address(safe1), oracle);
        assertEq(factory.getNonce(address(safe1)), 1);
    }

    // ============ Cross-Chain Determinism Tests ============

    function testSameAddressAcrossFactories() public {
        // Deploy a second factory at a different address
        ModuleRegistry registry2 = new ModuleRegistry(owner);
        ModuleFactory factory2 = new ModuleFactory(owner, address(registry2), false);

        // Compute addresses - should be different because factories are at different addresses
        address predicted1 = factory.computeModuleAddress(address(safe1), oracle, 0);
        address predicted2 = factory2.computeModuleAddress(address(safe1), oracle, 0);

        // They should be different since factory addresses are different
        assertTrue(predicted1 != predicted2);
    }

    function testSameSaltSameNonce() public {
        // Verify that same safe + nonce always produces same salt
        bytes32 salt1 = factory.computeSalt(address(safe1), 0);
        bytes32 salt2 = factory.computeSalt(address(safe1), 0);
        assertEq(salt1, salt2);
    }
}
