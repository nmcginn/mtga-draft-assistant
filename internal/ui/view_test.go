package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/background"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch/scryfall"
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

func TestTheWindowSaysWhenItCannotReadTheLogAndWhatToDo(t *testing.T) {
	w, _ := render(t, app.New().Apply(app.LogFound{Path: "/logs/Player.log"}), defaultWidth, defaultHeight)
	Post(w, app.LogUnreadable{Err: errors.New("open /logs/Player.log: permission denied")})
	w.FrameFn()
	got := texts(w.TestRender(nil))
	if !slices.Contains(got, "Can't read Arena's log") {
		t.Errorf("window shows %q, want the unreadable headline", got)
	}
	if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "permission denied") && strings.Contains(s, "-log") }) {
		t.Errorf("window shows %q, want the reason and what to do", got)
	}
}

func TestWhileCatchingUpOnTheLogTheWindowSaysSoThenThatItIsReady(t *testing.T) {
	w, got := render(t, app.New().Apply(app.LogFound{Path: "/logs/Player.log"}), defaultWidth, defaultHeight)
	if !slices.Contains(got, "Reading Arena's log") {
		t.Errorf("before catching up the window shows %q", got)
	}
	Post(w, app.LogCaughtUp{})
	w.FrameFn()
	if got := texts(w.TestRender(nil)); !slices.Contains(got, "Ready for your next draft") {
		t.Errorf("after catching up the window shows %q", got)
	}
}

// drafting is a model following a log, with the draft events given.
func drafting(events ...arena.Event) app.Model {
	return app.New().Apply(app.LogFound{Path: "/logs/Player.log"}).Apply(app.LogCaughtUp{}).
		Apply(app.DraftEvents{Events: events})
}

var (
	premierStart = arena.DraftStarted{Event: "PremierDraft_FRA_20260929", Set: "FRA", Format: arena.PremierDraft}
	firstPack    = arena.PackShown{Pack: 1, Pick: 1, Cards: []int{106325, 106235, 106268, 106407, 106411, 106335, 106385, 106391, 106444, 106321, 106501, 106485, 106453, 106439}}
	// fraCards is FRA's cards as the card task reports them, for the
	// first pack but its last card (106439), which stands for one
	// Scryfall does not know yet. The names are invented, as in
	// testdata/scryfall.
	fraCards = app.CardsLoaded{Set: "FRA", Cards: []cards.Card{
		{ArenaID: 106325, Set: "FRA", Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid", Rarity: "uncommon"},
		{ArenaID: 106235, Set: "FRA", Name: "Thornwood Scout", ManaCost: "{G}", TypeLine: "Creature — Elf Scout", Rarity: "common"},
		{ArenaID: 106268, Set: "FRA", Name: "Ashen Courier", ManaCost: "{1}{B}", TypeLine: "Creature — Human Rogue", Rarity: "common"},
		{ArenaID: 106407, Set: "FRA", Name: "Duskmarch Paladin", ManaCost: "{W/B}{W/B}", TypeLine: "Creature — Human Knight", Rarity: "uncommon"},
		{ArenaID: 106411, Set: "FRA", Name: "Sovereign of the Hollow", ManaCost: "{3}{B}{B}", TypeLine: "Legendary Creature — Vampire Noble", Rarity: "mythic"},
		{ArenaID: 106335, Set: "FRA", Name: "Ember Litany", ManaCost: "{X}{R}", TypeLine: "Sorcery", Rarity: "rare"},
		{ArenaID: 106385, Set: "FRA", Name: "Tidecaller's Riddle", ManaCost: "{1}{U}", TypeLine: "Instant", Rarity: "common"},
		{ArenaID: 106391, Set: "FRA", Name: "Mossback Pathway", TypeLine: "Land", Rarity: "uncommon"},
		{ArenaID: 106444, Set: "FRA", Name: "Lanternwatch Recruit // Lanternwatch Captain", ManaCost: "{1}{W}", TypeLine: "Creature — Human Soldier // Creature — Human Soldier", Rarity: "rare"},
		{ArenaID: 106321, Set: "FRA", Name: "Gale Lantern", ManaCost: "{2}", TypeLine: "Artifact", Rarity: "uncommon"},
		{ArenaID: 106501, Set: "FRA", Name: "Stormglass Adept", ManaCost: "{2}{U}", TypeLine: "Creature — Merfolk Wizard", Rarity: "common"},
		{ArenaID: 106485, Set: "FRA", Name: "Forest", TypeLine: "Basic Land — Forest", Rarity: "common"},
		{ArenaID: 106453, Set: "FRA", Name: "Gilded Pathfinder", ManaCost: "{1}{G}", TypeLine: "Creature — Elf Ranger", Rarity: "common"},
	}}
)

// fullDraft is a model that has read the whole Premier Draft fixture.
func fullDraft(t *testing.T) app.Model {
	t.Helper()
	b, err := os.ReadFile("../../testdata/logs/2026-10-fra-premier-full-draft.log")
	if err != nil {
		t.Fatal(err)
	}
	var p arena.Parser
	var events []arena.Event
	for l := range strings.SplitSeq(string(b), "\n") {
		events = append(events, p.Line(l)...)
	}
	return drafting(events...)
}

func TestThePackIsShownCardByCardWithWhereInTheDraftItIs(t *testing.T) {
	model := drafting(premierStart, firstPack).Apply(fraCards)
	_, got := render(t, model, defaultWidth, defaultHeight)
	for _, want := range []string{"FRA · Premier Draft", "Pack 1 · Pick 1", "14 cards in this pack", "Card details from Scryfall."} {
		if !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
	if i, j := slices.Index(got, "Verdant Oathkeeper"), slices.Index(got, "Card 106439"); i < 0 || j < i {
		t.Errorf("window shows %q, want the pack in Arena's order", got)
	}
	if slices.Contains(got, model.Status().Headline) {
		t.Errorf("window shows %q, the status as well as the draft", got)
	}
}

func TestEachCardShowsItsNameCostTypeAndRarity(t *testing.T) {
	_, got := render(t, drafting(premierStart, firstPack).Apply(fraCards), defaultWidth, defaultHeight)
	for _, want := range []string{
		"Verdant Oathkeeper", "2GG · Creature — Elf Druid · Uncommon",
		"Duskmarch Paladin", "(W/B)(W/B) · Creature — Human Knight · Uncommon",
		"Forest", "Basic Land — Forest · Common",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
	if i := slices.Index(got, "Verdant Oathkeeper"); i < 0 || i+1 >= len(got) || got[i+1] != "2GG · Creature — Elf Druid · Uncommon" {
		t.Errorf("window shows %q, want a card's details straight under its name", got)
	}
}

func TestACardScryfallDoesNotKnowShowsByIDAndSaysSo(t *testing.T) {
	_, got := render(t, drafting(premierStart, firstPack).Apply(fraCards), defaultWidth, defaultHeight)
	i := slices.Index(got, "Card 106439")
	if i < 0 || i+1 >= len(got) || got[i+1] != "Not on Scryfall yet" {
		t.Errorf("window shows %q, want 106439 by id with a note", got)
	}
}

func TestWhileTheSetIsFetchedTheWindowSaysSoAndShowsTheCardsByID(t *testing.T) {
	_, got := render(t, drafting(premierStart, firstPack).Apply(app.CardsFetching{Set: "FRA"}), defaultWidth, defaultHeight)
	if !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "Fetching FRA's cards from Scryfall") }) {
		t.Errorf("window shows %q, want that FRA is being fetched", got)
	}
	if !slices.Contains(got, "Card 106325") || !slices.Contains(got, "Card 106439") {
		t.Errorf("window shows %q, want the pack by id meanwhile", got)
	}
}

func TestOfflineTheWindowSaysWhatFailedAndStillShowsThePack(t *testing.T) {
	model := drafting(premierStart, firstPack).Apply(app.CardsFailed{Set: "FRA", Err: errors.New("can't reach api.scryfall.com"), RetryIn: time.Minute})
	_, got := render(t, model, defaultWidth, defaultHeight)
	want := "Couldn't get FRA's cards from Scryfall (can't reach api.scryfall.com). Check your internet connection; " +
		"the assistant tries again in 1 minute, and shows cards by their Arena id until then."
	if !slices.Contains(got, want) {
		t.Errorf("window shows %q, want %q", got, want)
	}
	if !slices.Contains(got, "Card 106325") || !slices.Contains(got, "14 cards in this pack") {
		t.Errorf("window shows %q, want the pack by id", got)
	}
}

func TestTheSetArrivingPutsNamesOnScreenTheFrameAfter(t *testing.T) {
	w, _ := render(t, drafting(premierStart, firstPack).Apply(app.CardsFetching{Set: "FRA"}), defaultWidth, defaultHeight)
	w.FrameFn()
	Post(w, fraCards)
	if !w.FrameFn() {
		t.Error("the frame after the set arrived did not rebuild the view")
	}
	got := texts(w.TestRender(nil))
	if !slices.Contains(got, "Verdant Oathkeeper") || slices.Contains(got, "Card 106325") {
		t.Errorf("after the set arrived the window shows %q", got)
	}
	if slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "Fetching") }) {
		t.Errorf("after the set arrived the window still says it is fetching: %q", got)
	}
}

func TestAnUnusableCacheIsNotedWithWhatToDo(t *testing.T) {
	model := drafting(premierStart, firstPack).Apply(fraCards).
		Apply(app.CacheUnavailable{Dir: "/home/me/.cache/mtga-draft-assistant", Err: errors.New("permission denied")})
	_, got := render(t, model, defaultWidth, defaultHeight)
	if !slices.Contains(got, model.DraftScreen().Footnote) || !strings.Contains(model.DraftScreen().Footnote, "permission denied") {
		t.Errorf("window shows %q, want the cache problem in the footnote", got)
	}
	if !slices.Contains(got, "Verdant Oathkeeper") {
		t.Errorf("window shows %q, want the cards still named", got)
	}
}

func TestAfterAPickTheWindowSaysItIsWaitingAndListsThePick(t *testing.T) {
	_, got := render(t, drafting(premierStart, firstPack, arena.PickMade{Pack: 1, Pick: 1, Card: 106453}).Apply(fraCards), defaultWidth, defaultHeight)
	for _, want := range []string{"You took Gilded Pathfinder. Waiting for the next pack.", "Your picks (1)", "Pack 1: Gilded Pathfinder"} {
		if !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
	if slices.Contains(got, "Verdant Oathkeeper") {
		t.Errorf("window shows %q, still listing the pack picked from", got)
	}
}

func TestAFinishedDraftShowsThePoolByPack(t *testing.T) {
	model := fullDraft(t)
	_, got := render(t, model, defaultWidth, defaultHeight)
	s := model.DraftScreen()
	for _, want := range append([]string{"Draft complete", "Your pool (42 cards)", s.Heading}, s.Picks...) {
		if !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
}

func TestAPackArrivingIsOnScreenTheFrameAfterItIsPosted(t *testing.T) {
	w, _ := render(t, drafting(premierStart), defaultWidth, defaultHeight)
	w.FrameFn()
	Post(w, app.DraftEvents{Events: []arena.Event{firstPack}})
	if !w.FrameFn() {
		t.Error("the frame after a pack arrived did not rebuild the view")
	}
	if got := texts(w.TestRender(nil)); !slices.Contains(got, "Card 106325") {
		t.Errorf("after the pack arrived the window shows %q", got)
	}
}

func TestTheDraftFitsTinyAndNarrowWindowsWithoutLosingCards(t *testing.T) {
	// A whole pack and three packs of picks are taller than the window; the
	// view scrolls rather than dropping them.
	model := drafting(premierStart, firstPack).Apply(fraCards)
	for _, size := range [][2]int{{1, 1}, {minWidth, minHeight}} {
		_, got := render(t, model, size[0], size[1])
		for _, want := range []string{"FRA · Premier Draft", "Verdant Oathkeeper", "2GG · Creature — Elf Druid · Uncommon", "Card 106439"} {
			if !slices.Contains(got, want) {
				t.Errorf("at %dx%d the window shows %q, missing %q", size[0], size[1], got, want)
			}
		}
	}
	_, got := render(t, fullDraft(t), minWidth, minHeight)
	if !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "Pack 3: ") }) {
		t.Errorf("at %dx%d a finished draft shows %q, missing its last pack", minWidth, minHeight, got)
	}
}

// find is the shape with id in the layout tree, or nil.
func find(ly *gui.Layout, id string) *gui.Shape {
	if ly.Shape != nil && ly.Shape.ID == id {
		return ly.Shape
	}
	for i := range ly.Children {
		if s := find(&ly.Children[i], id); s != nil {
			return s
		}
	}
	return nil
}

func TestTheDraftViewTakesItsColoursFromTheTheme(t *testing.T) {
	custom := Dark
	custom.Background = gui.Hex(0x123456)
	custom.Text = gui.Hex(0x654321)
	custom.TextMuted = gui.Hex(0x112233)
	// Named cards, with their details and the footnote; and the same pack
	// while its set is fetched, with the notice saying so.
	for model, ids := range map[*app.Model]map[string]gui.Color{
		new(drafting(premierStart, firstPack).Apply(fraCards)): {
			"draft-title": custom.Text, "draft-card-0": custom.Text, "draft-card-0-detail": custom.TextMuted,
			"draft-heading": custom.TextMuted, "draft-footnote": custom.TextMuted,
		},
		new(drafting(premierStart, firstPack).Apply(app.CardsFetching{Set: "FRA"})): {
			"draft-notice": custom.TextMuted, "draft-card-0": custom.Text,
		},
	} {
		w := gui.NewTestWindow(t, gui.WindowCfg{State: &state{model: *model, theme: custom}})
		ly := w.TestRender(mainView)
		if s := find(ly, "draft"); s == nil || s.Color != custom.Background {
			t.Error("the draft view did not paint the theme's background")
		}
		for id, want := range ids {
			if s := find(ly, id); s == nil || s.TC == nil || s.TC.TextStyle.Color != want {
				t.Errorf("%s is not in the theme's colour", id)
			}
		}
	}
}

func TestAFailureWhileDraftingReplacesTheDraft(t *testing.T) {
	w, _ := render(t, drafting(premierStart, firstPack), defaultWidth, defaultHeight)
	Post(w, app.TaskFailed{Task: "Watching Arena's log", Err: errors.New("boom")})
	w.FrameFn()
	got := texts(w.TestRender(nil))
	if !slices.Contains(got, "Something went wrong") || slices.Contains(got, "Card 106325") {
		t.Errorf("window shows %q, want the failure and not the stale pack", got)
	}
}

func TestTheRealLogTaskPutsAFixturesDraftOnScreen(t *testing.T) {
	// End to end, without a display: -log naming the fixture, as the smoke
	// job's second launch does.
	model, start, err := background.FindLog(background.LogSetup{
		Flag: "../../testdata/logs/2026-10-fra-quick-full-draft.log", GOOS: "linux", Home: t.TempDir(),
		Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := windowCfg(t.Context(), model, Options{Start: start})
	got := framesUntil(t, gui.NewTestWindow(t, cfg), "Draft complete")
	if !slices.Contains(got, "FRA · Quick Draft") || !slices.Contains(got, "Your pool (42 cards)") {
		t.Errorf("window shows %q, want the quick draft's pool", got)
	}
}

func TestADraftTallerThanTheWindowScrolls(t *testing.T) {
	model := drafting(premierStart, firstPack, arena.PickMade{Pack: 1, Pick: 1, Card: 106453},
		arena.PackShown{Pack: 1, Pick: 2, Cards: firstPack.Cards[1:]})
	w, _ := render(t, model, minWidth, minHeight)
	if err := w.TestScroll("draft", 0, -200); err != nil {
		t.Fatal(err)
	}
	w.FrameFn()
	if _, y, err := w.TestScrollOffset("draft"); err != nil || y == 0 {
		t.Errorf("after scrolling, offset %v (%v); want the view moved", y, err)
	}
}

func TestTheRealTasksPutAFixturesDraftOnScreenWithCardNamesFromScryfall(t *testing.T) {
	// End to end, without a display or the network: -log naming the
	// Premier Draft fixture, and the card task fetching FRA from an
	// httptest server serving testdata/scryfall into a fresh cache.
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := "fra-search-page1.json"
		if r.URL.Query().Get("page") == "2" {
			page = "fra-search-page2.json"
		}
		b, err := os.ReadFile("../../testdata/scryfall/" + page)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(string(b), scryfall.BaseURL, srv.URL)))
	}))
	defer srv.Close()
	cardTask := background.NewCards(background.CardSetup{
		Dir: t.TempDir(),
		Source: scryfall.Source{BaseURL: srv.URL, Client: fetch.New(fetch.Options{
			UserAgent: "mtga-draft-assistant/test", Transport: srv.Client().Transport,
			Limits: map[string]time.Duration{"127.0.0.1": time.Millisecond},
		})},
	})
	model, start, err := background.FindLog(background.LogSetup{
		Flag: "../../testdata/logs/2026-10-fra-premier-full-draft.log", GOOS: "linux", Home: t.TempDir(),
		Getenv: func(string) string { return "" }, WantSet: cardTask.Want,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := gui.NewTestWindow(t, windowCfg(t.Context(), model, Options{Start: background.All(cardTask.Start, start)}))
	deadline := time.Now().Add(5 * time.Second)
	for {
		w.FrameFn()
		got := texts(w.TestRender(nil))
		// The fixture's first pick is 106453, which testdata/scryfall names.
		if slices.Contains(got, "Card details from Scryfall.") &&
			slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "Pack 1: Gilded Pathfinder, ") }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 5s the window shows %q, never the pool by name", got)
		}
		time.Sleep(time.Millisecond)
	}
}
