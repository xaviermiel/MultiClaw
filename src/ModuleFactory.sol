// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {DeFiInteractorModule} from "./DeFiInteractorModule.sol";
import {IModuleRegistry} from "./interfaces/IModuleRegistry.sol";

/**
 * @title ModuleFactory
 * @notice Factory for deploying DeFiInteractorModules with deterministic CREATE2 addresses
 * @dev Ensures same module address across all EVM chains for a given Safe
 */
contract ModuleFactory is Ownable {
    // ============ State Variables ============

    /// @notice The registry contract for auto-registration
    IModuleRegistry public registry;

    /// @notice Whether to auto-register deployed modules
    bool public autoRegister;

    /// @notice Tracks all deployments from this factory: safe => module
    mapping(address => address) public deployedModules;

    /// @notice Nonce for additional salt uniqueness (increments after each deployment)
    mapping(address => uint256) public deploymentNonce;

    // ============ Events ============

    event ModuleDeployed(
        address indexed module,
        address indexed safe,
        address indexed oracle,
        bytes32 salt
    );
    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);
    event AutoRegisterToggled(bool enabled);

    // ============ Errors ============

    error InvalidAddress();
    error DeploymentFailed();
    error RegistryNotSet();

    // ============ Constructor ============

    /**
     * @notice Initialize the factory
     * @param _initialOwner The initial owner (MultiSub team address)
     * @param _registry The ModuleRegistry address (can be address(0) initially)
     * @param _autoRegister Whether to auto-register on deploy
     */
    constructor(
        address _initialOwner,
        address _registry,
        bool _autoRegister
    ) Ownable(_initialOwner) {
        registry = IModuleRegistry(_registry);
        autoRegister = _autoRegister;
    }

    // ============ Configuration ============

    /**
     * @notice Set the registry address
     * @param _registry The new registry address
     */
    function setRegistry(address _registry) external onlyOwner {
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
    function computeModuleAddress(
        address safe,
        address oracle,
        uint256 nonce
    ) external view returns (address predicted) {
        bytes32 salt = computeSalt(safe, nonce);
        bytes memory bytecode = _getCreationBytecode(safe, oracle);
        bytes32 hash = keccak256(
            abi.encodePacked(
                bytes1(0xff),
                address(this),
                salt,
                keccak256(bytecode)
            )
        );
        return address(uint160(uint256(hash)));
    }

    // ============ Deployment Functions ============

    /**
     * @notice Deploy a new DeFiInteractorModule with CREATE2
     * @param safe The Safe address (avatar and owner)
     * @param oracle The authorized oracle address
     * @return module The deployed module address
     */
    function deployModule(
        address safe,
        address oracle
    ) external onlyOwner returns (address module) {
        return _deployModule(safe, oracle, deploymentNonce[safe]);
    }

    /**
     * @notice Deploy with explicit nonce (for redeployments or testing)
     * @param safe The Safe address
     * @param oracle The oracle address
     * @param nonce The nonce for salt generation
     * @return module The deployed module address
     */
    function deployModuleWithNonce(
        address safe,
        address oracle,
        uint256 nonce
    ) external onlyOwner returns (address module) {
        return _deployModule(safe, oracle, nonce);
    }

    /**
     * @notice Internal deployment logic
     * @param safe The Safe address
     * @param oracle The oracle address
     * @param nonce The nonce for salt generation
     * @return module The deployed module address
     */
    function _deployModule(
        address safe,
        address oracle,
        uint256 nonce
    ) internal returns (address module) {
        if (safe == address(0) || oracle == address(0)) {
            revert InvalidAddress();
        }

        bytes32 salt = computeSalt(safe, nonce);
        bytes memory bytecode = _getCreationBytecode(safe, oracle);

        assembly {
            module := create2(0, add(bytecode, 0x20), mload(bytecode), salt)
        }

        if (module == address(0)) {
            revert DeploymentFailed();
        }

        // Update tracking
        deployedModules[safe] = module;
        deploymentNonce[safe] = nonce + 1;

        emit ModuleDeployed(module, safe, oracle, salt);

        // Auto-register if enabled
        if (autoRegister) {
            if (address(registry) == address(0)) {
                revert RegistryNotSet();
            }
            registry.registerModuleFromFactory(module, safe, oracle);
        }

        return module;
    }

    /**
     * @notice Get creation bytecode for DeFiInteractorModule
     * @param safe The Safe address (avatar and owner)
     * @param oracle The oracle address
     * @return bytecode The creation bytecode with constructor args
     */
    function _getCreationBytecode(
        address safe,
        address oracle
    ) internal pure returns (bytes memory) {
        return abi.encodePacked(
            type(DeFiInteractorModule).creationCode,
            abi.encode(safe, safe, oracle) // avatar, owner, authorizedOracle
        );
    }

    // ============ View Functions ============

    /**
     * @notice Check if a module has been deployed for a Safe
     * @param safe The Safe address
     * @return hasModule Whether a module exists
     * @return module The module address (address(0) if none)
     */
    function getDeployedModule(address safe) external view returns (bool hasModule, address module) {
        module = deployedModules[safe];
        hasModule = module != address(0);
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
