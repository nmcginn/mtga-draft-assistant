package app

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
)

const (
	oathkeeperURL = "https://cards.scryfall.io/normal/front/0/0/106325.jpg?1"
	scoutURL      = "https://cards.scryfall.io/normal/front/0/0/106235.jpg?1"
)

// pictured is fra with a picture for each card Scryfall knows; 106453 is
// still unknown to it, so has none.
var pictured = CardsLoaded{Set: "FRA", Cards: []cards.Card{
	{ArenaID: 106325, Set: "FRA", Name: "Verdant Oathkeeper", ManaCost: "{2}{G}{G}", TypeLine: "Creature — Elf Druid", Rarity: "uncommon", ImageURL: oathkeeperURL},
	{ArenaID: 106235, Set: "FRA", Name: "Thornwood Scout", ManaCost: "{G}", TypeLine: "Creature — Elf Scout", Rarity: "common", ImageURL: scoutURL},
}}

func TestThePacksPicturesAreTheOnesShownInPackOrder(t *testing.T) {
	got := inDraft(p1p1).Apply(pictured).ImagesShown()
	want := []ImageWant{{ArenaID: 106325, URL: oathkeeperURL}, {ArenaID: 106235, URL: scoutURL}}
	if !slices.Equal(got, want) {
		t.Errorf("ImagesShown = %+v, want %+v: the pack's cards that have a picture", got, want)
	}
}

func TestNoPictureIsWantedForWhatIsNotOnScreen(t *testing.T) {
	picked := inDraft(p1p1, arena.PickMade{Pack: 1, Pick: 1, Card: 106325}).Apply(pictured)
	done := picked.Apply(DraftEvents{Events: []arena.Event{arena.DraftEnded{Pool: []int{106325}}}})
	failed := inDraft(p1p1).Apply(pictured).Apply(TaskFailed{Task: "Watching Arena's log"})
	cases := map[string]Model{
		"no draft":                  following().Apply(pictured),
		"before the first pack":     inDraft().Apply(pictured),
		"names not yet known":       inDraft(p1p1),
		"waiting for the next pack": picked,
		"the pool, once done":       done,
		"a failure on screen":       failed,
	}
	for name, m := range cases {
		if got := m.ImagesShown(); len(got) != 0 {
			t.Errorf("%s: ImagesShown = %+v, want none", name, got)
		}
	}
}

func TestACardShownTwiceInAPackIsFetchedOnce(t *testing.T) {
	m := inDraft(arena.PackShown{Pack: 1, Pick: 1, Cards: []int{106235, 106325, 106235}}).Apply(pictured)
	if got := m.ImagesShown(); len(got) != 2 {
		t.Errorf("ImagesShown = %+v, want each picture once", got)
	}
}

func TestAPictureOnDiskIsShownInPlaceOfTheCardsName(t *testing.T) {
	m := inDraft(p1p1).Apply(pictured).Apply(ImageReady{ArenaID: 106325, Path: "/cache/images/a.jpg"})
	s := m.DraftScreen()
	if s.Cards[0].Image != "/cache/images/a.jpg" || s.Cards[0].Name != "Verdant Oathkeeper" {
		t.Errorf("card 1 = %+v, want its picture, and its name kept for a screen reader", s.Cards[0])
	}
	if s.Cards[1].Image != "" {
		t.Errorf("card 2 = %+v, whose picture is still coming", s.Cards[1])
	}
	if s.Notice != "" {
		t.Errorf("notice %q, though a picture on its way needs no explaining", s.Notice)
	}
	if s.Footnote != "Card details and images from Scryfall." {
		t.Errorf("footnote %q, want Scryfall credited for the pictures too", s.Footnote)
	}
}

func TestAFailedDownloadSaysWhatFailedAndWhenItIsTriedAgain(t *testing.T) {
	err := &fetch.HostError{Host: "cards.scryfall.io", Err: errors.New("dial tcp: no route to host")}
	m := inDraft(p1p1).Apply(pictured).Apply(ImageFailed{ArenaID: 106235, Err: err, RetryIn: 30 * time.Second})
	s := m.DraftScreen()
	for _, want := range []string{"can't reach cards.scryfall.io", "internet connection", "show by name", "in 30 seconds"} {
		if !strings.Contains(s.Notice, want) {
			t.Errorf("notice %q does not say %q", s.Notice, want)
		}
	}
	if s.Cards[1].Image != "" || s.Cards[1].Name != "Thornwood Scout" {
		t.Errorf("card %+v, want it shown by name", s.Cards[1])
	}

	// Once it arrives, nothing is left to explain.
	m = m.Apply(ImageReady{ArenaID: 106235, Path: "/cache/images/b.jpg"})
	if n := m.DraftScreen().Notice; n != "" {
		t.Errorf("after the picture arrived, notice %q", n)
	}
}

func TestAFailureForACardNoLongerShownIsNotMentioned(t *testing.T) {
	m := inDraft(p1p1).Apply(pictured).Apply(ImageFailed{ArenaID: 106235, Err: errors.New("x"), RetryIn: time.Minute})
	m = m.Apply(DraftEvents{Events: []arena.Event{
		arena.PickMade{Pack: 1, Pick: 1, Card: 106235},
		arena.PackShown{Pack: 1, Pick: 2, Cards: []int{106325}},
	}})
	if n := m.DraftScreen().Notice; n != "" {
		t.Errorf("notice %q about a picture not on screen", n)
	}
}

func TestAnEvictedPictureIsForgottenSoItIsAskedForAgain(t *testing.T) {
	m := inDraft(p1p1).Apply(pictured).
		Apply(ImageReady{ArenaID: 106325, Path: "/cache/images/a.jpg"}).
		Apply(ImageReady{ArenaID: 106235, Path: "/cache/images/b.jpg"})
	held := m
	m = m.Apply(ImagesEvicted{ArenaIDs: []int{106325}})
	if _, ok := m.Images[106325]; ok {
		t.Error("an evicted picture's path was kept; the window would point at a deleted file")
	}
	if m.Images[106235].Path == "" {
		t.Error("a picture that was not evicted was forgotten")
	}
	if held.Images[106325].Path == "" {
		t.Error("Apply changed the map of a model held elsewhere")
	}
	if m.Apply(ImagesEvicted{}).Images[106235].Path == "" {
		t.Error("an empty eviction changed the pictures")
	}
}

func TestPicturesOutliveTheLogBeingLost(t *testing.T) {
	m := inDraft(p1p1).Apply(ImageReady{ArenaID: 106325, Path: "/cache/images/a.jpg"})
	if m = m.Apply(LogLost{}).Apply(LogFound{Path: "/logs/Player.log"}); m.Images[106325].Path == "" {
		t.Error("the pictures on disk were forgotten with the log")
	}
	if m.Apply(ImageReady{ArenaID: 1}).Images[1].Path != "" {
		t.Error("a picture with no path was taken")
	}
}

func TestAnImageFolderThatCannotBeWrittenIsReportedWithWhatToDo(t *testing.T) {
	err := &fs.PathError{Op: "open", Path: "/home/me/.cache/mtga-draft-assistant/images/x.tmp", Err: syscall.EACCES}
	m := inDraft(p1p1).Apply(pictured).Apply(ImageCacheUnavailable{Dir: "/home/me/.cache/mtga-draft-assistant/images", Err: err})
	s := m.DraftScreen()
	for _, want := range []string{"Card images can't be saved in /home/me/.cache/mtga-draft-assistant/images (permission denied)", "show by name", "can write to that folder"} {
		if !strings.Contains(s.Footnote, want) {
			t.Errorf("footnote %q does not say %q", s.Footnote, want)
		}
	}
	if strings.Count(s.Footnote, "/home/me") != 1 {
		t.Errorf("footnote %q names the folder more than once", s.Footnote)
	}
	// A picture saved later means the folder works again.
	if n := m.Apply(ImageReady{ArenaID: 106325, Path: "/x.jpg"}).DraftScreen().Footnote; strings.Contains(n, "can't be saved") {
		t.Errorf("after a picture was saved, footnote %q", n)
	}
}

func TestWhenNeitherDetailsNorPicturesCanBeSavedTheFootnoteSaysSoOnce(t *testing.T) {
	m := inDraft(p1p1).Apply(pictured).
		Apply(CacheUnavailable{Dir: "/ro/cache", Err: errors.New("read-only file system")}).
		Apply(ImageCacheUnavailable{Dir: "/ro/cache/images", Err: errors.New("read-only file system")})
	n := m.DraftScreen().Footnote
	if !strings.HasPrefix(n, "Card details and images can't be saved in /ro/cache (read-only file system)") || strings.Count(n, "can't be saved") != 1 {
		t.Errorf("footnote %q", n)
	}
}
