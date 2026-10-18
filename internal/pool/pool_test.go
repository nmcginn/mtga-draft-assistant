package pool

import (
	"testing"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

var (
	elf      = cards.Card{Name: "Thornwood Scout", ManaCost: "{G}", TypeLine: "Creature — Elf Scout"}
	druid    = cards.Card{Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid"}
	knight   = cards.Card{Name: "Duskmarch Paladin", ManaCost: "{W/B}{W/B}", TypeLine: "Creature — Human Knight"}
	litany   = cards.Card{Name: "Ember Litany", ManaCost: "{X}{R}", TypeLine: "Sorcery"}
	lantern  = cards.Card{Name: "Gale Lantern", ManaCost: "{2}", TypeLine: "Artifact"}
	pathway  = cards.Card{Name: "Mossback Pathway", TypeLine: "Land"}
	colossus = cards.Card{Name: "Colossus", ManaCost: "{9}", TypeLine: "Artifact Creature — Golem"}
)

func TestNothingDraftedCountsUpToNothing(t *testing.T) {
	if got := Summarize(nil); got != (Summary{}) {
		t.Errorf("Summarize(nil) = %+v, want zero", got)
	}
}

func TestAPoolIsCountedByColourCurveAndKind(t *testing.T) {
	s := Summarize([]cards.Card{elf, druid, knight, litany, lantern, pathway})
	if want := [5]int{1, 0, 1, 1, 2}; s.Colour != want {
		t.Errorf("Colour = %v, want %v (W U B R G)", s.Colour, want)
	}
	if s.Colourless != 1 {
		t.Errorf("Colourless = %d, want the artifact", s.Colourless)
	}
	if want := [CurveTop + 1]int{0, 2, 2, 0, 1, 0, 0}; s.Curve != want {
		t.Errorf("Curve = %v, want %v", s.Curve, want)
	}
	if s.Creatures != 3 || s.Others != 2 || s.Lands != 1 {
		t.Errorf("Creatures, Others, Lands = %d, %d, %d; want 3, 2, 1", s.Creatures, s.Others, s.Lands)
	}
	if s.Spells() != 5 {
		t.Errorf("Spells() = %d, want 5", s.Spells())
	}
}

func TestAGoldCardCountsInEachOfItsColours(t *testing.T) {
	s := Summarize([]cards.Card{knight})
	if s.Colour[cards.White] != 1 || s.Colour[cards.Black] != 1 || s.Colourless != 0 {
		t.Errorf("Summarize(knight) = %+v, want one white, one black, no colourless", s)
	}
}

func TestLandsAreOnNeitherTheCurveNorTheColours(t *testing.T) {
	s := Summarize([]cards.Card{pathway, pathway})
	if s.Lands != 2 || s.Spells() != 0 || s.Colourless != 0 || s.Curve != ([CurveTop + 1]int{}) {
		t.Errorf("two lands = %+v, want only Lands counted", s)
	}
}

func TestExpensiveSpellsShareTheCurvesLastColumn(t *testing.T) {
	big := cards.Card{ManaCost: "{6}", TypeLine: "Sorcery"}
	s := Summarize([]cards.Card{big, colossus})
	if s.Curve[CurveTop] != 2 {
		t.Errorf("Curve = %v, want both in the %d+ column", s.Curve, CurveTop)
	}
}

func TestACardWithNoDetailsCountsAsAColourlessFreeSpell(t *testing.T) {
	// The app leaves unknown cards out before it counts (it says so in the
	// window); this pins what Summarize itself does with an empty card, so
	// that is a choice and not an accident.
	s := Summarize([]cards.Card{{}})
	if s.Others != 1 || s.Colourless != 1 || s.Curve[0] != 1 {
		t.Errorf("Summarize(empty card) = %+v", s)
	}
}
