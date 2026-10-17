package background

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
	"github.com/nmcginn/mtga-draft-assistant/internal/images"
)

// jpegBytes is a small, real card picture.
func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 49, 68)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func want(id int) app.ImageWant {
	return app.ImageWant{ArenaID: id, URL: fmt.Sprintf("https://cards.scryfall.io/normal/front/0/0/%d.jpg?1", id)}
}

// fakeImages serves pictures, failing each URL the first fail[url] times,
// and holding every request until gate is closed when gate is set.
type fakeImages struct {
	body []byte
	gate chan struct{}

	mu      sync.Mutex
	fail    map[string]int
	bad     map[string]bool
	calls   map[string]int
	active  int
	maxSeen int
}

func newFakeImages(t *testing.T) *fakeImages {
	return &fakeImages{body: jpegBytes(t), fail: map[string]int{}, bad: map[string]bool{}, calls: map[string]int{}}
}

func (f *fakeImages) Image(ctx context.Context, u string) ([]byte, error) {
	f.mu.Lock()
	f.calls[u]++
	f.active++
	f.maxSeen = max(f.maxSeen, f.active)
	failing := f.fail[u] > 0
	if failing {
		f.fail[u]--
	}
	bad := f.bad[u]
	gate := f.gate
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	switch {
	case failing:
		return nil, &fetch.HostError{Host: "cards.scryfall.io", Err: errors.New("no route to host")}
	case bad:
		return []byte("<html>Service Unavailable</html>"), nil
	}
	return f.body, nil
}

func (f *fakeImages) asked(u string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[u]
}

// until waits, up to 5s, for cond to hold of f.
func (f *fakeImages) until(t *testing.T, what string, cond func(f *fakeImages) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		ok := cond(f)
		f.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 5s, still not %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *fakeImages) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func imageSetup(dir string, src ImageSource) ImageSetup {
	return ImageSetup{Dir: dir, Source: src, RetryAfter: func(int) time.Duration { return 10 * time.Millisecond }}
}

// startImages starts the image task and returns it with a way to read what
// it posts, and to wait for a quiet spell.
func startImages(t *testing.T, s ImageSetup) (*Images, func() app.Msg, func() (app.Msg, bool)) {
	t.Helper()
	ch := make(chan app.Msg, 64)
	im := NewImages(s)
	im.Start(t.Context(), func(m app.Msg) { ch <- m })
	next := func() app.Msg {
		t.Helper()
		select {
		case m := <-ch:
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("no message within 5s")
			return nil
		}
	}
	maybe := func() (app.Msg, bool) {
		select {
		case m := <-ch:
			return m, true
		case <-time.After(100 * time.Millisecond):
			return nil, false
		}
	}
	return im, next, maybe
}

// readyFor reads messages until every one of ids has an ImageReady, and
// returns their paths; anything else posted on the way is failed.
func readyFor(t *testing.T, next func() app.Msg, ids ...int) map[int]string {
	t.Helper()
	paths := map[int]string{}
	for len(paths) < len(ids) {
		r := expect[app.ImageReady](t, next)
		if !slices.Contains(ids, r.ArenaID) {
			t.Fatalf("ready %d, want one of %v", r.ArenaID, ids)
		}
		paths[r.ArenaID] = r.Path
	}
	return paths
}

func TestAPictureNotOnDiskIsDownloadedKeptAndHandedOverAsALocalPath(t *testing.T) {
	dir := t.TempDir()
	src := newFakeImages(t)
	im, next, maybe := startImages(t, imageSetup(dir, src))
	im.Want([]app.ImageWant{want(1)})
	path := readyFor(t, next, 1)[1]
	if filepath.Dir(path) != filepath.Join(dir, images.DirName) {
		t.Errorf("handed over %q, want a file in the cache's image folder", path)
	}
	if b, err := os.ReadFile(path); err != nil || !bytes.Equal(b, src.body) {
		t.Errorf("the file holds %d bytes (%v)", len(b), err)
	}
	quiet(t, maybe)
	if n := src.asked(want(1).URL); n != 1 {
		t.Errorf("downloaded %d times", n)
	}
}

func TestAPictureAlreadyOnDiskCostsNoRequest(t *testing.T) {
	dir := t.TempDir()
	c, err := images.Open(filepath.Join(dir, images.DirName), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored, _, err := c.Put(want(1).URL, jpegBytes(t), nil)
	if err != nil {
		t.Fatal(err)
	}

	src := newFakeImages(t)
	im, next, _ := startImages(t, imageSetup(dir, src))
	im.Want([]app.ImageWant{want(1)})
	if got := readyFor(t, next, 1)[1]; got != stored {
		t.Errorf("handed over %q, want the file already stored, %q", got, stored)
	}
	if n := src.total(); n != 0 {
		t.Errorf("made %d requests for a picture already on disk", n)
	}
}

func TestTheSameListAgainDoesNothing(t *testing.T) {
	src := newFakeImages(t)
	im, next, maybe := startImages(t, imageSetup(t.TempDir(), src))
	for range 3 {
		im.Want([]app.ImageWant{want(1)})
	}
	readyFor(t, next, 1)
	for range 50 { // the window calls Want after every message it applies
		im.Want([]app.ImageWant{want(1)})
	}
	quiet(t, maybe)
	if n := src.total(); n != 1 {
		t.Errorf("made %d requests for one picture", n)
	}
}

func TestNoMoreThanFourPicturesAreFetchedAtOnce(t *testing.T) {
	src := newFakeImages(t)
	src.gate = make(chan struct{})
	im, next, _ := startImages(t, imageSetup(t.TempDir(), src))
	var ws []app.ImageWant
	var ids []int
	for id := 1; id <= 14; id++ { // a whole pack
		ws = append(ws, want(id))
		ids = append(ids, id)
	}
	im.Want(ws)
	src.until(t, "fetching four", func(f *fakeImages) bool { return f.active == 4 })
	time.Sleep(50 * time.Millisecond) // room for a fifth, if it would start one
	close(src.gate)
	readyFor(t, next, ids...)
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.maxSeen != 4 {
		t.Errorf("at most %d pictures were fetched at once, want 4", src.maxSeen)
	}
}

func TestAFailedDownloadIsRetriedLaterNotEveryTimeTheWindowAsks(t *testing.T) {
	src := newFakeImages(t)
	src.fail[want(1).URL] = 1
	s := imageSetup(t.TempDir(), src)
	s.RetryAfter = func(int) time.Duration { return 300 * time.Millisecond }
	im, next, _ := startImages(t, s)
	im.Want([]app.ImageWant{want(1)})
	failed := expect[app.ImageFailed](t, next)
	if failed.ArenaID != 1 || failed.RetryIn != 300*time.Millisecond || failed.Err == nil || failed.Err.Error() != "can't reach cards.scryfall.io" {
		t.Errorf("failed = %+v", failed)
	}

	// Every frame, and every pack that shows the card again, asks again;
	// none of that is a reason to try before the retry is due.
	for i := range 20 {
		if i%2 == 0 {
			im.Want([]app.ImageWant{want(1), want(2)})
		} else {
			im.Want([]app.ImageWant{want(1)})
		}
	}
	im.Want([]app.ImageWant{want(1), want(2)})
	if n := src.asked(want(1).URL); n != 1 {
		t.Errorf("tried %d times before the retry was due", n)
	}
	readyFor(t, next, 2, 1) // 2 at once; 1 once its retry falls due
	if n := src.asked(want(1).URL); n != 2 {
		t.Errorf("tried %d times in all, want once and once more when due", n)
	}
}

func TestAFailedPictureWaitsLongerEachTimeItFailsAgain(t *testing.T) {
	src := newFakeImages(t)
	src.fail[want(1).URL] = 2
	s := imageSetup(t.TempDir(), src)
	var mu sync.Mutex
	var asked []int
	s.RetryAfter = func(n int) time.Duration {
		mu.Lock()
		asked = append(asked, n)
		mu.Unlock()
		return 10 * time.Millisecond
	}
	im, next, _ := startImages(t, s)
	im.Want([]app.ImageWant{want(1)})
	expect[app.ImageFailed](t, next)
	expect[app.ImageFailed](t, next)
	readyFor(t, next, 1)
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(asked, []int{1, 2}) {
		t.Errorf("waits were chosen for failures %v, want 1 then 2 in a row", asked)
	}
}

func TestADownloadThatIsNotAPictureIsNotKeptAndIsTriedAgain(t *testing.T) {
	dir := t.TempDir()
	src := newFakeImages(t)
	src.bad[want(1).URL] = true
	im, next, _ := startImages(t, imageSetup(dir, src))
	im.Want([]app.ImageWant{want(1)})
	failed := expect[app.ImageFailed](t, next)
	if !errors.Is(failed.Err, images.ErrUnreadable) {
		t.Errorf("failed with %v, want ErrUnreadable", failed.Err)
	}
	src.mu.Lock()
	src.bad[want(1).URL] = false
	src.mu.Unlock()
	readyFor(t, next, 1)
}

func TestACorruptFileOnDiskIsDownloadedAgain(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, images.DirName)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(folder, images.Name(want(1).URL))
	if err := os.WriteFile(corrupt, []byte("half a jpe"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := newFakeImages(t)
	im, next, _ := startImages(t, imageSetup(dir, src))
	im.Want([]app.ImageWant{want(1)})
	path := readyFor(t, next, 1)[1]
	if b, _ := os.ReadFile(path); !bytes.Equal(b, src.body) || src.asked(want(1).URL) != 1 {
		t.Errorf("the corrupt file was not replaced by a download (%d requests)", src.asked(want(1).URL))
	}
}

func TestAPictureTheUserMovedPastIsNotFetched(t *testing.T) {
	src := newFakeImages(t)
	src.gate = make(chan struct{})
	s := imageSetup(t.TempDir(), src)
	s.Workers = 1
	im, next, maybe := startImages(t, s)
	im.Want([]app.ImageWant{want(1), want(2)})
	// 1 is being fetched; 2 waits its turn.
	src.until(t, "fetching 1", func(f *fakeImages) bool { return f.calls[want(1).URL] == 1 })
	im.Want([]app.ImageWant{want(3)}) // the pick was made: a new pack
	close(src.gate)
	got := map[int]bool{}
	for len(got) < 2 {
		got[expect[app.ImageReady](t, next).ArenaID] = true
	}
	if !got[1] || !got[3] {
		t.Errorf("ready %v, want 1 (already on its way) and 3", got)
	}
	quiet(t, maybe)
	if n := src.asked(want(2).URL); n != 0 {
		t.Errorf("fetched a picture from a pack no longer on screen %d times", n)
	}
}

func TestEvictedPicturesAreReportedSoTheWindowForgetsThem(t *testing.T) {
	src := newFakeImages(t)
	s := imageSetup(t.TempDir(), src)
	s.Cap = int64(len(src.body)) // room for one
	im, next, _ := startImages(t, s)
	im.Want([]app.ImageWant{want(1)})
	first := readyFor(t, next, 1)[1]
	im.Want([]app.ImageWant{want(2)})
	readyFor(t, next, 2)
	if ev := expect[app.ImagesEvicted](t, next); !slices.Equal(ev.ArenaIDs, []int{1}) {
		t.Errorf("evicted %v, want 1, no longer on screen", ev.ArenaIDs)
	}
	if _, err := os.Stat(first); err == nil {
		t.Error("the evicted picture is still on disk")
	}

	// Shown again, it is fetched again.
	im.Want([]app.ImageWant{want(1)})
	readyFor(t, next, 1)
	if n := src.asked(want(1).URL); n != 2 {
		t.Errorf("an evicted picture shown again was fetched %d times in all, want 2", n)
	}
}

func TestPicturesOnScreenAreNotEvictedToKeepUnderTheCap(t *testing.T) {
	src := newFakeImages(t)
	s := imageSetup(t.TempDir(), src)
	s.Cap = int64(len(src.body)) // room for one
	im, next, maybe := startImages(t, s)
	im.Want([]app.ImageWant{want(1), want(2)})
	paths := readyFor(t, next, 1, 2)
	if m, ok := maybe(); ok {
		t.Errorf("posted %T %+v; nothing on screen may be evicted", m, m)
	}
	for id, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("picture %d, on screen, is gone from disk", id)
		}
	}
}

func TestADeletedCacheFolderIsRecoveredFromWhileRunning(t *testing.T) {
	dir := t.TempDir()
	src := newFakeImages(t)
	im, next, _ := startImages(t, imageSetup(dir, src))
	im.Want([]app.ImageWant{want(1)})
	readyFor(t, next, 1)
	if err := os.RemoveAll(filepath.Join(dir, images.DirName)); err != nil {
		t.Fatal(err)
	}
	im.Want([]app.ImageWant{want(1), want(2)})
	if ev := expect[app.ImagesEvicted](t, next); !slices.Equal(ev.ArenaIDs, []int{1}) {
		t.Errorf("forgot %v, want 1, whose file was deleted", ev.ArenaIDs)
	}
	for id, p := range readyFor(t, next, 1, 2) {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("picture %d was handed over at %q, which is not there", id, p)
		}
	}
}

func TestAnImageFolderThatCannotBeUsedIsReportedOnceAndTriedAgain(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, images.DirName)
	if err := os.WriteFile(blocker, nil, 0o644); err != nil { // a file where the folder goes
		t.Fatal(err)
	}
	src := newFakeImages(t)
	im, next, maybe := startImages(t, imageSetup(dir, src))
	im.Want([]app.ImageWant{want(1)})
	u := expect[app.ImageCacheUnavailable](t, next)
	if u.Dir != blocker || u.Err == nil {
		t.Errorf("unavailable = %+v", u)
	}
	quiet(t, maybe) // retried every 10 ms, but said once
	if n := src.total(); n != 0 {
		t.Errorf("downloaded %d pictures with nowhere to keep them", n)
	}

	// Fixed while it runs: no restart needed.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	readyFor(t, next, 1)
}

func TestWithNoCacheFolderAtAllTheWindowIsToldWhyPicturesCannotBeKept(t *testing.T) {
	why := errors.New("$XDG_CACHE_HOME is relative")
	s := imageSetup("", newFakeImages(t))
	s.DirErr = why
	im, next, maybe := startImages(t, s)
	im.Want([]app.ImageWant{want(1)})
	if u := expect[app.ImageCacheUnavailable](t, next); !errors.Is(u.Err, why) {
		t.Errorf("unavailable = %+v", u)
	}
	quiet(t, maybe)
}

func TestNothingIsTouchedUntilAPictureIsWanted(t *testing.T) {
	dir := t.TempDir()
	src := newFakeImages(t)
	im, _, maybe := startImages(t, imageSetup(dir, src))
	im.Want(nil)
	quiet(t, maybe)
	if _, err := os.Stat(filepath.Join(dir, images.DirName)); err == nil {
		t.Error("the image folder was created with no picture wanted")
	}
}

func TestTheImageTaskStopsWithItsContext(t *testing.T) {
	src := newFakeImages(t)
	src.gate = make(chan struct{}) // never opened: a download hangs
	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan app.Msg, 64)
	im := NewImages(imageSetup(t.TempDir(), src))
	im.Start(ctx, func(m app.Msg) { ch <- m })
	im.Want([]app.ImageWant{want(1)})
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case m := <-ch:
		t.Errorf("posted %T %+v after being stopped", m, m)
	case <-time.After(100 * time.Millisecond):
	}
}
