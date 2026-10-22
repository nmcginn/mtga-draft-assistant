package background

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch/scryfall"
	"github.com/nmcginn/mtga-draft-assistant/internal/store"
)

var (
	oathkeeper = cards.Card{ArenaID: 106325, Set: "FRA", Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid", Rarity: "uncommon"}
	scout      = cards.Card{ArenaID: 106235, Set: "FRA", Name: "Thornwood Scout", ManaCost: "{G}", TypeLine: "Creature — Elf Scout", Rarity: "common"}
	now        = time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
)

// fakeSource answers Set from a script: each call takes the next answer,
// and the last answer repeats.
type fakeSource struct {
	mu      sync.Mutex
	answers []answer
	calls   []string
}

type answer struct {
	cards []cards.Card
	err   error
}

func (f *fakeSource) Set(ctx context.Context, set string) ([]cards.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, set)
	a := f.answers[min(len(f.calls), len(f.answers))-1]
	return a.cards, a.err
}

func (f *fakeSource) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func cardSetup(dir string, src SetSource) CardSetup {
	return CardSetup{
		Dir: dir, Source: src, Now: func() time.Time { return now },
		RetryAfter: func(int) time.Duration { return 10 * time.Millisecond },
	}
}

// startCards starts the card task and returns it with a way to read what it
// posts.
func startCards(t *testing.T, s CardSetup) (*Cards, func() app.Msg) {
	t.Helper()
	post, next := collect(t)
	c := NewCards(s)
	c.Start(t.Context(), post)
	return c, next
}

// quiet fails if the task posts anything within a short while.
func quiet(t *testing.T, post func() (app.Msg, bool)) {
	t.Helper()
	if m, ok := post(); ok {
		t.Errorf("posted %T %+v, want nothing", m, m)
	}
}

func TestASetNeverSeenIsFetchedShownAndStored(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{answers: []answer{{cards: []cards.Card{oathkeeper, scout}}}}
	c, next := startCards(t, cardSetup(dir, src))
	c.Want("FRA")
	if m := expect[app.CardsFetching](t, next); m.Set != "FRA" {
		t.Errorf("fetching %q", m.Set)
	}
	if m := expect[app.CardsLoaded](t, next); m.Set != "FRA" || len(m.Cards) != 2 {
		t.Errorf("loaded %+v", m)
	}
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if got, ok, err := st.Set(t.Context(), "FRA"); !ok || err != nil || len(got.Cards) != 2 || !got.FetchedAt.Equal(now) {
		t.Errorf("stored %+v, %v, %v", got, ok, err)
	}
}

func TestASetAlreadyStoredIsShownWithoutAskingScryfall(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSet(t.Context(), "FRA", now.Add(-time.Hour), []cards.Card{oathkeeper}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	src := &fakeSource{answers: []answer{{err: errors.New("should not be asked")}}}
	c, next := startCards(t, cardSetup(dir, src))
	c.Want("FRA")
	if m := expect[app.CardsLoaded](t, next); len(m.Cards) != 1 || m.Cards[0] != oathkeeper {
		t.Errorf("loaded %+v", m)
	}
	time.Sleep(20 * time.Millisecond)
	if got := src.asked(); len(got) != 0 {
		t.Errorf("asked the source for %v, though the set was stored an hour ago", got)
	}
}

func TestAStoredSetAWeekOldIsShownAtOnceThenFetchedAgain(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSet(t.Context(), "FRA", now.Add(-8*24*time.Hour), []cards.Card{oathkeeper}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	src := &fakeSource{answers: []answer{{cards: []cards.Card{oathkeeper, scout}}}}
	c, next := startCards(t, cardSetup(dir, src))
	c.Want("FRA")
	if m := expect[app.CardsLoaded](t, next); len(m.Cards) != 1 {
		t.Errorf("first, the stored set: %+v", m)
	}
	// No "fetching" notice: the stored cards are on screen already.
	if m := expect[app.CardsLoaded](t, next); len(m.Cards) != 2 {
		t.Errorf("then, the fresh one: %+v", m)
	}
	st, err = store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if got, _, _ := st.Set(t.Context(), "FRA"); len(got.Cards) != 2 || !got.FetchedAt.Equal(now) {
		t.Errorf("after the refresh the store holds %+v", got)
	}
}

func TestAFailedRefreshKeepsTheStoredCardsAndSaysNothing(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSet(t.Context(), "FRA", now.Add(-8*24*time.Hour), []cards.Card{oathkeeper}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	src := &fakeSource{answers: []answer{{err: &fetch.HostError{Host: scryfall.Host}}}}
	post, arrive, next := collectQuietly(t)
	c := NewCards(cardSetup(dir, src))
	c.Start(t.Context(), post)
	c.Want("FRA")
	if m := expect[app.CardsLoaded](t, arrive); len(m.Cards) != 1 {
		t.Errorf("loaded %+v", m)
	}
	quiet(t, next)
	if got := src.asked(); len(got) != 1 {
		t.Errorf("asked %v, want one refresh and no retries: the stored cards stand until next launch", got)
	}
}

func TestASetScryfallHadNoCardsForIsAskedAboutAgainADayLater(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSet(t.Context(), "NEW", now.Add(-25*time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	src := &fakeSource{answers: []answer{{cards: []cards.Card{oathkeeper}}}}
	c, next := startCards(t, cardSetup(dir, src))
	c.Want("NEW")
	if m := expect[app.CardsLoaded](t, next); len(m.Cards) != 0 {
		t.Errorf("first, the stored empty set: %+v", m)
	}
	if m := expect[app.CardsLoaded](t, next); len(m.Cards) != 1 {
		t.Errorf("then, the refreshed set: %+v", m)
	}
}

func TestOfflineTheWindowIsToldAndTheSetIsTriedAgainUntilItArrives(t *testing.T) {
	src := &fakeSource{answers: []answer{
		{err: &fetch.HostError{Host: scryfall.Host}},
		{err: &fetch.HostError{Host: scryfall.Host}},
		{cards: []cards.Card{oathkeeper}},
	}}
	s := cardSetup(t.TempDir(), src)
	var tries []int
	s.RetryAfter = func(n int) time.Duration { tries = append(tries, n); return 10 * time.Millisecond }
	c, next := startCards(t, s)
	c.Want("FRA")
	expect[app.CardsFetching](t, next)
	f := expect[app.CardsFailed](t, next)
	if f.Set != "FRA" || f.RetryIn != 10*time.Millisecond || f.Err == nil || f.Err.Error() != "can't reach api.scryfall.com" {
		t.Errorf("failed %+v", f)
	}
	expect[app.CardsFetching](t, next)
	expect[app.CardsFailed](t, next)
	expect[app.CardsFetching](t, next)
	if m := expect[app.CardsLoaded](t, next); len(m.Cards) != 1 {
		t.Errorf("loaded %+v", m)
	}
	if len(tries) != 2 || tries[0] != 1 || tries[1] != 2 {
		t.Errorf("waits asked for failures %v, want 1 then 2 in a row", tries)
	}
}

func TestTheDefaultRetriesBackOffToFiveMinutes(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i, w := range want {
		if got := retryAfter(i + 1); got != w {
			t.Errorf("retryAfter(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestTheSameSetWantedAgainIsNotFetchedAgain(t *testing.T) {
	src := &fakeSource{answers: []answer{{cards: []cards.Card{oathkeeper}}}}
	post, arrive, next := collectQuietly(t)
	c := NewCards(cardSetup(t.TempDir(), src))
	c.Start(t.Context(), post)
	c.Want("FRA")
	expect[app.CardsFetching](t, arrive)
	expect[app.CardsLoaded](t, arrive)
	c.Want("FRA")
	quiet(t, next)
	// Another set, then FRA again before the task looks: FRA is the one
	// wanted, and it is already on screen.
	c.Want("DSK")
	c.Want("FRA")
	time.Sleep(20 * time.Millisecond)
	for _, set := range src.asked()[1:] {
		if set == "FRA" {
			t.Errorf("asked %v: FRA a second time", src.asked())
		}
	}
}

func TestOnlyTheLatestSetWantedIsFetched(t *testing.T) {
	// The card task is busy with nothing when many drafts' starts arrive at
	// once (catching up on a long session): it wakes once, for the last.
	src := &fakeSource{answers: []answer{{cards: []cards.Card{oathkeeper}}}}
	c := NewCards(cardSetup(t.TempDir(), src))
	for _, set := range []string{"DSK", "BLB", "FRA"} {
		c.Want(set)
	}
	post, next := collect(t)
	c.Start(t.Context(), post)
	if m := expect[app.CardsFetching](t, next); m.Set != "FRA" {
		t.Errorf("fetched %q first, want the latest, FRA", m.Set)
	}
	expect[app.CardsLoaded](t, next)
	if got := src.asked(); len(got) != 1 {
		t.Errorf("asked %v, want only FRA", got)
	}
}

func TestAnUnwritableCacheIsReportedAndTheCardsStillArrive(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(parent, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(parent, "mtga-draft-assistant")
	src := &fakeSource{answers: []answer{{cards: []cards.Card{oathkeeper}}}}
	c, next := startCards(t, cardSetup(dir, src))
	c.Want("FRA")
	if m := expect[app.CacheUnavailable](t, next); m.Dir != dir || m.Err == nil {
		t.Errorf("cache unavailable %+v", m)
	}
	expect[app.CardsFetching](t, next)
	if m := expect[app.CardsLoaded](t, next); len(m.Cards) != 1 {
		t.Errorf("loaded %+v", m)
	}
	m := app.New().Apply(app.CacheUnavailable{Dir: dir, Err: errors.New("x")})
	if m.CacheProblem == "" {
		t.Error("the model kept no note of it")
	}
}

func TestWithNoCacheFolderAtAllTheWindowIsToldWhy(t *testing.T) {
	s := cardSetup("", &fakeSource{answers: []answer{{}}})
	s.DirErr = errors.New("neither $XDG_CACHE_HOME nor $HOME are defined")
	c, next := startCards(t, s)
	c.Want("FRA")
	if m := expect[app.CacheUnavailable](t, next); m.Dir != "" || m.Err == nil || !strings.Contains(m.Err.Error(), "HOME") {
		t.Errorf("cache unavailable %+v", m)
	}
}

func TestACorruptCacheIsReportedAndTheCardsStillArrive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, store.FileName), []byte(strings.Repeat("not sqlite ", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	c, next := startCards(t, cardSetup(dir, &fakeSource{answers: []answer{{cards: []cards.Card{scout}}}}))
	c.Want("FRA")
	expect[app.CacheUnavailable](t, next)
	expect[app.CardsFetching](t, next)
	expect[app.CardsLoaded](t, next)
}

func TestNothingIsFetchedOrOpenedUntilADraftNeedsASet(t *testing.T) {
	src := &fakeSource{answers: []answer{{}}}
	dir := filepath.Join(t.TempDir(), "cache")
	post, _, next := collectQuietly(t)
	NewCards(cardSetup(dir, src)).Start(t.Context(), post)
	quiet(t, next)
	if got := src.asked(); len(got) != 0 {
		t.Errorf("asked %v with no draft", got)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the cache folder was created with no draft (%v)", err)
	}
}

func TestOnceTheWindowClosesTheCardTaskStops(t *testing.T) {
	block := make(chan struct{})
	src := &blockingSource{release: block}
	ctx, cancel := context.WithCancel(t.Context())
	post, arrive, next := collectQuietly(t)
	c := NewCards(cardSetup(t.TempDir(), src))
	c.Start(ctx, post)
	c.Want("FRA")
	expect[app.CardsFetching](t, arrive)
	cancel()
	close(block)
	quiet(t, next)
}

// blockingSource holds every request until released or cancelled, as a slow
// network would.
type blockingSource struct{ release chan struct{} }

func (b *blockingSource) Set(ctx context.Context, _ string) ([]cards.Card, error) {
	select {
	case <-b.release:
		return []cards.Card{oathkeeper}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// collectQuietly is collect for tests that expect silence too: arrive
// waits for a message as collect's next does, up to 5 s, since opening the
// cache and running its migrations under the race detector can take far
// longer than a frame; next reports whether anything arrived within 50 ms,
// rather than failing, and is only for asserting that nothing does.
func collectQuietly(t *testing.T) (post Post, arrive func() app.Msg, next func() (app.Msg, bool)) {
	t.Helper()
	ch := make(chan app.Msg, 10)
	post = func(m app.Msg) { ch <- m }
	arrive = func() app.Msg {
		t.Helper()
		select {
		case m := <-ch:
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("no message within 5s")
			return nil
		}
	}
	next = func() (app.Msg, bool) {
		select {
		case m := <-ch:
			return m, true
		case <-time.After(50 * time.Millisecond):
			return nil, false
		}
	}
	return post, arrive, next
}

// scryfallServer serves testdata/scryfall's FRA pages, counting requests.
func scryfallServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		page := "fra-search-page1.json"
		if r.URL.Query().Get("page") == "2" {
			page = "fra-search-page2.json"
		}
		b, err := os.ReadFile("../../testdata/scryfall/" + page)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(string(b), scryfall.BaseURL, srv.URL)))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestASecondLaunchDraftingTheSameSetMakesNoRequests(t *testing.T) {
	// End to end through the real client, Scryfall source, and store: the
	// acceptance test for "subsequent drafts of the set make no Scryfall
	// API requests".
	srv, requests := scryfallServer(t)
	dir := t.TempDir()
	src := scryfall.Source{
		Client: fetch.New(fetch.Options{
			UserAgent: "mtga-draft-assistant/test", Transport: srv.Client().Transport,
			Limits: map[string]time.Duration{"127.0.0.1": time.Millisecond},
		}),
		BaseURL: srv.URL,
	}
	for launch := 1; launch <= 2; launch++ {
		ctx, cancel := context.WithCancel(t.Context())
		post, next := collect(t)
		c := NewCards(CardSetup{Dir: dir, Source: src})
		c.Start(ctx, post)
		c.Want("FRA")
		m := applyUntil[app.CardsLoaded](t, app.New(), next)
		if s := m.CardSets["FRA"]; len(s.Cards) != 13 || s.Cards[106325].Name != "Verdant Oathkeeper" {
			t.Errorf("launch %d: FRA is %+v", launch, s)
		}
		cancel()
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("two launches made %d requests, want the first launch's two pages only", got)
	}
}
