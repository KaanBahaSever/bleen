//go:build darwin

package platform

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func fsName(st *unix.Statfs_t) string {
	b := unsafe.Slice((*byte)(unsafe.Pointer(&st.Fstypename[0])), len(st.Fstypename))
	return cstr(b)
}
