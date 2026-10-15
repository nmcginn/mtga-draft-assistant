package arena

import (
	"bytes"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const (
	premierFixture = "../../testdata/logs/2026-10-fra-premier-full-draft.log"
	quickFixture   = "../../testdata/logs/2026-10-fra-quick-full-draft.log"
)

// fixtureLines reads a fixture as the follower hands it over: one string per
// line, without the newline.
func fixtureLines(t testing.TB, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// lineIndex is the index of the nth (from 0) line of lines starting with
// prefix. Tests take the lines they change from a fixture this way, so
// every line they feed the parser had a real one as its model.
func lineIndex(t testing.TB, lines []string, prefix string, nth int) int {
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

// parse feeds every line to a new Parser, returning the events and what was
// reported to Debug.
func parse(lines []string) ([]Event, string) {
	var log bytes.Buffer
	p := Parser{Debug: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	var events []Event
	for _, l := range lines {
		events = append(events, p.Line(l)...)
	}
	return events, log.String()
}

func without(lines []string, i int) []string {
	return slices.Delete(slices.Clone(lines), i, i+1)
}

func with(lines []string, i int, l string) []string {
	out := slices.Clone(lines)
	out[i] = l
	return out
}

// checkWholeDraft asserts that events are one whole three-pack draft of 14
// cards a pack: start, then each pack and the pick from it, then the pool,
// which holds exactly the cards picked.
func checkWholeDraft(t *testing.T, events []Event, start DraftStarted) (packs []PackShown, picks []PickMade) {
	t.Helper()
	if len(events) != 1+42*2+1 {
		t.Fatalf("got %d events, want 86 (start, 42 packs, 42 picks, end): %v", len(events), events)
	}
	if !reflect.DeepEqual(events[0], start) {
		t.Errorf("first event = %+v, want %+v", events[0], start)
	}
	for i := range 42 {
		pack, ok := events[1+2*i].(PackShown)
		if !ok {
			t.Fatalf("event %d = %T, want PackShown", 1+2*i, events[1+2*i])
		}
		pick, ok := events[2+2*i].(PickMade)
		if !ok {
			t.Fatalf("event %d = %T, want PickMade", 2+2*i, events[2+2*i])
		}
		wantPack, wantPick := i/14+1, i%14+1
		if pack.Pack != wantPack || pack.Pick != wantPick || pick.Pack != wantPack || pick.Pick != wantPick {
			t.Errorf("pick %d: pack shown as p%dp%d, picked as p%dp%d, want p%dp%d",
				i+1, pack.Pack, pack.Pick, pick.Pack, pick.Pick, wantPack, wantPick)
		}
		if len(pack.Cards) != 15-wantPick {
			t.Errorf("p%dp%d has %d cards, want %d", wantPack, wantPick, len(pack.Cards), 15-wantPick)
		}
		if !slices.Contains(pack.Cards, pick.Card) {
			t.Errorf("p%dp%d: picked %d, which was not in the pack %v", wantPack, wantPick, pick.Card, pack.Cards)
		}
		packs, picks = append(packs, pack), append(picks, pick)
	}
	end, ok := events[len(events)-1].(DraftEnded)
	if !ok {
		t.Fatalf("last event = %T, want DraftEnded", events[len(events)-1])
	}
	picked := make([]int, len(picks))
	for i, p := range picks {
		picked[i] = p.Card
	}
	pool := slices.Clone(end.Pool)
	slices.Sort(picked)
	slices.Sort(pool)
	if !slices.Equal(pool, picked) {
		t.Errorf("pool %v is not the cards picked %v", end.Pool, picked)
	}
	return packs, picks
}

func TestAWholePremierDraftIsReadFromItsFixture(t *testing.T) {
	events, debug := parse(fixtureLines(t, premierFixture))
	packs, picks := checkWholeDraft(t, events, DraftStarted{Event: "PremierDraft_FRA_20260929", Set: "FRA", Format: PremierDraft})

	// Spot checks against the fixture's own lines.
	if want := []int{106325, 106235, 106268, 106407, 106411, 106335, 106385, 106391, 106444, 106321, 106501, 106485, 106453, 106439}; !slices.Equal(packs[0].Cards, want) {
		t.Errorf("p1p1 = %v, want %v", packs[0].Cards, want)
	}
	if picks[0].Card != 106453 || picks[41].Card != 106241 {
		t.Errorf("first and last picks = %d, %d; want 106453, 106241", picks[0].Card, picks[41].Card)
	}
	if debug != "" {
		t.Errorf("a clean fixture reported skipped lines:\n%s", debug)
	}
}

func TestAWholeQuickDraftIsReadFromItsFixture(t *testing.T) {
	events, debug := parse(fixtureLines(t, quickFixture))
	packs, picks := checkWholeDraft(t, events, DraftStarted{Event: "QuickDraft_FRA_20261008", Set: "FRA", Format: QuickDraft})

	// Bot drafts count from 0 in the log; the events count from 1.
	if want := []int{106360, 106433, 106394, 106226, 106299, 106335, 106399, 106233, 106409, 106389, 106450, 106459, 106500, 106435}; !slices.Equal(packs[0].Cards, want) {
		t.Errorf("p1p1 = %v, want %v", packs[0].Cards, want)
	}
	if picks[0].Card != 106409 || picks[41].Card != 106355 {
		t.Errorf("first and last picks = %d, %d; want 106409, 106355", picks[0].Card, picks[41].Card)
	}
	if debug != "" {
		t.Errorf("a clean fixture reported skipped lines:\n%s", debug)
	}
}

func TestAnEmptyLogHoldsNoDraft(t *testing.T) {
	for _, lines := range [][]string{nil, {""}, {"", "", ""}} {
		if events, debug := parse(lines); len(events) != 0 || debug != "" {
			t.Errorf("%q: events %v, debug %q", lines, events, debug)
		}
	}
}

func TestLinesEndingInCRLFReadTheSame(t *testing.T) {
	// Arena on Windows, and so under Wine, may end lines with \r\n.
	lines := fixtureLines(t, premierFixture)
	crlf := make([]string, len(lines))
	for i, l := range lines {
		crlf[i] = l + "\r"
	}
	want, _ := parse(lines)
	if got, _ := parse(crlf); !reflect.DeepEqual(got, want) {
		t.Error("the same log with \\r\\n line endings read differently")
	}
}

func TestALineCutOffMidJSONIsSkippedAndTheDraftGoesOn(t *testing.T) {
	cut := func(l string) string { return l[:len(l)/2] }

	t.Run("a pack", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		i := lineIndex(t, lines, notifyPrefix, 2) // p1p3
		events, debug := parse(with(lines, i, cut(lines[i])))
		if len(events) != 85 {
			t.Fatalf("got %d events, want 85: every one but the cut pack", len(events))
		}
		if pick := events[5].(PickMade); pick.Pack != 1 || pick.Pick != 3 {
			t.Errorf("event 5 = %+v, want the p1p3 pick right after p1p2's", pick)
		}
		if !strings.Contains(debug, "Draft.Notify") || strings.Count(debug, "\n") != 1 {
			t.Errorf("debug = %q, want the one cut line reported", debug)
		}
	})

	t.Run("a pick", func(t *testing.T) {
		lines := fixtureLines(t, quickFixture)
		i := lineIndex(t, lines, requestPrefix+"BotDraftDraftPick", 0)
		events, debug := parse(with(lines, i, cut(lines[i])))
		if p2, ok := events[2].(PackShown); len(events) != 85 || !ok || p2.Pack != 1 || p2.Pick != 2 {
			t.Errorf("got %d events, starting %v; want 85, with no p1p1 pick", len(events), events[:3])
		}
		if !strings.Contains(debug, "BotDraftDraftPick") {
			t.Errorf("debug = %q, want the cut line reported", debug)
		}
	})

	t.Run("a response body", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		i := lineIndex(t, lines, replyPrefix+"EventJoin", 0) + 1
		events, debug := parse(with(lines, i, cut(lines[i])))
		if len(events) != 85 {
			t.Fatalf("got %d events, want 85", len(events))
		}
		if _, ok := events[0].(PackShown); !ok {
			t.Errorf("first event = %T, want the first pack, since the start was cut", events[0])
		}
		if !strings.Contains(debug, "EventJoin") {
			t.Errorf("debug = %q, want the cut line reported", debug)
		}
	})

	t.Run("the last line, still being written", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		i := lineIndex(t, lines, notifyPrefix, 41)
		events, _ := parse(append(slices.Clone(lines[:i]), cut(lines[i])))
		if len(events) != 1+41*2 {
			t.Errorf("got %d events, want 83: everything before the cut line", len(events))
		}
	})
}

func TestAPackSeenTwiceIsReportedOnce(t *testing.T) {
	t.Run("premier", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		i := lineIndex(t, lines, notifyPrefix, 4)
		events, _ := parse(slices.Insert(slices.Clone(lines), i, lines[i]))
		checkWholeDraft(t, events, DraftStarted{Event: "PremierDraft_FRA_20260929", Set: "FRA", Format: PremierDraft})
	})

	t.Run("quick, when the status is asked for again", func(t *testing.T) {
		// After a reconnect Arena asks for the draft's status, whose answer
		// is the pack the last pick's answer already showed.
		lines := fixtureLines(t, quickFixture)
		status := lineIndex(t, lines, replyPrefix+"BotDraftDraftStatus", 0)
		pick := lineIndex(t, lines, replyPrefix+"BotDraftDraftPick", 4)
		events, _ := parse(slices.Insert(slices.Clone(lines), pick+2, lines[status], lines[pick+1]))
		checkWholeDraft(t, events, DraftStarted{Event: "QuickDraft_FRA_20261008", Set: "FRA", Format: QuickDraft})
	})

	t.Run("the same pick number with other cards is news", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		i := lineIndex(t, lines, notifyPrefix, 0)
		other := strings.Replace(lines[i], "106325,", "", 1)
		events, _ := parse([]string{lines[i], other})
		if len(events) != 2 {
			t.Errorf("got %v, want both packs", events)
		}
	})
}

func TestEventsOutOfTheExpectedOrderAreStillRead(t *testing.T) {
	lines := fixtureLines(t, premierFixture)
	notify2 := lines[lineIndex(t, lines, notifyPrefix, 14)] // p2p1
	notify1 := lines[lineIndex(t, lines, notifyPrefix, 0)]  // p1p1
	pick := lines[lineIndex(t, lines, requestPrefix+"EventPlayerDraftMakePick", 0)]

	// A pick with no pack or start before it, then packs in reverse: the
	// parser reports what the log says, and leaves sense to the model.
	events, _ := parse([]string{pick, notify2, notify1})
	if len(events) != 3 {
		t.Fatalf("got %v, want all three", events)
	}
	if p := events[0].(PickMade); p.Card != 106453 {
		t.Errorf("pick = %+v", p)
	}
	if a, b := events[1].(PackShown), events[2].(PackShown); a.Pack != 2 || b.Pack != 1 {
		t.Errorf("packs = p%d, p%d; want p2 then p1", a.Pack, b.Pack)
	}
}

func TestAResponseBodyIsReadOnlyRightAfterItsHeader(t *testing.T) {
	lines := fixtureLines(t, premierFixture)
	header := lineIndex(t, lines, replyPrefix+"EventJoin", 0)
	body := lines[header+1]

	if events, _ := parse([]string{body}); len(events) != 0 {
		t.Errorf("a body with no header gave %v", events)
	}
	if events, _ := parse([]string{lines[header], "AK Message: Audio thread resumed, audio restarts.", body}); len(events) != 0 {
		t.Errorf("a body after an unrelated line gave %v", events)
	}
	if events, _ := parse([]string{lines[header], body, body}); len(events) != 1 {
		t.Errorf("a body read twice gave %v, want one start", events)
	}
}

func TestAMalformedDraftLineIsSkippedNotFatal(t *testing.T) {
	premier := fixtureLines(t, premierFixture)
	quick := fixtureLines(t, quickFixture)
	notify := premier[lineIndex(t, premier, notifyPrefix, 0)]
	pick := premier[lineIndex(t, premier, requestPrefix+"EventPlayerDraftMakePick", 0)]
	botPick := quick[lineIndex(t, quick, requestPrefix+"BotDraftDraftPick", 0)]
	completeAt := lineIndex(t, premier, replyPrefix+"DraftCompleteDraft", 0)
	statusAt := lineIndex(t, quick, replyPrefix+"BotDraftDraftStatus", 0)
	replyAt := lineIndex(t, quick, replyPrefix+"BotDraftDraftPick", 0)

	for name, lines := range map[string][]string{
		"a pack with no cards":         {strings.Replace(notify, `"PackCards"`, `"Other"`, 1)},
		"a pack with an empty card id": {strings.Replace(notify, `"106325,`, `",`, 1)},
		"a pack with no pick number":   {strings.Replace(notify, `"SelfPick":1,`, ``, 1)},
		"a pack that is not JSON":      {notifyPrefix + "not json"},
		"a pick of two cards":          {strings.Replace(pick, `[106453]`, `[106453,106235]`, 1)},
		"a pick of no cards":           {strings.Replace(pick, `[106453]`, `[]`, 1)},
		"a pick with no request":       {strings.Replace(pick, `"request"`, `"other"`, 1)},
		"a bot pick of a word":         {strings.Replace(botPick, `[\"106409\"]`, `[\"Plains\"]`, 1)},
		"a pool that is missing":       {premier[completeAt], strings.Replace(premier[completeAt+1], `"CardPool"`, `"Other"`, 1)},
		"a status nobody has seen":     {quick[statusAt], strings.Replace(quick[statusAt+1], `PickNext`, `Paused`, 1)},
		"a pack picked from a word":    {quick[replyAt], strings.Replace(quick[replyAt+1], `\"PickedCards\":[\"106409\"`, `\"PickedCards\":[\"Plains\"`, 1)},
		"a status with no payload":     {quick[statusAt], strings.Replace(quick[statusAt+1], `"Payload"`, `"Other"`, 1)},
		"a status with an empty pack":  {quick[statusAt], quick[statusAt+1][:strings.Index(quick[statusAt+1], `[\"`)] + `[],\"PackStyles\":[],\"PickedCards\":[],\"PickedStyles\":[]}"}`},
	} {
		t.Run(name, func(t *testing.T) {
			events, debug := parse(lines)
			if len(events) != 0 {
				t.Errorf("got %v, want nothing", events)
			}
			if !strings.Contains(debug, "skipped a draft line") {
				t.Errorf("debug = %q, want the line reported", debug)
			}
		})
	}
}

func TestADraftInAFormatWithNoFixtureIsSkipped(t *testing.T) {
	// Arena may log another format differently, so its lines are not read
	// as one of the formats a fixture has shown.
	t.Run("drafting against people", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		join := lineIndex(t, lines, replyPrefix+"EventJoin", 0) + 1
		lines = with(lines, join, strings.ReplaceAll(lines[join], "PremierDraft_", "TradDraft_"))
		events, debug := parse(lines)
		if len(events) != 0 {
			t.Errorf("got %d events, want none", len(events))
		}
		if !strings.Contains(debug, "TradDraft_FRA_20260929") || !strings.Contains(debug, "not covered yet") {
			t.Errorf("debug does not name the event and why:\n%.500s", debug)
		}
	})

	t.Run("drafting against bots", func(t *testing.T) {
		lines := fixtureLines(t, quickFixture)
		for i := range lines {
			lines[i] = strings.ReplaceAll(lines[i], "QuickDraft_", "CubeDraft_")
		}
		if events, _ := parse(lines); len(events) != 0 {
			t.Errorf("got %d events, want none", len(events))
		}
	})

	t.Run("then a known format is read again", func(t *testing.T) {
		lines := fixtureLines(t, premierFixture)
		join := lineIndex(t, lines, replyPrefix+"EventJoin", 0) + 1
		trad := with(lines, join, strings.ReplaceAll(lines[join], "PremierDraft_", "TradDraft_"))
		events, _ := parse(append(trad, fixtureLines(t, quickFixture)...))
		checkWholeDraft(t, events, DraftStarted{Event: "QuickDraft_FRA_20261008", Set: "FRA", Format: QuickDraft})
	})
}

func TestJoiningAnEventThatIsNotADraftSaysNothing(t *testing.T) {
	lines := fixtureLines(t, premierFixture)
	join := lineIndex(t, lines, replyPrefix+"EventJoin", 0)
	body := strings.Replace(lines[join+1], `"CurrentModule":"PlayerDraft"`, `"CurrentModule":"DeckSelect"`, 1)
	if events, debug := parse([]string{lines[join], body}); len(events) != 0 || debug != "" {
		t.Errorf("events %v, debug %q; want neither", events, debug)
	}
}

func TestABotDraftJoinedBeforeTheLogBeganStartsAtItsStatus(t *testing.T) {
	// Relaunching Arena mid-draft starts a new log, without the join.
	lines := fixtureLines(t, quickFixture)
	join := lineIndex(t, lines, replyPrefix+"EventJoin", 0)
	events, _ := parse(without(without(lines, join+1), join))
	checkWholeDraft(t, events, DraftStarted{Event: "QuickDraft_FRA_20261008", Set: "FRA", Format: QuickDraft})
}

func TestDraftingTheSameEventAgainIsANewDraft(t *testing.T) {
	for _, path := range []string{premierFixture, quickFixture} {
		lines := fixtureLines(t, path)
		events, _ := parse(append(slices.Clone(lines), lines...))
		var starts int
		for _, e := range events {
			if _, ok := e.(DraftStarted); ok {
				starts++
			}
		}
		if len(events) != 2*86 || starts != 2 {
			t.Errorf("%s twice: %d events, %d starts; want 172 and 2", path, len(events), starts)
		}
	}
}

func TestJoiningAgainBeforeADraftEndedStartsANewOne(t *testing.T) {
	// A draft left unfinished, then the same event joined afresh: the
	// second join is news even though no end came between.
	lines := fixtureLines(t, premierFixture)
	half := lineIndex(t, lines, notifyPrefix, 20)
	events, _ := parse(append(slices.Clone(lines[:half]), lines...))
	checkWholeDraft(t, events[1+20*2:], DraftStarted{Event: "PremierDraft_FRA_20260929", Set: "FRA", Format: PremierDraft})
}

func TestEventNamesGiveTheFormatAndSet(t *testing.T) {
	for _, c := range []struct {
		name string
		f    Format
		set  string
		ok   bool
	}{
		{"PremierDraft_FRA_20260929", PremierDraft, "FRA", true},
		{"QuickDraft_FRA_20261008", QuickDraft, "FRA", true},
		{"TradDraft_FRA_20260929", "", "", false},
		{"Ladder", "", "", false},
		{"PremierDraft_FRA", "", "", false},
		{"PremierDraft__20260929", "", "", false},
		{"", "", "", false},
	} {
		f, set, ok := ParseEventName(c.name)
		if f != c.f || set != c.set || ok != c.ok {
			t.Errorf("ParseEventName(%q) = %q, %q, %v; want %q, %q, %v", c.name, f, set, ok, c.f, c.set, c.ok)
		}
	}
}

func TestLinesThatAreNotAboutDraftingCostNothing(t *testing.T) {
	// Most of the log is stack traces and other requests, and the parser
	// sees every line of it.
	lines := fixtureLines(t, premierFixture)
	for _, l := range []string{
		"UnityEngine.DebugLogHandler:Internal_Log(LogType, LogOption, String, Object)",
		"[UnityCrossThreadLogger]10/10/2026 12:44:31 PM",
		lines[lineIndex(t, lines, unityPrefix+"Client.SceneChange", 0)],
		lines[lineIndex(t, lines, requestPrefix+"EventJoin", 0)],
		"",
	} {
		var p Parser
		if n := testing.AllocsPerRun(100, func() { p.Line(l) }); n != 0 {
			t.Errorf("%.60q allocates %v times", l, n)
		}
	}
}

func BenchmarkParsingAWholeDraft(b *testing.B) {
	lines := fixtureLines(b, premierFixture)
	for b.Loop() {
		var p Parser
		for _, l := range lines {
			p.Line(l)
		}
	}
}

func TestABotDraftsPackSaysWhatWasPickedBeforeIt(t *testing.T) {
	quick, _ := parse(fixtureLines(t, quickFixture))
	var shown int
	for _, e := range quick {
		pack, ok := e.(PackShown)
		if !ok {
			continue
		}
		if want := (pack.Pack-1)*14 + pack.Pick - 1; pack.Picked == nil || len(pack.Picked) != want {
			t.Errorf("p%dp%d says %v were picked before it, want %d cards", pack.Pack, pack.Pick, pack.Picked, want)
		}
		shown++
	}
	if shown != 42 {
		t.Errorf("saw %d packs, want 42", shown)
	}

	premier, _ := parse(fixtureLines(t, premierFixture))
	for _, e := range premier {
		if pack, ok := e.(PackShown); ok && pack.Picked != nil {
			t.Errorf("p%dp%d of a Premier Draft says %v were picked, which its line does not say", pack.Pack, pack.Pick, pack.Picked)
		}
	}
}
