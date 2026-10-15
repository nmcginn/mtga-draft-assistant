package draft

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
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

// lineIndex is the index of the nth (from 0) line of lines starting with
// prefix, so the lines a test starts from or repeats are real ones.
func lineIndex(t *testing.T, lines []string, prefix string, nth int) int {
	t.Helper()
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			if nth == 0 {
				return i
			}
			nth--
		}
	}
	t.Fatalf("no line %d starting %q", nth, prefix)
	return -1
}

// replay reads lines with a new parser, as the app does with a log it has
// just found, and applies every event to no draft.
func replay(lines []string) *Draft {
	var p arena.Parser
	var d *Draft
	for _, l := range lines {
		for _, e := range p.Line(l) {
			d = Apply(d, e)
		}
	}
	return d
}

// checkPicksInOrder asserts picks are the 42 picks of a three-pack draft of
// 14-card packs, each at its position, in the order taken.
func checkPicksInOrder(t *testing.T, picks []Pick) {
	t.Helper()
	if len(picks) != 42 {
		t.Fatalf("got %d picks, want 42: %v", len(picks), picks)
	}
	for i, p := range picks {
		if p.Pack != i/14+1 || p.Pick != i%14+1 {
			t.Errorf("pick %d is at p%dp%d, want p%dp%d", i+1, p.Pack, p.Pick, i/14+1, i%14+1)
		}
	}
}

func cardsOf(picks []Pick) []int {
	out := make([]int, len(picks))
	for i, p := range picks {
		out[i] = p.Card
	}
	return out
}

func sorted(s []int) []int {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

func TestAWholeDraftReplayedFromItsFixtureEndsWithThePoolItPicked(t *testing.T) {
	for _, c := range []struct {
		path        string
		event       string
		format      arena.Format
		first, last int
	}{
		{premierFixture, "PremierDraft_FRA_20260929", arena.PremierDraft, 106453, 106241},
		{quickFixture, "QuickDraft_FRA_20261008", arena.QuickDraft, 106409, 106355},
	} {
		t.Run(string(c.format), func(t *testing.T) {
			d := replay(fixtureLines(t, c.path))
			if d == nil {
				t.Fatal("no draft")
			}
			if d.Event != c.event || d.Set != "FRA" || d.Format != c.format {
				t.Errorf("draft is %q, %q, %q; want %q, FRA, %q", d.Event, d.Set, d.Format, c.event, c.format)
			}
			if !d.Done || d.Cards != nil {
				t.Errorf("done %v with pack %v; want done, with no pack on show", d.Done, d.Cards)
			}
			checkPicksInOrder(t, d.Picks)
			if d.Picks[0].Card != c.first || d.Picks[41].Card != c.last {
				t.Errorf("first and last picks %d, %d; want %d, %d", d.Picks[0].Card, d.Picks[41].Card, c.first, c.last)
			}
			if !slices.Equal(sorted(d.Pool), sorted(cardsOf(d.Picks))) {
				t.Errorf("pool %v is not the cards picked %v", d.Pool, cardsOf(d.Picks))
			}
		})
	}
}

func TestMidDraftThePackOnShowIsTheLatestAndThePicksAreThoseBeforeIt(t *testing.T) {
	lines := fixtureLines(t, premierFixture)
	sixth := lineIndex(t, lines, "[UnityCrossThreadLogger]Draft.Notify ", 5)
	d := replay(lines[:sixth+1])
	if d.Pack != 1 || d.Pick != 6 || len(d.Cards) != 9 {
		t.Errorf("on show: p%dp%d with %d cards; want p1p6 with 9", d.Pack, d.Pick, len(d.Cards))
	}
	if len(d.Picks) != 5 || d.Picks[0] != (Pick{Pack: 1, Pick: 1, Card: 106453}) {
		t.Errorf("picks %v, want the first five, starting with 106453 at p1p1", d.Picks)
	}
	if _, ok := d.Picked(); ok {
		t.Error("the pack on show counts as picked from before any pick from it")
	}

	pick := lineIndex(t, lines, "[UnityCrossThreadLogger]==> EventPlayerDraftMakePick", 5)
	d = replay(lines[:pick+1])
	if card, ok := d.Picked(); !ok || card != d.Picks[5].Card {
		t.Errorf("after the sixth pick, Picked() = %d, %v; want %d, true", card, ok, d.Picks[5].Card)
	}
}

func TestAPickArenaSendsAgainOnReconnectIsRecordedOnce(t *testing.T) {
	t.Run("premier, the request logged twice", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		i := lineIndex(t, lines, "[UnityCrossThreadLogger]==> EventPlayerDraftMakePick", 3)
		d := replay(slices.Insert(slices.Clone(lines), i+1, lines[i]))
		checkPicksInOrder(t, d.Picks)
	})

	t.Run("premier, pack and pick shown again after later picks", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		notify := lineIndex(t, lines, "[UnityCrossThreadLogger]Draft.Notify ", 2)
		pick := lineIndex(t, lines, "[UnityCrossThreadLogger]==> EventPlayerDraftMakePick", 2)
		later := lineIndex(t, lines, "[UnityCrossThreadLogger]Draft.Notify ", 5)
		d := replay(slices.Insert(slices.Clone(lines), later, lines[notify], lines[pick]))
		checkPicksInOrder(t, d.Picks)
	})

	t.Run("quick, the status asked for again", func(t *testing.T) {
		lines := fixtureLines(t, quickFixture)
		status := lineIndex(t, lines, "<== BotDraftDraftStatus", 0)
		reply := lineIndex(t, lines, "<== BotDraftDraftPick", 6)
		request := lineIndex(t, lines, "[UnityCrossThreadLogger]==> BotDraftDraftPick", 6)
		// Reconnecting, Arena asks for the status, which is the pack the
		// last reply showed, and may send the pick in flight again.
		again := []string{lines[status], lines[reply+1], lines[request]}
		d := replay(slices.Insert(slices.Clone(lines), reply+2, again...))
		checkPicksInOrder(t, d.Picks)
	})
}

func TestABotDraftResumedAfterRelaunchingArenaKnowsItsEarlierPicks(t *testing.T) {
	// Relaunching Arena starts a new log, which opens with the draft's
	// status; the earlier picks are only in Arena's list of picked cards,
	// which is sorted by id, so where each was taken is not known.
	lines := fixtureLines(t, quickFixture)
	want := replay(lines)
	for _, n := range []int{0, 5, 13, 14, 20, 27, 28, 40} {
		from := lineIndex(t, lines, "<== BotDraftDraftPick", n)
		d := replay(lines[from:])
		if d == nil || d.Event != want.Event || d.Set != "FRA" || !d.Done {
			t.Fatalf("resumed after pick %d: %+v, want the same draft, finished", n+1, d)
		}
		earlier := n + 1
		if len(d.Picks) != 42 {
			t.Fatalf("resumed after pick %d: %d picks, want 42", n+1, len(d.Picks))
		}
		for _, p := range d.Picks[:earlier] {
			if p.Pack != 0 || p.Pick != 0 {
				t.Errorf("resumed after pick %d: earlier pick %+v has a position the log never gave", n+1, p)
			}
		}
		if !reflect.DeepEqual(d.Picks[earlier:], want.Picks[earlier:]) {
			t.Errorf("resumed after pick %d, the picks seen are %v; want %v", n+1, d.Picks[earlier:], want.Picks[earlier:])
		}
		if !slices.Equal(sorted(cardsOf(d.Picks[:earlier])), sorted(cardsOf(want.Picks[:earlier]))) {
			t.Errorf("resumed after pick %d, earlier picks %v; want the cards of %v", n+1, d.Picks[:earlier], want.Picks[:earlier])
		}
	}
}

func TestAPremierDraftSeenOnlyFromItsMiddleStillShowsItsPacks(t *testing.T) {
	// No fixture shows rejoining a draft against people after relaunching
	// Arena (decision 23), so such a draft may first appear as a pack with
	// nothing to say which draft it is in.
	lines := fixtureLines(t, premierFixture)
	from := lineIndex(t, lines, "[UnityCrossThreadLogger]Draft.Notify ", 16)
	d := replay(lines[from:])
	if d.Event != "" || d.Set != "" || d.Format != "" {
		t.Errorf("draft %q, %q, %q; want it not to claim an event", d.Event, d.Set, d.Format)
	}
	if !d.Done || len(d.Picks) != 42-16 || d.Picks[0].Pack != 2 || d.Picks[0].Pick != 3 {
		t.Errorf("done %v, %d picks starting %+v; want done, 26 picks from p2p3", d.Done, len(d.Picks), d.Picks[0])
	}
	if len(d.Pool) != 42 {
		t.Errorf("pool of %d, want all 42 cards Arena listed", len(d.Pool))
	}
}

func TestAPackFromEarlierInTheDraftDoesNotReplaceTheOneOnShow(t *testing.T) {
	d := Apply(nil, arena.DraftStarted{Event: "PremierDraft_FRA_20260929", Set: "FRA", Format: arena.PremierDraft})
	d = Apply(d, arena.PackShown{Pack: 2, Pick: 1, Cards: []int{1, 2, 3}})
	d = Apply(d, arena.PackShown{Pack: 1, Pick: 14, Cards: []int{4}})
	if d.Pack != 2 || d.Pick != 1 || !slices.Equal(d.Cards, []int{1, 2, 3}) {
		t.Errorf("on show: p%dp%d %v; want p2p1 [1 2 3]", d.Pack, d.Pick, d.Cards)
	}
	d = Apply(d, arena.PackShown{Pack: 2, Pick: 1, Cards: []int{1, 2}})
	if !slices.Equal(d.Cards, []int{1, 2}) {
		t.Errorf("the same position shown with other cards left %v on show, want the new cards", d.Cards)
	}
}

func TestJoiningADraftReplacesTheOneBefore(t *testing.T) {
	d := Apply(nil, arena.DraftStarted{Event: "QuickDraft_FRA_20261008", Set: "FRA", Format: arena.QuickDraft})
	d = Apply(d, arena.PickMade{Pack: 1, Pick: 1, Card: 7})
	d = Apply(d, arena.DraftStarted{Event: "QuickDraft_FRA_20261008", Set: "FRA", Format: arena.QuickDraft})
	if len(d.Picks) != 0 || d.Done {
		t.Errorf("after joining again: %+v, want a fresh draft", d)
	}
}

func TestAPackAfterADraftEndedIsANewDraft(t *testing.T) {
	d := Apply(nil, arena.DraftStarted{Event: "PremierDraft_FRA_20260929", Set: "FRA", Format: arena.PremierDraft})
	d = Apply(d, arena.PickMade{Pack: 1, Pick: 1, Card: 7})
	d = Apply(d, arena.DraftEnded{Pool: []int{7}})
	d = Apply(d, arena.PackShown{Pack: 1, Pick: 1, Cards: []int{8, 9}})
	if d.Done || d.Set != "" || len(d.Picks) != 0 || !slices.Equal(d.Cards, []int{8, 9}) {
		t.Errorf("got %+v, want a new draft with only the pack", d)
	}
}

func TestTheEndOfADraftNeverSeenStillSaysWhatWasDrafted(t *testing.T) {
	d := Apply(nil, arena.DraftEnded{Pool: []int{3, 1, 2}})
	if d == nil || !d.Done || !slices.Equal(d.Pool, []int{3, 1, 2}) {
		t.Errorf("got %+v, want a finished draft with the pool", d)
	}
}

func TestNoEventsMeanNoDraft(t *testing.T) {
	if d := replay(nil); d != nil {
		t.Errorf("an empty log gave %+v", d)
	}
	if d := replay([]string{"", "[UnityCrossThreadLogger]10/10/2026 12:44:31 PM"}); d != nil {
		t.Errorf("a log with no draft gave %+v", d)
	}
}

func TestApplyLeavesTheDraftItWasGivenAlone(t *testing.T) {
	// The app keeps the old model until the new one replaces it, and tests
	// compare the two; neither may see the other's changes.
	cards := []int{1, 2, 3}
	d := Apply(nil, arena.PackShown{Pack: 1, Pick: 1, Cards: cards})
	cards[0] = 99
	if d.Cards[0] != 1 {
		t.Error("the pack on show changed with the event's slice")
	}

	d = Apply(d, arena.PickMade{Pack: 1, Pick: 1, Card: 1})
	d.Picks = slices.Grow(d.Picks, 10) // room to append in place
	before := *d
	a := Apply(d, arena.PickMade{Pack: 1, Pick: 2, Card: 2})
	b := Apply(d, arena.PickMade{Pack: 1, Pick: 2, Card: 3})
	if len(d.Picks) != 1 || !reflect.DeepEqual(*d, before) {
		t.Errorf("the draft applied to changed: %+v", d)
	}
	if a.Picks[1].Card != 2 || b.Picks[1].Card != 3 {
		t.Errorf("two drafts from one share their picks: %v and %v", a.Picks, b.Picks)
	}
	c := Apply(d, arena.PickMade{Pack: 1, Pick: 1, Card: 5})
	if d.Picks[0].Card != 1 || c.Picks[0].Card != 5 {
		t.Errorf("replacing a pick changed the original: %v, %v", d.Picks, c.Picks)
	}
}

func TestArenasListOfPicksAddsOnlyThePicksNotSeen(t *testing.T) {
	seen := []Pick{{1, 1, 10}, {1, 2, 20}}
	for _, c := range []struct {
		name   string
		picked []int
		want   []Pick
	}{
		{"nothing picked yet", []int{}, seen},
		{"exactly the picks seen, in another order", []int{20, 10}, seen},
		{"one before the log began", []int{5, 10, 20}, []Pick{{0, 0, 5}, {1, 1, 10}, {1, 2, 20}}},
		{"a second copy of a card seen once", []int{10, 10, 20}, []Pick{{0, 0, 10}, {1, 1, 10}, {1, 2, 20}}},
		{"fewer than seen", []int{10}, seen},
	} {
		if got := withEarlier(seen, c.picked); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: withEarlier(%v) = %v, want %v", c.name, c.picked, got, c.want)
		}
	}
}

func TestPicksWithNoPositionAreNeverMistakenForTheSamePick(t *testing.T) {
	d := Apply(nil, arena.PackShown{Pack: 3, Pick: 2, Cards: []int{9}, Picked: []int{1, 2, 3, 4}})
	d = Apply(d, arena.PickMade{Pack: 3, Pick: 2, Card: 9})
	if got := cardsOf(d.Picks); !slices.Equal(got, []int{1, 2, 3, 4, 9}) {
		t.Errorf("picks %v, want the four unplaced then the new one", got)
	}
	if card, ok := d.Picked(); !ok || card != 9 {
		t.Errorf("Picked() = %d, %v; want 9, true", card, ok)
	}
}
