// Package app holds the assistant's state and every transition on it, as pure
// functions over plain data. Nothing here touches the filesystem, the network,
// or the window: the UI renders a Model, and I/O goroutines report what they
// found as messages that Apply folds in. That is what lets the whole
// application be tested by feeding it messages.
package app

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

func (LogFound) isMsg() {}
func (LogLost) isMsg()  {}

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
	case LogLost:
		m.Phase = PhaseFindingLog
		m.LogPath = ""
	}
	return m
}

// Status is the headline and the next step the window shows for a phase. It is
// the empty state's whole job to say what the user should do, so it lives here,
// where it can be tested, rather than in a view.
type Status struct {
	Headline string
	Detail   string
}

// Status describes the current phase for the user.
func (m Model) Status() Status {
	switch m.Phase {
	case PhaseFollowing:
		return Status{
			Headline: "Ready for your next draft",
			Detail:   "Start a draft in MTG Arena and the current pack appears here.",
		}
	default:
		return Status{
			Headline: "Waiting for MTG Arena",
			Detail: "Launch Arena, then turn on Options → Account → Detailed Logs (Plugin Support) " +
				"so the assistant can see your draft.",
		}
	}
}
