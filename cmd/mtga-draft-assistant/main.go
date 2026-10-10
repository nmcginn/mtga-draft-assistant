// Command mtga-draft-assistant is a thin shell: it parses flags and hands off
// to the library. Anything with behaviour worth testing lives under internal/.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nmcginn/mtga-draft-assistant/internal/app"
	"github.com/nmcginn/mtga-draft-assistant/internal/ui"
	"github.com/nmcginn/mtga-draft-assistant/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Name, version.Version)
		return
	}
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "%s takes no arguments; run with -h to see its flags\n", version.Name)
		os.Exit(2)
	}

	ui.Run(app.New())
}
