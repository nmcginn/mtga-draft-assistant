package arena

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FollowEvery is how often the follower checks the log for new bytes. Each
// check is one stat; only a log that grew is opened and read (decision 14).
// A fifth of a second is well inside a pick, and costs nothing measurable.
const FollowEvery = 200 * time.Millisecond

// LostAfter is how long the log has to stay missing before the follower
// gives up on it. Arena moves Player.log aside and starts a new one on every
// launch, so a log missing for a moment is a restart in progress, not a lost
// log; waiting this long keeps the window from flashing back to "Waiting
// for MTG Arena" in between (decision 22).
const LostAfter = 3 * time.Second

// MaxLine is the longest line the follower keeps. Arena's longest lines are
// JSON payloads of some kilobytes; anything far beyond that is not a line a
// draft is read from, and buffering it without limit would let a log with
// no newlines grow the app's memory without limit too. A longer line is
// dropped whole, up to its newline.
const MaxLine = 16 << 20

// readChunk bounds how much of the log is read and split at once, so
// catching up on a long session holds a megabyte, not the whole log.
const readChunk = 1 << 20

// ErrLogGone is what Follower.Run returns when the log has been missing for
// LostAfter.
var ErrLogGone = errors.New("the log is gone")

// Cursor is how far into one log file the follower has read: Offset bytes,
// the last of which may be a line Arena has not finished writing yet. The
// zero Cursor is the start of a file. A Cursor is a value: Consume returns a
// new one and never changes the one it was called on.
type Cursor struct {
	// Offset is the number of bytes consumed, including the unfinished
	// line held back until its newline arrives.
	Offset int64
	// partial is that unfinished line.
	partial []byte
	// skipping is set while a line longer than MaxLine is being dropped:
	// everything up to its newline is discarded.
	skipping bool
}

// Restarted reports whether the log has started over since the cursor was
// taken: it is no longer the same file (Arena moved the old one aside and
// began a new one), or it is shorter than what was already read (it was
// truncated in place). Either way, it is read again from the start.
func (c Cursor) Restarted(size int64, sameFile bool) bool {
	return !sameFile || size < c.Offset
}

// Consume takes the bytes read from the log at c.Offset and returns the
// cursor after them and the lines they complete, in order. A trailing line
// with no newline yet is held in the returned cursor, not returned, so a
// line Arena is in the middle of writing is never handed on cut short.
// Lines come without their line ending ("\n" or Arena's "\r\n" on Windows).
func (c Cursor) Consume(chunk []byte) (Cursor, []string) {
	next := Cursor{Offset: c.Offset + int64(len(chunk)), skipping: c.skipping}
	pending := c.partial
	var lines []string
	rest := chunk
	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			break
		}
		seg := rest[:i]
		rest = rest[i+1:]
		if next.skipping || len(pending)+len(seg) > MaxLine {
			next.skipping = false
			pending = nil
			continue
		}
		var line string
		if len(pending) > 0 {
			line = string(pending) + string(seg)
			pending = nil
		} else {
			line = string(seg)
		}
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		lines = append(lines, line)
	}
	switch {
	case next.skipping:
	case len(pending)+len(rest) > MaxLine:
		next.skipping = true
	case len(pending)+len(rest) > 0:
		// A fresh slice: the caller reuses chunk, and the old cursor
		// keeps its own partial.
		next.partial = append(append(make([]byte, 0, len(pending)+len(rest)), pending...), rest...)
	}
	return next, lines
}

// Follower reads Arena's log as Arena writes it. Its callbacks run on the
// goroutine that called Run, in the order the log was read; any of them may
// be nil.
type Follower struct {
	// Every is how often to check the log; zero means FollowEvery.
	Every time.Duration
	// LostAfter is how long the log may be missing before Run gives up;
	// zero means LostAfter.
	LostAfter time.Duration

	// Lines receives each batch of complete lines, in log order.
	Lines func(lines []string)
	// Restarted is called when the log started over (Arena was relaunched),
	// before the new session's lines.
	Restarted func()
	// CaughtUp is called once the log has been read to its end, the first
	// time for this session: after the first read, after a restart, and
	// after reading works again following an error.
	CaughtUp func()
	// Failed is called when the log is there but could not be read. Run
	// keeps trying on every check, and calls Failed again only if the
	// reason changes, so a log that stays unreadable does not redraw the
	// window five times a second.
	Failed func(error)
}

// Run follows the log at path: first the whole of the current session, from
// the start, so a draft already in progress is seen, and then whatever
// Arena appends, reading only the new bytes. It returns ErrLogGone once the
// log has been missing for LostAfter, or ctx.Err() when ctx ends.
func (f Follower) Run(ctx context.Context, path string) error {
	every := f.Every
	if every <= 0 {
		every = FollowEvery
	}
	t := newTail(f, path)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if t.poll(ctx, time.Now()) {
			return ErrLogGone
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// tail is one follower's state between checks.
type tail struct {
	f         Follower
	path      string
	lostAfter time.Duration

	cursor Cursor
	// file is the log as last opened, from the open file itself rather
	// than the path, so it names the file that was actually read. Nil
	// until the first read.
	file fs.FileInfo
	// missingSince is when the log was first seen missing, or zero.
	missingSince time.Time
	caughtUp     bool
	lastErr      string
}

func newTail(f Follower, path string) *tail {
	lost := f.LostAfter
	if lost <= 0 {
		lost = LostAfter
	}
	return &tail{f: f, path: path, lostAfter: lost}
}

// poll makes one check of the log at now, reading whatever is new, and
// reports whether the log has been missing long enough to count as gone.
// When nothing changed it costs one stat.
func (t *tail) poll(ctx context.Context, now time.Time) (gone bool) {
	info, err := os.Stat(t.path)
	if errors.Is(err, fs.ErrNotExist) {
		if t.missingSince.IsZero() {
			t.missingSince = now
		}
		return now.Sub(t.missingSince) >= t.lostAfter
	}
	t.missingSince = time.Time{}
	if err != nil {
		t.fail(err)
		return false
	}
	if t.file != nil && t.caughtUp && info.Size() == t.cursor.Offset && os.SameFile(t.file, info) {
		return false
	}
	if err := t.read(ctx); err != nil {
		t.fail(err)
	}
	return false
}

// read opens the log, starts over if it was replaced or truncated, and
// reads from the cursor to the end the file had when opened. The file is
// closed again before read returns: an open handle on Windows would stop
// Arena moving its log aside at its next launch.
func (t *tail) read(ctx context.Context) error {
	fh, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer func() { _ = fh.Close() }()
	info, err := fh.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return &fs.PathError{Op: "read", Path: t.path, Err: errors.New("it is not a regular file")}
	}
	if t.file != nil && t.cursor.Restarted(info.Size(), os.SameFile(t.file, info)) {
		t.cursor = Cursor{}
		t.caughtUp = false
		if t.f.Restarted != nil {
			t.f.Restarted()
		}
	}
	t.file = info

	end := info.Size()
	for t.cursor.Offset < end {
		if ctx.Err() != nil {
			return nil
		}
		buf := make([]byte, min(end-t.cursor.Offset, readChunk))
		n, err := fh.ReadAt(buf, t.cursor.Offset)
		var lines []string
		t.cursor, lines = t.cursor.Consume(buf[:n])
		if len(lines) > 0 && t.f.Lines != nil {
			t.f.Lines(lines)
		}
		if errors.Is(err, io.EOF) {
			// Shorter than when it was opened: truncated as it was
			// read. The next check sees the size and starts over.
			return nil
		}
		if err != nil {
			return err
		}
	}
	t.lastErr = ""
	if !t.caughtUp {
		t.caughtUp = true
		if t.f.CaughtUp != nil {
			t.f.CaughtUp()
		}
	}
	return nil
}

// fail reports a read error, once per distinct reason, and makes the next
// successful read report that the log is caught up again.
func (t *tail) fail(err error) {
	t.caughtUp = false
	if msg := err.Error(); msg != t.lastErr {
		t.lastErr = msg
		if t.f.Failed != nil {
			t.f.Failed(err)
		}
	}
}

// Exactly is the one place a log named with -log can be: the file itself,
// shown as the folder it is in. It is what the search watches when that
// log goes missing, rather than Arena's usual places, which the user has
// already said are not where their log is.
func Exactly(goos, home, path string) Candidate {
	return Candidate{Where: "-log: " + tilde(goos, home, filepath.Dir(path)), Pattern: escape(goos, path)}
}
