package startup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// clockTicksPerSecond is USER_HZ, the unit of /proc's start time. It is 100
// on every architecture Go and go-gui build for; reading it properly needs
// sysconf, which needs cgo, which Linux builds do without (decision 3).
const clockTicksPerSecond = 100

// processStart reads when this process started from /proc/self/stat, which
// counts in clock ticks since boot, and turns it into a wall-clock time by
// asking how long the machine has been up now. The answer is good to one
// tick, 10 ms.
func processStart(now time.Time) (time.Time, error) {
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return time.Time{}, fmt.Errorf("reading the process start time: %w", err)
	}
	ticks, err := startTicks(stat)
	if err != nil {
		return time.Time{}, err
	}
	var ts unix.Timespec
	// CLOCK_BOOTTIME, not CLOCK_MONOTONIC: /proc's start time counts time
	// spent suspended, and so must the clock it is compared with.
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return time.Time{}, fmt.Errorf("reading the time since boot: %w", err)
	}
	return startFromBoot(ticks, time.Duration(ts.Nano()), now), nil
}

// startTicks pulls field 22, starttime, out of a /proc/<pid>/stat line.
// Field 2 is the command name in parentheses, and it may itself hold spaces
// and parentheses, so fields are counted from the last ')'.
func startTicks(stat []byte) (uint64, error) {
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, errors.New("process stat has no command name")
	}
	// After ")" come fields 3 onwards; starttime is field 22, the 20th.
	fields := bytes.Fields(stat[end+1:])
	const startField = 22 - 3
	if len(fields) <= startField {
		return 0, fmt.Errorf("process stat has %d fields after the command name, want at least %d", len(fields), startField+1)
	}
	ticks, err := strconv.ParseUint(string(fields[startField]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("process start time: %w", err)
	}
	return ticks, nil
}

// startFromBoot converts a start time in clock ticks since boot into wall
// time, given how long the machine has been up at now.
func startFromBoot(ticks uint64, sinceBoot time.Duration, now time.Time) time.Time {
	startedAfterBoot := time.Duration(ticks) * time.Second / clockTicksPerSecond
	return now.Add(-(sinceBoot - startedAfterBoot))
}
