#!/usr/bin/env bash
# Fork differential test: the TS oracle and the Go oracle run against the
# same anvil fork state, and the transactions they send must be identical.
#
# On a local anvil fork of FORK_URL, each module (REGISTRY_ADDRESS's active
# modules, else MODULE_ADDRESS) gets a throwaway oracle key as its
# authorizedOracle (sent as the module owner, on the fork only). Then:
#   1. the TS oracle runs one Safe value check and one spending refresh
#      (scripts/ts-run-once.ts);
#   2. the fork is reverted to the state before step 1;
#   3. the Go oracle does the same (oracle run --once);
# and the calldata of the transactions each one sent is compared.
#
# Usage (from oracle/):
#   FORK_URL=https://... REGISTRY_ADDRESS=0x... make fork-diff
#   FORK_URL=https://... REGISTRY_ADDRESS=0x... scripts/fork-diff.sh [go-binary]
#
# Environment:
#   FORK_URL           RPC endpoint to fork (required)
#   REGISTRY_ADDRESS   ModuleRegistry whose modules are tested, or
#   MODULE_ADDRESS     a single module
#   CHAIN              default base-sepolia
#   FORK_BLOCK         block to fork at (default: the latest)
#   ANVIL_PORT         default 8546
#   OUT_DIR            where the logs and results go (default: a new temp dir)
#   BLOCKS_TO_LOOK_BACK, CONFIRMATION_BLOCKS, MAX_BLOCKS_PER_QUERY,
#   MAX_HISTORICAL_BLOCKS, GAS_LIMIT, SAFE_VALUE_CHANGE_THRESHOLD_BPS,
#   SAFE_VALUE_MAX_STALENESS_SECONDS, ALLOWANCE_INCREASE_THRESHOLD_BPS and
#   SPENDING_ORACLE_MAX_STALENESS_SECONDS are passed to both oracles.
# Nothing else reaches the oracles (no .env file, no other variable), and
# their only RPC endpoint is the fork.
#
# The TS reads each subaccount's events in a single eth_getLogs over
# 2*BLOCKS_TO_LOOK_BACK blocks, which the fork forwards to FORK_URL: with an
# RPC limiting the range (sepolia.base.org: 200 blocks), set
# BLOCKS_TO_LOOK_BACK=100 MAX_BLOCKS_PER_QUERY=200.
#
# Expected differences: on Base mainnet the TS cannot read the LINK/USD and
# AAVE/USD feeds (see README), so updateSafeValue differs for a Safe holding
# LINK or AAVE. A spending event crossing the window's edge between the two
# runs can also change an allowance: run again to rule that out.
#
# Exit status: 0 identical, 1 different, 2 setup error or nothing compared.
set -Eeuo pipefail

die() {
	echo "fork-diff: $*" >&2
	exit 2
}
# Any other failing command is a setup error too, not a difference.
trap 'die "setup failed at line $LINENO"' ERR

oracle_dir=$(cd "$(dirname "$0")/.." && pwd)
go_bin=${1:-$oracle_dir/bin/oracle}
tsx=$oracle_dir/node_modules/.bin/tsx

[ -n "${FORK_URL:-}" ] || die "FORK_URL is required"
[ -n "${REGISTRY_ADDRESS:-}${MODULE_ADDRESS:-}" ] || die "REGISTRY_ADDRESS or MODULE_ADDRESS is required"
for tool in anvil cast jq openssl; do
	command -v "$tool" >/dev/null || die "$tool not found"
done
[ -x "$go_bin" ] || die "$go_bin not found (make build)"
go_bin=$(cd "$(dirname "$go_bin")" && pwd)/$(basename "$go_bin") # the runs cd
[ -x "$tsx" ] || die "$tsx not found (npm install)"

out=${OUT_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/fork-diff.XXXXXX")}
mkdir -p "$out"
rpc=http://127.0.0.1:${ANVIL_PORT:-8546}

lower() { echo "$1" | tr '[:upper:]' '[:lower:]'; }
rpc_call() { cast rpc --rpc-url "$rpc" "$@"; }

# --- Fork ---------------------------------------------------------------
if cast block-number --rpc-url "$rpc" >/dev/null 2>&1; then
	die "something already answers on $rpc (set ANVIL_PORT)"
fi
anvil_args=(--fork-url "$FORK_URL" --host 127.0.0.1 --port "${ANVIL_PORT:-8546}")
if [ -n "${FORK_BLOCK:-}" ]; then
	anvil_args+=(--fork-block-number "$FORK_BLOCK")
fi
anvil "${anvil_args[@]}" >"$out/anvil.log" 2>&1 &
anvil_pid=$!
trap 'kill "$anvil_pid" 2>/dev/null || true' EXIT
for _ in $(seq 120); do
	cast block-number --rpc-url "$rpc" >/dev/null 2>&1 && break
	kill -0 "$anvil_pid" 2>/dev/null || die "anvil exited (see $out/anvil.log)"
	sleep 0.5
done
fork_block=$(cast block-number --rpc-url "$rpc") || die "anvil did not start (see $out/anvil.log)"
echo "Forked ${CHAIN:-base-sepolia} at block $fork_block on $rpc"

# --- A throwaway oracle for every module (on the fork only) ---------------
key=0x$(openssl rand -hex 32)
oracle=$(cast wallet address --private-key "$key")
rpc_call anvil_setBalance "$oracle" 0x3635C9ADC5DEA00000 >/dev/null # 1000 ETH

modules=()
if [ -n "${REGISTRY_ADDRESS:-}" ]; then
	read -ra modules <<<"$(cast call --rpc-url "$rpc" "$REGISTRY_ADDRESS" 'getActiveModules()(address[])' | tr -d '[],')"
fi
if [ ${#modules[@]} -eq 0 ] && [ -n "${MODULE_ADDRESS:-}" ]; then
	modules=("$MODULE_ADDRESS")
fi
[ ${#modules[@]} -gt 0 ] || die "no active modules"

for m in "${modules[@]}"; do
	owner=$(cast call --rpc-url "$rpc" "$m" 'owner()(address)')
	was=$(cast call --rpc-url "$rpc" "$m" 'authorizedOracle()(address)')
	# The owner (the Safe) pays for the call; its balance is restored after.
	balance=$(cast balance --rpc-url "$rpc" "$owner")
	rpc_call anvil_impersonateAccount "$owner" >/dev/null
	rpc_call anvil_setBalance "$owner" 0x3635C9ADC5DEA00000 >/dev/null
	if cast send --rpc-url "$rpc" --unlocked --from "$owner" "$m" 'setAuthorizedOracle(address)' "$oracle" \
		>/dev/null 2>"$out/set-oracle.err"; then
		echo "Module $m: oracle $was -> $oracle"
	else
		echo "Module $m: cannot set the oracle, both runs skip it: $(tail -n 1 "$out/set-oracle.err")"
	fi
	rpc_call anvil_setBalance "$owner" "$(cast to-hex "$balance")" >/dev/null
	rpc_call anvil_stopImpersonatingAccount "$owner" >/dev/null
done

snapshot=$(rpc_call evm_snapshot | tr -d '"')
base_block=$(cast block-number --rpc-url "$rpc")

# --- The two runs --------------------------------------------------------
oracle_env=(
	PATH="$PATH" HOME="$HOME" TMPDIR="${TMPDIR:-/tmp}"
	CHAIN="${CHAIN:-base-sepolia}" RPC_URL="$rpc" PRIVATE_KEY="$key"
)
for v in REGISTRY_ADDRESS MODULE_ADDRESS BLOCKS_TO_LOOK_BACK CONFIRMATION_BLOCKS MAX_BLOCKS_PER_QUERY \
	MAX_HISTORICAL_BLOCKS GAS_LIMIT SAFE_VALUE_CHANGE_THRESHOLD_BPS SAFE_VALUE_MAX_STALENESS_SECONDS \
	ALLOWANCE_INCREASE_THRESHOLD_BPS SPENDING_ORACLE_MAX_STALENESS_SECONDS; do
	if [ -n "${!v:-}" ]; then
		oracle_env+=("$v=${!v}")
	fi
done

# run NAME COMMAND...: runs an oracle from $out with oracle_env only (the TS
# script runs from a new empty directory, the Go oracle reads an empty env
# file, so no .env reaches either), logging to $out/NAME.log.
: >"$out/empty.env"
run() {
	local name=$1 status=0
	shift
	echo "Running the $name oracle (log: $out/$name.log)"
	(cd "$out" && env -i "${oracle_env[@]}" "$@") >"$out/$name.log" 2>&1 || status=$?
	if [ "$status" -ne 0 ]; then
		echo "  the $name oracle exited with status $status"
	fi
}

# sent: the oracle's transactions since the snapshot, one "to calldata" per line.
sent() {
	local head n from
	head=$(cast block-number --rpc-url "$rpc")
	from=$(lower "$oracle")
	for ((n = base_block + 1; n <= head; n++)); do
		rpc_call eth_getBlockByNumber "$(cast to-hex "$n")" true |
			jq -r --arg from "$from" '.transactions[] | select((.from | ascii_downcase) == $from) | "\(.to) \(.input)"'
	done
}

# describe: the transactions of sent's output, decoded.
describe() {
	local to data sig
	while read -r to data; do
		case ${data:0:10} in
		0x1eed40fa) sig='batchUpdate(address,uint256,uint256,address[],uint256[],uint256[])' ;;
		0xe70d0840) sig='updateSafeValue(uint256)' ;;
		*) sig= ;;
		esac
		echo "${sig%%(*} to $to"
		if [ -n "$sig" ]; then
			cast decode-calldata "$sig" "$data" | sed 's/^/  /'
		fi
		echo "  calldata $data"
	done
}

run ts "$tsx" "$oracle_dir/scripts/ts-run-once.ts"
sent >"$out/ts.txs"

[ "$(rpc_call evm_revert "$snapshot")" = true ] || die "evm_revert failed"
[ "$(cast block-number --rpc-url "$rpc")" = "$base_block" ] || die "evm_revert did not restore block $base_block"

run go "$go_bin" run --once --env-file "$out/empty.env"
sent >"$out/go.txs"

# --- Comparison ----------------------------------------------------------
describe <"$out/ts.txs" >"$out/ts.decoded"
describe <"$out/go.txs" >"$out/go.decoded"
ts_count=$(wc -l <"$out/ts.txs" | tr -d ' ')
go_count=$(wc -l <"$out/go.txs" | tr -d ' ')
echo "TS sent $ts_count transaction(s), Go sent $go_count."

if cmp -s "$out/ts.txs" "$out/go.txs"; then
	[ "$ts_count" -gt 0 ] || die "neither oracle sent a transaction: nothing was compared (see the logs)"
	echo "IDENTICAL: same transactions, byte for byte ($out/go.decoded)"
	exit 0
fi
diff -u --label ts --label go "$out/ts.decoded" "$out/go.decoded" || true
echo "DIFFERENT: see the logs in $out"
exit 1
