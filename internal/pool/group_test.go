package pool

import (
	"slices"
	"testing"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// names is each group's cards by name, for comparing a layout at a glance.
func names(gs []Group) [][]string {
	out := make([][]string, len(gs))
	for i, g := range gs {
		for _, c := range g.Cards {
			out[i] = append(out[i], c.Name)
		}
	}
	return out
}

func TestNothingDraftedHasNoGroups(t *testing.T) {
	if got := Groups(nil); got != nil {
		t.Errorf("Groups(nil) = %+v, want none", got)
	}
}

func TestAPoolIsGroupedByColourThenGoldColourlessAndLands(t *testing.T) {
	courier := cards.Card{Name: "Ashen Courier", ManaCost: "{1}{B}", TypeLine: "Creature — Human Rogue"}
	gs := Groups([]cards.Card{pathway, lantern, litany, druid, courier, elf, knight})
	// Green has two spells, and black two with the gold knight's half.
	// Level, they keep WUBRG order.
	want := []Group{
		{Heap: Mono, Colour: cards.Black},
		{Heap: Mono, Colour: cards.Green},
		{Heap: Mono, Colour: cards.Red},
		{Heap: Multicolour},
		{Heap: Colourless},
		{Heap: Lands},
	}
	if len(gs) != len(want) {
		t.Fatalf("groups %v, want %d", names(gs), len(want))
	}
	for i, g := range gs {
		if g.Heap != want[i].Heap || g.Colour != want[i].Colour {
			t.Errorf("group %d is heap %d colour %d, want heap %d colour %d", i, g.Heap, g.Colour, want[i].Heap, want[i].Colour)
		}
	}
	wantNames := [][]string{
		{"Ashen Courier"},
		{"Thornwood Scout", "Verdant Oathkeeper"},
		{"Ember Litany"},
		{"Duskmarch Paladin"},
		{"Gale Lantern"},
		{"Mossback Pathway"},
	}
	if got := names(gs); !slices.EqualFunc(got, wantNames, slices.Equal) {
		t.Errorf("groups %q, want %q", got, wantNames)
	}
}

func TestColoursLeadInTheOrderTheSummaryCountsThem(t *testing.T) {
	// Green has two cards of its own and black one, but two gold
	// black-and-red cards count in black as the summary line counts them,
	// so black leads, as it does on the line over the groups.
	courier := cards.Card{Name: "Ashen Courier", ManaCost: "{1}{B}", TypeLine: "Creature — Human Rogue"}
	rakdos := cards.Card{Name: "Cinder Fiend", ManaCost: "{B}{R}", TypeLine: "Creature — Devil"}
	gs := Groups([]cards.Card{elf, druid, courier, rakdos, rakdos})
	if len(gs) != 3 || gs[0].Colour != cards.Black || gs[1].Colour != cards.Green || gs[2].Heap != Multicolour {
		t.Errorf("groups %q, want black, green, then gold", names(gs))
	}
}

func TestAHeapRunsUpTheCurveThenByNameWithCopiesTogether(t *testing.T) {
	big := cards.Card{ArenaID: 3, Name: "Ancient Oak", ManaCost: "{5}{G}", TypeLine: "Creature — Treefolk"}
	a := cards.Card{ArenaID: 2, Name: "Bear", ManaCost: "{1}{G}", TypeLine: "Creature — Bear"}
	b := a
	b.ArenaID = 1
	gs := Groups([]cards.Card{big, a, druid, elf, b})
	if got, want := names(gs)[0], []string{"Thornwood Scout", "Bear", "Bear", "Verdant Oathkeeper", "Ancient Oak"}; !slices.Equal(got, want) {
		t.Errorf("green heap %q, want %q", got, want)
	}
	if gs[0].Cards[1].ArenaID != 1 || gs[0].Cards[2].ArenaID != 2 {
		t.Error("two prints of one name are not in Arena id order")
	}
}

func TestAPoolOfOnlyLandsIsOneHeap(t *testing.T) {
	forest := cards.Card{Name: "Forest", TypeLine: "Basic Land — Forest"}
	gs := Groups([]cards.Card{pathway, forest})
	if len(gs) != 1 || gs[0].Heap != Lands || !slices.Equal(names(gs)[0], []string{"Forest", "Mossback Pathway"}) {
		t.Errorf("groups %q, want the two lands in one heap", names(gs))
	}
}

func TestAHybridCardIsGoldAndAnArtifactCreatureIsColourless(t *testing.T) {
	gs := Groups([]cards.Card{knight, colossus})
	if len(gs) != 2 || gs[0].Heap != Multicolour || gs[1].Heap != Colourless {
		t.Errorf("groups %+v, want the hybrid knight gold and the golem colourless", gs)
	}
}

func TestGroupingLeavesTheCallersSliceAsItWas(t *testing.T) {
	cs := []cards.Card{druid, elf}
	Groups(cs)
	if cs[0].Name != druid.Name {
		t.Error("Groups reordered the slice it was given")
	}
}
