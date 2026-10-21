package pool

import (
	"cmp"
	"slices"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// Heap is which group of a laid-out pool a card goes in.
type Heap int

const (
	// Mono is the cards of one colour, the group's Colour.
	Mono Heap = iota
	// Multicolour is the cards of two colours or more, which only go in
	// a deck that plays all of them.
	Multicolour
	// Colourless is the spells that go in any deck: artifacts, mostly.
	Colourless
	// Lands is the lands, which have no colour in a cost to sort them by.
	Lands
)

// Group is one heap of a laid-out pool, and the cards in it.
type Group struct {
	Heap Heap
	// Colour is the group's colour when Heap is Mono, and White (zero)
	// otherwise.
	Colour cards.Colour
	Cards  []cards.Card
}

// Groups lays cs out as a player sorts a pool on the table to build from it
// (decision 30): a heap for each colour, the colours the pool leans to first
// (the order of Summary.Colour, most first, ties in WUBRG order), then the
// multicoloured cards, the colourless spells, and the lands. Empty heaps are
// left out. Within a heap, cards run up the curve, then by name, so copies
// of a card sit together.
func Groups(cs []cards.Card) []Group {
	s := Summarize(cs)
	order := slices.Clone(cards.Colours)
	slices.SortStableFunc(order, func(a, b cards.Colour) int { return cmp.Compare(s.Colour[b], s.Colour[a]) })

	var mono [5][]cards.Card
	var multi, colourless, lands []cards.Card
	for _, c := range cs {
		if c.Kind() == cards.Land {
			lands = append(lands, c)
			continue
		}
		switch colours := c.Colours(); len(colours) {
		case 0:
			colourless = append(colourless, c)
		case 1:
			mono[colours[0]] = append(mono[colours[0]], c)
		default:
			multi = append(multi, c)
		}
	}

	var out []Group
	add := func(h Heap, col cards.Colour, cs []cards.Card) {
		if len(cs) == 0 {
			return
		}
		slices.SortStableFunc(cs, ByCurveThenName)
		out = append(out, Group{Heap: h, Colour: col, Cards: cs})
	}
	for _, col := range order {
		add(Mono, col, mono[col])
	}
	add(Multicolour, cards.White, multi)
	add(Colourless, cards.White, colourless)
	add(Lands, cards.White, lands)
	return out
}

// ByCurveThenName orders a heap: cheapest first, then by name. The Arena id
// last keeps two prints of one name in the same order every time.
func ByCurveThenName(a, b cards.Card) int {
	return cmp.Or(
		cmp.Compare(a.ManaValue(), b.ManaValue()),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.ArenaID, b.ArenaID),
	)
}
