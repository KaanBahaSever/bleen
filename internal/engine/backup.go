package engine

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/platform"
	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

type BackupOptions struct {
	Name string // display name for a source seen for the first time
	Full bool   // start a new generation even if one exists
	// AutoFullEvery starts a new generation once the current one has this
	// many backups, keeping restore chains short (0 = never).
	AutoFullEvery int
	Readers       int   // concurrent file reads (default 4)
	Gate          *Gate // optional: pause/resume between files
	Progress      Progress

	// Confirm is called with the preflight plan; returning false cancels.
	// When nil, the run proceeds unless the mass-change guard trips and
	// AllowMassChange is false.
	Confirm         func(*Plan) bool
	AllowMassChange bool
	MassChangeRatio float64 // default 0.30

	MaxPartSize int64 // 0 = auto (split on FAT32), <0 = never split
	TempDir     string
	Now         func() time.Time
	RetryDelays []time.Duration // for files locked by other programs

	afterRename func() error // test hook: simulates a crash before the catalog is saved
}

type Report struct {
	Plan         *Plan
	SnapshotID   string
	Archives     []string
	FilesStored  int
	FilesDeduped int
	BytesSource  int64
	BytesStored  int64
	Issues       []archive.Issue
	Verified     bool
	NothingToDo  bool
	Duration     time.Duration
}

type task struct {
	e        source.Entry
	op       archive.Op
	prev     *catalog.Version
	replaces bool // the path was a folder or link before
}

type result struct {
	t     task
	c     *archive.Compressed
	size  int64
	mtime time.Time
	issue *archive.Issue
}

// Backup backs up src into the vault: a full backup the first time (or when
// opt.Full is set), otherwise only what changed since the last backup.
func Backup(ctx context.Context, v *vault.Vault, src *source.LocalFS, opt BackupOptions) (*Report, error) {
	fill(&opt)
	began := opt.Now()
	prog := opt.Progress
	host, _ := os.Hostname()
	origin := src.Root()

	if err := src.Probe(); err != nil {
		return nil, errorf(ESourceUnreachable, err, "can't reach %s", origin)
	}

	row, err := v.Catalog.SourceByOrigin(host, origin)
	if err != nil {
		return nil, err
	}
	info := archive.SourceInfo{Origin: origin, Host: host}
	var folder string
	var latest *catalog.Snapshot
	if row == nil {
		info.ID = uuid.NewString()
		info.Name = opt.Name
		if info.Name == "" {
			info.Name = filepath.Base(origin)
		}
	} else {
		info.ID, info.Name, folder = row.UUID, row.Name, row.Folder
		if latest, err = v.Catalog.LatestSnapshot(row.ID); err != nil {
			return nil, err
		}
	}

	snap := archive.SnapshotInfo{ID: uuid.NewString(), Kind: "full", Generation: 1, StartedAt: began.UTC()}
	state := map[string]catalog.Version{}
	// last is the most recent known state, even when this run starts a new
	// generation: the safety guards compare against it so that full backups
	// (and the retention that follows them) are protected too.
	var last map[string]catalog.Version
	plan := &Plan{SourceName: info.Name, Origin: origin, Kind: "full"}
	if latest != nil {
		plan.LastBackup = latest.FinishedAt
		if last, err = v.Catalog.CurrentState(row.ID, latest.GenerationID); err != nil {
			return nil, err
		}
		if opt.Full || (opt.AutoFullEvery > 0 && latest.Seq+1 >= opt.AutoFullEvery) {
			snap.Generation = latest.Generation + 1
		} else {
			snap.Kind, plan.Kind = "incremental", "incremental"
			snap.Generation, snap.Seq = latest.Generation, latest.Seq+1
			state = last
		}
		if snaps, err := v.Catalog.Snapshots(row.ID); err == nil {
			for _, s := range snaps {
				if s.Kind == "full" {
					plan.LastFull = s.FinishedAt
				}
			}
		}
	}

	// Scan.
	entries := map[string]source.Entry{}
	var issues []archive.Issue
	var bad []string // paths that could not be read: never recorded as deleted
	seen := 0
	err = src.Walk(ctx, func(e source.Entry) {
		entries[e.Path] = e
		if e.Kind == archive.KindFile {
			if seen++; seen%500 == 0 {
				prog.Scanning(seen)
			}
		}
	}, func(wi source.WalkIssue) {
		bad = append(bad, wi.Path)
		issues = append(issues, archive.Issue{Path: wi.Path, Code: wi.Code, Message: wi.Err.Error()})
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, errorf(ECancelled, nil, "cancelled")
		}
		return nil, errorf(ESourceUnreachable, err, "can't read %s", origin)
	}
	prog.Scanning(seen)

	// Diff.
	unreadable := func(p string) bool {
		for _, b := range bad {
			if p == b || strings.HasPrefix(p, b+"/") {
				return true
			}
		}
		return false
	}
	var tasks []task
	var meta []archive.Entry
	for p, e := range entries {
		prev, had := state[p]
		switch e.Kind {
		case archive.KindDir:
			if !had || prev.Kind != archive.KindDir {
				meta = append(meta, archive.Entry{Op: opFor(had), Path: p, Kind: archive.KindDir, MTime: e.ModTime.UTC(), Replaces: had})
			}
		case archive.KindSymlink:
			if !had || prev.Kind != archive.KindSymlink || prev.LinkTarget != e.Target {
				meta = append(meta, archive.Entry{Op: opFor(had), Path: p, Kind: archive.KindSymlink, Target: e.Target, MTime: e.ModTime.UTC(),
					Replaces: had && prev.Kind != archive.KindSymlink})
			}
		case archive.KindFile:
			plan.TotalFiles++
			switch {
			case !had || prev.Kind != archive.KindFile:
				tasks = append(tasks, task{e: e, op: opFor(had), replaces: had})
				plan.New++
			case prev.Size == e.Size && prev.MTimeNS == e.ModTime.UnixNano():
				// unchanged
			default:
				pv := prev
				tasks = append(tasks, task{e: e, op: archive.OpModified, prev: &pv})
				plan.Modified++
			}
		}
	}
	// A hand restore deletes what DELETED.txt lists before it extracts the
	// archive, so a rename that only changes letter case ("Docs" to "docs")
	// is simply listed: the old name goes, then the new one is extracted.
	for p, prev := range state {
		if _, ok := entries[p]; ok || unreadable(p) {
			continue
		}
		meta = append(meta, archive.Entry{Op: archive.OpDeleted, Path: p, Kind: prev.Kind})
		if prev.Kind == archive.KindFile {
			plan.Deleted++
		}
	}

	// Safety guards, measured against the last known state.
	lastFiles, lastChanged := 0, 0
	for p, l := range last {
		if l.Kind != archive.KindFile {
			continue
		}
		lastFiles++
		e, ok := entries[p]
		switch {
		case unreadable(p):
		case !ok, e.Size != l.Size, e.ModTime.UnixNano() != l.MTimeNS:
			lastChanged++
		}
	}
	if lastFiles > 0 && plan.TotalFiles == 0 {
		return nil, errorf(ESourceEmpty, nil,
			"%s looks empty. If the server or folder is disconnected, nothing is recorded; check it and try again", origin)
	}
	for _, t := range tasks {
		plan.BytesToRead += t.e.Size
	}
	plan.EstStored = int64(float64(plan.BytesToRead) * 0.7)
	plan.ScanIssues = len(issues)
	if lastFiles > 0 {
		plan.ChangedRatio = float64(lastChanged) / float64(lastFiles)
		plan.MassChange = lastFiles >= 20 && plan.ChangedRatio > opt.MassChangeRatio
	}
	plan.NothingToDo = len(tasks) == 0 && len(meta) == 0
	if free, err := platform.FreeSpace(v.Root); err == nil {
		plan.VaultFree = free
	}
	prog.Planned(plan)

	if plan.NothingToDo {
		return &Report{Plan: plan, NothingToDo: true, Issues: issues, Duration: opt.Now().Sub(began)}, nil
	}
	if plan.VaultFree > 0 && uint64(float64(plan.EstStored)*1.1) > plan.VaultFree {
		return nil, errorf(EVaultFull, nil, "the backup disk needs about %d MB but has %d MB free",
			plan.EstStored>>20, plan.VaultFree>>20)
	}
	if opt.Confirm != nil {
		if !opt.Confirm(plan) {
			return nil, errorf(ECancelled, nil, "cancelled")
		}
	} else if plan.MassChange && !opt.AllowMassChange {
		return nil, errorf(EMassChange, nil,
			"unusually many changes (%d of %d files changed or deleted); check the folder, then confirm to continue",
			lastChanged, lastFiles)
	}

	release := platform.KeepAwake()
	defer release()

	// Archive.
	if folder == "" {
		if folder, err = v.AllocFolder(info.Name); err != nil {
			return nil, errorf(EVaultWrite, err, "can't create a folder on the backup disk")
		}
	}
	dir := v.SourceDir(folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errorf(EVaultWrite, err, "can't write to the backup disk")
	}
	maxPart := opt.MaxPartSize
	if maxPart == 0 {
		maxPart = platform.MaxPartSize(dir)
	}
	base := archiveBase(dir, began, snap.Kind)
	w, err := archive.NewWriter(dir, base, maxPart, archive.Manifest{VaultID: v.Meta.ID, Source: info, Snapshot: snap}, v.Sealer())
	if err != nil {
		return nil, errorf(EVaultWrite, err, "can't write to the backup disk")
	}
	committed := false
	defer func() {
		if !committed {
			w.Abort()
		}
	}()

	sort.Slice(meta, func(i, j int) bool { return meta[i].Path < meta[j].Path })
	for _, m := range meta {
		if err := w.AddMeta(m); err != nil {
			return nil, errorf(EVaultWrite, err, "can't write to the backup disk")
		}
	}
	for _, is := range issues {
		w.AddIssue(is)
		prog.Issue(is)
	}

	rep := &Report{Plan: plan, SnapshotID: snap.ID, Issues: issues}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].e.Path < tasks[j].e.Path })
	if err := runPipeline(ctx, v, src, w, tasks, opt, rep, plan.BytesToRead); err != nil {
		return nil, err
	}

	parts, err := w.Close(opt.Now().UTC())
	if err != nil {
		return nil, errorf(EVaultWrite, err, "can't finish the archive")
	}

	// Verify every part from disk before trusting it.
	prog.Phase("verifying")
	applied := make([]catalog.AppliedPart, 0, len(parts))
	for _, p := range parts {
		sum, err := verifyNew(v, p)
		if err != nil {
			return nil, errorf(EChecksumMismatch, err, "the new archive did not verify")
		}
		fi, err := os.Stat(p.TempPath)
		if err != nil {
			return nil, errorf(EVaultWrite, err, "can't read back the archive")
		}
		applied = append(applied, catalog.AppliedPart{Filename: p.FinalName, Size: fi.Size(), SHA256: hex.EncodeToString(sum), Manifest: p.Manifest})
		rep.BytesStored += fi.Size()
	}
	rep.Verified = true

	// Commit: rename, record, publish.
	prog.Phase("saving")
	for _, p := range parts {
		final := filepath.Join(dir, p.FinalName)
		if _, err := os.Stat(final); err == nil {
			return nil, errorf(EVaultWrite, nil, "%s already exists", p.FinalName)
		}
		if err := os.Rename(p.TempPath, final); err != nil {
			return nil, errorf(EVaultWrite, err, "can't finish the archive")
		}
		rep.Archives = append(rep.Archives, folder+"/"+p.FinalName)
	}
	committed = true // from here on the archives are valid backups; recovery re-imports them
	if opt.afterRename != nil {
		if err := opt.afterRename(); err != nil {
			return nil, err
		}
	}
	if err := v.Catalog.ApplySnapshot(folder, applied); err != nil {
		return nil, errorf(EVaultWrite, err, "can't update the catalog")
	}
	if err := v.Publish(); err != nil {
		return nil, errorf(EVaultWrite, err, "can't save the catalog")
	}
	rep.Duration = opt.Now().Sub(began)
	return rep, nil
}

func runPipeline(ctx context.Context, v *vault.Vault, src *source.LocalFS, w *archive.Writer,
	tasks []task, opt BackupOptions, rep *Report, bytesTotal int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan task)
	results := make(chan result, opt.Readers)
	var wg sync.WaitGroup
	for range opt.Readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				r := readOne(ctx, src, t, opt)
				select {
				case results <- r:
				case <-ctx.Done():
					if r.c != nil {
						r.c.Spool.Close()
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, t := range tasks {
			select {
			case jobs <- t:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	prog := opt.Progress
	cp := CopyProgress{FilesTotal: len(tasks), BytesTotal: bytesTotal}
	var firstErr error
	for r := range results {
		if firstErr != nil {
			if r.c != nil {
				r.c.Spool.Close()
			}
			continue
		}
		cp.FilesDone++
		cp.BytesDone += r.t.e.Size
		cp.Current = r.t.e.Path
		prog.Copying(cp)
		if r.issue != nil {
			w.AddIssue(*r.issue)
			rep.Issues = append(rep.Issues, *r.issue)
			prog.Issue(*r.issue)
			continue
		}
		if err := store(w, r, rep); err != nil {
			firstErr = err
			cancel()
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if ctx.Err() != nil {
		return errorf(ECancelled, nil, "cancelled")
	}
	return nil
}

// store appends one read file to the archive. The only exception is a file
// whose date changed but whose content did not: it is recorded as a
// reference to the bytes already stored for the same path. Moved and copied
// files are always stored again, so that extracting the ZIPs by hand (FULL,
// then each INCREMENTAL in order) rebuilds the folder without bleen.
func store(w *archive.Writer, r result, rep *Report) error {
	defer r.c.Spool.Close()
	sha := hex.EncodeToString(r.c.SHA256[:])
	e := archive.Entry{Op: r.t.op, Path: r.t.e.Path, Kind: archive.KindFile, Size: r.size, MTime: r.mtime.UTC(), SHA256: sha, Replaces: r.t.replaces}
	rep.BytesSource += r.size

	if p := r.t.prev; p != nil && p.SHA256 == sha && p.Archive != "" {
		e.Ref = &archive.Ref{Archive: p.Archive, Zip: p.EntryName}
	}
	if e.Ref != nil {
		if err := w.AddMeta(e); err != nil {
			return errorf(EVaultWrite, err, "can't write to the backup disk")
		}
		rep.FilesDeduped++
		return nil
	}
	if !w.Fits(r.c.Blob.CompressedSize) {
		is := archive.Issue{Path: e.Path, Code: ETooLarge,
			Message: "the file is larger than this disk allows (FAT32: 4 GB per file); format the disk as exFAT or NTFS"}
		w.AddIssue(is)
		rep.Issues = append(rep.Issues, is)
		return nil
	}
	data, err := r.c.Spool.Reader()
	if err != nil {
		return errorf(EVaultWrite, err, "spool")
	}
	b := r.c.Blob
	b.Data = data
	if _, _, err := w.AddFile(e, b); err != nil {
		return errorf(EVaultWrite, err, "can't write to the backup disk")
	}
	rep.FilesStored++
	return nil
}

// readOne reads, hashes and compresses one file. It retries files that are
// locked by another program or that change while being read.
func readOne(ctx context.Context, src *source.LocalFS, t task, opt BackupOptions) result {
	fail := func(code string, err error) result {
		return result{t: t, issue: &archive.Issue{Path: t.e.Path, Code: code, Message: err.Error()}}
	}
	locked, unstable := 0, 0
	opt.Gate.Wait(ctx)
	for {
		if ctx.Err() != nil {
			return fail(ECancelled, ctx.Err())
		}
		before, err := src.Stat(t.e.Path)
		if err != nil {
			return fail(source.ErrCode(err), err)
		}
		f, err := src.Open(t.e.Path)
		if err != nil {
			if source.IsSharingViolation(err) && locked < len(opt.RetryDelays) {
				sleep(ctx, opt.RetryDelays[locked])
				locked++
				continue
			}
			return fail(source.ErrCode(err), err)
		}
		c, err := archive.Compress(f, t.e.Path, before.Size, opt.TempDir)
		f.Close()
		if err != nil {
			return fail(source.ErrCode(err), err)
		}
		after, err := src.Stat(t.e.Path)
		if err == nil && after.Size == before.Size && after.ModTime.Equal(before.ModTime) && c.Blob.UncompressedSize == before.Size {
			return result{t: t, c: c, size: before.Size, mtime: before.ModTime}
		}
		c.Spool.Close()
		if err != nil {
			return fail(source.ErrCode(err), err)
		}
		if unstable >= 2 {
			return fail(EFileUnstable, errors.New("the file kept changing while it was being read; it will be tried again next time"))
		}
		unstable++
		sleep(ctx, 500*time.Millisecond)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func fill(o *BackupOptions) {
	if o.Readers <= 0 {
		o.Readers = 4
	}
	if o.Progress == nil {
		o.Progress = NopProgress{}
	}
	if o.MassChangeRatio <= 0 {
		o.MassChangeRatio = 0.30
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.RetryDelays == nil {
		o.RetryDelays = []time.Duration{time.Second, 3 * time.Second, 10 * time.Second}
	}
	if o.MaxPartSize < 0 {
		o.MaxPartSize = 1 << 62
	}
}

func opFor(existed bool) archive.Op {
	if existed {
		return archive.OpModified
	}
	return archive.OpAdded
}

// archiveBase names an archive by local time, always with seconds. A hand
// restore applies archives in name order, so a name never sorts before an
// existing one: after a time-zone change or a clock set back, the time in
// the name is moved to one second after the newest existing archive.
func archiveBase(dir string, t time.Time, kind string) string {
	const layout = "2006-01-02_150405"
	names := map[string]bool{}
	newest := ""
	if des, err := os.ReadDir(dir); err == nil {
		for _, de := range des {
			names[de.Name()] = true
			if n := de.Name(); len(n) > len(layout) && n[len(layout)] == '_' && n[:len(layout)] > newest {
				if _, err := time.ParseInLocation(layout, n[:len(layout)], time.Local); err == nil {
					newest = n[:len(layout)]
				}
			}
		}
	}
	if newest != "" && t.Local().Format(layout) <= newest {
		n, _ := time.ParseInLocation(layout, newest, time.Local)
		for t = n.Add(time.Second); t.Local().Format(layout) <= newest; t = t.Add(time.Second) {
		}
	}
	taken := func(base string) bool {
		for n := range names {
			if strings.HasPrefix(n, base+".") {
				return true
			}
		}
		return false
	}
	base := t.Local().Format("2006-01-02_150405") + "_" + strings.ToUpper(kind)
	for n := 2; taken(base); n++ {
		base = fmt.Sprintf("%s_%s_%d", t.Local().Format("2006-01-02_150405"), strings.ToUpper(kind), n)
	}
	return base
}

// verifyNew checks a freshly written part from disk and returns the SHA-256
// of the file as stored. Plain parts are re-read entry by entry. Sealed
// parts must decrypt, byte for byte, to the ZIP that was produced, and are
// then also checked entry by entry from a temporary local copy (removed
// right after); the plain ZIP never touches the backup disk.
func verifyNew(v *vault.Vault, p archive.Part) ([]byte, error) {
	if !p.Sealed {
		return archive.VerifyPart(p.TempPath)
	}
	plain, err := v.Key.PlainSHA256(p.TempPath)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(plain, p.PlainSHA256) {
		return nil, errors.New("decrypted archive differs from what was written")
	}
	f, err := os.CreateTemp("", "bleen-open-*.zip")
	if err != nil {
		return nil, err
	}
	f.Close()
	defer os.Remove(f.Name())
	if err := v.Key.DecryptFile(p.TempPath, f.Name()); err != nil {
		return nil, err
	}
	if _, err := archive.VerifyPart(f.Name()); err != nil {
		return nil, err
	}
	return archive.FileSHA256(p.TempPath)
}
