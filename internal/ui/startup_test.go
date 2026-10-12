package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/startup"
)

// tickingClock returns a clock that moves on a millisecond each time it is
// read, so every stage gets a distinct, ordered timestamp.
func tickingClock() func() time.Time {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	return func() time.Time {
		now = now.Add(time.Millisecond)
		return now
	}
}

// timedWindow builds the real window headless with startup timing on, and
// counts how often it reports.
func timedWindow(t *testing.T) (*gui.Window, *startup.Timeline, *[]startup.Timeline) {
	t.Helper()
	tl := startup.Timeline{Main: time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)}
	var reports []startup.Timeline
	opts := Options{
		Startup:      &tl,
		OnFirstFrame: func(t startup.Timeline) { reports = append(reports, t) },
		now:          tickingClock(),
	}
	cfg := windowCfg(t.Context(), app.New(), opts)
	cfg.Width, cfg.Height = defaultWidth, defaultHeight
	return gui.NewTestWindow(t, cfg), &tl, &reports
}

func TestTheWindowBeingCreatedIsTimedBeforeAnythingIsDrawn(t *testing.T) {
	_, tl, reports := timedWindow(t)
	if tl.WindowCreated.IsZero() {
		t.Error("window creation was not timed by OnInit")
	}
	if !tl.FirstView.IsZero() || len(*reports) != 0 {
		t.Errorf("before any frame: first view %v, %d reports; want neither", tl.FirstView, len(*reports))
	}
}

func TestTheFirstFrameIsReportedOnlyOnceItHasBeenPresented(t *testing.T) {
	w, tl, reports := timedWindow(t)

	// Frame 1 builds the view; the backend then draws and presents it.
	w.FrameFn()
	if tl.FirstView.IsZero() {
		t.Fatal("building the first frame's view was not timed")
	}
	if !tl.FirstFrame.IsZero() || len(*reports) != 0 {
		t.Fatalf("the first frame was reported presented while it was still being built")
	}

	// Frame 2 starts only after frame 1 was presented.
	w.FrameFn()
	if len(*reports) != 1 {
		t.Fatalf("after the second frame began, %d reports; want 1", len(*reports))
	}
	got := (*reports)[0]
	stages := []time.Time{got.Main, got.WindowCreated, got.FirstView, got.FirstFrame}
	if !slices.IsSortedFunc(stages, time.Time.Compare) || slices.ContainsFunc(stages, time.Time.IsZero) {
		t.Errorf("stages missing or out of order: %+v", got)
	}
	if got != *tl {
		t.Errorf("reported %+v, but the timeline holds %+v", got, *tl)
	}
}

func TestLaterFramesDoNotReportAgain(t *testing.T) {
	w, tl, reports := timedWindow(t)
	w.FrameFn()
	w.FrameFn()
	first := tl.FirstFrame

	// A message forces a rebuild, the way a pack arriving would.
	Post(w, app.LogFound{Path: "/logs/Player.log"})
	for range 3 {
		w.FrameFn()
	}
	if len(*reports) != 1 {
		t.Errorf("%d reports after five frames; want exactly 1", len(*reports))
	}
	if tl.FirstFrame != first {
		t.Errorf("first frame moved from %v to %v on a later frame", first, tl.FirstFrame)
	}
}

func TestATimedWindowShowsTheSameFirstPaintAsAnUntimedOne(t *testing.T) {
	w, _, _ := timedWindow(t)
	w.FrameFn()
	got := texts(w.TestRender(nil))
	_, want := render(t, app.New(), defaultWidth, defaultHeight)
	if !slices.Equal(got, want) {
		t.Errorf("timed first paint shows %q, untimed %q", got, want)
	}
}

func TestTimingWithNobodyToTellIsHarmless(t *testing.T) {
	tl := startup.Timeline{}
	w := gui.NewTestWindow(t, windowCfg(t.Context(), app.New(), Options{Startup: &tl}))
	w.FrameFn()
	w.FrameFn()
	if tl.FirstFrame.IsZero() {
		t.Error("the timeline was not filled in when no one asked to be told")
	}
}

func TestWithTimingOffNothingIsRecorded(t *testing.T) {
	// The default launch: the clock is never read, and a timeline that
	// happens to exist is not touched.
	reads := 0
	opts := Options{now: func() time.Time { reads++; return time.Time{} }}
	w := gui.NewTestWindow(t, windowCfg(t.Context(), app.New(), opts))
	w.FrameFn()
	w.FrameFn()
	if reads != 0 {
		t.Errorf("the clock was read %d times with timing off", reads)
	}
}
