#!/bin/sh
# Launches the real binary, waits for its startup work to finish, screenshots
# first paint, and checks it then sits idle while nothing is happening
# (decision 10).
#
#   scripts/smoke.sh <binary> <screenshot.png>
#
# Needs an X display; run it under `xvfb-run -a` on a headless machine, with
# Mesa's EGL installed (libegl1 libegl-mesa0 libgl1-mesa-dri) and ImageMagick
# for `import`. Linux only: it reads /proc for CPU time.
#
# Startup is waited out, not timed: under Mesa's software renderer a cold
# shader cache costs over a CPU-second of compiling before the first frame,
# and on a slow CI runner that runs past any fixed sleep. Measuring "idle"
# from a fixed moment would then measure the tail of startup instead. Time to
# first frame is its own measurement (see the roadmap).
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 <binary> <screenshot.png>" >&2
	exit 2
fi
bin=$1
shot=$2
window=${SMOKE_IDLE_SECONDS:-5}
# How long startup may keep the CPU busy before that alone is a failure.
startup_limit=${SMOKE_STARTUP_SECONDS:-30}
# Clock ticks of CPU the idle window may use. 100 ticks is one CPU-second, so
# the default allows 2% of one core over five seconds; a redraw loop that
# never sleeps burns roughly 500.
max_ticks=${SMOKE_MAX_IDLE_TICKS:-10}

"$bin" &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT

cpu_ticks() { awk '{ print $14 + $15 }' "/proc/$pid/stat"; }
alive() {
	if ! kill -0 "$pid" 2>/dev/null; then
		wait "$pid" || true
		echo "the app exited during startup; its output is above" >&2
		exit 1
	fi
}

# Startup is over once a whole second passes with (almost) no CPU used.
waited=0
prev=$(cpu_ticks)
while :; do
	sleep 1
	waited=$((waited + 1))
	alive
	now=$(cpu_ticks)
	if [ $((now - prev)) -le 1 ]; then
		break
	fi
	if [ "$waited" -ge "$startup_limit" ]; then
		echo "the app was still busy ${startup_limit}s after launch; it never went idle" >&2
		exit 1
	fi
	prev=$now
done
startup_ticks=$(cpu_ticks)

import -window root "$shot"

before=$(cpu_ticks)
sleep "$window"
after=$(cpu_ticks)
used=$((after - before))
rss=$(awk '/^VmRSS/ { print $2 " " $3 }' "/proc/$pid/status")

echo "first paint: $shot"
echo "startup: settled within ${waited}s, using $startup_ticks ticks (includes Mesa compiling shaders on a cold cache)"
echo "idle CPU: $used ticks over ${window}s (budget $max_ticks); RSS: $rss"
if [ "$used" -gt "$max_ticks" ]; then
	echo "the app is not idle while nothing is happening; something is redrawing or polling too often" >&2
	exit 1
fi
