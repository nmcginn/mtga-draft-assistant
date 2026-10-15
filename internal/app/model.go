// Package app holds the assistant's state and every transition on it, as pure
// functions over plain data. Nothing here touches the filesystem, the network,
// or the window: the UI renders a Model, and I/O goroutines report what they
// found as messages that Apply folds in. That is what lets the whole
// application be tested by feeding it messages.
package app

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
	"github.com/nmcginn/mtga-draft-assistant/internal/draft"
	"github.com/nmcginn/mtga-draft-assistant/internal/version"
)

// Phase is where the assistant is in its lifecycle, as the user would describe
// it.
type Phase int

const (
	// PhaseFindingLog is the first-paint state: no Player.log has been found
	// yet. It is a normal state, not an error — Arena may simply not have been
	// launched on this machine.
	PhaseFindingLog Phase = iota
	// PhaseFollowing means a log was found and is being read; no draft is in
	// progress.
	PhaseFollowing
)

// Model is the whole of the application's state. The UI reads it; only Apply
// changes it.
type Model struct {
	Phase Phase
	// LogPath is the Player.log being followed; empty until one is found.
	LogPath string
	// Searched is whether the search for the log has finished a pass
	// without finding it, and Looked is everywhere that pass looked, as the
	// window names the places. Searched with nothing in Looked means there
	// was nowhere to look.
	Searched bool
	Looked   []string
	// Given is the log named with -log, when that is the one file the
	// search is waiting for; empty when it searches Arena's usual places.
	Given string
	// CaughtUp is whether the log has been read to its end since it was
	// found or last started over. Until then the window says it is
	// reading, because a draft already under way may still be coming.
	CaughtUp bool
	// Unreadable is why the log could not be read, while it cannot; empty
	// otherwise.
	Unreadable string
	// Draft is the draft read from the log, or nil when the log has shown
	// none. A finished draft stays until the next one starts, so the window
	// can show what was drafted.
	Draft *draft.Draft
	// Failure is the first background task that stopped unexpectedly, or
	// nil. It is kept until restart: the task is not running any more, so
	// whatever it would have shown never will.
	Failure *Failure
}

// Failure is a background task that stopped unexpectedly.
type Failure struct {
	// Task says what was being done, as a user would put it: "Looking for
	// Arena's log".
	Task string
	Err  string
}

// New returns the state the window paints first, before any I/O has happened.
func New() Model {
	return Model{Phase: PhaseFindingLog}
}

// Msg is something an I/O goroutine observed. Each one is a plain value so a
// test can construct it.
type Msg interface{ isMsg() }

// LogFound reports that a Player.log exists at Path and is now being followed.
type LogFound struct{ Path string }

// LogLost reports that the followed log went away (Arena's install moved, the
// file was deleted) and the assistant is looking again.
type LogLost struct{}

// LogCaughtUp reports that the followed log has been read to its end, so
// everything Arena has written this session has been seen.
type LogCaughtUp struct{}

// SessionStarted reports that the followed log started over because Arena
// was relaunched. Whatever was read from the old session no longer
// describes what is on screen in Arena, and the new one is read from its
// start.
type SessionStarted struct{}

// LogUnreadable reports that the log is there but reading it failed. The
// follower keeps trying; LogCaughtUp says when it works again.
type LogUnreadable struct{ Err error }

// LogSearched reports that a search for the log looked in every one of
// Looked and found nothing. The search goes on; this is sent once per
// search, so the window can say where it is looking. Given is set when the
// search is for the file named with -log, which went missing, rather than
// Arena's usual places.
type LogSearched struct {
	Looked []string
	Given  string
}

// DraftEvents reports what the log said about drafting, in the order it was
// written. The log's lines are read in batches, and one message carries a
// batch's events, so catching up on a long session costs a frame per
// batch, not one per pick.
type DraftEvents struct{ Events []arena.Event }

// TaskFailed reports that a background task stopped, typically because it
// panicked: the goroutine recovered and sent this instead of taking the
// whole window down with it (decision 4).
type TaskFailed struct {
	Task string
	Err  error
}

func (LogFound) isMsg()       {}
func (LogLost) isMsg()        {}
func (LogSearched) isMsg()    {}
func (LogCaughtUp) isMsg()    {}
func (SessionStarted) isMsg() {}
func (LogUnreadable) isMsg()  {}
func (DraftEvents) isMsg()    {}
func (TaskFailed) isMsg()     {}

// Apply returns the state after msg. It never fails: a message that does not
// make sense in the current phase leaves the state as it was.
func (m Model) Apply(msg Msg) Model {
	switch msg := msg.(type) {
	case LogFound:
		if msg.Path == "" {
			return m
		}
		m.Phase = PhaseFollowing
		m.LogPath = msg.Path
		m.Searched, m.Looked, m.Given = false, nil, ""
		m.CaughtUp, m.Unreadable = false, ""
		// The log is read from its start, and says again what draft is
		// under way.
		m.Draft = nil
	case LogLost:
		m.Phase = PhaseFindingLog
		m.LogPath = ""
		m.Searched, m.Looked, m.Given = false, nil, ""
		m.CaughtUp, m.Unreadable = false, ""
		m.Draft = nil
	case LogCaughtUp:
		// The follower's messages describe the log it was following; one
		// still queued when that log was lost has nothing to say.
		if m.Phase != PhaseFollowing {
			return m
		}
		m.CaughtUp, m.Unreadable = true, ""
	case SessionStarted:
		if m.Phase != PhaseFollowing {
			return m
		}
		m.CaughtUp = false
		// A draft Arena reopens after relaunching says so in the new
		// session's log; one that it does not was left behind.
		m.Draft = nil
	case DraftEvents:
		// Events still queued from a log that was since lost describe
		// nothing on screen.
		if m.Phase != PhaseFollowing {
			return m
		}
		for _, e := range msg.Events {
			m.Draft = draft.Apply(m.Draft, e)
		}
	case LogUnreadable:
		if m.Phase != PhaseFollowing {
			return m
		}
		m.CaughtUp = false
		m.Unreadable = "no reason given"
		// The window names the file already; an OS error would name it
		// again, so only its reason is kept ("permission denied").
		var pe *fs.PathError
		switch {
		case errors.As(msg.Err, &pe) && pe.Err != nil:
			m.Unreadable = pe.Err.Error()
		case msg.Err != nil:
			m.Unreadable = msg.Err.Error()
		}
	case LogSearched:
		// A search that finishes after the log was given another way (the
		// -log flag) has nothing to add.
		if m.Phase != PhaseFindingLog {
			return m
		}
		m.Searched = true
		m.Looked = slices.Clone(msg.Looked)
		m.Given = msg.Given
	case TaskFailed:
		if m.Failure != nil {
			// The first failure is the one most likely to explain the rest.
			return m
		}
		f := Failure{Task: msg.Task, Err: "no reason given"}
		if msg.Err != nil {
			f.Err = msg.Err.Error()
		}
		m.Failure = &f
	}
	return m
}

// Status is what the window shows when there is no draft to display: a
// headline, the next step, and optionally the places searched and a smaller
// note under them. It is the empty state's whole job to say what the user
// should do, so it lives here, where it can be tested, rather than in a view.
type Status struct {
	Headline string
	Detail   string
	Places   []string
	Footnote string
}

// logFlagHint says how to point the assistant at a log it cannot find.
const logFlagHint = `start the assistant with -log "/path/to/Player.log"`

// Status describes the current state for the user.
func (m Model) Status() Status {
	if f := m.Failure; f != nil {
		return Status{
			Headline: "Something went wrong",
			Detail: fmt.Sprintf("%s stopped unexpectedly (%s). Restart the assistant to try again.",
				f.Task, f.Err),
			Footnote: "If it happens again, please report it, with what you were doing, at " + version.IssuesURL + ".",
		}
	}
	switch m.Phase {
	case PhaseFollowing:
		if m.Unreadable != "" {
			return Status{
				Headline: "Can't read Arena's log",
				Detail: fmt.Sprintf("Reading it failed (%s). Check that your user account can read the file, "+
					"or %s to read a copy. The assistant keeps trying in the meantime.", m.Unreadable, logFlagHint),
				Footnote: "Trying to read " + m.LogPath,
			}
		}
		if !m.CaughtUp {
			return Status{
				Headline: "Reading Arena's log",
				Detail:   "Catching up on this Arena session, so a draft already under way shows up.",
				Footnote: "Reading " + m.LogPath,
			}
		}
		return Status{
			Headline: "Ready for your next draft",
			Detail:   "Start a draft in MTG Arena and the current pack appears here.",
			Footnote: "Reading " + m.LogPath,
		}
	default:
		if m.Given != "" {
			// The user named this file, so the usual advice (where the
			// assistant looked, and to name the file) is beside the point.
			return Status{
				Headline: "Waiting for the log you named",
				Detail: "There is no file at " + m.Given + " any more. Arena starts a new log each time it launches; " +
					"the assistant picks it up as soon as it is back.",
				Footnote: "To look in Arena's usual places instead, restart the assistant without -log.",
			}
		}
		if m.Searched && len(m.Looked) == 0 {
			return Status{
				Headline: "Can't tell where Arena keeps its log",
				Detail: "There is no usual place to look for it on this computer, so " + logFlagHint + ". " +
					"Arena writes it while it runs, with Options → Account → Detailed Logs (Plugin Support) turned on.",
			}
		}
		st := Status{
			Headline: "Waiting for MTG Arena",
			Detail: "Launch Arena, then turn on Options → Account → Detailed Logs (Plugin Support) " +
				"so the assistant can see your draft.",
		}
		if m.Searched {
			st.Places = m.Looked
			st.Footnote = "No Arena log in any of these yet; checking again every few seconds. " +
				"If Arena is installed somewhere else, " + logFlagHint + "."
		}
		return st
	}
}
