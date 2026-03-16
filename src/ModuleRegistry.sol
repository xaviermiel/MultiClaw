// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {IModuleRegistry} from "./interfaces/IModuleRegistry.sol";

/**
 * @title ModuleRegistry
 * @notice Central registry for all deployed DeFiInteractorModules
 * @dev Only callable by MultiSub team (owner), not self-service
 */
contract ModuleRegistry is IModuleRegistry, Ownable {
    // ============ State Variables ============

    /// @notice All registered module addresses (compact array, swap-and-pop on removal)
    address[] private _allModules;

    /// @notice Module address => index in _allModules (for O(1) swap-and-pop removal)
    mapping(address => uint256) private _moduleIndex;

    /// @notice Module address => ModuleInfo
    mapping(address => ModuleInfo) private _moduleInfo;

    /// @notice Safe address => Module address (one module per Safe)
    mapping(address => address) public safeToModule;

    /// @notice Tracks if a module address is registered
    mapping(address => bool) private _isRegistered;

    /// @notice Authorized factory addresses that can auto-register
    mapping(address => bool) public authorizedFactories;

    /// @notice Cached count of active modules (avoids O(n) iteration)
    uint256 private _activeModuleCount;

    // ============ Events ============

    event ModuleRegistered(address indexed module, address indexed safe, address indexed oracle);
    event ModuleDeactivated(address indexed module);
    event ModuleReactivated(address indexed module);
    event ModuleRemoved(address indexed module);
    event FactoryAuthorized(address indexed factory);
    event FactoryDeauthorized(address indexed factory);

    // ============ Errors ============

    error ModuleAlreadyRegistered(address module);
    error ModuleNotRegistered(address module);
    error SafeAlreadyHasModule(address safe, address existingModule);
    error InvalidAddress();
    error OnlyAuthorizedFactory();

    // ============ Constructor ============

    /**
     * @notice Initialize the registry with an owner
     * @param _initialOwner The initial owner address (MultiSub team EOA)
     */
    constructor(address _initialOwner) Ownable(_initialOwner) {}

    // ============ Factory Authorization ============

    /**
     * @notice Authorize a factory to auto-register modules
     * @param factory The factory address to authorize
     */
    function authorizeFactory(address factory) external onlyOwner {
        if (factory == address(0)) revert InvalidAddress();
        authorizedFactories[factory] = true;
        emit FactoryAuthorized(factory);
    }

    /**
     * @notice Remove factory authorization
     * @param factory The factory address to deauthorize
     */
    function deauthorizeFactory(address factory) external onlyOwner {
        authorizedFactories[factory] = false;
        emit FactoryDeauthorized(factory);
    }

    // ============ Registration Functions ============

    /**
     * @notice Register a new module (owner only)
     * @param module The module address
     * @param safe The Safe address this module serves
     * @param oracle The authorized oracle address
     */
    function registerModule(address module, address safe, address oracle) external onlyOwner {
        _registerModule(module, safe, oracle);
    }

    /**
     * @notice Register a module from an authorized factory
     * @dev Called by ModuleFactory after CREATE2 deployment
     * @param module The deployed module address
     * @param safe The Safe address this module serves
     * @param oracle The authorized oracle address
     */
    function registerModuleFromFactory(address module, address safe, address oracle) external override {
        if (!authorizedFactories[msg.sender]) revert OnlyAuthorizedFactory();
        _registerModule(module, safe, oracle);
    }

    /**
     * @notice Internal registration logic
     * @param module The module address
     * @param safe The Safe address
     * @param oracle The oracle address
     */
    function _registerModule(address module, address safe, address oracle) internal {
        if (module == address(0) || safe == address(0) || oracle == address(0)) {
            revert InvalidAddress();
        }
        if (_isRegistered[module]) {
            revert ModuleAlreadyRegistered(module);
        }
        if (safeToModule[safe] != address(0)) {
            revert SafeAlreadyHasModule(safe, safeToModule[safe]);
        }

        _moduleInfo[module] =
            ModuleInfo({safeAddress: safe, authorizedOracle: oracle, deployedAt: block.timestamp, isActive: true});

        _moduleIndex[module] = _allModules.length;
        _allModules.push(module);
        safeToModule[safe] = module;
        _isRegistered[module] = true;
        _activeModuleCount++;

        emit ModuleRegistered(module, safe, oracle);
    }

    // ============ Module Management ============

    /**
     * @notice Deactivate a module (soft delete)
     * @param module The module address to deactivate
     */
    function deactivateModule(address module) external onlyOwner {
        if (!_isRegistered[module]) revert ModuleNotRegistered(module);
        if (_moduleInfo[module].isActive) {
            _moduleInfo[module].isActive = false;
            _activeModuleCount--;
        }
        emit ModuleDeactivated(module);
    }

    /**
     * @notice Reactivate a previously deactivated module
     * @param module The module address to reactivate
     */
    function reactivateModule(address module) external onlyOwner {
        if (!_isRegistered[module]) revert ModuleNotRegistered(module);
        if (!_moduleInfo[module].isActive) {
            _moduleInfo[module].isActive = true;
            _activeModuleCount++;
        }
        emit ModuleReactivated(module);
    }

    /**
     * @notice Remove a module entirely (hard delete, swap-and-pop)
     * @dev Removes from _allModules via swap-and-pop for O(1) removal,
     *      keeping the array compact and preventing unbounded growth.
     *      Also clears safeToModule mapping, allowing Safe to register a new module.
     * @param module The module address to remove
     */
    function removeModule(address module) external onlyOwner {
        if (!_isRegistered[module]) revert ModuleNotRegistered(module);

        // Decrement active count if module was active
        if (_moduleInfo[module].isActive) {
            _activeModuleCount--;
        }

        // Swap-and-pop removal from _allModules array
        uint256 index = _moduleIndex[module];
        uint256 lastIndex = _allModules.length - 1;
        if (index != lastIndex) {
            address lastModule = _allModules[lastIndex];
            _allModules[index] = lastModule;
            _moduleIndex[lastModule] = index;
        }
        _allModules.pop();
        delete _moduleIndex[module];

        address safe = _moduleInfo[module].safeAddress;
        delete safeToModule[safe];
        delete _moduleInfo[module];
        _isRegistered[module] = false;

        emit ModuleRemoved(module);
    }

    // ============ View Functions ============

    /**
     * @notice Get all active modules
     * @return modules Array of active module addresses
     * @dev Array is compact (swap-and-pop on removal), only deactivated entries need filtering
     */
    function getActiveModules() external view override returns (address[] memory) {
        uint256 count = _activeModuleCount;
        uint256 length = _allModules.length;

        address[] memory active = new address[](count);
        uint256 index = 0;
        for (uint256 i = 0; i < length && index < count; i++) {
            if (_moduleInfo[_allModules[i]].isActive) {
                active[index++] = _allModules[i];
            }
        }
        return active;
    }

    /**
     * @notice Get active modules with pagination
     * @param offset Starting index
     * @param limit Maximum number of modules to return
     * @return modules Array of active module addresses
     * @return total Total number of active modules
     * @dev Array is compact (swap-and-pop on removal), only deactivated entries need filtering
     */
    function getActiveModulesPaginated(uint256 offset, uint256 limit)
        external
        view
        override
        returns (address[] memory modules, uint256 total)
    {
        total = _activeModuleCount;
        uint256 length = _allModules.length;

        // Calculate return size
        uint256 remaining = offset < total ? total - offset : 0;
        uint256 returnSize = remaining < limit ? remaining : limit;
        modules = new address[](returnSize);

        if (returnSize == 0) return (modules, total);

        // Populate results
        uint256 activeIndex = 0;
        uint256 resultIndex = 0;
        for (uint256 i = 0; i < length && resultIndex < returnSize; i++) {
            if (_moduleInfo[_allModules[i]].isActive) {
                if (activeIndex >= offset) {
                    modules[resultIndex++] = _allModules[i];
                }
                activeIndex++;
            }
        }

        return (modules, total);
    }

    /**
     * @notice Get the module for a specific Safe
     * @param safe The Safe address
     * @return module The module address (address(0) if none)
     */
    function getModuleForSafe(address safe) external view override returns (address) {
        return safeToModule[safe];
    }

    /**
     * @notice Get count of active modules
     * @return count Number of active modules
     * @dev Uses cached count for O(1) gas efficiency
     */
    function getActiveModuleCount() external view override returns (uint256 count) {
        return _activeModuleCount;
    }

    /**
     * @notice Get total count of all registered modules (including inactive but not removed)
     * @return count Total number of currently registered modules
     */
    function getTotalModuleCount() external view returns (uint256) {
        return _allModules.length;
    }

    /**
     * @notice Check if a module is registered
     * @param module The module address
     * @return registered Whether the module is registered
     */
    function isRegistered(address module) external view override returns (bool) {
        return _isRegistered[module];
    }

    /**
     * @notice Get module information
     * @param module The module address
     * @return info The module information
     */
    function moduleInfo(address module) external view override returns (ModuleInfo memory) {
        return _moduleInfo[module];
    }
}
