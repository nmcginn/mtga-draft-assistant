package arena

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// consumeAll feeds data to a fresh cursor in chunks of size n and returns
// every line and the final cursor.
func consumeAll(data []byte, n int) (Cursor, []string) {
	var c Cursor
	var all []string
	for len(data) > 0 {
		k := min(n, len(data))
		var lines []string
		c, lines = c.Consume(data[:k])
		all = append(all, lines...)
		data = data[k:]
	}
	return c, all
}

func TestAnEmptyChunkGivesNoLinesAndDoesNotMove(t *testing.T) {
	c, lines := Cursor{}.Consume(nil)
	if c.Offset != 0 || len(lines) != 0 {
		t.Errorf("Consume(nil) = %+v, %q; want nothing", c, lines)
	}
}

func TestCompleteLinesComeOutInOrderWithoutTheirLineEndings(t *testing.T) {
	in := "first\r\nsecond\n\nthird\n"
	c, lines := Cursor{}.Consume([]byte(in))
	if want := []string{"first", "second", "", "third"}; !slices.Equal(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
	if c.Offset != int64(len(in)) || len(c.partial) != 0 {
		t.Errorf("cursor = %+v, want offset %d and nothing held back", c, len(in))
	}
}

func TestALineCutMidWriteIsHeldBackUntilItsNewlineArrives(t *testing.T) {
	c, lines := Cursor{}.Consume([]byte(`done` + "\n" + `[UnityCrossThreadLogger]==> Event_PlayerDraftMakePick {"id":`))
	if !slices.Equal(lines, []string{"done"}) {
		t.Fatalf("first read gave %q, want only the finished line", lines)
	}
	c, lines = c.Consume([]byte(`"1"}`))
	if len(lines) != 0 {
		t.Fatalf("a still-unfinished line was handed on: %q", lines)
	}
	c, lines = c.Consume([]byte("\r\nnext\n"))
	want := []string{`[UnityCrossThreadLogger]==> Event_PlayerDraftMakePick {"id":"1"}`, "next"}
	if !slices.Equal(lines, want) {
		t.Errorf("once finished: %q, want %q", lines, want)
	}
	if c.Offset != 4+1+59+5+2+5 {
		t.Errorf("offset = %d, want every byte counted once", c.Offset)
	}
}

func TestConsumeChangesNeitherItsCursorNorKeepsTheCallersBuffer(t *testing.T) {
	buf := []byte("half a li")
	before, _ := Cursor{}.Consume(buf)
	copy(buf, "XXXXXXXXX") // the follower reuses nothing, but a caller may
	after, lines := before.Consume([]byte("ne\n"))
	if !slices.Equal(lines, []string{"half a line"}) {
		t.Errorf("lines = %q: the held-back part changed with the caller's buffer", lines)
	}
	if before.Offset != 9 || string(before.partial) != "half a li" {
		t.Errorf("Consume changed the cursor it was called on: %+v", before)
	}
	if after.Offset != 12 {
		t.Errorf("after.Offset = %d, want 12", after.Offset)
	}
}

func TestEveryFixtureGivesTheSameLinesHoweverItIsCutUp(t *testing.T) {
	// The follower sees the log in whatever pieces Arena's writes and its
	// own polls happen to make; the lines must not depend on where they fall.
	fixtures, _ := filepath.Glob("../../testdata/logs/*.log")
	if len(fixtures) == 0 {
		t.Fatal("no fixtures under testdata/logs")
	}
	for _, fx := range fixtures {
		data, err := os.ReadFile(fx)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		for _, n := range []int{1, 7, 4096, len(data)} {
			c, got := consumeAll(data, n)
			if !slices.Equal(got, want) {
				t.Errorf("%s in %d-byte reads: %d lines, want %d", filepath.Base(fx), n, len(got), len(want))
			}
			if c.Offset != int64(len(data)) {
				t.Errorf("%s in %d-byte reads: offset %d, want %d", filepath.Base(fx), n, c.Offset, len(data))
			}
		}
	}
}

func TestALineLongerThanTheLimitIsDroppedWholeAndTheNextOneKept(t *testing.T) {
	huge := strings.Repeat("x", MaxLine+1)
	for name, n := range map[string]int{"in one read": 3 * MaxLine, "across reads": 1 << 20} {
		c, lines := consumeAll([]byte("before\n"+huge+"\nafter\n"), n)
		if !slices.Equal(lines, []string{"before", "after"}) {
			t.Errorf("%s: got %d lines starting %.20q, want before and after only", name, len(lines), lines)
		}
		if len(c.partial) != 0 || c.skipping {
			t.Errorf("%s: cursor still holds the long line: %d bytes, skipping %v", name, len(c.partial), c.skipping)
		}
	}
	// One exactly at the limit is a line like any other.
	_, lines := Cursor{}.Consume([]byte(strings.Repeat("y", MaxLine) + "\n"))
	if len(lines) != 1 || len(lines[0]) != MaxLine {
		t.Errorf("a line of exactly MaxLine bytes was not kept")
	}
}

func TestAnUnfinishedLineOverTheLimitIsNotHeldInMemory(t *testing.T) {
	c, _ := consumeAll([]byte(strings.Repeat("z", MaxLine+10)), 1<<20)
	if len(c.partial) != 0 || !c.skipping {
		t.Errorf("cursor holds %d bytes of a line with no end in sight", len(c.partial))
	}
}

func TestTheLogHasRestartedWhenItIsShorterOrAnotherFile(t *testing.T) {
	c := Cursor{Offset: 100}
	for _, tc := range []struct {
		size int64
		same bool
		want bool
	}{
		{100, true, false}, // nothing new
		{150, true, false}, // grew
		{99, true, true},   // truncated
		{0, true, true},    // emptied
		{150, false, true}, // replaced, even though longer
		{10, false, true},  // replaced
	} {
		if got := c.Restarted(tc.size, tc.same); got != tc.want {
			t.Errorf("Restarted(%d, same=%v) = %v, want %v", tc.size, tc.same, got, tc.want)
		}
	}
}

// recorder is a Follower whose callbacks record what they were told.
type recorder struct {
	lines    []string
	batches  int
	events   []string // "restarted", "caught up", "failed: ..."
	follower Follower
}

func newRecorder() *recorder {
	r := &recorder{}
	r.follower = Follower{
		Lines:     func(l []string) { r.lines = append(r.lines, l...); r.batches++ },
		Restarted: func() { r.events = append(r.events, "restarted") },
		CaughtUp:  func() { r.events = append(r.events, "caught up") },
		Failed:    func(err error) { r.events = append(r.events, "failed: "+err.Error()) },
	}
	return r
}

// take returns what was recorded since the last take, and forgets it.
func (r *recorder) take() ([]string, []string) {
	l, e := r.lines, r.events
	r.lines, r.events = nil, nil
	return l, e
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// following returns a log path in a fresh directory, a recorder, and its
// tail, not yet polled.
func following(t *testing.T) (string, *recorder, *tail) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Player.log")
	r := newRecorder()
	return path, r, newTail(r.follower, path)
}

var epoch = time.Date(2026, 10, 13, 20, 0, 0, 0, time.UTC)

func TestAnEmptyLogIsCaughtUpAtOnceWithNoLines(t *testing.T) {
	path, r, tl := following(t)
	write(t, path, "")
	tl.poll(t.Context(), epoch)
	lines, events := r.take()
	if len(lines) != 0 || !slices.Equal(events, []string{"caught up"}) {
		t.Errorf("lines %q, events %q; want no lines and caught up", lines, events)
	}
}

func TestOnFindingTheLogTheWholeSessionSoFarIsRead(t *testing.T) {
	// A draft may already be under way when the assistant starts.
	data, err := os.ReadFile("../../testdata/logs/2026-10-fra-premier-full-draft.log")
	if err != nil {
		t.Fatal(err)
	}
	path, r, tl := following(t)
	write(t, path, string(data))
	tl.poll(t.Context(), epoch)
	lines, events := r.take()
	if want := strings.Count(string(data), "\n"); len(lines) != want {
		t.Errorf("read %d lines, want all %d", len(lines), want)
	}
	if !slices.Equal(events, []string{"caught up"}) {
		t.Errorf("events = %q, want caught up once", events)
	}
}

func TestAfterwardOnlyWhatArenaAppendsIsRead(t *testing.T) {
	path, r, tl := following(t)
	write(t, path, "one\ntwo\n")
	tl.poll(t.Context(), epoch)
	r.take()

	tl.poll(t.Context(), epoch)
	if lines, events := r.take(); len(lines) != 0 || len(events) != 0 {
		t.Errorf("a check with nothing new reported lines %q, events %q", lines, events)
	}

	appendTo(t, path, "three\n")
	tl.poll(t.Context(), epoch)
	if lines, events := r.take(); !slices.Equal(lines, []string{"three"}) || len(events) != 0 {
		t.Errorf("after an append: lines %q, events %q; want only the new line", lines, events)
	}
	if tl.cursor.Offset != int64(len("one\ntwo\nthree\n")) {
		t.Errorf("offset %d, want the end of the file", tl.cursor.Offset)
	}
}

func TestALineArenaIsHalfwayThroughWritingIsHandedOnOnceItIsFinished(t *testing.T) {
	path, r, tl := following(t)
	write(t, path, "whole\n[UnityCrossThreadLogger]==> Event_Pla")
	tl.poll(t.Context(), epoch)
	if lines, _ := r.take(); !slices.Equal(lines, []string{"whole"}) {
		t.Fatalf("first read gave %q, want only the finished line", lines)
	}
	appendTo(t, path, "yerDraftMakePick {}\n")
	tl.poll(t.Context(), epoch)
	if lines, _ := r.take(); !slices.Equal(lines, []string{"[UnityCrossThreadLogger]==> Event_PlayerDraftMakePick {}"}) {
		t.Errorf("once finished: %q, want the line whole, once", lines)
	}
}

func TestATruncatedLogIsReadAgainFromTheStartAsANewSession(t *testing.T) {
	path, r, tl := following(t)
	write(t, path, "old session line one\nold session line two\n")
	tl.poll(t.Context(), epoch)
	r.take()

	write(t, path, "new\n") // os.WriteFile truncates in place: the same file
	tl.poll(t.Context(), epoch)
	lines, events := r.take()
	if !slices.Equal(lines, []string{"new"}) {
		t.Errorf("lines = %q, want the new session from its start", lines)
	}
	if !slices.Equal(events, []string{"restarted", "caught up"}) {
		t.Errorf("events = %q, want restarted then caught up", events)
	}
}

func TestALogReplacedByArenasNextLaunchIsReadAfreshEvenWhenLonger(t *testing.T) {
	// Arena moves Player.log to Player-prev.log and starts a new one, which
	// may already be longer than the old one by the time it is checked.
	path, r, tl := following(t)
	write(t, path, "old\n")
	tl.poll(t.Context(), epoch)
	r.take()

	if err := os.Rename(path, filepath.Join(filepath.Dir(path), "Player-prev.log")); err != nil {
		t.Fatal(err)
	}
	write(t, path, "a new session, already longer\nsecond\n")
	tl.poll(t.Context(), epoch)
	lines, events := r.take()
	if !slices.Equal(lines, []string{"a new session, already longer", "second"}) {
		t.Errorf("lines = %q, want the new log from its start", lines)
	}
	if !slices.Equal(events, []string{"restarted", "caught up"}) {
		t.Errorf("events = %q, want restarted then caught up", events)
	}
}

func TestAPartialLineFromTheOldSessionIsNotGluedToTheNewOne(t *testing.T) {
	path, r, tl := following(t)
	write(t, path, "complete\ncut off by the cra")
	tl.poll(t.Context(), epoch)
	r.take()
	write(t, path, "fresh\n")
	tl.poll(t.Context(), epoch)
	if lines, _ := r.take(); !slices.Equal(lines, []string{"fresh"}) {
		t.Errorf("lines = %q, want only the new session's line", lines)
	}
}

func TestAMultiMegabyteLogIsReadInPiecesAndThenIncrementally(t *testing.T) {
	path, r, tl := following(t)
	line := strings.Repeat("x", 99) + "\n"
	big := strings.Repeat(line, 50_000) // 5 MB
	write(t, path, big)
	tl.poll(t.Context(), epoch)
	if len(r.lines) != 50_000 {
		t.Fatalf("read %d lines, want 50000", len(r.lines))
	}
	if r.batches < 5 {
		t.Errorf("5 MB arrived in %d batches; it should be read a chunk at a time", r.batches)
	}
	r.take()

	for i := range 20 {
		appendTo(t, path, fmt.Sprintf("appended %d\n", i))
		tl.poll(t.Context(), epoch)
		if lines, _ := r.take(); !slices.Equal(lines, []string{fmt.Sprintf("appended %d", i)}) {
			t.Fatalf("append %d: got %d lines, want just the one appended", i, len(lines))
		}
	}
}

func TestALogMissingForAMomentIsNotLost(t *testing.T) {
	path, r, tl := following(t)
	write(t, path, "before\n")
	tl.poll(t.Context(), epoch)
	r.take()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if tl.poll(t.Context(), epoch.Add(time.Second)) || tl.poll(t.Context(), epoch.Add(LostAfter)) {
		t.Fatal("the log counted as gone before it had been missing for LostAfter")
	}
	write(t, path, "back\n")
	if tl.poll(t.Context(), epoch.Add(LostAfter+time.Second)) {
		t.Fatal("a log that came back counted as gone")
	}
	if lines, events := r.take(); !slices.Equal(lines, []string{"back"}) || !slices.Equal(events, []string{"restarted", "caught up"}) {
		t.Errorf("lines %q, events %q; want the new file read as a new session", lines, events)
	}
	// The clock starts again from its return.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if tl.poll(t.Context(), epoch.Add(LostAfter+2*time.Second)) {
		t.Error("the earlier absence was counted against the second one")
	}
}

func TestALogMissingForLongEnoughIsGone(t *testing.T) {
	path, _, tl := following(t)
	write(t, path, "x\n")
	tl.poll(t.Context(), epoch)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tl.poll(t.Context(), epoch.Add(time.Second))
	if !tl.poll(t.Context(), epoch.Add(time.Second+LostAfter)) {
		t.Error("a log missing for LostAfter was not reported gone")
	}
}

func TestALogThatCannotBeReadIsReportedOnceAndRecovers(t *testing.T) {
	// A folder where the log should be stands in for any read failure:
	// unlike permissions, it fails even for root, which CI may run as.
	path, r, tl := following(t)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		tl.poll(t.Context(), epoch)
	}
	_, events := r.take()
	if len(events) != 1 || !strings.HasPrefix(events[0], "failed: ") || !strings.Contains(events[0], path) {
		t.Fatalf("events = %q, want one failure naming the path", events)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(t, path, "readable now\n")
	tl.poll(t.Context(), epoch)
	if lines, events := r.take(); !slices.Equal(lines, []string{"readable now"}) || !slices.Equal(events, []string{"caught up"}) {
		t.Errorf("after recovering: lines %q, events %q; want the line and caught up", lines, events)
	}
}

func TestALogWithoutReadPermissionIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their permissions")
	}
	path, r, tl := following(t)
	write(t, path, "secret\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	tl.poll(t.Context(), epoch)
	if lines, events := r.take(); len(lines) != 0 || len(events) != 1 || !strings.Contains(events[0], "permission denied") {
		t.Errorf("lines %q, events %q; want one permission failure", lines, events)
	}
}

func TestRunReportsTheLogGoneOnceItHasBeenDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	write(t, path, "x\n")
	caught := make(chan struct{}, 1)
	f := Follower{Every: time.Millisecond, LostAfter: 20 * time.Millisecond, CaughtUp: func() { caught <- struct{}{} }}
	done := make(chan error, 1)
	go func() { done <- f.Run(t.Context(), path) }()
	<-caught
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrLogGone) {
			t.Errorf("Run = %v, want ErrLogGone", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the log was deleted")
	}
}

func TestRunStopsWhenItsContextEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	write(t, path, "")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Follower{Every: time.Millisecond}.Run(ctx, path) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

func TestALogGivenWithTheFlagIsWatchedForAtThatPathAlone(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "logs [copy]*")
	path := filepath.Join(dir, "Player.log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A neighbour the brackets and star would match, were they wildcards.
	if err := os.MkdirAll(filepath.Join(home, "logs c-anything"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, "logs c-anything", "Player.log"), "")

	c := Exactly("linux", home, path)
	if c.Where != "-log: ~/logs [copy]*" {
		t.Errorf("Where = %q, want the folder, home as ~", c.Where)
	}
	if got := Look([]Candidate{c}); got != "" {
		t.Errorf("before the file exists Look found %q", got)
	}
	write(t, path, "")
	if got := Look([]Candidate{c}); got != path {
		t.Errorf("Look = %q, want %q", got, path)
	}
}
