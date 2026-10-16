package background

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/cards"
	"github.com/nmcginn/mtga-draft-assistant/internal/store"
)

// SetSource fetches a set's cards: scryfall.Source in the app, a fake in
// tests.
type SetSource interface {
	Set(ctx context.Context, set string) ([]cards.Card, error)
}

// CardSetup is everything the card task depends on, gathered by main.
type CardSetup struct {
	// Dir is the cache folder, or "" when there is none, and DirErr then
	// says why (os.UserCacheDir failed). Without one, cards are fetched
	// each launch and kept in memory.
	Dir    string
	DirErr error
	Source SetSource

	// MaxAge is how old a stored set may be before a draft of it fetches
	// it again (zero is a week), and EmptyMaxAge the same for a set
	// Scryfall had no cards for (zero is a day), which is most likely one
	// too new for Scryfall to have caught up with.
	MaxAge, EmptyMaxAge time.Duration
	// RetryAfter is how long to wait before fetching a set again after the
	// nth failure in a row; nil is 30 s, doubling, at most 5 min.
	RetryAfter func(n int) time.Duration
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// The task the card goroutine does, as the window names it if it stops.
const cardTask = "Fetching card details"

// Cards is the card task: one goroutine that, for the set a draft needs,
// reads its cards from the cache, or fetches them from Scryfall and stores
// them, and posts them to the window (decision 26). It does nothing until a
// set is wanted, so it costs nothing while no draft is under way.
type Cards struct {
	setup CardSetup

	mu     sync.Mutex
	wanted string
	wake   chan struct{}
}

// NewCards returns the card task, not yet started.
func NewCards(s CardSetup) *Cards {
	if s.MaxAge <= 0 {
		s.MaxAge = 7 * 24 * time.Hour
	}
	if s.EmptyMaxAge <= 0 {
		s.EmptyMaxAge = 24 * time.Hour
	}
	if s.RetryAfter == nil {
		s.RetryAfter = retryAfter
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	return &Cards{setup: s, wake: make(chan struct{}, 1)}
}

// retryAfter is the default wait after the nth failure in a row: 30 s, 1 min,
// 2 min, 4 min, then every 5 min. A user offline mid-draft gets their cards
// within a few minutes of coming back; a Scryfall that is down is asked a
// dozen times an hour, not every few seconds.
func retryAfter(n int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < n && d < 5*time.Minute; i++ {
		d *= 2
	}
	return min(d, 5*time.Minute)
}

// Want asks for set's cards. It returns at once and is safe from any
// goroutine; the log task calls it for each draft it reads the start of.
// Only the latest set asked for is fetched: catching up on a session that
// held several drafts fetches the one on screen, not every one in it.
func (c *Cards) Want(set string) {
	if set == "" {
		return
	}
	c.mu.Lock()
	changed := c.wanted != set
	c.wanted = set
	c.mu.Unlock()
	// The same set asked for again (the log read from the start after
	// Arena relaunched) is not a reason to retry sooner than planned.
	if !changed {
		return
	}
	select {
	case c.wake <- struct{}{}:
	default: // already woken; the task reads the latest set when it runs
	}
}

func (c *Cards) latest() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wanted
}

// Start runs the card task until ctx ends. It opens the cache on its own
// goroutine, and only once a draft needs a set: the first frame never waits
// on the disk, and a launch with no draft never touches it.
func (c *Cards) Start(ctx context.Context, post Post) {
	Go(cardTask, post, func() { c.run(ctx, post) })
}

func (c *Cards) run(ctx context.Context, post Post) {
	var st *store.Store
	opened := false
	defer func() {
		if st != nil {
			_ = st.Close()
		}
	}()

	loaded := map[string]bool{}
	var retry *time.Timer
	var retryC <-chan time.Time
	failedSet, failures := "", 0
	stopRetry := func() {
		if retry != nil {
			retry.Stop()
		}
		retry, retryC = nil, nil
	}
	defer stopRetry()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-retryC:
			retryC = nil
		}
		set := c.latest()
		if set == "" || loaded[set] {
			continue
		}
		if !opened {
			st, opened = c.open(ctx, post), true
		}
		err := c.load(ctx, st, set, post)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			loaded[set] = true
			failedSet, failures = "", 0
			stopRetry()
			continue
		}
		if set != failedSet {
			failedSet, failures = set, 0
		}
		failures++
		wait := c.setup.RetryAfter(failures)
		post(app.CardsFailed{Set: set, Err: err, RetryIn: wait})
		stopRetry()
		retry = time.NewTimer(wait)
		retryC = retry.C
	}
}

// open opens the cache, or reports why it cannot be used and returns nil;
// the task then works from memory.
func (c *Cards) open(ctx context.Context, post Post) *store.Store {
	if c.setup.Dir == "" {
		post(app.CacheUnavailable{Err: c.setup.DirErr})
		return nil
	}
	st, err := store.Open(ctx, c.setup.Dir)
	if err != nil {
		if ctx.Err() == nil {
			post(app.CacheUnavailable{Dir: c.setup.Dir, Err: err})
		}
		return nil
	}
	return st
}

// load posts set's cards: from the cache when it has them, fetched from the
// source and stored when it does not. A stored set that is out of date is
// shown at once, then fetched again quietly; if that fails, the stored cards
// stand. An error means the set is not on screen, and is worth retrying.
func (c *Cards) load(ctx context.Context, st *store.Store, set string, post Post) error {
	if st != nil {
		stored, ok, err := st.Set(ctx, set)
		switch {
		case err != nil:
			// Fetching it again repairs whatever was wrong with it.
			log.Printf("%s: %v", cardTask, err)
		case ok:
			post(app.CardsLoaded{Set: set, Cards: stored.Cards})
			if !c.stale(stored) {
				return nil
			}
			if fresh, err := c.setup.Source.Set(ctx, set); err == nil {
				c.save(ctx, st, set, fresh, post)
				post(app.CardsLoaded{Set: set, Cards: fresh})
			} else if ctx.Err() == nil {
				log.Printf("%s: refreshing %s: %v", cardTask, set, err)
			}
			return nil
		}
	}

	post(app.CardsFetching{Set: set})
	fetched, err := c.setup.Source.Set(ctx, set)
	if err != nil {
		return err
	}
	c.save(ctx, st, set, fetched, post)
	post(app.CardsLoaded{Set: set, Cards: fetched})
	return nil
}

// save stores set, if there is a store. A failure is the window's to know
// about, since it means fetching again next launch, but it does not stop
// the cards being shown now.
func (c *Cards) save(ctx context.Context, st *store.Store, set string, cs []cards.Card, post Post) {
	if st == nil {
		return
	}
	if err := st.SaveSet(ctx, set, c.setup.Now(), cs); err != nil && ctx.Err() == nil {
		post(app.CacheUnavailable{Dir: c.setup.Dir, Err: err})
	}
}

// stale reports whether a stored set is old enough to fetch again. One
// stored in the future (the clock was wrong then, or is now) is fetched
// again too, rather than trusted until that date.
func (c *Cards) stale(s store.Set) bool {
	limit := c.setup.MaxAge
	if len(s.Cards) == 0 {
		limit = c.setup.EmptyMaxAge
	}
	age := c.setup.Now().Sub(s.FetchedAt)
	return age > limit || age < -time.Hour
}
