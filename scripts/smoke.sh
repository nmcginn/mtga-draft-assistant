#!/bin/sh
# Launches the real binary, waits for its startup work to finish, screenshots
# first paint, and checks it then sits idle while nothing is happening
# (decision 10).
#
#   scripts/smoke.sh <binary> <screenshot.png>
#
# Needs an X display; on a headless machine run it as CI does,
#   dbus-run-session -- xvfb-run -a make smoke
# with Mesa's EGL (libegl1 libegl-mesa0 libgl1-mesa-dri), ImageMagick, and
# dbus installed. Linux only: it reads /proc for CPU time.
#
# Startup is waited out, not timed: the script first waits for the window to
# paint (the screen holds more than one colour), then for a second with no CPU.
# Neither alone is enough. Under Mesa's software renderer a cold shader cache
# costs over a CPU-second before the first frame, which outlasts any fixed
# sleep on a slow runner; and before rendering starts the app can sit blocked
# and quiet (for example while D-Bus starts up for go-gui's accessibility
# bridge), which looks like "settled" if CPU is all that is watched. Time to
# first frame is its own measurement (see the roadmap).
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 <binary> <screenshot.png>" >&2
	exit 2
fi
bin=$1
shot=$2
window=${SMOKE_IDLE_SECONDS:-5}
# How long the app may take to paint and then go quiet before that alone is a
# failure.
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

# How many colours are on screen. An empty Xvfb root is one flat colour; a
# painted window, with its text antialiased, is hundreds.
colours() {
	import -window root "$shot" 2>/dev/null || { echo 0; return; }
	convert "$shot" -format '%k' info: 2>/dev/null || echo 0
}

waited=0
tick() {
	sleep 1
	waited=$((waited + 1))
	alive
	if [ "$waited" -ge "$startup_limit" ]; then
		echo "the app was still $1 ${startup_limit}s after launch" >&2
		exit 1
	fi
}

# First the window has to paint at all...
while [ "$(colours)" -le 1 ]; do
	tick "not painted"
done
painted_at=$waited

# ...then the work around first paint has to finish: a whole second with
# (almost) no CPU used.
prev=$(cpu_ticks)
while :; do
	tick "busy"
	now=$(cpu_ticks)
	if [ $((now - prev)) -le 1 ]; then
		break
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
echo "startup: painted within ${painted_at}s, settled within ${waited}s, using $startup_ticks ticks (includes Mesa compiling shaders on a cold cache)"
echo "idle CPU: $used ticks over ${window}s (budget $max_ticks); RSS: $rss"
if [ "$used" -gt "$max_ticks" ]; then
	echo "the app is not idle while nothing is happening; something is redrawing or polling too often" >&2
	exit 1
fi
