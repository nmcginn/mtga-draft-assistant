package app

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/deck"
	"github.com/nmcginn/mtga-draft-assistant/internal/pool"
)

// Build is the deck the player is making from a finished draft's pool
// (decision 31). Until they choose, it is a suggestion worked out afresh
// from what is known, so cards' details arriving late still reach it; what
// they choose is kept here.
type Build struct {
	// Pool is the pool the build is from. A build for another pool is
	// not used, so the next draft starts afresh.
	Pool []int
	// Chosen is set once the player has chosen colours; Colours is then
	// their choice. Until then the build is in the pool's leading two.
	Chosen  bool
	Colours deck.Colours
	// Edited is set once the player has moved a card or changed a basic
	// land; Deck is then their deck. Until then it is the suggestion for
	// the colours. Choosing colours starts a new suggestion.
	Edited bool
	Deck   deck.Deck
	// Copied is set when the deck was copied to the clipboard, until it
	// next changes, so the window never says a copy is current when it is
	// not.
	Copied bool
}

// DeckColourToggled reports that the player chose or dropped a colour to
// build in.
type DeckColourToggled struct{ Colour cards.Colour }

// DeckCardMoved reports that the player moved one copy of Card into the
// deck from the sideboard (ToDeck), or out of it.
type DeckCardMoved struct {
	Card   int
	ToDeck bool
}

// DeckBasicChanged reports that the player added (By 1) or took out (By -1)
// a basic land of Colour.
type DeckBasicChanged struct {
	Colour cards.Colour
	By     int
}

// DeckCopied reports that the window put the deck on the clipboard.
type DeckCopied struct{}

func (DeckColourToggled) isMsg() {}
func (DeckCardMoved) isMsg()     {}
func (DeckBasicChanged) isMsg()  {}
func (DeckCopied) isMsg()        {}

// build is the build of the finished draft's pool as it stands, with the
// pool's cards whose details are known (copies repeated), and false when
// there is nothing to build: no finished draft, or none of its cards known.
func (m Model) build() ([]cards.Card, Build, bool) {
	d := m.Draft
	if d == nil || !d.Done {
		return nil, Build{}, false
	}
	known := make([]cards.Card, 0, len(d.Pool))
	for _, id := range d.Pool {
		if c, ok := m.card(d.Set, id); ok {
			known = append(known, c)
		}
	}
	if len(known) == 0 {
		return nil, Build{}, false
	}
	b := m.Build
	if !slices.Equal(b.Pool, d.Pool) {
		b = Build{Pool: slices.Clone(d.Pool)}
	}
	if !b.Chosen {
		b.Colours = deck.Leading(known)
	}
	if !b.Edited {
		b.Deck = deck.Suggest(known, b.Colours)
	}
	return known, b, true
}

// applyDeck is Apply for the messages the deck section sends. A message
// with no build to change (the draft started over before it was applied)
// changes nothing.
func (m Model) applyDeck(msg Msg) Model {
	known, b, ok := m.build()
	if !ok {
		return m
	}
	switch msg := msg.(type) {
	case DeckColourToggled:
		if msg.Colour < cards.White || msg.Colour > cards.Green {
			return m
		}
		b.Chosen = true
		b.Colours[msg.Colour] = !b.Colours[msg.Colour]
		b.Edited, b.Deck = false, deck.Deck{}
	case DeckCardMoved:
		inPool := 0
		for _, c := range known {
			if c.ArenaID == msg.Card {
				inPool++
			}
		}
		main := slices.Clone(b.Deck.Main)
		inDeck := 0
		for _, id := range main {
			if id == msg.Card {
				inDeck++
			}
		}
		switch {
		case msg.ToDeck && inDeck < inPool:
			main = append(main, msg.Card)
			slices.Sort(main)
		case !msg.ToDeck && inDeck > 0:
			main = slices.Delete(main, slices.Index(main, msg.Card), slices.Index(main, msg.Card)+1)
		default:
			return m
		}
		b.Edited, b.Deck.Main = true, main
	case DeckBasicChanged:
		if msg.Colour < cards.White || msg.Colour > cards.Green {
			return m
		}
		n := min(max(b.Deck.Basics[msg.Colour]+msg.By, 0), deck.Size)
		if n == b.Deck.Basics[msg.Colour] {
			return m
		}
		b.Edited = true
		b.Deck.Basics[msg.Colour] = n
	case DeckCopied:
		m.Build = b
		m.Build.Copied = true
		return m
	}
	b.Copied = false
	m.Build = b
	return m
}

// DeckScreen is the deck being built from a finished draft's pool, put into
// words for the window: the colours to choose from, the deck by kind, the
// basic lands, the rest of the pool, and the deck as Arena imports it.
type DeckScreen struct {
	// Heading names the section with the deck's size: "Your deck (40
	// cards)".
	Heading string
	// Colours is every colour, and whether the deck is in it.
	Colours []DeckColour
	// Note says how the deck was made and how to change it.
	Note string
	// Size says how far from 40 the deck is, and what to do; empty at 40.
	Size string
	// Summary counts the deck up as the picks panel counts the pool: its
	// kinds and its curve.
	Summary *PicksSummary
	// Groups is the deck's creatures, other spells, and the pool's lands
	// in it, each up the curve; a group with no cards is left out, but
	// the lands are always there, for Basics.
	Groups []DeckGroup
	// Basics is the basic lands, one for each colour chosen or in the
	// deck, with how many: "8 Forest".
	Basics []DeckBasic
	// SideboardHeading and Sideboard are the rest of the pool: what the
	// colours can play first, then the rest, each up the curve.
	SideboardHeading string
	Sideboard        []DeckLine
	// Unknown says that some of the pool cannot be built with, because
	// its details are not known; empty when all is known.
	Unknown string
	// Export is the deck in Arena's deck-import format, with the rest of
	// the pool as its sideboard, for the clipboard.
	Export string
	// Copied confirms the copy, while the deck is as it was copied.
	Copied string
}

// DeckColour is one colour the deck can be built in.
type DeckColour struct {
	Colour cards.Colour
	Letter string
	Name   string
	Chosen bool
}

// DeckGroup is the deck's cards of one kind, under a heading with their
// count: "Creatures (15)".
type DeckGroup struct {
	Heading string
	Cards   []DeckLine
}

// DeckLine is a line of the deck or sideboard: one card, with how many
// copies, and its cost, "2× Gilded Pathfinder · 1G". Clicking it moves one
// copy of Card.
type DeckLine struct {
	Card int
	Text string
}

// DeckBasic is how many basic lands of Colour the deck has: "8 Forest".
type DeckBasic struct {
	Colour cards.Colour
	Text   string
}

// deckScreen describes the build of a finished draft's pool; nil when there
// is nothing to build.
func (m Model) deckScreen() *DeckScreen {
	known, b, ok := m.build()
	if !ok {
		return nil
	}
	var main, side []cards.Card
	left := slices.Clone(b.Deck.Main)
	for _, c := range known {
		if i := slices.Index(left, c.ArenaID); i >= 0 {
			main = append(main, c)
			left = slices.Delete(left, i, i+1)
		} else {
			side = append(side, c)
		}
	}

	s := &DeckScreen{Heading: fmt.Sprintf("Your deck (%s)", cardCount(b.Deck.Count()))}
	for _, c := range cards.Colours {
		s.Colours = append(s.Colours, DeckColour{Colour: c, Letter: c.Letter(), Name: c.Name(), Chosen: b.Colours[c]})
	}
	switch {
	case len(b.Colours.List()) == 0:
		s.Note = "Choose the colours to build in, and the cards they can cast are suggested."
	case b.Edited:
		s.Note = "Click a card to move it between the deck and the sideboard. Choosing colours again suggests a new deck."
	default:
		s.Note = fmt.Sprintf("Suggested from what these colours can cast: the rarest first, then creatures, "+
			"then the cheapest. Of %d lands, the pool's that make only these colours' mana, or all of them, "+
			"go in first, and basic lands make up the rest, shared by the colours' mana symbols. "+
			"Click a card to move it between the deck and the sideboard.", deck.Lands)
	}
	switch n := b.Deck.Count(); {
	case n < deck.Size:
		s.Size = fmt.Sprintf("%d short of %d: add cards from the sideboard, or basic lands.", deck.Size-n, deck.Size)
	case n > deck.Size:
		s.Size = fmt.Sprintf("%d over %d. Arena allows it, but at %d your best cards turn up most often.", n-deck.Size, deck.Size, deck.Size)
	}

	counted := slices.Clone(main)
	for _, c := range cards.Colours {
		for range b.Deck.Basics[c] {
			counted = append(counted, deck.Basic(c))
		}
	}
	if len(counted) > 0 {
		s.Summary = summarize(counted)
		// The colours chosen say what colours the deck is; counting a
		// hybrid card in both its colours would show a colour it does
		// not play.
		s.Summary.Colours = ""
	}

	slices.SortStableFunc(main, pool.ByCurveThenName)
	var creatures, others, lands []cards.Card
	for _, c := range main {
		switch c.Kind() {
		case cards.Creature:
			creatures = append(creatures, c)
		case cards.Land:
			lands = append(lands, c)
		default:
			others = append(others, c)
		}
	}
	basics := 0
	for _, c := range cards.Colours {
		if n := b.Deck.Basics[c]; n > 0 || b.Colours[c] {
			s.Basics = append(s.Basics, DeckBasic{Colour: c, Text: fmt.Sprintf("%d %s", n, deck.Basic(c).Name)})
			basics += n
		}
	}
	for _, g := range []struct {
		name string
		cs   []cards.Card
		n    int
	}{{"Creatures", creatures, len(creatures)}, {"Other spells", others, len(others)}, {"Lands", lands, len(lands) + basics}} {
		if len(g.cs) == 0 && g.name != "Lands" {
			continue
		}
		s.Groups = append(s.Groups, DeckGroup{Heading: fmt.Sprintf("%s (%d)", g.name, g.n), Cards: deckLines(g.cs)})
	}

	slices.SortStableFunc(side, func(a, b2 cards.Card) int {
		return cmp.Or(
			cmp.Compare(sideRank(a, b.Colours), sideRank(b2, b.Colours)),
			pool.ByCurveThenName(a, b2),
		)
	})
	s.SideboardHeading = fmt.Sprintf("Sideboard (%d)", len(side))
	s.Sideboard = deckLines(side)

	switch missing := len(m.Draft.Pool) - len(known); {
	case missing == 1:
		s.Unknown = "1 card of the pool can't be built with until its details are known."
	case missing > 1:
		s.Unknown = fmt.Sprintf("%d cards of the pool can't be built with until their details are known.", missing)
	}

	s.Export = deck.Export(slices.Concat(creatures, others, lands), b.Deck.Basics, side)
	if b.Copied {
		s.Copied = fmt.Sprintf("Copied %s in Arena's deck-import format, with the rest of the pool as the sideboard.",
			cardCount(b.Deck.Count()))
	}
	return s
}

// sideRank puts the sideboard's cards the colours can play first: the
// spells they can cast, then the lands that make their mana. The other
// spells come next, then the other lands.
func sideRank(c cards.Card, colours deck.Colours) int {
	switch {
	case deck.Castable(c, colours):
		return 0
	case deck.OnColour(c, colours):
		return 1
	case c.Kind() == cards.Land:
		return 3
	}
	return 2
}

// deckLines is cs a line each, copies of a card (the same Arena id) on one
// line, in order. cs keeps copies next to each other.
func deckLines(cs []cards.Card) []DeckLine {
	var out []DeckLine
	for i := 0; i < len(cs); {
		j := i + 1
		for j < len(cs) && cs[j].ArenaID == cs[i].ArenaID {
			j++
		}
		text := cs[i].Name
		if cost := cards.Mana(cs[i].ManaCost); cost != "" {
			text += " · " + cost
		}
		if n := j - i; n > 1 {
			text = fmt.Sprintf("%d× %s", n, text)
		}
		out = append(out, DeckLine{Card: cs[i].ArenaID, Text: text})
		i = j
	}
	return out
}
