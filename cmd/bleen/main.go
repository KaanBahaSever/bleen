// Command bleen is the desktop app. It runs only while its window is open.
package main

import (
	"fmt"
	"os"

	"github.com/kaanbahasever/bleen/frontend"
	"github.com/kaanbahasever/bleen/internal/app"
	"github.com/kaanbahasever/bleen/internal/ui"
)

// version is set with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	app.Version = version
	// Started by the opt-in scheduled task: back up without a window, then exit.
	if len(os.Args) > 1 && os.Args[1] == "--scheduled" {
		if err := app.RunScheduled(); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := ui.Run(frontend.Assets()); err != nil {
		fmt.Fprintln(os.Stderr, "bleen:", err)
		os.Exit(1)
	}
}
