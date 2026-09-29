//go:build windows

package source

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// openShared opens with FILE_SHARE_READ|WRITE|DELETE so colleagues working
// on the share are never blocked while bleen reads.
func openShared(p string) (io.ReadCloser, error) {
	name, err := windows.UTF16PtrFromString(longPath(p))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: p, Err: err}
	}
	return os.NewFile(uintptr(h), p), nil
}

// longPath converts an absolute path to its extended-length form so paths
// beyond 260 characters work (os does this internally; CreateFile does not).
func longPath(p string) string {
	switch {
	case len(p) >= 4 && p[:4] == `\\?\`:
		return p
	case len(p) >= 2 && p[:2] == `\\`:
		return `\\?\UNC\` + p[2:]
	case len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/'):
		return `\\?\` + p
	}
	return p
}

func isSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

const (
	fileAttributeOffline            = 0x00001000
	fileAttributeRecallOnOpen       = 0x00040000
	fileAttributeRecallOnDataAccess = 0x00400000
)

func isCloudPlaceholder(fi fs.FileInfo) bool {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	return d.FileAttributes&(fileAttributeOffline|fileAttributeRecallOnOpen|fileAttributeRecallOnDataAccess) != 0
}
