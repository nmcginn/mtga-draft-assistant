// Command mtga-draft-assistant is a thin shell: it parses flags and hands off
// to the library. Anything with behaviour worth testing lives under internal/.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/background"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch"
	"github.com/nmcginn/mtga-draft-assistant/internal/fetch/scryfall"
	"github.com/nmcginn/mtga-draft-assistant/internal/startup"
	"github.com/nmcginn/mtga-draft-assistant/internal/ui"
	"github.com/nmcginn/mtga-draft-assistant/internal/version"
)

func main() {
	// First, so the startup report sees as much of startup as Go can.
	mainAt := time.Now()

	showVersion := flag.Bool("version", false, "print the version and exit")
	logPath := flag.String("log", "", "the path to Arena's Player.log, if the assistant does not find it on its own")
	startupTime := flag.Bool("startup-time", false, "print to stderr how long the first frame took to reach the screen")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Name, version.Version)
		return
	}
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "%s takes no arguments; run with -h to see its flags\n", version.Name)
		os.Exit(2)
	}

	// Everything the assistant writes lives here, and can be deleted at any
	// time (decision 16). Without one, cards are fetched each launch.
	cacheDir, cacheErr := os.UserCacheDir()
	if cacheErr == nil {
		cacheDir = filepath.Join(cacheDir, version.Name)
	}
	client := fetch.New(fetch.Options{
		UserAgent: version.UserAgent(),
		Limits:    map[string]time.Duration{scryfall.Host: scryfall.Interval, scryfall.ImageHost: scryfall.ImageInterval},
	})
	cards := background.NewCards(background.CardSetup{
		Dir: cacheDir, DirErr: cacheErr, Source: scryfall.Source{Client: client},
	})

	pictures := background.NewImages(background.ImageSetup{
		Dir: cacheDir, DirErr: cacheErr, Source: scryfall.Images{Client: client},
	})

	// A failed lookup leaves home empty, and the search skips the places
	// under it; the window then says where it did look.
	home, _ := os.UserHomeDir()
	model, start, err := background.FindLog(background.LogSetup{
		Flag: *logPath, GOOS: runtime.GOOS, Home: home, Getenv: os.Getenv, WantSet: cards.Want,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	opts := ui.Options{Start: background.All(cards.Start, pictures.Start, start), ShowImages: pictures.Want}
	if *startupTime {
		timeline := startup.New(mainAt)
		opts.Startup = &timeline
		opts.OnFirstFrame = func(t startup.Timeline) { fmt.Fprintln(os.Stderr, t.Report()) }
	}
	ui.Run(model, opts)
}
