// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

/**
 * @title PresetRegistry
 * @notice Stores template presets for AgentVaultFactory deployments
 * @dev Separated from the factory to keep both contracts under the 24KB EVM size limit.
 *      Presets are on-chain configurations that define common vault setups (DeFi Trader,
 *      Yield Farmer, Payment Agent, etc.) so users don't need to specify every parameter.
 */
contract PresetRegistry is Ownable {
    // ============ Structs ============

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

    // ============ State ============

    mapping(uint256 => Preset) private _presets;
    uint256 public presetCount;

    // ============ Events ============

    event PresetCreated(uint256 indexed presetId, string name);
    event PresetUpdated(uint256 indexed presetId, string name);

    // ============ Errors ============

    error PresetNotFound(uint256 presetId);
    error ArrayLengthMismatch();

    // ============ Constructor ============

    constructor(address _initialOwner) Ownable(_initialOwner) {}

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

    // ============ View Functions ============

    function presetExists(uint256 presetId) external view returns (bool) {
        return _presets[presetId].exists;
    }

    function getPreset(uint256 presetId)
        external
        view
        returns (string memory name, uint16 roleId, uint256 maxSpendingBps, uint256 windowDuration)
    {
        Preset storage p = _presets[presetId];
        if (!p.exists) revert PresetNotFound(presetId);
        return (p.name, p.roleId, p.maxSpendingBps, p.windowDuration);
    }

    function getPresetProtocols(uint256 presetId) external view returns (address[] memory) {
        if (!_presets[presetId].exists) revert PresetNotFound(presetId);
        return _presets[presetId].allowedProtocols;
    }

    function getPresetFull(uint256 presetId)
        external
        view
        returns (
            uint16 roleId,
            uint256 maxSpendingBps,
            uint256 windowDuration,
            address[] memory allowedProtocols,
            address[] memory parserProtocols,
            address[] memory parserAddresses,
            bytes4[] memory selectors,
            uint8[] memory selectorTypes
        )
    {
        Preset storage p = _presets[presetId];
        if (!p.exists) revert PresetNotFound(presetId);
        return (
            p.roleId,
            p.maxSpendingBps,
            p.windowDuration,
            p.allowedProtocols,
            p.parserProtocols,
            p.parserAddresses,
            p.selectors,
            p.selectorTypes
        );
    }
}
