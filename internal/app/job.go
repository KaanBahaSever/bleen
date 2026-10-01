package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/config"
	"github.com/kaanbahasever/bleen/internal/engine"
	"github.com/kaanbahasever/bleen/internal/seal"
	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// Phase of the current job, mirrored by the UI's run sheet.
type Phase string

const (
	PhaseScanning  Phase = "scanning"
	PhaseAwaiting  Phase = "awaiting" // preflight: waiting for the user
	PhaseRunning   Phase = "running"
	PhaseVerifying Phase = "verifying"
	PhaseSaving    Phase = "saving"
	PhaseDone      Phase = "done"
	PhaseFailed    Phase = "failed"
	PhaseCancelled Phase = "cancelled"
)

type JobState struct {
	ID         string              `json:"id"`
	Kind       string              `json:"kind"` // "backup" | "restore" | "verify"
	Phase      Phase               `json:"phase"`
	SourceName string              `json:"sourceName"`
	Index      int                 `json:"index"` // 1-based source number in a multi-source run
	Count      int                 `json:"count"`
	Scanned    int                 `json:"scanned"`
	Plan       *engine.Plan        `json:"plan"`
	Progress   engine.CopyProgress `json:"progress"`
	Results    []RunRecord         `json:"results"`
	Error      *JobError           `json:"error"`
	Dest       string              `json:"dest,omitempty"`
	Paused     bool                `json:"paused"`
}

type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s JobState) done() bool {
	return s.Phase == PhaseDone || s.Phase == PhaseFailed || s.Phase == PhaseCancelled
}

type job struct {
	gate     *engine.Gate
	state    JobState
	ctx      context.Context
	cancel   context.CancelFunc
	confirm  chan bool
	finished chan struct{}
	lastEmit time.Time
}

// RunRecord is one line of the Activity screen.
type RunRecord struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Source     string          `json:"source"`
	StartedAt  time.Time       `json:"startedAt"`
	FinishedAt time.Time       `json:"finishedAt"`
	Result     string          `json:"result"` // "done" | "nothing" | "failed" | "cancelled"
	BackupKind string          `json:"backupKind,omitempty"`
	Files      int             `json:"files"`
	Deduped    int             `json:"deduped"`
	Bytes      int64           `json:"bytes"`
	Stored     int64           `json:"stored"`
	Verified   bool            `json:"verified"`
	Archives   []string        `json:"archives,omitempty"`
	Dest       string          `json:"dest,omitempty"`
	Issues     []archive.Issue `json:"issues,omitempty"`
	Error      *JobError       `json:"error,omitempty"`
	Pruned     int             `json:"pruned,omitempty"` // old full backups removed by retention
}

func (a *App) newJob(kind string, count int) (*job, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.job != nil && !a.job.state.done() {
		return nil, errors.New("E_BUSY")
	}
	if a.cfg.Vault.Path == "" {
		return nil, errors.New("E_VAULT_MISSING")
	}
	if !vault.IsVault(a.cfg.Vault.Path) {
		return nil, errors.New("E_VAULT_DISCONNECTED")
	}
	if a.vaultInfo != nil && a.vaultInfo.Locked {
		return nil, errors.New("E_PASSWORD_REQUIRED")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	j := &job{
		gate:     &engine.Gate{},
		state:    JobState{ID: uuid.NewString(), Kind: kind, Phase: PhaseScanning, Count: count},
		ctx:      ctx,
		cancel:   cancel,
		confirm:  make(chan bool, 1),
		finished: make(chan struct{}),
	}
	a.job = j
	return j, nil
}

// update mutates the job state and emits it; progress is throttled to 10 Hz.
func (a *App) update(j *job, throttle bool, f func(*JobState)) {
	a.mu.Lock()
	f(&j.state)
	if throttle && time.Since(j.lastEmit) < 100*time.Millisecond {
		a.mu.Unlock()
		return
	}
	j.lastEmit = time.Now()
	st := j.state
	a.emitMu.Lock() // taken under a.mu: events leave in the order they were made
	a.mu.Unlock()
	a.Emit("job", st)
	a.emitMu.Unlock()
}

// finish ends a job. The caller must have closed the vault already, so the
// lock is gone before anyone (Shutdown, the next job) sees the job as done.
func (a *App) finish(j *job, phase Phase, err error) {
	a.refreshVault()
	a.update(j, false, func(s *JobState) {
		s.Phase = phase
		if err != nil {
			s.Error = toJobError(err)
		}
	})
	close(j.finished)
}

// openJobVault opens the disk in use and makes sure it is the disk the
// user chose, not another bleen disk mounted at the same path.
func openJobVault(path, id string, key *seal.Key) (*vault.Vault, error) {
	v, err := vault.Open(path, vault.OpenOptions{Key: key})
	if err != nil {
		return nil, err
	}
	if id != "" && v.Meta.ID != id {
		v.Close()
		return nil, errors.New("E_FOREIGN_VAULT")
	}
	return v, nil
}

var codeRe = regexp.MustCompile(`E_[A-Z_]+`)

func toJobError(err error) *JobError {
	var e *engine.Error
	if errors.As(err, &e) {
		return &JobError{Code: e.Code, Message: e.Error()}
	}
	var le *vault.LockedError
	if errors.As(err, &le) {
		if le.Running {
			return &JobError{Code: "E_VAULT_IN_USE", Message: le.Error()}
		}
		return &JobError{Code: "E_VAULT_LOCKED", Message: le.Error()}
	}
	if errors.Is(err, context.Canceled) {
		return &JobError{Code: engine.ECancelled, Message: err.Error()}
	}
	if errors.Is(err, os.ErrNotExist) {
		return &JobError{Code: "E_VAULT_DISCONNECTED", Message: err.Error()}
	}
	if c := codeRe.FindString(err.Error()); c != "" {
		return &JobError{Code: c, Message: err.Error()}
	}
	return &JobError{Code: "E_UNKNOWN", Message: err.Error()}
}

// ConfirmPlan answers the preflight sheet.
func (a *App) ConfirmPlan(ok bool) {
	a.mu.Lock()
	j := a.job
	waiting := j != nil && j.state.Phase == PhaseAwaiting
	if waiting {
		j.state.Phase = PhaseRunning // further clicks are ignored
	}
	a.mu.Unlock()
	if waiting {
		select {
		case j.confirm <- ok:
		default:
		}
	}
}

// Cancel stops the running job; earlier backups are never affected.
func (a *App) Cancel() {
	a.mu.Lock()
	j := a.job
	a.mu.Unlock()
	if j != nil {
		j.cancel()
	}
}

// Pause holds a running backup between files; Resume continues it.
func (a *App) Pause()  { a.setPaused(true) }
func (a *App) Resume() { a.setPaused(false) }

func (a *App) setPaused(on bool) {
	a.mu.Lock()
	j := a.job
	done := j == nil || j.state.done()
	a.mu.Unlock()
	if done {
		return
	}
	if on {
		j.gate.Pause()
	} else {
		j.gate.Resume()
	}
	a.update(j, false, func(s *JobState) { s.Paused = on })
}

// DismissJob clears a finished job from the screen.
func (a *App) DismissJob() {
	a.mu.Lock()
	if a.job != nil && a.job.state.done() {
		a.job = nil
	}
	a.mu.Unlock()
	a.emitState()
}

// progress adapts engine events to job state.
type progress struct {
	a *App
	j *job
}

func (p progress) Scanning(n int) {
	p.a.update(p.j, true, func(s *JobState) { s.Phase, s.Scanned = PhaseScanning, n })
}
func (p progress) Planned(pl *engine.Plan) {
	p.a.update(p.j, false, func(s *JobState) { s.Plan = pl })
}
func (p progress) Copying(c engine.CopyProgress) {
	p.a.update(p.j, c.FilesDone != c.FilesTotal, func(s *JobState) {
		if s.Kind == "backup" {
			s.Phase = PhaseRunning
		}
		s.Progress = c
	})
}
func (p progress) Phase(name string) {
	ph := map[string]Phase{"verifying": PhaseVerifying, "saving": PhaseSaving, "restoring": PhaseRunning}[name]
	if ph != "" {
		p.a.update(p.j, false, func(s *JobState) { s.Phase = ph })
	}
}
func (p progress) Issue(archive.Issue) {}

// BackupNow backs up the given sources (all enabled ones when empty), one
// after another. It returns immediately; progress arrives as "job" events.
func (a *App) BackupNow(ids []string, full bool) (string, error) {
	a.mu.Lock()
	var srcs []config.Source
	for _, s := range a.cfg.Sources {
		if (len(ids) == 0 && s.On()) || contains(ids, s.ID) {
			srcs = append(srcs, s)
		}
	}
	confirmAlways := a.cfg.Backup.ConfirmBeforeRun
	excludes := append(append([]string{}, source.DefaultExcludes...), a.cfg.Backup.Exclude...)
	guard := a.cfg.Backup.MassChangeGuard
	fullEvery, keep := a.cfg.Backup.NewFullEvery, a.cfg.Backup.KeepGenerations
	vaultPath, vaultID := a.cfg.Vault.Path, a.cfg.Vault.ID
	key := a.vaultKeyLocked()
	a.mu.Unlock()
	if len(srcs) == 0 {
		return "", errors.New("E_NO_SOURCES")
	}
	j, err := a.newJob("backup", len(srcs))
	if err != nil {
		return "", err
	}
	a.emitState()

	go func() {
		v, err := openJobVault(vaultPath, vaultID, key)
		if err != nil {
			a.finish(j, PhaseFailed, err)
			return
		}
		var lastErr error
		anyOK := false
		cancelled := false
		for i, s := range srcs {
			a.update(j, false, func(st *JobState) {
				st.Index, st.SourceName, st.Phase = i+1, s.Name, PhaseScanning
				st.Plan, st.Progress, st.Scanned = nil, engine.CopyProgress{}, 0
			})
			started := time.Now()
			rep, err := engine.Backup(j.ctx, v, source.NewLocal(s.Path, append(excludes, s.Exclude...)), engine.BackupOptions{
				Name:            s.Name,
				Full:            full,
				AutoFullEvery:   fullEvery,
				Gate:            j.gate,
				Progress:        progress{a, j},
				MassChangeRatio: guard,
				Confirm: func(pl *engine.Plan) bool {
					if !confirmAlways && !pl.MassChange {
						return true
					}
					select { // drop any stale answer
					case <-j.confirm:
					default:
					}
					a.update(j, false, func(st *JobState) { st.Phase, st.Plan = PhaseAwaiting, pl })
					select {
					case ok := <-j.confirm:
						return ok
					case <-j.ctx.Done():
						return false
					}
				},
			})
			rec := RunRecord{ID: uuid.NewString(), Kind: "backup", Source: s.Name, StartedAt: started, FinishedAt: time.Now()}
			switch {
			case err != nil:
				rec.Result, rec.Error = "failed", toJobError(err)
				var e *engine.Error
				if errors.As(err, &e) && e.Code == engine.ECancelled {
					rec.Result, cancelled = "cancelled", true
				} else {
					lastErr = err
				}
			case rep.NothingToDo:
				rec.Result, rec.Issues = "nothing", rep.Issues
				anyOK = true
			default:
				rec.Result, rec.BackupKind = "done", rep.Plan.Kind
				rec.Files, rec.Deduped = rep.FilesStored+rep.FilesDeduped, rep.FilesDeduped
				rec.Bytes, rec.Stored, rec.Verified = rep.BytesSource, rep.BytesStored, rep.Verified
				rec.Archives, rec.Issues = rep.Archives, rep.Issues
				rec.Pruned = prune(v, s.Path, keep)
				anyOK = true
			}
			a.record(rec)
			a.update(j, false, func(st *JobState) { st.Results = append(st.Results, rec) })
			if cancelled || j.ctx.Err() != nil {
				cancelled = true
				break
			}
		}
		v.Close()
		switch {
		case cancelled:
			a.finish(j, PhaseCancelled, nil)
		case lastErr != nil && !anyOK:
			a.finish(j, PhaseFailed, lastErr) // nothing succeeded
		default:
			a.finish(j, PhaseDone, nil) // per-source failures are listed in Results
		}
	}()
	return j.state.ID, nil
}

// Restore restores a backup (or only some paths of it) into dest.
func (a *App) Restore(sourceID, backupID, dest string, paths []string) (string, error) {
	return a.restoreJob("restore", sourceID, dest, func(ctx context.Context, v *vault.Vault, src *catalog.Source, p engine.Progress) (*engine.RestoreReport, error) {
		return engine.Restore(ctx, v, src, engine.RestoreOptions{Snapshot: backupID, Dest: dest, Paths: paths, Progress: p})
	})
}

// Export saves a backup (or only some paths of it) as one new ZIP file.
func (a *App) Export(sourceID, backupID, dest string, paths []string) (string, error) {
	return a.restoreJob("export", sourceID, dest, func(ctx context.Context, v *vault.Vault, src *catalog.Source, p engine.Progress) (*engine.RestoreReport, error) {
		return engine.Export(ctx, v, src, engine.ExportOptions{Snapshot: backupID, Dest: dest, Paths: paths, Progress: p})
	})
}

type restoreFunc func(context.Context, *vault.Vault, *catalog.Source, engine.Progress) (*engine.RestoreReport, error)

func (a *App) restoreJob(kind, sourceID, dest string, run restoreFunc) (string, error) {
	a.mu.Lock()
	vaultPath, vaultID := a.cfg.Vault.Path, a.cfg.Vault.ID
	key := a.vaultKeyLocked()
	a.mu.Unlock()
	if !filepath.IsAbs(dest) {
		return "", errors.New("E_NOT_ABSOLUTE")
	}
	if !destReachable(dest) {
		return "", errors.New("E_DEST_UNREACHABLE")
	}
	j, err := a.newJob(kind, 1)
	if err != nil {
		return "", err
	}
	a.update(j, false, func(s *JobState) { s.Phase, s.Dest = PhaseRunning, dest })
	go func() {
		v, err := openJobVault(vaultPath, vaultID, key)
		if err != nil {
			a.finish(j, PhaseFailed, err)
			return
		}
		src, err := v.Catalog.FindSource(sourceID)
		if err != nil || src == nil {
			v.Close()
			a.finish(j, PhaseFailed, errors.New(engine.ENotFound))
			return
		}
		a.update(j, false, func(s *JobState) { s.SourceName = src.Name })
		started := time.Now()
		rep, err := run(j.ctx, v, src, progress{a, j})
		v.Close()
		rec := RunRecord{ID: uuid.NewString(), Kind: kind, Source: src.Name, StartedAt: started, FinishedAt: time.Now(), Dest: dest}
		if err != nil {
			rec.Result, rec.Error = "failed", toJobError(err)
			if rec.Error.Code == "E_VAULT_DISCONNECTED" && vault.IsVault(vaultPath) {
				// The disk is still there: the destination went away.
				rec.Error.Code = "E_DEST_UNREACHABLE"
				err = errors.New("E_DEST_UNREACHABLE: " + err.Error())
			}
			phase := PhaseFailed
			if rec.Error.Code == engine.ECancelled {
				rec.Result, phase = "cancelled", PhaseCancelled
			}
			a.record(rec)
			a.update(j, false, func(s *JobState) { s.Results = append(s.Results, rec) })
			a.finish(j, phase, err)
			return
		}
		rec.Result, rec.Files, rec.Bytes, rec.Issues = "done", rep.Files, rep.Bytes, rep.Issues
		rec.Verified = len(rep.Issues) == 0
		rec.Dest = rep.Dest
		a.record(rec)
		a.update(j, false, func(s *JobState) { s.Results = append(s.Results, rec) })
		a.finish(j, PhaseDone, nil)
	}()
	return j.state.ID, nil
}

// destReachable reports whether dest's drive or network share is there.
func destReachable(dest string) bool {
	vol := filepath.VolumeName(dest)
	if vol == "" {
		return true
	}
	_, err := os.Stat(vol + string(filepath.Separator))
	return err == nil
}

// CheckBackups re-reads every archive on the disk ("verify").
func (a *App) CheckBackups() (string, error) {
	a.mu.Lock()
	vaultPath, vaultID := a.cfg.Vault.Path, a.cfg.Vault.ID
	key := a.vaultKeyLocked()
	a.mu.Unlock()
	j, err := a.newJob("verify", 1)
	if err != nil {
		return "", err
	}
	a.update(j, false, func(s *JobState) { s.Phase = PhaseVerifying })
	go func() {
		v, err := openJobVault(vaultPath, vaultID, key)
		if err != nil {
			a.finish(j, PhaseFailed, err)
			return
		}
		started := time.Now()
		rep, err := engine.VerifyVault(j.ctx, v, progress{a, j})
		v.Close()
		rec := RunRecord{ID: uuid.NewString(), Kind: "verify", StartedAt: started, FinishedAt: time.Now()}
		if err != nil {
			rec.Result, rec.Error = "failed", toJobError(err)
		} else {
			rec.Result, rec.Files, rec.Bytes, rec.Issues = "done", rep.Archives, rep.Bytes, rep.Problems
			rec.Verified = len(rep.Problems) == 0
		}
		a.record(rec)
		a.update(j, false, func(s *JobState) { s.Results = append(s.Results, rec) })
		if err != nil {
			a.finish(j, PhaseFailed, err)
			return
		}
		a.finish(j, PhaseDone, nil)
	}()
	return j.state.ID, nil
}

// ---- Activity history (runs.json, newest first, last 200) -----------------

var historyMu sync.Mutex

func historyPath(dir string) string { return filepath.Join(dir, "runs.json") }

func loadHistory(dir string) []RunRecord {
	b, err := os.ReadFile(historyPath(dir))
	if err != nil {
		return nil
	}
	var out []RunRecord
	json.Unmarshal(b, &out)
	return out
}

func (a *App) record(r RunRecord) {
	historyMu.Lock()
	defer historyMu.Unlock()
	a.mu.Lock()
	dir := a.cfg.Dir()
	a.mu.Unlock()
	// Merge with the file: a scheduled run may have added records while the
	// window was open.
	merged := []RunRecord{r}
	seen := map[string]bool{r.ID: true}
	a.mu.Lock()
	mem := append([]RunRecord{}, a.history...)
	a.mu.Unlock()
	for _, list := range [][]RunRecord{loadHistory(dir), mem} {
		for _, x := range list {
			if !seen[x.ID] {
				seen[x.ID] = true
				merged = append(merged, x)
			}
		}
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].StartedAt.After(merged[j].StartedAt) })
	if len(merged) > 200 {
		merged = merged[:200]
	}
	b, _ := json.MarshalIndent(merged, "", " ")
	os.MkdirAll(dir, 0o755)
	tmp := historyPath(dir) + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, historyPath(dir))
	}
	a.mu.Lock()
	a.history = merged
	a.mu.Unlock()
}

// Activity returns past runs, newest first.
func (a *App) Activity() []RunRecord {
	a.mu.Lock()
	dir := a.cfg.Dir()
	a.mu.Unlock()
	if h := loadHistory(dir); h != nil {
		a.mu.Lock()
		a.history = h
		a.mu.Unlock()
		return h
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]RunRecord{}, a.history...)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// prune applies retention after a successful backup of origin.
func prune(v *vault.Vault, origin string, keep int) int {
	if keep < 1 {
		return 0
	}
	host, _ := os.Hostname()
	row, err := v.Catalog.SourceByOrigin(host, filepath.Clean(origin))
	if err != nil || row == nil {
		return 0
	}
	rep, err := engine.Prune(v, row, keep)
	if err != nil {
		return 0
	}
	return rep.Generations
}
