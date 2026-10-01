package vault

import (
	"time"

	"golang.org/x/sys/unix"
)

// processStart returns when a process started, from sysctl.
func processStart(pid int) (time.Time, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return time.Time{}, false
	}
	t := kp.Proc.P_starttime
	return time.Unix(int64(t.Sec), int64(t.Usec)*1000), true
}
