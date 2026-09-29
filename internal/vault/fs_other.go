//go:build !windows

package vault

import "os"

func hideDir(string) {} // the leading dot already hides it

func syncDir(p string) {
	if d, err := os.Open(p); err == nil {
		d.Sync()
		d.Close()
	}
}
