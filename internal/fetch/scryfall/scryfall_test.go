package scryfall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
)

// The pages under testdata/scryfall are written to Scryfall's documented
// schema, not recorded (see its README): the session that wrote this could
// not reach Scryfall.

// server serves the FRA search from testdata, in its two pages, and counts
// the requests it was sent. handle, when set, answers first; returning true
// means it did.
type server struct {
	*httptest.Server
	requests atomic.Int32

	mu      sync.Mutex
	queries []string
}

func newServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, n int) bool) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(s.requests.Add(1))
		s.mu.Lock()
		s.queries = append(s.queries, r.URL.RawQuery)
		s.mu.Unlock()
		if r.Header.Get("User-Agent") == "" || r.Header.Get("Accept") == "" {
			// Scryfall turns away requests without both.
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if handle != nil && handle(w, r, n) {
			return
		}
		if r.URL.Path != "/cards/search" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch q := r.URL.Query(); {
		case q.Get("q") == "e:fra game:arena" && q.Get("unique") == "prints" && q.Get("page") == "":
			s.serve(t, w, "fra-search-page1.json")
		case q.Get("q") == "e:fra game:arena" && q.Get("page") == "2":
			s.serve(t, w, "fra-search-page2.json")
		default:
			w.WriteHeader(http.StatusNotFound)
			s.serve(t, w, "search-not-found.json")
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// serve writes the testdata page name, pointing its next page at s.
func (s *server) serve(t *testing.T, w http.ResponseWriter, name string) {
	b, err := os.ReadFile("../../../testdata/scryfall/" + name)
	if err != nil {
		t.Error(err)
		return
	}
	_, _ = w.Write([]byte(strings.ReplaceAll(string(b), BaseURL, s.URL)))
}

func (s *server) source() Source {
	return Source{
		Client: fetch.New(fetch.Options{
			UserAgent: "mtga-draft-assistant/test (+https://example.invalid)",
			Transport: s.Client().Transport,
			Limits:    map[string]time.Duration{"127.0.0.1": time.Millisecond},
			Backoff:   time.Millisecond,
		}),
		BaseURL: s.URL,
	}
}

func byID(cs []cards.Card) map[int]cards.Card {
	m := make(map[int]cards.Card, len(cs))
	for _, c := range cs {
		m[c.ArenaID] = c
	}
	return m
}

func TestASetIsFetchedAcrossItsPagesByArenaID(t *testing.T) {
	srv := newServer(t, nil)
	got, err := srv.source().Set(t.Context(), "FRA")
	if err != nil {
		t.Fatal(err)
	}
	if n := srv.requests.Load(); n != 2 {
		t.Errorf("fetching a two-page set made %d requests", n)
	}
	m := byID(got)
	if len(m) != 13 || len(got) != 13 {
		t.Errorf("got %d cards (%d ids), want the 13 with Arena ids", len(got), len(m))
	}
	want := cards.Card{ArenaID: 106325, Set: "FRA", Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid", Rarity: "uncommon",
		ImageURL: "https://cards.scryfall.io/normal/front/0/0/00000000-0000-4000-8000-000000106325.jpg?1759190400", Produces: "G"}
	if m[106325] != want {
		t.Errorf("106325 = %+v, want %+v", m[106325], want)
	}
	if _, ok := m[106501]; !ok {
		t.Error("a card from the second page is missing")
	}
}

func TestTheSetIsAskedForByCodeEveryPrintAndOnlyOnArena(t *testing.T) {
	srv := newServer(t, nil)
	if _, err := srv.source().Set(t.Context(), "FRA"); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if got := srv.queries[0]; got != "q=e%3Afra+game%3Aarena&unique=prints" {
		t.Errorf("the search asked %q", got)
	}
}

func TestAPrintArenaDoesNotHaveIsLeftOut(t *testing.T) {
	got, err := newServer(t, nil).source().Set(t.Context(), "FRA")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Name == "Paper-Only Promo" || c.ArenaID == 0 {
			t.Errorf("got %+v, a print with no Arena id", c)
		}
	}
}

func TestATwoFacedCardTakesItsCostFromItsFaces(t *testing.T) {
	got, err := newServer(t, nil).source().Set(t.Context(), "FRA")
	if err != nil {
		t.Fatal(err)
	}
	c := byID(got)[106444]
	if c.Name != "Lanternwatch Recruit // Lanternwatch Captain" || c.ManaCost != "{1}{W}" || c.TypeLine != "Creature — Human Soldier // Creature — Human Soldier" {
		t.Errorf("the transforming card is %+v", c)
	}
}

func TestATwoFacedCardIsPicturedByItsFront(t *testing.T) {
	got, err := newServer(t, nil).source().Set(t.Context(), "FRA")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://cards.scryfall.io/normal/front/0/0/00000000-0000-4000-8000-000000106444.jpg?1759190400"
	if c := byID(got)[106444]; c.ImageURL != want {
		t.Errorf("the transforming card's picture is %q, want its front face's %q", c.ImageURL, want)
	}
}

func TestOnlyAPictureOnScryfallsImageHostIsKept(t *testing.T) {
	// An image is fetched without the user asking, so a page naming some
	// other address must not make the assistant go there.
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) bool {
		_, _ = w.Write([]byte(`{"object":"list","has_more":false,"data":[
			{"arena_id":1,"name":"Kept","image_uris":{"normal":"https://cards.scryfall.io/normal/front/a/b/1.jpg?1"}},
			{"arena_id":2,"name":"Elsewhere","image_uris":{"normal":"https://tracker.example/normal/2.jpg"}},
			{"arena_id":3,"name":"Plain HTTP","image_uris":{"normal":"http://cards.scryfall.io/normal/front/a/b/3.jpg"}},
			{"arena_id":4,"name":"Not an address","image_uris":{"normal":"::"}},
			{"arena_id":5,"name":"No picture"}
		]}`))
		return true
	})
	got, err := srv.source().Set(t.Context(), "FRA")
	if err != nil {
		t.Fatal(err)
	}
	m := byID(got)
	if m[1].ImageURL != "https://cards.scryfall.io/normal/front/a/b/1.jpg?1" {
		t.Errorf("the picture on Scryfall's host was dropped: %+v", m[1])
	}
	for _, id := range []int{2, 3, 4, 5} {
		if c, ok := m[id]; !ok || c.ImageURL != "" {
			t.Errorf("card %d = %+v (found %v), want it kept with no picture", id, c, ok)
		}
	}
}

func TestAnImageIsDownloadedNamedAndCapped(t *testing.T) {
	var ua string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		ua = r.Header.Get("User-Agent")
		switch r.URL.Path {
		case "/normal/front/a/b/1.jpg":
			_, _ = w.Write([]byte("jpeg bytes"))
		case "/huge.jpg":
			_, _ = w.Write(make([]byte, maxImageBytes+1))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
		return true
	})
	imgs := Images{Client: srv.source().Client}
	got, err := imgs.Image(t.Context(), srv.URL+"/normal/front/a/b/1.jpg")
	if err != nil || string(got) != "jpeg bytes" {
		t.Errorf("Image = %q, %v", got, err)
	}
	if ua == "" {
		t.Error("the image was asked for without naming the app")
	}
	if _, err := imgs.Image(t.Context(), srv.URL+"/huge.jpg"); err == nil {
		t.Error("an image larger than any card picture was accepted")
	}
	if _, err := imgs.Image(t.Context(), srv.URL+"/gone.jpg"); !fetch.IsStatus(err, http.StatusNotFound) {
		t.Errorf("a missing image returned %v, want the 404", err)
	}
}

func TestALandHasNoManaCost(t *testing.T) {
	got, err := newServer(t, nil).source().Set(t.Context(), "FRA")
	if err != nil {
		t.Fatal(err)
	}
	if c := byID(got)[106485]; c.Name != "Forest" || c.ManaCost != "" || c.TypeLine != "Basic Land — Forest" {
		t.Errorf("the basic land is %+v", c)
	}
}

func TestTheManaACardMakesIsKeptInWUBRGOrderAndNoneWhenScryfallListsNone(t *testing.T) {
	got, err := newServer(t, nil).source().Set(t.Context(), "FRA")
	if err != nil {
		t.Fatal(err)
	}
	m := byID(got)
	for id, want := range map[int]string{
		106391: "UG", // Mossback Pathway, which the page lists as G, U
		106485: "G",  // Forest
		106325: "G",  // Verdant Oathkeeper: a creature makes mana too
		106385: "",   // Tidecaller's Riddle makes none, and has no field
	} {
		if c := m[id]; c.Produces != want {
			t.Errorf("%s produces %q, want %q", c.Name, c.Produces, want)
		}
	}
}

func TestASetScryfallHasNeverHeardOfIsEmptyNotAnError(t *testing.T) {
	srv := newServer(t, nil)
	got, err := srv.source().Set(t.Context(), "ZZZ")
	if err != nil || len(got) != 0 {
		t.Errorf("an unknown set gave %d cards and %v, want none and no error", len(got), err)
	}
	if n := srv.requests.Load(); n != 1 {
		t.Errorf("an unknown set made %d requests, want 1", n)
	}
}

func TestAPageThatIsMissingForAnotherReasonIsAnError(t *testing.T) {
	// A 404 that is not Scryfall's "no cards matched" (a moved endpoint, a
	// proxy's page) must not be stored as a set with no cards.
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) bool {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>not here</html>"))
		return true
	})
	if _, err := srv.source().Set(t.Context(), "FRA"); err == nil {
		t.Error("a 404 with no Scryfall error object was taken for an empty set")
	}
}

func TestABusyScryfallIsBackedOffFromAndAskedAgain(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request, n int) bool {
		if n == 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		return false
	})
	got, err := srv.source().Set(t.Context(), "FRA")
	if err != nil || len(got) != 13 {
		t.Fatalf("got %d cards and %v, want the whole set after the retry", len(got), err)
	}
	if n := srv.requests.Load(); n != 3 {
		t.Errorf("made %d requests, want two pages and one retry", n)
	}
}

func TestAFailureOnALaterPageFailsTheWholeSet(t *testing.T) {
	// Half a set stored would be read as the whole set until the next
	// refresh, and its missing cards shown as unknown to Scryfall.
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusInternalServerError)
			return true
		}
		return false
	})
	got, err := srv.source().Set(t.Context(), "FRA")
	if err == nil || got != nil {
		t.Errorf("got %d cards and %v, want an error and no cards", len(got), err)
	}
	if want := "answered 500 Internal Server Error"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("the error says %q, want %q", err, want)
	}
}

func TestOfflineTheErrorSaysScryfallCannotBeReached(t *testing.T) {
	srv := newServer(t, nil)
	src := srv.source()
	src.BaseURL = "http://" + Host + ".invalid"
	_, err := src.Set(t.Context(), "FRA")
	if _, ok := errors.AsType[*fetch.HostError](err); !ok {
		t.Fatalf("got %v, want a HostError", err)
	}
	if !strings.HasPrefix(err.Error(), "can't reach ") {
		t.Errorf("the error says %q", err)
	}
}

func TestAPageThatIsNotJSONIsAnError(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) bool {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"arena_id":`)) // cut off
		return true
	})
	if _, err := srv.source().Set(t.Context(), "FRA"); err == nil {
		t.Error("a cut-off page was accepted")
	}
}

func TestANextPageOnAnotherHostIsNotFollowed(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		if r.URL.Query().Get("page") == "" {
			_, _ = w.Write([]byte(`{"object":"list","has_more":true,"next_page":"https://elsewhere.example/cards/search?page=2","data":[]}`))
			return true
		}
		return false
	})
	if _, err := srv.source().Set(t.Context(), "FRA"); err == nil {
		t.Error("a next page on another host was followed")
	}
	if n := srv.requests.Load(); n != 1 {
		t.Errorf("made %d requests", n)
	}
}

func TestASearchThatNeverEndsIsCutOff(t *testing.T) {
	srv := newServer(t, nil)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		srv.requests.Add(1)
		_, _ = w.Write([]byte(`{"object":"list","has_more":true,"next_page":"` + srv.URL + `/cards/search?page=n","data":[]}`))
	})
	if _, err := srv.source().Set(t.Context(), "FRA"); err == nil {
		t.Error("an endless search returned no error")
	}
	if n := srv.requests.Load(); n != maxPages {
		t.Errorf("made %d requests, want %d", n, maxPages)
	}
}

func TestASetCodeThatCouldChangeTheQueryIsRefusedWithoutARequest(t *testing.T) {
	srv := newServer(t, nil)
	for _, set := range []string{"", "F", "FRA game:paper", "FRA&q=x", "ABCDEFGHI", "é"} {
		if _, err := srv.source().Set(t.Context(), set); err == nil {
			t.Errorf("Set(%q) succeeded", set)
		}
	}
	if n := srv.requests.Load(); n != 0 {
		t.Errorf("made %d requests for set codes it refused", n)
	}
}

func TestFetchingASetStopsWithItsContext(t *testing.T) {
	srv := newServer(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := srv.source().Set(ctx, "FRA"); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled fetch returned %v", err)
	}
}
