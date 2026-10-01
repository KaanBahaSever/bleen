//go:build dev

package ui

import "os"

// In `wails dev`, BLEEN_DEV_HIDDEN=1 keeps the native window hidden so the
// UI can be driven from a browser at the dev server (real Go backend).
var devHidden = os.Getenv("BLEEN_DEV_HIDDEN") == "1"

// A separate single-instance id, so a dev run never talks to an installed bleen.
const instanceID = "app.bleen.desktop.dev"
