// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {DeFiInteractorModule} from "./DeFiInteractorModule.sol";
import {IModuleRegistry} from "./interfaces/IModuleRegistry.sol";
import {PresetRegistry} from "./PresetRegistry.sol";

/**
 * @title AgentVaultFactory
 * @notice Permissionless factory that deploys a fully-configured DeFiInteractorModule in one transaction
 * @dev Anyone can deploy an agent vault. The factory deploys with itself as temporary owner,
 *      configures everything, then transfers ownership to the Safe — so only the Safe can
 *      modify the module after deployment. Admin functions (setRegistry, setPresetRegistry) remain owner-only.
 *
 *      Supports two flows:
 *      - deployVault(VaultConfig): Full custom configuration
 *      - deployVaultFromPreset(safe, oracle, agent, presetId, priceFeeds): Use a template from PresetRegistry
 */
contract AgentVaultFactory is Ownable {
    // ============ State Variables ============

    /// @notice Registry for module registration
    IModuleRegistry public registry;

    /// @notice Preset registry for template configurations
    PresetRegistry public presetRegistry;

    /// @notice Nonce per Safe for deterministic CREATE2 deployment
    mapping(address => uint256) public vaultNonce;

    /// @notice All modules deployed by this factory per Safe
    mapping(address => address[]) private _deployedModules;

    // ============ Structs ============

    /// @notice Full vault configuration for custom deployments
    struct VaultConfig {
        address safe;
        address oracle;
        address agentAddress;
        uint16 roleId;
        uint256 maxSpendingBps;
        uint256 windowDuration;
        address[] allowedProtocols;
        address[] parserProtocols;
        address[] parserAddresses;
        bytes4[] selectors;
        uint8[] selectorTypes;
        address[] priceFeedTokens;
        address[] priceFeedAddresses;
    }

    // ============ Events ============

    event AgentVaultCreated(address indexed safe, address indexed agentAddress, address module, uint256 presetId);
    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);
    event PresetRegistryUpdated(address indexed oldPresetRegistry, address indexed newPresetRegistry);

    // ============ Errors ============

    error InvalidAddress();
    error InvalidConfig();
    error PresetRegistryNotSet();
    error ArrayLengthMismatch();
    error SafeAlreadyHasModule(address safe, address existingModule);

    // ============ Constructor ============

    /**
     * @notice Initialize the AgentVaultFactory
     * @param _initialOwner The factory owner (Multisub team)
     * @param _registry The ModuleRegistry address (can be address(0) initially)
     * @param _presetRegistry The PresetRegistry address (can be address(0) if not using presets)
     */
    constructor(address _initialOwner, address _registry, address _presetRegistry) Ownable(_initialOwner) {
        registry = IModuleRegistry(_registry);
        presetRegistry = PresetRegistry(_presetRegistry);
    }

    // ============ Configuration ============

    /**
     * @notice Set the module registry address
     * @param _registry The new registry address
     */
    function setRegistry(address _registry) external onlyOwner {
        if (_registry == address(0)) revert InvalidAddress();
        address old = address(registry);
        registry = IModuleRegistry(_registry);
        emit RegistryUpdated(old, _registry);
    }

    /**
     * @notice Set the preset registry address
     * @param _presetRegistry The new preset registry address
     */
    function setPresetRegistry(address _presetRegistry) external onlyOwner {
        if (_presetRegistry == address(0)) revert InvalidAddress();
        address old = address(presetRegistry);
        presetRegistry = PresetRegistry(_presetRegistry);
        emit PresetRegistryUpdated(old, _presetRegistry);
    }

    // ============ Deployment: Custom Config ============

    /**
     * @notice Deploy a fully-configured agent vault with custom configuration
     * @dev Permissionless — anyone can deploy. Module ownership transfers to the Safe after configuration,
     *      so only the Safe can modify the module post-deployment.
     *      User must separately enable the module on their Safe (requires Safe multisig tx).
     * @param config The full vault configuration
     * @return module The deployed module address
     */
    function deployVault(VaultConfig calldata config) external returns (address module) {
        // Validate inputs
        _validateConfig(config.safe, config.oracle, config.agentAddress);
        if (config.parserProtocols.length != config.parserAddresses.length) revert ArrayLengthMismatch();
        if (config.selectors.length != config.selectorTypes.length) revert ArrayLengthMismatch();
        if (config.priceFeedTokens.length != config.priceFeedAddresses.length) revert ArrayLengthMismatch();

        // Check registry for existing module
        _checkNoExistingModule(config.safe);

        // 1. Deploy module with CREATE2 (factory is temporary owner)
        module = _deployModule(config.safe, config.oracle);
        DeFiInteractorModule m = DeFiInteractorModule(module);

        // 2. Configure module (factory is owner, so these calls succeed)
        m.grantRole(config.agentAddress, config.roleId);
        m.setSubAccountLimits(config.agentAddress, config.maxSpendingBps, config.windowDuration);
        if (config.allowedProtocols.length > 0) {
            m.setAllowedAddresses(config.agentAddress, config.allowedProtocols, true);
        }
        _configureParsers(m, config.parserProtocols, config.parserAddresses);
        _configureSelectors(m, config.selectors, config.selectorTypes);
        if (config.priceFeedTokens.length > 0) {
            m.setTokenPriceFeeds(config.priceFeedTokens, config.priceFeedAddresses);
        }

        // 3. Transfer ownership to Safe (factory can no longer configure)
        m.transferOwnership(config.safe);

        // 4. Register in ModuleRegistry (for oracle discovery)
        _registerModule(module, config.safe, config.oracle);

        emit AgentVaultCreated(config.safe, config.agentAddress, module, type(uint256).max);
    }

    // ============ Deployment: From Preset ============

    /**
     * @notice Deploy a vault using a preset template from PresetRegistry
     * @dev Permissionless — anyone can deploy. Module ownership transfers to the Safe.
     * @param safe The Safe address
     * @param oracle The oracle address
     * @param agentAddress The AI agent's EOA
     * @param presetId The preset template ID (from PresetRegistry)
     * @param priceFeedTokens Token addresses for price feeds (chain-specific)
     * @param priceFeedAddresses Chainlink price feed addresses (chain-specific)
     * @return module The deployed module address
     */
    function deployVaultFromPreset(
        address safe,
        address oracle,
        address agentAddress,
        uint256 presetId,
        address[] calldata priceFeedTokens,
        address[] calldata priceFeedAddresses
    ) external returns (address module) {
        if (address(presetRegistry) == address(0)) revert PresetRegistryNotSet();
        _validateConfig(safe, oracle, agentAddress);
        if (priceFeedTokens.length != priceFeedAddresses.length) revert ArrayLengthMismatch();

        // Check registry for existing module
        _checkNoExistingModule(safe);

        // Read preset from registry
        (
            uint16 roleId,
            uint256 maxSpendingBps,
            uint256 windowDuration,
            address[] memory allowedProtocols,
            address[] memory parserProtocols,
            address[] memory parserAddresses,
            bytes4[] memory selectors,
            uint8[] memory selectorTypes
        ) = presetRegistry.getPresetFull(presetId);

        // 1. Deploy module with CREATE2
        module = _deployModule(safe, oracle);
        DeFiInteractorModule m = DeFiInteractorModule(module);

        // 2. Configure from preset
        m.grantRole(agentAddress, roleId);
        m.setSubAccountLimits(agentAddress, maxSpendingBps, windowDuration);
        if (allowedProtocols.length > 0) {
            m.setAllowedAddresses(agentAddress, allowedProtocols, true);
        }
        _configureParsers(m, parserProtocols, parserAddresses);
        _configureSelectors(m, selectors, selectorTypes);
        if (priceFeedTokens.length > 0) {
            m.setTokenPriceFeeds(priceFeedTokens, priceFeedAddresses);
        }

        // 3. Transfer ownership to Safe
        m.transferOwnership(safe);

        // 4. Register in registry
        _registerModule(module, safe, oracle);

        emit AgentVaultCreated(safe, agentAddress, module, presetId);
    }

    // ============ Internal: Deployment ============

    /**
     * @notice Deploy a DeFiInteractorModule with CREATE2
     * @dev Module is deployed with avatar=safe, owner=factory (temporary), oracle.
     *      Factory configures the module, then transfers ownership to the Safe.
     * @param safe The Safe address (becomes avatar and eventual owner)
     * @param oracle The authorized oracle address
     * @return module The deployed module address
     */
    function _deployModule(address safe, address oracle) internal returns (address module) {
        uint256 nonce = vaultNonce[safe]++;
        bytes32 salt = keccak256(abi.encodePacked(safe, nonce));

        // Deploy with avatar=safe, owner=this (factory), oracle
        module = address(
            new DeFiInteractorModule{salt: salt}(
                safe, // avatar (the Safe)
                address(this), // temporary owner (factory configures, then transfers)
                oracle // authorized oracle
            )
        );

        _deployedModules[safe].push(module);
    }

    // ============ Internal: Configuration ============

    /// @notice Register parsers for each protocol address
    function _configureParsers(DeFiInteractorModule m, address[] memory protocols, address[] memory parsers) internal {
        for (uint256 i = 0; i < protocols.length; i++) {
            m.registerParser(protocols[i], parsers[i]);
        }
    }

    /// @notice Register function selectors with their operation types
    function _configureSelectors(DeFiInteractorModule m, bytes4[] memory selectors, uint8[] memory selectorTypes)
        internal
    {
        for (uint256 i = 0; i < selectors.length; i++) {
            m.registerSelector(selectors[i], DeFiInteractorModule.OperationType(selectorTypes[i]));
        }
    }

    // ============ Internal: Validation ============

    /// @notice Validate that core addresses are non-zero and oracle != safe
    function _validateConfig(address safe, address oracle, address agentAddress) internal pure {
        if (safe == address(0) || oracle == address(0) || agentAddress == address(0)) {
            revert InvalidAddress();
        }
        if (oracle == safe) revert InvalidConfig();
    }

    /// @notice Check that the Safe doesn't already have a module registered
    function _checkNoExistingModule(address safe) internal view {
        if (address(registry) != address(0)) {
            address existing = registry.getModuleForSafe(safe);
            if (existing != address(0)) {
                revert SafeAlreadyHasModule(safe, existing);
            }
        }
    }

    /// @notice Register the module in ModuleRegistry (for oracle multi-module discovery)
    function _registerModule(address module, address safe, address oracle) internal {
        if (address(registry) != address(0)) {
            registry.registerModuleFromFactory(module, safe, oracle);
        }
    }

    // ============ View Functions ============

    /**
     * @notice Compute the address a module would be deployed to
     * @param safe The Safe address
     * @param oracle The oracle address
     * @return predicted The predicted module address
     */
    function computeModuleAddress(address safe, address oracle) external view returns (address predicted) {
        uint256 nonce = vaultNonce[safe];
        bytes32 salt = keccak256(abi.encodePacked(safe, nonce));
        bytes memory bytecode =
            abi.encodePacked(type(DeFiInteractorModule).creationCode, abi.encode(safe, address(this), oracle));
        bytes32 hash = keccak256(abi.encodePacked(bytes1(0xff), address(this), salt, keccak256(bytecode)));
        return address(uint160(uint256(hash)));
    }

    /**
     * @notice Get all modules deployed for a Safe
     * @param safe The Safe address
     * @return modules Array of deployed module addresses
     */
    function getDeployedModules(address safe) external view returns (address[] memory) {
        return _deployedModules[safe];
    }

    /**
     * @notice Get the current nonce for a Safe
     * @param safe The Safe address
     * @return nonce The current nonce
     */
    function getNonce(address safe) external view returns (uint256) {
        return vaultNonce[safe];
    }
}
