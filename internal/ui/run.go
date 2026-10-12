package ui

import (
	"context"

	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
)

// Window size on first launch: tall and narrow, to sit beside Arena rather
// than over it.
const (
	defaultWidth  = 420
	defaultHeight = 760
	minWidth      = 320
	minHeight     = 400
)

// windowCfg is the window's configuration, separate from Run so a test can
// build the same window headless. ctx ends the background tasks.
func windowCfg(ctx context.Context, model app.Model, opts Options) gui.WindowCfg {
	view := mainView
	now := opts.clock()
	if opts.Startup != nil {
		view = timeFirstFrame(mainView, opts.Startup, now, opts.OnFirstFrame)
	}
	return gui.WindowCfg{
		State:     newState(model),
		Title:     "MTGA Draft Assistant",
		Width:     defaultWidth,
		Height:    defaultHeight,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		OnInit: func(w *gui.Window) {
			// The backend calls OnInit once the native window and its GPU
			// context exist, and before anything is drawn.
			if opts.Startup != nil {
				opts.Startup.WindowCreated = now()
			}
			w.SetView(view)
			// Started here, not before, because they report to this
			// window; and they only start goroutines, so the first frame
			// does not wait on them.
			if opts.Start != nil {
				opts.Start(ctx, func(msg app.Msg) { Post(w, msg) })
			}
		},
	}
}

// Run opens the window and blocks until it is closed. It is the only function
// in the program that talks to the window system, and it does nothing before
// the first frame that could wait on disk or network: whatever the first
// frame needs is already in model.
func Run(model app.Model, opts Options) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backend.Run(gui.NewWindow(windowCfg(ctx, model, opts)))
}
