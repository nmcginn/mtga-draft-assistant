#!/bin/sh
# Launches the real binary, waits for it to report how long its first frame
# took to reach the screen (decision 19), prints that line, and closes it.
#
#   scripts/first-frame.sh <binary> [runs]
#
# Needs a display: on macOS a logged-in session, as CI's runners have; on
# Linux an X server (make smoke runs the same measurement under Xvfb). Each
# run is a fresh process, so the first run pays any cold caches (shaders,
# fonts) and later runs show what a relaunch costs.
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	echo "usage: $0 <binary> [runs]" >&2
	exit 2
fi
bin=$1
runs=${2:-1}
# Tenths of a second to wait for the line before giving up on a run.
limit=${FIRST_FRAME_LIMIT_TENTHS:-300}

log=$(mktemp)
pid=
trap 'if [ -n "$pid" ]; then kill "$pid" 2>/dev/null || true; fi; rm -f "$log"' EXIT

run=1
while [ "$run" -le "$runs" ]; do
	: >"$log"
	"$bin" -startup-time 2>"$log" &
	pid=$!
	waited=0
	until grep -q '^startup:' "$log"; do
		if ! kill -0 "$pid" 2>/dev/null; then
			wait "$pid" || true
			sed 's/^/  app: /' "$log" >&2
			echo "run $run: the app exited before presenting a frame; its output is above" >&2
			exit 1
		fi
		if [ "$waited" -ge "$limit" ]; then
			sed 's/^/  app: /' "$log" >&2
			echo "run $run: no frame presented within $((limit / 10))s" >&2
			exit 1
		fi
		sleep 0.1
		waited=$((waited + 1))
	done
	echo "run $run: $(grep '^startup:' "$log")"
	kill "$pid" 2>/dev/null || true
	wait "$pid" 2>/dev/null || true
	pid=
	run=$((run + 1))
done
