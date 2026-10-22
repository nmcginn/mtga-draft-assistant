package deck

import (
	"fmt"
	"slices"
	"testing"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// colours is a choice of the colours given.
func colours(cs ...cards.Colour) Colours {
	var out Colours
	for _, c := range cs {
		out[c] = true
	}
	return out
}

func spell(id int, name, cost, rarity string) cards.Card {
	return cards.Card{ArenaID: id, Name: name, ManaCost: cost, TypeLine: "Sorcery", Rarity: rarity}
}

func creature(id int, name, cost, rarity string) cards.Card {
	return cards.Card{ArenaID: id, Name: name, ManaCost: cost, TypeLine: "Creature — Elf", Rarity: rarity}
}

func TestACardIsCastableWhenEveryColouredSymbolIsAColourChosen(t *testing.T) {
	bg := colours(cards.Black, cards.Green)
	for _, tc := range []struct {
		cost string
		want bool
	}{
		{"{2}{G}{G}", true},
		{"{1}{B}{G}", true},
		{"{1}{R}", false},
		{"{G}{U}", false},
		{"{2}", true},        // colourless goes in any deck
		{"{0}", true},        // and so does free
		{"", true},           // a spell with no cost at all, cast another way
		{"{X}{G}", true},     // X is paid with any mana
		{"{W/B}{W/B}", true}, // a hybrid symbol needs either half
		{"{W/U}", false},
		{"{2/R}", true},            // or two of anything
		{"{R/P}", true},            // or two life
		{"{1}{G} // {3}{R}", true}, // the front face is the one cast
		{"{1}{R} // {3}{G}", false},
	} {
		c := spell(1, "Test", tc.cost, "common")
		if got := Castable(c, bg); got != tc.want {
			t.Errorf("Castable(%q) in BG = %v, want %v", tc.cost, got, tc.want)
		}
	}
}

func TestALandIsNeverCast(t *testing.T) {
	for _, c := range []cards.Card{
		{Name: "Mossback Pathway", TypeLine: "Land"},
		{Name: "Forest", TypeLine: "Basic Land — Forest"},
		{Name: "Dryad Arbor", ManaCost: "", TypeLine: "Land Creature — Forest Dryad"},
	} {
		if Castable(c, colours(cards.Colours...)) {
			t.Errorf("%s counts as a spell to cast", c.Name)
		}
	}
}

func TestWithNoColoursChosenOnlyColourlessSpellsAreCastable(t *testing.T) {
	if !Castable(spell(1, "Gale Lantern", "{2}", "common"), Colours{}) {
		t.Error("a colourless spell needs a colour")
	}
	if Castable(spell(1, "Ember Litany", "{X}{R}", "common"), Colours{}) {
		t.Error("a red spell is castable with no colours")
	}
}

func TestTheLeadingColoursAreTheTwoWithTheMostSpells(t *testing.T) {
	cs := []cards.Card{
		spell(1, "a", "{G}", "common"), spell(2, "b", "{G}", "common"), spell(3, "c", "{G}", "common"),
		spell(4, "d", "{B}", "common"), spell(5, "e", "{B}", "common"),
		spell(6, "f", "{R}", "common"),
		{ArenaID: 7, Name: "Forest", TypeLine: "Basic Land — Forest"},
	}
	if got, want := Leading(cs), colours(cards.Black, cards.Green); got != want {
		t.Errorf("leading %v, want BG", got.List())
	}
}

func TestLeadingColoursLevelWithEachOtherKeepWUBRGOrder(t *testing.T) {
	cs := []cards.Card{spell(1, "a", "{G}", "common"), spell(2, "b", "{R}", "common"), spell(3, "c", "{U}", "common")}
	if got, want := Leading(cs), colours(cards.Blue, cards.Red); got != want {
		t.Errorf("leading %v, want UR", got.List())
	}
}

func TestAPoolOfOneColourOrNoneLeadsWithThatMany(t *testing.T) {
	if got, want := Leading([]cards.Card{spell(1, "a", "{G}", "common"), spell(2, "b", "{2}", "common")}), colours(cards.Green); got != want {
		t.Errorf("leading %v for a green pool, want G alone", got.List())
	}
	if got := Leading([]cards.Card{spell(1, "a", "{2}", "common")}); got != (Colours{}) {
		t.Errorf("leading %v for a colourless pool, want none", got.List())
	}
	if got := Leading(nil); got != (Colours{}) {
		t.Errorf("leading %v for an empty pool, want none", got.List())
	}
}

// bigPool is 30 green and black spells, a red one, and a land: more than
// a deck can hold, so the suggestion has to choose.
func bigPool() []cards.Card {
	var cs []cards.Card
	for i := range 26 {
		col := "G"
		if i%2 == 1 {
			col = "B"
		}
		cs = append(cs, creature(100+i, fmt.Sprintf("Common %02d", i), fmt.Sprintf("{%d}{%s}", i%5, col), "common"))
	}
	cs = append(cs,
		spell(200, "Rare Sorcery", "{4}{B}{B}", "rare"),
		creature(201, "Mythic Beast", "{5}{G}", "mythic"),
		spell(202, "Uncommon Trick", "{1}{G}", "uncommon"),
		creature(203, "Uncommon Elf", "{3}{G}", "uncommon"),
		spell(204, "Red Rare", "{R}", "rare"),
		cards.Card{ArenaID: 205, Name: "Mossback Pathway", TypeLine: "Land", Rarity: "uncommon"},
	)
	return cs
}

func TestTheSuggestionIs23SpellsAnd17BasicLands(t *testing.T) {
	d := Suggest(bigPool(), colours(cards.Black, cards.Green))
	if len(d.Main) != Spells {
		t.Errorf("%d spells, want %d", len(d.Main), Spells)
	}
	lands := 0
	for _, n := range d.Basics {
		lands += n
	}
	if lands != Lands || d.Count() != Size {
		t.Errorf("%d basic lands and %d cards, want %d and %d", lands, d.Count(), Lands, Size)
	}
	if d.Basics[cards.Black] == 0 || d.Basics[cards.Green] == 0 {
		t.Errorf("basics %v, want both colours", d.Basics)
	}
}

func TestTheSuggestionTakesTheRarestThenCreaturesThenTheCheapest(t *testing.T) {
	d := Suggest(bigPool(), colours(cards.Black, cards.Green))
	for _, id := range []int{200, 201, 202, 203} {
		if !slices.Contains(d.Main, id) {
			t.Errorf("the deck leaves out %d, rarer than every common", id)
		}
	}
	// 19 places are left for 26 commons, which cost i%5 generic and a
	// coloured symbol: the 16 of mana value 1 to 3 go in, then three of
	// the five four-drops, and none of the five-drops.
	for _, out := range []int{104, 109, 114, 119, 124} {
		if slices.Contains(d.Main, out) {
			t.Errorf("the deck has five-drop %d over cheaper commons", out)
		}
	}
	if !slices.IsSorted(d.Main) {
		t.Errorf("deck %v is not sorted by id", d.Main)
	}
}

func TestTheSuggestionLeavesOutWhatTheColoursCannotCastAndALandThatMakesNothingKnown(t *testing.T) {
	// bigPool's land has no Produces: a cache from before it was stored,
	// or a land that makes no mana itself.
	d := Suggest(bigPool(), colours(cards.Black, cards.Green))
	if slices.Contains(d.Main, 204) || slices.Contains(d.Main, 205) {
		t.Errorf("deck %v has the red rare or the land", d.Main)
	}
}

func TestAPoolShortOfSpellsSuggestsEveryOneItCanCast(t *testing.T) {
	cs := []cards.Card{
		creature(1, "a", "{G}", "common"), creature(1, "a", "{G}", "common"), spell(2, "b", "{2}", "common"),
		spell(3, "c", "{U}", "rare"),
	}
	d := Suggest(cs, colours(cards.Green))
	if want := []int{1, 1, 2}; !slices.Equal(d.Main, want) {
		t.Errorf("deck %v, want both copies and the colourless spell: %v", d.Main, want)
	}
	if d.Basics[cards.Green] != Lands || d.Count() != 20 {
		t.Errorf("basics %v, count %d; want 17 forests and 20 cards", d.Basics, d.Count())
	}
}

func TestAnEmptyPoolOrNoColoursSuggestsNoSpells(t *testing.T) {
	if d := Suggest(nil, colours(cards.Green)); len(d.Main) != 0 || d.Basics[cards.Green] != Lands {
		t.Errorf("empty pool: %+v, want 17 forests and nothing else", d)
	}
	d := Suggest([]cards.Card{spell(1, "a", "{G}", "common"), spell(2, "b", "{3}", "common")}, Colours{})
	if !slices.Equal(d.Main, []int{2}) || d.Basics != [5]int{} {
		t.Errorf("no colours: %+v, want the colourless spell and no basics", d)
	}
}

func TestBasicsAreSharedByTheColoursManaSymbols(t *testing.T) {
	var spells []cards.Card
	for range 10 {
		spells = append(spells, spell(1, "g", "{1}{G}", "common"))
	}
	for range 8 {
		spells = append(spells, spell(2, "b", "{1}{B}", "common"))
	}
	got := SplitBasics(spells, colours(cards.Black, cards.Green), 17)
	if want := ([5]int{cards.Black: 8, cards.Green: 9}); got != want {
		t.Errorf("basics %v, want 9 forests and 8 swamps", got)
	}
}

func TestASplashedColourGetsALandForEvenOneSymbol(t *testing.T) {
	var spells []cards.Card
	for range 12 {
		spells = append(spells, spell(1, "g", "{G}", "common"))
	}
	for range 10 {
		spells = append(spells, spell(2, "b", "{B}", "common"))
	}
	spells = append(spells, spell(3, "r", "{3}{R}", "rare"))
	got := SplitBasics(spells, colours(cards.Black, cards.Red, cards.Green), 17)
	if got[cards.Red] < 1 || got[cards.Green] <= got[cards.Black] || got[cards.Black]+got[cards.Red]+got[cards.Green] != 17 {
		t.Errorf("basics %v, want 17 with green most and red at least one", got)
	}
}

func TestAHybridSymbolCountsTowardEachChosenHalf(t *testing.T) {
	got := SplitBasics([]cards.Card{spell(1, "wb", "{W/B}{W/B}", "common")}, colours(cards.White, cards.Black), 4)
	if want := ([5]int{cards.White: 2, cards.Black: 2}); got != want {
		t.Errorf("basics %v, want two of each", got)
	}
}

func TestColourlessSpellsSplitTheBasicsEvenly(t *testing.T) {
	got := SplitBasics([]cards.Card{spell(1, "x", "{4}", "common")}, colours(cards.Blue, cards.Black), 17)
	if got[cards.Blue]+got[cards.Black] != 17 || got[cards.Blue]-got[cards.Black] > 1 || got[cards.Black]-got[cards.Blue] > 1 {
		t.Errorf("basics %v, want 17 split evenly", got)
	}
}

func TestFewerLandsThanColoursStillAddUp(t *testing.T) {
	spells := []cards.Card{spell(1, "a", "{W}{U}{B}", "common")}
	got := SplitBasics(spells, colours(cards.White, cards.Blue, cards.Black), 2)
	if n := got[cards.White] + got[cards.Blue] + got[cards.Black]; n != 2 {
		t.Errorf("basics %v add up to %d, want 2", got, n)
	}
	if got := SplitBasics(spells, colours(cards.White), 0); got != ([5]int{}) {
		t.Errorf("no lands to share: %v", got)
	}
	if got := SplitBasics(spells, Colours{}, 17); got != ([5]int{}) {
		t.Errorf("no colours to share them by: %v", got)
	}
}

func TestTheExportIsArenasDeckImportFormat(t *testing.T) {
	main := []cards.Card{
		creature(1, "Gilded Pathfinder", "{1}{G}", "common"),
		creature(1, "Gilded Pathfinder", "{1}{G}", "common"),
		creature(2, "Lanternwatch Recruit // Lanternwatch Captain", "{1}{G}", "rare"),
		{ArenaID: 3, Name: "Forest", TypeLine: "Basic Land — Forest"},
	}
	side := []cards.Card{spell(4, "Ember Litany", "{X}{R}", "rare")}
	got := Export(main, [5]int{cards.Black: 7, cards.Green: 8}, side)
	want := "Deck\n" +
		"2 Gilded Pathfinder\n" +
		"1 Lanternwatch Recruit\n" +
		"9 Forest\n" +
		"7 Swamp\n" +
		"\n" +
		"Sideboard\n" +
		"1 Ember Litany\n"
	if got != want {
		t.Errorf("export:\n%s\nwant:\n%s", got, want)
	}
}

func TestAnExportWithNothingLeftOverHasNoSideboard(t *testing.T) {
	got := Export([]cards.Card{spell(1, "Gale Lantern", "{2}", "common")}, [5]int{}, nil)
	if want := "Deck\n1 Gale Lantern\n"; got != want {
		t.Errorf("export %q, want %q", got, want)
	}
	if got, want := Export(nil, [5]int{}, nil), "Deck\n"; got != want {
		t.Errorf("empty export %q, want %q", got, want)
	}
}

func TestEachBasicLandIsNamedForItsColour(t *testing.T) {
	for c, want := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest"} {
		b := Basic(cards.Colour(c))
		if b.Name != want || b.Kind() != cards.Land {
			t.Errorf("basic for %s is %+v", cards.Colour(c).Name(), b)
		}
	}
}

func land(id int, name, produces string) cards.Card {
	return cards.Card{ArenaID: id, Name: name, TypeLine: "Land", Rarity: "uncommon", Produces: produces}
}

func TestALandIsPlayedWhenItMakesTheDecksColoursAndNoOthers(t *testing.T) {
	bg := colours(cards.Black, cards.Green)
	for _, tc := range []struct {
		c    cards.Card
		want bool
	}{
		{land(1, "Dual", "BG"), true},                    // both of the deck's colours
		{land(1, "Utility", "G"), true},                  // one of them, nothing else
		{land(1, "Utility with colourless", "GC"), true}, // colourless mana is no other colour
		{land(1, "Rainbow", "WUBRG"), true},              // any colour includes both
		{land(1, "Tri", "BRG"), true},                    // a dual for this deck, and more
		{land(1, "Off dual", "UG"), false},               // half of it is off colour
		{land(1, "Off colour", "R"), false},
		{land(1, "Colourless", "C"), false}, // no source of either colour
		{land(1, "Fetch", ""), false},       // makes nothing itself, or not known
		{cards.Card{Name: "Forest", TypeLine: "Basic Land — Forest", Produces: "G"}, false},
		{cards.Card{Name: "Snow-Covered Swamp", TypeLine: "Basic Snow Land — Swamp", Produces: "B"}, false},
		{cards.Card{Name: "Llanowar Elder", ManaCost: "{G}", TypeLine: "Creature — Elf", Produces: "G"}, false},
		{cards.Card{Name: "Spell // Land", ManaCost: "{1}{G}", TypeLine: "Sorcery // Land", Produces: "G"}, false},
		{cards.Card{Name: "Land // Spell", TypeLine: "Land // Sorcery", Produces: "B"}, true}, // played as its front
	} {
		if got := OnColour(tc.c, bg); got != tc.want {
			t.Errorf("OnColour(%s, makes %q) in BG = %v, want %v", tc.c.Name, tc.c.Produces, got, tc.want)
		}
	}
}

func TestInOneColourALandOfThatColourIsPlayed(t *testing.T) {
	g := colours(cards.Green)
	if !OnColour(land(1, "Dual", "UG"), g) {
		t.Error("a land making green is not played in mono-green")
	}
	if OnColour(land(1, "Dual", "UB"), g) {
		t.Error("a land making no green is played in mono-green")
	}
}

func TestWithNoColoursChosenNoLandIsPlayed(t *testing.T) {
	if OnColour(land(1, "Rainbow", "WUBRG"), Colours{}) {
		t.Error("a land is played in a deck of no colours")
	}
	if d := Suggest([]cards.Card{land(1, "Rainbow", "WUBRG")}, Colours{}); len(d.Main) != 0 || d.Count() != 0 {
		t.Errorf("no colours: %+v, want an empty deck", d)
	}
}

func TestThePoolsLandsCountAmongThe17(t *testing.T) {
	cs := append(bigPool(), land(300, "Duskmoss Grove", "BG"), land(301, "Thornhollow", "G"), land(302, "Tidal Mire", "UB"))
	d := Suggest(cs, colours(cards.Black, cards.Green))
	if !slices.Contains(d.Main, 300) || !slices.Contains(d.Main, 301) {
		t.Errorf("deck %v leaves out the dual or the green land", d.Main)
	}
	if slices.Contains(d.Main, 302) {
		t.Errorf("deck %v plays the blue-black land in black-green", d.Main)
	}
	if len(d.Main) != Spells+2 || d.Count() != Size {
		t.Errorf("%d of the pool's cards and %d in all, want %d spells and 2 lands in %d", len(d.Main), d.Count(), Spells, Size)
	}
	without := Suggest(bigPool(), colours(cards.Black, cards.Green))
	basics := 0
	for _, n := range d.Basics {
		basics += n
	}
	if basics != Lands-2 {
		t.Errorf("%d basics beside 2 of the pool's lands, want %d", basics, Lands-2)
	}
	// The green land stands in for a Forest. The dual stands in for
	// whichever colour had more basics; either way each colour keeps at
	// least as many sources as it had.
	if d.Basics[cards.Green]+2 < without.Basics[cards.Green] || d.Basics[cards.Black]+1 < without.Basics[cards.Black] {
		t.Errorf("basics %v beside the lands, %v without: a colour lost a source", d.Basics, without.Basics)
	}
}

func TestADualTakesTheBasicOfTheColourWithMoreToSpare(t *testing.T) {
	split := [5]int{cards.White: 9, cards.Green: 8}
	got := basicsBeside(split, []cards.Card{land(1, "Dual", "WG")}, colours(cards.White, cards.Green))
	if want := ([5]int{cards.White: 8, cards.Green: 8}); got != want {
		t.Errorf("basics %v, want a Plains given up: %v", got, want)
	}
	got = basicsBeside(split, []cards.Card{land(1, "Green", "G")}, colours(cards.White, cards.Green))
	if want := ([5]int{cards.White: 9, cards.Green: 7}); got != want {
		t.Errorf("basics %v, want a Forest given up for a green land: %v", got, want)
	}
}

func TestALandOfASplashedColourDoesNotLeaveItWithoutABasicToGiveUp(t *testing.T) {
	// A red splash has one Mountain; two red lands take its place and
	// then a basic of the colour with most.
	split := [5]int{cards.Black: 8, cards.Red: 1, cards.Green: 8}
	brg := colours(cards.Black, cards.Red, cards.Green)
	got := basicsBeside(split, []cards.Card{land(1, "Red one", "R"), land(2, "Red two", "R")}, brg)
	if got[cards.Red] != 0 || got[cards.Black]+got[cards.Green] != 15 {
		t.Errorf("basics %v, want no Mountain and 15 others", got)
	}
}

func TestMoreOnColourLandsThanTheDeckHoldsPlaysThoseFixingMostFirst(t *testing.T) {
	var cs []cards.Card
	for i := range 20 {
		cs = append(cs, land(i, fmt.Sprintf("Green land %02d", i), "G"))
	}
	cs = append(cs, land(99, "Zzz dual", "BG"))
	d := Suggest(cs, colours(cards.Black, cards.Green))
	if len(d.Main) != Lands || d.Basics != ([5]int{}) {
		t.Errorf("deck %v with basics %v, want %d of the pool's lands and no basics", d.Main, d.Basics, Lands)
	}
	if !slices.Contains(d.Main, 99) {
		t.Errorf("deck %v leaves out the dual for lands of one colour", d.Main)
	}
}

func TestCopiesOfALandAreEachPlayed(t *testing.T) {
	cs := []cards.Card{creature(1, "a", "{G}", "common"), land(2, "Dual", "BG"), land(2, "Dual", "BG")}
	d := Suggest(cs, colours(cards.Black, cards.Green))
	if want := []int{1, 2, 2}; !slices.Equal(d.Main, want) {
		t.Errorf("deck %v, want both copies of the dual: %v", d.Main, want)
	}
	if d.Count() != 1+Lands {
		t.Errorf("%d cards, want the spell and %d lands", d.Count(), Lands)
	}
}
