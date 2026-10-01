//go:build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const unitName = "bleen-backup"

func SchedulingSupported() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func unitDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "systemd", "user")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user")
}

// InstallDailyTask writes a systemd user service and timer. Persistent=true
// runs a missed backup after the computer was off.
func InstallDailyTask(exe string, args []string, at string) error {
	var h, m int
	if _, err := fmt.Sscanf(at, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return fmt.Errorf("invalid time %q", at)
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
	cmd := quote(exe)
	for _, a := range args {
		cmd += " " + quote(a)
	}
	service := fmt.Sprintf("[Unit]\nDescription=bleen daily backup (turn it off in bleen's settings)\n\n[Service]\nType=oneshot\nExecStart=%s\n", cmd)
	timer := fmt.Sprintf("[Unit]\nDescription=bleen daily backup\n\n[Timer]\nOnCalendar=*-*-* %02d:%02d:00\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n", h, m)
	d := unitDir()
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d, unitName+".service"), []byte(service), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d, unitName+".timer"), []byte(timer), 0o644); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	return systemctl("enable", "--now", unitName+".timer")
}

func RemoveDailyTask() error {
	if !DailyTaskInstalled() {
		return nil
	}
	systemctl("disable", "--now", unitName+".timer")
	os.Remove(filepath.Join(unitDir(), unitName+".timer"))
	os.Remove(filepath.Join(unitDir(), unitName+".service"))
	return systemctl("daemon-reload")
}

func DailyTaskInstalled() bool {
	_, err := os.Stat(filepath.Join(unitDir(), unitName+".timer"))
	return err == nil
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
