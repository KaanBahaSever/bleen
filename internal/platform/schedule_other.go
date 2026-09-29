//go:build !windows

package platform

import "errors"

// SchedulingSupported reports whether opt-in automation works on this OS.
// launchd (macOS) and systemd user timers (Linux) are planned.
func SchedulingSupported() bool { return false }

var errNoScheduler = errors.New("automatic backups are not available on this system yet")

func InstallDailyTask(exe string, args []string, at string) error { return errNoScheduler }
func RemoveDailyTask() error                                      { return nil }
func DailyTaskInstalled() bool                                    { return false }
