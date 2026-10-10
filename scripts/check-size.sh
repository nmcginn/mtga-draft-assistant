#!/bin/sh
# Fails when a release binary is larger than the budget (decision 10).
#
#   scripts/check-size.sh <binary> <budget-in-MiB>
#
# Raising the budget is allowed, but deliberately: change it in the Makefile in
# the PR whose dependency costs the space, and say what bought it.
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 <binary> <budget-in-MiB>" >&2
	exit 2
fi
bin=$1
budget_mib=$2

if [ ! -f "$bin" ]; then
	echo "no binary at $bin; build it first with 'make release'" >&2
	exit 2
fi

bytes=$(wc -c <"$bin" | tr -d ' ')
budget=$((budget_mib * 1024 * 1024))
size=$(awk "BEGIN { printf \"%.1f\", $bytes / 1048576 }")

if [ "$bytes" -gt "$budget" ]; then
	echo "$bin is ${size} MiB, over the ${budget_mib} MiB budget." >&2
	echo "Find what grew with 'go version -m $bin' and 'go tool nm -size -sort size $bin | head'." >&2
	echo "If the growth is worth it, raise SIZE_BUDGET_MIB in the Makefile and say why in the PR." >&2
	exit 1
fi
echo "$bin is ${size} MiB (budget ${budget_mib} MiB)."
