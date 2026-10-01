package app

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kaanbahasever/bleen/internal/config"
	"github.com/kaanbahasever/bleen/internal/engine"
	"github.com/kaanbahasever/bleen/internal/platform"
	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// SetRetention sets when a fresh full backup starts (every N backups, 0 =
// never) and how many full backups are kept with their changes (0 = all).
func (a *App) SetRetention(keepGenerations, newFullEvery int) error {
	if keepGenerations < 0 || newFullEvery < 0 {
		return errors.New("E_INVALID")
	}
	a.mu.Lock()
	a.cfg.Backup.KeepGenerations = keepGenerations
	a.cfg.Backup.NewFullEvery = newFullEvery
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

// SetAutomation turns the opt-in daily backup on or off. On means one
// Task Scheduler entry that starts "bleen --scheduled"; nothing of bleen
// runs in the background otherwise.
func (a *App) SetAutomation(enabled bool, at string) error {
	if at == "" {
		at = "18:00"
	}
	a.mu.Lock()
	encrypted := a.vaultInfo != nil && a.vaultInfo.Encrypted
	a.mu.Unlock()
	if enabled {
		if encrypted {
			return errors.New("E_AUTOMATION_ENCRYPTED")
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := platform.InstallDailyTask(exe, []string{"--scheduled"}, at); err != nil {
			return err
		}
	} else if err := platform.RemoveDailyTask(); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg.Automation = config.Automation{Enabled: enabled, Time: at}
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

// RunScheduled is what the scheduled task runs: back up every enabled
// location to whichever known disk is plugged in, record the result in
// Activity, and exit. It never asks questions: if the disk is missing,
// encrypted, or the mass-change guard trips, the run is recorded as failed
// and the user sees it next time they open bleen.
func RunScheduled() error {
	a, err := New()
	if err != nil {
		return err
	}
	fail := func(code, msg string) error {
		a.record(RunRecord{ID: uuid.NewString(), Kind: "backup", Source: "⏰", StartedAt: time.Now(),
			FinishedAt: time.Now(), Result: "failed", Error: &JobError{Code: code, Message: msg}})
		return errors.New(code)
	}
	vc := a.cfg.Vault
	if !vault.IsVault(vc.Path) {
		if other := a.connectedKnownVault(vc.ID); other != nil {
			vc = *other
		} else {
			return fail("E_VAULT_DISCONNECTED", "scheduled backup: the backup disk was not connected")
		}
	}
	if m, err := vault.ReadMeta(vc.Path); err == nil && m.Encrypted() {
		return fail("E_AUTOMATION_ENCRYPTED", "scheduled backup: encrypted disks need your password")
	}
	v, err := openJobVault(vc.Path, vc.ID, nil)
	if err != nil {
		return fail(toJobError(err).Code, err.Error())
	}
	defer v.Close()
	excludes := append(append([]string{}, source.DefaultExcludes...), a.cfg.Backup.Exclude...)
	for _, s := range a.cfg.Sources {
		if !s.Enabled {
			continue
		}
		started := time.Now()
		rep, err := engine.Backup(context.Background(), v, source.NewLocal(s.Path, append(excludes, s.Exclude...)), engine.BackupOptions{
			Name:            s.Name,
			AutoFullEvery:   a.cfg.Backup.NewFullEvery,
			MassChangeRatio: a.cfg.Backup.MassChangeGuard,
			// Confirm is nil: a mass change fails the run with E_MASS_CHANGE.
		})
		rec := RunRecord{ID: uuid.NewString(), Kind: "backup", Source: s.Name + " ⏰", StartedAt: started, FinishedAt: time.Now()}
		switch {
		case err != nil:
			rec.Result, rec.Error = "failed", toJobError(err)
		case rep.NothingToDo:
			rec.Result, rec.Issues = "nothing", rep.Issues
		default:
			rec.Result, rec.BackupKind = "done", rep.Plan.Kind
			rec.Files, rec.Deduped = rep.FilesStored+rep.FilesDeduped, rep.FilesDeduped
			rec.Bytes, rec.Stored, rec.Verified = rep.BytesSource, rep.BytesStored, rep.Verified
			rec.Archives, rec.Issues = rep.Archives, rep.Issues
			rec.Pruned = prune(v, s.Path, a.cfg.Backup.KeepGenerations)
		}
		a.record(rec)
	}
	return nil
}

// ExportReport writes the Activity history as a standalone HTML file.
func (a *App) ExportReport(path string) error {
	runs := a.Activity()
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>bleen report</title><style>
body{font:14px system-ui,sans-serif;margin:32px;color:#1f2328;background:#fbfaf7}
h1{font-size:22px}table{border-collapse:collapse;width:100%;background:#fff}
th,td{border-bottom:1px solid #e7e3dc;padding:8px 10px;text-align:left;vertical-align:top}
th{font-size:12px;color:#646a73}.ok{color:#1f7a55}.bad{color:#b23a2e}.warn{color:#8a5a12}
small{color:#646a73}code{font:12px ui-monospace,monospace}</style></head><body>`)
	fmt.Fprintf(&b, "<h1>bleen report</h1><p><small>%s · %d runs</small></p><table><tr><th>Date</th><th>What</th><th>Result</th><th>Files</th><th>Size</th><th>Details</th></tr>",
		html.EscapeString(time.Now().Format("2006-01-02 15:04")), len(runs))
	for _, r := range runs {
		cls := map[string]string{"done": "ok", "failed": "bad", "cancelled": "warn", "nothing": ""}[r.Result]
		if len(r.Issues) > 0 && r.Result == "done" {
			cls = "warn"
		}
		var det strings.Builder
		if r.Error != nil {
			fmt.Fprintf(&det, "<div class=bad>%s</div>", html.EscapeString(r.Error.Message))
		}
		for _, ar := range r.Archives {
			fmt.Fprintf(&det, "<div><code>%s</code></div>", html.EscapeString(ar))
		}
		if r.Dest != "" {
			fmt.Fprintf(&det, "<div><code>→ %s</code></div>", html.EscapeString(r.Dest))
		}
		if r.Pruned > 0 {
			fmt.Fprintf(&det, "<div>%d old full backup(s) removed</div>", r.Pruned)
		}
		for _, is := range r.Issues {
			fmt.Fprintf(&det, "<div class=warn><code>%s</code> %s</div>", html.EscapeString(is.Path), html.EscapeString(is.Message))
		}
		size := r.Stored
		if size == 0 {
			size = r.Bytes
		}
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s %s</td><td class=%s>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>",
			html.EscapeString(r.StartedAt.Local().Format("2006-01-02 15:04")), html.EscapeString(r.Kind), html.EscapeString(r.Source),
			cls, html.EscapeString(r.Result), r.Files, humanSize(size), det.String())
	}
	b.WriteString("</table></body></html>")
	if filepath.Ext(path) == "" {
		path += ".html"
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// AddSourceAs connects to a network share with a user name and password
// (kept by Windows, not by bleen) and then adds the folder.
func (a *App) AddSourceAs(path, user, password string) (SourceState, error) {
	server, share, ok := splitUNC(strings.TrimSpace(path))
	if !ok {
		return SourceState{}, errors.New("E_NOT_NETWORK_PATH")
	}
	if err := platform.SaveShareCredential(server, share, user, password); err != nil {
		if errors.Is(err, platform.ErrLogonFailed) {
			return SourceState{}, errors.New("E_LOGON_FAILED")
		}
		return SourceState{}, err
	}
	return a.AddSource(path)
}

// splitUNC splits \server\share\rest into server and share.
func splitUNC(p string) (server, share string, ok bool) {
	p = strings.ReplaceAll(p, "/", `\`)
	if !strings.HasPrefix(p, `\`) {
		return "", "", false
	}
	parts := strings.SplitN(p[2:], `\`, 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
