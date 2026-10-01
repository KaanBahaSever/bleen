//go:build windows

package vault

import "golang.org/x/sys/windows"

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
