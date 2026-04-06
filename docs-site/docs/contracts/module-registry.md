---
sidebar_position: 4
title: ModuleRegistry
---

# ModuleRegistry

Central registry tracking all deployed `DeFiInteractorModule` instances. Used by the oracle to discover which modules to monitor.

**Source:** [`src/ModuleRegistry.sol`](https://github.com/xaviermiel/MultiClaw/blob/main/src/ModuleRegistry.sol)

## Purpose

When a module is deployed via `AgentVaultFactory` or `ModuleFactory`, it is automatically registered here. The oracle queries the registry to find all active modules and their associated Safes and oracle addresses.

## Key functions

### Registration (factory only)

```solidity
function registerModule(address module, address safe, address oracle) external
```

Only authorized factories can call this.

### Factory authorization (owner only)

```solidity
function authorizeFactory(address factory) external onlyOwner
function revokeFactory(address factory) external onlyOwner
```

### Module lifecycle (owner only)

```solidity
function deactivateModule(address module) external onlyOwner  // soft delete
function reactivateModule(address module) external onlyOwner  // undo soft delete
function removeModule(address module) external onlyOwner      // hard delete
```

### Discovery (public)

```solidity
function getActiveModules() external view returns (ModuleInfo[] memory)
function getModuleBySafe(address safe) external view returns (address)
function isModuleActive(address module) external view returns (bool)
```

## ModuleInfo struct

```solidity
struct ModuleInfo {
    address module;
    address safe;
    address oracle;
    bool active;
    uint256 registeredAt;
}
```
