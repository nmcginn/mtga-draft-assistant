package app

import (
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
	if got := New().Apply(LogFound{}); got != New() {
		t.Errorf("LogFound{} changed the state to %+v", got)
	}
}

func TestLosingTheLogGoesBackToLookingAndForgetsThePath(t *testing.T) {
	m := New().Apply(LogFound{Path: "/logs/Player.log"}).Apply(LogLost{})
	if m != New() {
		t.Errorf("after LogLost: %+v, want the first-paint state", m)
	}
}

func TestApplyDoesNotMutateTheModelItWasCalledOn(t *testing.T) {
	before := New()
	_ = before.Apply(LogFound{Path: "/logs/Player.log"})
	if before != New() {
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
	for _, phase := range []Phase{PhaseFindingLog, PhaseFollowing} {
		s := Model{Phase: phase}.Status()
		if s.Headline == "" || s.Detail == "" {
			t.Errorf("phase %v: status %+v has an empty field", phase, s)
		}
	}
}
