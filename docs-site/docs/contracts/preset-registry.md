---
sidebar_position: 3
title: PresetRegistry
---

# PresetRegistry

On-chain storage for vault template configurations. Presets define a reusable set of roles, spending limits, protocols, parsers, and selectors that can be applied when deploying a vault.

**Source:** [`src/PresetRegistry.sol`](https://github.com/xaviermiel/MultiClaw/blob/main/src/PresetRegistry.sol)

## Default presets

| ID  | Name          | Role                   | Budget             | Protocols                             |
| --- | ------------- | ---------------------- | ------------------ | ------------------------------------- |
| 0   | DeFi Trader   | DEFI_EXECUTE_ROLE (1)  | 500 BPS (5%/day)   | Uniswap V3, Universal Router, Aave V3 |
| 1   | Yield Farmer  | DEFI_EXECUTE_ROLE (1)  | 1000 BPS (10%/day) | Aave V3                               |
| 2   | Payment Agent | DEFI_TRANSFER_ROLE (2) | 100 BPS (1%/day)   | None (transfer only)                  |

## Preset struct

```solidity
struct Preset {
    string name;
    bool exists;
    uint16 roleId;
    uint256 maxSpendingBps;
    uint256 maxSpendingUSD;
    uint256 windowDuration;
    address[] allowedProtocols;
    address[] parserProtocols;
    address[] parserAddresses;
    bytes4[] selectors;
    uint8[] selectorTypes;
    bool recipientWhitelistEnabled;
}
```

Presets do **not** store price feed addresses or specific recipient addresses because those are user/chain-specific. Price feeds and recipient lists are passed per-deployment. The `recipientWhitelistEnabled` flag controls whether transfers require whitelisted recipients (used by Payment Agent presets).

## Functions

### `createPreset(...)`

Create a new preset. Owner only.

```solidity
function createPreset(
    string calldata name,
    uint16 roleId,
    uint256 maxSpendingBps,
    uint256 maxSpendingUSD,
    uint256 windowDuration,
    address[] calldata allowedProtocols,
    address[] calldata parserProtocols,
    address[] calldata parserAddresses,
    bytes4[] calldata selectors,
    uint8[] calldata selectorTypes
) external onlyOwner returns (uint256 presetId)
```

### `updatePreset(uint256 presetId, ...)`

Update an existing preset. Owner only. Same parameters as `createPreset`.

### `getPreset(uint256 presetId)`

Read a preset configuration.

```solidity
function getPreset(uint256 presetId) external view returns (Preset memory)
```
