//go:build darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const launchLabel = "app.bleen.backup"

func SchedulingSupported() bool { return true }

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist")
}

// InstallDailyTask registers a per-user LaunchAgent. launchd runs a missed
// calendar job when the Mac wakes up.
func InstallDailyTask(exe string, args []string, at string) error {
	var h, m int
	if _, err := fmt.Sscanf(at, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return fmt.Errorf("invalid time %q", at)
	}
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	var argv strings.Builder
	for _, a := range append([]string{exe}, args...) {
		fmt.Fprintf(&argv, "    <string>%s</string>\n", esc(a))
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>StartCalendarInterval</key>
  <dict><key>Hour</key><integer>%d</integer><key>Minute</key><integer>%d</integer></dict>
</dict>
</plist>
`, launchLabel, argv.String(), h, m)
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	RemoveDailyTask()
	if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
		return err
	}
	return run("launchctl", "bootstrap", "gui/"+strconv.Itoa(os.Getuid()), p)
}

func RemoveDailyTask() error {
	if !DailyTaskInstalled() {
		return nil
	}
	run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchLabel)
	return os.Remove(plistPath())
}

func DailyTaskInstalled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
