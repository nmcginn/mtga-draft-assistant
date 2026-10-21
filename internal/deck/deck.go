// Package deck builds a 40-card deck from a draft's pool: which of its cards
// a deck of some colours can cast, a suggested 23 spells and 17 lands, how
// the basic lands split between the colours, and the deck written out in
// Arena's deck-import format. It is pure, so a build is tested without a
// window.
package deck

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/pool"
)

// A limited deck is at least Size cards, and the usual one is Spells spells
// and Lands lands.
const (
	Size   = 40
	Spells = 23
	Lands  = 17
)

// Colours is a choice of colours to play, indexed by cards.Colour.
type Colours [5]bool

// List is the colours chosen, in WUBRG order.
func (cs Colours) List() []cards.Colour {
	var out []cards.Colour
	for _, c := range cards.Colours {
		if cs[c] {
			out = append(out, c)
		}
	}
	return out
}

// Deck is a build from a pool: the pool's cards in it, by Arena id (a card
// the pool holds twice may be here twice), and how many of each basic land,
// by colour. Arena supplies basic lands to every limited deck, so they are
// not the pool's.
type Deck struct {
	Main   []int
	Basics [5]int
}

// Count is how many cards the deck holds, basic lands included.
func (d Deck) Count() int {
	n := len(d.Main)
	for _, b := range d.Basics {
		n += b
	}
	return n
}

// basicNames are the basic lands, in WUBRG order.
var basicNames = [...]string{"Plains", "Island", "Swamp", "Mountain", "Forest"}

// Basic is the basic land that makes colour c's mana, as a card: "Forest".
func Basic(c cards.Colour) cards.Card {
	return cards.Card{Name: basicNames[c], TypeLine: "Basic Land — " + basicNames[c]}
}

// Castable reports whether a deck of colours can cast c: every coloured
// symbol of its front face's cost can be paid with one of them. A hybrid
// symbol needs either of its halves ({W/B} with white alone; {2/G} with
// anything), and a Phyrexian one can be paid with life. The front face is
// the one that counts, as for the curve (decision 28): it is the face a
// two-faced card is cast as. A land is not cast; it is never castable.
func Castable(c cards.Card, colours Colours) bool {
	if c.Kind() == cards.Land {
		return false
	}
	front, _, _ := strings.Cut(c.ManaCost, " // ")
	for _, sym := range cards.Symbols(front) {
		if !payable(sym, colours) {
			return false
		}
	}
	return true
}

// payable reports whether one mana symbol (without its braces) can be paid
// in colours: one of its halves is not a colour, or is one of them.
func payable(sym string, colours Colours) bool {
	for half := range strings.SplitSeq(sym, "/") {
		col, ok := colourOf(half)
		if !ok || colours[col] {
			return true
		}
	}
	return false
}

// colourOf is the colour a symbol's half names, if it names one.
func colourOf(half string) (cards.Colour, bool) {
	for _, c := range cards.Colours {
		if half == c.Letter() {
			return c, true
		}
	}
	return 0, false
}

// Leading is the pool's two leading colours, as the picks panel lists them
// (decision 28): the most spells first, ties in WUBRG order. A pool whose
// spells ask for one colour or none leads with that many.
func Leading(cs []cards.Card) Colours {
	s := pool.Summarize(cs)
	order := slices.Clone(cards.Colours)
	slices.SortStableFunc(order, func(a, b cards.Colour) int { return cmp.Compare(s.Colour[b], s.Colour[a]) })
	var out Colours
	for _, c := range order[:2] {
		if s.Colour[c] > 0 {
			out[c] = true
		}
	}
	return out
}

// Suggest is a deck in colours from the cards of a pool (copies repeated):
// up to Spells of the spells it can cast, and Lands basic lands split
// between the colours. Until there are ratings to rank by, the spells are
// taken the rarest first, as a rare is likelier to be worth its slot,
// then creatures before other spells, as a limited deck wants most of its
// spells to be creatures, then the cheapest first, for a curve that
// starts early (decision 31). The pool's own lands are left for the
// player to add: what mana a non-basic land makes is in its rules text,
// which is not stored.
func Suggest(cs []cards.Card, colours Colours) Deck {
	var playable []cards.Card
	for _, c := range cs {
		if Castable(c, colours) {
			playable = append(playable, c)
		}
	}
	slices.SortStableFunc(playable, func(a, b cards.Card) int {
		return cmp.Or(
			cmp.Compare(rarityRank(b.Rarity), rarityRank(a.Rarity)),
			cmp.Compare(kindRank(a), kindRank(b)),
			cmp.Compare(a.ManaValue(), b.ManaValue()),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.ArenaID, b.ArenaID),
		)
	})
	playable = playable[:min(len(playable), Spells)]
	d := Deck{Main: make([]int, len(playable))}
	for i, c := range playable {
		d.Main[i] = c.ArenaID
	}
	slices.Sort(d.Main)
	d.Basics = SplitBasics(playable, colours, Lands)
	return d
}

// rarityRank orders rarities, the rarest highest. A rarity Scryfall adds
// later ranks with the commons.
func rarityRank(r string) int {
	switch r {
	case "mythic":
		return 3
	case "rare", "special", "bonus":
		return 2
	case "uncommon":
		return 1
	}
	return 0
}

// kindRank puts creatures before other spells.
func kindRank(c cards.Card) int {
	if c.Kind() == cards.Creature {
		return 0
	}
	return 1
}

// SplitBasics shares n basic lands between colours in proportion to how
// many symbols of each the spells' front faces ask for, the largest
// remainders taking what rounding leaves. A colour any spell asks for gets
// at least one, so a light splash still has a source. Spells that ask for
// none of the colours (colourless ones) leave the split even.
func SplitBasics(spells []cards.Card, colours Colours, n int) [5]int {
	var out [5]int
	chosen := colours.List()
	if n <= 0 || len(chosen) == 0 {
		return out
	}
	var pips [5]int
	total := 0
	for _, c := range spells {
		front, _, _ := strings.Cut(c.ManaCost, " // ")
		for _, sym := range cards.Symbols(front) {
			for half := range strings.SplitSeq(sym, "/") {
				if col, ok := colourOf(half); ok && colours[col] {
					pips[col]++
					total++
				}
			}
		}
	}
	if total == 0 {
		for _, c := range chosen {
			pips[c] = 1
		}
		total = len(chosen)
	}

	// Each colour asked for starts with one, while there are lands to
	// give, and the rest are shared by largest remainder.
	given := 0
	asked := slices.DeleteFunc(slices.Clone(chosen), func(c cards.Colour) bool { return pips[c] == 0 })
	for _, c := range asked {
		if given < n {
			out[c]++
			given++
		}
	}
	rest := n - given
	type share struct {
		c         cards.Colour
		remainder int
	}
	var shares []share
	for _, c := range asked {
		whole := rest * pips[c] / total
		out[c] += whole
		given += whole
		shares = append(shares, share{c, rest * pips[c] % total})
	}
	slices.SortStableFunc(shares, func(a, b share) int { return cmp.Compare(b.remainder, a.remainder) })
	for i := 0; given < n; i++ {
		out[shares[i%len(shares)].c]++
		given++
	}
	return out
}

// Export is a deck in the text Arena's deck import reads: "Deck", then a
// line for each card with how many ("2 Gilded Pathfinder"), then, if
// anything is left, a blank line, "Sideboard", and the rest of the pool
// the same way. Cards are named by their front face, as Arena names a
// two-faced card, and copies share a line, in the order first given;
// basic lands come after the deck's own cards.
func Export(main []cards.Card, basics [5]int, side []cards.Card) string {
	var b strings.Builder
	b.WriteString("Deck\n")
	deck := slices.Clone(main)
	for _, c := range cards.Colours {
		for range basics[c] {
			deck = append(deck, Basic(c))
		}
	}
	writeLines(&b, deck)
	if len(side) > 0 {
		b.WriteString("\nSideboard\n")
		writeLines(&b, side)
	}
	return b.String()
}

// writeLines writes cs a name to a line, with how many of each, in the
// order each name first appears.
func writeLines(b *strings.Builder, cs []cards.Card) {
	var names []string
	counts := map[string]int{}
	for _, c := range cs {
		name, _, _ := strings.Cut(c.Name, " // ")
		if counts[name] == 0 {
			names = append(names, name)
		}
		counts[name]++
	}
	for _, name := range names {
		b.WriteString(strconv.Itoa(counts[name]) + " " + name + "\n")
	}
}
