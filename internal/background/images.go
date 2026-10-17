package background

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/images"
)

// ImageSource downloads a card picture: scryfall.Images in the app, a fake
// in tests.
type ImageSource interface {
	Image(ctx context.Context, url string) ([]byte, error)
}

// ImageSetup is everything the image task depends on, gathered by main.
type ImageSetup struct {
	// Dir is the cache folder, or "" when there is none, and DirErr then
	// says why. Pictures are kept in its images folder; without one,
	// cards show by name.
	Dir    string
	DirErr error
	// Cap is how many bytes the pictures may take; zero is
	// images.DefaultCap.
	Cap    int64
	Source ImageSource
	// Workers is how many pictures are fetched at once; zero is 4
	// (docs/ARCHITECTURE.md's network budget).
	Workers int
	// RetryAfter is how long to wait before trying a picture again after
	// its nth failure in a row, and before trying the folder again when it
	// could not be opened; nil is 30 s, doubling, at most 5 min.
	RetryAfter func(n int) time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// The task the image goroutines do, as the window names it if one stops.
const imageTask = "Fetching card images"

// Images is the image task (decision 12). The window tells it, through
// Want, which pictures are on screen; it hands back each one's local path,
// from disk when it is there and downloaded when it is not, at most
// Workers at a time. A picture that fails is tried again on its own
// schedule, however often the window asks. It does nothing until a picture
// is wanted, so it costs nothing while no pack is on screen.
type Images struct {
	setup ImageSetup

	mu     sync.Mutex
	wanted []app.ImageWant
	wake   chan struct{}
}

// NewImages returns the image task, not yet started.
func NewImages(s ImageSetup) *Images {
	if s.Workers <= 0 {
		s.Workers = 4
	}
	if s.RetryAfter == nil {
		s.RetryAfter = retryAfter
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	return &Images{setup: s, wake: make(chan struct{}, 1)}
}

// Want says which pictures are on screen now, replacing what was said
// before. It returns at once and is safe from any goroutine; the window
// calls it after every message it applies, so the same list again does
// nothing.
func (im *Images) Want(ws []app.ImageWant) {
	im.mu.Lock()
	changed := !slices.Equal(im.wanted, ws)
	if changed {
		im.wanted = slices.Clone(ws)
	}
	im.mu.Unlock()
	if !changed {
		return
	}
	select {
	case im.wake <- struct{}{}:
	default: // already woken; the task reads the latest list when it runs
	}
}

func (im *Images) latest() []app.ImageWant {
	im.mu.Lock()
	defer im.mu.Unlock()
	return slices.Clone(im.wanted)
}

// onScreen reports, for the cache's eviction, whether a file holds a
// picture that is wanted now.
func (im *Images) onScreen() func(name string) bool {
	names := map[string]bool{}
	for _, w := range im.latest() {
		names[images.Name(w.URL)] = true
	}
	return func(name string) bool { return names[name] }
}

// stillWanted reports whether w is still on screen, so a picture the user
// has moved past is not downloaded after all.
func (im *Images) stillWanted(w app.ImageWant) bool {
	im.mu.Lock()
	defer im.mu.Unlock()
	return slices.Contains(im.wanted, w)
}

// imageJob is one picture for a worker, and imageResult what came of it.
type imageJob struct {
	want  app.ImageWant
	cache *images.Cache
}

type imageResult struct {
	want    app.ImageWant
	path    string
	evicted []string
	err     error
	// skipped is a picture no longer on screen by the time a worker
	// reached it, which was neither fetched nor failed.
	skipped bool
	// saving is an error keeping the picture rather than fetching it: the
	// folder's fault, not the network's.
	saving bool
}

// Start runs the image task until ctx ends: one goroutine that decides what
// to fetch, and Workers that fetch it. The image folder is opened only once
// a picture is wanted, so a launch with no draft never touches it.
func (im *Images) Start(ctx context.Context, post Post) {
	jobs := make(chan imageJob)
	results := make(chan imageResult, im.setup.Workers)
	for range im.setup.Workers {
		Go(imageTask, post, func() { im.work(ctx, jobs, results) })
	}
	Go(imageTask, post, func() { im.run(ctx, post, jobs, results) })
}

// work fetches pictures until ctx ends.
func (im *Images) work(ctx context.Context, jobs <-chan imageJob, results chan<- imageResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-jobs:
			r := im.fetch(ctx, j)
			select {
			case results <- r:
			case <-ctx.Done():
				return
			}
		}
	}
}

// fetch is one picture: from disk if it is there, downloaded and kept if
// it is not.
func (im *Images) fetch(ctx context.Context, j imageJob) imageResult {
	r := imageResult{want: j.want}
	if path, ok := j.cache.Lookup(j.want.URL); ok {
		r.path = path
		return r
	}
	if !im.stillWanted(j.want) {
		r.skipped = true
		return r
	}
	body, err := im.setup.Source.Image(ctx, j.want.URL)
	if err != nil {
		r.err = err
		return r
	}
	r.path, r.evicted, r.err = j.cache.Put(j.want.URL, body, im.onScreen())
	r.saving = r.err != nil && !errors.Is(r.err, images.ErrUnreadable)
	return r
}

// shownImage is a picture the window was given.
type shownImage struct{ url, path string }

// imageState is what the deciding goroutine keeps, and only it touches.
type imageState struct {
	cache        *images.Cache
	openFailures int
	openAgain    time.Time

	shown    map[int]shownImage // each picture posted, by Arena id
	inFlight map[app.ImageWant]bool
	failures map[string]int // by URL: failures in a row
	again    map[string]time.Time

	timer  *time.Timer
	timerC <-chan time.Time
}

func (im *Images) run(ctx context.Context, post Post, jobs chan<- imageJob, results <-chan imageResult) {
	st := &imageState{
		shown:    map[int]shownImage{},
		inFlight: map[app.ImageWant]bool{},
		failures: map[string]int{},
		again:    map[string]time.Time{},
	}
	defer func() {
		if st.timer != nil {
			st.timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-im.wake:
		case <-st.timerC:
			st.timer, st.timerC = nil, nil
		case r := <-results:
			im.done(ctx, st, r, post)
			if ctx.Err() != nil {
				return
			}
		}
		im.dispatch(ctx, st, post, jobs)
	}
}

// dispatch hands the wanted pictures that are not shown, not on their way,
// and not waiting to be tried again to idle workers, then sets the timer
// for the earliest retry still to come among the rest.
func (im *Images) dispatch(ctx context.Context, st *imageState, post Post, jobs chan<- imageJob) {
	wanted := im.latest()
	now := im.setup.Now()
	var next time.Time // the earliest retry due later; zero for none
	defer func() { im.schedule(st, next, now) }()
	if len(wanted) == 0 {
		return
	}
	if st.cache == nil {
		if now.Before(st.openAgain) {
			next = st.openAgain
			return
		}
		if !im.open(st, now, post) {
			next = st.openAgain
			return
		}
	}
	im.recheck(st, wanted, post)
	for _, w := range wanted {
		if st.shown[w.ArenaID].url == w.URL || st.inFlight[w] {
			continue
		}
		if at := st.again[w.URL]; now.Before(at) {
			if next.IsZero() || at.Before(next) {
				next = at
			}
			continue
		}
		// Every busy worker is counted in inFlight, so below the limit one
		// is free, and the rest are sent as results come back.
		if len(st.inFlight) >= im.setup.Workers {
			continue
		}
		select {
		case jobs <- imageJob{want: w, cache: st.cache}:
			st.inFlight[w] = true
		case <-ctx.Done():
			return
		}
	}
}

// open opens the image folder, or reports why it cannot be used and plans
// to try again: a folder made writable fixes itself without a restart.
func (im *Images) open(st *imageState, now time.Time, post Post) bool {
	if im.setup.Dir == "" {
		st.openFailures++
		st.openAgain = now.Add(im.setup.RetryAfter(st.openFailures))
		if st.openFailures == 1 {
			post(app.ImageCacheUnavailable{Err: im.setup.DirErr})
		}
		return false
	}
	dir := filepath.Join(im.setup.Dir, images.DirName)
	c, err := images.Open(dir, im.setup.Cap, im.setup.Now)
	if err != nil {
		st.openFailures++
		st.openAgain = now.Add(im.setup.RetryAfter(st.openFailures))
		// Said once: the window keeps saying it until a picture is saved.
		if st.openFailures == 1 {
			post(app.ImageCacheUnavailable{Dir: dir, Err: err})
		}
		return false
	}
	st.cache, st.openFailures = c, 0
	return true
}

// recheck forgets, and tells the window to forget, any picture on screen
// whose file has gone: the cache folder was deleted while the assistant
// ran, which is always allowed (decision 16). It is then fetched again,
// rather than the window pointing at a file that is not there.
func (im *Images) recheck(st *imageState, wanted []app.ImageWant, post Post) {
	var gone []int
	for _, w := range wanted {
		s, ok := st.shown[w.ArenaID]
		if !ok || s.url != w.URL {
			continue
		}
		if _, err := os.Stat(s.path); err != nil {
			delete(st.shown, w.ArenaID)
			gone = append(gone, w.ArenaID)
		}
	}
	if len(gone) > 0 {
		post(app.ImagesEvicted{ArenaIDs: gone})
	}
}

// done takes in what a worker found.
func (im *Images) done(ctx context.Context, st *imageState, r imageResult, post Post) {
	delete(st.inFlight, r.want)
	switch {
	case r.skipped:
	case r.err != nil:
		if ctx.Err() != nil {
			return
		}
		st.failures[r.want.URL]++
		wait := im.setup.RetryAfter(st.failures[r.want.URL])
		st.again[r.want.URL] = im.setup.Now().Add(wait)
		if r.saving {
			post(app.ImageCacheUnavailable{Dir: filepath.Join(im.setup.Dir, images.DirName), Err: r.err})
		}
		post(app.ImageFailed{ArenaID: r.want.ArenaID, Err: r.err, RetryIn: wait})
	default:
		delete(st.failures, r.want.URL)
		delete(st.again, r.want.URL)
		st.shown[r.want.ArenaID] = shownImage{url: r.want.URL, path: r.path}
		post(app.ImageReady{ArenaID: r.want.ArenaID, Path: r.path})
		if ids := st.forget(r.evicted); len(ids) > 0 {
			post(app.ImagesEvicted{ArenaIDs: ids})
		}
	}
}

// forget drops the pictures in the evicted files from those posted, and
// returns their cards, for the window to forget too.
func (st *imageState) forget(evicted []string) []int {
	if len(evicted) == 0 {
		return nil
	}
	var ids []int
	for id, s := range st.shown {
		if slices.Contains(evicted, images.Name(s.url)) {
			delete(st.shown, id)
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// schedule sets the timer to wake the task at next, or stops it when next
// is zero: nothing wanted is waiting to be tried again.
func (im *Images) schedule(st *imageState, next, now time.Time) {
	if st.timer != nil {
		st.timer.Stop()
		st.timer, st.timerC = nil, nil
	}
	if next.IsZero() {
		return
	}
	st.timer = time.NewTimer(next.Sub(now))
	st.timerC = st.timer.C
}
