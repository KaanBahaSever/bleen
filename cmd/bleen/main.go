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
	if err := ui.Run(frontend.Assets()); err != nil {
		fmt.Fprintln(os.Stderr, "bleen:", err)
		os.Exit(1)
	}
}
