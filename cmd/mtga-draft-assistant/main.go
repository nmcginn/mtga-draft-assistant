// Command mtga-draft-assistant is a thin shell: it parses flags and hands off
// to the library. Anything with behaviour worth testing lives under internal/.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/startup"
	"github.com/nmcginn/mtga-draft-assistant/internal/ui"
	"github.com/nmcginn/mtga-draft-assistant/internal/version"
)

func main() {
	// First, so the startup report sees as much of startup as Go can.
	mainAt := time.Now()

	showVersion := flag.Bool("version", false, "print the version and exit")
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

	var opts ui.Options
	if *startupTime {
		timeline := startup.New(mainAt)
		opts.Startup = &timeline
		opts.OnFirstFrame = func(t startup.Timeline) { fmt.Fprintln(os.Stderr, t.Report()) }
	}
	ui.Run(app.New(), opts)
}
