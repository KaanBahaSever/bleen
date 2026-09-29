// Package app is the use-case layer between the engine and any UI. It owns
// settings, the single running job and the cached view of the backup disk.
// Everything here runs only while the app is open.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/config"
	"github.com/kaanbahasever/bleen/internal/engine"
	"github.com/kaanbahasever/bleen/internal/platform"
	"github.com/kaanbahasever/bleen/internal/seal"
	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// Version is set by the desktop entrypoint.
var Version = "dev"

// App holds all state behind the UI.
type App struct {
	// Emit sends an event to the UI. Set before Start.
	Emit func(name string, data any)

	mu        sync.Mutex
	cfg       *config.Config
	view      *catalog.DB // read-only copy of the vault catalog
	viewPath  string
	vaultInfo *VaultState
	reach     map[string]*bool // source id → reachable (nil = checking)
	job       *job
	history   []RunRecord
	key       *seal.Key // unlocked key of an encrypted disk (memory only)
	keyVault  string    // which disk the key belongs to

	ctx    context.Context
	cancel context.CancelFunc
}

func New() (*App, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, reach: map[string]*bool{}, Emit: func(string, any) {}}
	a.history = loadHistory(cfg.Dir())
	return a, nil
}

// Start begins the while-open housekeeping: reading the disk's catalog,
// probing sources, and noticing when the backup disk is plugged in.
func (a *App) Start(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.refreshVault()
	a.probeSources()
	go a.watch()
}

// Shutdown stops housekeeping and cancels any running job.
func (a *App) Shutdown() {
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Lock()
	j := a.job
	a.mu.Unlock()
	if j != nil {
		j.cancel()
		<-j.finished
	}
	a.mu.Lock()
	if a.view != nil {
		a.view.Close()
		os.Remove(a.viewPath)
	}
	a.mu.Unlock()
}

// Busy reports whether a job is running (the UI asks before quitting).
func (a *App) Busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.job != nil && !a.job.state.done()
}

func (a *App) watch() {
	vt := time.NewTicker(3 * time.Second)
	st := time.NewTicker(60 * time.Second)
	defer vt.Stop()
	defer st.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-vt.C:
			a.mu.Lock()
			p, was, busy := a.cfg.Vault.Path, a.vaultInfo != nil && a.vaultInfo.Connected, a.job != nil
			a.mu.Unlock()
			if p != "" && !busy && vault.IsVault(p) != was {
				a.refreshVault()
			}
		case <-st.C:
			a.probeSources()
		}
	}
}

// ---- State --------------------------------------------------------------

type State struct {
	Version          string        `json:"version"`
	Language         string        `json:"language"`
	Theme            string        `json:"theme"`
	ConfirmBeforeRun bool          `json:"confirmBeforeRun"`
	Exclude          []string      `json:"exclude"`
	DefaultExclude   []string      `json:"defaultExclude"`
	Vault            *VaultState   `json:"vault"`
	Vaults           []KnownVault  `json:"vaults"`
	Sources          []SourceState `json:"sources"`
	Job              *JobState     `json:"job"`
	OS               string        `json:"os"`
	KeepGenerations  int           `json:"keepGenerations"`
	NewFullEvery     int           `json:"newFullEvery"`
	Automation       Automation    `json:"automation"`
}

// Automation is the opt-in daily backup as the UI shows it.
type Automation struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Time      string `json:"time"`
}

type VaultState struct {
	ID        string `json:"id"`
	Encrypted bool   `json:"encrypted"`
	Locked    bool   `json:"locked"` // encrypted and not unlocked this session
	Path      string `json:"path"`
	Label     string `json:"label"`
	Connected bool   `json:"connected"`
	Free      uint64 `json:"free"`
	FSType    string `json:"fsType"`
	Error     string `json:"error,omitempty"`
}

type SourceState struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Reachable  *bool     `json:"reachable"`
	VaultID    string    `json:"vaultSourceId"` // catalog uuid, "" if never backed up
	LastBackup time.Time `json:"lastBackup"`
	Backups    int       `json:"backups"`
	LastIssues int       `json:"lastIssues"`
	Recent     []int64   `json:"recent"` // bytes stored by the last backups, oldest first
}

// GetState returns everything the UI renders.
func (a *App) GetState() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stateLocked()
}

func (a *App) stateLocked() State {
	st := State{
		Version:          Version,
		Language:         a.cfg.Language,
		Theme:            a.cfg.Theme,
		ConfirmBeforeRun: a.cfg.Backup.ConfirmBeforeRun,
		Exclude:          append([]string{}, a.cfg.Backup.Exclude...),
		DefaultExclude:   source.DefaultExcludes,
		Vault:            a.vaultInfo,
		Vaults:           a.knownVaultsLocked(),
		OS:               runtime.GOOS,
		Sources:          []SourceState{},
		KeepGenerations:  a.cfg.Backup.KeepGenerations,
		NewFullEvery:     a.cfg.Backup.NewFullEvery,
		Automation: Automation{Supported: platform.SchedulingSupported(),
			Enabled: a.cfg.Automation.Enabled, Time: a.cfg.Automation.Time},
	}
	host, _ := os.Hostname()
	for _, s := range a.cfg.Sources {
		ss := SourceState{ID: s.ID, Name: s.Name, Path: s.Path, Reachable: a.reach[s.ID], Recent: []int64{}}
		if a.view != nil {
			if row, _ := a.view.SourceByOrigin(host, s.Path); row != nil {
				ss.VaultID = row.UUID
				snaps, _ := a.view.Snapshots(row.ID)
				ss.Backups = len(snaps)
				if n := len(snaps); n > 0 {
					ss.LastBackup = snaps[n-1].FinishedAt
					ss.LastIssues = snaps[n-1].FilesSkipped
					for _, sn := range snaps[max(0, n-7):] {
						ss.Recent = append(ss.Recent, sn.BytesStored)
					}
				}
			}
		}
		st.Sources = append(st.Sources, ss)
	}
	if a.job != nil {
		js := a.job.state
		st.Job = &js
	}
	return st
}

func (a *App) emitState() {
	a.mu.Lock()
	st := a.stateLocked()
	a.mu.Unlock()
	a.Emit("state", st)
}

// ---- Settings -----------------------------------------------------------

func (a *App) save() error { return a.cfg.Save() }

func (a *App) SetLanguage(lang string) error {
	a.mu.Lock()
	a.cfg.Language = lang
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

func (a *App) SetTheme(theme string) error {
	a.mu.Lock()
	a.cfg.Theme = theme
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

func (a *App) SetConfirmBeforeRun(on bool) error {
	a.mu.Lock()
	a.cfg.Backup.ConfirmBeforeRun = on
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

// SetExcludes replaces the user's extra exclude patterns.
func (a *App) SetExcludes(patterns []string) error {
	var clean []string
	for _, p := range patterns {
		if p = strings.TrimSpace(p); p != "" {
			clean = append(clean, p)
		}
	}
	a.mu.Lock()
	a.cfg.Backup.Exclude = clean
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

// ---- Sources ------------------------------------------------------------

// AddSource validates and adds a folder (local or \\SERVER\share\path).
func (a *App) AddSource(path string) (SourceState, error) {
	path = strings.TrimSpace(strings.Trim(strings.TrimSpace(path), `"`))
	if path == "" {
		return SourceState{}, errors.New("E_EMPTY_PATH")
	}
	if err := probe(path, 8*time.Second); err != nil {
		return SourceState{}, err
	}
	a.mu.Lock()
	if a.cfg.Vault.Path != "" && isInside(path, a.cfg.Vault.Path) {
		a.mu.Unlock()
		return SourceState{}, errors.New("E_SOURCE_IS_VAULT")
	}
	s := a.cfg.AddSource(path, "")
	t := true
	a.reach[s.ID] = &t
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return SourceState{ID: s.ID, Name: s.Name, Path: s.Path}, err
}

func (a *App) RemoveSource(id string) error {
	a.mu.Lock()
	a.cfg.RemoveSource(id)
	delete(a.reach, id)
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

func (a *App) RenameSource(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("E_EMPTY_NAME")
	}
	a.mu.Lock()
	for i := range a.cfg.Sources {
		if a.cfg.Sources[i].ID == id {
			a.cfg.Sources[i].Name = name
		}
	}
	err := a.save()
	a.mu.Unlock()
	a.emitState()
	return err
}

// probe checks a folder with a timeout: an unreachable server can make a
// plain stat hang for a long time.
func probe(path string, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- source.NewLocal(path, nil).Probe() }()
	select {
	case err := <-done:
		if err != nil {
			var code = engine.ESourceUnreachable
			if errors.Is(err, os.ErrPermission) {
				code = "E_ACCESS_DENIED"
			}
			return errors.New(code)
		}
		return nil
	case <-time.After(timeout):
		return errors.New(engine.ESourceUnreachable)
	}
}

func (a *App) probeSources() {
	a.mu.Lock()
	srcs := append([]config.Source{}, a.cfg.Sources...)
	a.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range srcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok := probe(s.Path, 10*time.Second) == nil
			a.mu.Lock()
			a.reach[s.ID] = &ok
			a.mu.Unlock()
		}()
	}
	wg.Wait()
	a.emitState()
}

func isInside(p, dir string) bool {
	p, _ = filepath.Abs(p)
	dir, _ = filepath.Abs(dir)
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ---- Backup disk --------------------------------------------------------

// Drives lists drives that could hold backups.
func (a *App) Drives() []platform.Volume { return platform.Volumes() }

// KnownVault is a backup disk bleen has used. Several disks can take turns
// (one at home, one at the office): whichever is plugged in is used.
type KnownVault struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	Connected bool   `json:"connected"`
	Active    bool   `json:"active"`
}

func (a *App) knownVaultsLocked() []KnownVault {
	out := []KnownVault{}
	for _, v := range a.cfg.Vaults {
		out = append(out, KnownVault{ID: v.ID, Label: v.Label, Path: v.Path,
			Connected: vault.IsVault(v.Path), Active: v.ID == a.cfg.Vault.ID})
	}
	return out
}

// UseVaultFolder sets the backup disk. It uses an existing bleen folder or
// creates "<dir>/bleen", encrypted with password when one is given.
func (a *App) UseVaultFolder(dir, password string) (VaultState, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	root := dir
	switch {
	case vault.IsVault(dir):
	case vault.IsVault(filepath.Join(dir, "bleen")):
		root = filepath.Join(dir, "bleen")
	default:
		if !strings.EqualFold(filepath.Base(dir), "bleen") {
			root = filepath.Join(dir, "bleen")
		}
		a.mu.Lock()
		for _, s := range a.cfg.Sources {
			if isInside(root, s.Path) {
				a.mu.Unlock()
				return VaultState{}, errors.New("E_VAULT_INSIDE_SOURCE")
			}
		}
		a.mu.Unlock()
		label := platform.VolumeLabel(dir)
		if label == "" {
			// "E:" for a drive root, otherwise the chosen folder's name.
			if vol := filepath.VolumeName(dir); vol != "" && filepath.Clean(vol+`\`) == dir {
				label = vol
			} else {
				label = filepath.Base(dir)
			}
		}
		v, err := vault.Create(root, label, "bleen "+Version, password)
		if err != nil {
			return VaultState{}, err
		}
		v.Close()
	}
	meta, err := vault.ReadMeta(root)
	if err != nil {
		return VaultState{}, err
	}
	var key *seal.Key
	if meta.Encrypted() && password != "" {
		if key, err = vault.Unlock(root, password); err != nil {
			return VaultState{}, err
		}
	}
	a.mu.Lock()
	for _, s := range a.cfg.Sources {
		if isInside(root, s.Path) {
			a.mu.Unlock()
			return VaultState{}, errors.New("E_VAULT_INSIDE_SOURCE")
		}
	}
	a.cfg.Vault = config.Vault{ID: meta.ID, Label: meta.Label, Path: root}
	a.cfg.RememberVault(a.cfg.Vault)
	if key != nil {
		a.key, a.keyVault = key, meta.ID
	}
	err = a.save()
	a.mu.Unlock()
	a.refreshVault()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vaultInfo == nil {
		return VaultState{}, err
	}
	return *a.vaultInfo, err
}

// SwitchVault makes another known disk the one in use.
func (a *App) SwitchVault(id string) error {
	a.mu.Lock()
	found := false
	for _, v := range a.cfg.Vaults {
		if v.ID == id {
			a.cfg.Vault, found = v, true
		}
	}
	err := a.save()
	a.mu.Unlock()
	if !found {
		return errors.New(engine.ENotFound)
	}
	a.refreshVault()
	return err
}

// ForgetVault removes a disk from the list. Its backups stay on the disk.
func (a *App) ForgetVault(id string) error {
	a.mu.Lock()
	a.cfg.ForgetVault(id)
	if a.keyVault == id {
		a.key, a.keyVault = nil, ""
	}
	err := a.save()
	a.mu.Unlock()
	a.refreshVault()
	return err
}

// Unlock opens the encrypted disk in use for this session. The key is kept
// in memory only and forgotten when bleen closes.
func (a *App) Unlock(password string) error {
	a.mu.Lock()
	vc := a.cfg.Vault
	a.mu.Unlock()
	key, err := vault.Unlock(vc.Path, password)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.key, a.keyVault = key, vc.ID
	a.mu.Unlock()
	a.refreshVault()
	return nil
}

// Lock forgets the key of the encrypted disk.
func (a *App) Lock() {
	a.mu.Lock()
	a.key, a.keyVault = nil, ""
	a.mu.Unlock()
	a.refreshVault()
}

// vaultKeyLocked returns the key for the disk in use, if unlocked.
func (a *App) vaultKeyLocked() *seal.Key {
	if a.key != nil && a.keyVault == a.cfg.Vault.ID {
		return a.key
	}
	return nil
}

// refreshVault re-reads the disk: connection, free space and a read-only
// copy of the catalog for Home and History.
func (a *App) refreshVault() {
	a.mu.Lock()
	vc := a.cfg.Vault
	a.mu.Unlock()
	if vc.Path == "" {
		a.setView(nil, nil)
		return
	}
	if !vault.IsVault(vc.Path) {
		if moved := findMovedVault(vc); moved != "" {
			a.mu.Lock()
			a.cfg.Vault.Path = moved
			a.cfg.RememberVault(a.cfg.Vault)
			a.save()
			a.mu.Unlock()
			vc.Path = moved
		} else if other := a.connectedKnownVault(vc.ID); other != nil {
			// Rotating disks: use whichever known disk is plugged in.
			a.mu.Lock()
			a.cfg.Vault = *other
			a.save()
			a.mu.Unlock()
			vc = *other
		}
	}
	info := &VaultState{ID: vc.ID, Path: vc.Path, Label: vc.Label}
	if !vault.IsVault(vc.Path) {
		a.setView(info, nil)
		return
	}
	info.Connected = true
	info.Free, _ = platform.FreeSpace(vc.Path)
	info.FSType, _ = platform.FSType(vc.Path)
	meta, err := vault.ReadMeta(vc.Path)
	if err != nil {
		info.Error = err.Error()
		a.setView(info, nil)
		return
	}
	info.Encrypted = meta.Encrypted()
	a.mu.Lock()
	key := a.vaultKeyLocked()
	a.mu.Unlock()
	if info.Encrypted && key == nil {
		info.Locked = true
		a.setView(info, nil)
		return
	}
	db, err := openView(vc.Path, a.cfg.Dir(), key)
	if err != nil {
		info.Error = err.Error()
	}
	a.setView(info, db)
}

func (a *App) connectedKnownVault(except string) *config.Vault {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, v := range a.cfg.Vaults {
		if v.ID != except && vault.IsVault(v.Path) {
			v := v
			return &v
		}
	}
	return nil
}

func (a *App) setView(info *VaultState, db *viewDB) {
	a.mu.Lock()
	if a.view != nil {
		a.view.Close()
		os.Remove(a.viewPath)
	}
	a.view, a.viewPath = nil, ""
	if db != nil {
		a.view, a.viewPath = db.DB, db.path
	}
	a.vaultInfo = info
	a.mu.Unlock()
	a.emitState()
}

type viewDB struct {
	*catalog.DB
	path string
}

// openView copies the published catalog (never half-written: it is replaced
// by rename) and opens the copy. No lock is needed for reading. An
// encrypted catalog is decrypted into the local copy.
func openView(root, cacheDir string, key *seal.Key) (*viewDB, error) {
	name := "catalog.db"
	if key != nil {
		name += archive.SealedExt
	}
	src := filepath.Join(root, vault.SysDir, name)
	if _, err := os.Stat(src); err != nil {
		return nil, nil
	}
	os.MkdirAll(cacheDir, 0o755)
	f, err := os.CreateTemp(cacheDir, "view-*.db")
	if err != nil {
		return nil, err
	}
	f.Close()
	if key != nil {
		err = key.DecryptFile(src, f.Name())
	} else {
		var b []byte
		if b, err = os.ReadFile(src); err == nil {
			err = os.WriteFile(f.Name(), b, 0o600)
		}
	}
	if err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	db, err := catalog.Open(f.Name())
	if err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	return &viewDB{DB: db, path: f.Name()}, nil
}

// findMovedVault looks for the same vault under another drive letter
// (E: yesterday, F: today).
func findMovedVault(vc config.Vault) string {
	if runtime.GOOS != "windows" || vc.ID == "" {
		return ""
	}
	vol := filepath.VolumeName(vc.Path)
	if len(vol) != 2 {
		return ""
	}
	rest := vc.Path[len(vol):]
	for c := 'A'; c <= 'Z'; c++ {
		cand := string(c) + ":" + rest
		if strings.EqualFold(cand, vc.Path) || !vault.IsVault(cand) {
			continue
		}
		if m, err := vault.ReadMeta(cand); err == nil && m.ID == vc.ID {
			return cand
		}
	}
	return ""
}

// ---- History (read-only, from the cached catalog) -------------------------

type BackupSource struct {
	ID      string       `json:"id"` // catalog uuid
	Name    string       `json:"name"`
	Folder  string       `json:"folder"`
	Origin  string       `json:"origin"`
	Host    string       `json:"host"`
	Backups []BackupInfo `json:"backups"`
}

type BackupInfo struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Generation int       `json:"generation"`
	FinishedAt time.Time `json:"finishedAt"`
	New        int       `json:"new"`
	Modified   int       `json:"modified"`
	Deleted    int       `json:"deleted"`
	Skipped    int       `json:"skipped"`
	Stored     int64     `json:"stored"`
	SourceSize int64     `json:"sourceSize"`
}

// ListBackups returns every backed-up folder on the disk and its backups.
func (a *App) ListBackups() ([]BackupSource, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.view == nil {
		return nil, nil
	}
	srcs, err := a.view.Sources()
	if err != nil {
		return nil, err
	}
	out := make([]BackupSource, 0, len(srcs))
	for _, s := range srcs {
		bs := BackupSource{ID: s.UUID, Name: s.Name, Folder: s.Folder, Origin: s.Origin, Host: s.Host}
		snaps, err := a.view.Snapshots(s.ID)
		if err != nil {
			return nil, err
		}
		for _, sn := range snaps {
			bs.Backups = append(bs.Backups, BackupInfo{ID: sn.UUID, Kind: sn.Kind, Generation: sn.Generation,
				FinishedAt: sn.FinishedAt, New: sn.FilesNew, Modified: sn.FilesModified, Deleted: sn.FilesDeleted,
				Skipped: sn.FilesSkipped, Stored: sn.BytesStored, SourceSize: sn.BytesSource})
		}
		out = append(out, bs)
	}
	return out, nil
}

type BrowseEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Changed bool      `json:"changed"` // new or modified in this very backup
}

type BrowseResult struct {
	Entries    []BrowseEntry `json:"entries"`
	TotalFiles int           `json:"totalFiles"`
	TotalBytes int64         `json:"totalBytes"`
}

// Browse lists one folder of a backup as it was at that moment.
func (a *App) Browse(sourceID, backupID, dir string) (BrowseResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var res BrowseResult
	if a.view == nil {
		return res, errors.New("E_VAULT_MISSING")
	}
	src, snap, err := a.findSnapshot(sourceID, backupID)
	if err != nil {
		return res, err
	}
	all, err := a.view.StateAt(src.ID, snap.GenerationID, snap.Seq)
	if err != nil {
		return res, err
	}
	dir = strings.Trim(dir, "/")
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	sub := map[string]*BrowseEntry{}
	for _, v := range all {
		if v.Kind == archive.KindFile {
			res.TotalFiles++
			res.TotalBytes += v.Size
		}
		if !strings.HasPrefix(v.Path, prefix) || v.Path == dir {
			continue
		}
		rest := v.Path[len(prefix):]
		name, deeper, _ := strings.Cut(rest, "/")
		e := sub[name]
		if e == nil {
			e = &BrowseEntry{Name: name, Path: prefix + name}
			sub[name] = e
		}
		if deeper != "" || v.Kind == archive.KindDir {
			e.Dir = true
			if v.Kind == archive.KindFile {
				e.Size += v.Size
			}
			continue
		}
		e.Size, e.ModTime = v.Size, time.Unix(0, v.MTimeNS)
	}
	for _, e := range sub {
		res.Entries = append(res.Entries, *e)
	}
	sort.Slice(res.Entries, func(i, j int) bool {
		if res.Entries[i].Dir != res.Entries[j].Dir {
			return res.Entries[i].Dir
		}
		return strings.ToLower(res.Entries[i].Name) < strings.ToLower(res.Entries[j].Name)
	})
	return res, nil
}

func (a *App) findSnapshot(sourceID, backupID string) (*catalog.Source, *catalog.Snapshot, error) {
	src, err := a.view.FindSource(sourceID)
	if err != nil || src == nil {
		return nil, nil, errors.New(engine.ENotFound)
	}
	snaps, err := a.view.Snapshots(src.ID)
	if err != nil {
		return nil, nil, err
	}
	for i := range snaps {
		if snaps[i].UUID == backupID {
			return src, &snaps[i], nil
		}
	}
	if backupID == "" && len(snaps) > 0 {
		return src, &snaps[len(snaps)-1], nil
	}
	return nil, nil, errors.New(engine.ENotFound)
}

// OpenFolder shows a folder in the system file manager.
func (a *App) OpenFolder(path string) error { return platform.Reveal(path) }

// SuggestRestoreFolder proposes a new folder next to the user's Desktop.
func (a *App) SuggestRestoreFolder(name string, at time.Time) string {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, "Desktop")
	if _, err := os.Stat(base); err != nil {
		base = home
	}
	n := vault.SafeFolderName(name + " (" + at.Local().Format("2006-01-02 1504") + ")")
	p := filepath.Join(base, n)
	for i := 2; ; i++ {
		if _, err := os.Stat(p); err != nil {
			return p
		}
		p = filepath.Join(base, n+" "+strconv.Itoa(i))
	}
}
