// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title IModuleRegistry
 * @notice Interface for the ModuleRegistry contract
 * @dev Used by ModuleFactory for auto-registration
 */
interface IModuleRegistry {
    /// @notice Module information struct
    struct ModuleInfo {
        address safeAddress;
        address authorizedOracle;
        uint256 deployedAt;
        bool isActive;
    }

    /// @notice Register a module from an authorized factory
    /// @param module The deployed module address
    /// @param safe The Safe address this module serves
    /// @param oracle The authorized oracle address
    function registerModuleFromFactory(address module, address safe, address oracle) external;

    /// @notice Deactivate a module (soft delete)
    /// @param module The module address to deactivate
    function deactivateModule(address module) external;

    /// @notice Reactivate a previously deactivated module
    /// @param module The module address to reactivate
    function reactivateModule(address module) external;

    /// @notice Remove a module entirely (hard delete, swap-and-pop from array)
    /// @param module The module address to remove
    function removeModule(address module) external;

    /// @notice Authorize a factory to auto-register modules
    /// @param factory The factory address to authorize
    function authorizeFactory(address factory) external;

    /// @notice Remove factory authorization
    /// @param factory The factory address to deauthorize
    function deauthorizeFactory(address factory) external;

    /// @notice Get all active modules
    /// @return modules Array of active module addresses
    function getActiveModules() external view returns (address[] memory modules);

    /// @notice Get active modules with pagination
    /// @param offset Starting index
    /// @param limit Maximum number of modules to return
    /// @return modules Array of active module addresses
    /// @return total Total number of active modules
    function getActiveModulesPaginated(uint256 offset, uint256 limit)
        external
        view
        returns (address[] memory modules, uint256 total);

    /// @notice Get the module for a specific Safe
    /// @param safe The Safe address
    /// @return module The module address (address(0) if none)
    function getModuleForSafe(address safe) external view returns (address module);

    /// @notice Get count of active modules
    /// @return count Number of active modules
    function getActiveModuleCount() external view returns (uint256 count);

    /// @notice Check if a module is registered
    /// @param module The module address
    /// @return registered Whether the module is registered
    function isRegistered(address module) external view returns (bool registered);

    /// @notice Get module information
    /// @param module The module address
    /// @return info The module information
    function moduleInfo(address module) external view returns (ModuleInfo memory info);
}
