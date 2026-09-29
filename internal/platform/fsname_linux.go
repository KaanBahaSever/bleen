//go:build linux

package platform

import "golang.org/x/sys/unix"

func fsName(st *unix.Statfs_t) string {
	switch st.Type {
	case unix.MSDOS_SUPER_MAGIC:
		return "vfat"
	case unix.EXFAT_SUPER_MAGIC:
		return "exfat"
	case unix.EXT4_SUPER_MAGIC:
		return "ext4"
	case unix.BTRFS_SUPER_MAGIC:
		return "btrfs"
	case unix.XFS_SUPER_MAGIC:
		return "xfs"
	}
	return "unknown"
}
