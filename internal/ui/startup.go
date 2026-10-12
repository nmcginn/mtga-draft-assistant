package ui

import (
	"time"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/background"
	"github.com/nmcginn/mtga-draft-assistant/internal/startup"
)

// Options are the ways Run can be asked to behave differently from a plain
// launch. The zero value is a plain launch.
type Options struct {
	// Startup, when set, is filled in as the window comes up, and
	// OnFirstFrame is called with it, on the UI thread, once the first frame
	// has been presented. Left nil, nothing is timed: the window is built
	// exactly as it would be without the option (decision 19).
	Startup      *startup.Timeline
	OnFirstFrame func(startup.Timeline)

	// Start launches the background tasks once the window exists, with a
	// context that ends when the window closes. Nil starts nothing.
	Start background.Start

	// now is the clock, swapped for a fake in tests.
	now func() time.Time
}

func (o Options) clock() func() time.Time {
	if o.now != nil {
		return o.now
	}
	return time.Now
}

// timeFirstFrame wraps view so the first time it is built, the timeline
// records it, and the frame after records that the first one was presented.
//
// go-gui has no "frame presented" callback, so this leans on the order of
// its frame loop, the same on every backend: run queued commands, build the
// view if it changed, connect the accessibility bridge (first frame only),
// draw, present. A command queued while a frame is being built is not run
// by that frame; it waits for the next one, which starts only after this
// one has been handed to the display. Building the view is also proof that
// this frame will be drawn: an occluded window skips the build, not just
// the draw, so a hidden first frame is not mistaken for a presented one.
func timeFirstFrame(view func(*gui.Window) gui.View, t *startup.Timeline, now func() time.Time, done func(startup.Timeline)) func(*gui.Window) gui.View {
	built := false
	return func(w *gui.Window) gui.View {
		if !built {
			built = true
			t.FirstView = now()
			w.QueueCommand(func(*gui.Window) {
				t.FirstFrame = now()
				if done != nil {
					done(*t)
				}
			})
		}
		return view(w)
	}
}
