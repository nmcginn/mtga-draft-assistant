package app

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"strings"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// CardSet is what the window knows of one set's cards: whether they are on
// their way, why they could not be had, or the cards themselves.
type CardSet struct {
	// Fetching is set while the set is fetched from Scryfall, which is the
	// one wait long enough to say so.
	Fetching bool
	// Err is why the last fetch failed, while the set is still not known,
	// and RetryIn how soon it is tried again.
	Err     string
	RetryIn time.Duration
	// Loaded is set once the set's cards are known, from the cache or from
	// Scryfall; Cards then holds every one Scryfall has an Arena id for.
	// A card of the set not among them is one Scryfall does not know yet.
	Loaded bool
	Cards  map[int]cards.Card
}

// CardsFetching reports that Set is being fetched from Scryfall, because the
// cache did not have it.
type CardsFetching struct{ Set string }

// CardsLoaded reports Set's cards, from the cache or from Scryfall. It
// replaces whatever was known of the set before.
type CardsLoaded struct {
	Set   string
	Cards []cards.Card
}

// CardsFailed reports that Set could not be fetched, and that the card task
// tries again after RetryIn.
type CardsFailed struct {
	Set     string
	Err     error
	RetryIn time.Duration
}

// CacheUnavailable reports that the card cache in Dir cannot be used, so
// card details are fetched again each time the assistant starts.
type CacheUnavailable struct {
	Dir string
	Err error
}

func (CardsFetching) isMsg()    {}
func (CardsLoaded) isMsg()      {}
func (CardsFailed) isMsg()      {}
func (CacheUnavailable) isMsg() {}

// applyCards is Apply for the card task's messages. The cards known do not
// depend on which log is followed, so unlike the draft they are kept when
// the log is lost or starts over.
func (m Model) applyCards(msg Msg) Model {
	switch msg := msg.(type) {
	case CardsFetching:
		set := m.CardSets[msg.Set]
		if set.Loaded {
			return m
		}
		set.Fetching, set.Err, set.RetryIn = true, "", 0
		m.CardSets = withSet(m.CardSets, msg.Set, set)
	case CardsLoaded:
		byID := make(map[int]cards.Card, len(msg.Cards))
		for _, c := range msg.Cards {
			byID[c.ArenaID] = c
		}
		m.CardSets = withSet(m.CardSets, msg.Set, CardSet{Loaded: true, Cards: byID})
	case CardsFailed:
		set := m.CardSets[msg.Set]
		if set.Loaded {
			// Cards already on screen are better than a message that they
			// could not be fetched again.
			return m
		}
		set.Fetching, set.Err, set.RetryIn = false, "no reason given", msg.RetryIn
		if msg.Err != nil {
			set.Err = msg.Err.Error()
		}
		m.CardSets = withSet(m.CardSets, msg.Set, set)
	case CacheUnavailable:
		m.CacheDir = msg.Dir
		m.CacheProblem = "no reason given"
		// As with the log, the window names the folder already, so an OS
		// error's own copy of the path is left out.
		var pe *fs.PathError
		switch {
		case errors.As(msg.Err, &pe) && pe.Err != nil:
			m.CacheProblem = pe.Err.Error()
		case msg.Err != nil:
			m.CacheProblem = msg.Err.Error()
		}
	}
	return m
}

// withSet is sets with code's entry replaced by set, as a new map, so a
// Model held elsewhere keeps the map it had.
func withSet(sets map[string]CardSet, code string, set CardSet) map[string]CardSet {
	next := maps.Clone(sets)
	if next == nil {
		next = map[string]CardSet{}
	}
	next[code] = set
	return next
}

// card is what is known of Arena id id in a draft of set: its own set's
// cards first, then any other set loaded, since a draft the log did not name
// may still be of a set already known.
func (m Model) card(set string, id int) (cards.Card, bool) {
	if c, ok := m.CardSets[set].Cards[id]; ok {
		return c, true
	}
	for _, s := range m.CardSets {
		if c, ok := s.Cards[id]; ok {
			return c, true
		}
	}
	return cards.Card{}, false
}

// cardName is how a card is named in the window: by name when known, and by
// its Arena id when not.
func (m Model) cardName(set string, id int) string {
	if c, ok := m.card(set, id); ok {
		return c.Name
	}
	return cards.Unnamed(id)
}

// notOnScryfall is the detail line of a card in a set whose cards are known
// but which is not among them: brand new, or digital only (decision 15).
const notOnScryfall = "Not on Scryfall yet"

// cardLine is one card in the pack, put into words.
func (m Model) cardLine(set string, id int) CardLine {
	if c, ok := m.card(set, id); ok {
		return CardLine{Name: c.Name, Detail: c.Detail()}
	}
	l := CardLine{Name: cards.Unnamed(id)}
	if m.CardSets[set].Loaded {
		l.Detail = notOnScryfall
	}
	return l
}

// cardsNotice says why the draft's cards show by id, when some do: their set
// is still coming, could not be fetched, or is not known at all. Empty when
// every card in ids is named, or the set is known and only some cards are
// missing from it, which their own lines say.
func (m Model) cardsNotice(set string, ids []int) string {
	named := true
	for _, id := range ids {
		if _, ok := m.card(set, id); !ok {
			named = false
			break
		}
	}
	if named {
		return ""
	}
	if set == "" {
		return "Arena's log has not said which set this draft is, so its cards show by their Arena id."
	}
	s, ok := m.CardSets[set]
	switch {
	case s.Loaded:
		return ""
	case s.Err != "":
		return fmt.Sprintf("Couldn't get %s's cards from Scryfall (%s). Check your internet connection; "+
			"the assistant tries again %s, and shows cards by their Arena id until then.", set, s.Err, within(s.RetryIn))
	case s.Fetching:
		return fmt.Sprintf("Fetching %s's cards from Scryfall. This happens once per set; after that they are kept on this computer.", set)
	case !ok:
		return "Loading card names…"
	}
	return ""
}

// within says when, from now, d is up: "in 30 seconds", "in 2 minutes".
func within(d time.Duration) string {
	switch {
	case d <= 0:
		return "shortly"
	case d < time.Minute:
		return plural(int(d.Round(time.Second)/time.Second), "second")
	default:
		return plural(int(d.Round(time.Minute)/time.Minute), "minute")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "in 1 " + unit
	}
	return fmt.Sprintf("in %d %ss", n, unit)
}

// cacheNote is the footnote while the cache cannot be used: what that costs,
// and what to do. Card details and pictures are kept in the same folder, so
// when neither can be, it says so once.
func (m Model) cacheNote() string {
	if m.CacheProblem == "" && m.ImageProblem == "" {
		return ""
	}
	where := func(dir string) string {
		if dir == "" {
			return "the cache folder"
		}
		return dir
	}
	var said string
	switch {
	case m.ImageProblem == "":
		said = fmt.Sprintf("Card details can't be saved in %s (%s), so they are fetched from Scryfall again each time the assistant starts.",
			where(m.CacheDir), m.CacheProblem)
	case m.CacheProblem == "":
		said = fmt.Sprintf("Card images can't be saved in %s (%s), so cards show by name; the assistant keeps trying.",
			where(m.ImageDir), m.ImageProblem)
	default:
		said = fmt.Sprintf("Card details and images can't be saved in %s (%s), so details are fetched from Scryfall again each time the assistant starts, and cards show by name.",
			where(m.CacheDir), m.CacheProblem)
	}
	return strings.Join([]string{
		said,
		"Check that your user account can write to that folder, or delete it to start it afresh: it only holds what can be fetched again.",
	}, " ")
}
