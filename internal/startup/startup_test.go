package startup

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

// full is a timeline where every stage happened, 180 ms end to end.
func full() Timeline {
	return Timeline{
		ProcessStart:  at(0),
		Main:          at(20),
		WindowCreated: at(110),
		FirstView:     at(112),
		FirstFrame:    at(180),
	}
}

func TestTheReportCountsFromProcessStartAndSaysWhereTheTimeWent(t *testing.T) {
	got := full().Report()
	for _, want := range []string{
		"first frame presented 180 ms after process start",
		"within the 250 ms target",
		"Go runtime and package init 20 ms",
		"window and GPU context 90 ms",
		"until the first view 2 ms",
		"layout, accessibility bridge, draw and present 68 ms",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report %q does not say %q", got, want)
		}
	}
	if !strings.HasPrefix(got, "startup: ") {
		t.Errorf("report %q does not start with the prefix scripts look for", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("report %q is more than one line", got)
	}
}

func TestTheStagesAreReportedInTheOrderTheyHappen(t *testing.T) {
	got := full().Report()
	order := []string{"Go runtime", "window and GPU", "until the first view", "layout,"}
	last := -1
	for _, s := range order {
		i := strings.Index(got, s)
		if i <= last {
			t.Fatalf("report %q does not list %q after the stage before it", got, s)
		}
		last = i
	}
}

func TestAStartupOverTheTargetSaysSo(t *testing.T) {
	tl := full()
	tl.FirstFrame = at(400)
	if got := tl.Report(); !strings.Contains(got, "400 ms") || !strings.Contains(got, "over the 250 ms target") {
		t.Errorf("report %q does not say 400 ms is over the target", got)
	}
}

func TestExactlyTheTargetIsWithinIt(t *testing.T) {
	tl := full()
	tl.FirstFrame = tl.ProcessStart.Add(Target)
	if got := tl.Report(); !strings.Contains(got, "within the 250 ms target") {
		t.Errorf("report %q, want exactly 250 ms to count as within the target", got)
	}
}

func TestWithoutAProcessStartTheReportCountsFromMainAndSaysWhatIsMissing(t *testing.T) {
	tl := full()
	tl.ProcessStart = time.Time{}
	if got, want := tl.Total(), 160*time.Millisecond; got != want {
		t.Errorf("total from main = %v, want %v", got, want)
	}
	got := tl.Report()
	if !strings.Contains(got, "160 ms after main()") || !strings.Contains(got, "Go runtime start-up is not counted") {
		t.Errorf("report %q does not say it counts from main and what that leaves out", got)
	}
	if strings.Contains(got, "Go runtime and package init") {
		t.Errorf("report %q times a stage whose start is unknown", got)
	}
}

func TestNothingIsReportedAsPresentedBeforeTheFirstFrame(t *testing.T) {
	tl := full()
	tl.FirstFrame = time.Time{}
	if got := tl.Total(); got != 0 {
		t.Errorf("total before the first frame = %v, want 0", got)
	}
	if got := tl.Report(); strings.Contains(got, "presented ") && !strings.Contains(got, "no frame") {
		t.Errorf("report before the first frame claims one: %q", got)
	}
}

func TestStagesThatDidNotHappenAreLeftOutRatherThanShownAsNegative(t *testing.T) {
	// A window built by hand, not by the backend, has no OnInit time.
	tl := full()
	tl.WindowCreated = time.Time{}
	got := tl.Report()
	if strings.Contains(got, "window and GPU context") || strings.Contains(got, "until the first view") {
		t.Errorf("report %q times stages around a missing timestamp", got)
	}
	if strings.Contains(got, "-") {
		t.Errorf("report %q has a negative duration", got)
	}
}

func TestDurationsAreRoundedToTheNearestMillisecond(t *testing.T) {
	tl := full()
	tl.FirstFrame = at(180).Add(600 * time.Microsecond)
	if got := tl.Report(); !strings.Contains(got, "181 ms after") {
		t.Errorf("report %q, want 180.6 ms rounded to 181 ms", got)
	}
}

func TestNewKnowsWhenThisProcessStartedWhereTheOSCanSay(t *testing.T) {
	main := time.Now()
	tl := New(main)
	if tl.Main != main {
		t.Errorf("Main = %v, want %v", tl.Main, main)
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		if tl.ProcessStart.IsZero() {
			t.Fatal("no process start time on a platform that has one")
		}
		if tl.ProcessStart.After(main) {
			t.Errorf("process started at %v, after main at %v", tl.ProcessStart, main)
		}
		// The test binary started moments ago; an hour allows for a slow
		// machine running the whole suite under the race detector.
		if age := main.Sub(tl.ProcessStart); age > time.Hour {
			t.Errorf("process start is %v before main; the clock arithmetic is off", age)
		}
	default:
		if !tl.ProcessStart.IsZero() {
			t.Errorf("ProcessStart = %v on a platform with no implementation", tl.ProcessStart)
		}
	}
}
