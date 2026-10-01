//go:build !windows

package vault

import (
	"os"
	"syscall"
)

func hideDir(string) {} // the leading dot already hides it

func syncDir(p string) {
	if d, err := os.Open(p); err == nil {
		d.Sync()
		d.Close()
	}
}

// processAlive reports whether a process with this PID is running.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
