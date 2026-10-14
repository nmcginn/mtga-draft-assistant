#!/bin/sh
# Launches the real binary, waits for its startup work to finish, screenshots
# first paint, and checks it then sits idle while nothing is happening
# (decision 10).
#
#   scripts/smoke.sh <binary> <screenshot.png> [app flags...]
#
# Any flags after the screenshot go to the app: `make smoke` runs it once as a
# plain launch (still looking for Arena's log) and once with -log naming a
# fixture, so idle is also measured while the log is being followed.
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
# first frame is measured by the app itself (-startup-time, decision 19) and
# printed beside the idle numbers; it is reported, not gated.
set -eu

if [ "$#" -lt 2 ]; then
	echo "usage: $0 <binary> <screenshot.png> [app flags...]" >&2
	exit 2
fi
bin=$1
shot=$2
shift 2
window=${SMOKE_IDLE_SECONDS:-5}
# How long the app may take to paint and then go quiet before that alone is a
# failure.
startup_limit=${SMOKE_STARTUP_SECONDS:-30}
# Clock ticks of CPU the idle window may use. 100 ticks is one CPU-second, so
# the default allows 2% of one core over five seconds; a redraw loop that
# never sleeps burns roughly 500.
max_ticks=${SMOKE_MAX_IDLE_TICKS:-10}

# The app's stderr goes to a file so the startup line can be picked out of
# it; it is shown in full if anything goes wrong.
log=$(mktemp)
"$bin" -startup-time "$@" 2>"$log" &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; rm -f "$log"' EXIT
show_log() { sed 's/^/  app: /' "$log" >&2; }

cpu_ticks() { awk '{ print $14 + $15 }' "/proc/$pid/stat"; }
alive() {
	if ! kill -0 "$pid" 2>/dev/null; then
		wait "$pid" || true
		show_log
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
		show_log
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

# The app prints this at the start of its second frame, just after the first
# was presented; the waits above outlast that by seconds.
first_frame=$(grep '^startup:' "$log" || true)
if [ -z "$first_frame" ]; then
	show_log
	echo "the app painted but never reported its time to first frame (-startup-time)" >&2
	exit 1
fi

echo "first paint: $shot"
echo "$first_frame"
echo "settled: painted within ${painted_at}s, settled within ${waited}s, using $startup_ticks ticks (includes Mesa compiling shaders on a cold cache)"
echo "idle CPU: $used ticks over ${window}s (budget $max_ticks); RSS: $rss"
if [ "$used" -gt "$max_ticks" ]; then
	show_log
	echo "the app is not idle while nothing is happening; something is redrawing or polling too often" >&2
	exit 1
fi
