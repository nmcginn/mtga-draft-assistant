package app

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/pool"
)

// PicksSummary is the picks so far, or a finished draft's pool, counted up
// and put into words: what colours they lean to, how many are creatures,
// and their curve (decision 28).
type PicksSummary struct {
	// Colours is how many spells ask for each colour, most first:
	// "B 6 · G 4 · W 1 · Colourless 1". Empty when no spell is known.
	Colours string
	// Kinds is the split by what the cards are: "9 creatures · 4 other
	// spells · 1 land". Empty when no card is known.
	Kinds string
	// Curve is a column for each mana value, from 1 to "6+", and 0 when a
	// spell costs nothing. Nil when no spell is known.
	Curve []CurveColumn
	// Unknown says that some cards are left out of the counts, because
	// their details are not known; empty when none are.
	Unknown string
}

// CurveColumn is one mana value on the curve, and how many spells have it.
type CurveColumn struct {
	Label string
	Count int
}

// picksSummary counts up ids, a draft's picks or its pool, from the cards
// known for set. Nil when there are no ids.
func (m Model) picksSummary(set string, ids []int) *PicksSummary {
	if len(ids) == 0 {
		return nil
	}
	known := make([]cards.Card, 0, len(ids))
	for _, id := range ids {
		if c, ok := m.card(set, id); ok {
			known = append(known, c)
		}
	}
	out := &PicksSummary{}
	switch missing := len(ids) - len(known); {
	case len(known) == 0:
		out.Unknown = "Colours, creatures and curve appear once the cards' details are known."
		return out
	case missing == 1:
		out.Unknown = "Leaves out 1 card whose details aren't known."
	case missing > 1:
		out.Unknown = fmt.Sprintf("Leaves out %d cards whose details aren't known.", missing)
	}

	sum := summarize(known)
	sum.Unknown = out.Unknown
	return sum
}

// summarize counts up cs, every one of whose details are known.
func summarize(cs []cards.Card) *PicksSummary {
	out := &PicksSummary{}
	s := pool.Summarize(cs)
	out.Kinds = kindsLine(s)
	if s.Spells() == 0 {
		return out
	}
	out.Colours = coloursLine(s)
	for mv, n := range s.Curve {
		if mv == 0 && n == 0 {
			// A spell that costs nothing is rare enough that an
			// empty column for it is noise.
			continue
		}
		label := strconv.Itoa(mv)
		if mv == pool.CurveTop {
			label += "+"
		}
		out.Curve = append(out.Curve, CurveColumn{Label: label, Count: n})
	}
	return out
}

// coloursLine is each colour any spell asks for, with how many do, the
// most first so the pool's main colours lead; colours level with each
// other keep WUBRG order. Colourless spells come last, since they go in
// any deck.
func coloursLine(s pool.Summary) string {
	order := slices.Clone(cards.Colours)
	slices.SortStableFunc(order, func(a, b cards.Colour) int { return cmp.Compare(s.Colour[b], s.Colour[a]) })
	var parts []string
	for _, c := range order {
		if s.Colour[c] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", c.Letter(), s.Colour[c]))
		}
	}
	if s.Colourless > 0 {
		parts = append(parts, fmt.Sprintf("Colourless %d", s.Colourless))
	}
	return strings.Join(parts, " · ")
}

// kindsLine is the split by creature, other spell, and land. Creatures and
// other spells are always said, since none of either is worth knowing;
// lands only when there are some.
func kindsLine(s pool.Summary) string {
	parts := []string{
		count(s.Creatures, "creature", "creatures"),
		count(s.Others, "other spell", "other spells"),
	}
	if s.Lands > 0 {
		parts = append(parts, count(s.Lands, "land", "lands"))
	}
	return strings.Join(parts, " · ")
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// PoolGroup is one heap of a finished draft's pool: a colour, the gold
// cards, the colourless spells, the lands, or the cards whose details are
// not known.
type PoolGroup struct {
	// Heading names the heap and how many cards are in it: "Green (12)".
	Heading string
	// Cards is the heap's cards up the curve, one line each, with copies
	// on one line: "2× Gilded Pathfinder · 1G". A land has no cost.
	Cards []string
}

// poolGroups lays out ids, a finished draft's pool, by colour (decision 30),
// with any cards whose details are not known in a heap of their own at the
// end, by id. Nil when no card's details are known, since then there is
// nothing to group by.
func (m Model) poolGroups(set string, ids []int) []PoolGroup {
	var known []cards.Card
	var unknown []int
	for _, id := range ids {
		if c, ok := m.card(set, id); ok {
			known = append(known, c)
		} else {
			unknown = append(unknown, id)
		}
	}
	if len(known) == 0 {
		return nil
	}
	var out []PoolGroup
	for _, g := range pool.Groups(known) {
		lines := make([]string, len(g.Cards))
		for i, c := range g.Cards {
			lines[i] = c.Name
			if cost := cards.Mana(c.ManaCost); cost != "" {
				lines[i] += " · " + cost
			}
		}
		out = append(out, poolGroup(heapName(g), lines))
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		lines := make([]string, len(unknown))
		for i, id := range unknown {
			lines[i] = cards.Unnamed(id)
		}
		out = append(out, poolGroup("Details not known", lines))
	}
	return out
}

// poolGroup is a heap headed name, its lines one per card in order, with
// copies of a card (the same line, so the same name and cost: two prints
// of a card are one card to a deck) folded into one. The heap's order keeps
// copies next to each other.
func poolGroup(name string, lines []string) PoolGroup {
	g := PoolGroup{Heading: fmt.Sprintf("%s (%d)", name, len(lines))}
	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		if n := j - i; n > 1 {
			g.Cards = append(g.Cards, fmt.Sprintf("%d× %s", n, lines[i]))
		} else {
			g.Cards = append(g.Cards, lines[i])
		}
		i = j
	}
	return g
}

// heapName is how a heap of the pool is headed.
func heapName(g pool.Group) string {
	switch g.Heap {
	case pool.Multicolour:
		return "Multicolour"
	case pool.Colourless:
		return "Colourless"
	case pool.Lands:
		return "Lands"
	}
	return g.Colour.Name()
}
