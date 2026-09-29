//go:build windows

package platform

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

// TaskName is the single Task Scheduler entry bleen may create.
const TaskName = `bleen\Daily backup`

// SchedulingSupported reports whether opt-in automation works on this OS.
func SchedulingSupported() bool { return true }

// InstallDailyTask registers (or replaces) a per-user task that starts
// exe with args every day at hh:mm. It runs only while the user is logged
// in, so network shares use their normal credentials, and it catches up
// after the computer was off at that time.
func InstallDailyTask(exe string, args []string, at string) error {
	var h, m int
	if _, err := fmt.Sscanf(at, "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return fmt.Errorf("invalid time %q", at)
	}
	f, err := os.CreateTemp("", "bleen-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(utf16le(TaskXML(exe, strings.Join(args, " "), h, m)))
	f.Close()
	if err != nil {
		return err
	}
	return schtasks("/Create", "/TN", TaskName, "/XML", f.Name(), "/F")
}

// RemoveDailyTask deletes the task; it is not an error if there is none.
func RemoveDailyTask() error {
	if !DailyTaskInstalled() {
		return nil
	}
	return schtasks("/Delete", "/TN", TaskName, "/F")
}

// DailyTaskInstalled reports whether the task exists.
func DailyTaskInstalled() bool { return schtasks("/Query", "/TN", TaskName) == nil }

func schtasks(args ...string) error {
	cmd := exec.Command("schtasks.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// TaskXML is the Task Scheduler definition bleen registers.
func TaskXML(exe, args string, h, m int) string {
	esc := func(s string) string {
		r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
		return r.Replace(s)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>bleen daily backup. Created because you turned it on in bleen's settings; turn it off there to remove it.</Description>
  </RegistrationInfo>
  <Triggers>
    <CalendarTrigger>
      <StartBoundary>2026-01-01T%02d:%02d:00</StartBoundary>
      <Enabled>true</Enabled>
      <ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>
    </CalendarTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT12H</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
    </Exec>
  </Actions>
</Task>
`, h, m, esc(exe), esc(args))
}

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2+2*len(u))
	b[0], b[1] = 0xFF, 0xFE
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2+2*i:], c)
	}
	return b
}
