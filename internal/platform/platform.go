// Package platform holds the few OS-specific hooks bleen needs. A scheduled
// task is installed only when the user turns on automatic backups.
package platform

import (
	"errors"
	"strings"
)

// FAT32MaxPart keeps archive parts under FAT32's 4 GiB file-size limit.
const FAT32MaxPart int64 = 3900 << 20

// MaxPartSize returns the archive part limit for a vault directory:
// FAT32 needs splitting, everything else does not.
func MaxPartSize(dir string) int64 {
	t, err := FSType(dir)
	if err != nil {
		return FAT32MaxPart // unknown: splitting is harmless, a 4 GB failure is not
	}
	if IsFAT32(t) {
		return FAT32MaxPart
	}
	return 0
}

func IsFAT32(fsType string) bool {
	switch strings.ToLower(fsType) {
	case "fat32", "fat", "vfat", "msdos":
		return true
	}
	return false
}

var errNotWindows = errors.New("only available on Windows")
