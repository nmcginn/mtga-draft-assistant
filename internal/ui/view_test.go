package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/background"
)

// render builds the real window headless, at the given size, and returns the
// text that landed on screen in reading order.
func render(t *testing.T, model app.Model, width, height int) (*gui.Window, []string) {
	t.Helper()
	cfg := windowCfg(t.Context(), model, Options{})
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

func TestAPostedMessageRedrawsTheWindowOnItsOwn(t *testing.T) {
	// The backend only presents a frame FrameFn reports as rebuilt, and
	// running a queued command does not by itself mark the view stale. So
	// a message must ask for the redraw, or the window would go on showing
	// the old state until the user happened to move the mouse over it.
	w, _ := render(t, app.New(), defaultWidth, defaultHeight)
	w.FrameFn()
	if w.FrameFn() {
		t.Fatal("an idle frame rebuilt the view; the test cannot tell a post's redraw apart")
	}
	Post(w, app.LogSearched{Looked: places})
	if !w.FrameFn() {
		t.Error("the frame after a posted message did not rebuild the view")
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

// framesUntil runs frames, the way the backend does when a goroutine wakes
// it, until the window shows want or five seconds pass.
func framesUntil(t *testing.T, w *gui.Window, want string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w.FrameFn()
		got := texts(w.TestRender(nil))
		if slices.Contains(got, want) {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 5s the window shows %q, never %q", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

var places = []string{
	"Wine: ~/.wine",
	"Steam (Flatpak): ~/.var/app/com.valvesoftware.Steam/.local/share/Steam/steamapps/compatdata/2141910/pfx",
}

func TestTheWaitingWindowListsWhereItLookedAndHowToPassTheLog(t *testing.T) {
	model := app.New().Apply(app.LogSearched{Looked: places})
	_, got := render(t, model, defaultWidth, defaultHeight)
	for _, want := range append([]string{model.Status().Footnote}, places...) {
		if !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
}

func TestPlacesAreShownInTheOrderSearched(t *testing.T) {
	_, got := render(t, app.New().Apply(app.LogSearched{Looked: places}), defaultWidth, defaultHeight)
	first, second := slices.Index(got, places[0]), slices.Index(got, places[1])
	if first < 0 || second < first {
		t.Errorf("window shows %q, want %q before %q", got, places[0], places[1])
	}
}

func TestFirstPaintListsNoPlacesBeforeTheSearchHasLooked(t *testing.T) {
	_, got := render(t, app.New(), defaultWidth, defaultHeight)
	if slices.Contains(got, "Looked for Player.log in:") {
		t.Errorf("first paint shows %q, claiming a search that has not happened", got)
	}
}

func TestAWindowAtMinimumWidthStillShowsEveryPlace(t *testing.T) {
	// The paths are longer than the window is wide; they wrap rather than
	// drop out of the layout.
	_, got := render(t, app.New().Apply(app.LogSearched{Looked: places}), minWidth, minHeight)
	for _, p := range places {
		if !slices.Contains(got, p) {
			t.Errorf("at %dx%d the window shows %q, missing %q", minWidth, minHeight, got, p)
		}
	}
}

func TestATinyWindowWithPlacesStillRenders(t *testing.T) {
	_, got := render(t, app.New().Apply(app.LogSearched{Looked: places}), 1, 1)
	if !slices.Contains(got, places[0]) {
		t.Errorf("1x1 window shows %q, want the places still in the tree", got)
	}
}

func TestTheFollowingWindowSaysWhichLogItIsReading(t *testing.T) {
	_, got := render(t, app.New().Apply(app.LogFound{Path: "/logs/Player.log"}), defaultWidth, defaultHeight)
	if !slices.Contains(got, "Reading /logs/Player.log") {
		t.Errorf("window shows %q, want the path being read", got)
	}
}

func TestAFailureIsShownWithWhatToDo(t *testing.T) {
	model := app.New().Apply(app.TaskFailed{Task: "Looking for Arena's log", Err: errors.New("boom")})
	_, got := render(t, model, defaultWidth, defaultHeight)
	st := model.Status()
	for _, want := range []string{st.Headline, st.Detail, st.Footnote} {
		if !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
}

func startWith(t *testing.T, start background.Start) *gui.Window {
	t.Helper()
	cfg := windowCfg(t.Context(), app.New(), Options{Start: start})
	cfg.Width, cfg.Height = defaultWidth, defaultHeight
	return gui.NewTestWindow(t, cfg)
}

func TestBackgroundTasksStartWithTheWindowAndReportToIt(t *testing.T) {
	w := startWith(t, func(_ context.Context, post background.Post) {
		background.Go("test", post, func() { post(app.LogSearched{Looked: places}) })
	})
	framesUntil(t, w, places[0])
}

func TestAPanicInABackgroundTaskShowsInTheWindowInsteadOfClosingIt(t *testing.T) {
	w := startWith(t, func(_ context.Context, post background.Post) {
		background.Go("Looking for Arena's log", post, func() {
			var s []int
			_ = s[3]
		})
	})
	got := framesUntil(t, w, "Something went wrong")
	if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "Looking for Arena's log") }) {
		t.Errorf("window shows %q, want the stopped task named", got)
	}
}

func TestBackgroundTasksAreToldWhenTheWindowsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	cfg := windowCfg(ctx, app.New(), Options{Start: func(ctx context.Context, post background.Post) {
		background.Go("test", post, func() { <-ctx.Done(); close(stopped) })
	}})
	gui.NewTestWindow(t, cfg)
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the task did not see its context end")
	}
}

func TestTheRealSearchReachesTheWindow(t *testing.T) {
	// End to end, without a display: the same FindLog main uses, on an empty
	// home, shows where it looked.
	_, start, err := background.FindLog(background.LogSetup{
		GOOS: "linux", Home: t.TempDir(), Getenv: func(string) string { return "" }, Every: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	framesUntil(t, startWith(t, start), "Wine: ~/.wine")
}
