package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// tall is a window tall enough that the whole deck section is on screen,
// so a test can click anywhere in it: a click only lands on what is
// visible, as it would for the user.
const tall = 4000

// building is the real window, headless, on finishedPool's deck, sized to
// show all of it.
func building(t *testing.T) *gui.Window {
	t.Helper()
	w, _ := render(t, finishedPool(), defaultWidth, tall)
	return w
}

// model is the window's state as it stands.
func model(w *gui.Window) app.Model {
	return gui.State[state](w).model
}

// click clicks the element id, failing the test if nothing took it.
func click(t *testing.T, w *gui.Window, id string) []string {
	t.Helper()
	w.TestRender(nil)
	// A button is addressed by its path under the containers that hold
	// it; the leaf names it uniquely in this window.
	ids := w.ResolveID(id)
	if len(ids) != 1 {
		t.Fatalf("%s resolves to %q, want one widget", id, ids)
	}
	if err := w.TestClick(ids[0]); err != nil {
		t.Fatalf("clicking %s: %v", id, err)
	}
	return texts(w.TestRender(nil))
}

func TestAFinishedDraftShowsADeckToBuildUnderThePool(t *testing.T) {
	_, got := render(t, finishedPool(), defaultWidth, defaultHeight)
	want := []string{
		"Your pool (10 cards)", "Lands (1)", "Card 106439",
		"Your deck (24 cards)", "W", "U", "B", "R", "G",
		"16 short of 40: add cards from the sideboard, or basic lands.", "Copy for Arena",
		"Creatures (6)", "Thornwood Scout · G", "2× Gilded Pathfinder · 1G",
		"Other spells (1)", "Gale Lantern · 2",
		"Lands (17)", "−", "+", "7 Swamp", "−", "+", "10 Forest",
		"Sideboard (2)", "Ember Litany · XR", "Mossback Pathway",
		"1 card of the pool can't be built with until its details are known.",
	}
	last := -1
	for _, s := range want {
		i := slices.Index(got[last+1:], s)
		if i < 0 {
			t.Errorf("window shows %q, missing %q after what comes before it", got, s)
			continue
		}
		last += i + 1
	}
}

func TestThereIsNoDeckWhileTheDraftIsOn(t *testing.T) {
	_, got := render(t, threePicks(), defaultWidth, defaultHeight)
	if slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "Your deck") }) || slices.Contains(got, "Copy for Arena") {
		t.Errorf("window shows %q, with a deck mid-draft", got)
	}
}

func TestClickingAColourBuildsInIt(t *testing.T) {
	w := building(t)
	got := click(t, w, "deck-colour-R")
	if !slices.Contains(got, "Ember Litany · XR") || !slices.Contains(got, "Your deck (25 cards)") {
		t.Errorf("after choosing red the window shows %q, want the red sorcery in a 25-card deck", got)
	}
	if b := model(w).Build; !b.Chosen || !b.Colours[cards.Red] {
		t.Errorf("build %+v, want red chosen", b)
	}
	got = click(t, w, "deck-colour-R")
	if !slices.Contains(got, "Your deck (24 cards)") {
		t.Errorf("after dropping red again the window shows %q", got)
	}
}

func TestClickingACardMovesItBetweenTheDeckAndTheSideboard(t *testing.T) {
	w := building(t)
	// The first creature, up the curve, is the green one-drop.
	got := click(t, w, "deck-group-0-0")
	if !slices.Contains(got, "Creatures (5)") || !slices.Contains(got, "Sideboard (3)") {
		t.Fatalf("after moving the scout out the window shows %q", got)
	}
	if side := find(w.TestRender(nil), "deck-side-0-label"); side == nil || side.TC.Text != "Thornwood Scout · G" {
		t.Errorf("the sideboard does not lead with the scout, castable in the deck's colours")
	}
	got = click(t, w, "deck-side-0")
	if !slices.Contains(got, "Creatures (6)") || !slices.Contains(got, "Sideboard (2)") {
		t.Errorf("after moving it back the window shows %q", got)
	}
}

func TestBasicLandsAreAddedAndTakenOutOneAtATime(t *testing.T) {
	w := building(t)
	if got := click(t, w, "deck-basic-G-more"); !slices.Contains(got, "11 Forest") || !slices.Contains(got, "Your deck (25 cards)") {
		t.Errorf("after adding a forest the window shows %q", got)
	}
	if got := click(t, w, "deck-basic-B-less"); !slices.Contains(got, "6 Swamp") || !slices.Contains(got, "Lands (17)") {
		t.Errorf("after taking out a swamp the window shows %q", got)
	}
}

func TestCopyingPutsTheDeckOnTheClipboardInArenasFormat(t *testing.T) {
	w := building(t)
	var clipboard []string
	w.SetClipboardFn(func(s string) { clipboard = append(clipboard, s) })
	want := model(w).DraftScreen().Deck.Export
	got := click(t, w, "deck-copy")
	if len(clipboard) != 1 || clipboard[0] != want {
		t.Fatalf("clipboard %q, want the export once:\n%s", clipboard, want)
	}
	if !strings.HasPrefix(want, "Deck\n1 Thornwood Scout\n") || !strings.Contains(want, "\nSideboard\n") {
		t.Errorf("export\n%s\nis not in Arena's format", want)
	}
	if !slices.Contains(got, "Copied 24 cards in Arena's deck-import format, with the rest of the pool as the sideboard.") {
		t.Errorf("after copying the window shows %q, with no confirmation", got)
	}
	got = click(t, w, "deck-basic-G-more")
	if slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "Copied") }) {
		t.Errorf("after a change the window still shows %q", got)
	}
	click(t, w, "deck-copy")
	if len(clipboard) != 2 || !strings.Contains(clipboard[1], "11 Forest") {
		t.Errorf("the second copy %q is not the deck as changed", clipboard[1:])
	}
}

func TestTheDeckIsPaintedFromTheTheme(t *testing.T) {
	custom := Dark
	custom.Background = gui.Hex(0x010203)
	custom.Control = gui.Hex(0x111111)
	custom.Hover = gui.Hex(0x222222)
	custom.Chosen = gui.Hex(0x333333)
	custom.Text = gui.Hex(0x444444)
	w := gui.NewTestWindow(t, gui.WindowCfg{State: &state{model: finishedPool(), theme: custom}, Height: tall})
	ly := w.TestRender(mainView)
	for id, want := range map[string]gui.Color{
		"deck-colour-B": custom.Chosen, "deck-colour-G": custom.Chosen, "deck-colour-W": custom.Control,
		"deck-copy": custom.Control, "deck-basic-G-more": custom.Control,
		"deck-group-0-0": custom.Background, "deck-side-0": custom.Background,
	} {
		if s := find(ly, id); s == nil || s.Color != want {
			t.Errorf("%s is not in the theme's colour", id)
		}
	}
	for _, id := range []string{"deck-colour-W-label", "deck-group-0-0-label", "deck-copy-label"} {
		if s := find(ly, id); s == nil || s.TC == nil || s.TC.TextStyle.Color != custom.Text {
			t.Errorf("%s is not in the theme's text colour", id)
		}
	}
	if s := find(ly, "deck-curve-1-bar"); s == nil || s.Color != custom.Bar {
		t.Error("the deck's curve is not in the theme's bar colour")
	}
}

func TestTheDeckFitsATinyWindowWithoutLosingCards(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {minWidth, minHeight}, {1600, 900}} {
		_, got := render(t, finishedPool(), size[0], size[1])
		for _, want := range []string{"Your deck (24 cards)", "Copy for Arena", "Verdant Oathkeeper · 2GG", "10 Forest", "Mossback Pathway"} {
			if !slices.Contains(got, want) {
				t.Errorf("at %dx%d the window shows %q, missing %q", size[0], size[1], got, want)
			}
		}
	}
}
