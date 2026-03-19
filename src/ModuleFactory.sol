// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {DeFiInteractorModule} from "./DeFiInteractorModule.sol";
import {IModuleRegistry} from "./interfaces/IModuleRegistry.sol";

/**
 * @title ModuleFactory
 * @notice Permissionless factory for deploying DeFiInteractorModules with deterministic CREATE2 addresses
 * @dev Anyone can deploy a module for their Safe. Ensures same module address across all EVM chains for a given Safe.
 *      Admin functions (setRegistry, setAutoRegister) remain owner-only.
 */
contract ModuleFactory is Ownable {
    // ============ State Variables ============

    /// @notice The registry contract for auto-registration
    IModuleRegistry public registry;

    /// @notice Whether to auto-register deployed modules
    bool public autoRegister;

    /// @notice Tracks all deployments from this factory: safe => list of deployed modules
    mapping(address => address[]) private _deployedModules;

    /// @notice Nonce for additional salt uniqueness (increments after each deployment)
    mapping(address => uint256) public deploymentNonce;

    // ============ Events ============

    event ModuleDeployed(address indexed module, address indexed safe, address indexed oracle, bytes32 salt);
    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);
    event AutoRegisterToggled(bool enabled);

    // ============ Errors ============

    error InvalidAddress();
    error InvalidOracleAddress(address oracle);
    error DeploymentFailed();
    error ModuleAlreadyDeployed(address module);
    error SafeAlreadyHasModule(address safe, address existingModule);
    error RegistryNotSet();

    // ============ Constructor ============

    /**
     * @notice Initialize the factory
     * @param _initialOwner The initial owner (MultiClaw team address)
     * @param _registry The ModuleRegistry address (can be address(0) initially)
     * @param _autoRegister Whether to auto-register on deploy
     */
    constructor(address _initialOwner, address _registry, bool _autoRegister) Ownable(_initialOwner) {
        registry = IModuleRegistry(_registry);
        autoRegister = _autoRegister;
    }

    // ============ Configuration ============

    /**
     * @notice Set the registry address
     * @param _registry The new registry address (must not be address(0))
     */
    function setRegistry(address _registry) external onlyOwner {
        if (_registry == address(0)) revert InvalidAddress();
        address oldRegistry = address(registry);
        registry = IModuleRegistry(_registry);
        emit RegistryUpdated(oldRegistry, _registry);
    }

    /**
     * @notice Toggle auto-registration
     * @param _autoRegister Whether to auto-register
     */
    function setAutoRegister(bool _autoRegister) external onlyOwner {
        autoRegister = _autoRegister;
        emit AutoRegisterToggled(_autoRegister);
    }

    // ============ Salt Generation ============

    /**
     * @notice Generate deterministic salt for CREATE2
     * @param safe The Safe address
     * @param nonce Optional nonce for redeployments
     * @return salt The computed salt
     * @dev Salt = keccak256(abi.encodePacked(safe, nonce))
     *      This ensures same address across chains for same Safe
     */
    function computeSalt(address safe, uint256 nonce) public pure returns (bytes32) {
        return keccak256(abi.encodePacked(safe, nonce));
    }

    /**
     * @notice Compute the address a module would be deployed to
     * @param safe The Safe address
     * @param oracle The oracle address
     * @param nonce The nonce for salt generation
     * @return predicted The predicted module address
     */
    function computeModuleAddress(address safe, address oracle, uint256 nonce)
        external
        view
        returns (address predicted)
    {
        bytes32 salt = computeSalt(safe, nonce);
        bytes memory bytecode = _getCreationBytecode(safe, oracle);
        bytes32 hash = keccak256(abi.encodePacked(bytes1(0xff), address(this), salt, keccak256(bytecode)));
        return address(uint160(uint256(hash)));
    }

    // ============ Deployment Functions ============

    /**
     * @notice Deploy a new DeFiInteractorModule with CREATE2
     * @dev Permissionless — anyone can deploy a module for any Safe.
     *      The module's owner is set to the Safe address, so only the Safe can configure it.
     * @param safe The Safe address (avatar and owner)
     * @param oracle The authorized oracle address
     * @return module The deployed module address
     */
    function deployModule(address safe, address oracle) external returns (address module) {
        return _deployModule(safe, oracle, deploymentNonce[safe]);
    }

    /**
     * @notice Deploy with explicit nonce (for redeployments or testing)
     * @dev Permissionless — anyone can deploy. Module ownership is set to the Safe.
     * @param safe The Safe address
     * @param oracle The oracle address
     * @param nonce The nonce for salt generation
     * @return module The deployed module address
     */
    function deployModuleWithNonce(address safe, address oracle, uint256 nonce) external returns (address module) {
        return _deployModule(safe, oracle, nonce);
    }

    /**
     * @notice Internal deployment logic
     * @param safe The Safe address
     * @param oracle The oracle address
     * @param nonce The nonce for salt generation
     * @return module The deployed module address
     */
    function _deployModule(address safe, address oracle, uint256 nonce) internal returns (address module) {
        // Validate addresses
        if (safe == address(0) || oracle == address(0)) {
            revert InvalidAddress();
        }

        // Validate oracle is not Safe or Factory (prevents self-authorization)
        if (oracle == safe || oracle == address(this)) {
            revert InvalidOracleAddress(oracle);
        }

        // Always check registry for existing Safe module (prevents factory/registry desync
        // regardless of autoRegister setting - e.g., toggling autoRegister after deployment)
        if (address(registry) != address(0)) {
            address existingModule = registry.getModuleForSafe(safe);
            if (existingModule != address(0)) {
                revert SafeAlreadyHasModule(safe, existingModule);
            }
        }

        // Validate auto-register prerequisites before deployment
        if (autoRegister && address(registry) == address(0)) {
            revert RegistryNotSet();
        }

        bytes32 salt = computeSalt(safe, nonce);
        bytes memory bytecode = _getCreationBytecode(safe, oracle);

        // Check if contract already exists at predicted address
        address predictedAddress = _computeCreate2Address(salt, bytecode);
        if (predictedAddress.code.length > 0) {
            revert ModuleAlreadyDeployed(predictedAddress);
        }

        assembly {
            module := create2(0, add(bytecode, 0x20), mload(bytecode), salt)
        }

        if (module == address(0)) {
            revert DeploymentFailed();
        }

        // Update tracking
        _deployedModules[safe].push(module);
        deploymentNonce[safe] = nonce + 1;

        emit ModuleDeployed(module, safe, oracle, salt);

        // Auto-register if enabled
        if (autoRegister) {
            registry.registerModuleFromFactory(module, safe, oracle);
        }

        return module;
    }

    /**
     * @notice Compute CREATE2 address for given salt and bytecode
     * @param salt The salt for CREATE2
     * @param bytecode The creation bytecode
     * @return predicted The predicted address
     */
    function _computeCreate2Address(bytes32 salt, bytes memory bytecode) internal view returns (address predicted) {
        bytes32 hash = keccak256(abi.encodePacked(bytes1(0xff), address(this), salt, keccak256(bytecode)));
        return address(uint160(uint256(hash)));
    }

    /**
     * @notice Get creation bytecode for DeFiInteractorModule
     * @param safe The Safe address (avatar and owner)
     * @param oracle The oracle address
     * @return bytecode The creation bytecode with constructor args
     */
    function _getCreationBytecode(address safe, address oracle) internal pure returns (bytes memory) {
        return abi.encodePacked(
            type(DeFiInteractorModule).creationCode,
            abi.encode(safe, safe, oracle) // avatar, owner, authorizedOracle
        );
    }

    // ============ View Functions ============

    /**
     * @notice Get all modules deployed for a Safe
     * @param safe The Safe address
     * @return modules Array of deployed module addresses
     */
    function getDeployedModules(address safe) external view returns (address[] memory modules) {
        return _deployedModules[safe];
    }

    /**
     * @notice Get the latest module deployed for a Safe
     * @param safe The Safe address
     * @return hasModule Whether any module exists
     * @return module The latest module address (address(0) if none)
     */
    function getLatestDeployedModule(address safe) external view returns (bool hasModule, address module) {
        uint256 length = _deployedModules[safe].length;
        if (length == 0) {
            return (false, address(0));
        }
        return (true, _deployedModules[safe][length - 1]);
    }

    /**
     * @notice Get the number of modules deployed for a Safe
     * @param safe The Safe address
     * @return count Number of deployed modules
     */
    function getDeployedModuleCount(address safe) external view returns (uint256) {
        return _deployedModules[safe].length;
    }

    /**
     * @notice Get the current nonce for a Safe
     * @param safe The Safe address
     * @return nonce The current nonce
     */
    function getNonce(address safe) external view returns (uint256) {
        return deploymentNonce[safe];
    }
}
