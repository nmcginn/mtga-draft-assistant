// Package background runs the assistant's I/O off the UI thread. Every
// goroutine the app starts is started here, by Go, so each one recovers its
// own panic and reports it to the window as a message (decision 4). The
// tasks themselves are thin: they call the I/O packages and translate what
// those report into app messages.
package background

import (
	"context"
	"errors"
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

// LogSetup is everything deciding how the log is found and followed depends
// on, gathered by main so that the decision itself can be tested.
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
	// Follow sets how often the found log is checked and how long it may go
	// missing; its zero value is the defaults. Its callbacks are set by
	// FindLog, not here.
	Follow arena.Follower
	// Lines receives the log's lines, in order, on the background goroutine,
	// after the draft parser has read them. Nil is the usual: tests set it to
	// see what was read.
	Lines func([]string)
}

// The task the log goroutine does, as the window names it if it stops.
const logTask = "Watching Arena's log"

// FindLog decides, before the window opens, how the log will be found. With
// -log, the path is checked now, and a bad one is an error for main to report
// and exit on, never a quiet fall back to searching; the returned model is
// already following it. Without it, the model is the waiting state.
//
// Either way, Start runs one goroutine that finds the log (unless -log
// already did), follows it, and, when it goes away, says so and looks for
// it again: in Arena's usual places, or with -log, at that path alone.
func FindLog(s LogSetup) (app.Model, Start, error) {
	model := app.New()
	var path string
	var cands []arena.Candidate
	if s.Flag != "" {
		p, err := arena.ResolveFlag(s.Flag)
		if err != nil {
			return app.Model{}, nil, err
		}
		path = p
		cands = []arena.Candidate{arena.Exactly(s.GOOS, s.Home, p)}
		model = model.Apply(app.LogFound{Path: p})
	} else {
		cands = arena.Candidates(s.GOOS, s.Home, s.Getenv)
	}
	every := s.Every
	if every <= 0 {
		every = arena.PollEvery
	}
	start := func(ctx context.Context, post Post) {
		Go(logTask, post, func() {
			f, reset := follower(s, post)
			watchLog(ctx, path, path, cands, every, f, reset, post)
		})
	}
	return model, start, nil
}

// follower is s.Follow with its callbacks turned into app messages: the
// lines it reads go through a draft parser, and each batch's draft events
// become one app.DraftEvents. reset starts the parser afresh, for a log read
// from its start. The follower calls back on the goroutine that runs it, so
// the parser is only ever used from there.
func follower(s LogSetup, post Post) (f arena.Follower, reset func()) {
	var parser arena.Parser
	reset = func() { parser = arena.Parser{} }
	f = s.Follow
	f.Lines = func(lines []string) {
		var events []arena.Event
		for _, l := range lines {
			events = append(events, parser.Line(l)...)
		}
		if len(events) > 0 {
			post(app.DraftEvents{Events: events})
		}
		if s.Lines != nil {
			s.Lines(lines)
		}
	}
	f.Restarted = func() {
		reset()
		post(app.SessionStarted{})
	}
	f.CaughtUp = func() { post(app.LogCaughtUp{}) }
	f.Failed = func(err error) { post(app.LogUnreadable{Err: err}) }
	return f, reset
}

// watchLog follows path, or first searches cands for it when path is "",
// and goes back to searching each time the log is lost, until ctx ends.
// given is the path named with -log, if any, for the window to name.
// reset is called before each log is followed, which is read from its start.
func watchLog(ctx context.Context, path, given string, cands []arena.Candidate, every time.Duration, f arena.Follower, reset func(), post Post) {
	for {
		if path == "" {
			arena.Watch(ctx, cands, every, func(r arena.Search) {
				if r.Found != "" {
					path = r.Found
					post(app.LogFound{Path: r.Found})
					return
				}
				looked := make([]string, len(r.Looked))
				for i, c := range r.Looked {
					looked[i] = c.Where
				}
				post(app.LogSearched{Looked: looked, Given: given})
			})
			if path == "" {
				return // ctx ended before the log turned up
			}
		}
		reset()
		if err := f.Run(ctx, path); !errors.Is(err, arena.ErrLogGone) {
			return
		}
		post(app.LogLost{})
		path = ""
	}
}
