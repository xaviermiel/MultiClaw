---
sidebar_position: 1
title: DeFiInteractorModule
---

# DeFiInteractorModule

The core smart contract. A custom Zodiac module that enforces 12 layers of security on every AI agent transaction.

**Source:** [`src/DeFiInteractorModule.sol`](https://github.com/xaviermiel/MultiClaw/blob/main/src/DeFiInteractorModule.sol)

## Overview

- Solidity 0.8.24, Cancun EVM
- Deployed as ERC-1167 minimal proxy clones via `AgentVaultFactory`
- Owned by the Safe after factory configuration
- Supports direct deployment (constructor) and proxy deployment (`initialize()`)

## Agent functions

### `executeOnProtocol(address target, bytes calldata data)`

Execute a DeFi operation through the Safe. The agent must have `DEFI_EXECUTE_ROLE` (1).

**Checks enforced:**

1. Caller has `DEFI_EXECUTE_ROLE`
2. Oracle data is fresh (< 60 min)
3. Function selector is registered
4. Target is in caller's allowlist
5. Parser extracts tokens/amounts/recipient
6. Recipient is the Safe address
7. Spending fits within allowance
8. Cumulative cap not exceeded
9. Acquired balance deducted first

**Emits:** `ProtocolExecution(subAccount, target, opType, tokensIn, amountsIn, tokensOut, amountsOut, spendingCost)`

### `executeOnProtocolWithValue(address target, bytes calldata data, uint256 value)`

Same as `executeOnProtocol` but sends ETH value with the call.

### `transferToken(address token, address recipient, uint256 amount)`

Transfer tokens from the Safe. Requires `DEFI_TRANSFER_ROLE` (2).

Pass `token = address(0)` to send native ETH directly from the Safe; `amount` is then interpreted as wei. For ERC-20s, pass the token contract address and the amount in the token's smallest unit. Spending cost, acquired balance, and recipient-whitelist checks apply uniformly to both modes.

**Emits:** `TransferExecuted(subAccount, token, recipient, amount, spendingCost)`

## Owner functions

All owner functions are restricted to the module owner (the Safe).

### Role management

```solidity
function grantRole(address member, uint16 roleId) external onlyOwner
function revokeRole(address member, uint16 roleId) external onlyOwner
```

### Spending limits

```solidity
function setSubAccountLimits(
    address subAccount,
    uint256 maxSpendingBps,   // 0 for USD mode
    uint256 maxSpendingUSD,   // 0 for BPS mode
    uint256 windowDuration    // seconds
) external onlyOwner
```

Exactly one of `maxSpendingBps` or `maxSpendingUSD` must be non-zero.

### Recipient whitelist

```solidity
function setRecipientWhitelistEnabled(address subAccount, bool enabled) external onlyOwner
function setAllowedRecipients(address subAccount, address[] calldata recipients, bool allowed) external onlyOwner
```

When enabled for a sub-account, `transferToken` will only allow transfers to explicitly whitelisted recipients — even if the whitelist is empty. When disabled (default), any non-zero recipient is accepted.

The Safe and the module itself cannot be whitelisted as recipients.

**Emits:** `RecipientWhitelistToggled(subAccount, enabled)`, `AllowedRecipientsSet(subAccount, recipients, allowed)`

### Protocol allowlists

```solidity
function setAllowedAddresses(
    address subAccount,
    address[] calldata targets,
    bool allowed
) external onlyOwner
```

### Selector registry

```solidity
function registerSelector(bytes4 selector, OperationType opType) external onlyOwner
function unregisterSelector(bytes4 selector) external onlyOwner
```

Operation types: `UNKNOWN(0)`, `SWAP(1)`, `DEPOSIT(2)`, `WITHDRAW(3)`, `CLAIM(4)`, `APPROVE(5)`, `REPAY(6)`

### Parser registration

```solidity
function registerParser(address protocol, address parser) external onlyOwner
```

### Price feeds

```solidity
function setTokenPriceFeed(address token, address priceFeed) external onlyOwner
function setTokenPriceFeeds(address[] calldata tokens, address[] calldata priceFeeds) external onlyOwner
```

### Safety caps

```solidity
function setMaxOracleAcquiredBps(uint256 newMaxBps) external onlyOwner
```

### Emergency controls

```solidity
function pause() external onlyOwner
function unpause() external onlyOwner
function setAuthorizedOracle(address newOracle) external onlyOwner
```

## Oracle functions

Restricted to the `authorizedOracle` address.

```solidity
function updateSafeValue(uint256 totalValueUSD) external
function updateSpendingAllowance(address subAccount, uint256 expectedVersion, uint256 newAllowance) external
function updateAcquiredBalance(address subAccount, address token, uint256 expectedVersion, uint256 newBalance) external
function batchUpdate(...) external
```

Version counters prevent stale overwrites. If `expectedVersion` doesn't match the current version, the update is skipped (not reverted).

## Read functions

```solidity
function getSafeValue() external view returns (uint256 totalValueUSD, uint256 lastUpdated, uint256 updateCount)
function getSpendingAllowance(address subAccount) external view returns (uint256)
function getAcquiredBalance(address subAccount, address token) external view returns (uint256)
function getSubAccountLimits(address subAccount) external view returns (uint256 maxBps, uint256 maxUSD, uint256 windowDuration)
function getSubaccountsByRole(uint16 roleId) external view returns (address[] memory)
function getTokenBalances(address[] calldata tokens) external view returns (uint256[] memory)
function hasRole(address member, uint16 roleId) external view returns (bool)
```

## Key storage

| Variable                    | Type                                              | Description                                                |
| --------------------------- | ------------------------------------------------- | ---------------------------------------------------------- |
| `spendingAllowance`         | `mapping(address => uint256)`                     | Oracle-managed remaining budget                            |
| `acquiredBalance`           | `mapping(address => mapping(address => uint256))` | Free-to-use token balances                                 |
| `cumulativeSpent`           | `mapping(address => uint256)`                     | On-chain spending counter (oracle cannot reset)            |
| `windowStart`               | `mapping(address => uint256)`                     | Current window start timestamp                             |
| `windowSafeValue`           | `mapping(address => uint256)`                     | Safe value snapshot at window start                        |
| `maxOracleAcquiredBps`      | `uint256`                                         | Oracle acquired budget cap (default: 2000 = 20%)           |
| `recipientWhitelistEnabled` | `mapping(address => bool)`                        | Whether recipient whitelisting is enforced per sub-account |
| `allowedRecipients`         | `mapping(address => mapping(address => bool))`    | Whitelisted transfer recipients per sub-account            |

## Events

```solidity
event ProtocolExecution(address indexed subAccount, address indexed target, uint8 opType,
    address[] tokensIn, uint256[] amountsIn, address[] tokensOut, uint256[] amountsOut, uint256 spendingCost)
event TransferExecuted(address indexed subAccount, address indexed token,
    address indexed recipient, uint256 amount, uint256 spendingCost)
event RoleAssigned(address indexed member, uint16 indexed roleId)
event RoleRevoked(address indexed member, uint16 indexed roleId)
event SubAccountLimitsSet(address indexed subAccount, uint256 maxSpendingBps, uint256 maxSpendingUSD, uint256 windowDuration)
event AllowedAddressesSet(address indexed subAccount, address[] targets, bool allowed)
event RecipientWhitelistToggled(address indexed subAccount, bool enabled)
event AllowedRecipientsSet(address indexed subAccount, address[] recipients, bool allowed)
event SafeValueUpdated(uint256 totalValueUSD, uint256 updateCount)
event CumulativeSpendingReset(address indexed subAccount, uint256 windowSafeValue)
event EmergencyPaused()
event EmergencyUnpaused()
```

## Errors

```solidity
error NotAuthorized()
error InvalidOracleAddress()
error InvalidAddress()
error OracleDataStale()
error UnknownSelector(bytes4 selector)
error TargetNotAllowed(address target)
error RecipientNotSafe(address recipient)
error ExceedsSpendingAllowance(uint256 cost, uint256 allowance)
error ExceedsCumulativeSpendingLimit(uint256 cumulative, uint256 maximum)
error ExceedsOracleAcquiredBudget(uint256 cumulative, uint256 maximum)
error NeitherLimitModeSet()
error AlreadyInitialized()
error CannotWhitelistCoreAddress(address addr)
error RecipientNotWhitelisted(address recipient)
```
