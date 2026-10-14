package app

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestFirstPaintIsWaitingForTheLogWithNoPathYet(t *testing.T) {
	m := New()
	if m.Phase != PhaseFindingLog {
		t.Errorf("New().Phase = %v, want PhaseFindingLog", m.Phase)
	}
	if m.LogPath != "" {
		t.Errorf("New().LogPath = %q, want empty", m.LogPath)
	}
}

func TestFindingTheLogStartsFollowingIt(t *testing.T) {
	m := New().Apply(LogFound{Path: "/logs/Player.log"})
	if m.Phase != PhaseFollowing || m.LogPath != "/logs/Player.log" {
		t.Errorf("after LogFound: %+v, want following /logs/Player.log", m)
	}
}

func TestALogFoundWithNoPathIsIgnored(t *testing.T) {
	if got := New().Apply(LogFound{}); !reflect.DeepEqual(got, New()) {
		t.Errorf("LogFound{} changed the state to %+v", got)
	}
}

func TestLosingTheLogGoesBackToLookingAndForgetsThePath(t *testing.T) {
	m := New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(LogLost{})
	if !reflect.DeepEqual(m, New()) {
		t.Errorf("after LogLost: %+v, want the first-paint state", m)
	}
}

func TestApplyDoesNotMutateTheModelItWasCalledOn(t *testing.T) {
	before := New()
	_ = before.Apply(LogFound{Path: "/logs/Player.log"})
	if !reflect.DeepEqual(before, New()) {
		t.Errorf("Apply mutated its receiver: %+v", before)
	}
}

func TestWaitingStatusTellsTheUserHowToTurnOnDetailedLogs(t *testing.T) {
	// Without Detailed Logs, Arena writes nothing a draft can be read from,
	// so the empty state has to say where the switch is.
	s := New().Status()
	if !strings.Contains(s.Detail, "Detailed Logs") {
		t.Errorf("waiting detail = %q, want it to name the Detailed Logs setting", s.Detail)
	}
}

func TestEveryPhaseHasAHeadlineAndADetail(t *testing.T) {
	for name, m := range map[string]Model{
		"first paint":          New(),
		"searched, not found":  New().Apply(LogSearched{Looked: []string{"Wine: ~/.wine"}}),
		"nowhere to look":      New().Apply(LogSearched{}),
		"-log file gone":       New().Apply(LogSearched{Looked: []string{"-log: ~/logs"}, Given: "/home/sam/logs/Player.log"}),
		"following":            New().Apply(LogFound{Path: "/logs/Player.log"}),
		"caught up":            New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(LogCaughtUp{}),
		"unreadable":           New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(LogUnreadable{Err: errors.New("permission denied")}),
		"unreadable, no error": New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(LogUnreadable{}),
		"a task failed":        New().Apply(TaskFailed{Task: "Looking for Arena's log", Err: errors.New("boom")}),
		"failed with no error": New().Apply(TaskFailed{Task: "Looking for Arena's log"}),
	} {
		s := m.Status()
		if s.Headline == "" || s.Detail == "" {
			t.Errorf("%s: status %+v has an empty field", name, s)
		}
	}
}

func TestASearchThatFindsNothingListsWhereItLookedAndHowToPassTheLog(t *testing.T) {
	looked := []string{"Wine: ~/.wine", "Steam: ~/.steam/steam/steamapps/compatdata/2141910/pfx"}
	s := New().Apply(LogSearched{Looked: looked}).Status()
	if !slices.Equal(s.Places, looked) {
		t.Errorf("places = %q, want %q", s.Places, looked)
	}
	if !strings.Contains(s.Footnote, "-log") {
		t.Errorf("footnote = %q, want it to say how to pass -log", s.Footnote)
	}
	if !strings.Contains(s.Detail, "Detailed Logs") {
		t.Errorf("detail = %q, still wants to name the Detailed Logs setting", s.Detail)
	}
}

func TestBeforeTheFirstSearchPassNoPlacesAreClaimed(t *testing.T) {
	// First paint comes before the search has looked anywhere; listing
	// places then would be saying something that has not happened yet.
	if s := New().Status(); len(s.Places) != 0 || s.Footnote != "" {
		t.Errorf("first paint status = %+v, want no places and no footnote", s)
	}
}

func TestWithNowhereToLookTheOnlyAdviceIsTheLogFlag(t *testing.T) {
	m := New().Apply(LogSearched{})
	if !m.Searched {
		t.Fatal("an empty search was not recorded as a search")
	}
	s := m.Status()
	if s.Headline == New().Status().Headline {
		t.Errorf("headline %q reads as if the assistant were waiting normally", s.Headline)
	}
	if !strings.Contains(s.Detail, "-log") || len(s.Places) != 0 {
		t.Errorf("status = %+v, want no places and advice to pass -log", s)
	}
}

func TestTheSearchResultIsCopiedNotShared(t *testing.T) {
	looked := []string{"Wine: ~/.wine"}
	m := New().Apply(LogSearched{Looked: looked})
	looked[0] = "changed"
	if m.Looked[0] != "Wine: ~/.wine" {
		t.Errorf("the model's places changed with the sender's slice: %q", m.Looked)
	}
}

func TestFindingTheLogForgetsWhereTheSearchLooked(t *testing.T) {
	m := New().Apply(LogSearched{Looked: []string{"Wine: ~/.wine"}}).Apply(LogFound{Path: "/logs/Player.log"})
	if m.Searched || m.Looked != nil {
		t.Errorf("after LogFound: searched %v, looked %q; want neither", m.Searched, m.Looked)
	}
	if s := m.Status(); len(s.Places) != 0 || !strings.Contains(s.Footnote, "/logs/Player.log") {
		t.Errorf("following status = %+v, want no places and the path being read", s)
	}
}

func TestASearchReportArrivingAfterTheLogWasGivenIsIgnored(t *testing.T) {
	following := New().Apply(LogFound{Path: "/logs/Player.log"})
	if got := following.Apply(LogSearched{Looked: []string{"Wine: ~/.wine"}}); !reflect.DeepEqual(got, following) {
		t.Errorf("a late search report changed the state to %+v", got)
	}
}

func TestAFailedTaskIsShownWithWhatToDoNext(t *testing.T) {
	m := New().Apply(TaskFailed{Task: "Looking for Arena's log", Err: errors.New("index out of range")})
	s := m.Status()
	for _, want := range []string{"Looking for Arena's log", "index out of range", "Restart"} {
		if !strings.Contains(s.Detail, want) {
			t.Errorf("detail = %q, want it to contain %q", s.Detail, want)
		}
	}
	if !strings.Contains(s.Footnote, "/issues") {
		t.Errorf("footnote = %q, want where to report it", s.Footnote)
	}
}

func TestAFailureOutranksEveryOtherState(t *testing.T) {
	// Whatever the stopped task would have changed, it never will, so the
	// window must not go on looking as if all were well.
	m := New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(TaskFailed{Task: "Following the log", Err: errors.New("x")})
	if got := m.Status().Headline; got != "Something went wrong" {
		t.Errorf("headline = %q, want the failure", got)
	}
}

func TestTheFirstFailureIsKept(t *testing.T) {
	m := New().
		Apply(TaskFailed{Task: "first", Err: errors.New("a")}).
		Apply(TaskFailed{Task: "second", Err: errors.New("b")})
	if m.Failure.Task != "first" {
		t.Errorf("failure = %+v, want the first one", m.Failure)
	}
}

// caughtUp is a model following /logs/Player.log that has read it to the end.
func caughtUp() Model {
	return New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(LogCaughtUp{})
}

func TestUntilTheLogHasBeenReadToTheEndTheWindowSaysItIsReading(t *testing.T) {
	// A draft already under way may still be in the unread part, so
	// "ready for your next draft" would not yet be true.
	reading := New().Apply(LogFound{Path: "/logs/Player.log"}).Status()
	ready := caughtUp().Status()
	if reading.Headline == ready.Headline {
		t.Errorf("reading and caught up both say %q", reading.Headline)
	}
	if !strings.Contains(reading.Headline, "Reading") || !strings.Contains(reading.Footnote, "/logs/Player.log") {
		t.Errorf("reading status = %+v, want it to say so and name the file", reading)
	}
	if ready.Headline != "Ready for your next draft" {
		t.Errorf("caught-up headline = %q", ready.Headline)
	}
}

func TestANewArenaSessionIsReadFromItsStartAgain(t *testing.T) {
	m := caughtUp().Apply(SessionStarted{})
	if m.CaughtUp || m.Phase != PhaseFollowing || m.LogPath != "/logs/Player.log" {
		t.Errorf("after SessionStarted: %+v, want still following, catching up", m)
	}
	if m = m.Apply(LogCaughtUp{}); !m.CaughtUp {
		t.Error("the new session never counted as caught up")
	}
}

func TestAnUnreadableLogSaysWhyAndWhatToDo(t *testing.T) {
	m := caughtUp().Apply(LogUnreadable{Err: errors.New("open /logs/Player.log: permission denied")})
	s := m.Status()
	if !strings.Contains(s.Headline, "Can't read") {
		t.Errorf("headline = %q, want it to say the log cannot be read", s.Headline)
	}
	for _, want := range []string{"permission denied", "can read the file", "-log", "keeps trying"} {
		if !strings.Contains(s.Detail, want) {
			t.Errorf("detail = %q, want it to contain %q", s.Detail, want)
		}
	}
	if !strings.Contains(s.Footnote, "/logs/Player.log") {
		t.Errorf("footnote = %q, want the path", s.Footnote)
	}
}

func TestReadingAgainClearsTheError(t *testing.T) {
	m := caughtUp().Apply(LogUnreadable{Err: errors.New("x")}).Apply(LogCaughtUp{})
	if m.Unreadable != "" || m.Status().Headline != "Ready for your next draft" {
		t.Errorf("after reading again: %+v", m)
	}
}

func TestLosingOrFindingTheLogForgetsHowReadingTheOldOneWent(t *testing.T) {
	broken := caughtUp().Apply(LogUnreadable{Err: errors.New("x")})
	if lost := broken.Apply(LogLost{}); !reflect.DeepEqual(lost, New()) {
		t.Errorf("after LogLost: %+v, want the first-paint state", lost)
	}
	if found := caughtUp().Apply(LogFound{Path: "/other/Player.log"}); found.CaughtUp || found.Unreadable != "" {
		t.Errorf("a newly found log started out %+v, want unread", found)
	}
}

func TestReadingNewsArrivingAfterTheLogWasLostIsIgnored(t *testing.T) {
	// The follower may have queued a message just before the log went.
	lost := caughtUp().Apply(LogLost{})
	for _, msg := range []Msg{LogCaughtUp{}, SessionStarted{}, LogUnreadable{Err: errors.New("x")}} {
		if got := lost.Apply(msg); !reflect.DeepEqual(got, lost) {
			t.Errorf("%T while looking changed the state to %+v", msg, got)
		}
	}
}

func TestWhenTheLogNamedWithTheFlagGoesMissingTheWindowDoesNotAdviseTheFlag(t *testing.T) {
	// The user already named the file; telling them to name it, or listing
	// the folder they chose, would be no help.
	m := caughtUp().Apply(LogLost{}).Apply(LogSearched{Looked: []string{"-log: ~/logs"}, Given: "/home/sam/logs/Player.log"})
	s := m.Status()
	if !strings.Contains(s.Detail, "/home/sam/logs/Player.log") {
		t.Errorf("detail = %q, want the file named", s.Detail)
	}
	if strings.Contains(s.Detail, `-log "`) || len(s.Places) != 0 {
		t.Errorf("status = %+v, want neither the -log hint nor a list of places", s)
	}
	if !strings.Contains(s.Footnote, "without -log") {
		t.Errorf("footnote = %q, want how to go back to the usual places", s.Footnote)
	}
	if found := m.Apply(LogFound{Path: "/home/sam/logs/Player.log"}); found.Given != "" {
		t.Errorf("finding it again kept Given = %q", found.Given)
	}
}

func TestAnOSErrorIsShownByItsReasonWithoutRepeatingThePath(t *testing.T) {
	err := &fs.PathError{Op: "open", Path: "/logs/Player.log", Err: fs.ErrPermission}
	s := caughtUp().Apply(LogUnreadable{Err: fmt.Errorf("reading: %w", err)}).Status()
	if !strings.Contains(s.Detail, "(permission denied)") || strings.Contains(s.Detail, "/logs/Player.log") {
		t.Errorf("detail = %q, want the reason and not the path, which the footnote has", s.Detail)
	}
}
