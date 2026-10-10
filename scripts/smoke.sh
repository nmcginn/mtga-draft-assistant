#!/bin/sh
# Launches the real binary, screenshots its first paint, and checks it sits
# idle when nothing is happening (decision 10).
#
#   scripts/smoke.sh <binary> <screenshot.png>
#
# Needs an X display; run it under `xvfb-run -a` on a headless machine, with
# Mesa's EGL installed (libegl1 libegl-mesa0 libgl1-mesa-dri) and ImageMagick
# for `import`. Linux only: it reads /proc for CPU time.
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 <binary> <screenshot.png>" >&2
	exit 2
fi
bin=$1
shot=$2
settle=${SMOKE_SETTLE_SECONDS:-2}
window=${SMOKE_IDLE_SECONDS:-5}
# Clock ticks of CPU the idle window may use. 100 ticks is one CPU-second, so
# the default allows 2% of one core over five seconds; a redraw loop that
# never sleeps burns roughly 500.
max_ticks=${SMOKE_MAX_IDLE_TICKS:-10}

"$bin" &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT

sleep "$settle"
if ! kill -0 "$pid" 2>/dev/null; then
	wait "$pid" || true
	echo "the app exited within ${settle}s of launch; its output is above" >&2
	exit 1
fi

import -window root "$shot"

cpu_ticks() { awk '{ print $14 + $15 }' "/proc/$pid/stat"; }
before=$(cpu_ticks)
sleep "$window"
after=$(cpu_ticks)
used=$((after - before))
rss=$(awk '/^VmRSS/ { print $2 " " $3 }' "/proc/$pid/status")

echo "first paint: $shot"
echo "idle CPU: $used ticks over ${window}s (budget $max_ticks); RSS: $rss"
if [ "$used" -gt "$max_ticks" ]; then
	echo "the app is not idle while nothing is happening; something is redrawing or polling too often" >&2
	exit 1
fi
