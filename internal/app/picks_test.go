package app

import (
	"slices"
	"strings"
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
	{ArenaID: 7, Set: "FRA", Name: "Mossback Pathway", TypeLine: "Land", Produces: "UG"},
	{ArenaID: 8, Set: "FRA", Name: "Gilded Pathfinder", ManaCost: "{1}{G}", TypeLine: "Creature — Elf Ranger"},
	{ArenaID: 9, Set: "FRA", Name: "Hollow Titan", ManaCost: "{7}{B}", TypeLine: "Creature — Horror"},
	{ArenaID: 10, Set: "FRA", Name: "Free Trinket", ManaCost: "{0}", TypeLine: "Artifact"},
	{ArenaID: 11, Set: "FRA", Name: "Tidecaller's Riddle", ManaCost: "{1}{U}", TypeLine: "Instant"},
	{ArenaID: 12, Set: "FRA", Name: "Forest", TypeLine: "Basic Land — Forest", Produces: "G"},
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

// finished is a draft in FRA that ended with pool, its picks read as the
// same cards in pack 1.
func finished(pool ...int) Model {
	return picking(pool...).Apply(DraftEvents{Events: []arena.Event{arena.DraftEnded{Pool: pool}}})
}

// headings is each group's heading, in order.
func headings(gs []PoolGroup) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Heading
	}
	return out
}

func TestAFinishedPoolIsGroupedByColourWithItsCardsUpTheCurve(t *testing.T) {
	s := finished(2, 8, 1, 3, 9, 11, 4, 6, 10, 7, 12, 5).Apply(fraPool).DraftScreen()
	want := []PoolGroup{
		// Black has two of its own and the gold knight, green three: level,
		// they keep WUBRG order.
		{Heading: "Black (2)", Cards: []string{"Ashen Courier · 1B", "Hollow Titan · 7B"}},
		{Heading: "Green (3)", Cards: []string{"Thornwood Scout · G", "Gilded Pathfinder · 1G", "Verdant Oathkeeper · 2GG"}},
		{Heading: "Blue (1)", Cards: []string{"Tidecaller's Riddle · 1U"}},
		{Heading: "Red (1)", Cards: []string{"Ember Litany · XR"}},
		{Heading: "Multicolour (1)", Cards: []string{"Duskmarch Paladin · (W/B)(W/B)"}},
		{Heading: "Colourless (2)", Cards: []string{"Free Trinket · 0", "Gale Lantern · 2"}},
		{Heading: "Lands (2)", Cards: []string{"Forest", "Mossback Pathway"}},
	}
	if !slices.EqualFunc(s.Pool, want, func(a, b PoolGroup) bool { return a.Heading == b.Heading && slices.Equal(a.Cards, b.Cards) }) {
		t.Errorf("pool grouped as\n%q\nwant\n%q", s.Pool, want)
	}
	if s.Picks != nil {
		t.Errorf("picks %q listed beside the grouped pool", s.Picks)
	}
}

func TestTheGroupsLeadWithTheColoursTheSummaryLeadsWith(t *testing.T) {
	s := finished(1, 2, 8, 3, 9, 11).Apply(fraPool).DraftScreen()
	if got := headings(s.Pool); !slices.Equal(got, []string{"Green (3)", "Black (2)", "Blue (1)"}) {
		t.Errorf("groups %q under %q, want green, black, blue", got, s.Summary.Colours)
	}
	if !strings.HasPrefix(s.Summary.Colours, "G 3 · B 2") {
		t.Errorf("summary %q, want it to lead green then black", s.Summary.Colours)
	}
}

func TestCopiesOfACardInThePoolShareALine(t *testing.T) {
	s := finished(8, 2, 8, 8).Apply(fraPool).DraftScreen()
	if len(s.Pool) != 1 || s.Pool[0].Heading != "Green (4)" ||
		!slices.Equal(s.Pool[0].Cards, []string{"3× Gilded Pathfinder · 1G", "Verdant Oathkeeper · 2GG"}) {
		t.Errorf("pool %q, want four green cards on two lines", s.Pool)
	}
}

func TestPoolCardsWithNoDetailsAreGroupedLastByID(t *testing.T) {
	s := finished(106440, 8, 106439, 106440).Apply(fraPool).DraftScreen()
	if got := headings(s.Pool); !slices.Equal(got, []string{"Green (1)", "Details not known (3)"}) {
		t.Fatalf("groups %q, want green, then the unknown cards", got)
	}
	if got := s.Pool[1].Cards; !slices.Equal(got, []string{"Card 106439", "2× Card 106440"}) {
		t.Errorf("unknown cards %q, want them by id with copies together", got)
	}
	if s.Summary.Unknown == "" {
		t.Error("the summary does not say cards are left out of its counts")
	}
}

func TestAPoolIsListedByPackUntilItsCardsArriveThenGrouped(t *testing.T) {
	m := finished(1, 2, 3).Apply(CardsFetching{Set: "FRA"})
	if s := m.DraftScreen(); s.Pool != nil || !slices.Equal(s.Picks, []string{"Pack 1: Card 1, Card 2, Card 3"}) {
		t.Fatalf("while fetching: pool %q, picks %q; want the pool by pack, by id", s.Pool, s.Picks)
	}
	s := m.Apply(fraPool).DraftScreen()
	if got := headings(s.Pool); !slices.Equal(got, []string{"Green (2)", "Black (1)"}) || s.Picks != nil {
		t.Errorf("after the set arrived: groups %q, picks %q; want the pool grouped", got, s.Picks)
	}
}

func TestAPoolOfASetScryfallHasNotGotIsListedByPack(t *testing.T) {
	// A set loaded, but none of the pool in it: brand new cards, say.
	m := finished(106439, 106440).Apply(fraPool)
	if s := m.DraftScreen(); s.Pool != nil || len(s.Picks) != 1 {
		t.Errorf("pool %q, picks %q; want the unknown pool by pack", s.Pool, s.Picks)
	}
}

func TestTheWholePoolIsGroupedEvenWhenTheLogMissedSomePicks(t *testing.T) {
	// One pick read, three cards in the pool: the pool is what is grouped.
	m := picking(1).Apply(DraftEvents{Events: []arena.Event{arena.DraftEnded{Pool: []int{1, 2, 7}}}}).Apply(fraPool)
	if got := headings(m.DraftScreen().Pool); !slices.Equal(got, []string{"Green (2)", "Lands (1)"}) {
		t.Errorf("groups %q, want the whole pool", got)
	}
}

func TestThePoolIsNotGroupedWhileTheDraftIsOnOrWhenItIsEmpty(t *testing.T) {
	if s := picking(1, 2).Apply(fraPool).DraftScreen(); s.Pool != nil {
		t.Errorf("mid-draft, pool grouped as %q", s.Pool)
	}
	if s := finished().Apply(fraPool).DraftScreen(); s.Pool != nil || s.Picks != nil {
		t.Errorf("an empty pool: groups %q, picks %q", s.Pool, s.Picks)
	}
}

func TestAWholeDraftFromTheFixtureIsGroupedCardForCard(t *testing.T) {
	m := read(following(), fixtureLines(t, premierFixture), 500)
	known := CardsLoaded{Set: m.Draft.Set}
	for _, id := range m.Draft.Pool {
		known.Cards = append(known.Cards, cards.Card{ArenaID: id, Set: m.Draft.Set, Name: "Bear", ManaCost: "{1}{G}", TypeLine: "Creature — Bear"})
	}
	s := m.Apply(known).DraftScreen()
	if len(s.Pool) != 1 || s.Pool[0].Heading != "Green (42)" || !slices.Equal(s.Pool[0].Cards, []string{"42× Bear · 1G"}) {
		t.Errorf("pool %q, want 42 green bears on one line", s.Pool)
	}
}
