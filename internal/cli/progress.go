package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/engine"
)

type archiveIssue = archive.Issue

// printer renders engine progress on one terminal line, at most 5 times a second.
type printer struct {
	last  time.Time
	width int
	quiet bool // stdout is not a terminal: no live line
}

func newPrinter() *printer { return &printer{quiet: !isTerminal(os.Stdout)} }

func (p *printer) line(s string, force bool) {
	if p.quiet {
		return
	}
	if !force && time.Since(p.last) < 200*time.Millisecond {
		return
	}
	p.last = time.Now()
	if len([]rune(s)) > 100 {
		r := []rune(s)
		s = string(r[:48]) + "…" + string(r[len(r)-50:])
	}
	pad := max(0, p.width-len([]rune(s)))
	fmt.Print("\r" + s + strings.Repeat(" ", pad))
	p.width = len([]rune(s))
}

func (p *printer) clear() {
	if p.width > 0 {
		fmt.Print("\r" + strings.Repeat(" ", p.width) + "\r")
		p.width = 0
	}
}

func (p *printer) done() { p.clear() }

func (p *printer) Scanning(n int) {
	p.line(fmt.Sprintf("Looking for changes… %d files checked", n), false)
}

func (p *printer) Planned(pl *engine.Plan) {
	p.clear()
	if pl.NothingToDo {
		return
	}
	fmt.Printf("%s  ←  %s\n", pl.SourceName, pl.Origin)
	if pl.Kind == "full" {
		fmt.Printf("Full backup: %d files, %s\n", pl.TotalFiles, size(pl.BytesToRead))
	} else {
		if !pl.LastBackup.IsZero() {
			fmt.Printf("Last backup      %s\n", pl.LastBackup.Local().Format("2006-01-02 15:04"))
		}
		fmt.Printf("New files        %d\nChanged files    %d\nDeleted files    %d\n", pl.New, pl.Modified, pl.Deleted)
		fmt.Printf("To read          %s\n", size(pl.BytesToRead))
	}
	if pl.VaultFree > 0 {
		fmt.Printf("Disk free        %s\n", size(int64(pl.VaultFree)))
	}
	if pl.ScanIssues > 0 {
		fmt.Printf("Unreadable       %d (listed at the end)\n", pl.ScanIssues)
	}
}

func (p *printer) Copying(c engine.CopyProgress) {
	pct := 100.0
	if c.BytesTotal > 0 {
		pct = float64(c.BytesDone) * 100 / float64(c.BytesTotal)
	}
	p.line(fmt.Sprintf("%3.0f%%  %d/%d files  %s", pct, c.FilesDone, c.FilesTotal, c.Current), c.FilesDone == c.FilesTotal)
}

func (p *printer) Phase(name string) {
	msg := map[string]string{"verifying": "Verifying archive…", "saving": "Saving catalog…", "restoring": "Restoring…"}[name]
	if msg != "" {
		p.line(msg, true)
	}
}

func (p *printer) Issue(archive.Issue) {}
