package background

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
)

// collect returns a Post that forwards to a channel, and a way to wait for
// the next message.
func collect(t *testing.T) (Post, func() app.Msg) {
	t.Helper()
	ch := make(chan app.Msg, 10)
	return func(m app.Msg) { ch <- m }, func() app.Msg {
		t.Helper()
		select {
		case m := <-ch:
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("no message within 5s")
			return nil
		}
	}
}

func TestAPanickingTaskIsReportedNotFatal(t *testing.T) {
	post, next := collect(t)
	Go("Looking for Arena's log", post, func() {
		var picks []int
		_ = picks[3] // a real runtime panic, not a call to panic()
	})
	failed, ok := next().(app.TaskFailed)
	if !ok {
		t.Fatalf("got %T, want app.TaskFailed", failed)
	}
	if failed.Task != "Looking for Arena's log" || failed.Err == nil || !strings.Contains(failed.Err.Error(), "index out of range") {
		t.Errorf("TaskFailed = %+v, want the task named and the panic's reason", failed)
	}
}

func TestATaskThatReturnsReportsNothing(t *testing.T) {
	ch := make(chan app.Msg, 1)
	done := make(chan struct{})
	Go("nothing", func(m app.Msg) { ch <- m }, func() { close(done) })
	<-done
	select {
	case m := <-ch:
		t.Errorf("a task that returned normally posted %+v", m)
	case <-time.After(50 * time.Millisecond):
	}
}

func setup(home, flag string) LogSetup {
	return LogSetup{Flag: flag, GOOS: "linux", Home: home, Getenv: func(string) string { return "" }, Every: time.Millisecond}
}

func TestWithTheLogFlagTheWindowOpensFollowingItAndNothingSearches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	model, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	if model.Phase != app.PhaseFollowing || model.LogPath != path {
		t.Errorf("model = %+v, want following %q", model, path)
	}
	if start != nil {
		t.Error("a search was set up although -log gave the path")
	}
}

func TestABadLogFlagIsAnErrorBeforeTheWindowOpens(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "Player.log")
	_, start, err := FindLog(setup(t.TempDir(), missing))
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %v, want one naming %q", err, missing)
	}
	if start != nil {
		t.Error("a bad -log fell back to searching")
	}
}

func TestWithoutTheFlagTheWindowOpensWaitingAndTheSearchReportsToIt(t *testing.T) {
	home := t.TempDir()
	model, start, err := FindLog(setup(home, ""))
	if err != nil || start == nil {
		t.Fatalf("FindLog = %v, start set %v; want a search to start", err, start != nil)
	}
	if model.Phase != app.PhaseFindingLog || model.Searched {
		t.Errorf("first-paint model = %+v, want waiting, nothing searched yet", model)
	}

	post, next := collect(t)
	start(t.Context(), post)

	searched, ok := next().(app.LogSearched)
	if !ok || len(searched.Looked) == 0 || !strings.HasPrefix(searched.Looked[0], "Wine: ~/.wine") {
		t.Fatalf("first message = %+v, want LogSearched naming the places", searched)
	}

	want := filepath.Join(home, ".wine/drive_c/users/sam/AppData/LocalLow/Wizards Of The Coast/MTGA/Player.log")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if found, ok := next().(app.LogFound); !ok || found.Path != want {
		t.Errorf("second message = %+v, want LogFound %q", found, want)
	}
}

func TestWithNoHomeTheSearchSaysThereIsNowhereToLook(t *testing.T) {
	_, start, err := FindLog(setup("", ""))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	if m, ok := next().(app.LogSearched); !ok || len(m.Looked) != 0 {
		t.Errorf("message = %+v, want LogSearched with nowhere looked", m)
	}
}
