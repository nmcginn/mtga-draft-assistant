package images

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// picture is a small, real JPEG, distinct per seed so files differ.
func picture(t *testing.T, seed uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 49, 68))
	for x := range 49 {
		for y := range 68 {
			img.Set(x, y, color.RGBA{seed, uint8(x * 5), uint8(y * 3), 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// clock is a time that only moves when told to.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }
func (c *clock) tick()          { c.at = c.at.Add(time.Minute) }

func newClock() *clock { return &clock{at: time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)} }

func addr(n string) string {
	return "https://cards.scryfall.io/normal/front/a/b/" + n + ".jpg?1759190400"
}

func openCache(t *testing.T, dir string, maxBytes int64, c *clock) *Cache {
	t.Helper()
	cache, err := Open(dir, maxBytes, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func put(t *testing.T, c *Cache, u string, body []byte) (string, []string) {
	t.Helper()
	path, evicted, err := c.Put(u, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	return path, evicted
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestEvictionDeletesTheLeastRecentlyUsedFirst(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 17, 3, m, 0, 0, time.UTC) }
	entries := []Entry{
		{Name: "newest", Size: 10, Used: at(3)},
		{Name: "oldest", Size: 10, Used: at(0)},
		{Name: "middle", Size: 10, Used: at(2)},
		{Name: "older", Size: 10, Used: at(1)},
	}
	if got := Evict(entries, 20, nil); !slices.Equal(got, []string{"oldest", "older"}) {
		t.Errorf("Evict = %v, want the two least recently used, oldest first", got)
	}
}

func TestUnderTheCapNothingIsEvicted(t *testing.T) {
	entries := []Entry{{Name: "a", Size: 10}, {Name: "b", Size: 10}}
	if got := Evict(entries, 20, nil); got != nil {
		t.Errorf("Evict = %v at exactly the cap", got)
	}
	if got := Evict(nil, 0, nil); got != nil {
		t.Errorf("Evict of nothing = %v", got)
	}
}

func TestAPictureOnScreenIsNeverEvictedEvenOverTheCap(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 10, 17, 3, m, 0, 0, time.UTC) }
	entries := []Entry{
		{Name: "on-screen", Size: 30, Used: at(0)},
		{Name: "old", Size: 10, Used: at(1)},
		{Name: "new", Size: 10, Used: at(2)},
	}
	keep := func(n string) bool { return n == "on-screen" }
	if got := Evict(entries, 20, keep); !slices.Equal(got, []string{"old", "new"}) {
		t.Errorf("Evict = %v, want everything but the picture on screen", got)
	}
}

func TestAStoredPictureIsFoundAgainByItsAddress(t *testing.T) {
	c := openCache(t, t.TempDir(), 0, newClock())
	if _, ok := c.Lookup(addr("1")); ok {
		t.Fatal("a picture never stored was found")
	}
	body := picture(t, 1)
	path, _ := put(t, c, addr("1"), body)
	got, ok := c.Lookup(addr("1"))
	if !ok || got != path {
		t.Fatalf("Lookup = %q, %v, want %q", got, ok, path)
	}
	if b, err := os.ReadFile(got); err != nil || !bytes.Equal(b, body) {
		t.Errorf("the file holds %d bytes (%v), want the picture stored", len(b), err)
	}
	if filepath.Ext(got) != ".jpg" {
		t.Errorf("stored as %q; go-gui only shows files with an image extension", got)
	}
}

func TestAPictureIsNamedByItsWholeAddress(t *testing.T) {
	// Scryfall changes the query when it replaces a picture; the new one
	// must not be taken for the old.
	if Name(addr("1")) == Name(strings.Replace(addr("1"), "1759190400", "1760000000", 1)) {
		t.Error("two versions of a picture share a file")
	}
	if got := Name("https://cards.scryfall.io/png/front/a/b/1.png?1"); filepath.Ext(got) != ".png" {
		t.Errorf("a PNG is named %q", got)
	}
	if got := Name("::"); !fileName.MatchString(got) {
		t.Errorf("an address that does not parse is named %q", got)
	}
}

func TestStoringPastTheCapDeletesTheLeastRecentlyUsed(t *testing.T) {
	clk := newClock()
	one, two, three := picture(t, 1), picture(t, 2), picture(t, 3)
	maxBytes := int64(len(one) + len(two) + len(three) - 1) // room for two
	c := openCache(t, t.TempDir(), maxBytes, clk)

	p1, _ := put(t, c, addr("1"), one)
	clk.tick()
	p2, _ := put(t, c, addr("2"), two)
	clk.tick()
	if _, ok := c.Lookup(addr("1")); !ok { // 1 is now the more recently used
		t.Fatal("lost picture 1")
	}
	clk.tick()
	p3, evicted := put(t, c, addr("3"), three)

	if !slices.Equal(evicted, []string{Name(addr("2"))}) {
		t.Errorf("evicted %v, want picture 2, the least recently used", evicted)
	}
	if exists(p2) || !exists(p1) || !exists(p3) {
		t.Errorf("on disk: 1 %v, 2 %v, 3 %v; want 1 and 3", exists(p1), exists(p2), exists(p3))
	}
	if c.Size() > maxBytes {
		t.Errorf("the cache holds %d bytes, over its cap of %d", c.Size(), maxBytes)
	}
}

func TestPicturesOnScreenSurviveEviction(t *testing.T) {
	clk := newClock()
	one, two := picture(t, 1), picture(t, 2)
	c := openCache(t, t.TempDir(), int64(len(one)), clk) // room for one
	p1, _ := put(t, c, addr("1"), one)
	clk.tick()
	onScreen := func(n string) bool { return n == Name(addr("1")) }
	p2, evicted, err := c.Put(addr("2"), two, onScreen)
	if err != nil {
		t.Fatal(err)
	}
	if len(evicted) != 0 || !exists(p1) || !exists(p2) {
		t.Errorf("evicted %v; want both kept, since both are on screen", evicted)
	}
}

func TestThePictureJustStoredIsNotDeletedToMakeRoomForItself(t *testing.T) {
	body := picture(t, 1)
	c := openCache(t, t.TempDir(), int64(len(body)/2), newClock())
	path, evicted := put(t, c, addr("1"), body)
	if len(evicted) != 0 || !exists(path) {
		t.Errorf("a picture larger than the cap was deleted as it was stored (evicted %v)", evicted)
	}
}

func TestTheCapHoldsAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	clk := newClock()
	one, two, three := picture(t, 1), picture(t, 2), picture(t, 3)
	c := openCache(t, dir, 0, clk)
	p1, _ := put(t, c, addr("1"), one)
	clk.tick()
	p2, _ := put(t, c, addr("2"), two)
	clk.tick()
	p3, _ := put(t, c, addr("3"), three)
	clk.tick()
	if _, ok := c.Lookup(addr("1")); !ok { // used last, so kept longest
		t.Fatal("lost picture 1")
	}

	// The next launch has room for two: the order of use was kept on disk.
	maxBytes := int64(len(one) + len(three))
	c = openCache(t, dir, maxBytes, clk)
	if exists(p2) || !exists(p1) || !exists(p3) {
		t.Errorf("after reopening: 1 %v, 2 %v, 3 %v; want 2, the least recently used, gone", exists(p1), exists(p2), exists(p3))
	}
	if c.Size() != maxBytes {
		t.Errorf("the reopened cache counts %d bytes, want %d", c.Size(), maxBytes)
	}
}

func TestACorruptFileIsReplaced(t *testing.T) {
	dir := t.TempDir()
	c := openCache(t, dir, 0, newClock())
	path := filepath.Join(dir, Name(addr("1")))
	good := picture(t, 1)
	for name, bad := range map[string][]byte{
		"not a picture": []byte("<html>Service Unavailable</html>"),
		"cut short":     good[:len(good)/2],
		"empty":         nil,
	} {
		if err := os.WriteFile(path, bad, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := c.Lookup(addr("1")); ok {
			t.Errorf("%s: a corrupt file was handed out", name)
		}
		if exists(path) {
			t.Errorf("%s: the corrupt file was left on disk", name)
		}
	}
	if _, _, err := c.Put(addr("1"), good, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok := c.Lookup(addr("1")); !ok || got != path {
		t.Errorf("after downloading it again, Lookup = %q, %v", got, ok)
	}
}

func TestADownloadThatIsNotAPictureIsNotStored(t *testing.T) {
	dir := t.TempDir()
	c := openCache(t, dir, 0, newClock())
	good := picture(t, 1)
	for name, bad := range map[string][]byte{
		"an error page": []byte("<html>Service Unavailable</html>"),
		"cut short":     good[:len(good)-40],
		"empty":         nil,
	} {
		if _, _, err := c.Put(addr("1"), bad, nil); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s: Put = %v, want ErrUnreadable", name, err)
		}
	}
	list, _ := os.ReadDir(dir)
	if len(list) != 0 {
		t.Errorf("left %d files in the folder", len(list))
	}
}

func TestWhatACrashLeftHalfWrittenIsCleanedUpOnOpening(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, "123456.tmp")
	theirs := filepath.Join(dir, "README.txt")
	for _, p := range []string{stray, theirs} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := openCache(t, dir, 1, newClock())
	if exists(stray) {
		t.Error("a half-written picture survived opening")
	}
	if !exists(theirs) || c.Size() != 0 {
		t.Errorf("a file that is not a picture was counted or deleted (size %d)", c.Size())
	}
}

func TestAnImageFolderThatCannotBeCreatedIsAnError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, DirName), 0, nil); err == nil {
		t.Error("opened an image folder inside a file")
	}
}

func TestAnImageFolderThatCannotBeWrittenIsAnErrorOnOpening(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to any folder")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := Open(dir, 0, nil); err == nil {
		t.Error("opened a folder that cannot be written")
	}
}
