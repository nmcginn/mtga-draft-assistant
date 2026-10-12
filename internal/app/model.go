// Package app holds the assistant's state and every transition on it, as pure
// functions over plain data. Nothing here touches the filesystem, the network,
// or the window: the UI renders a Model, and I/O goroutines report what they
// found as messages that Apply folds in. That is what lets the whole
// application be tested by feeding it messages.
package app

import (
	"fmt"
	"slices"

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

// LogSearched reports that a search for the log looked in every one of
// Looked and found nothing. The search goes on; this is sent once per
// search, so the window can say where it is looking.
type LogSearched struct{ Looked []string }

// TaskFailed reports that a background task stopped, typically because it
// panicked: the goroutine recovered and sent this instead of taking the
// whole window down with it (decision 4).
type TaskFailed struct {
	Task string
	Err  error
}

func (LogFound) isMsg()    {}
func (LogLost) isMsg()     {}
func (LogSearched) isMsg() {}
func (TaskFailed) isMsg()  {}

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
		m.Searched, m.Looked = false, nil
	case LogLost:
		m.Phase = PhaseFindingLog
		m.LogPath = ""
		m.Searched, m.Looked = false, nil
	case LogSearched:
		// A search that finishes after the log was given another way (the
		// -log flag) has nothing to add.
		if m.Phase != PhaseFindingLog {
			return m
		}
		m.Searched = true
		m.Looked = slices.Clone(msg.Looked)
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
		return Status{
			Headline: "Ready for your next draft",
			Detail:   "Start a draft in MTG Arena and the current pack appears here.",
			Footnote: "Reading " + m.LogPath,
		}
	default:
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
