---
sidebar_position: 2
title: AgentVaultFactory
---

# AgentVaultFactory

Permissionless factory for deploying fully configured Agent Vaults in a single transaction. Uses ERC-1167 minimal proxy clones for gas-efficient deployment.

**Source:** [`src/AgentVaultFactory.sol`](https://github.com/xaviermiel/MultiClaw/blob/main/src/AgentVaultFactory.sol)

## How it works

1. Clones the `DeFiInteractorModule` implementation via `Clones.cloneDeterministic()`
2. Calls `initialize()` on the clone with `owner = factory` (temporary)
3. Configures the module: roles, spending limits, allowlists, parsers, selectors, price feeds
4. Transfers ownership to the Safe
5. Registers the module in the `ModuleRegistry`

After deployment, the Safe owner must enable the module on their Safe (one multisig transaction).

## Deployment functions

### `deployVault(VaultConfig calldata config)`

Deploy a vault with a custom configuration.

```solidity
struct VaultConfig {
    address safe;
    address oracle;
    address agentAddress;
    uint16 roleId;
    uint256 maxSpendingBps;       // 0 for USD mode
    uint256 maxSpendingUSD;       // 0 for BPS mode
    uint256 windowDuration;       // seconds
    address[] allowedProtocols;
    address[] parserProtocols;
    address[] parserAddresses;
    bytes4[] selectors;
    uint8[] selectorTypes;        // OperationType values
    address[] priceFeedTokens;
    address[] priceFeedAddresses;
}
```

**Returns:** `address module` — the deployed module address.

**Emits:** `AgentVaultCreated(safe, agentAddress, module, 0)`

### `deployVaultFromPreset(...)`

Deploy a vault from a template preset stored in the `PresetRegistry`.

```solidity
function deployVaultFromPreset(
    address safe,
    address oracle,
    address agent,
    uint256 presetId,
    address[] calldata priceFeedTokens,
    address[] calldata priceFeedAddresses
) external returns (address module)
```

Price feeds are passed per-deployment because they are chain-specific (not stored in presets).

**Emits:** `AgentVaultCreated(safe, agentAddress, module, presetId)`

## Address prediction

### `computeModuleAddress(address safe)`

Predict the address a module will be deployed to for a given Safe, before deploying.

```solidity
function computeModuleAddress(address safe) external view returns (address predicted)
```

Uses `Clones.predictDeterministicAddress()` with a salt derived from `keccak256(abi.encodePacked(safe, vaultNonce[safe]))`.

## Owner functions

The factory owner (MultiClaw team) can update infrastructure addresses:

```solidity
function setImplementation(address _implementation) external onlyOwner
function setRegistry(IModuleRegistry _registry) external onlyOwner
function setPresetRegistry(PresetRegistry _presetRegistry) external onlyOwner
```

## State

```solidity
address public implementation;           // Module implementation to clone
IModuleRegistry public registry;         // Module registry
PresetRegistry public presetRegistry;    // Preset template registry
mapping(address => uint256) public vaultNonce;           // Per-Safe deployment counter
mapping(address => address[]) internal _deployedModules; // Safe → modules mapping
```

## Events

```solidity
event AgentVaultCreated(address indexed safe, address indexed agentAddress, address module, uint256 presetId)
event RegistryUpdated(address indexed oldRegistry, address indexed newRegistry)
event PresetRegistryUpdated(address indexed oldPresetRegistry, address indexed newPresetRegistry)
event ImplementationUpdated(address indexed oldImplementation, address indexed newImplementation)
```

## Errors

```solidity
error InvalidAddress()
error InvalidConfig()
error PresetRegistryNotSet()
error ImplementationNotSet()
error ArrayLengthMismatch()
error SafeAlreadyHasModule(address safe, address existingModule)
```
