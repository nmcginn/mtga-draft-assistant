package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// threePicks is the first pack shown, three of its cards taken over the
// first picks of the pack, and the second pack's first pick showing: the
// two-mana elf, the four-mana elf, and the land.
func threePicks() app.Model {
	return drafting(premierStart,
		arena.PickMade{Pack: 1, Pick: 1, Card: 106453},
		arena.PickMade{Pack: 1, Pick: 2, Card: 106325},
		arena.PickMade{Pack: 1, Pick: 3, Card: 106391},
		arena.PackShown{Pack: 1, Pick: 4, Cards: firstPack.Cards},
	).Apply(fraCards)
}

func TestThePicksAreCountedUpOverTheListOfThem(t *testing.T) {
	_, got := render(t, threePicks(), defaultWidth, defaultHeight)
	for _, want := range []string{"Your picks (3)", "G 2", "2 creatures · 0 other spells · 1 land", "Pack 1: Gilded Pathfinder, Verdant Oathkeeper, Mossback Pathway"} {
		if !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
	if i, j := slices.Index(got, "G 2"), slices.Index(got, "Pack 1: Gilded Pathfinder, Verdant Oathkeeper, Mossback Pathway"); i > j {
		t.Errorf("window shows %q, want the counts above the list", got)
	}
}

func TestTheCurveHasACountAndALabelForEachManaValue(t *testing.T) {
	w, _ := render(t, threePicks(), defaultWidth, defaultHeight)
	ly := w.TestRender(nil)
	for i, want := range [][2]string{{"0", "1"}, {"1", "2"}, {"0", "3"}, {"1", "4"}, {"0", "5"}, {"0", "6+"}} {
		count, label := find(ly, curveID(i)+"-count"), find(ly, curveID(i)+"-label")
		if count == nil || label == nil || count.TC.Text != want[0] || label.TC.Text != want[1] {
			t.Errorf("curve column %d is not %s at %s", i, want[0], want[1])
		}
	}
}

// curveID is the ID of the curve's column i, counting from the left.
func curveID(i int) string {
	return fmt.Sprintf("draft-curve-%d", i)
}

func TestTheTallestCurveColumnIsFullHeightAndAnEmptyOneHasNoColumn(t *testing.T) {
	// One two-drop, two four-drops (bar heights are set by the view, not
	// measured from text, so they are safe to compare headless).
	model := drafting(premierStart,
		arena.PickMade{Pack: 1, Pick: 1, Card: 106453},
		arena.PickMade{Pack: 1, Pick: 2, Card: 106325},
		arena.PickMade{Pack: 1, Pick: 3, Card: 106411},
	).Apply(fraCards)
	w, _ := render(t, model, defaultWidth, defaultHeight)
	ly := w.TestRender(nil)
	// Columns: 1, 2, 3, 4, 5, 6+.
	two, four, five := find(ly, curveID(1)+"-bar"), find(ly, curveID(3)+"-bar"), find(ly, curveID(4)+"-bar")
	if two == nil || four == nil || five == nil {
		t.Fatal("a mana value with spells has no column")
	}
	if four.Height != curveHeight || five.Height != curveHeight || two.Height != curveHeight {
		t.Errorf("heights %v, %v, %v; want each at the full %d, being level", two.Height, four.Height, five.Height, curveHeight)
	}
	if find(ly, curveID(0)+"-bar") != nil {
		t.Error("the one-drop column has a bar with no one-drops")
	}

	model = drafting(premierStart,
		arena.PickMade{Pack: 1, Pick: 1, Card: 106453},
		arena.PickMade{Pack: 1, Pick: 2, Card: 106268},
		arena.PickMade{Pack: 1, Pick: 3, Card: 106325},
	).Apply(fraCards)
	w, _ = render(t, model, defaultWidth, defaultHeight)
	ly = w.TestRender(nil)
	if two, four := find(ly, curveID(1)+"-bar"), find(ly, curveID(3)+"-bar"); two == nil || four == nil || two.Height != curveHeight || four.Height != curveHeight/2 {
		t.Errorf("two two-drops and one four-drop: want the four-drop's column half the two-drops'")
	}
}

func TestTheCurveIsPaintedInTheThemesBarColour(t *testing.T) {
	custom := Dark
	custom.Bar = gui.Hex(0x13579b)
	w := gui.NewTestWindow(t, gui.WindowCfg{State: &state{model: threePicks(), theme: custom}})
	ly := w.TestRender(mainView)
	if s := find(ly, curveID(1)+"-bar"); s == nil || s.Color != custom.Bar {
		t.Error("the curve's column is not in the theme's bar colour")
	}
	for _, id := range []string{"draft-summary-colours", "draft-summary-kinds"} {
		if s := find(ly, id); s == nil || s.TC == nil || s.TC.TextStyle.Color != custom.Text {
			t.Errorf("%s is not in the theme's text colour", id)
		}
	}
}

func TestPicksSitUnderThePackInANarrowWindowAndBesideItInAWideOne(t *testing.T) {
	model := threePicks()
	for _, width := range []int{1, minWidth, defaultWidth, besideWidth - 1} {
		w, _ := render(t, model, width, defaultHeight)
		ly := w.TestRender(nil)
		if find(ly, "draft-beside") != nil || find(ly, "draft-picks-panel") != nil {
			t.Errorf("at %dpx the picks are beside the pack; want them under it", width)
		}
		if find(ly, "draft-picks") == nil {
			t.Errorf("at %dpx the picks are not on screen", width)
		}
	}
	for _, width := range []int{besideWidth, 1200} {
		w, got := render(t, model, width, defaultHeight)
		ly := w.TestRender(nil)
		row := find(ly, "draft-beside")
		if row == nil || find(ly, "draft-picks-panel") == nil || find(ly, "draft-pack") == nil {
			t.Errorf("at %dpx the picks are not beside the pack", width)
			continue
		}
		if !slices.Contains(got, "2 creatures · 0 other spells · 1 land") || !slices.Contains(got, "Verdant Oathkeeper") {
			t.Errorf("at %dpx the window shows %q, missing the pack or the counts", width, got)
		}
	}
}

func TestAWideWindowKeepsTwoCardsAbreastBesideThePicks(t *testing.T) {
	// The panel takes its width from the pack, not the other way round:
	// at the width it moves beside, the pack still has two columns.
	w, _ := render(t, threePicks(), besideWidth, defaultHeight)
	if rows, cards := gridRows(t, w.TestRender(nil)), len(firstPack.Cards); rows > (cards+1)/2 {
		t.Errorf("%d rows for %d cards beside the picks; want two abreast", rows, cards)
	}
}

func TestWithNoPackOnScreenThePicksStayUnderTheHeading(t *testing.T) {
	// Waiting for the next pack, there is nothing to sit beside.
	model := drafting(premierStart, firstPack, arena.PickMade{Pack: 1, Pick: 1, Card: 106453}).Apply(fraCards)
	w, got := render(t, model, 1200, defaultHeight)
	if find(w.TestRender(nil), "draft-beside") != nil {
		t.Error("with no pack, the picks are laid out beside an empty column")
	}
	if !slices.Contains(got, "1 creature · 0 other spells") {
		t.Errorf("window shows %q, want the pick counted", got)
	}
}

func TestPicksWithNoDetailsAreSaidToBeLeftOut(t *testing.T) {
	model := drafting(premierStart,
		arena.PickMade{Pack: 1, Pick: 1, Card: 106439}, // not on Scryfall yet
		arena.PickMade{Pack: 1, Pick: 2, Card: 106453},
	).Apply(fraCards)
	_, got := render(t, model, defaultWidth, defaultHeight)
	if !slices.Contains(got, "Leaves out 1 card whose details aren't known.") {
		t.Errorf("window shows %q, want the unknown pick said to be left out", got)
	}

	model = drafting(premierStart, arena.PickMade{Pack: 1, Pick: 1, Card: 106453}).Apply(app.CardsFetching{Set: "FRA"})
	w, got := render(t, model, defaultWidth, defaultHeight)
	if !slices.Contains(got, "Colours, creatures and curve appear once the cards' details are known.") {
		t.Errorf("while the set is fetched the window shows %q, want it to say the counts are coming", got)
	}
	if find(w.TestRender(nil), "draft-curve") != nil {
		t.Error("an empty curve is drawn while the set is fetched")
	}
}

func TestAFinishedDraftShowsItsPoolCountedUp(t *testing.T) {
	model := fullDraft(t)
	// fraCards, and every other card in the pool as a bear.
	known := app.CardsLoaded{Set: "FRA", Cards: slices.Clone(fraCards.Cards)}
	for _, id := range model.Draft.Pool {
		if !slices.ContainsFunc(known.Cards, func(c cards.Card) bool { return c.ArenaID == id }) {
			known.Cards = append(known.Cards, cards.Card{ArenaID: id, Set: "FRA", Name: "Bear", ManaCost: "{1}{G}", TypeLine: "Creature — Bear"})
		}
	}
	model = model.Apply(known)
	_, got := render(t, model, defaultWidth, defaultHeight)
	sum := model.DraftScreen().Summary
	for _, want := range []string{"Your pool (42 cards)", sum.Colours, sum.Kinds} {
		if want == "" || !slices.Contains(got, want) {
			t.Errorf("window shows %q, want %q", got, want)
		}
	}
}

func TestTheSummaryFitsATinyWindow(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {minWidth, minHeight}} {
		_, got := render(t, threePicks(), size[0], size[1])
		for _, want := range []string{"G 2", "2 creatures · 0 other spells · 1 land", "6+"} {
			if !slices.Contains(got, want) {
				t.Errorf("at %dx%d the window shows %q, missing %q", size[0], size[1], got, want)
			}
		}
	}
}

func TestWideningTheWindowMovesThePicksBesideThePack(t *testing.T) {
	w, _ := render(t, threePicks(), defaultWidth, defaultHeight)
	if find(w.TestRender(nil), "draft-beside") != nil {
		t.Fatal("beside the pack at the default width")
	}
	w.EventFn(&gui.Event{Type: gui.EventResized, WindowWidth: 1000, WindowHeight: defaultHeight})
	if !w.FrameFn() {
		t.Error("the frame after a resize did not rebuild the view")
	}
	if find(w.TestRender(nil), "draft-beside") == nil {
		t.Error("after widening to 1000px the picks are still under the pack")
	}
}

// finishedPool is a draft that ended with a pool of the first pack's
// named cards, two of the green two-drop, and the one Scryfall does not
// know, with FRA's cards known.
func finishedPool() app.Model {
	pool := []int{106325, 106235, 106268, 106407, 106335, 106321, 106391, 106453, 106453, 106439}
	return drafting(premierStart, firstPack, arena.DraftEnded{Pool: pool}).Apply(fraCards)
}

func TestAFinishedDraftLaysItsPoolOutByColour(t *testing.T) {
	_, got := render(t, finishedPool(), defaultWidth, defaultHeight)
	want := []string{
		"Your pool (10 cards)",
		"Green (4)", "Thornwood Scout · G", "2× Gilded Pathfinder · 1G", "Verdant Oathkeeper · 2GG",
		"Black (1)", "Ashen Courier · 1B",
		"Red (1)", "Ember Litany · XR",
		"Multicolour (1)", "Duskmarch Paladin · (W/B)(W/B)",
		"Colourless (1)", "Gale Lantern · 2",
		"Lands (1)", "Mossback Pathway",
		"Details not known (1)", "Card 106439",
	}
	last := -1
	for _, w := range want {
		i := slices.Index(got, w)
		if i < 0 {
			t.Errorf("window shows %q, missing %q", got, w)
			continue
		}
		if i < last {
			t.Errorf("window shows %q, want %q after what comes before it", got, w)
		}
		last = i
	}
	if slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "Pack 1: ") }) {
		t.Errorf("window shows %q, still listing the pool by pack", got)
	}
}

func TestThePoolsHeapsAreLabelledAndItsCardsReadAsCards(t *testing.T) {
	custom := Dark
	custom.Text = gui.Hex(0x654321)
	custom.TextMuted = gui.Hex(0x112233)
	w := gui.NewTestWindow(t, gui.WindowCfg{State: &state{model: finishedPool(), theme: custom}})
	ly := w.TestRender(mainView)
	for id, want := range map[string]gui.Color{
		"draft-pool-0-heading": custom.TextMuted, "draft-pool-0-0": custom.Text,
		"draft-pool-6-heading": custom.TextMuted, "draft-pool-6-0": custom.Text,
	} {
		if s := find(ly, id); s == nil || s.TC == nil || s.TC.TextStyle.Color != want {
			t.Errorf("%s is not in the theme's colour", id)
		}
	}
	if find(ly, "draft-pool-7") != nil {
		t.Error("a heap past the seven the pool has")
	}
}

func TestThePoolFitsATinyWindowWithoutLosingCards(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {minWidth, minHeight}, {1600, 900}} {
		_, got := render(t, finishedPool(), size[0], size[1])
		for _, want := range []string{"Green (4)", "Verdant Oathkeeper · 2GG", "Lands (1)", "Card 106439"} {
			if !slices.Contains(got, want) {
				t.Errorf("at %dx%d the window shows %q, missing %q", size[0], size[1], got, want)
			}
		}
	}
}
