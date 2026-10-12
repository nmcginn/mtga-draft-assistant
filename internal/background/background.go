// Package background runs the assistant's I/O off the UI thread. Every
// goroutine the app starts is started here, by Go, so each one recovers its
// own panic and reports it to the window as a message (decision 4). The
// tasks themselves are thin: they call the I/O packages and translate what
// those report into app messages.
package background

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
)

// Post hands a message to the UI thread. ui.Post, bound to the window, in the
// app; a channel or a slice in tests.
type Post func(app.Msg)

// Start launches the app's background tasks once the window exists. It must
// return at once: it runs on the UI thread, before the first frame.
type Start func(ctx context.Context, post Post)

// Go runs fn on its own goroutine. If fn panics, the panic ends fn but not the
// app: its stack goes to stderr for a bug report, and the window is told, as
// app.TaskFailed naming task, so the user knows that part has stopped and
// what to do. task is what fn does, as a user would put it.
func Go(task string, post Post, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("%s: panic: %v\n%s", task, r, debug.Stack())
				post(app.TaskFailed{Task: task, Err: fmt.Errorf("internal error: %v", r)})
			}
		}()
		fn()
	}()
}

// LogSetup is everything deciding how the log is found depends on, gathered
// by main so that the decision itself can be tested.
type LogSetup struct {
	// Flag is the -log value, or "" to search.
	Flag string
	// GOOS, Home, and Getenv describe the machine for arena.Candidates. Home
	// is "" when it could not be determined.
	GOOS   string
	Home   string
	Getenv func(string) string
	// Every is how often to look again while nothing is found; zero means
	// arena.PollEvery.
	Every time.Duration
}

// FindLog decides, before the window opens, how the log will be found. With
// -log, the path is checked now, and a bad one is an error for main to report
// and exit on, never a quiet fall back to searching; the returned model is
// already following it and there is nothing to start. Without it, the model
// is the waiting state and Start runs the search.
func FindLog(s LogSetup) (app.Model, Start, error) {
	if s.Flag != "" {
		path, err := arena.ResolveFlag(s.Flag)
		if err != nil {
			return app.Model{}, nil, err
		}
		return app.New().Apply(app.LogFound{Path: path}), nil, nil
	}
	cands := arena.Candidates(s.GOOS, s.Home, s.Getenv)
	every := s.Every
	if every <= 0 {
		every = arena.PollEvery
	}
	start := func(ctx context.Context, post Post) {
		Go("Looking for Arena's log", post, func() {
			arena.Watch(ctx, cands, every, func(r arena.Search) {
				if r.Found != "" {
					post(app.LogFound{Path: r.Found})
					return
				}
				looked := make([]string, len(r.Looked))
				for i, c := range r.Looked {
					looked[i] = c.Where
				}
				post(app.LogSearched{Looked: looked})
			})
		})
	}
	return app.New(), start, nil
}
