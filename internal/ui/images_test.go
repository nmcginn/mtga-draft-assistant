package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-gui-org/go-gui/gui"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/background"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch/scryfall"
)

// cardJPEG is a small, real card picture.
func cardJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 49, 68)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pictureOnDisk writes a card picture where go-gui will look for it.
func pictureOnDisk(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "0123456789abcdef0123456789abcdef.jpg")
	if err := os.WriteFile(path, cardJPEG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// withPictures is fraCards with a picture address for every card.
func withPictures() app.CardsLoaded {
	loaded := app.CardsLoaded{Set: fraCards.Set, Cards: slices.Clone(fraCards.Cards)}
	for i := range loaded.Cards {
		loaded.Cards[i].ImageURL = "https://cards.scryfall.io/normal/front/0/0/" + loaded.Cards[i].Name + ".jpg"
	}
	return loaded
}

// gridRows is how many rows the pack's grid laid its cards out in.
func gridRows(t *testing.T, ly *gui.Layout) int {
	t.Helper()
	var grid *gui.Layout
	var walk func(*gui.Layout)
	walk = func(l *gui.Layout) {
		if l.Shape != nil && l.Shape.ID == "draft-grid" {
			grid = l
		}
		for i := range l.Children {
			walk(&l.Children[i])
		}
	}
	walk(ly)
	if grid == nil {
		t.Fatal("no pack grid on screen")
	}
	return len(grid.Children)
}

func TestACardWhosePictureIsOnDiskIsShownAsThatPicture(t *testing.T) {
	path := pictureOnDisk(t)
	model := drafting(premierStart, firstPack).Apply(withPictures()).Apply(app.ImageReady{ArenaID: 106325, Path: path})
	w, got := render(t, model, defaultWidth, defaultHeight)
	ly := w.TestRender(nil)
	img := find(ly, "draft-card-0-image")
	if img == nil || img.Resource != path {
		t.Fatalf("card 0 is %+v, want its picture from %q", img, path)
	}
	if slices.Contains(got, "Verdant Oathkeeper") {
		t.Errorf("window shows %q: the card's name in text as well as on its picture", got)
	}
	// The next card's picture is still coming: it is named meanwhile.
	if !slices.Contains(got, "Thornwood Scout") || find(ly, "draft-card-1-image") != nil {
		t.Errorf("window shows %q; want card 1 as a placeholder with its name", got)
	}
	if !slices.Contains(got, "Card details and images from Scryfall.") {
		t.Errorf("window shows %q, want Scryfall credited for the pictures", got)
	}
}

func TestAPictureAndThePlaceholderBeforeItTakeTheSameSpace(t *testing.T) {
	// The grid must not jump as pictures arrive under the user's eyes.
	path := pictureOnDisk(t)
	model := drafting(premierStart, firstPack).Apply(withPictures()).Apply(app.ImageReady{ArenaID: 106325, Path: path})
	w := gui.NewTestWindow(t, windowCfg(t.Context(), model, Options{}))
	ly := w.TestRender(nil)
	pictured, waiting := find(ly, "draft-card-0-box"), find(ly, "draft-card-1-box")
	if pictured == nil || waiting == nil {
		t.Fatal("card tiles missing")
	}
	if pictured.Width != waiting.Width || pictured.Height != waiting.Height || pictured.Width != cardWidth || pictured.Height != cardHeight {
		t.Errorf("picture %vx%v, placeholder %vx%v; want both %vx%v",
			pictured.Width, pictured.Height, waiting.Width, waiting.Height, cardWidth, cardHeight)
	}
}

func TestThePackReflowsWithTheWindowsWidth(t *testing.T) {
	model := drafting(premierStart, firstPack).Apply(fraCards)
	rows := map[int]int{}
	for _, width := range []int{minWidth, defaultWidth, 900} {
		w, _ := render(t, model, width, defaultHeight)
		rows[width] = gridRows(t, w.TestRender(nil))
	}
	cards := len(firstPack.Cards)
	if rows[minWidth] != cards {
		t.Errorf("at the narrowest, %d rows for %d cards; want one card a row", rows[minWidth], cards)
	}
	if rows[defaultWidth] != (cards+1)/2 {
		t.Errorf("at the default width, %d rows for %d cards; want two abreast", rows[defaultWidth], cards)
	}
	if rows[900] >= rows[defaultWidth] {
		t.Errorf("at 900px, %d rows; want fewer than the %d at the default width", rows[900], rows[defaultWidth])
	}
}

func TestAPlaceholderIsPaintedInTheThemesSurfaceColour(t *testing.T) {
	custom := Dark
	custom.Surface = gui.Hex(0x445566)
	model := drafting(premierStart, firstPack).Apply(fraCards)
	w := gui.NewTestWindow(t, gui.WindowCfg{State: &state{model: model, theme: custom}})
	ly := w.TestRender(mainView)
	if s := find(ly, "draft-card-0-box"); s == nil || s.Color != custom.Surface {
		t.Error("the placeholder did not paint the theme's surface colour")
	}
}

func TestADownloadThatFailedIsExplainedAboveThePack(t *testing.T) {
	model := drafting(premierStart, firstPack).Apply(withPictures()).
		Apply(app.ImageFailed{ArenaID: 106235, Err: &fetch.HostError{Host: "cards.scryfall.io"}, RetryIn: time.Minute})
	_, got := render(t, model, defaultWidth, defaultHeight)
	notice := model.DraftScreen().Notice
	if !slices.Contains(got, notice) || !strings.Contains(notice, "can't reach cards.scryfall.io") {
		t.Errorf("window shows %q, want the notice %q", got, notice)
	}
	if !slices.Contains(got, "Thornwood Scout") {
		t.Errorf("window shows %q, want the card named in its place", got)
	}
}

func TestPicturesFitATinyWindow(t *testing.T) {
	path := pictureOnDisk(t)
	model := drafting(premierStart, firstPack).Apply(withPictures()).Apply(app.ImageReady{ArenaID: 106325, Path: path})
	w, got := render(t, model, 1, 1)
	if find(w.TestRender(nil), "draft-card-0-image") == nil || !slices.Contains(got, "Thornwood Scout") {
		t.Errorf("at 1x1 the window shows %q, and lost the picture or the placeholders", got)
	}
}

// wants records what the window tells the image task.
type wants struct {
	mu   sync.Mutex
	last []app.ImageWant
	n    int
}

func (r *wants) want(ws []app.ImageWant) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last, r.n = slices.Clone(ws), r.n+1
}

func TestTheWindowTellsTheImageTaskWhatIsOnScreen(t *testing.T) {
	rec := &wants{}
	w := gui.NewTestWindow(t, windowCfg(t.Context(), drafting(premierStart), Options{ShowImages: rec.want}))
	w.TestRender(nil)
	Post(w, app.DraftEvents{Events: []arena.Event{firstPack}})
	Post(w, withPictures())
	w.FrameFn()
	if len(rec.last) != len(fraCards.Cards) || rec.last[0].ArenaID != 106325 {
		t.Errorf("told %+v, want the pack's %d pictures in pack order", rec.last, len(fraCards.Cards))
	}

	Post(w, app.DraftEvents{Events: []arena.Event{arena.PickMade{Pack: 1, Pick: 1, Card: 106325}}})
	w.FrameFn()
	if len(rec.last) != 0 {
		t.Errorf("after the pick, told %+v; want nothing on screen", rec.last)
	}
}

func TestAWindowThatOpensOnAPackAsksForItsPicturesAtOnce(t *testing.T) {
	rec := &wants{}
	cfg := windowCfg(t.Context(), drafting(premierStart, firstPack).Apply(withPictures()), Options{ShowImages: rec.want})
	w := gui.NewTestWindow(t, cfg)
	w.TestRender(nil)
	if len(rec.last) != len(fraCards.Cards) {
		t.Errorf("told %+v before any message, want the pack's pictures", rec.last)
	}
}

// firstPackLog is the Premier Draft fixture cut just after its first pack
// is shown, as the log reads mid-draft, waiting on the first pick.
func firstPackLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/logs/2026-10-fra-premier-full-draft.log")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(b), "\n")
	var p arena.Parser
	for i, l := range lines {
		for _, e := range p.Line(strings.TrimRight(l, "\r\n")) {
			if _, ok := e.(arena.PackShown); ok {
				path := filepath.Join(t.TempDir(), "Player.log")
				if err := os.WriteFile(path, []byte(strings.Join(lines[:i+1], "")), 0o644); err != nil {
					t.Fatal(err)
				}
				return path
			}
		}
	}
	t.Fatal("the fixture shows no pack")
	return ""
}

func TestTheRealTasksPutAFixturesPackOnScreenWithPicturesFromScryfall(t *testing.T) {
	// End to end, without a display or the network: the log stops at the
	// first pack; the card task fetches FRA and the image task each card's
	// picture, both from an httptest server standing in for Scryfall's API
	// and its image host at their real addresses, into a fresh cache.
	picture := cardJPEG(t)
	var mu sync.Mutex
	asked := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.Host]++
		mu.Unlock()
		switch r.Host {
		case scryfall.Host:
			page := "fra-search-page1.json"
			if r.URL.Query().Get("page") == "2" {
				page = "fra-search-page2.json"
			}
			b, err := os.ReadFile("../../testdata/scryfall/" + page)
			if err != nil {
				t.Error(err)
			}
			_, _ = w.Write(b)
		case scryfall.ImageHost:
			_, _ = w.Write(picture)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	// Every host is answered by the test server, over plain HTTP.
	base := srv.Client().Transport
	route := roundTrip(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Host = r.URL.Host
		r.URL.Scheme, r.URL.Host = "http", srv.Listener.Addr().String()
		return base.RoundTrip(r)
	})
	client := fetch.New(fetch.Options{
		UserAgent: "mtga-draft-assistant/test", Transport: route,
		Limits: map[string]time.Duration{scryfall.Host: time.Millisecond, scryfall.ImageHost: time.Millisecond},
	})
	dir := t.TempDir()
	cardTask := background.NewCards(background.CardSetup{Dir: dir, Source: scryfall.Source{Client: client}})
	imageTask := background.NewImages(background.ImageSetup{Dir: dir, Source: scryfall.Images{Client: client}})
	model, start, err := background.FindLog(background.LogSetup{
		Flag: firstPackLog(t), GOOS: "linux", Home: t.TempDir(),
		Getenv: func(string) string { return "" }, WantSet: cardTask.Want,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := gui.NewTestWindow(t, windowCfg(t.Context(), model, Options{
		Start: background.All(cardTask.Start, imageTask.Start, start), ShowImages: imageTask.Want,
	}))

	// testdata/scryfall knows every card in the first pack but its last,
	// 106439, which has no picture to fetch.
	pack := len(firstPack.Cards)
	deadline := time.Now().Add(5 * time.Second)
	for {
		w.FrameFn()
		ly := w.TestRender(nil)
		n := 0
		for i := range pack {
			if find(ly, fmt.Sprintf("draft-card-%d-image", i)) != nil {
				n++
			}
		}
		if n == pack-1 {
			if got := texts(ly); !slices.Contains(got, "Card 106439") {
				t.Errorf("window shows %q, want the card Scryfall lacks named by id", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 5s, %d of %d pictures on screen; window shows %q", n, pack-1, texts(ly))
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if asked[scryfall.ImageHost] != pack-1 {
		t.Errorf("asked the image host %d times, want once per picture (%d)", asked[scryfall.ImageHost], pack-1)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
