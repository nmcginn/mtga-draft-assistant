package app

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/draft"
)

const (
	premierFixture = "../../testdata/logs/2026-10-fra-premier-full-draft.log"
	quickFixture   = "../../testdata/logs/2026-10-fra-quick-full-draft.log"
)

func fixtureLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// following is the model once the log is found and read to its end.
func following() Model {
	return New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(LogCaughtUp{})
}

// read applies lines to m as the log task does: parsed in batches of
// batch lines, one DraftEvents for each batch that held any.
func read(m Model, lines []string, batch int) Model {
	var p arena.Parser
	for chunk := range slices.Chunk(lines, batch) {
		var events []arena.Event
		for _, l := range chunk {
			events = append(events, p.Line(l)...)
		}
		if len(events) > 0 {
			m = m.Apply(DraftEvents{Events: events})
		}
	}
	return m
}

var (
	started = arena.DraftStarted{Event: "PremierDraft_FRA_20260929", Set: "FRA", Format: arena.PremierDraft}
	p1p1    = arena.PackShown{Pack: 1, Pick: 1, Cards: []int{106325, 106235, 106453}}
)

func TestAWholeDraftReadFromTheLogEndsWithItsPoolInTheModel(t *testing.T) {
	for _, path := range []string{premierFixture, quickFixture} {
		lines := fixtureLines(t, path)
		// One line at a time, and in the follower's batches of many: the
		// same draft either way.
		one := read(following(), lines, 1)
		many := read(following(), lines, 500)
		if one.Draft == nil || many.Draft == nil {
			t.Fatalf("%s: no draft in the model", path)
		}
		if !one.Draft.Done || len(one.Draft.Picks) != 42 || len(one.Draft.Pool) != 42 {
			t.Errorf("%s: done %v, %d picks, pool of %d; want done, 42 and 42", path, one.Draft.Done, len(one.Draft.Picks), len(one.Draft.Pool))
		}
		if !slices.Equal(one.Draft.Picks, many.Draft.Picks) {
			t.Errorf("%s: read in batches, the picks differ", path)
		}
		if !one.ShowsDraft() {
			t.Errorf("%s: a finished draft is not shown", path)
		}
	}
}

func TestDraftEventsForALogNoLongerFollowedAreIgnored(t *testing.T) {
	m := New().Apply(DraftEvents{Events: []arena.Event{started, p1p1}})
	if m.Draft != nil || m.ShowsDraft() {
		t.Errorf("while looking for the log, a draft arrived: %+v", m.Draft)
	}
}

func TestANewLogOrSessionForgetsTheDraft(t *testing.T) {
	drafting := following().Apply(DraftEvents{Events: []arena.Event{started, p1p1}})
	for name, msg := range map[string]Msg{
		"Arena relaunched":      SessionStarted{},
		"the log went away":     LogLost{},
		"another log was found": LogFound{Path: "/other/Player.log"},
	} {
		if m := drafting.Apply(msg); m.Draft != nil {
			t.Errorf("%s: the old draft is still there: %+v", name, m.Draft)
		}
	}
	if drafting.Draft == nil {
		t.Error("forgetting the draft changed the model it was applied to")
	}
}

func TestADraftResumedInANewSessionIsReadWhole(t *testing.T) {
	// Arena relaunched mid bot draft: the old session's draft is forgotten,
	// and the new log's status brings it back with every card picked.
	lines := fixtureLines(t, quickFixture)
	half := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "<== BotDraftDraftPick") })
	m := read(following(), lines[:half+10], 500)
	m = m.Apply(SessionStarted{})
	m = read(m.Apply(LogCaughtUp{}), lines[half:], 500)
	if m.Draft == nil || m.Draft.Set != "FRA" || len(m.Draft.Picks) != 42 {
		t.Fatalf("after resuming: %+v, want the FRA draft with all 42 picks", m.Draft)
	}
}

func TestAFailureOrAnUnreadableLogIsShownInsteadOfTheDraft(t *testing.T) {
	drafting := following().Apply(DraftEvents{Events: []arena.Event{started, p1p1}})
	if !drafting.ShowsDraft() {
		t.Fatal("the draft is not shown")
	}
	if m := drafting.Apply(LogUnreadable{Err: errors.New("permission denied")}); m.ShowsDraft() {
		t.Error("a log that cannot be read is hidden behind a draft that has stopped updating")
	}
	if m := drafting.Apply(TaskFailed{Task: "Watching Arena's log", Err: errors.New("boom")}); m.ShowsDraft() {
		t.Error("a stopped task is hidden behind the draft")
	}
	if m := following(); m.ShowsDraft() {
		t.Error("with no draft, the window shows a draft")
	}
}

func TestTheDraftScreenSaysWhichDraftAndWhereInIt(t *testing.T) {
	m := following().Apply(DraftEvents{Events: []arena.Event{started}})
	s := m.DraftScreen()
	if s.Title != "FRA · Premier Draft" || s.Position != "" || s.Heading != "Waiting for the first pack." || s.Cards != nil || s.PicksHeading != "" {
		t.Errorf("before the first pack: %+v", s)
	}

	m = m.Apply(DraftEvents{Events: []arena.Event{p1p1}})
	s = m.DraftScreen()
	if s.Position != "Pack 1 · Pick 1" || s.Heading != "3 cards in this pack" || !slices.Equal(s.Cards, []CardLine{{Name: "Card 106325"}, {Name: "Card 106235"}, {Name: "Card 106453"}}) {
		t.Errorf("with the first pack: %+v", s)
	}

	m = m.Apply(DraftEvents{Events: []arena.Event{arena.PickMade{Pack: 1, Pick: 1, Card: 106453}}})
	s = m.DraftScreen()
	if s.Heading != "You took Card 106453. Waiting for the next pack." || s.Cards != nil {
		t.Errorf("after picking: heading %q, cards %v; want the pick named and the pack gone", s.Heading, s.Cards)
	}
	if s.PicksHeading != "Your picks (1)" || !slices.Equal(s.Picks, []string{"Pack 1: Card 106453"}) {
		t.Errorf("after picking: %q %q", s.PicksHeading, s.Picks)
	}

	m = m.Apply(DraftEvents{Events: []arena.Event{arena.PackShown{Pack: 1, Pick: 2, Cards: []int{106235}}}})
	if s = m.DraftScreen(); s.Heading != "1 card in this pack" || s.Position != "Pack 1 · Pick 2" {
		t.Errorf("with the last card: %q at %q", s.Heading, s.Position)
	}
}

func TestPicksAreListedOneLinePerPackInTheOrderTaken(t *testing.T) {
	lines := fixtureLines(t, premierFixture)
	p2p3 := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "[UnityCrossThreadLogger]Draft.Notify ") && strings.Contains(l, `"SelfPick":3,"SelfPack":2`) {
			p2p3 = i
		}
	}
	s := read(following(), lines[:p2p3+1], 500).DraftScreen()
	if s.PicksHeading != "Your picks (16)" || len(s.Picks) != 2 {
		t.Fatalf("at p2p3: %q %q, want 16 picks on two lines", s.PicksHeading, s.Picks)
	}
	if !strings.HasPrefix(s.Picks[0], "Pack 1: Card 106453, Card 106225, ") || strings.Count(s.Picks[0], ",") != 13 {
		t.Errorf("first line %q, want pack 1's 14 picks from 106453", s.Picks[0])
	}
	if !strings.HasPrefix(s.Picks[1], "Pack 2: ") || strings.Count(s.Picks[1], ",") != 1 {
		t.Errorf("second line %q, want pack 2's two picks", s.Picks[1])
	}
}

func TestAFinishedDraftShowsItsPoolAndWhatToDoNext(t *testing.T) {
	s := read(following(), fixtureLines(t, premierFixture), 500).DraftScreen()
	if s.Title != "FRA · Premier Draft" || s.Position != "Draft complete" || s.Cards != nil {
		t.Errorf("finished: %+v", s)
	}
	if !strings.Contains(s.Heading, "Start another draft") {
		t.Errorf("heading %q, want what to do next", s.Heading)
	}
	if s.PicksHeading != "Your pool (42 cards)" || len(s.Picks) != 3 || !strings.HasPrefix(s.Picks[2], "Pack 3: ") {
		t.Errorf("pool: %q %q, want 42 cards on three lines, by pack", s.PicksHeading, s.Picks)
	}
	if s.Pool != nil {
		t.Errorf("pool grouped as %+v with no card's details known", s.Pool)
	}
}

func TestAPoolWithPicksTheLogMissedIsShownAsArenaGaveIt(t *testing.T) {
	lines := fixtureLines(t, premierFixture)
	from := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, `"SelfPick":1,"SelfPack":2`) })
	s := read(following(), lines[from:], 500).DraftScreen()
	if s.Title != "Draft" || s.PicksHeading != "Your pool (42 cards)" || len(s.Picks) != 1 || strings.Count(s.Picks[0], ",") != 41 {
		t.Errorf("finished from mid-draft: %+v, want an untitled draft with all 42 cards on one line", s)
	}
}

func TestEarlierPicksFromABotDraftsOwnListAreShownApart(t *testing.T) {
	lines := fixtureLines(t, quickFixture)
	from := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "<== BotDraftDraftPick") })
	s := read(following(), lines[from:], 500).DraftScreen()
	if len(s.Picks) != 4 || !strings.HasPrefix(s.Picks[0], "Earlier: ") || strings.Contains(s.Picks[0], ",") || !strings.HasPrefix(s.Picks[1], "Pack 1: ") {
		t.Errorf("pool %q, want the one pick made before the log, then three packs", s.Picks)
	}
}

func TestADraftTheLogNeverNamedSaysSoInItsTitle(t *testing.T) {
	m := following().Apply(DraftEvents{Events: []arena.Event{p1p1}})
	if s := m.DraftScreen(); s.Title != "Draft in progress" || s.Position != "Pack 1 · Pick 1" {
		t.Errorf("unnamed draft: %+v", s)
	}
	// A format added to the parser before this map still has a name.
	if got := draftTitle(draft.Draft{Set: "FRA", Format: "TradDraft"}); got != "FRA · TradDraft" {
		t.Errorf("unknown format titled %q", got)
	}
}

func TestWhileCatchingUpTheDraftScreenSaysThePackMayBeOld(t *testing.T) {
	m := New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(DraftEvents{Events: []arena.Event{started, p1p1}})
	if s := m.DraftScreen(); !strings.Contains(s.Footnote, "Catching up") {
		t.Errorf("catching up, footnote %q", s.Footnote)
	}
	if s := m.Apply(LogCaughtUp{}).DraftScreen(); strings.Contains(s.Footnote, "Catching up") {
		t.Errorf("caught up, footnote %q, still catching up", s.Footnote)
	}
}

func TestWithNoDraftTheDraftScreenIsEmpty(t *testing.T) {
	if s := following().DraftScreen(); s.Title != "" || s.Cards != nil {
		t.Errorf("no draft: %+v", s)
	}
}
