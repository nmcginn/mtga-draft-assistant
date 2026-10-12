package arena

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// env is a fake environment.
func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

var noEnv = env(nil)

func patterns(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Pattern
	}
	return out
}

func wheres(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Where
	}
	return out
}

const inPrefix = "/drive_c/users/*/AppData/LocalLow/Wizards Of The Coast/MTGA/Player.log"

func TestOnMacOSTheLogIsUnderLibraryLogs(t *testing.T) {
	got := Candidates("darwin", "/Users/sam", noEnv)
	want := []Candidate{{
		Where:   "~/Library/Logs/Wizards Of The Coast/MTGA",
		Pattern: "/Users/sam/Library/Logs/Wizards Of The Coast/MTGA/Player.log",
	}}
	if !slices.Equal(got, want) {
		t.Errorf("darwin candidates = %+v, want %+v", got, want)
	}
}

func TestOnWindowsTheLogIsUnderTheProfilesLocalLow(t *testing.T) {
	got := Candidates("windows", `C:\Users\home`, env(map[string]string{"USERPROFILE": `D:\Profiles\sam`}))
	want := []Candidate{{
		Where:   `D:\Profiles\sam\AppData\LocalLow\Wizards Of The Coast\MTGA`,
		Pattern: `D:\Profiles\sam\AppData\LocalLow\Wizards Of The Coast\MTGA\Player.log`,
	}}
	if !slices.Equal(got, want) {
		t.Errorf("windows candidates = %+v, want %+v", got, want)
	}
}

func TestOnWindowsWithoutUserProfileTheHomeDirectoryIsUsed(t *testing.T) {
	got := patterns(Candidates("windows", `C:\Users\sam`, noEnv))
	want := []string{`C:\Users\sam\AppData\LocalLow\Wizards Of The Coast\MTGA\Player.log`}
	if !slices.Equal(got, want) {
		t.Errorf("windows candidates = %q, want %q", got, want)
	}
}

func TestOnLinuxEveryCommonPrefixIsSearchedInOrder(t *testing.T) {
	got := Candidates("linux", "/home/sam", noEnv)
	want := []Candidate{
		{"Wine: ~/.wine", "/home/sam/.wine" + inPrefix},
		{"Lutris: ~/Games/*", "/home/sam/Games/*" + inPrefix},
		{"Bottles: ~/.local/share/bottles/bottles/*", "/home/sam/.local/share/bottles/bottles/*" + inPrefix},
		{"Bottles (Flatpak): ~/.var/app/com.usebottles.bottles/data/bottles/bottles/*", "/home/sam/.var/app/com.usebottles.bottles/data/bottles/bottles/*" + inPrefix},
		{"Steam: ~/.steam/steam/steamapps/compatdata/2141910/pfx", "/home/sam/.steam/steam/steamapps/compatdata/2141910/pfx" + inPrefix},
		{"Steam: ~/.local/share/Steam/steamapps/compatdata/2141910/pfx", "/home/sam/.local/share/Steam/steamapps/compatdata/2141910/pfx" + inPrefix},
		{"Steam (Flatpak): ~/.var/app/com.valvesoftware.Steam/.local/share/Steam/steamapps/compatdata/2141910/pfx", "/home/sam/.var/app/com.valvesoftware.Steam/.local/share/Steam/steamapps/compatdata/2141910/pfx" + inPrefix},
	}
	if !slices.Equal(got, want) {
		t.Errorf("linux candidates:\n got %+v\nwant %+v", got, want)
	}
}

func TestOnLinuxAnExplicitWinePrefixIsSearchedFirst(t *testing.T) {
	got := Candidates("linux", "/home/sam", env(map[string]string{"WINEPREFIX": "/mnt/games/arena"}))
	if got[0].Pattern != "/mnt/games/arena"+inPrefix || got[0].Where != "Wine: /mnt/games/arena" {
		t.Errorf("first candidate = %+v, want the WINEPREFIX", got[0])
	}
	if !slices.Contains(patterns(got), "/home/sam/.wine"+inPrefix) {
		t.Error("the default prefix was dropped when WINEPREFIX was set")
	}
}

func TestAWinePrefixThatIsTheDefaultIsSearchedOnce(t *testing.T) {
	got := patterns(Candidates("linux", "/home/sam", env(map[string]string{"WINEPREFIX": "/home/sam/.wine"})))
	n := 0
	for _, p := range got {
		if p == "/home/sam/.wine"+inPrefix {
			n++
		}
	}
	if n != 1 {
		t.Errorf("~/.wine appears %d times in %q", n, got)
	}
}

func TestOnLinuxXDGDataHomeMovesBottlesAndSteam(t *testing.T) {
	got := patterns(Candidates("linux", "/home/sam", env(map[string]string{"XDG_DATA_HOME": "/data/sam"})))
	for _, want := range []string{
		"/data/sam/bottles/bottles/*" + inPrefix,
		"/data/sam/Steam/steamapps/compatdata/2141910/pfx" + inPrefix,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("candidates %q are missing %q", got, want)
		}
	}
	if slices.Contains(got, "/home/sam/.local/share/bottles/bottles/*"+inPrefix) {
		t.Error("the default data home was searched despite XDG_DATA_HOME")
	}
}

func TestRelativeEnvironmentPathsAreIgnored(t *testing.T) {
	// The XDG spec says to ignore a relative XDG_DATA_HOME; a relative
	// WINEPREFIX would resolve against wherever the app was launched from.
	withRelative := Candidates("linux", "/home/sam", env(map[string]string{"WINEPREFIX": "arena", "XDG_DATA_HOME": "data"}))
	if !slices.Equal(withRelative, Candidates("linux", "/home/sam", noEnv)) {
		t.Errorf("relative env values changed the candidates: %+v", withRelative)
	}
}

func TestWithNoHomeDirectoryOnlyAbsoluteEnvironmentPathsAreSearched(t *testing.T) {
	if got := Candidates("darwin", "", noEnv); len(got) != 0 {
		t.Errorf("darwin with no home: %+v, want none", got)
	}
	if got := Candidates("windows", "", noEnv); len(got) != 0 {
		t.Errorf("windows with no home or profile: %+v, want none", got)
	}
	if got := Candidates("linux", "", noEnv); len(got) != 0 {
		t.Errorf("linux with no home: %+v, want none", got)
	}
	got := patterns(Candidates("linux", "", env(map[string]string{"WINEPREFIX": "/w", "XDG_DATA_HOME": "/d"})))
	want := []string{
		"/w" + inPrefix,
		"/d/bottles/bottles/*" + inPrefix,
		"/d/Steam/steamapps/compatdata/2141910/pfx" + inPrefix,
	}
	if !slices.Equal(got, want) {
		t.Errorf("linux with no home = %q, want %q", got, want)
	}
}

func TestARelativeHomeIsTreatedAsNoHome(t *testing.T) {
	if got := Candidates("darwin", "Users/sam", noEnv); len(got) != 0 {
		t.Errorf("relative home: %+v, want none", got)
	}
}

func TestAnUnknownPlatformHasNowhereToLook(t *testing.T) {
	if got := Candidates("plan9", "/usr/sam", noEnv); len(got) != 0 {
		t.Errorf("plan9 candidates = %+v, want none", got)
	}
}

func TestPlacesAreShownWithTheHomeDirectoryAsTilde(t *testing.T) {
	for _, w := range wheres(Candidates("linux", "/home/sam/", noEnv)) {
		if strings.Contains(w, "/home/sam") {
			t.Errorf("place %q shows the home directory in full", w)
		}
	}
}

// writeLog creates a Player.log at path, with modification time at.
func writeLog(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[UnityCrossThreadLogger]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

const logBelowPrefix = "drive_c/users/sam/AppData/LocalLow/Wizards Of The Coast/MTGA/Player.log"

func TestLookFindsNothingInAnEmptyHome(t *testing.T) {
	if got := Look(Candidates("linux", t.TempDir(), noEnv)); got != "" {
		t.Errorf("Look in an empty home = %q, want nothing", got)
	}
}

func TestLookFindsALogInsideALutrisPrefixWhateverItIsCalled(t *testing.T) {
	home := t.TempDir()
	want := filepath.Join(home, "Games", "mtg-arena", logBelowPrefix)
	writeLog(t, want, time.Now())
	if got := Look(Candidates("linux", home, noEnv)); got != want {
		t.Errorf("Look = %q, want %q", got, want)
	}
}

func TestLookFindsAProtonLogUnderSteamuser(t *testing.T) {
	home := t.TempDir()
	want := filepath.Join(home, ".local/share/Steam/steamapps/compatdata/2141910/pfx",
		"drive_c/users/steamuser/AppData/LocalLow/Wizards Of The Coast/MTGA/Player.log")
	writeLog(t, want, time.Now())
	if got := Look(Candidates("linux", home, noEnv)); got != want {
		t.Errorf("Look = %q, want %q", got, want)
	}
}

func TestWhenArenaIsInstalledTwiceTheNewestLogWins(t *testing.T) {
	// Someone who moved from Wine to Steam still has the old prefix; the log
	// Arena wrote to last is the one in use.
	home := t.TempDir()
	old := filepath.Join(home, ".wine", logBelowPrefix)
	current := filepath.Join(home, ".steam/steam/steamapps/compatdata/2141910/pfx", logBelowPrefix)
	writeLog(t, old, time.Now().Add(-48*time.Hour))
	writeLog(t, current, time.Now())
	if got := Look(Candidates("linux", home, noEnv)); got != current {
		t.Errorf("Look = %q, want the newer %q", got, current)
	}
}

func TestAFolderNamedPlayerLogIsNotALog(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".wine", logBelowPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Look(Candidates("linux", home, noEnv)); got != "" {
		t.Errorf("Look = %q, want a directory skipped", got)
	}
}

func TestAHomeWithWildcardCharactersMatchesOnlyItself(t *testing.T) {
	// A '*' or '[' in the home directory is part of a name, not a pattern:
	// a sibling directory it would match as a glob must not be searched.
	root := t.TempDir()
	home := filepath.Join(root, "s*m [x]")
	sibling := filepath.Join(root, "sam x")
	writeLog(t, filepath.Join(sibling, ".wine", logBelowPrefix), time.Now())
	if got := Look(Candidates("linux", home, noEnv)); got != "" {
		t.Errorf("Look = %q, a sibling of the home directory matched", got)
	}
	want := filepath.Join(home, ".wine", logBelowPrefix)
	writeLog(t, want, time.Now())
	if got := Look(Candidates("linux", home, noEnv)); got != want {
		t.Errorf("Look = %q, want %q", got, want)
	}
}

func TestOnMacOSLookFindsTheLog(t *testing.T) {
	home := t.TempDir()
	want := filepath.Join(home, "Library/Logs/Wizards Of The Coast/MTGA/Player.log")
	writeLog(t, want, time.Now())
	if got := Look(Candidates("darwin", home, noEnv)); got != want {
		t.Errorf("Look = %q, want %q", got, want)
	}
}

// watch runs Watch in the background and collects its reports.
func watch(t *testing.T, ctx context.Context, cands []Candidate) (<-chan Search, <-chan struct{}) {
	t.Helper()
	reports := make(chan Search, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Watch(ctx, cands, time.Millisecond, func(s Search) { reports <- s })
	}()
	return reports, done
}

func next(t *testing.T, reports <-chan Search) Search {
	t.Helper()
	select {
	case s := <-reports:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no report from Watch within 5s")
		return Search{}
	}
}

func TestALogAlreadyThereIsReportedAtOnceAndTheSearchEnds(t *testing.T) {
	home := t.TempDir()
	want := filepath.Join(home, ".wine", logBelowPrefix)
	writeLog(t, want, time.Now())
	reports, done := watch(t, t.Context(), Candidates("linux", home, noEnv))
	if got := next(t, reports); got.Found != want {
		t.Errorf("first report = %+v, want found %q", got, want)
	}
	<-done
}

func TestArenaLaunchedAfterTheAssistantIsFoundOnALaterPass(t *testing.T) {
	home := t.TempDir()
	cands := Candidates("linux", home, noEnv)
	reports, done := watch(t, t.Context(), cands)

	first := next(t, reports)
	if first.Found != "" || !slices.Equal(first.Looked, cands) {
		t.Fatalf("first report = %+v, want nothing found and every candidate looked in", first)
	}

	want := filepath.Join(home, "Games", "magic-the-gathering-arena", logBelowPrefix)
	writeLog(t, want, time.Now())
	// Only two reports ever: the empty passes between are silent, so the
	// window does not redraw every few seconds while it waits.
	if got := next(t, reports); got.Found != want {
		t.Errorf("second report = %+v, want found %q", got, want)
	}
	<-done
}

func TestTheSearchStopsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reports, done := watch(t, ctx, Candidates("linux", t.TempDir(), noEnv))
	next(t, reports)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return after its context was cancelled")
	}
	if len(reports) != 0 {
		t.Errorf("Watch reported again while waiting: %+v", <-reports)
	}
}

func TestWithNowhereToLookTheSearchSaysSoOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	reports, done := watch(t, ctx, nil)
	if got := next(t, reports); got.Found != "" || len(got.Looked) != 0 {
		t.Errorf("report = %+v, want nothing found and nowhere looked", got)
	}
	cancel()
	<-done
}

func TestTheLogFlagAcceptsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	writeLog(t, path, time.Now())
	got, err := ResolveFlag(path)
	if err != nil || got != path {
		t.Errorf("ResolveFlag(%q) = %q, %v; want the same path", path, got, err)
	}
}

func TestTheLogFlagMakesARelativePathAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, filepath.Join(dir, "Player.log"), time.Now())
	t.Chdir(dir)
	got, err := ResolveFlag("Player.log")
	if err != nil || !filepath.IsAbs(got) || filepath.Base(got) != "Player.log" {
		t.Errorf("ResolveFlag(relative) = %q, %v; want an absolute path to the log", got, err)
	}
}

func TestTheLogFlagAcceptsTheFolderHoldingTheLog(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "Player.log")
	writeLog(t, want, time.Now())
	if got, err := ResolveFlag(dir); err != nil || got != want {
		t.Errorf("ResolveFlag(folder) = %q, %v; want %q", got, err, want)
	}
}

func TestAMissingLogFlagPathIsAnErrorNamingItAndTheWayOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "Player.log")
	_, err := ResolveFlag(path)
	if err == nil {
		t.Fatal("a path that does not exist was accepted")
	}
	for _, want := range []string{path, "no file there", "leave out -log"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestAFolderWithNoLogInItIsAnError(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveFlag(dir)
	if err == nil || !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "Player.log") {
		t.Errorf("ResolveFlag(empty folder) error = %v, want one naming the folder and Player.log", err)
	}
}

func TestEscapeLeavesOrdinaryPathsAlone(t *testing.T) {
	for goos, p := range map[string]string{"linux": "/home/sam", "windows": `C:\Users\sam`} {
		if got := escape(goos, p); got != p {
			t.Errorf("escape(%s, %q) = %q", goos, p, got)
		}
	}
	if got, err := filepath.Match(escape("linux", `/a\b*[c]?`), `/a\b*[c]?`); err != nil || !got {
		t.Errorf("an escaped path does not match itself: %v, %v", got, err)
	}
}

func TestAnUnreadablePrefixIsSkippedAndTheRestStillSearched(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory, so there is nothing unreadable to test")
	}
	home := t.TempDir()
	locked := filepath.Join(home, "Games", "locked")
	writeLog(t, filepath.Join(locked, logBelowPrefix), time.Now())
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	want := filepath.Join(home, ".wine", logBelowPrefix)
	writeLog(t, want, time.Now().Add(-time.Hour))
	if got := Look(Candidates("linux", home, noEnv)); got != want {
		t.Errorf("Look = %q, want %q past the unreadable prefix", got, want)
	}
}
