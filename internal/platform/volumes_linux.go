//go:build linux

package platform

import (
	"bufio"
	"os"
	"strings"
)

// Volumes lists removable-looking mounts (/media, /run/media, /mnt).
func Volumes() []Volume {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Volume
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 {
			continue
		}
		mount := unescapeMount(fields[4])
		if seen[mount] || !(strings.HasPrefix(mount, "/media/") || strings.HasPrefix(mount, "/run/media/") || strings.HasPrefix(mount, "/mnt/")) {
			continue
		}
		seen[mount] = true
		if v, ok := volumeAt(mount); ok {
			out = append(out, v)
		}
	}
	return out
}

// unescapeMount decodes the octal escapes (\040 for space) used in mountinfo.
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] >= '0' && s[i+1] <= '3' {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
