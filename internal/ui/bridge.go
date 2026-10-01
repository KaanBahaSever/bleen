package ui

import (
	"context"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kaanbahasever/bleen/internal/app"
	"github.com/kaanbahasever/bleen/internal/platform"
)

// Bridge is the only type bound to the frontend. Every method here becomes
// a typed TypeScript function; keep the surface small and validate in Go.
type Bridge struct {
	a   *app.App
	ctx context.Context
}

func (b *Bridge) GetState() app.State                 { return b.a.GetState() }
func (b *Bridge) SetLanguage(lang string) error       { return b.a.SetLanguage(lang) }
func (b *Bridge) SetTheme(theme string) error         { return b.a.SetTheme(theme) }
func (b *Bridge) SetConfirmBeforeRun(on bool) error   { return b.a.SetConfirmBeforeRun(on) }
func (b *Bridge) SetExcludes(patterns []string) error { return b.a.SetExcludes(patterns) }
func (b *Bridge) AddSource(path string) (app.SourceState, error) {
	return b.a.AddSource(path)
}
func (b *Bridge) AddSourceAs(path, user, password string) (app.SourceState, error) {
	return b.a.AddSourceAs(path, user, password)
}
func (b *Bridge) RemoveSource(id string) error       { return b.a.RemoveSource(id) }
func (b *Bridge) RenameSource(id, name string) error { return b.a.RenameSource(id, name) }
func (b *Bridge) Drives() []platform.Volume          { return b.a.Drives() }
func (b *Bridge) UseVaultFolder(dir, password string) (app.VaultState, error) {
	return b.a.UseVaultFolder(dir, password)
}
func (b *Bridge) BackupNow(ids []string, full bool) (string, error) { return b.a.BackupNow(ids, full) }
func (b *Bridge) Pause()                                            { b.a.Pause() }
func (b *Bridge) Resume()                                           { b.a.Resume() }
func (b *Bridge) Unlock(password string) error                      { return b.a.Unlock(password) }
func (b *Bridge) Lock()                                             { b.a.Lock() }
func (b *Bridge) BreakLock() error                                  { return b.a.BreakLock() }
func (b *Bridge) DismissNotice()                                    { b.a.DismissNotice() }
func (b *Bridge) SwitchVault(id string) error                       { return b.a.SwitchVault(id) }
func (b *Bridge) ForgetVault(id string) error                       { return b.a.ForgetVault(id) }
func (b *Bridge) SetRetention(keep, newFullEvery int) error {
	return b.a.SetRetention(keep, newFullEvery)
}
func (b *Bridge) SetAutomation(enabled bool, at string) error { return b.a.SetAutomation(enabled, at) }

// ExportReport asks where to save the Activity report and writes it. It
// returns the chosen path, or "" if cancelled.
func (b *Bridge) ExportReport(title string) (string, error) {
	p, err := runtime.SaveFileDialog(b.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: "bleen-report-" + time.Now().Format("2006-01-02") + ".html",
		Filters:         []runtime.FileFilter{{DisplayName: "HTML", Pattern: "*.html"}},
	})
	if err != nil || p == "" {
		return "", err
	}
	return p, b.a.ExportReport(p)
}
func (b *Bridge) ConfirmPlan(ok bool) { b.a.ConfirmPlan(ok) }
func (b *Bridge) Cancel()             { b.a.Cancel() }
func (b *Bridge) DismissJob()         { b.a.DismissJob() }
func (b *Bridge) ListBackups() ([]app.BackupSource, error) {
	return b.a.ListBackups()
}
func (b *Bridge) Browse(sourceID, backupID, dir string) (app.BrowseResult, error) {
	return b.a.Browse(sourceID, backupID, dir)
}
func (b *Bridge) Restore(sourceID, backupID, dest string, paths []string) (string, error) {
	return b.a.Restore(sourceID, backupID, dest, paths)
}

// SuggestRestoreFolder proposes a new folder; at is an RFC 3339 time.
func (b *Bridge) SuggestRestoreFolder(name, at string) string {
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t = time.Now()
	}
	return b.a.SuggestRestoreFolder(name, t)
}

// Export saves a backup (or some of its files) as one ZIP file.
func (b *Bridge) Export(sourceID, backupID, dest string, paths []string) (string, error) {
	return b.a.Export(sourceID, backupID, dest, paths)
}

// SuggestExportFile proposes a new ZIP file; at is an RFC 3339 time.
func (b *Bridge) SuggestExportFile(name, at string) string {
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t = time.Now()
	}
	return b.a.SuggestExportFile(name, t)
}

// ChooseZipFile shows the native save dialog. It returns "" if cancelled.
func (b *Bridge) ChooseZipFile(title, suggested string) (string, error) {
	return runtime.SaveFileDialog(b.ctx, runtime.SaveDialogOptions{
		Title:            title,
		DefaultDirectory: filepath.Dir(suggested),
		DefaultFilename:  filepath.Base(suggested),
		Filters:          []runtime.FileFilter{{DisplayName: "ZIP", Pattern: "*.zip"}},
	})
}
func (b *Bridge) CheckBackups() (string, error) { return b.a.CheckBackups() }
func (b *Bridge) Activity() []app.RunRecord     { return b.a.Activity() }
func (b *Bridge) OpenFolder(path string) error  { return b.a.OpenFolder(path) }

// ChooseFolder shows the native folder picker. It returns "" if cancelled.
func (b *Bridge) ChooseFolder(title, start string) (string, error) {
	return runtime.OpenDirectoryDialog(b.ctx, runtime.OpenDialogOptions{Title: title, DefaultDirectory: start})
}

// OpenURL opens a link in the default browser (About screen).
func (b *Bridge) OpenURL(url string) {
	if len(url) > 8 && url[:8] == "https://" {
		runtime.BrowserOpenURL(b.ctx, url)
	}
}
