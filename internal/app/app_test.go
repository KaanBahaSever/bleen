package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaanbahasever/bleen/internal/config"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// home points the user's config folder at a temporary one.
func home(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("APPDATA", d)         // Windows
	t.Setenv("XDG_CONFIG_HOME", d) // Linux
	t.Setenv("HOME", d)            // macOS
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(dir, 0o755)
	return dir
}

func newDisk(t *testing.T, root string) config.Vault {
	t.Helper()
	v, err := vault.Create(root, "disk", "test", "")
	if err != nil {
		t.Fatal(err)
	}
	id := v.Meta.ID
	v.Close()
	return config.Vault{ID: id, Label: "disk", Path: root}
}

func lastRun(t *testing.T, a *App) RunRecord {
	t.Helper()
	runs := a.Activity()
	if len(runs) == 0 {
		t.Fatal("no run recorded")
	}
	return runs[0]
}

// TestScheduledRunUsesRotatedDisk: two rotating disks show up at the same
// path; the scheduled run must use whichever known disk is plugged in.
func TestScheduledRunUsesRotatedDisk(t *testing.T) {
	dir := home(t)
	usb := filepath.Join(t.TempDir(), "E")
	a := newDisk(t, filepath.Join(usb, "bleen"))
	os.Rename(usb, usb+"-A") // disk A unplugged
	b := newDisk(t, filepath.Join(usb, "bleen"))

	src := filepath.Join(t.TempDir(), "docs")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644)

	cfg, _ := config.Load(dir)
	cfg.AddSource(src, "docs")
	cfg.Vault = a
	cfg.RememberVault(a)
	cfg.RememberVault(b)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := RunScheduled(); err != nil {
		t.Fatalf("scheduled run: %v", err)
	}
	app, _ := New()
	if r := lastRun(t, app); r.Result != "done" {
		t.Fatalf("run: %+v", r)
	}
	if app.cfg.Vault.ID != b.ID {
		t.Fatal("the disk in use was not switched to the plugged-in one")
	}
}

// TestScheduledRunReportsResetSettings: a broken settings file is reported
// in Activity and shown the next time bleen opens.
func TestScheduledRunReportsResetSettings(t *testing.T) {
	dir := home(t)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("this = is [ not toml"), 0o644)
	if err := RunScheduled(); err == nil {
		t.Fatal("scheduled run with broken settings succeeded")
	}
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if r := lastRun(t, a); r.Error == nil || r.Error.Code != "E_CONFIG_RESET" {
		t.Fatalf("run: %+v", r)
	}
	if a.notice != "E_CONFIG_RESET" {
		t.Fatal("the app does not show the reset notice")
	}
	a.DismissNotice()
	if b, _ := New(); b.notice != "" {
		t.Fatal("the notice comes back after it was dismissed")
	}
}

// TestBackupAllFailedIsFailed: when every location fails, the run as a
// whole failed; it is not "All done".
func TestBackupAllFailedIsFailed(t *testing.T) {
	dir := home(t)
	disk := newDisk(t, filepath.Join(t.TempDir(), "bleen"))
	cfg, _ := config.Load(dir)
	cfg.AddSource(filepath.Join(t.TempDir(), "missing-1"), "one")
	cfg.AddSource(filepath.Join(t.TempDir(), "missing-2"), "two")
	cfg.Vault = disk
	cfg.Backup.ConfirmBeforeRun = false
	cfg.Save()

	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	a.ctx, a.cancel = context.WithCancel(context.Background()) // what Start sets, without its watchers
	defer a.Shutdown()
	if _, err := a.BackupNow(nil, false); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	j := a.job
	a.mu.Unlock()
	<-j.finished
	if j.state.Phase != PhaseFailed || len(j.state.Results) != 2 {
		t.Fatalf("phase %s, %d results", j.state.Phase, len(j.state.Results))
	}
}

// TestHandAddedSourceIsOn: a location added to config.toml by hand without
// "enabled" is backed up.
func TestHandAddedSourceIsOn(t *testing.T) {
	dir := home(t)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("version = 1\n\n[[sources]]\nname = \"docs\"\npath = \"C:/docs\"\n"), 0o644)
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 1 || !cfg.Sources[0].On() || cfg.Sources[0].ID == "" {
		t.Fatalf("sources: %+v", cfg.Sources)
	}
}

// TestScheduledRunFindsDiskUnderNewLetter: a known disk that now shows up at
// the path of the disk in use (a new drive letter) is used, not refused.
func TestScheduledRunFindsDiskUnderNewLetter(t *testing.T) {
	dir := home(t)
	e := filepath.Join(t.TempDir(), "E", "bleen")
	a := newDisk(t, e)
	os.RemoveAll(filepath.Dir(e)) // disk A is gone
	f := filepath.Join(t.TempDir(), "F", "bleen")
	b := newDisk(t, f)
	os.Rename(filepath.Dir(f), filepath.Dir(e)) // disk B now appears as E:
	src := filepath.Join(t.TempDir(), "docs")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644)
	cfg, _ := config.Load(dir)
	cfg.AddSource(src, "docs")
	cfg.Vault = a
	cfg.RememberVault(a)
	cfg.RememberVault(b)
	cfg.Save()
	if err := RunScheduled(); err != nil {
		t.Fatalf("scheduled run: %v", err)
	}
	app, _ := New()
	if app.cfg.Vault.ID != b.ID || app.cfg.Vault.Path != e {
		t.Fatalf("disk in use: %+v", app.cfg.Vault)
	}
}

// TestDestProblem: a wrong restore or export destination is refused before
// a job starts, so the restore sheet can stay open.
func TestDestProblem(t *testing.T) {
	d := t.TempDir()
	vaultPath := filepath.Join(d, "bleen")
	full := filepath.Join(d, "full")
	os.MkdirAll(full, 0o755)
	os.WriteFile(filepath.Join(full, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(d, "x.zip"), []byte("zip"), 0o644)
	cases := []struct{ kind, dest, want string }{
		{"restore", filepath.Join(d, "new"), ""},
		{"restore", full, "E_DEST_NOT_EMPTY"},
		{"restore", filepath.Join(vaultPath, "r"), "E_DEST_IN_VAULT"},
		{"export", filepath.Join(d, "y.zip"), ""},
		{"export", filepath.Join(d, "x.zip"), "E_DEST_EXISTS"},
		{"export", filepath.Join(vaultPath, "y.zip"), "E_DEST_IN_VAULT"},
	}
	for _, c := range cases {
		if got := destProblem(c.kind, c.dest, vaultPath); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.kind, c.dest, got, c.want)
		}
	}
}

// TestExportNeedsAFileName: a folder (or a path ending in a separator) is
// not a ZIP name; it must not become a file called ".zip".
func TestExportNeedsAFileName(t *testing.T) {
	home(t)
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	for _, dest := range []string{d, d + string(filepath.Separator), d + string(filepath.Separator) + ". "} {
		if _, err := a.Export("x", "y", dest, nil); err == nil || err.Error() != "E_DEST_NOT_FILE" {
			t.Errorf("%q: %v", dest, err)
		}
	}
}
