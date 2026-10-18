package app

import (
	"slices"
	"testing"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// fraPool is a spread of FRA cards to pick from, with invented names as in
// testdata/scryfall; the ids are free, since no pack is shown.
var fraPool = CardsLoaded{Set: "FRA", Cards: []cards.Card{
	{ArenaID: 1, Set: "FRA", Name: "Thornwood Scout", ManaCost: "{G}", TypeLine: "Creature — Elf Scout"},
	{ArenaID: 2, Set: "FRA", Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid"},
	{ArenaID: 3, Set: "FRA", Name: "Ashen Courier", ManaCost: "{1}{B}", TypeLine: "Creature — Human Rogue"},
	{ArenaID: 4, Set: "FRA", Name: "Duskmarch Paladin", ManaCost: "{W/B}{W/B}", TypeLine: "Creature — Human Knight"},
	{ArenaID: 5, Set: "FRA", Name: "Ember Litany", ManaCost: "{X}{R}", TypeLine: "Sorcery"},
	{ArenaID: 6, Set: "FRA", Name: "Gale Lantern", ManaCost: "{2}", TypeLine: "Artifact"},
	{ArenaID: 7, Set: "FRA", Name: "Mossback Pathway", TypeLine: "Land"},
	{ArenaID: 8, Set: "FRA", Name: "Gilded Pathfinder", ManaCost: "{1}{G}", TypeLine: "Creature — Elf Ranger"},
	{ArenaID: 9, Set: "FRA", Name: "Hollow Titan", ManaCost: "{7}{B}", TypeLine: "Creature — Horror"},
	{ArenaID: 10, Set: "FRA", Name: "Free Trinket", ManaCost: "{0}", TypeLine: "Artifact"},
	{ArenaID: 11, Set: "FRA", Name: "Tidecaller's Riddle", ManaCost: "{1}{U}", TypeLine: "Instant"},
	{ArenaID: 12, Set: "FRA", Name: "Forest", TypeLine: "Basic Land — Forest"},
}}

// picking is a draft in FRA with ids picked in pack 1, in order.
func picking(ids ...int) Model {
	events := make([]arena.Event, len(ids))
	for i, id := range ids {
		events[i] = arena.PickMade{Pack: 1, Pick: i + 1, Card: id}
	}
	return inDraft(events...)
}

func TestThereIsNoSummaryBeforeTheFirstPick(t *testing.T) {
	if s := inDraft(p1p1).Apply(fraPool).DraftScreen(); s.Summary != nil {
		t.Errorf("summary %+v before any pick", s.Summary)
	}
}

func TestThePicksAreCountedByColourKindAndCurve(t *testing.T) {
	sum := picking(1, 2, 3, 4, 5, 6, 7, 8).Apply(fraPool).DraftScreen().Summary
	if sum == nil {
		t.Fatal("no summary after eight picks")
	}
	if want := "G 3 · B 2 · W 1 · R 1 · Colourless 1"; sum.Colours != want {
		t.Errorf("colours %q, want %q", sum.Colours, want)
	}
	if want := "5 creatures · 2 other spells · 1 land"; sum.Kinds != want {
		t.Errorf("kinds %q, want %q", sum.Kinds, want)
	}
	want := []CurveColumn{{"1", 2}, {"2", 4}, {"3", 0}, {"4", 1}, {"5", 0}, {"6+", 0}}
	if !slices.Equal(sum.Curve, want) {
		t.Errorf("curve %+v, want %+v", sum.Curve, want)
	}
	if sum.Unknown != "" {
		t.Errorf("unknown %q, though every pick is known", sum.Unknown)
	}
}

func TestColoursLevelWithEachOtherKeepWUBRGOrder(t *testing.T) {
	// One each of blue, black, red and green, and the gold white-black
	// card: black leads with two, then white, blue, red, green with one.
	sum := picking(11, 3, 5, 1, 4).Apply(fraPool).DraftScreen().Summary
	if want := "B 2 · W 1 · U 1 · R 1 · G 1"; sum.Colours != want {
		t.Errorf("colours %q, want %q", sum.Colours, want)
	}
}

func TestTheCurveShowsAZeroColumnOnlyWhenASpellCostsNothing(t *testing.T) {
	with := picking(10, 1).Apply(fraPool).DraftScreen().Summary
	if len(with.Curve) == 0 || with.Curve[0] != (CurveColumn{"0", 1}) {
		t.Errorf("curve %+v, want it to start with the free spell's 0", with.Curve)
	}
	without := picking(1).Apply(fraPool).DraftScreen().Summary
	if len(without.Curve) == 0 || without.Curve[0].Label != "1" {
		t.Errorf("curve %+v, want it to start at 1", without.Curve)
	}
}

func TestAnExpensiveSpellIsInTheCurvesLastColumn(t *testing.T) {
	sum := picking(9).Apply(fraPool).DraftScreen().Summary
	if last := sum.Curve[len(sum.Curve)-1]; last != (CurveColumn{"6+", 1}) {
		t.Errorf("last column %+v, want the eight-drop in 6+", last)
	}
}

func TestOneOfEachIsSaidInTheSingular(t *testing.T) {
	sum := picking(1, 5, 7).Apply(fraPool).DraftScreen().Summary
	if want := "1 creature · 1 other spell · 1 land"; sum.Kinds != want {
		t.Errorf("kinds %q, want %q", sum.Kinds, want)
	}
}

func TestOnlyLandsPickedSaysSoWithNoCurveOrColours(t *testing.T) {
	sum := picking(7, 12).Apply(fraPool).DraftScreen().Summary
	if want := "0 creatures · 0 other spells · 2 lands"; sum.Kinds != want {
		t.Errorf("kinds %q, want %q", sum.Kinds, want)
	}
	if sum.Colours != "" || sum.Curve != nil {
		t.Errorf("colours %q and curve %+v for a pool of lands", sum.Colours, sum.Curve)
	}
}

func TestPicksWithNoDetailsAreLeftOutOfTheCountsAndSaidToBe(t *testing.T) {
	// 106439 is not in the set: brand new, or not on Scryfall yet.
	sum := picking(1, 106439, 3).Apply(fraPool).DraftScreen().Summary
	if want := "Leaves out 1 card whose details aren't known."; sum.Unknown != want {
		t.Errorf("unknown %q, want %q", sum.Unknown, want)
	}
	if want := "2 creatures · 0 other spells"; sum.Kinds != want {
		t.Errorf("kinds %q, want %q: only the two known picks", sum.Kinds, want)
	}
	sum = picking(1, 106439, 106440).Apply(fraPool).DraftScreen().Summary
	if want := "Leaves out 2 cards whose details aren't known."; sum.Unknown != want {
		t.Errorf("unknown %q, want %q", sum.Unknown, want)
	}
}

func TestWhileTheSetIsComingTheSummarySaysWhenItWillAppear(t *testing.T) {
	for name, m := range map[string]Model{
		"fetching": picking(1, 2).Apply(CardsFetching{Set: "FRA"}),
		"no set":   following().Apply(DraftEvents{Events: []arena.Event{arena.PickMade{Pack: 1, Pick: 1, Card: 1}}}),
	} {
		sum := m.DraftScreen().Summary
		if sum == nil {
			t.Errorf("%s: no summary, though there are picks", name)
			continue
		}
		if sum.Unknown != "Colours, creatures and curve appear once the cards' details are known." {
			t.Errorf("%s: unknown %q", name, sum.Unknown)
		}
		if sum.Colours != "" || sum.Kinds != "" || sum.Curve != nil {
			t.Errorf("%s: counted %+v with no card known", name, sum)
		}
	}
}

func TestTheSummaryFillsInWhenTheSetArrives(t *testing.T) {
	m := picking(1, 2).Apply(CardsFetching{Set: "FRA"})
	if m.DraftScreen().Summary.Kinds != "" {
		t.Fatal("counted before the set arrived")
	}
	if got := m.Apply(fraPool).DraftScreen().Summary.Kinds; got != "2 creatures · 0 other spells" {
		t.Errorf("after the set arrived, kinds %q", got)
	}
}

func TestAFinishedDraftCountsItsPool(t *testing.T) {
	// The pool, not the picks read: the log may have begun mid-draft, and
	// the pool is everything drafted.
	m := picking(1).Apply(DraftEvents{Events: []arena.Event{arena.DraftEnded{Pool: []int{1, 2, 7}}}}).Apply(fraPool)
	sum := m.DraftScreen().Summary
	if sum == nil || sum.Kinds != "2 creatures · 0 other spells · 1 land" {
		t.Errorf("summary %+v, want the whole pool counted", sum)
	}
}

func TestAFinishedDraftWithAnEmptyPoolHasNoSummary(t *testing.T) {
	m := inDraft().Apply(DraftEvents{Events: []arena.Event{arena.DraftEnded{}}})
	if s := m.DraftScreen(); s.Summary != nil {
		t.Errorf("summary %+v for an empty pool", s.Summary)
	}
}

func TestAWholeDraftFromTheFixtureIsCountedCardForCard(t *testing.T) {
	// The fixture's 42 picks, with none of them known: all left out, and
	// said to be; then with every one known as a creature, all counted.
	m := read(following(), fixtureLines(t, premierFixture), 500)
	if got := m.DraftScreen().Summary; got == nil || got.Kinds != "" || got.Unknown == "" {
		t.Fatalf("summary %+v, want the picks said to be unknown", got)
	}
	known := CardsLoaded{Set: m.Draft.Set}
	for _, id := range m.Draft.Pool {
		known.Cards = append(known.Cards, cards.Card{ArenaID: id, Set: m.Draft.Set, Name: "Bear", ManaCost: "{1}{G}", TypeLine: "Creature — Bear"})
	}
	sum := m.Apply(known).DraftScreen().Summary
	if sum.Kinds != "42 creatures · 0 other spells" || sum.Colours != "G 42" || sum.Unknown != "" {
		t.Errorf("summary %+v, want all 42 counted", sum)
	}
}
