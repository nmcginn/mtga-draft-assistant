package cards

import (
	"slices"
	"strconv"
	"strings"
)

// The facts a summary of picks needs (colour, mana value, creature or not)
// are read from the mana cost and type line the card already carries, rather
// than from more of Scryfall's fields: they say all the panel needs, and the
// cache keeps working as it is (decision 28).

// Colour is one of Magic's five colours, in the order the game lists them.
type Colour int

const (
	White Colour = iota
	Blue
	Black
	Red
	Green
)

// Colours is every colour, in WUBRG order.
var Colours = []Colour{White, Blue, Black, Red, Green}

// colourLetters are the colours as a mana cost writes them.
const colourLetters = "WUBRG"

// Letter is the colour as a mana cost writes it: "W", "U", "B", "R", "G".
func (c Colour) Letter() string {
	return colourLetters[c : c+1]
}

// colourNames are the colours as words, in WUBRG order.
var colourNames = [...]string{"White", "Blue", "Black", "Red", "Green"}

// Name is the colour as a word: "White", "Blue", "Black", "Red", "Green".
func (c Colour) Name() string {
	return colourNames[c]
}

// Kind is what a card is, for counting a pool: a creature, a land, or any
// other spell.
type Kind int

const (
	OtherSpell Kind = iota
	Creature
	Land
)

// Colours is which colours c's mana cost asks for, on any of its faces, each
// once, in WUBRG order. A card with two castable faces is both faces' colours,
// since a pool can cast it as either. A colourless card, and a land, has none.
func (c Card) Colours() []Colour {
	letters := symbolLetters(c.ManaCost)
	var out []Colour
	for _, col := range Colours {
		if strings.Contains(letters, col.Letter()) {
			out = append(out, col)
		}
	}
	return out
}

// symbolLetters is what is inside the braces of cost, run together:
// "{2}{G/U}" is "2G/U". Text outside braces is not a symbol.
func symbolLetters(cost string) string {
	var b strings.Builder
	for _, sym := range symbols(cost) {
		b.WriteString(sym)
	}
	return b.String()
}

// symbols is the symbols of cost without their braces: "{2}{G/U}" is
// ["2", "G/U"]. A symbol left open is not read.
func symbols(cost string) []string {
	var out []string
	for {
		open := strings.IndexByte(cost, '{')
		if open < 0 {
			return out
		}
		closing := strings.IndexByte(cost[open:], '}')
		if closing < 0 {
			return out
		}
		out = append(out, cost[open+1:open+closing])
		cost = cost[open+closing+1:]
	}
}

// ManaValue is the card's mana value, as the curve counts it: its front
// face's cost, where {X} is 0, a hybrid symbol is its larger half ({2/W} is
// 2, {G/U} is 1), and a Phyrexian symbol is 1. Only the front face counts:
// a two-faced card is cast from its front, or its back is a land or a
// cheaper option. A split card ("{1}{R} // {2}{U}") is counted by its left
// half alone, where the game adds both halves: the joined cost cannot say
// which kind of two-faced card it is.
func (c Card) ManaValue() int {
	front, _, _ := strings.Cut(c.ManaCost, " // ")
	total := 0
	for _, sym := range symbols(front) {
		best := 0
		for part := range strings.SplitSeq(sym, "/") {
			best = max(best, symbolValue(part))
		}
		total += best
	}
	return total
}

// symbolValue is what one half of a mana symbol adds to a mana value.
func symbolValue(s string) int {
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	switch s {
	case "X", "Y", "Z", "P":
		// {X} is 0 off the stack; P is the Phyrexian half of {G/P},
		// whose other half counts.
		return 0
	}
	return 1
}

// Kind is whether the card's front face is a land, a creature, or another
// spell. A land creature is a land: it is played as one.
func (c Card) Kind() Kind {
	front, _, _ := strings.Cut(c.TypeLine, " // ")
	types, _, _ := strings.Cut(front, " — ")
	words := strings.Fields(types)
	switch {
	case slices.Contains(words, "Land"):
		return Land
	case slices.Contains(words, "Creature"):
		return Creature
	}
	return OtherSpell
}
