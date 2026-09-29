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
