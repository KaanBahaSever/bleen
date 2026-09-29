package ui

import (
	"context"
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
func (b *Bridge) RemoveSource(id string) error       { return b.a.RemoveSource(id) }
func (b *Bridge) RenameSource(id, name string) error { return b.a.RenameSource(id, name) }
func (b *Bridge) Drives() []platform.Volume          { return b.a.Drives() }
func (b *Bridge) UseVaultFolder(dir string) (app.VaultState, error) {
	return b.a.UseVaultFolder(dir)
}
func (b *Bridge) BackupNow(ids []string) (string, error) { return b.a.BackupNow(ids) }
func (b *Bridge) ConfirmPlan(ok bool)                    { b.a.ConfirmPlan(ok) }
func (b *Bridge) Cancel()                                { b.a.Cancel() }
func (b *Bridge) DismissJob()                            { b.a.DismissJob() }
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
