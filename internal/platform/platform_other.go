//go:build darwin || linux

package platform

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

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

// KeepAwake prevents idle sleep until release is called, using the system's
// own tool: caffeinate on macOS, systemd-inhibit on Linux. If the tool is
// missing, backups still run; the computer may just sleep.
func KeepAwake() (release func()) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	default:
		cmd = exec.Command("systemd-inhibit", "--what=idle:sleep", "--who=bleen",
			"--why=Backup in progress", "--mode=block", "sleep", "infinity")
	}
	if err := cmd.Start(); err != nil {
		return func() {}
	}
	return func() {
		cmd.Process.Kill()
		cmd.Wait()
	}
}

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

func volumeAt(mount string) (Volume, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(mount, &st); err != nil {
		return Volume{}, false
	}
	return Volume{
		Path:      mount,
		Label:     filepath.Base(mount),
		FSType:    fsName(&st),
		Removable: true,
		Free:      uint64(st.Bavail) * uint64(st.Bsize),
		Total:     uint64(st.Blocks) * uint64(st.Bsize),
	}, true
}

// VolumeLabel returns a friendly name for the volume holding dir.
func VolumeLabel(dir string) string {
	for _, v := range Volumes() {
		if rel, err := filepath.Rel(v.Path, dir); err == nil && !filepath.IsAbs(rel) && rel != ".." && (len(rel) < 3 || rel[:3] != "../") {
			return v.Label
		}
	}
	return ""
}

// Reveal opens a folder in the file manager.
func Reveal(path string) error {
	isFile := false
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		isFile = true
	}
	if runtime.GOOS == "darwin" {
		if isFile {
			return exec.Command("open", "-R", path).Start()
		}
		return exec.Command("open", path).Start()
	}
	if isFile {
		path = filepath.Dir(path)
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

// SaveShareCredential is Windows-only; elsewhere shares are mounted by the
// system (Finder "Connect to Server", GNOME Files, /etc/fstab).
func SaveShareCredential(server, share, user, password string) error {
	return errNotWindows
}
