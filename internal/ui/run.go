package ui

import (
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
// build the same window headless.
func windowCfg(model app.Model) gui.WindowCfg {
	return gui.WindowCfg{
		State:     newState(model),
		Title:     "MTGA Draft Assistant",
		Width:     defaultWidth,
		Height:    defaultHeight,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		OnInit:    func(w *gui.Window) { w.SetView(mainView) },
	}
}

// Run opens the window and blocks until it is closed. It is the only function
// in the program that talks to the window system, and it does nothing before
// the first frame that could wait on disk or network: whatever the first
// frame needs is already in model.
func Run(model app.Model) {
	backend.Run(gui.NewWindow(windowCfg(model)))
}
