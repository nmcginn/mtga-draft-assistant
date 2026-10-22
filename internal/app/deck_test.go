package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/deck"
)

// wholePool is fraPool drafted whole: black and green lead, level at three
// spells each (the gold knight counting in black).
func wholePool() Model {
	return finished(2, 8, 1, 3, 9, 11, 4, 6, 10, 7, 12, 5).Apply(fraPool)
}

// lines is the text of each line, in order.
func lines(ls []DeckLine) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Text
	}
	return out
}

// groups is each group of the deck as its heading and its lines.
func groups(s *DeckScreen) [][]string {
	var out [][]string
	for _, g := range s.Groups {
		out = append(out, append([]string{g.Heading}, lines(g.Cards)...))
	}
	return out
}

// chosen is the letters of the colours the deck is in.
func chosen(s *DeckScreen) string {
	var b strings.Builder
	for _, c := range s.Colours {
		if c.Chosen {
			b.WriteString(c.Letter)
		}
	}
	return b.String()
}

func TestAFinishedDraftSuggestsADeckInItsLeadingColours(t *testing.T) {
	s := wholePool().DraftScreen().Deck
	if s == nil {
		t.Fatal("no deck for a finished draft with its cards known")
	}
	if got := chosen(s); got != "BG" {
		t.Errorf("colours %q, want the pool's leading black and green", got)
	}
	if len(s.Colours) != 5 || s.Colours[0].Name != "White" || s.Colours[4].Letter != "G" {
		t.Errorf("colours %+v, want all five to choose from in WUBRG order", s.Colours)
	}
	want := [][]string{
		{"Creatures (6)", "Thornwood Scout · G", "Ashen Courier · 1B", "Duskmarch Paladin · (W/B)(W/B)",
			"Gilded Pathfinder · 1G", "Verdant Oathkeeper · 2GG", "Hollow Titan · 7B"},
		{"Other spells (2)", "Free Trinket · 0", "Gale Lantern · 2"},
		{"Lands (17)"},
	}
	if got := groups(s); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("deck\n%q\nwant\n%q", got, want)
	}
	// Four green symbols and four black (the knight's hybrids paid in
	// black): level, so black, first in WUBRG, takes the odd land.
	if got := []DeckBasic{{cards.Black, "9 Swamp"}, {cards.Green, "8 Forest"}}; !slices.Equal(s.Basics, got) {
		t.Errorf("basics %+v, want %+v", s.Basics, got)
	}
	if s.Heading != "Your deck (25 cards)" || s.Size != "15 short of 40: add cards from the sideboard, or basic lands." {
		t.Errorf("heading %q, size %q", s.Heading, s.Size)
	}
	if s.SideboardHeading != "Sideboard (4)" ||
		!slices.Equal(lines(s.Sideboard), []string{"Ember Litany · XR", "Tidecaller's Riddle · 1U", "Forest", "Mossback Pathway"}) {
		t.Errorf("%s: %q, want the off-colour spells, then the lands", s.SideboardHeading, lines(s.Sideboard))
	}
	if !strings.HasPrefix(s.Note, "Suggested from what these colours can cast") {
		t.Errorf("note %q, want it to say how the deck was suggested", s.Note)
	}
	if s.Summary == nil || s.Summary.Kinds != "6 creatures · 2 other spells · 17 lands" || s.Summary.Colours != "" {
		t.Errorf("summary %+v, want the deck counted up, basics and all, and no colours line: "+
			"the knight's hybrid cost would count it as white", s.Summary)
	}
	if s.Unknown != "" || s.Copied != "" {
		t.Errorf("unknown %q, copied %q; want neither", s.Unknown, s.Copied)
	}
}

func TestTheDeckIsExportedInArenasFormatWithTheRestAsTheSideboard(t *testing.T) {
	got := wholePool().DraftScreen().Deck.Export
	want := "Deck\n" +
		"1 Thornwood Scout\n1 Ashen Courier\n1 Duskmarch Paladin\n1 Gilded Pathfinder\n1 Verdant Oathkeeper\n1 Hollow Titan\n" +
		"1 Free Trinket\n1 Gale Lantern\n" +
		"9 Swamp\n8 Forest\n" +
		"\nSideboard\n" +
		"1 Ember Litany\n1 Tidecaller's Riddle\n1 Forest\n1 Mossback Pathway\n"
	if got != want {
		t.Errorf("export\n%s\nwant\n%s", got, want)
	}
}

func TestThereIsNoDeckUntilTheDraftEndsAndItsCardsAreKnown(t *testing.T) {
	if s := picking(1, 2, 3).Apply(fraPool).DraftScreen(); s.Deck != nil {
		t.Error("a deck while the draft is on")
	}
	m := finished(1, 2, 3).Apply(CardsFetching{Set: "FRA"})
	if s := m.DraftScreen(); s.Deck != nil || !strings.Contains(s.Summary.Unknown, "a deck to build appear") {
		t.Errorf("before any card's details are known: deck %+v, note %q; want no deck, and to say one is coming", s.Deck, s.Summary.Unknown)
	}
	if s := picking(1, 2).Apply(CardsFetching{Set: "FRA"}).DraftScreen(); strings.Contains(s.Summary.Unknown, "deck") {
		t.Errorf("mid-draft the picks promise a deck: %q", s.Summary.Unknown)
	}
	if s := m.Apply(fraPool).DraftScreen(); s.Deck == nil || chosen(s.Deck) != "BG" {
		t.Error("no deck once the cards arrived")
	}
	if s := finished().Apply(fraPool).DraftScreen(); s.Deck != nil {
		t.Error("a deck from an empty pool")
	}
}

func TestChoosingAColourSuggestsAgainInTheColoursChosen(t *testing.T) {
	m := wholePool().Apply(DeckCardMoved{Card: 5, ToDeck: true})
	m = m.Apply(DeckColourToggled{Colour: cards.Green})
	s := m.DraftScreen().Deck
	if got := chosen(s); got != "B" {
		t.Fatalf("colours %q after dropping green, want black alone", got)
	}
	want := [][]string{
		{"Creatures (3)", "Ashen Courier · 1B", "Duskmarch Paladin · (W/B)(W/B)", "Hollow Titan · 7B"},
		{"Other spells (2)", "Free Trinket · 0", "Gale Lantern · 2"},
		{"Lands (17)"},
	}
	if got := groups(s); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("deck\n%q\nwant\n%q: the red card moved in is dropped with the old suggestion", got, want)
	}
	if !slices.Equal(s.Basics, []DeckBasic{{cards.Black, "17 Swamp"}}) {
		t.Errorf("basics %+v, want 17 swamps", s.Basics)
	}

	m = m.Apply(DeckColourToggled{Colour: cards.Red})
	if s := m.DraftScreen().Deck; chosen(s) != "BR" || !slices.Contains(lines(s.Groups[1].Cards), "Ember Litany · XR") {
		t.Errorf("after choosing red: colours %q, deck %q", chosen(s), groups(s))
	}
}

func TestDroppingEveryColourLeavesTheColourlessSpellsAndSaysToChoose(t *testing.T) {
	m := wholePool().Apply(DeckColourToggled{Colour: cards.Black}).Apply(DeckColourToggled{Colour: cards.Green})
	s := m.DraftScreen().Deck
	if chosen(s) != "" || s.Basics != nil {
		t.Errorf("colours %q, basics %+v; want none", chosen(s), s.Basics)
	}
	if want := [][]string{{"Other spells (2)", "Free Trinket · 0", "Gale Lantern · 2"}, {"Lands (0)"}}; !slices.EqualFunc(groups(s), want, slices.Equal) {
		t.Errorf("deck %q, want %q", groups(s), want)
	}
	if !strings.HasPrefix(s.Note, "Choose the colours") {
		t.Errorf("note %q, want it to ask for colours", s.Note)
	}
}

func TestACardMovesIntoTheDeckAndOutAgain(t *testing.T) {
	m := wholePool().Apply(DeckCardMoved{Card: 5, ToDeck: true})
	s := m.DraftScreen().Deck
	if !slices.Contains(lines(s.Groups[1].Cards), "Ember Litany · XR") || slices.Contains(lines(s.Sideboard), "Ember Litany · XR") {
		t.Fatalf("after moving it in: deck %q, sideboard %q", groups(s), lines(s.Sideboard))
	}
	if s.Heading != "Your deck (26 cards)" || !strings.HasPrefix(s.Note, "Click a card") {
		t.Errorf("heading %q, note %q", s.Heading, s.Note)
	}
	if again := m.Apply(DeckCardMoved{Card: 5, ToDeck: true}); !slices.Equal(again.Build.Deck.Main, m.Build.Deck.Main) {
		t.Error("a card the pool has one of went into the deck twice")
	}

	m = m.Apply(DeckCardMoved{Card: 1})
	s = m.DraftScreen().Deck
	if slices.Contains(lines(s.Groups[0].Cards), "Thornwood Scout · G") || lines(s.Sideboard)[0] != "Thornwood Scout · G" {
		t.Errorf("after moving it out: deck %q, sideboard %q; want the scout first in the sideboard, castable", groups(s), lines(s.Sideboard))
	}
	if again := m.Apply(DeckCardMoved{Card: 1}); !slices.Equal(again.Build.Deck.Main, m.Build.Deck.Main) {
		t.Error("moving out a card not in the deck changed it")
	}
	if other := m.Apply(DeckCardMoved{Card: 999, ToDeck: true}); !slices.Equal(other.Build.Deck.Main, m.Build.Deck.Main) {
		t.Error("a card not in the pool went into the deck")
	}
}

func TestAPoolLandMovedIntoTheDeckIsListedWithTheBasics(t *testing.T) {
	s := wholePool().Apply(DeckCardMoved{Card: 7, ToDeck: true}).DraftScreen().Deck
	if g := s.Groups[len(s.Groups)-1]; g.Heading != "Lands (18)" || !slices.Equal(lines(g.Cards), []string{"Mossback Pathway"}) {
		t.Errorf("lands %q %q, want the pathway and 17 basics", g.Heading, lines(g.Cards))
	}
	if !strings.Contains(s.Export, "1 Mossback Pathway\n9 Swamp\n") {
		t.Errorf("export\n%s\nwant the pathway before the basics", s.Export)
	}
}

func TestCopiesOfACardMoveOneAtATime(t *testing.T) {
	m := finished(8, 8, 8, 2).Apply(fraPool)
	if s := m.DraftScreen().Deck; !slices.Equal(lines(s.Groups[0].Cards), []string{"3× Gilded Pathfinder · 1G", "Verdant Oathkeeper · 2GG"}) {
		t.Fatalf("deck %q, want the three copies on one line", groups(s))
	}
	s := m.Apply(DeckCardMoved{Card: 8}).DraftScreen().Deck
	if !slices.Equal(lines(s.Groups[0].Cards), []string{"2× Gilded Pathfinder · 1G", "Verdant Oathkeeper · 2GG"}) ||
		!slices.Equal(lines(s.Sideboard), []string{"Gilded Pathfinder · 1G"}) {
		t.Errorf("deck %q, sideboard %q; want one copy moved", groups(s), lines(s.Sideboard))
	}
	if !strings.Contains(s.Export, "Deck\n2 Gilded Pathfinder\n") || !strings.HasSuffix(s.Export, "Sideboard\n1 Gilded Pathfinder\n") {
		t.Errorf("export\n%s", s.Export)
	}
}

func TestBasicLandsChangeOneAtATimeAndNeverBelowNone(t *testing.T) {
	m := wholePool().Apply(DeckBasicChanged{Colour: cards.Green, By: 1})
	s := m.DraftScreen().Deck
	if !slices.Equal(s.Basics, []DeckBasic{{cards.Black, "9 Swamp"}, {cards.Green, "9 Forest"}}) || s.Heading != "Your deck (26 cards)" {
		t.Errorf("basics %+v, heading %q", s.Basics, s.Heading)
	}
	for range 12 {
		m = m.Apply(DeckBasicChanged{Colour: cards.Black, By: -1})
	}
	if s := m.DraftScreen().Deck; !slices.Equal(s.Basics, []DeckBasic{{cards.Black, "0 Swamp"}, {cards.Green, "9 Forest"}}) {
		t.Errorf("basics %+v, want the swamps at none", s.Basics)
	}
	if s := m.Apply(DeckBasicChanged{Colour: cards.Red, By: 1}).DraftScreen().Deck; !slices.Contains(s.Basics, DeckBasic{cards.Red, "1 Mountain"}) {
		t.Errorf("basics %+v, want a mountain listed once there is one, though red is not chosen", s.Basics)
	}
	for _, bad := range []cards.Colour{-1, 5} {
		if got := m.Apply(DeckBasicChanged{Colour: bad, By: 1}); got.Build.Deck.Basics != m.Build.Deck.Basics {
			t.Errorf("a basic of colour %d changed the deck", bad)
		}
		if got := m.Apply(DeckColourToggled{Colour: bad}); got.Build.Colours != m.Build.Colours {
			t.Errorf("colour %d was chosen", bad)
		}
	}
}

func TestTheSizeLineSaysHowFarFrom40AndWhatToDo(t *testing.T) {
	m := wholePool()
	for range 15 {
		m = m.Apply(DeckBasicChanged{Colour: cards.Green, By: 1})
	}
	if s := m.DraftScreen().Deck; s.Size != "" || s.Heading != "Your deck (40 cards)" {
		t.Errorf("at 40: size %q, heading %q", s.Size, s.Heading)
	}
	m = m.Apply(DeckBasicChanged{Colour: cards.Green, By: 1})
	if s := m.DraftScreen().Deck; !strings.HasPrefix(s.Size, "1 over 40.") {
		t.Errorf("at 41: size %q", s.Size)
	}
}

func TestEditsOutliveTheCardsBeingLoadedAgain(t *testing.T) {
	m := wholePool().Apply(DeckCardMoved{Card: 5, ToDeck: true}).Apply(DeckColourToggled{Colour: cards.Red})
	m = m.Apply(DeckCardMoved{Card: 5, ToDeck: true}).Apply(fraPool)
	if s := m.DraftScreen().Deck; chosen(s) != "BRG" || !slices.Contains(lines(s.Groups[1].Cards), "Ember Litany · XR") {
		t.Errorf("after the set loaded again: colours %q, deck %q; want the choices kept", chosen(s), groups(s))
	}
}

func TestANewDraftStartsANewBuild(t *testing.T) {
	m := wholePool().Apply(DeckColourToggled{Colour: cards.Blue}).Apply(DeckCardMoved{Card: 11})
	m = m.Apply(DraftEvents{Events: []arena.Event{started, arena.DraftEnded{Pool: []int{1, 2, 3}}}})
	s := m.DraftScreen().Deck
	if chosen(s) != "BG" || !strings.HasPrefix(s.Note, "Suggested") {
		t.Errorf("the next draft's deck: colours %q, note %q; want a fresh suggestion", chosen(s), s.Note)
	}
}

func TestDeckMessagesWithNoFinishedDraftChangeNothing(t *testing.T) {
	for _, m := range []Model{New(), picking(1, 2).Apply(fraPool), finished(1, 2)} {
		for _, msg := range []Msg{DeckColourToggled{Colour: cards.Red}, DeckCardMoved{Card: 1}, DeckBasicChanged{Colour: cards.Green, By: 1}, DeckCopied{}} {
			if got := m.Apply(msg); got.Build.Pool != nil || got.Build.Chosen || got.Build.Edited || got.Build.Copied {
				t.Errorf("%T with no build changed it: %+v", msg, got.Build)
			}
		}
	}
}

func TestACopyIsConfirmedUntilTheDeckChanges(t *testing.T) {
	m := wholePool().Apply(DeckCopied{})
	if s := m.DraftScreen().Deck; s.Copied != "Copied 25 cards in Arena's deck-import format, with the rest of the pool as the sideboard." {
		t.Errorf("after copying: %q", s.Copied)
	}
	if s := m.Apply(fraPool).DraftScreen().Deck; s.Copied == "" {
		t.Error("the set loading again, with the deck the same, took back the confirmation")
	}
	for _, msg := range []Msg{DeckCardMoved{Card: 5, ToDeck: true}, DeckBasicChanged{Colour: cards.Green, By: 1}, DeckColourToggled{Colour: cards.Red}} {
		if s := m.Apply(msg).DraftScreen().Deck; s.Copied != "" {
			t.Errorf("after %T the window still says the deck was copied", msg)
		}
	}
}

func TestPoolCardsWithNoDetailsAreLeftOutOfTheBuildAndSaidSo(t *testing.T) {
	s := finished(1, 2, 106439, 106440).Apply(fraPool).DraftScreen().Deck
	if s.Unknown != "2 cards of the pool can't be built with until their details are known." {
		t.Errorf("unknown %q", s.Unknown)
	}
	if strings.Contains(s.Export, "Card 1064") {
		t.Errorf("export names cards by id, which Arena cannot import:\n%s", s.Export)
	}
	if s := finished(1, 106439).Apply(fraPool).DraftScreen().Deck; s.Unknown != "1 card of the pool can't be built with until its details are known." {
		t.Errorf("unknown %q", s.Unknown)
	}
}

func TestAWholeDraftFromTheFixtureBuildsA40CardDeck(t *testing.T) {
	m := read(following(), fixtureLines(t, premierFixture), 500)
	// The fixture's 42 cards, given details: alternately green and black,
	// then a red one in every seven, on a curve from one to five.
	known := CardsLoaded{Set: m.Draft.Set}
	for i, id := range m.Draft.Pool {
		col := []string{"G", "B", "G", "B", "G", "B", "R"}[i%7]
		known.Cards = append(known.Cards, cards.Card{
			ArenaID: id, Set: m.Draft.Set, Name: "Card " + strconv.Itoa(id),
			ManaCost: fmt.Sprintf("{%d}{%s}", i%5, col), TypeLine: "Creature — Bear", Rarity: "common",
		})
	}
	s := m.Apply(known).DraftScreen().Deck
	if s == nil || s.Heading != "Your deck (40 cards)" || s.Size != "" || chosen(s) != "BG" {
		t.Fatalf("deck %+v, want 40 cards in black and green", s)
	}
	deckPart, sidePart, _ := strings.Cut(s.Export, "\nSideboard\n")
	if n := exportCount(t, strings.TrimPrefix(deckPart, "Deck\n")); n != deck.Size {
		t.Errorf("the export's deck is %d cards, want %d", n, deck.Size)
	}
	if n := exportCount(t, sidePart); n != len(m.Draft.Pool)-deck.Spells {
		t.Errorf("the export's sideboard is %d cards, want the other %d", n, len(m.Draft.Pool)-deck.Spells)
	}
}

// exportCount adds up the counts of an export's lines.
func exportCount(t *testing.T, s string) int {
	t.Helper()
	total := 0
	for l := range strings.SplitSeq(strings.TrimSpace(s), "\n") {
		n, _, ok := strings.Cut(l, " ")
		count, err := strconv.Atoi(n)
		if !ok || err != nil {
			t.Fatalf("export line %q has no count", l)
		}
		total += count
	}
	return total
}

func TestAPoolLandMakingTheChosenColoursIsSuggestedAmongThe17(t *testing.T) {
	// Blue and green: the pathway makes both, so it takes a basic's place.
	m := wholePool().Apply(DeckColourToggled{Colour: cards.Black}).Apply(DeckColourToggled{Colour: cards.Blue})
	s := m.DraftScreen().Deck
	if chosen(s) != "UG" {
		t.Fatalf("colours %q, want UG", chosen(s))
	}
	g := s.Groups[len(s.Groups)-1]
	if g.Heading != "Lands (17)" || !slices.Equal(lines(g.Cards), []string{"Mossback Pathway"}) {
		t.Errorf("lands %q %q, want the pathway among 17", g.Heading, lines(g.Cards))
	}
	if want := []DeckBasic{{cards.Blue, "4 Island"}, {cards.Green, "12 Forest"}}; !slices.Equal(s.Basics, want) {
		t.Errorf("basics %+v beside the pathway, want %+v", s.Basics, want)
	}
	if !strings.Contains(s.Export, "1 Mossback Pathway\n") || strings.Contains(s.Export, "Sideboard\n1 Mossback Pathway") {
		t.Errorf("export\n%s\nwant the pathway in the deck", s.Export)
	}
	// The pool's Forest is a basic: Arena supplies those, so it stays out.
	if !slices.Contains(lines(s.Sideboard), "Forest") {
		t.Errorf("sideboard %q, want the pool's Forest left in it", lines(s.Sideboard))
	}
	if !strings.Contains(s.Note, "the pool's that make only these colours' mana") {
		t.Errorf("note %q, want it to say the pool's lands go in", s.Note)
	}
}

func TestAPoolLandMakingAColourNotChosenIsLeftInTheSideboard(t *testing.T) {
	// Black and green: the pathway's blue half is no use, and its green
	// half is a Forest's.
	s := wholePool().DraftScreen().Deck
	if g := s.Groups[len(s.Groups)-1]; len(g.Cards) != 0 {
		t.Errorf("lands %q, want only basics", lines(g.Cards))
	}
	if !slices.Contains(lines(s.Sideboard), "Mossback Pathway") {
		t.Errorf("sideboard %q, want the pathway in it", lines(s.Sideboard))
	}
}

func TestTheSideboardListsLandsTheColoursCanPlayAfterTheirSpells(t *testing.T) {
	m := wholePool().Apply(DeckColourToggled{Colour: cards.Black}).Apply(DeckColourToggled{Colour: cards.Blue})
	m = m.Apply(DeckCardMoved{Card: 7, ToDeck: false})
	s := m.DraftScreen().Deck
	// Tidecaller's Riddle, the one blue spell, is in the deck.
	want := []string{"Mossback Pathway", "Ember Litany · XR", "Ashen Courier · 1B", "Duskmarch Paladin · (W/B)(W/B)",
		"Hollow Titan · 7B", "Forest"}
	if got := lines(s.Sideboard); !slices.Equal(got, want) {
		t.Errorf("sideboard %q, want %q", got, want)
	}
}
