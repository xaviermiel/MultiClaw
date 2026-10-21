# MultiClaw Oracle

Off-chain oracle infrastructure for the MultiClaw DeFi module. Replaces the previous Chainlink CRE (Computation Runtime Environment) implementation with a self-hosted service.

The oracle is written in Go (`cmd/oracle`, `internal/`). It replaced a TypeScript implementation, removed after the port: it reads the same `.env`, and [Changes from the TS oracle](#changes-from-the-ts-oracle) lists what differs for operators migrating from it.

## Architecture

The oracle consists of two services, which `oracle run` runs in one process:

### 1. Spending Oracle (`internal/spendingoracle`, state machine in `internal/spending`)

Event-driven monitor that tracks spending and acquired balances for sub-accounts.

- **RPC polling** for `ProtocolExecution`, `TransferExecuted` and `SubAccountLimitsSet` events, behind a confirmation depth, with reorg detection
- **Rolling window** tracking for spending calculations (each sub-account's window is read on-chain)
- **Dual-mode allowance calculation**:
  - **BPS mode** (`maxSpendingBps > 0`): `maxAllowance = safeValue × maxSpendingBps / 10000`
  - **USD mode** (`maxSpendingUSD > 0`): `maxAllowance = maxSpendingUSD` (fixed dollar cap)
- **Deposit/withdrawal matching** for acquired balance status (FIFO queues)
- **Stale balance clearing**: acquired balances the events no longer support are set back to 0
- **Cron-based periodic refresh** to update allowances as spending expires
- **Multi-module support** via ModuleRegistry discovery

### 2. Safe Value Oracle (`internal/safevalue`)

Periodic portfolio valuation service that calculates and stores the total USD value of the Safe.

- **Token balance fetching** (ERC20 + native ETH)
- **Chainlink price feed** integration for USD conversion
- **DeFi position support**: Aave aTokens (1:1), Morpho vaults (share-to-asset conversion), Uniswap V2 LP (reserve-based)
- **Threshold-based updates** — only writes to chain if value changed >1% or data is stale
- **Multi-module support** via ModuleRegistry

## Trust Model

The oracle wallet has constrained on-chain power, bounded by cumulative counters it cannot reset:

| Capability                             | On-chain constraint                                                                                        |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Set `spendingAllowance` per subaccount | Per-account cap (`maxSpendingBps * safeValue` or `maxSpendingUSD`) — advisory only, real cap is cumulative |
| Set `acquiredBalance` per token        | `_capToSafeBalance` + cumulative oracle acquired budget (`maxOracleAcquiredBps`, default 20%)              |
| Update `safeValue`                     | Snapshotted at window start; mid-window inflation has no effect on spending cap                            |

**If the oracle key is compromised**, maximum damage per window is capped to the per-account spending limit (`maxSpendingBps` or `maxSpendingUSD`) plus `maxOracleAcquiredBps` (default 20%) — enforced by on-chain cumulative counters. Swap acquired balances are marked trustlessly (Tier 1). Version counters prevent stale oracle writes from overwriting on-chain state changes. See [`ORACLE_SECURITY.md`](./ORACLE_SECURITY.md) for full analysis.

**If the oracle goes down**, all subaccount operations freeze within `maxOracleAge` (default 60 minutes) due to staleness checks.

## Running the Oracle

### Build

Requires Go 1.25 or later. `go.mod` pins the go1.27.3 toolchain, which Go downloads automatically (`GOTOOLCHAIN=auto`, the default): it blocks the compromised eMudhra intermediates, which Go's verifier would otherwise trust because it does not check revocation.

```bash
cd oracle
make build                         # bin/oracle, for this machine
make build-linux                   # bin/oracle-linux-amd64: static, for a server
make build-linux GOARCH_LINUX=arm64
```

`go version -m <binary>` shows the commit a binary was built from (`vcs.revision`).

### Configure

```bash
cp .env.example .env
```

[`.env.example`](./.env.example) documents every variable and its default. The oracle needs:

- `PRIVATE_KEY`: the key of the address set as the modules' `authorizedOracle`
- `REGISTRY_ADDRESS` (the ModuleRegistry, for the modules it lists) or `MODULE_ADDRESS` (a single module)
- `CHAIN` (`sepolia`, `base` or `base-sepolia`; default `sepolia`) and an `RPC_URL` for it

`bin/oracle config` prints the resolved configuration, with RPC URLs and the key redacted, and checks it.

### Choose which modules it serves

Anyone can deploy a module naming your oracle, and the oracle serves every module that names it. To serve only your own vaults, restrict the oracle in the registry. It then serves only the modules it approved and those deployed through a factory by deployers it trusts, including their future vaults:

```bash
# Once, signed by the oracle key: let a cold wallet manage the policy
cast send $REGISTRY_ADDRESS "setOracleManager(address)" $MANAGER --account oracle
# Signed by the manager (a keystore account here; --ledger works too)
cast send $REGISTRY_ADDRESS "setTrustedDeployers(address,address[],bool)" $ORACLE "[$YOUR_DEPLOYER]" true --account manager
cast send $REGISTRY_ADDRESS "setApprovedModules(address,address[],bool)" $ORACLE "[$SOME_MODULE]" true --account manager
cast send $REGISTRY_ADDRESS "setOracleRestricted(address,bool)" $ORACLE true --account manager
```

The oracle reads the modules it serves with `getModulesServedBy` at every run, so changes need no restart. A registry deployed before access policies has no policy: every active module is served.

### Commands

```bash
bin/oracle run          # both oracles
bin/oracle safe-value   # the Safe value oracle only
bin/oracle spending     # the spending oracle only
bin/oracle config       # print and check the configuration
bin/oracle analyze-tx   # explain why a transaction failed (below)
```

| Flag              | Effect                                                                               |
| ----------------- | ------------------------------------------------------------------------------------ |
| `--env-file PATH` | dotenv file to load (default `.env` in the working directory); set variables win     |
| `--once`          | one Safe value check and/or one spending refresh, then exit                          |
| `--dry-run`       | simulate every transaction instead of sending it (same as `DRY_RUN=true`, see below) |

The oracle logs to stdout (`[SpendingOracle 2026-10-09T16:58:00.012Z] ...`, the TS oracle's format), and stops cleanly on SIGINT or SIGTERM. It keeps no state on disk: after a restart it rebuilds everything from the chain, and its first refresh pushes an update for every subaccount.

### Dry run

With `--dry-run` or `DRY_RUN=true`, the oracle runs as usual but never signs: every `updateSafeValue` and `batchUpdate` is simulated with `eth_call` from the oracle address, with `GAS_LIMIT`, and logged, then treated as mined (as a reverted transaction when the simulation reverts):

```
[SpendingOracle ...] DRY RUN: not sending batchUpdate to 0xbB24...2c00 (292 bytes of calldata); at block 47909077: simulation ok
```

A dry run needs no key: set `ORACLE_ADDRESS` to the oracle's address instead of `PRIVATE_KEY`.

### Analyze a failed transaction

```bash
bin/oracle analyze-tx 0x<tx hash>                 # reads CHAIN and RPC_URL from .env
bin/oracle analyze-tx --no-tenderly 0x<tx hash>
```

It explains any failed transaction, and module calls in detail (subaccount operations, oracle updates, owner functions):

- **The call**: the module function with its arguments, and the protocol call it makes the Safe execute (ERC20, Aave V3, Uniswap V3 swaps), with token symbols and amounts. Calls of modules deployed before March 19, 2026 (the payable `executeOnProtocolWithValue(target, data)`, the 3-argument `setSubAccountLimits`) are decoded too.
- **The error**: replayed with `debug_traceTransaction`, else `eth_call` at the block before (which misses the effect of the transactions before it in its block), else Tenderly's public trace API (which receives the transaction hash; `--no-tenderly` turns it off). Module, parser and token errors are decoded by name, with what they mean and what to do; the module's and the parsers' explanations are only given for errors a module raised, since another contract's error of the same name can mean something else. For `TransactionFailed` (and `GS013`, a Safe transaction whose call failed), it also finds the error of the call that failed, in the trace or by replaying that call. A token error's amounts use the decimals of the token that raised it when the trace shows which one did.
- **The module state behind it**, at the block before the transaction (or now, when the node cannot read that block): allowance, last oracle update, roles or owner, whitelists, acquired balances, prices, window totals (for oracleless modules, what is left of the window's cap).

It reads only `CHAIN`, `RPC_URL` and `RPC_FALLBACK_URLS` (other variables, even malformed, are ignored); a node with the `debug` API gives the most precise answers. Interrupted, it prints no report and exits with status 1.

### Deploy with systemd

[`deploy/multiclaw-oracle.service`](./deploy/multiclaw-oracle.service) runs `multiclaw-oracle run` as a transient unprivileged user with no write access to the host, restarts it 10 s after it exits, and reads the configuration from `/etc/multiclaw-oracle/oracle.env`.

```bash
make build-linux
scp bin/oracle-linux-amd64 .env.example deploy/multiclaw-oracle.service server:

# On the server, as root:
install -m 0755 oracle-linux-amd64 /usr/local/bin/multiclaw-oracle
install -d -m 0700 /etc/multiclaw-oracle
install -m 0600 .env.example /etc/multiclaw-oracle/oracle.env    # then edit it
multiclaw-oracle config --env-file /etc/multiclaw-oracle/oracle.env
install -m 0644 multiclaw-oracle.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now multiclaw-oracle
journalctl -u multiclaw-oracle -f
```

systemd reads the env file itself (as root, so it can stay `0600 root`), with its own syntax: comments on their own lines only, and quotes around values that contain spaces, such as cron expressions. To upgrade, install the new binary and `systemctl restart multiclaw-oracle`.

### RPC requirements

- **`eth_getLogs` range.** Event queries span at most `MAX_BLOCKS_PER_QUERY` blocks (default 5000). The public `sepolia.base.org` only accepts 200: set `MAX_BLOCKS_PER_QUERY=200` there, or use a provider endpoint.
- **Query volume.** The first refresh after a start reads `MAX_HISTORICAL_BLOCKS` (30 days) of `AcquiredBalanceUpdated` events per subaccount, about 260 queries on Base. Later refreshes only read the new blocks (one query per subaccount), and the full read is repeated every hour.
- **Fallbacks.** `RPC_FALLBACK_URLS` endpoints are used in turn when one fails. At startup the oracle checks the endpoints' chain ID against `CHAIN`: one serving another chain is an error, one not answering is logged.

## Development

```bash
make check       # gofmt, go vet, go test
make test        # go test ./...
make fuzz        # fuzz the spending state machine (FUZZTIME=2m)
make abi         # re-extract the contract ABIs after a contract change (runs forge build)
```

| Path                              | Contents                                                                           |
| --------------------------------- | ---------------------------------------------------------------------------------- |
| `cmd/oracle`                      | the `oracle` command                                                               |
| `internal/config`                 | environment variables and chain presets (tokens, price feeds)                      |
| `internal/spending`               | the spending state machine: windows, FIFO acquired queues, deposit matching        |
| `internal/spendingoracle`         | the spending oracle service: polling, refreshes, reorgs, `batchUpdate`             |
| `internal/safevalue`              | Safe valuation and the Safe value oracle service                                   |
| `internal/contracts`              | embedded ABIs (extracted from the Foundry build), event and revert decoding        |
| `internal/ethx`                   | RPC client with fallbacks, chunked log queries, transaction sender, dry-run sender |
| `internal/pricing`                | Chainlink prices                                                                   |
| `internal/discovery`              | module discovery and the `authorizedOracle` check                                  |
| `internal/analyze`                | the `analyze-tx` command: call decoding, replays, error help, module state         |
| `internal/bn`, `logx`, `schedule` | big-number helpers, logging, cron loops                                            |
| `deploy/`                         | systemd unit                                                                       |

### Test data from the TS oracle

Three test data sets were generated from the TS oracle before its removal (their generators are in the git history, under `scripts/`):

- `internal/spending/testdata/golden`: 481 scenarios run through the TS `buildSubAccountState`, with the state it produced, which the Go state machine must produce too. A deliberate change to the accounting changes some of them: update their `want` by hand.
- `internal/config/testdata/ts-config.json`: the TS oracle's configuration per chain, which the chain presets must still match: a preset change updates it too.
- `internal/config/testdata/dotenv-cases.json`: dotenv 16's parse of tricky `.env` files, which the Go `.env` parser must match.

The whole oracles were also compared on anvil forks of Base Sepolia: run once from the same state, the TS and the Go oracle sent byte-identical transactions.

## Changes from the TS Oracle

For operators of the TS oracle (`npm start`): the Go oracle reads the same `.env`, and writes the same values on-chain, except where the TS failed to read something (LINK and AAVE on Base) or computed a power of ten inexactly (tokens or feeds with more than 22 decimals; no preset has any). It differs in how it operates:

- **Base mainnet LINK/USD and AAVE/USD feeds.** The TS had both addresses with an invalid checksum, which viem rejects, so it valued LINK and AAVE at $0. The Go oracle prices them.
- **One transaction sender** for both services: consecutive nonces, resynced when the node rejects one, and from the latest block when a transaction isn't mined within 2 minutes, so that a transaction the node dropped doesn't hold back the ones after it. The TS read the nonce before every send, and its services could collide.
- **EIP-1559 fees** capped at 2 × base fee + tip (viem: 1.2 × base fee + tip). The tip is `eth_maxPriorityFeePerGas`, or as in viem the gas price minus the base fee when the node lacks it. The gas limit is still `GAS_LIMIT`.
- **RPC fallback** moves to the next endpoint on every failed attempt. Reverts don't count as endpoint failures. The Safe value oracle uses the fallbacks too.
- **Event queries** are all split by `MAX_BLOCKS_PER_QUERY`, ranges stop at block 0 on young chains, and the head block is never skipped. A failed history chunk is retried at the next refresh instead of being skipped.
- **Incremental history**: `AcquiredBalanceUpdated` events are read once, not for 30 days on every refresh (see RPC requirements).
- **Reorgs** deeper than one block are rewound in one poll.
- **Decimals**: powers of 10 are exact, and price feeds with more than 18 decimals work.
- **Revert reasons** are decoded by name (module and parser errors).
- **Startup checks**: malformed or out-of-range variables are errors (the TS read `"100abc"` as 100 and junk as NaN), the RPC endpoints must serve `CHAIN`, and a registry that lists no module without `MODULE_ADDRESS` means no modules. A command argument that isn't a flag is an error, so a flag after it can't be silently dropped.
- **`.env` files** are parsed like dotenv 16, the TS loader: lines it skips are skipped, `$VAR` is not expanded, and the file's content never appears in an error. An unreadable file is an error (the TS ignored it).
- **Cron expressions** are parsed by robfig/cron, not node-cron: a day of week of 7 (Sunday for node-cron) and 7-field expressions are rejected at startup, `@hourly`-style descriptors are accepted, and when both the day of month and the day of week are set a run happens on either (node-cron: on both). The default schedules are unaffected.
- **Process**: clean shutdown on SIGINT/SIGTERM; cron runs never overlap, and a schedule that never fires (e.g. February 30) runs only at startup; the first refresh runs before polling starts; a failure or panic in one subaccount or module doesn't stop the others (a poll that can't read a module's events is retried whole, as in the TS); transactions already sent are awaited even when a run fails, except at shutdown.
- **Access policies**: the oracle serves only the modules its registry access policy covers (all of them unless restricted); the TS served every module naming it.
- **`analyze-tx`** replaces the TS `analyze-failed-tx.ts` script: it decodes calls with the module's current ABI (the script's copy had drifted), follows the trace to the protocol error, and needs no `MODULE_ADDRESS`. It reads `CHAIN` (the script always used Base Sepolia); prints amounts exactly (the script rounded them, as 1.50K) and unknown errors by selector and length, without their raw data; treats a mined transaction without a receipt as an error (the script reported it as pending); and exits with status 1 when interrupted, with no report.
- **Dry-run mode** and `ORACLE_ADDRESS` are new. `WINDOW_DURATION_SECONDS` is ignored, with a warning, as the TS ignored it: the window comes from each subaccount's on-chain limits.
- **The RPC URL** is redacted in the startup banner and in error messages (the TS printed it with its API key in both).

## Supported Chains

| Chain        | Chain ID | RPC Default                           | Block Time | Confirmations | ETH/USD Feed   |
| ------------ | -------- | ------------------------------------- | ---------- | ------------- | -------------- |
| Sepolia      | 11155111 | `ethereum-sepolia-rpc.publicnode.com` | 12s        | 12            | `0x694A...306` |
| Base         | 8453     | `mainnet.base.org`                    | 2s         | 3             | `0x7104...70`  |
| Base Sepolia | 84532    | `sepolia.base.org`                    | 2s         | 3             | `0x4aDC...b1`  |

## Token Configuration

Tokens for Safe value calculation are configured per chain in `internal/config/chains.go`. Supported token types:

| Type            | Valuation Method                                      |
| --------------- | ----------------------------------------------------- |
| `erc20`         | `balance * chainlinkPrice`                            |
| `aave-atoken`   | Same as erc20 (1:1 with underlying)                   |
| `morpho-vault`  | `convertToAssets(shares) * underlyingPrice`           |
| `uniswap-v2-lp` | `(ownedReserve0 * price0) + (ownedReserve1 * price1)` |

## Monitoring

Recommended alerts:

- **Oracle wallet balance** — needs ETH/gas to submit transactions
- **Staleness** — if `lastOracleUpdate` exceeds `maxOracleAge` (60 min), operations are frozen
- **Safe value age** — if `safeValue.lastUpdated` exceeds `maxSafeValueAge` (60 min), allowance updates fail
- **RPC availability** — configure `RPC_FALLBACK_URLS` so one endpoint going down doesn't stop the oracle
- **Process health** — systemd restarts the process; repeated restarts show in `journalctl -u multiclaw-oracle`. There is no persistence layer: a restart replays events

## Failure Modes

| Failure                  | Impact                                                                                 | Recovery                                              |
| ------------------------ | -------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| Oracle process crashes   | Operations freeze in 60 min                                                            | systemd restarts it; state rebuilds from events       |
| RPC endpoint down        | Same as process crash, unless a fallback endpoint answers                              | Switch RPC URL, restart                               |
| Oracle wallet out of gas | Oracle can't update chain                                                              | Fund the wallet                                       |
| Oracle key compromised   | Max damage per window: per-account spending cap + `maxOracleAcquiredBps` (default 20%) | Rotate oracle via `setAuthorizedOracle`, pause module |
