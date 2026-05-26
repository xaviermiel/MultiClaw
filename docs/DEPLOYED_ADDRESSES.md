# Deployed Addresses — Base Mainnet (chainId 8453)

Option B (oracleless).

## Core

| Contract                    | Address                                      | Verified |
| --------------------------- | -------------------------------------------- | -------- |
| ModuleRegistry              | `0x9889390D3ac31f6Ec97833D18Df4C4E689157251` | ✅       |
| DeFiInteractorModule (impl) | `0x61F2f0cAf2cBc2a3520D11B92473C61C935EcCde` | ✅       |
| PresetRegistry              | `0x49991a970BEBb0FBE7b472636dF2302c83B3bdD6` | ✅       |
| AgentVaultFactory           | `0x389623997Bc006dA3BdBbE18d7Be04dACF4f09Ff` | ✅       |

- AgentVaultFactory **deploy block: 46515237** (subgraph `startBlock`)
- Owner of all owned contracts: the deployer EOA above.
- ModuleFactory: **not deployed** (unused by Option B).

## Parsers

| Parser                | Address                                      |
| --------------------- | -------------------------------------------- |
| AaveV3Parser          | `0x297b1f0E44e318a94bd7C0f06fA7e9d1Dd594d87` |
| UniswapV3Parser       | `0x1F0A52c5b462312a4bb76cD5fD33f981782e652D` |
| UniversalRouterParser | `0x07aae0A675bC910bE9A8ABa4a235Aef548587023` |
| MorphoBlueParser      | `0xeC2519fB177A6160E40fc9F597FB32E91C8Ce6CB` |
| UniswapV4Parser       | `0xE95809f6eD491A4Db1E03001e8D129dEecA5A093` |
| MorphoParser          | `0x0735d88d8dB42e47b8D1b0692D3c017337bE820A` |
| MerklParser           | `0x7614eed98d68B08DE2e346C2f68A8c8E5EEe5b33` |
| OneInchParser         | `0xD192a7676E668cdEbA9736B778c23c24C1232E7b` |
| ParaswapParser        | `0x12726F4C2b6ea61959fAB06C00335859f6F59d4f` |
| KyberSwapParser       | `0x0916427168DdE3fD1c0F7Db714E6D5E14903931E` |

All 10 verified on Basescan.

## Presets (oracleless USD caps, 24h window)

| ID  | Name          | maxSpendingUSD | roleId       |
| --- | ------------- | -------------- | ------------ |
| 0   | DeFi Trader   | $1,000         | 1 (EXECUTE)  |
| 1   | Yield Farmer  | $2,500         | 1 (EXECUTE)  |
| 2   | Payment Agent | $500           | 2 (TRANSFER) |
