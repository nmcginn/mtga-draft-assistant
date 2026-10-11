// Package startup measures how long the app takes to put its first frame on
// screen, so the 250 ms target in CLAUDE.md is checked against a number
// rather than a feeling (decision 19).
//
// A Timeline is a handful of timestamps filled in as the window comes up.
// Reading the clock is the only thing it does while the app starts; turning
// them into a sentence is a pure function, done once, after the fact.
package startup

import (
	"fmt"
	"strings"
	"time"
)

// Target is the startup budget from CLAUDE.md: the window is up within this
// long of launch.
const Target = 250 * time.Millisecond

// Timeline is when each stage of startup finished. A zero field is a stage
// that has not happened, or, for ProcessStart, one the OS would not tell us.
type Timeline struct {
	// ProcessStart is when the OS started the process, before the Go runtime
	// or any package initialised. Zero where the platform cannot say.
	ProcessStart time.Time
	// Main is when main() began: the Go runtime, and every package's init
	// (go-gui's included), have run.
	Main time.Time
	// WindowCreated is when the backend handed the window to the app's
	// OnInit: the native window and its GPU context exist, nothing is drawn.
	WindowCreated time.Time
	// FirstView is when the first frame began building the app's view.
	FirstView time.Time
	// FirstFrame is when the first frame had been drawn and handed to the
	// display. That stretch also holds go-gui connecting its accessibility
	// bridge, which it does once, between building the first frame and
	// presenting it.
	FirstFrame time.Time
}

// New starts a timeline at main, asking the OS when the process began. A
// platform that cannot say leaves ProcessStart zero, and the report then
// counts from main instead.
func New(main time.Time) Timeline {
	t := Timeline{Main: main}
	if start, err := processStart(main); err == nil && !start.After(main) {
		t.ProcessStart = start
	}
	return t
}

// Total is the time from the earliest point known (process start, or main
// where that is unknown) to the first presented frame. It is zero until
// the first frame has been presented.
func (t Timeline) Total() time.Duration {
	if t.FirstFrame.IsZero() {
		return 0
	}
	return t.FirstFrame.Sub(t.origin())
}

// origin is the earliest point the timeline knows.
func (t Timeline) origin() time.Time {
	if !t.ProcessStart.IsZero() {
		return t.ProcessStart
	}
	return t.Main
}

// Report is the timeline as one line for a person reading CI output or a
// terminal: the total against the target first, then where the time went.
func (t Timeline) Report() string {
	if t.FirstFrame.IsZero() {
		return "startup: no frame has been presented yet"
	}
	from := "process start"
	if t.ProcessStart.IsZero() {
		from = "main() (this platform does not say when the process started, so Go runtime start-up is not counted)"
	}
	total := t.Total()
	verdict := "within"
	if total > Target {
		verdict = "over"
	}

	type stage struct {
		name     string
		from, to time.Time
	}
	stages := []stage{
		{"Go runtime and package init", t.ProcessStart, t.Main},
		{"window and GPU context", t.Main, t.WindowCreated},
		{"until the first view", t.WindowCreated, t.FirstView},
		{"layout, accessibility bridge, draw and present", t.FirstView, t.FirstFrame},
	}
	var parts []string
	for _, s := range stages {
		if s.from.IsZero() || s.to.IsZero() {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", s.name, ms(s.to.Sub(s.from))))
	}
	return fmt.Sprintf("startup: first frame presented %s after %s, %s the %s target (%s)",
		ms(total), from, verdict, ms(Target), strings.Join(parts, "; "))
}

// ms rounds to whole milliseconds: the stages are a few to a few hundred
// milliseconds long, and the Linux process start is only known to 10 ms.
func ms(d time.Duration) string {
	return fmt.Sprintf("%d ms", d.Round(time.Millisecond).Milliseconds())
}
