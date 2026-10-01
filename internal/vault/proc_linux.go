package vault

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// processStart returns when a process started, from /proc.
func processStart(pid int) (time.Time, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, false
	}
	// The command name (field 2) may contain spaces; fields after it are fixed.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return time.Time{}, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseInt(f[19], 10, 64) // field 22: start time in clock ticks after boot
	if err != nil {
		return time.Time{}, false
	}
	st, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, false
	}
	for _, line := range strings.Split(string(st), "\n") {
		if rest, ok := strings.CutPrefix(line, "btime "); ok {
			boot, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			if err != nil {
				return time.Time{}, false
			}
			const clkTck = 100 // USER_HZ on every Linux architecture Go supports
			return time.Unix(boot, 0).Add(time.Duration(ticks) * time.Second / clkTck), true
		}
	}
	return time.Time{}, false
}
