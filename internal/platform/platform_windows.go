//go:build windows

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/sys/windows"
)

// FSType returns the filesystem name of the volume holding dir ("NTFS", "exFAT", "FAT32", …).
func FSType(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	p, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return "", err
	}
	var root [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumePathName(p, &root[0], uint32(len(root))); err != nil {
		return "", err
	}
	var fsName [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformation(&root[0], nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(fsName[:]), nil
}

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// KeepAwake prevents idle sleep until release is called. The execution
// state is per thread, so it lives on a dedicated locked OS thread.
func KeepAwake() (release func()) {
	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		setThreadExecutionState.Call(esContinuous | esSystemRequired)
		close(started)
		<-done
		setThreadExecutionState.Call(esContinuous)
	}()
	<-started
	return func() { close(done) }
}

// FreeSpace returns the bytes available to the current user on dir's volume.
func FreeSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	err = windows.GetDiskFreeSpaceEx(p, &free, nil, nil)
	return free, err
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

// Volumes lists local drives, removable ones first. The system drive is left out.
func Volumes() []Volume {
	var out []Volume
	sys := strings.ToUpper(os.Getenv("SystemDrive"))
	mask, _ := windows.GetLogicalDrives()
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		if strings.HasPrefix(strings.ToUpper(root), sys) && sys != "" {
			continue
		}
		p, _ := windows.UTF16PtrFromString(root)
		t := windows.GetDriveType(p)
		if t != windows.DRIVE_REMOVABLE && t != windows.DRIVE_FIXED {
			continue
		}
		var label, fs [windows.MAX_PATH + 1]uint16
		if err := windows.GetVolumeInformation(p, &label[0], uint32(len(label)), nil, nil, nil, &fs[0], uint32(len(fs))); err != nil {
			continue // e.g. an empty card reader
		}
		var free, total uint64
		windows.GetDiskFreeSpaceEx(p, &free, &total, nil)
		out = append(out, Volume{Path: root, Label: windows.UTF16ToString(label[:]), FSType: windows.UTF16ToString(fs[:]),
			Removable: t == windows.DRIVE_REMOVABLE, Free: free, Total: total})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Removable && !out[j].Removable })
	return out
}

// VolumeLabel returns the label of the drive holding dir ("Blue SanDisk").
func VolumeLabel(dir string) string {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return ""
	}
	var root [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumePathName(p, &root[0], uint32(len(root))); err != nil {
		return ""
	}
	var label [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformation(&root[0], &label[0], uint32(len(label)), nil, nil, nil, nil, 0); err != nil {
		return ""
	}
	return windows.UTF16ToString(label[:])
}

// Reveal opens a folder in Explorer.
func Reveal(path string) error { return exec.Command("explorer.exe", path).Start() }

// UILanguage returns the user's preferred UI language, e.g. "tr-TR".
func UILanguage() string {
	if l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(l) > 0 {
		return l[0]
	}
	return ""
}
