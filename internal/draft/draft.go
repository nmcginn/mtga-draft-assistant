// Package draft is the draft under way, as plain data: which draft it is, the
// pack in front of the player, and the picks so far. Every change to it is a
// pure function of the events read from Arena's log, so a whole draft can be
// replayed in a test.
package draft

import (
	"slices"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
)

// Draft is one draft, from its first news in the log to its end. Its slices
// are never changed in place: Apply returns a new Draft with new slices, so a
// Draft held elsewhere (an older app.Model) keeps saying what it said.
type Draft struct {
	// Event, Set and Format say which draft this is. All three are empty
	// when the log showed a pack or a pick without saying what draft it
	// belongs to: Arena relaunched in the middle of a draft against people,
	// whose rejoining no fixture shows yet (decision 23).
	Event  string
	Set    string
	Format arena.Format

	// Pack and Pick are the position of the pack last shown, counting from
	// 1; both are 0 before the first pack arrives.
	Pack, Pick int
	// Cards is that pack, as Arena ids in Arena's order; nil before the
	// first pack, and once the draft is over.
	Cards []int

	// Picks are the cards taken, in the order taken.
	Picks []Pick

	// Done is set when picking is over, and Pool then holds every card
	// drafted, in Arena's order.
	Done bool
	Pool []int
}

// Pick is one card taken. Pack and Pick count from 1, and are both 0 when the
// log did not say where in the draft it was taken.
type Pick struct {
	Pack, Pick int
	Card       int
}

// Apply returns the draft after e, starting a new one where e says so. d is
// nil when no draft is under way, and so is the result for an event that
// starts nothing.
//
// A pack or a pick with no draft under way (or after the last one ended)
// starts a draft that does not know its event: showing the pack beats
// hiding it until the log says which draft it is in.
func Apply(d *Draft, e arena.Event) *Draft {
	switch e := e.(type) {
	case arena.DraftStarted:
		return &Draft{Event: e.Event, Set: e.Set, Format: e.Format}
	case arena.PackShown:
		next := ongoing(d)
		next.showPack(e)
		return next
	case arena.PickMade:
		next := ongoing(d)
		next.pick(Pick{Pack: e.Pack, Pick: e.Pick, Card: e.Card})
		return next
	case arena.DraftEnded:
		if d == nil {
			// The end of a draft whose start and picks were never seen
			// still says what was drafted.
			d = &Draft{}
		}
		next := *d
		next.Cards = nil
		next.Done = true
		next.Pool = slices.Clone(e.Pool)
		return &next
	}
	return d
}

// ongoing is a copy of d to change, or a new draft if d is nil or over.
func ongoing(d *Draft) *Draft {
	if d == nil || d.Done {
		return &Draft{}
	}
	next := *d
	return &next
}

// showPack puts the pack in front of the player. A pack from earlier in the
// draft than the one already shown is a late repeat of a pack picked from,
// not news, and is ignored.
func (d *Draft) showPack(e arena.PackShown) {
	if before(e.Pack, e.Pick, d.Pack, d.Pick) {
		return
	}
	d.Pack, d.Pick = e.Pack, e.Pick
	d.Cards = slices.Clone(e.Cards)
	if e.Picked != nil {
		d.Picks = withEarlier(d.Picks, e.Picked)
	}
}

// withEarlier is picks with any card in picked, Arena's own list of every card
// taken before the pack on show, that picks does not already hold. Those are
// picks made before this log began, so they go first, without a position:
// Arena's list is not in the order taken, so where each was taken is not
// known.
func withEarlier(picks []Pick, picked []int) []Pick {
	seen := make(map[int]int, len(picks))
	for _, p := range picks {
		seen[p.Card]++
	}
	var earlier []Pick
	for _, c := range picked {
		if seen[c] > 0 {
			seen[c]--
			continue
		}
		earlier = append(earlier, Pick{Card: c})
	}
	if earlier == nil {
		return picks
	}
	return append(earlier, picks...)
}

// pick records a card taken. Arena may send a pick again when it reconnects;
// a pick at a position already recorded replaces it rather than adding a
// second card.
func (d *Draft) pick(p Pick) {
	picks := slices.Clone(d.Picks)
	i := slices.IndexFunc(picks, func(q Pick) bool { return q.Pack != 0 && q.Pack == p.Pack && q.Pick == p.Pick })
	if i >= 0 {
		picks[i] = p
	} else {
		picks = append(picks, p)
	}
	d.Picks = picks
}

// Picked is the card taken from the pack on show, and whether one has been:
// once it has, the player is waiting for the next pack.
func (d Draft) Picked() (card int, ok bool) {
	if d.Cards == nil {
		return 0, false
	}
	for _, p := range d.Picks {
		if p.Pack == d.Pack && p.Pick == d.Pick {
			return p.Card, true
		}
	}
	return 0, false
}

// before reports whether pack a, pick b comes before pack c, pick d.
func before(a, b, c, d int) bool {
	return a < c || (a == c && b < d)
}
