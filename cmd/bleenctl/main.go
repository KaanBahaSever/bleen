// Command bleenctl is the headless bleen CLI.
package main

import (
	"os"

	"github.com/kaanbahasever/bleen/internal/cli"
)

func main() { os.Exit(cli.Execute()) }
