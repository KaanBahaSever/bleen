//go:build darwin || linux

package platform

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/sys/unix"
)

// FSType returns the filesystem name of the volume holding dir.
func FSType(dir string) (string, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return "", err
	}
	return fsName(&st), nil
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// KeepAwake is not implemented yet on this OS (planned: IOPMAssertion on
// macOS, logind inhibitor on Linux).
func KeepAwake() (release func()) { return func() {} }

// FreeSpace returns the bytes available to the current user on dir's volume.
func FreeSpace(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// Volume describes a mounted drive offered as a backup disk.
type Volume struct {
	Path      string `json:"path"`
	Label     string `json:"label"`
	FSType    string `json:"fsType"`
	Removable bool   `json:"removable"`
	Free      uint64 `json:"free"`
	Total     uint64 `json:"total"`
}

// Volumes is not implemented yet here; users pick a folder instead.
func Volumes() []Volume { return nil }

func VolumeLabel(dir string) string { return "" }

// Reveal opens a folder in the file manager.
func Reveal(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// UILanguage returns the user's preferred UI language, e.g. "tr_TR".
func UILanguage() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
