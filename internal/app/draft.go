package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/draft"
)

// DraftScreen is what the window shows while there is a draft: which draft,
// where in it the player is, the pack, and the picks so far, already put into
// words. Like Status, it lives here so that what the user reads is decided
// and tested in one place, and the view only lays it out.
type DraftScreen struct {
	// Title names the draft: "FRA · Premier Draft".
	Title string
	// Position is where in the draft the player is: "Pack 1 · Pick 3", or
	// "Draft complete". Empty before the first pack.
	Position string
	// Notice says why cards are shown by their Arena id rather than by
	// name, when they are: still loading, Scryfall out of reach. Empty
	// when there is nothing to explain.
	Notice string
	// Heading is the line over the pack: how many cards it holds, or what
	// the player is waiting for.
	Heading string
	// Cards is the pack, in Arena's order.
	Cards []CardLine
	// PicksHeading is the line over the picks, with their count; empty
	// before the first pick.
	PicksHeading string
	// Summary counts up the picks, or the pool once the draft is done:
	// colours, creatures, and curve. Nil when PicksHeading is empty.
	Summary *PicksSummary
	// Picks is the picks so far, one line for each pack they came from;
	// or a finished draft's pool, the same way, while none of its cards'
	// details are known to group it by.
	Picks []string
	// Pool is a finished draft's pool, grouped by colour to build a deck
	// from (decision 30); nil before the draft ends, and while none of its
	// cards' details are known, when Picks lists it instead.
	Pool []PoolGroup
	// Deck is the deck being built from a finished draft's pool
	// (decision 31); nil before the draft ends, and while none of its
	// cards' details are known.
	Deck *DeckScreen
	// Footnote is a smaller note under it all.
	Footnote string
}

// CardLine is one card in the pack: its name, and under it its mana cost,
// type, and rarity. A card with no details is named by its Arena id, and its
// Detail says why when the reason is the card's own (Scryfall does not have
// it yet); when it is the whole set's, the screen's Notice says so once.
// Image is the card's picture on disk, once it is there; until then the
// card is shown by its name and details.
type CardLine struct {
	Name   string
	Detail string
	Image  string
}

// ShowsDraft reports whether the window shows the draft (DraftScreen) rather
// than Status. A failure, or a log that cannot be read, needs the user to do
// something, so it is shown instead: a draft that has stopped updating would
// otherwise look like one that is up to date.
func (m Model) ShowsDraft() bool {
	return m.Phase == PhaseFollowing && m.Draft != nil && m.Failure == nil && m.Unreadable == ""
}

// formatNames are the draft formats as Arena's own menus name them.
var formatNames = map[arena.Format]string{
	arena.PremierDraft: "Premier Draft",
	arena.QuickDraft:   "Quick Draft",
}

// DraftScreen describes the draft for the user. It is only meaningful while
// ShowsDraft.
func (m Model) DraftScreen() DraftScreen {
	d := m.Draft
	if d == nil {
		return DraftScreen{}
	}
	name := func(id int) string { return m.cardName(d.Set, id) }
	s := DraftScreen{Title: draftTitle(*d)}

	var shown []int // every card id on screen, to say why any is unnamed
	if d.Done {
		s.Position = "Draft complete"
		s.Heading = "Start another draft in Arena and its first pack appears here."
		s.PicksHeading = fmt.Sprintf("Your pool (%s)", cardCount(len(d.Pool)))
		if s.Pool = m.poolGroups(d.Set, d.Pool); s.Pool == nil {
			s.Picks = poolLines(*d, name)
		}
		s.Summary = m.picksSummary(d.Set, d.Pool)
		s.Deck = m.deckScreen()
		if s.Deck == nil && s.Summary != nil && s.Summary.Kinds == "" {
			// Nothing of the pool is known yet: the deck is coming
			// with the counts, and the window says so.
			s.Summary.Unknown = "Colours, creatures, curve and a deck to build appear once the cards' details are known."
		}
		shown = d.Pool
	} else {
		if d.Pack > 0 {
			s.Position = fmt.Sprintf("Pack %d · Pick %d", d.Pack, d.Pick)
		}
		switch card, picked := d.Picked(); {
		case d.Cards == nil:
			s.Heading = "Waiting for the first pack."
		case picked:
			s.Heading = fmt.Sprintf("You took %s. Waiting for the next pack.", name(card))
			shown = append(shown, card)
		default:
			s.Heading = fmt.Sprintf("%s in this pack", cardCount(len(d.Cards)))
			s.Cards = make([]CardLine, len(d.Cards))
			for i, c := range d.Cards {
				s.Cards[i] = m.cardLine(d.Set, c)
				s.Cards[i].Image = m.Images[c].Path
			}
			shown = append(shown, d.Cards...)
		}
		if len(d.Picks) > 0 {
			s.PicksHeading = fmt.Sprintf("Your picks (%d)", len(d.Picks))
			s.Picks = pickLines(d.Picks, name)
			picked := make([]int, len(d.Picks))
			for i, p := range d.Picks {
				picked[i] = p.Card
			}
			s.Summary = m.picksSummary(d.Set, picked)
			shown = append(shown, picked...)
		}
	}
	s.Notice = m.cardsNotice(d.Set, shown)
	if s.Notice == "" {
		s.Notice = m.imagesNotice(m.ImagesShown())
	}

	switch {
	case !m.CaughtUp:
		// The pack on screen may be one from earlier in the session that
		// the log has not got past yet.
		s.Footnote = "Catching up on Arena's log; the pack shown may not be the latest yet."
	case m.CacheProblem != "" || m.ImageProblem != "":
		s.Footnote = m.cacheNote()
	case slices.ContainsFunc(s.Cards, func(c CardLine) bool { return c.Image != "" }):
		s.Footnote = "Card details and images from Scryfall."
	case len(m.CardSets[d.Set].Cards) > 0:
		s.Footnote = "Card details from Scryfall."
	}
	return s
}

// draftTitle names the draft by its set and format, or says that it is a
// draft at all when the log never said which.
func draftTitle(d draft.Draft) string {
	if d.Set == "" {
		if d.Done {
			return "Draft"
		}
		return "Draft in progress"
	}
	name, ok := formatNames[d.Format]
	if !ok {
		name = string(d.Format)
	}
	return d.Set + " · " + name
}

func cardCount(n int) string {
	if n == 1 {
		return "1 card"
	}
	return fmt.Sprintf("%d cards", n)
}

// pickLines groups picks, in the order taken, into one line per pack, each
// card named by name: "Pack 1: Gilded Pathfinder, Card 106225". Picks whose
// pack the log did not say are "Earlier".
func pickLines(picks []draft.Pick, name func(int) string) []string {
	var lines []string
	var line strings.Builder
	for i, p := range picks {
		if i == 0 || p.Pack != picks[i-1].Pack {
			if i > 0 {
				lines = append(lines, line.String())
				line.Reset()
			}
			if p.Pack == 0 {
				line.WriteString("Earlier: ")
			} else {
				fmt.Fprintf(&line, "Pack %d: ", p.Pack)
			}
		} else {
			line.WriteString(", ")
		}
		line.WriteString(name(p.Card))
	}
	if len(picks) > 0 {
		lines = append(lines, line.String())
	}
	return lines
}

// poolLines is a finished draft's pool while none of its cards' details are
// known, so it cannot be grouped by colour. When the picks read are exactly
// the pool, they are shown by pack, in the order taken; otherwise some picks
// were missed (the log began mid-draft), and the pool is shown as Arena gave
// it.
func poolLines(d draft.Draft, name func(int) string) []string {
	if len(d.Pool) == 0 {
		return nil
	}
	picked := make([]int, len(d.Picks))
	for i, p := range d.Picks {
		picked[i] = p.Card
	}
	pool := slices.Clone(d.Pool)
	slices.Sort(picked)
	slices.Sort(pool)
	if slices.Equal(picked, pool) {
		return pickLines(d.Picks, name)
	}
	names := make([]string, len(d.Pool))
	for i, c := range d.Pool {
		names[i] = name(c)
	}
	return []string{strings.Join(names, ", ")}
}
