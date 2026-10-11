package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
)

// render builds the real window headless, at the given size, and returns the
// text that landed on screen in reading order.
func render(t *testing.T, model app.Model, width, height int) (*gui.Window, []string) {
	t.Helper()
	cfg := windowCfg(model, Options{})
	cfg.Width, cfg.Height = width, height
	w := gui.NewTestWindow(t, cfg)
	return w, texts(w.TestRender(nil))
}

// texts collects every piece of text in the layout tree, depth first.
func texts(ly *gui.Layout) []string {
	var out []string
	var walk func(*gui.Layout)
	walk = func(l *gui.Layout) {
		if l.Shape != nil && l.Shape.TC != nil && l.Shape.TC.Text != "" {
			out = append(out, l.Shape.TC.Text)
		}
		for i := range l.Children {
			walk(&l.Children[i])
		}
	}
	walk(ly)
	return out
}

func TestFirstPaintSaysWhatToDoBeforeAnyLogIsFound(t *testing.T) {
	_, got := render(t, app.New(), defaultWidth, defaultHeight)
	want := app.New().Status()
	if !slices.Contains(got, want.Headline) || !slices.Contains(got, want.Detail) {
		t.Errorf("first paint shows %q, want the headline %q and detail %q", got, want.Headline, want.Detail)
	}
}

func TestAPostedMessageChangesWhatIsOnScreenAfterTheNextFrame(t *testing.T) {
	w, _ := render(t, app.New(), defaultWidth, defaultHeight)
	Post(w, app.LogFound{Path: "/logs/Player.log"})
	w.FrameFn() // what the backend runs each frame: drain commands, rebuild
	got := texts(w.TestRender(nil))
	if want := (app.Model{Phase: app.PhaseFollowing}).Status().Headline; !slices.Contains(got, want) {
		t.Errorf("after LogFound the window shows %q, want %q", got, want)
	}
}

func TestATinyWindowStillRendersWithoutPanicking(t *testing.T) {
	// The OS enforces MinWidth on a resize drag, but a restored or tiled
	// window can still arrive smaller; layout must cope rather than crash.
	_, got := render(t, app.New(), 1, 1)
	if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "Arena") }) {
		t.Errorf("1x1 window shows %q, want the status text still in the tree", got)
	}
}

func TestEveryViewColourComesFromTheTheme(t *testing.T) {
	// A cheap guard on decision 7: the status view's background is the
	// theme's, so swapping the theme swaps the paint.
	custom := Dark
	custom.Background = gui.Hex(0x123456)
	w := gui.NewTestWindow(t, gui.WindowCfg{State: &state{model: app.New(), theme: custom}})
	ly := w.TestRender(mainView)
	var found bool
	var walk func(*gui.Layout)
	walk = func(l *gui.Layout) {
		if l.Shape != nil && l.Shape.ID == "status" {
			found = l.Shape.Color == custom.Background
		}
		for i := range l.Children {
			walk(&l.Children[i])
		}
	}
	walk(ly)
	if !found {
		t.Error("the status view did not paint the theme's background")
	}
}
