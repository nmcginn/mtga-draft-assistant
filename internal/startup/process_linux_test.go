package startup

import (
	"testing"
	"time"
)

// A /proc/self/stat line from a real Linux machine (kernel 6.x), as proc(5)
// documents it. starttime, field 22, is 48119.
const realStat = "10702 (head) R 10698 10702 10698 0 -1 4194304 85 0 0 0 0 0 0 0 20 0 1 0 48119 2789376 397 18446744073709551615 94470609076224 94470609098625 140726874602672 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 94470609111664 94470609113240 94471307804672 140726874611809 140726874611837 140726874611837 140726874619882 0\n"

func TestTheStartTimeIsReadFromARealStatLine(t *testing.T) {
	got, err := startTicks([]byte(realStat))
	if err != nil || got != 48119 {
		t.Errorf("startTicks = %d, %v; want 48119", got, err)
	}
}

func TestACommandNameWithSpacesAndParenthesesDoesNotShiftTheFields(t *testing.T) {
	// The command name is whatever the binary was called, and a user can
	// call it anything.
	stat := "10702 (my (draft) app) R 10698 10702 10698 0 -1 4194304 85 0 0 0 0 0 0 0 20 0 1 0 48119 2789376 397\n"
	got, err := startTicks([]byte(stat))
	if err != nil || got != 48119 {
		t.Errorf("startTicks = %d, %v; want 48119", got, err)
	}
}

func TestAStatLineThatCannotBeReadIsAnErrorNotAWrongTime(t *testing.T) {
	for name, stat := range map[string]string{
		"empty":                   "",
		"no command name":         "10702 head R 10698",
		"cut off before field 22": realStat[:60],
		"not a number":            "1 (a) R 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 soon 0 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := startTicks([]byte(stat)); err == nil {
				t.Errorf("startTicks(%q) = %d with no error", stat, got)
			}
		})
	}
}

func TestBootRelativeTicksBecomeWallClockTime(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	// Started 481.19 s after boot; the machine has been up 481.50 s.
	got := startFromBoot(48119, 481500*time.Millisecond, now)
	if want := now.Add(-310 * time.Millisecond); !got.Equal(want) {
		t.Errorf("startFromBoot = %v, want %v", got, want)
	}
}

func TestThisProcessStartTimeIsReadFromProc(t *testing.T) {
	now := time.Now()
	got, err := processStart(now)
	if err != nil {
		t.Fatal(err)
	}
	// One clock tick of slack: the start time is only known to 10 ms.
	if got.After(now.Add(10 * time.Millisecond)) {
		t.Errorf("process start %v is after now %v", got, now)
	}
}
