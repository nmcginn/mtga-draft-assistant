package background

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/arena"
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
	return LogSetup{
		Flag: flag, GOOS: "linux", Home: home, Getenv: func(string) string { return "" }, Every: time.Millisecond,
		Follow: arena.Follower{Every: time.Millisecond, LostAfter: 20 * time.Millisecond},
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// expect waits for the next message and fails unless it is a want.
func expect[M app.Msg](t *testing.T, next func() app.Msg) M {
	t.Helper()
	m := next()
	got, ok := m.(M)
	if !ok {
		var want M
		t.Fatalf("got %T %+v, want %T", m, m, want)
	}
	return got
}

func TestWithTheLogFlagTheWindowOpensFollowingItWithoutSearching(t *testing.T) {
	// The flag used to mean nothing started at all; now the log it names is
	// followed, but the usual places are still never searched.
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, "")
	model, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	if model.Phase != app.PhaseFollowing || model.LogPath != path {
		t.Errorf("model = %+v, want following %q", model, path)
	}
	post, next := collect(t)
	start(t.Context(), post)
	expect[app.LogCaughtUp](t, next)
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

func TestTheFoundLogsLinesAreHandedOnInOrderAsArenaWritesThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, "first\nsecond\n")
	s := setup(t.TempDir(), path)
	lines := make(chan []string, 10)
	s.Lines = func(l []string) { lines <- l }
	_, start, err := FindLog(s)
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	expect[app.LogCaughtUp](t, next)
	if got := <-lines; !slices.Equal(got, []string{"first", "second"}) {
		t.Errorf("first lines = %q, want the session so far", got)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("third\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-lines:
		if !slices.Equal(got, []string{"third"}) {
			t.Errorf("after an append: %q, want only the new line", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an appended line never arrived")
	}
}

func TestWhenArenaRestartsTheWindowIsToldANewSessionBegan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, "old session\n")
	_, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	expect[app.LogCaughtUp](t, next)

	if err := os.Rename(path, filepath.Join(filepath.Dir(path), "Player-prev.log")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "new session\n")
	expect[app.SessionStarted](t, next)
	expect[app.LogCaughtUp](t, next)
}

func TestWhenTheLogDisappearsTheWindowIsToldAndTheSearchStartsAgain(t *testing.T) {
	home := t.TempDir()
	_, start, err := FindLog(setup(home, ""))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	expect[app.LogSearched](t, next)

	path := filepath.Join(home, ".wine/drive_c/users/sam/AppData/LocalLow/Wizards Of The Coast/MTGA/Player.log")
	writeFile(t, path, "a line\n")
	if found := expect[app.LogFound](t, next); found.Path != path {
		t.Fatalf("found %q, want %q", found.Path, path)
	}
	expect[app.LogCaughtUp](t, next)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	expect[app.LogLost](t, next)
	// The window lists where it is looking again.
	if searched := expect[app.LogSearched](t, next); len(searched.Looked) == 0 || searched.Given != "" {
		t.Errorf("second search = %+v, want Arena's usual places", searched)
	}

	writeFile(t, path, "")
	if found := expect[app.LogFound](t, next); found.Path != path {
		t.Errorf("found %q again, want %q", found.Path, path)
	}
	expect[app.LogCaughtUp](t, next)
}

func TestWhenALogGivenWithTheFlagDisappearsOnlyThatPathIsWatched(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "copies", "Player.log")
	writeFile(t, path, "")
	_, start, err := FindLog(setup(home, path))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	expect[app.LogCaughtUp](t, next)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	expect[app.LogLost](t, next)
	searched := expect[app.LogSearched](t, next)
	if !slices.Equal(searched.Looked, []string{"-log: ~/copies"}) || searched.Given != path {
		t.Errorf("search = %+v, want only the -log folder, naming the file", searched)
	}
	writeFile(t, path, "")
	if found := expect[app.LogFound](t, next); found.Path != path {
		t.Errorf("found %q, want %q", found.Path, path)
	}
}

func TestALogThatCannotBeReadIsReportedToTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, "")
	_, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	expect[app.LogCaughtUp](t, next)

	// Replaced by a folder: a read failure that holds even for root.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if m := expect[app.LogUnreadable](t, next); m.Err == nil {
		t.Error("LogUnreadable carried no reason")
	}
}

func TestOnceTheWindowClosesTheLogTaskReportsNothingMore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, "")
	_, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan app.Msg, 10)
	start(ctx, func(m app.Msg) { ch <- m })
	if m := <-ch; m != (app.LogCaughtUp{}) {
		t.Fatalf("first message %+v, want LogCaughtUp", m)
	}
	cancel()
	// A log deleted after the window closed is not reported lost: the task
	// has stopped following it. LostAfter is 20 ms here; wait well past it.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-ch:
		t.Errorf("posted %T %+v after the window closed", m, m)
	case <-time.After(200 * time.Millisecond):
	}
}

const (
	premierFixture = "../../testdata/logs/2026-10-fra-premier-full-draft.log"
	quickFixture   = "../../testdata/logs/2026-10-fra-quick-full-draft.log"
)

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// applyUntil applies messages to m until one is an U, and returns the model
// then.
func applyUntil[U app.Msg](t *testing.T, m app.Model, next func() app.Msg) app.Model {
	t.Helper()
	for {
		msg := next()
		m = m.Apply(msg)
		if _, ok := msg.(U); ok {
			return m
		}
	}
}

func TestTheDraftInTheLogReachesTheWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, readFixture(t, premierFixture))
	model, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	m := applyUntil[app.LogCaughtUp](t, model, next)
	if d := m.Draft; d == nil || d.Set != "FRA" || !d.Done || len(d.Picks) != 42 {
		t.Errorf("draft once caught up: %+v, want the whole FRA draft", d)
	}
}

func TestAPackArenaWritesWhileFollowingIsShown(t *testing.T) {
	lines := strings.SplitAfter(readFixture(t, premierFixture), "\n")
	cut := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, `"SelfPick":4,"SelfPack":1`) })
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, strings.Join(lines[:cut], ""))
	model, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	m := applyUntil[app.LogCaughtUp](t, model, next)
	if d := m.Draft; d == nil || d.Pick != 3 {
		t.Fatalf("before the append: %+v, want p1p3 on show", d)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(lines[cut]); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	m = applyUntil[app.DraftEvents](t, m, next)
	if d := m.Draft; d.Pack != 1 || d.Pick != 4 || len(d.Cards) != 11 {
		t.Errorf("after the append: p%dp%d with %d cards, want p1p4 with 11", d.Pack, d.Pick, len(d.Cards))
	}
}

func TestAfterArenaRelaunchesTheNewLogIsReadAsANewSession(t *testing.T) {
	// The parser starts afresh with the new log: a bot draft that the old
	// one was in the middle of is news again when the new log resumes it,
	// and is named, not shown as a draft of unknown set.
	quick := strings.SplitAfter(readFixture(t, quickFixture), "\n")
	reply := slices.IndexFunc(quick, func(l string) bool { return strings.HasPrefix(l, "<== BotDraftDraftPick") })
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, strings.Join(quick[:reply+10], ""))
	model, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	m := applyUntil[app.LogCaughtUp](t, model, next)
	if m.Draft == nil || m.Draft.Done {
		t.Fatalf("first session: %+v, want a draft under way", m.Draft)
	}

	if err := os.Rename(path, filepath.Join(filepath.Dir(path), "Player-prev.log")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, strings.Join(quick[reply:], ""))
	m = applyUntil[app.SessionStarted](t, m, next)
	if m.Draft != nil {
		t.Errorf("a new session kept the old one's draft: %+v", m.Draft)
	}
	// The new log may be seen empty, and reported caught up, before its
	// lines land in it; so wait for the draft, not for a message.
	for m.Draft == nil || !m.Draft.Done {
		m = m.Apply(next())
	}
	if d := m.Draft; d.Event != "QuickDraft_FRA_20261008" || len(d.Picks) != 42 {
		t.Errorf("resumed draft: %+v, want QuickDraft_FRA_20261008 finished with 42 picks", d)
	}
}

func TestALogWithNoDraftSendsNoDraftEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	writeFile(t, path, "[UnityCrossThreadLogger]10/10/2026 12:44:31 PM\nAK Message: Audio thread resumed, audio restarts.\n\n")
	_, start, err := FindLog(setup(t.TempDir(), path))
	if err != nil {
		t.Fatal(err)
	}
	post, next := collect(t)
	start(t.Context(), post)
	expect[app.LogCaughtUp](t, next)
}
