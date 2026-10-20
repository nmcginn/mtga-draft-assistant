package app

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
)

// fra is a few of FRA's cards, as the card task reports them: p1p1 but its
// last card, 106453, which stands for one Scryfall does not know yet.
var fra = CardsLoaded{Set: "FRA", Cards: []cards.Card{
	{ArenaID: 106325, Set: "FRA", Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid", Rarity: "uncommon"},
	{ArenaID: 106235, Set: "FRA", Name: "Thornwood Scout", ManaCost: "{G}", TypeLine: "Creature — Elf Scout", Rarity: "common"},
}}

func inDraft(events ...arena.Event) Model {
	return following().Apply(DraftEvents{Events: append([]arena.Event{started}, events...)})
}

func TestALoadedSetNamesThePackWithEachCardsDetails(t *testing.T) {
	s := inDraft(p1p1).Apply(fra).DraftScreen()
	want := []CardLine{
		{Name: "Verdant Oathkeeper", Detail: "2GG · Creature — Elf Druid · Uncommon"},
		{Name: "Thornwood Scout", Detail: "G · Creature — Elf Scout · Common"},
		{Name: "Card 106453", Detail: "Not on Scryfall yet"},
	}
	if !slices.Equal(s.Cards, want) {
		t.Errorf("pack = %+v, want %+v", s.Cards, want)
	}
	if s.Notice != "" {
		t.Errorf("notice %q, though the set is known and its missing card says why on its own line", s.Notice)
	}
	if s.Footnote != "Card details from Scryfall." {
		t.Errorf("footnote %q, want Scryfall credited", s.Footnote)
	}
}

func TestPicksThePickJustMadeAndThePoolAreNamedToo(t *testing.T) {
	m := inDraft(p1p1, arena.PickMade{Pack: 1, Pick: 1, Card: 106325}).Apply(fra)
	s := m.DraftScreen()
	if s.Heading != "You took Verdant Oathkeeper. Waiting for the next pack." {
		t.Errorf("heading %q", s.Heading)
	}
	if !slices.Equal(s.Picks, []string{"Pack 1: Verdant Oathkeeper"}) {
		t.Errorf("picks %q", s.Picks)
	}
	// The pool, grouped once its cards are known (decision 30), names
	// every card in it, whether or not the picks read match it.
	done := m.Apply(DraftEvents{Events: []arena.Event{arena.DraftEnded{Pool: []int{106325}}}}).DraftScreen()
	if len(done.Pool) != 1 || !slices.Equal(done.Pool[0].Cards, []string{"Verdant Oathkeeper · 2GG"}) {
		t.Errorf("pool %q", done.Pool)
	}
	odd := m.Apply(DraftEvents{Events: []arena.Event{arena.DraftEnded{Pool: []int{106235, 106325}}}}).DraftScreen()
	if len(odd.Pool) != 1 || !slices.Equal(odd.Pool[0].Cards, []string{"Thornwood Scout · G", "Verdant Oathkeeper · 2GG"}) {
		t.Errorf("pool %q", odd.Pool)
	}
}

func TestBeforeTheSetIsLookedForTheWindowSaysCardNamesAreLoading(t *testing.T) {
	s := inDraft(p1p1).DraftScreen()
	if s.Notice != "Loading card names…" {
		t.Errorf("notice %q", s.Notice)
	}
	if s.Cards[0] != (CardLine{Name: "Card 106325"}) {
		t.Errorf("card %+v, want it by id with no detail yet", s.Cards[0])
	}
}

func TestWhileTheSetIsFetchedTheNoticeSaysFromWhereAndThatItIsOnce(t *testing.T) {
	s := inDraft(p1p1).Apply(CardsFetching{Set: "FRA"}).DraftScreen()
	if !strings.HasPrefix(s.Notice, "Fetching FRA's cards from Scryfall.") || !strings.Contains(s.Notice, "once per set") {
		t.Errorf("notice %q", s.Notice)
	}
	if s.Cards[2] != (CardLine{Name: "Card 106453"}) {
		t.Errorf("card %+v: not yet known to be missing from Scryfall", s.Cards[2])
	}
}

func TestAFailedFetchSaysWhyWhatToDoAndWhenItIsTriedAgain(t *testing.T) {
	m := inDraft(p1p1).Apply(CardsFetching{Set: "FRA"}).
		Apply(CardsFailed{Set: "FRA", Err: errors.New("can't reach api.scryfall.com"), RetryIn: 30 * time.Second})
	want := "Couldn't get FRA's cards from Scryfall (can't reach api.scryfall.com). Check your internet connection; " +
		"the assistant tries again in 30 seconds, and shows cards by their Arena id until then."
	if s := m.DraftScreen(); s.Notice != want {
		t.Errorf("notice %q, want %q", s.Notice, want)
	}
	// Trying again says so, rather than repeating the old failure.
	if s := m.Apply(CardsFetching{Set: "FRA"}).DraftScreen(); !strings.HasPrefix(s.Notice, "Fetching") {
		t.Errorf("retrying, notice %q", s.Notice)
	}
	if s := m.Apply(fra).DraftScreen(); s.Notice != "" || s.Cards[0].Name != "Verdant Oathkeeper" {
		t.Errorf("after a retry worked: notice %q, card %+v", s.Notice, s.Cards[0])
	}
}

func TestAFailureWithNoReasonStillSaysWhatToDo(t *testing.T) {
	s := inDraft(p1p1).Apply(CardsFailed{Set: "FRA"}).DraftScreen()
	if !strings.Contains(s.Notice, "(no reason given)") || !strings.Contains(s.Notice, "tries again shortly") {
		t.Errorf("notice %q", s.Notice)
	}
}

func TestCardsAlreadyShownAreKeptWhenARefreshFails(t *testing.T) {
	m := inDraft(p1p1).Apply(fra).Apply(CardsFetching{Set: "FRA"}).
		Apply(CardsFailed{Set: "FRA", Err: errors.New("api.scryfall.com is busy")})
	if s := m.DraftScreen(); s.Notice != "" || s.Cards[0].Name != "Verdant Oathkeeper" {
		t.Errorf("after a failed refresh: notice %q, card %+v", s.Notice, s.Cards[0])
	}
}

func TestARefreshedSetReplacesTheOldOne(t *testing.T) {
	newer := CardsLoaded{Set: "FRA", Cards: []cards.Card{{ArenaID: 106453, Set: "FRA", Name: "Gilded Pathfinder", TypeLine: "Creature — Elf Ranger", Rarity: "common"}}}
	s := inDraft(p1p1).Apply(fra).Apply(newer).DraftScreen()
	if s.Cards[2].Name != "Gilded Pathfinder" || s.Cards[0].Name != "Card 106325" {
		t.Errorf("after a refresh: %+v", s.Cards)
	}
}

func TestASetScryfallHasNoCardsForShowsEveryCardByIDAsUnknownToIt(t *testing.T) {
	s := inDraft(p1p1).Apply(CardsLoaded{Set: "FRA"}).DraftScreen()
	if s.Notice != "" || s.Cards[0] != (CardLine{Name: "Card 106325", Detail: "Not on Scryfall yet"}) {
		t.Errorf("notice %q, card %+v", s.Notice, s.Cards[0])
	}
	if s.Footnote != "" {
		t.Errorf("footnote %q credits Scryfall for no cards", s.Footnote)
	}
}

func TestADraftWhoseSetTheLogNeverSaidIsNamedFromAnySetKnown(t *testing.T) {
	unnamed := following().Apply(DraftEvents{Events: []arena.Event{p1p1}})
	if s := unnamed.DraftScreen(); !strings.Contains(s.Notice, "has not said which set") || s.Cards[0].Name != "Card 106325" {
		t.Errorf("unknown set, nothing loaded: notice %q, card %+v", s.Notice, s.Cards[0])
	}
	s := unnamed.Apply(fra).DraftScreen()
	if s.Cards[0].Name != "Verdant Oathkeeper" {
		t.Errorf("unknown set, FRA loaded: card %+v", s.Cards[0])
	}
	// 106453 is still unnamed, and why is the set's not being known.
	if !strings.Contains(s.Notice, "has not said which set") || s.Cards[2] != (CardLine{Name: "Card 106453"}) {
		t.Errorf("unknown set, FRA loaded: notice %q, card %+v", s.Notice, s.Cards[2])
	}
}

func TestEveryCardNamedLeavesNoNotice(t *testing.T) {
	s := inDraft(arena.PackShown{Pack: 1, Pick: 2, Cards: []int{106235}}).Apply(fra).DraftScreen()
	if s.Notice != "" {
		t.Errorf("notice %q", s.Notice)
	}
}

func TestCardsKnownOutliveTheLogTheyWereNeededFor(t *testing.T) {
	m := inDraft(p1p1).Apply(fra)
	for _, msg := range []Msg{LogLost{}, LogFound{Path: "/logs/Player.log"}, SessionStarted{}} {
		m = m.Apply(msg)
		if !m.CardSets["FRA"].Loaded {
			t.Fatalf("after %T the cards known were forgotten", msg)
		}
	}
}

func TestApplyingCardsDoesNotChangeAModelHeldElsewhere(t *testing.T) {
	before := inDraft(p1p1).Apply(CardsFetching{Set: "FRA"})
	_ = before.Apply(fra)
	_ = before.Apply(CardsFailed{Set: "FRA", Err: errors.New("x")})
	_ = before.Apply(CardsLoaded{Set: "DSK"})
	if s := before.CardSets["FRA"]; !s.Fetching || s.Loaded || len(before.CardSets) != 1 {
		t.Errorf("the earlier model changed: %+v", before.CardSets)
	}
}

func TestAnUnusableCacheIsNotedWithItsReasonAndWhatToDo(t *testing.T) {
	err := &fs.PathError{Op: "mkdir", Path: "/home/me/.cache/mtga-draft-assistant", Err: errors.New("permission denied")}
	m := inDraft(p1p1).Apply(fra).Apply(CacheUnavailable{Dir: "/home/me/.cache/mtga-draft-assistant", Err: err})
	s := m.DraftScreen()
	for _, want := range []string{
		"can't be saved in /home/me/.cache/mtga-draft-assistant (permission denied)",
		"fetched from Scryfall again each time",
		"write to that folder, or delete it",
	} {
		if !strings.Contains(s.Footnote, want) {
			t.Errorf("footnote %q, want %q", s.Footnote, want)
		}
	}
	if strings.Count(s.Footnote, "/home/me/.cache") != 1 {
		t.Errorf("footnote %q names the folder more than once", s.Footnote)
	}
	// Catching up still says first that the pack may be old.
	catching := m.Apply(SessionStarted{}).Apply(DraftEvents{Events: []arena.Event{started, p1p1}})
	if s := catching.DraftScreen(); !strings.Contains(s.Footnote, "Catching up") {
		t.Errorf("catching up, footnote %q", s.Footnote)
	}
}

func TestACacheProblemWithNoFolderOrReasonStillReads(t *testing.T) {
	s := inDraft(p1p1).Apply(CacheUnavailable{}).DraftScreen()
	if !strings.Contains(s.Footnote, "can't be saved in the cache folder (no reason given)") {
		t.Errorf("footnote %q", s.Footnote)
	}
}

func TestRetryTimesReadAsAPersonWouldSayThem(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                "shortly",
		time.Second:      "in 1 second",
		30 * time.Second: "in 30 seconds",
		time.Minute:      "in 1 minute",
		90 * time.Second: "in 2 minutes",
		5 * time.Minute:  "in 5 minutes",
	} {
		if got := within(d); got != want {
			t.Errorf("within(%v) = %q, want %q", d, got, want)
		}
	}
}
