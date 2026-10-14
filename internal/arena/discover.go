// Package arena knows where MTG Arena writes its log, and how to read drafts
// from its lines. It only ever reads Arena's files.
package arena

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SteamAppID is Arena's app id on Steam, which names its Proton prefix under
// steamapps/compatdata. Confirmed against Steam's store and community pages
// (store.steampowered.com/app/2141910).
const SteamAppID = "2141910"

// PollEvery is how often the search looks again while no log has been found.
// A pass is a handful of stats, so this costs nothing measurable, and a few
// seconds is well inside the time it takes Arena to start (decision 14).
const PollEvery = 3 * time.Second

// Candidate is one place Arena might have written Player.log.
type Candidate struct {
	// Where is the place as the waiting screen names it: what it is, and the
	// folder, with the home directory written as ~.
	Where string
	// Pattern is a filepath.Glob pattern for the log itself. The parts that
	// come from the home directory or the environment are escaped, so only
	// the wildcards put there on purpose match anything.
	Pattern string
}

// The Windows path below the user's profile, in the order Arena nests it.
var windowsLogDirs = []string{"AppData", "LocalLow", "Wizards Of The Coast", "MTGA"}

// Candidates lists where Arena's log may be, best guess first, for a machine
// running goos with the given home directory and environment. It touches no
// files: deciding which of these exist is Look's job.
//
// home may be empty (os.UserHomeDir failed); candidates under it are then
// left out rather than guessed at. Environment values that are not absolute
// paths are ignored, as the XDG base directory spec says to.
func Candidates(goos, home string, getenv func(string) string) []Candidate {
	env := func(key string) string {
		if v := getenv(key); isAbs(goos, v) {
			return v
		}
		return ""
	}
	if !isAbs(goos, home) {
		home = ""
	}

	var out []Candidate
	add := func(where, pattern string) {
		for _, c := range out {
			if c.Pattern == pattern {
				return
			}
		}
		out = append(out, Candidate{Where: where, Pattern: pattern})
	}

	switch goos {
	case "darwin":
		if home != "" {
			below := []string{"Library", "Logs", "Wizards Of The Coast", "MTGA"}
			add(tilde(goos, home, join(goos, append([]string{home}, below...)...)),
				join(goos, append(append([]string{escape(goos, home)}, below...), "Player.log")...))
		}

	case "windows":
		profile := env("USERPROFILE")
		if profile == "" {
			profile = home
		}
		if profile != "" {
			add(join(goos, append([]string{profile}, windowsLogDirs...)...),
				join(goos, append(append([]string{escape(goos, profile)}, windowsLogDirs...), "Player.log")...))
		}

	case "linux":
		// Inside a Wine prefix Arena writes the Windows path, under a user
		// named after the Linux user for plain Wine, Lutris and Bottles, and
		// "steamuser" for Proton. Matching any user covers all of them.
		prefix := func(where, root string, wild ...string) {
			shown := tilde(goos, home, root)
			if len(wild) > 0 {
				shown = join(goos, append([]string{shown}, wild...)...)
			}
			p := append([]string{escape(goos, root)}, wild...)
			p = append(p, "drive_c", "users", "*")
			p = append(p, windowsLogDirs...)
			add(where+": "+shown, join(goos, append(p, "Player.log")...))
		}
		dataHome := env("XDG_DATA_HOME")
		if dataHome == "" && home != "" {
			dataHome = join(goos, home, ".local", "share")
		}

		if wp := env("WINEPREFIX"); wp != "" {
			prefix("Wine", wp)
		}
		if home != "" {
			prefix("Wine", join(goos, home, ".wine"))
			// Lutris installs each game into its own prefix under ~/Games,
			// named after the game by default but renamable.
			prefix("Lutris", join(goos, home, "Games"), "*")
		}
		// Bottles names each bottle as the user chose.
		if dataHome != "" {
			prefix("Bottles", join(goos, dataHome, "bottles", "bottles"), "*")
		}
		if home != "" {
			prefix("Bottles (Flatpak)", join(goos, home, ".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles"), "*")
		}
		// Steam's root: ~/.steam/steam is usually a link to the second, but
		// not on every distribution, so both are tried.
		compat := []string{"steamapps", "compatdata", SteamAppID, "pfx"}
		var steamRoots [][2]string
		if home != "" {
			steamRoots = append(steamRoots, [2]string{"Steam", join(goos, home, ".steam", "steam")})
		}
		if dataHome != "" {
			steamRoots = append(steamRoots, [2]string{"Steam", join(goos, dataHome, "Steam")})
		}
		if home != "" {
			steamRoots = append(steamRoots, [2]string{"Steam (Flatpak)", join(goos, home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam")})
		}
		for _, r := range steamRoots {
			prefix(r[0], join(goos, append([]string{r[1]}, compat...)...))
		}
	}
	return out
}

// Look makes one pass over the candidates and returns the log Arena wrote to
// most recently, or "" if none exists. More than one can exist when Arena
// has been installed more than one way; the newest is the one in use.
func Look(cands []Candidate) string {
	var best string
	var bestTime time.Time
	for _, c := range cands {
		matches, err := filepath.Glob(c.Pattern)
		if err != nil {
			// Only a malformed pattern fails, and Candidates escapes
			// everything it did not mean as a wildcard.
			continue
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if best == "" || info.ModTime().After(bestTime) {
				best, bestTime = m, info.ModTime()
			}
		}
	}
	return best
}

// Search is what a search pass reports: the log it found, or, while there is
// none, everywhere it looked.
type Search struct {
	Found  string
	Looked []Candidate
}

// Watch looks for the log now and then every interval until it is found or
// ctx is done. It reports once when the first pass finds nothing, so the
// window can say where it looked, and once when the log turns up; the passes
// in between are silent, so a window waiting for Arena never redraws.
func Watch(ctx context.Context, cands []Candidate, every time.Duration, report func(Search)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	reported := false
	for {
		if found := Look(cands); found != "" {
			report(Search{Found: found})
			return
		}
		if !reported {
			report(Search{Looked: cands})
			reported = true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ResolveFlag checks the path given with -log before the window opens, and
// returns it absolute. A folder holding a Player.log is taken to mean that
// file. Anything else that is not a readable file is an error that names the
// path and says what to do, rather than a silent fall back to searching.
func ResolveFlag(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	info, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("-log %s: there is no file there. Check the path, or leave out -log to let the assistant look in Arena's usual places", abs)
	case err != nil:
		return "", fmt.Errorf("-log %s: it cannot be read (%w). Check the path and its permissions, or leave out -log", abs, err)
	case info.IsDir():
		inside := filepath.Join(abs, "Player.log")
		if in, err := os.Stat(inside); err == nil && in.Mode().IsRegular() {
			return inside, nil
		}
		return "", fmt.Errorf("-log %s: that is a folder with no Player.log in it. Pass the path to Arena's Player.log itself", abs)
	case !info.Mode().IsRegular():
		return "", fmt.Errorf("-log %s: that is not a regular file. Pass the path to Arena's Player.log", abs)
	}
	return abs, nil
}

// join and isAbs build and check paths for goos rather than for the machine
// running the code, so every platform's candidates can be tested anywhere.
func join(goos string, parts ...string) string {
	if goos == "windows" {
		return strings.Join(parts, `\`)
	}
	return strings.Join(parts, "/")
}

func isAbs(goos, p string) bool {
	if goos == "windows" {
		// C:\... or a UNC path \\server\share.
		return (len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')) || strings.HasPrefix(p, `\\`)
	}
	return strings.HasPrefix(p, "/")
}

// escape makes a literal path safe to embed in a glob pattern: a home
// directory with a '[' or '*' in it must not become a wildcard. Wrapping a
// character in brackets works on every platform; a backslash escape does
// not work on Windows, where it is the separator.
func escape(goos, literal string) string {
	var b strings.Builder
	for _, r := range literal {
		switch {
		case r == '*' || r == '?' || r == '[':
			b.WriteString("[" + string(r) + "]")
		case r == '\\' && goos != "windows":
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tilde writes p with the home directory shortened to ~, the way people read
// paths on macOS and Linux.
func tilde(goos, home, p string) string {
	if home == "" || goos == "windows" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, strings.TrimSuffix(home, "/")+"/"); ok {
		return "~/" + rest
	}
	return p
}
