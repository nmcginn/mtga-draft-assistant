// Package images is the on-disk store of card pictures (decision 12): a
// folder under the cache directory, capped in size, from which the least
// recently used pictures are deleted first. It hands out local paths only,
// so go-gui never downloads anything itself. It does nothing but read and
// write files; what to fetch, and when, is the image task's.
package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"

	// The decoders a card picture may need. go-gui's backends link both
	// already, so they cost no binary size here.
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// DirName is the folder inside the cache directory that holds the pictures.
const DirName = "images"

// DefaultCap is how much the pictures may take on disk: a few thousand
// cards, so several sets, at about 70 KB each.
const DefaultCap = 200 << 20

// maxPixels bounds a picture's size before it is decoded in full, so a file
// that claims to be enormous is turned away rather than allocated. A normal
// card is 488×680.
const maxPixels = 4096 * 4096

// Cache is the picture folder. It is safe to use from several goroutines.
type Cache struct {
	dir string
	cap int64
	now func() time.Time

	mu    sync.Mutex
	files map[string]Entry // by file name
}

// Entry is one picture on disk: its file name, how many bytes it takes,
// and when it was last used. The last use is kept as the file's
// modification time, so the order survives a restart.
type Entry struct {
	Name string
	Size int64
	Used time.Time
}

// ErrUnreadable is a picture that is not one: cut short, or not an image
// at all.
var ErrUnreadable = errors.New("not a picture the assistant can read")

// fileName is how a picture is named on disk: a hash of its address, which
// changes when Scryfall replaces the picture, and the address's extension.
var fileName = regexp.MustCompile(`^[0-9a-f]{32}\.(jpg|png)$`)

// Open opens the picture folder dir, creating it if need be. It checks that
// the folder can be written, deletes what a crash may have left half
// written, and brings the folder under maxBytes (DefaultCap when zero),
// so the cap holds across restarts and a lowered cap takes effect at once.
// now is the clock that dates each use; nil is time.Now.
func Open(dir string, maxBytes int64, now func() time.Time) (*Cache, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultCap
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating the image folder: %w", err)
	}
	// A folder that can be read but not written would otherwise fail on
	// every picture, one at a time.
	probe, err := os.CreateTemp(dir, "*.tmp")
	if err != nil {
		return nil, fmt.Errorf("writing to the image folder: %w", err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	list, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading the image folder: %w", err)
	}
	c := &Cache{dir: dir, cap: maxBytes, now: now, files: map[string]Entry{}}
	for _, de := range list {
		name := de.Name()
		switch {
		case strings.HasSuffix(name, ".tmp"):
			// A picture being written when the assistant stopped.
			_ = os.Remove(filepath.Join(dir, name))
		case fileName.MatchString(name) && de.Type().IsRegular():
			info, err := de.Info()
			if err != nil {
				continue
			}
			c.files[name] = Entry{Name: name, Size: info.Size(), Used: info.ModTime()}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictLocked(nil)
	return c, nil
}

// Name is the file name the picture at u is kept under.
func Name(u string) string {
	sum := sha256.Sum256([]byte(u))
	ext := ".jpg"
	if p, err := url.Parse(u); err == nil && strings.EqualFold(filepath.Ext(p.Path), ".png") {
		ext = ".png"
	}
	return hex.EncodeToString(sum[:16]) + ext
}

// Lookup returns the local path of the picture at u, and false when it is
// not on disk. A file that is there but is not a readable picture is
// deleted, and reported as not there, so it is downloaded again. A picture
// found counts as used now.
func (c *Cache) Lookup(u string) (string, bool) {
	name := Name(u)
	path := filepath.Join(c.dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		c.forget(name)
		return "", false
	}
	if check(b) != nil {
		_ = os.Remove(path)
		c.forget(name)
		return "", false
	}
	used := c.now()
	// Failing to date the use only makes this picture look older than it
	// is; it is still there to show.
	_ = os.Chtimes(path, used, used)
	c.mu.Lock()
	c.files[name] = Entry{Name: name, Size: int64(len(b)), Used: used}
	c.mu.Unlock()
	return path, true
}

// Put stores body as the picture at u and returns its local path, and the
// file names of the pictures deleted to keep the folder under its cap.
// keep says which pictures may not be deleted, whatever their age: the
// ones on screen. A body that is not a readable picture is ErrUnreadable,
// and nothing is written. The file appears whole or not at all.
func (c *Cache) Put(u string, body []byte, keep func(name string) bool) (path string, evicted []string, err error) {
	if err := check(body); err != nil {
		return "", nil, err
	}
	name := Name(u)
	path = filepath.Join(c.dir, name)
	// Deleting the cache folder is always safe (decision 16), even with
	// the assistant running: the next picture puts the folder back.
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return "", nil, fmt.Errorf("saving a card image: %w", err)
	}
	tmp, err := os.CreateTemp(c.dir, "*.tmp")
	if err != nil {
		return "", nil, fmt.Errorf("saving a card image: %w", err)
	}
	_, werr := tmp.Write(body)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, fmt.Errorf("saving a card image: %w", werr)
	}
	used := c.now()
	_ = os.Chtimes(path, used, used)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.files[name] = Entry{Name: name, Size: int64(len(body)), Used: used}
	pinned := func(n string) bool { return n == name || (keep != nil && keep(n)) }
	return path, c.evictLocked(pinned), nil
}

// Size is how many bytes the pictures take, as far as the cache knows.
func (c *Cache) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total int64
	for _, e := range c.files {
		total += e.Size
	}
	return total
}

func (c *Cache) forget(name string) {
	c.mu.Lock()
	delete(c.files, name)
	c.mu.Unlock()
}

// evictLocked deletes pictures until the folder is under its cap, and
// returns their names. c.mu is held.
func (c *Cache) evictLocked(keep func(string) bool) []string {
	entries := make([]Entry, 0, len(c.files))
	for _, e := range c.files {
		entries = append(entries, e)
	}
	names := Evict(entries, c.cap, keep)
	for _, n := range names {
		// One that cannot be deleted is forgotten all the same; it is
		// found again, with its size, the next time the folder is opened.
		_ = os.Remove(filepath.Join(c.dir, n))
		delete(c.files, n)
	}
	return names
}

// Evict is which of entries to delete, oldest use first, to bring their
// total size to maxBytes or under. An entry keep reports true for is never
// chosen, even if that leaves the total over: a picture on screen is worth
// more than the cap's last few megabytes. Pure, so the order is tested on
// its own.
func Evict(entries []Entry, maxBytes int64, keep func(string) bool) []string {
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	if total <= maxBytes {
		return nil
	}
	byAge := slices.Clone(entries)
	slices.SortFunc(byAge, func(a, b Entry) int {
		if c := a.Used.Compare(b.Used); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	var out []string
	for _, e := range byAge {
		if total <= maxBytes {
			break
		}
		if keep != nil && keep(e.Name) {
			continue
		}
		out = append(out, e.Name)
		total -= e.Size
	}
	return out
}

// check reports whether b is a whole picture: one that decodes to the end,
// at a size a card could be.
func check(b []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return ErrUnreadable
	}
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		return ErrUnreadable
	}
	return nil
}
