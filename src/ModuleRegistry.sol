// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {EnumerableSet} from "@openzeppelin/contracts/utils/structs/EnumerableSet.sol";
import {IModuleRegistry} from "./interfaces/IModuleRegistry.sol";

/**
 * @title ModuleRegistry
 * @notice Central registry for all deployed DeFiInteractorModules
 * @dev Registration is open to authorized factories (permissionless deployment via factories).
 *      Admin functions (deactivate, remove, authorize/deauthorize factories) remain owner-only.
 *
 *      Since deployment is permissionless, anyone can deploy a module naming any oracle. Each
 *      oracle therefore has an access policy here, which it (or a manager it designates) sets:
 *      open (the default) serves every active module, restricted serves only the modules it
 *      approved and those deployed by deployers it trusts. The policy tells oracles which modules
 *      to serve (getModulesServedBy); modules don't enforce it.
 */
contract ModuleRegistry is IModuleRegistry, Ownable {
    using EnumerableSet for EnumerableSet.AddressSet;

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

    /// @notice Module => the account that deployed it through an authorized factory
    ///         (address(0) for modules registered by the owner)
    mapping(address => address) public moduleDeployer;

    /// @notice Deployer => modules it deployed through an authorized factory
    mapping(address => address[]) private _deployerModules;

    /// @notice Oracle => account that may set its access policy besides the oracle itself
    mapping(address => address) public oracleManager;

    /// @notice Oracle => whether it serves only the modules it approved and those of the deployers it trusts
    mapping(address => bool) public oracleRestricted;

    /// @notice Oracle => deployers whose modules it serves when restricted
    mapping(address => EnumerableSet.AddressSet) private _trustedDeployers;

    /// @notice Oracle => modules it serves when restricted
    mapping(address => EnumerableSet.AddressSet) private _approvedModules;

    // ============ Events ============

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

    // ============ Errors ============

    error ModuleAlreadyRegistered(address module);
    error ModuleNotRegistered(address module);
    error SafeAlreadyHasModule(address safe, address existingModule);
    error InvalidAddress();
    error OnlyAuthorizedFactory();
    error NotOracleManager(address oracle, address caller);

    // ============ Modifiers ============

    /// @dev Only the oracle itself or the manager it designated may set its policy
    modifier onlyOracleOrManager(address oracle) {
        if (msg.sender != oracle && msg.sender != oracleManager[oracle]) {
            revert NotOracleManager(oracle, msg.sender);
        }
        _;
    }

    // ============ Constructor ============

    /**
     * @notice Initialize the registry with an owner
     * @param _initialOwner The initial owner address (MultiClaw team EOA)
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
     * @dev The module has no recorded deployer: restricted oracles serve it only if they approve it.
     * @param module The module address
     * @param safe The Safe address this module serves
     * @param oracle The authorized oracle address
     */
    function registerModule(address module, address safe, address oracle) external onlyOwner {
        _registerModule(module, safe, oracle, address(0));
    }

    /**
     * @notice Register a module from an authorized factory
     * @dev Called by the factories after deployment
     * @param module The deployed module address
     * @param safe The Safe address this module serves
     * @param oracle The authorized oracle address
     * @param deployer The account that called the factory (recorded for oracle access policies)
     */
    function registerModuleFromFactory(address module, address safe, address oracle, address deployer)
        external
        override
    {
        if (!authorizedFactories[msg.sender]) revert OnlyAuthorizedFactory();
        _registerModule(module, safe, oracle, deployer);
    }

    /**
     * @notice Internal registration logic
     * @param module The module address
     * @param safe The Safe address
     * @param oracle The oracle address
     * @param deployer The account that deployed the module through a factory (address(0) if none)
     */
    function _registerModule(address module, address safe, address oracle, address deployer) internal {
        // oracle can be address(0) for oracleless modules
        if (module == address(0) || safe == address(0)) {
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
        if (deployer != address(0)) {
            moduleDeployer[module] = deployer;
            _deployerModules[deployer].push(module);
        }

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
        // Its _deployerModules entry stays; it is skipped once the module has no deployer
        delete moduleDeployer[module];
        _isRegistered[module] = false;

        emit ModuleRemoved(module);
    }

    // ============ Oracle Access Policies ============

    /**
     * @notice Let `manager` set the calling oracle's access policy, so that the oracle's own key
     *         needn't (address(0) removes the manager; the oracle can always set it itself)
     * @param manager The account that may manage the policy of msg.sender
     */
    function setOracleManager(address manager) external {
        oracleManager[msg.sender] = manager;
        emit OracleManagerSet(msg.sender, manager);
    }

    /**
     * @notice Restrict an oracle to the modules it approved and those deployed by deployers it
     *         trusts, or open it to every active module (the default)
     * @param oracle The oracle whose policy is set
     * @param restricted Whether the oracle is restricted
     */
    function setOracleRestricted(address oracle, bool restricted) external onlyOracleOrManager(oracle) {
        oracleRestricted[oracle] = restricted;
        emit OracleRestrictionSet(oracle, restricted);
    }

    /**
     * @notice Trust (or stop trusting) deployers: a restricted oracle serves the modules they
     *         deploy through an authorized factory, including future ones
     * @param oracle The oracle whose policy is set
     * @param deployers The deployer accounts
     * @param trusted Whether to trust them
     */
    function setTrustedDeployers(address oracle, address[] calldata deployers, bool trusted)
        external
        onlyOracleOrManager(oracle)
    {
        EnumerableSet.AddressSet storage set = _trustedDeployers[oracle];
        for (uint256 i = 0; i < deployers.length; i++) {
            if (deployers[i] == address(0)) revert InvalidAddress();
            if (trusted) {
                set.add(deployers[i]);
            } else {
                set.remove(deployers[i]);
            }
            emit OracleDeployerTrustSet(oracle, deployers[i], trusted);
        }
    }

    /**
     * @notice Approve (or stop approving) modules: a restricted oracle serves them while they are
     *         active. A module can be approved before it is registered (at its predicted address).
     * @param oracle The oracle whose policy is set
     * @param modules The module addresses
     * @param approved Whether to approve them
     */
    function setApprovedModules(address oracle, address[] calldata modules, bool approved)
        external
        onlyOracleOrManager(oracle)
    {
        EnumerableSet.AddressSet storage set = _approvedModules[oracle];
        for (uint256 i = 0; i < modules.length; i++) {
            if (modules[i] == address(0)) revert InvalidAddress();
            if (approved) {
                set.add(modules[i]);
            } else {
                set.remove(modules[i]);
            }
            emit OracleModuleApprovalSet(oracle, modules[i], approved);
        }
    }

    // ============ View Functions ============

    /**
     * @notice Get all active modules
     * @return modules Array of active module addresses
     * @dev Array is compact (swap-and-pop on removal), only deactivated entries need filtering
     */
    function getActiveModules() public view override returns (address[] memory) {
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

    // ============ Oracle Access Policy Views ============

    /**
     * @notice Whether an oracle's access policy covers a module: the module is registered and
     *         active, and the oracle is open, approved it, or trusts its deployer
     * @dev Whether the module names this oracle is up to the module (its authorizedOracle).
     * @param module The module address
     * @param oracle The oracle address
     * @return served Whether the oracle serves the module
     */
    function isServedBy(address module, address oracle) external view override returns (bool) {
        if (!_isActive(module)) return false;
        if (!oracleRestricted[oracle] || _approvedModules[oracle].contains(module)) return true;
        address deployer = moduleDeployer[module];
        return deployer != address(0) && _trustedDeployers[oracle].contains(deployer);
    }

    /**
     * @notice The active modules an oracle serves: every active module while it is open; when
     *         restricted, the active modules it approved, then those of the deployers it trusts
     * @dev When restricted, the cost grows with the oracle's own modules, not the registry's size.
     * @param oracle The oracle address
     * @return modules The modules, without duplicates
     */
    function getModulesServedBy(address oracle) external view override returns (address[] memory modules) {
        if (!oracleRestricted[oracle]) return getActiveModules();

        EnumerableSet.AddressSet storage approved = _approvedModules[oracle];
        EnumerableSet.AddressSet storage deployers = _trustedDeployers[oracle];
        uint256 maxCount = approved.length();
        for (uint256 i = 0; i < deployers.length(); i++) {
            maxCount += _deployerModules[deployers.at(i)].length;
        }

        modules = new address[](maxCount);
        uint256 count = 0;
        for (uint256 i = 0; i < approved.length(); i++) {
            address module = approved.at(i);
            if (_isActive(module)) modules[count++] = module;
        }
        for (uint256 i = 0; i < deployers.length(); i++) {
            address deployer = deployers.at(i);
            address[] storage deployed = _deployerModules[deployer];
            for (uint256 j = 0; j < deployed.length; j++) {
                address module = deployed[j];
                // Skip removed modules (no deployer any more) and approved ones (already listed)
                if (_isActive(module) && moduleDeployer[module] == deployer && !approved.contains(module)) {
                    modules[count++] = module;
                }
            }
        }
        // Shrink the array to the modules found
        assembly ("memory-safe") {
            mstore(modules, count)
        }
    }

    /**
     * @notice Whether an oracle trusts a deployer, e.g. to check before deploying a vault that a
     *         restricted oracle will serve it
     * @param oracle The oracle address
     * @param deployer The deployer account
     * @return trusted Whether the oracle trusts the deployer
     */
    function isTrustedDeployer(address oracle, address deployer) external view returns (bool) {
        return _trustedDeployers[oracle].contains(deployer);
    }

    /**
     * @notice Whether an oracle approved a module
     * @param oracle The oracle address
     * @param module The module address
     * @return approved Whether the oracle approved the module
     */
    function isApprovedModule(address oracle, address module) external view returns (bool) {
        return _approvedModules[oracle].contains(module);
    }

    /**
     * @notice The deployers an oracle trusts
     * @param oracle The oracle address
     * @return deployers The trusted deployers
     */
    function getTrustedDeployers(address oracle) external view returns (address[] memory) {
        return _trustedDeployers[oracle].values();
    }

    /**
     * @notice The modules an oracle approved (registered or not, active or not)
     * @param oracle The oracle address
     * @return modules The approved modules
     */
    function getApprovedModules(address oracle) external view returns (address[] memory) {
        return _approvedModules[oracle].values();
    }

    /**
     * @notice The modules an account deployed through an authorized factory (removed ones
     *         included, though their moduleDeployer is cleared)
     * @param deployer The deployer account
     * @return modules The modules
     */
    function getDeployerModules(address deployer) external view returns (address[] memory) {
        return _deployerModules[deployer];
    }

    /// @dev Whether a module is registered and not deactivated
    function _isActive(address module) internal view returns (bool) {
        return _isRegistered[module] && _moduleInfo[module].isActive;
    }
}
