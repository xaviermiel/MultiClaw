#!/usr/bin/env bash
# Compare the decisions in the logs of a shadow run: the TS oracle running
# live (npm start) and, next to it, the Go oracle in dry-run mode acting as
# the same oracle (oracle run, with DRY_RUN=true and ORACLE_ADDRESS set to
# the TS oracle's address), on the same chain with the same cron schedules.
#
# Usage: scripts/shadow-compare.sh ts.log go.log
#
# Cycles are matched by the second of their start, i.e. their cron tick. For
# each cycle both logged, it compares per module the Safe value total and
# whether to write it, and per subaccount the "Allowance:" line and the
# batchUpdate decision (with its reasons). Initial runs at startup and cycles
# one oracle skipped (a poll held the lock) are only counted.
#
# Expected differences: values read while the TS oracle's own transactions
# were landing, e.g. a spending refresh starting with a Safe value write
# (the Go oracle read the chain after the write, or before).
#
# Exit status: 0 no differences, 1 differences, 2 nothing compared.
set -euo pipefail
export LC_ALL=C

if [ $# -ne 2 ]; then
	echo "usage: $0 ts.log go.log" >&2
	exit 2
fi
tab=$(printf '\t')

# records LOG: one "kind cycle module [subaccount] TAB decision" line per
# decision, sorted.
records() {
	awk '
	function emit(kind, key, value) { print kind " " key "\t" value }
	{
		# [Service 2026-10-09T16:58:00.012Z] message
		end = index($0, "] ")
		if (substr($0, 1, 1) != "[" || end == 0) next
		split(substr($0, 2, end - 2), head, " ")
		service = head[1]
		second = substr(head[2], 1, 19)
		msg = substr($0, end + 2)
	}
	service == "SafeValue" {
		if (msg == "=== Safe Value Monitor: Starting check ===") { cycle_sv = second; module_sv = "" }
		else if (msg == "=== Safe Value Monitor: Complete ===") cycle_sv = ""
		else if (cycle_sv == "") next
		else if (msg ~ /^--- Processing module: /) { split(msg, f, " "); module_sv = tolower(f[4]) }
		else if (msg ~ /^Total USD Value: /) { sub(/ \(on-chain: .*/, "", msg); emit("safe-value", cycle_sv " " module_sv, msg) }
		else if (msg ~ /^Updating due to staleness/) emit("safe-value-write", cycle_sv " " module_sv, "write (stale)")
		else if (msg ~ /^Updating due to value change/) emit("safe-value-write", cycle_sv " " module_sv, "write (value change)")
		else if (msg ~ /^Skipping update - value change within threshold/) emit("safe-value-write", cycle_sv " " module_sv, "no write")
		next
	}
	service == "SpendingOracle" {
		if (msg == "=== Spending Oracle: Periodic Refresh ===") { cycle = second; module = ""; account = "" }
		else if (msg == "=== Periodic Refresh Complete ===" || msg ~ /^Error in periodic refresh/) cycle = ""
		else if (cycle == "") next
		else if (msg ~ /^--- Processing module: /) { split(msg, f, " "); module = tolower(f[4]) }
		else if (msg ~ /^Processing subaccount: /) { split(msg, f, " "); account = tolower(f[3]) }
		else if (msg ~ /^Allowance: safeValue=/) emit("allowance", cycle " " module " " account, msg)
		else if (msg ~ /^  Reason: /) {
			reasons = substr(msg, 11)
			# Each oracle counts the time since its own last update.
			gsub(/stale \([0-9]+s > /, "stale (Ns > ", reasons)
			emit("batch", cycle " " module " " account, "update: " reasons)
		}
		else if (msg ~ /^Skipping batch update - no changes needed/) emit("batch", cycle " " module " " account, "no update")
		else if (msg ~ /^Error processing 0x/) emit("batch", cycle " " module " " account, "error")
	}
	' "$1" | sort -t "$tab" -k1,1
}

dir=$(mktemp -d "${TMPDIR:-/tmp}/shadow-compare.XXXXXX")
trap 'rm -f "$dir/ts" "$dir/go"; rmdir "$dir"' EXIT
records "$1" >"$dir/ts"
records "$2" >"$dir/go"

ts_only=$(join -t "$tab" -v 1 "$dir/ts" "$dir/go" | wc -l | tr -d ' ')
go_only=$(join -t "$tab" -v 2 "$dir/ts" "$dir/go" | wc -l | tr -d ' ')
join -t "$tab" "$dir/ts" "$dir/go" | awk -F "$tab" -v ts_only="$ts_only" -v go_only="$go_only" '
	$2 != $3 {
		print "DIFF " $1
		print "  ts: " $2
		print "  go: " $3
		diffs++
	}
	END {
		printf "%d decisions logged by both: %d identical, %d different.\n", NR, NR - diffs, diffs
		printf "Logged by one oracle only (startup runs, skipped cycles): TS %d, Go %d.\n", ts_only, go_only
		if (NR == 0) exit 2
		exit (diffs > 0)
	}'
