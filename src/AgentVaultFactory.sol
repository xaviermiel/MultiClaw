// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import {DeFiInteractorModule} from "./DeFiInteractorModule.sol";
import {IModuleRegistry} from "./interfaces/IModuleRegistry.sol";

/**
 * @title AgentVaultFactory
 * @notice Higher-level factory that deploys a fully-configured DeFiInteractorModule in one transaction
 * @dev Deploys module with factory as temporary owner, configures everything, then transfers ownership to Safe.
 *      The user must separately enable the module on their Safe (requires Safe multisig tx).
 *
 *      Supports two flows:
 *      - deployVault(VaultConfig): Full custom configuration
 *      - deployVaultFromPreset(safe, oracle, agent, presetId): Use a pre-configured template
 */
contract AgentVaultFactory is Ownable {
    // ============ State Variables ============

    /// @notice Registry for module registration
    IModuleRegistry public registry;

    /// @notice Nonce per Safe for deterministic CREATE2 deployment
    mapping(address => uint256) public vaultNonce;

    /// @notice All modules deployed by this factory per Safe
    mapping(address => address[]) private _deployedModules;

    /// @notice Stored presets by ID
    mapping(uint256 => Preset) private _presets;

    /// @notice Number of configured presets
    uint256 public presetCount;

    // ============ Structs ============

    /// @notice Template preset for common vault configurations
    struct Preset {
        string name;
        bool exists;
        uint16 roleId;
        uint256 maxSpendingBps;
        uint256 windowDuration;
        address[] allowedProtocols;
        address[] parserProtocols;
        address[] parserAddresses;
        bytes4[] selectors;
        uint8[] selectorTypes;
    }

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
    event PresetCreated(uint256 indexed presetId, string name);
    event PresetUpdated(uint256 indexed presetId, string name);
    event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry);

    // ============ Errors ============

    error InvalidAddress();
    error InvalidConfig();
    error PresetNotFound(uint256 presetId);
    error ArrayLengthMismatch();
    error SafeAlreadyHasModule(address safe, address existingModule);

    // ============ Constructor ============

    /**
     * @notice Initialize the AgentVaultFactory
     * @param _initialOwner The factory owner (Multisub team)
     * @param _registry The ModuleRegistry address (can be address(0) initially)
     */
    constructor(address _initialOwner, address _registry) Ownable(_initialOwner) {
        registry = IModuleRegistry(_registry);
    }

    // ============ Configuration ============

    /**
     * @notice Set the registry address
     * @param _registry The new registry address
     */
    function setRegistry(address _registry) external onlyOwner {
        if (_registry == address(0)) revert InvalidAddress();
        address oldRegistry = address(registry);
        registry = IModuleRegistry(_registry);
        emit RegistryUpdated(oldRegistry, _registry);
    }

    // ============ Preset Management ============

    /**
     * @notice Create a new preset template
     * @param name Human-readable preset name
     * @param roleId Role to grant the agent (1=EXECUTE, 2=TRANSFER)
     * @param maxSpendingBps Spending limit in basis points
     * @param windowDuration Time window in seconds
     * @param allowedProtocols Protocols the agent can interact with
     * @param parserProtocols Protocol addresses for parser registration
     * @param parserAddresses Parser contract addresses
     * @param selectors Function selectors to register
     * @param selectorTypes OperationType for each selector
     * @return presetId The ID of the created preset
     */
    function createPreset(
        string calldata name,
        uint16 roleId,
        uint256 maxSpendingBps,
        uint256 windowDuration,
        address[] calldata allowedProtocols,
        address[] calldata parserProtocols,
        address[] calldata parserAddresses,
        bytes4[] calldata selectors,
        uint8[] calldata selectorTypes
    ) external onlyOwner returns (uint256 presetId) {
        if (parserProtocols.length != parserAddresses.length) revert ArrayLengthMismatch();
        if (selectors.length != selectorTypes.length) revert ArrayLengthMismatch();

        presetId = presetCount++;

        Preset storage p = _presets[presetId];
        p.name = name;
        p.exists = true;
        p.roleId = roleId;
        p.maxSpendingBps = maxSpendingBps;
        p.windowDuration = windowDuration;
        p.allowedProtocols = allowedProtocols;
        p.parserProtocols = parserProtocols;
        p.parserAddresses = parserAddresses;
        p.selectors = selectors;
        p.selectorTypes = selectorTypes;

        emit PresetCreated(presetId, name);
    }

    /**
     * @notice Update an existing preset
     * @param presetId The preset ID to update
     */
    function updatePreset(
        uint256 presetId,
        string calldata name,
        uint16 roleId,
        uint256 maxSpendingBps,
        uint256 windowDuration,
        address[] calldata allowedProtocols,
        address[] calldata parserProtocols,
        address[] calldata parserAddresses,
        bytes4[] calldata selectors,
        uint8[] calldata selectorTypes
    ) external onlyOwner {
        if (!_presets[presetId].exists) revert PresetNotFound(presetId);
        if (parserProtocols.length != parserAddresses.length) revert ArrayLengthMismatch();
        if (selectors.length != selectorTypes.length) revert ArrayLengthMismatch();

        Preset storage p = _presets[presetId];
        p.name = name;
        p.roleId = roleId;
        p.maxSpendingBps = maxSpendingBps;
        p.windowDuration = windowDuration;
        p.allowedProtocols = allowedProtocols;
        p.parserProtocols = parserProtocols;
        p.parserAddresses = parserAddresses;
        p.selectors = selectors;
        p.selectorTypes = selectorTypes;

        emit PresetUpdated(presetId, name);
    }

    // ============ Deployment Functions ============

    /**
     * @notice Deploy a fully-configured agent vault with custom configuration
     * @param config The full vault configuration
     * @return module The deployed module address
     * @dev Flow:
     *   1. Deploy module with CREATE2 (factory as temporary owner)
     *   2. Configure roles, limits, allowlists, parsers, selectors, price feeds
     *   3. Transfer ownership to Safe
     *   4. Register in registry
     *   User must separately enable the module on their Safe.
     */
    function deployVault(VaultConfig calldata config) external onlyOwner returns (address module) {
        _validateConfig(config.safe, config.oracle, config.agentAddress);
        _validateArrays(
            config.parserProtocols.length,
            config.parserAddresses.length,
            config.selectors.length,
            config.selectorTypes.length,
            config.priceFeedTokens.length,
            config.priceFeedAddresses.length
        );

        // Check registry for existing module
        _checkNoExistingModule(config.safe);

        // 1. Deploy module with CREATE2
        module = _deployModule(config.safe, config.oracle);

        DeFiInteractorModule m = DeFiInteractorModule(module);

        // 2. Configure module
        _configureRole(m, config.agentAddress, config.roleId, config.maxSpendingBps, config.windowDuration);
        _configureAllowlist(m, config.agentAddress, config.allowedProtocols);
        _configureParsers(m, config.parserProtocols, config.parserAddresses);
        _configureSelectors(m, config.selectors, config.selectorTypes);
        _configurePriceFeeds(m, config.priceFeedTokens, config.priceFeedAddresses);

        // 3. Transfer ownership to Safe
        m.transferOwnership(config.safe);

        // 4. Register in registry
        _registerModule(module, config.safe, config.oracle);

        emit AgentVaultCreated(config.safe, config.agentAddress, module, type(uint256).max);
    }

    /**
     * @notice Deploy a vault using a preset template
     * @param safe The Safe address
     * @param oracle The Chainlink CRE oracle address
     * @param agentAddress The AI agent's EOA
     * @param presetId The preset template ID
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
    ) external onlyOwner returns (address module) {
        Preset storage p = _presets[presetId];
        if (!p.exists) revert PresetNotFound(presetId);

        _validateConfig(safe, oracle, agentAddress);
        if (priceFeedTokens.length != priceFeedAddresses.length) revert ArrayLengthMismatch();

        // Check registry for existing module
        _checkNoExistingModule(safe);

        // 1. Deploy module with CREATE2
        module = _deployModule(safe, oracle);

        DeFiInteractorModule m = DeFiInteractorModule(module);

        // 2. Configure from preset
        _configureRole(m, agentAddress, p.roleId, p.maxSpendingBps, p.windowDuration);
        _configureAllowlist(m, agentAddress, p.allowedProtocols);
        _configureParsers(m, p.parserProtocols, p.parserAddresses);
        _configureSelectors(m, p.selectors, p.selectorTypes);
        _configurePriceFeeds(m, priceFeedTokens, priceFeedAddresses);

        // 3. Transfer ownership to Safe
        m.transferOwnership(safe);

        // 4. Register in registry
        _registerModule(module, safe, oracle);

        emit AgentVaultCreated(safe, agentAddress, module, presetId);
    }

    // ============ Internal: Deployment ============

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

    function _configureRole(
        DeFiInteractorModule m,
        address agentAddress,
        uint16 roleId,
        uint256 maxSpendingBps,
        uint256 windowDuration
    ) internal {
        m.grantRole(agentAddress, roleId);
        m.setSubAccountLimits(agentAddress, maxSpendingBps, windowDuration);
    }

    function _configureAllowlist(DeFiInteractorModule m, address agentAddress, address[] memory protocols) internal {
        if (protocols.length > 0) {
            m.setAllowedAddresses(agentAddress, protocols, true);
        }
    }

    function _configureParsers(DeFiInteractorModule m, address[] memory protocols, address[] memory parsers) internal {
        for (uint256 i = 0; i < protocols.length; i++) {
            m.registerParser(protocols[i], parsers[i]);
        }
    }

    function _configureSelectors(DeFiInteractorModule m, bytes4[] memory selectors, uint8[] memory selectorTypes)
        internal
    {
        for (uint256 i = 0; i < selectors.length; i++) {
            m.registerSelector(selectors[i], DeFiInteractorModule.OperationType(selectorTypes[i]));
        }
    }

    function _configurePriceFeeds(DeFiInteractorModule m, address[] calldata tokens, address[] calldata feeds)
        internal
    {
        if (tokens.length > 0) {
            m.setTokenPriceFeeds(tokens, feeds);
        }
    }

    // ============ Internal: Validation ============

    function _validateConfig(address safe, address oracle, address agentAddress) internal pure {
        if (safe == address(0) || oracle == address(0) || agentAddress == address(0)) {
            revert InvalidAddress();
        }
        if (oracle == safe) revert InvalidConfig();
    }

    function _validateArrays(
        uint256 parserProtocolsLen,
        uint256 parserAddressesLen,
        uint256 selectorsLen,
        uint256 selectorTypesLen,
        uint256 priceFeedTokensLen,
        uint256 priceFeedAddressesLen
    ) internal pure {
        if (parserProtocolsLen != parserAddressesLen) revert ArrayLengthMismatch();
        if (selectorsLen != selectorTypesLen) revert ArrayLengthMismatch();
        if (priceFeedTokensLen != priceFeedAddressesLen) revert ArrayLengthMismatch();
    }

    function _checkNoExistingModule(address safe) internal view {
        if (address(registry) != address(0)) {
            address existing = registry.getModuleForSafe(safe);
            if (existing != address(0)) {
                revert SafeAlreadyHasModule(safe, existing);
            }
        }
    }

    function _registerModule(address module, address safe, address oracle) internal {
        if (address(registry) != address(0)) {
            registry.registerModuleFromFactory(module, safe, oracle);
        }
    }

    // ============ View Functions ============

    /**
     * @notice Get a preset by ID
     * @param presetId The preset ID
     * @return name The preset name
     * @return roleId The role to grant
     * @return maxSpendingBps Spending limit in basis points
     * @return windowDuration Time window in seconds
     */
    function getPreset(uint256 presetId)
        external
        view
        returns (string memory name, uint16 roleId, uint256 maxSpendingBps, uint256 windowDuration)
    {
        Preset storage p = _presets[presetId];
        if (!p.exists) revert PresetNotFound(presetId);
        return (p.name, p.roleId, p.maxSpendingBps, p.windowDuration);
    }

    /**
     * @notice Get the allowed protocols for a preset
     * @param presetId The preset ID
     * @return protocols Array of allowed protocol addresses
     */
    function getPresetProtocols(uint256 presetId) external view returns (address[] memory protocols) {
        if (!_presets[presetId].exists) revert PresetNotFound(presetId);
        return _presets[presetId].allowedProtocols;
    }

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
