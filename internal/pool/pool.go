// Package pool sums up a set of drafted cards at a glance: how many of each
// colour, the curve of their mana values, and how many are creatures. It is
// pure, so the panel beside the pack is tested without a window.
package pool

import "github.com/nmcginn/mtga-draft-assistant/internal/cards"

// CurveTop is the last column of the curve, which holds every spell of that
// mana value or more: past six, the exact value no longer changes a pick.
const CurveTop = 6

// Summary is a pool counted up. Lands are counted on their own and nowhere
// else: they have no colour of their own in a cost, and no place on a curve
// of spells.
type Summary struct {
	// Colour is how many spells ask for each colour, in WUBRG order. A
	// card of two colours counts once in each, so these can add up to
	// more than the spells.
	Colour [5]int
	// Colourless is how many spells ask for no colour: artifacts, mostly.
	Colourless int
	// Curve is how many spells there are at each mana value, from 0 to
	// CurveTop, the last holding CurveTop and above.
	Curve [CurveTop + 1]int
	// Creatures, Others and Lands split the pool by what each card is.
	Creatures, Others, Lands int
}

// Summarize counts up cs.
func Summarize(cs []cards.Card) Summary {
	var s Summary
	for _, c := range cs {
		switch c.Kind() {
		case cards.Land:
			s.Lands++
			continue
		case cards.Creature:
			s.Creatures++
		default:
			s.Others++
		}
		colours := c.Colours()
		for _, col := range colours {
			s.Colour[col]++
		}
		if len(colours) == 0 {
			s.Colourless++
		}
		s.Curve[min(c.ManaValue(), CurveTop)]++
	}
	return s
}

// Spells is how many of the pool are not lands.
func (s Summary) Spells() int {
	return s.Creatures + s.Others
}
