---
sidebar_position: 5
title: Protocol Parsers
---

# Protocol Parsers

Parsers decode calldata from DeFi protocol function calls to extract tokens, amounts, and recipients. The module uses this extracted data to enforce spending limits and recipient validation.

## Interface

All parsers implement `ICalldataParser`:

```solidity
interface ICalldataParser {
    function parseCalldata(bytes calldata data)
        external
        view
        returns (
            address[] memory tokensIn,
            uint256[] memory amountsIn,
            address[] memory tokensOut,
            uint256[] memory amountsOut,
            address recipient
        );
}
```

## Supported protocols

| Parser                  | Protocol                 | Operations                                                   | Notes                  |
| ----------------------- | ------------------------ | ------------------------------------------------------------ | ---------------------- |
| `AaveV3Parser`          | Aave V3 Pool             | supply, withdraw, repay                                      | Handles aToken mapping |
| `MorphoBlueParser`      | Morpho Blue              | supply, withdraw, borrow, repay                              | Isolated markets       |
| `MorphoParser`          | Morpho MetaMorpho        | deposit, withdraw                                            | Vault shares           |
| `UniswapV3Parser`       | Uniswap V3 Router        | exactInputSingle, exactOutputSingle, exactInput, exactOutput | Path decoding          |
| `UniswapV4Parser`       | Uniswap V4               | swap, modifyLiquidity                                        | Pool key extraction    |
| `UniversalRouterParser` | Uniswap Universal Router | execute (V2/V3 swaps)                                        | Command decoding       |
| `OneInchParser`         | 1inch Aggregator         | swap, unoswap                                                | Aggregated routes      |
| `KyberSwapParser`       | KyberSwap                | swap                                                         | Aggregated routes      |
| `ParaswapParser`        | Paraswap                 | multiSwap, megaSwap, simpleSwap                              | Aggregated routes      |
| `MerklParser`           | Merkl                    | claim                                                        | Reward claiming        |

## How parsers are used

1. Agent calls `executeOnProtocol(target, data)`
2. Module looks up the parser for `target` via `protocolParser[target]`
3. If a parser exists, module calls `parser.parseCalldata(data)`
4. Parser returns `tokensIn`, `amountsIn`, `tokensOut`, `amountsOut`, `recipient`
5. Module validates `recipient == safeAddress`
6. Module computes spending cost from `tokensIn` / `amountsIn` using price feeds

If no parser is registered for the target, the module falls back to selector-only classification (no calldata extraction).

## Parser registration

The Safe owner registers parsers on the module:

```solidity
module.registerParser(
    0xAAVE_V3_POOL,    // protocol address
    0xAAVE_PARSER      // parser contract address
);
```

Parsers are **stateless** — they only read calldata, have no storage, and hold no funds. A single parser deployment can be shared across multiple modules.

## Security properties

- Parsers are isolated: they cannot modify module state or access funds
- Recipient extraction prevents output redirection attacks
- Each parser validates function signatures and reverts on unknown selectors
- Complex encodings (Universal Router commands, Uniswap V3 paths) are fully decoded
