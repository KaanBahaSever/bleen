//go:build windows

package platform

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestTaskXML(t *testing.T) {
	x := TaskXML(`C:\Program Files\bleen & co\bleen.exe`, "--scheduled", 18, 5)
	// encoding/xml can't read the UTF-16 declaration; check the body.
	body := x[strings.Index(x, "<Task"):]
	var task struct {
		Triggers struct {
			Start string `xml:"CalendarTrigger>StartBoundary"`
		} `xml:"Triggers"`
		Settings struct {
			StartWhenAvailable bool `xml:"StartWhenAvailable"`
		} `xml:"Settings"`
		Exec struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Actions>Exec"`
	}
	if err := xml.Unmarshal([]byte(body), &task); err != nil {
		t.Fatal(err)
	}
	if task.Triggers.Start != "2026-01-01T18:05:00" || !task.Settings.StartWhenAvailable {
		t.Errorf("trigger/settings wrong: %+v", task)
	}
	if task.Exec.Command != `C:\Program Files\bleen & co\bleen.exe` || task.Exec.Arguments != "--scheduled" {
		t.Errorf("exec wrong: %+v", task.Exec)
	}
	if b := utf16le("A"); len(b) != 4 || b[0] != 0xFF || b[2] != 'A' {
		t.Errorf("utf16le: %v", b)
	}
}
