//go:build windows

package vault

import (
	"time"

	"golang.org/x/sys/windows"
)

func hideDir(p string) {
	if ptr, err := windows.UTF16PtrFromString(p); err == nil {
		if attrs, err := windows.GetFileAttributes(ptr); err == nil {
			windows.SetFileAttributes(ptr, attrs|windows.FILE_ATTRIBUTE_HIDDEN)
		}
	}
}

// syncDir is a no-op: Windows cannot fsync directories; NTFS journals renames.
func syncDir(string) {}

// processAlive reports whether a process with this PID is running.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied means it exists; "invalid parameter" means it doesn't.
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return true
	}
	return code == 259 // STILL_ACTIVE
}

// processStart returns when a process started, if it can be read.
func processStart(pid int) (time.Time, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return time.Time{}, false
	}
	return time.Unix(0, created.Nanoseconds()), true
}
