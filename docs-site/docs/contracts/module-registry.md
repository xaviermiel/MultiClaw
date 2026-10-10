---
sidebar_position: 4
title: ModuleRegistry
---

# ModuleRegistry

Central registry tracking all deployed `DeFiInteractorModule` instances. Used by the oracle to discover which modules to monitor.

**Source:** [`src/ModuleRegistry.sol`](https://github.com/xaviermiel/MultiClaw/blob/main/src/ModuleRegistry.sol)

## Purpose

When a module is deployed via `AgentVaultFactory` or `ModuleFactory`, it is automatically registered here, with the account that called the factory as its deployer. The oracle queries the registry for the active modules it serves under its [access policy](#oracle-access-policies).

## Key functions

### Registration

```solidity
// Authorized factories only; deployer is the account that called the factory
function registerModuleFromFactory(address module, address safe, address oracle, address deployer) external
// Owner only; the module has no recorded deployer
function registerModule(address module, address safe, address oracle) external onlyOwner
```

### Factory authorization (owner only)

```solidity
function authorizeFactory(address factory) external onlyOwner
function deauthorizeFactory(address factory) external onlyOwner
```

### Module lifecycle (owner only)

```solidity
function deactivateModule(address module) external onlyOwner  // soft delete
function reactivateModule(address module) external onlyOwner  // undo soft delete
function removeModule(address module) external onlyOwner      // hard delete
```

### Discovery (public)

```solidity
function getActiveModules() external view returns (address[] memory)
function getActiveModulesPaginated(uint256 offset, uint256 limit) external view returns (address[] memory modules, uint256 total)
function getModulesServedBy(address oracle) external view returns (address[] memory)
function getModuleForSafe(address safe) external view returns (address)
function moduleInfo(address module) external view returns (ModuleInfo memory)
function moduleDeployer(address module) external view returns (address)
function isRegistered(address module) external view returns (bool)
```

## ModuleInfo struct

```solidity
struct ModuleInfo {
    address safeAddress;
    address authorizedOracle; // the oracle at registration
    uint256 deployedAt;
    bool isActive;
}
```

## Oracle access policies

Deployment is permissionless, and the deployer chooses the oracle: anyone can deploy a module naming any oracle, which then spends gas and cycle time on it. Each oracle therefore chooses which modules it serves:

- **Open** (the default): every active module.
- **Restricted**: only the modules it approved, and those deployed by deployers it trusts, including their future vaults.

The oracle sets its policy itself, or through a manager it designates, so that its hot key does no admin work:

```solidity
function setOracleManager(address manager) external                                                  // called by the oracle
function setOracleRestricted(address oracle, bool restricted) external                               // oracle or its manager
function setTrustedDeployers(address oracle, address[] calldata deployers, bool trusted) external    // oracle or its manager
function setApprovedModules(address oracle, address[] calldata modules, bool approved) external      // oracle or its manager
```

```solidity
function isServedBy(address module, address oracle) external view returns (bool)
function isTrustedDeployer(address oracle, address deployer) external view returns (bool)
function isApprovedModule(address oracle, address module) external view returns (bool)
function oracleRestricted(address oracle) external view returns (bool)
```

For example, an operator serving only the vaults it deploys:

```bash
# Once, signed by the oracle key: let a cold wallet manage the policy
cast send $REGISTRY "setOracleManager(address)" $MANAGER --account oracle
# Signed by the manager
cast send $REGISTRY "setTrustedDeployers(address,address[],bool)" $ORACLE "[$DEPLOYER]" true --account manager
cast send $REGISTRY "setOracleRestricted(address,bool)" $ORACLE true --account manager
```

The policy tells oracles which modules to serve; modules don't enforce it. A module whose oracle doesn't serve it gets no oracle updates, so in BPS mode its operations revert once its oracle data is stale. Before deploying a vault with someone else's oracle, check that `oracleRestricted(oracle)` is false or `isTrustedDeployer(oracle, you)` is true; otherwise ask its operator to approve your module.

Modules registered by the owner (`registerModule`) have no deployer: a restricted oracle serves them only if it approves them. A module can be approved before it is deployed, at its predicted address.
