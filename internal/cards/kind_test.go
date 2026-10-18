package cards

import (
	"slices"
	"testing"
)

func TestACardsColoursAreTheColoursItsCostAsksFor(t *testing.T) {
	for cost, want := range map[string][]Colour{
		"{2}{G}{G}":          {Green},
		"{W/B}{W/B}":         {White, Black},
		"{3}":                nil,
		"":                   nil, // a land
		"{X}{R}":             {Red},
		"{G/P}":              {Green},
		"{2/W}":              {White},
		"{C}{C}":             nil,
		"{G}{U}{B}{R}{W}":    {White, Blue, Black, Red, Green},
		"{1}{W} // {3}{U}":   {White, Blue}, // either face may be cast
		"{2}{G":              nil,           // cut off: only whole symbols count
		"Bursting {R} Blast": {Red},
	} {
		if got := (Card{ManaCost: cost}).Colours(); !slices.Equal(got, want) {
			t.Errorf("Colours of %q = %v, want %v", cost, got, want)
		}
	}
}

func TestLettersOutsideACostsBracesAreNotColours(t *testing.T) {
	// "BURG" in a name-like string outside braces would read as four
	// colours if letters were taken from anywhere.
	if got := (Card{ManaCost: "BURG {1}"}).Colours(); got != nil {
		t.Errorf("Colours = %v, want none", got)
	}
}

func TestAColoursLetterIsHowACostWritesIt(t *testing.T) {
	var got string
	for _, c := range Colours {
		got += c.Letter()
	}
	if got != "WUBRG" {
		t.Errorf("letters in order = %q, want WUBRG", got)
	}
}

func TestManaValueCountsTheFrontFacesCostTheWayTheGameDoes(t *testing.T) {
	for cost, want := range map[string]int{
		"{2}{G}{G}":        4,
		"{G}":              1,
		"":                 0,
		"{0}":              0,
		"{X}{R}":           1,
		"{X}{X}{G}":        1,
		"{W/B}{W/B}":       2,
		"{2/W}{2/W}":       4,
		"{G/P}":            1,
		"{G/U/P}":          1,
		"{10}":             10,
		"{C}{S}":           2,
		"{1}{W} // {3}{W}": 2, // cast from its front
		"{2}{G":            2, // cut off: only whole symbols count
	} {
		if got := (Card{ManaCost: cost}).ManaValue(); got != want {
			t.Errorf("ManaValue of %q = %d, want %d", cost, got, want)
		}
	}
}

func TestACardsKindIsReadFromItsFrontFacesTypes(t *testing.T) {
	for line, want := range map[string]Kind{
		"Creature — Elf Druid":                    Creature,
		"Legendary Creature — Vampire Noble":      Creature,
		"Artifact Creature — Golem":               Creature,
		"Enchantment Creature — God":              Creature,
		"Sorcery":                                 OtherSpell,
		"Instant":                                 OtherSpell,
		"Artifact — Equipment":                    OtherSpell,
		"Land":                                    Land,
		"Basic Land — Forest":                     Land,
		"Land Creature — Forest Dryad":            Land, // played as a land
		"Creature — Human Soldier // Creature":    Creature,
		"Sorcery // Land":                         OtherSpell, // its front is cast
		"Enchantment — Aura":                      OtherSpell,
		"Kindred Instant — Elf":                   OtherSpell,
		"Legendary Planeswalker — Someone":        OtherSpell,
		"":                                        OtherSpell, // no details: counted as neither creature nor land
		"Instant — Creatures Are Not A Type Here": OtherSpell, // a subtype is not a type
	} {
		if got := (Card{TypeLine: line}).Kind(); got != want {
			t.Errorf("Kind of %q = %v, want %v", line, got, want)
		}
	}
}
