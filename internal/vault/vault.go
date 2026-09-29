// Package vault manages the bleen folder on a backup disk: its identity,
// the lock, the catalog working copy and crash recovery.
package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/catalog"
)

const (
	FormatV1    = "bleen.vault/v1"
	SysDir      = ".bleen"
	metaFile    = "vault.json"
	catalogFile = "catalog.db"
	lockFile    = "lock"
)

var ErrNotVault = errors.New("not a bleen backup folder")

// LockedError means another bleen instance is writing to the vault.
type LockedError struct{ Holder string }

func (e *LockedError) Error() string {
	return "backup disk is in use by another bleen (" + e.Holder + ")"
}

type Meta struct {
	Format     string    `json:"format"`
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
	Encryption any       `json:"encryption"`
}

type Vault struct {
	Root    string
	Meta    Meta
	Catalog *catalog.DB

	// Recovered lists what Open repaired (stray partial files, archives
	// re-imported after a crash).
	Recovered []string

	work string
}

type OpenOptions struct {
	BreakLock bool
}

func sys(root string, name ...string) string {
	return filepath.Join(append([]string{root, SysDir}, name...)...)
}

// IsVault reports whether root contains a bleen vault.
func IsVault(root string) bool {
	_, err := os.Stat(sys(root, metaFile))
	return err == nil
}

// Create initializes a new vault at root and opens it.
func Create(root, label, createdBy string) (*Vault, error) {
	if IsVault(root) {
		return nil, fmt.Errorf("%s already contains a bleen backup folder", root)
	}
	if err := os.MkdirAll(sys(root), 0o755); err != nil {
		return nil, err
	}
	hideDir(sys(root))
	if label == "" {
		label = filepath.Base(filepath.Clean(root))
	}
	m := Meta{Format: FormatV1, ID: uuid.NewString(), Label: label, CreatedAt: time.Now().UTC(), CreatedBy: createdBy}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := writeFileSync(sys(root, metaFile), b); err != nil {
		return nil, err
	}
	if err := writeFileSync(filepath.Join(root, "README.txt"), []byte(vaultReadme)); err != nil {
		return nil, err
	}
	v, err := Open(root, OpenOptions{})
	if err != nil {
		return nil, err
	}
	return v, v.Publish()
}

// Open locks the vault, loads a working copy of its catalog and repairs
// anything an interrupted run left behind.
func Open(root string, opt OpenOptions) (*Vault, error) {
	b, err := os.ReadFile(sys(root, metaFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", root, ErrNotVault)
	}
	if err != nil {
		return nil, err
	}
	v := &Vault{Root: root}
	if err := json.Unmarshal(b, &v.Meta); err != nil {
		return nil, fmt.Errorf("vault.json: %w", err)
	}
	if v.Meta.Format != FormatV1 {
		return nil, fmt.Errorf("unsupported vault format %q", v.Meta.Format)
	}
	if err := v.lock(opt.BreakLock); err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			v.Close()
		}
	}()
	os.Remove(sys(root, catalogFile+".tmp"))
	if err := v.loadCatalog(); err != nil {
		return nil, err
	}
	if err := v.recover(); err != nil {
		return nil, fmt.Errorf("recovery: %w", err)
	}
	ok = true
	return v, nil
}

func (v *Vault) lock(breakLock bool) error {
	p := sys(v.Root, lockFile)
	host, _ := os.Hostname()
	info, _ := json.Marshal(map[string]any{"host": host, "pid": os.Getpid(), "since": time.Now().UTC()})
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = f.Write(info)
			f.Close()
			return err
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if !breakLock {
			holder, _ := os.ReadFile(p)
			return &LockedError{Holder: strings.TrimSpace(string(holder))}
		}
		os.Remove(p)
	}
	return errors.New("could not lock the backup folder")
}

func (v *Vault) loadCatalog() error {
	f, err := os.CreateTemp("", "bleen-catalog-*.db")
	if err != nil {
		return err
	}
	v.work = f.Name()
	f.Close()

	for _, name := range []string{catalogFile, catalogFile + ".1"} {
		src := sys(v.Root, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, v.work); err != nil {
			return err
		}
		db, err := catalog.Open(v.work)
		if err == nil {
			v.Catalog = db
			if name != catalogFile {
				v.Recovered = append(v.Recovered, "catalog restored from previous revision")
			}
			return nil
		}
		v.Recovered = append(v.Recovered, fmt.Sprintf("%s unreadable: %v", name, err))
	}
	// No usable catalog: start empty; recover() re-imports every archive.
	os.Remove(v.work)
	db, err := catalog.Open(v.work)
	if err != nil {
		return err
	}
	v.Catalog = db
	return nil
}

// Publish writes the working catalog to the vault atomically, keeping the
// previous revision as catalog.db.1.
func (v *Vault) Publish() error {
	tmp := sys(v.Root, catalogFile+".tmp")
	os.Remove(tmp)
	if err := v.Catalog.SnapshotTo(tmp); err != nil {
		return err
	}
	if err := syncFile(tmp); err != nil {
		return err
	}
	cur := sys(v.Root, catalogFile)
	if _, err := os.Stat(cur); err == nil {
		prev := cur + ".1"
		os.Remove(prev)
		if err := os.Rename(cur, prev); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, cur); err != nil {
		return err
	}
	syncDir(sys(v.Root))
	return nil
}

// Close releases the lock and removes the working copy.
func (v *Vault) Close() error {
	if v.Catalog != nil {
		v.Catalog.Close()
		v.Catalog = nil
	}
	if v.work != "" {
		os.Remove(v.work)
	}
	return os.Remove(sys(v.Root, lockFile))
}

// SourceDir is the folder holding a source's archives.
func (v *Vault) SourceDir(folder string) string { return filepath.Join(v.Root, folder) }

// ArchivePath resolves a vault-relative archive path.
func (v *Vault) ArchivePath(rel string) string { return filepath.Join(v.Root, filepath.FromSlash(rel)) }

// AllocFolder picks and creates a unique folder name for a new source.
func (v *Vault) AllocFolder(name string) (string, error) {
	base := SafeFolderName(name)
	for i := 1; i < 1000; i++ {
		cand := base
		if i > 1 {
			cand = fmt.Sprintf("%s (%d)", base, i)
		}
		if strings.EqualFold(cand, SysDir) {
			continue
		}
		taken, err := v.Catalog.FolderTaken(cand)
		if err != nil {
			return "", err
		}
		if taken {
			continue
		}
		if _, err := os.Stat(v.SourceDir(cand)); err == nil {
			continue
		}
		return cand, os.MkdirAll(v.SourceDir(cand), 0o755)
	}
	return "", errors.New("could not find a free folder name")
}

// SafeFolderName makes name usable as a folder on every OS.
func SafeFolderName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := strings.TrimRight(strings.TrimSpace(b.String()), ".")
	if s == "" {
		s = "source"
	}
	return s
}

// Rebuild discards the catalog and re-imports every archive's manifest.
func (v *Vault) Rebuild() error {
	v.Catalog.Close()
	os.Remove(v.work)
	db, err := catalog.Open(v.work)
	if err != nil {
		return err
	}
	v.Catalog = db
	n, err := v.importArchives()
	if err != nil {
		return err
	}
	v.Recovered = append(v.Recovered, fmt.Sprintf("catalog rebuilt from %d backups", n))
	return v.Publish()
}

// recover deletes unfinished archives and imports archives that were
// written but never recorded (a crash between rename and catalog publish).
func (v *Vault) recover() error {
	partials, _ := filepath.Glob(filepath.Join(v.Root, "*", "*.partial"))
	for _, p := range partials {
		if err := os.Remove(p); err == nil {
			v.Recovered = append(v.Recovered, "removed unfinished "+filepath.Base(p))
		}
	}
	n, err := v.importArchives()
	if err != nil {
		return err
	}
	if n > 0 {
		v.Recovered = append(v.Recovered, fmt.Sprintf("recorded %d backup(s) that were missing from the catalog", n))
		return v.Publish()
	}
	return nil
}

type pendingSnapshot struct {
	folder string
	parts  []catalog.AppliedPart
}

func (v *Vault) importArchives() (int, error) {
	known := map[string]bool{}
	list, err := v.Catalog.Archives()
	if err != nil {
		return 0, err
	}
	for _, a := range list {
		known[strings.ToLower(a.Rel)] = true
	}
	zips, _ := filepath.Glob(filepath.Join(v.Root, "*", "*.zip"))
	pending := map[string]*pendingSnapshot{}
	for _, z := range zips {
		folder := filepath.Base(filepath.Dir(z))
		rel := folder + "/" + filepath.Base(z)
		if strings.EqualFold(folder, SysDir) || known[strings.ToLower(rel)] {
			continue
		}
		m, err := archive.ReadManifest(z)
		if err != nil {
			v.Recovered = append(v.Recovered, fmt.Sprintf("skipped %s: %v", rel, err))
			continue
		}
		if m.VaultID != v.Meta.ID {
			v.Recovered = append(v.Recovered, fmt.Sprintf("skipped %s: belongs to another vault", rel))
			continue
		}
		fi, err := os.Stat(z)
		if err != nil {
			return 0, err
		}
		sum, err := archive.FileSHA256(z)
		if err != nil {
			return 0, err
		}
		ps := pending[m.Snapshot.ID]
		if ps == nil {
			ps = &pendingSnapshot{folder: folder}
			pending[m.Snapshot.ID] = ps
		}
		ps.parts = append(ps.parts, catalog.AppliedPart{
			Filename: filepath.Base(z), Size: fi.Size(), SHA256: fmt.Sprintf("%x", sum), Manifest: m,
		})
	}
	snaps := make([]*pendingSnapshot, 0, len(pending))
	for _, ps := range pending {
		sort.Slice(ps.parts, func(i, j int) bool { return ps.parts[i].Manifest.Part.Index < ps.parts[j].Manifest.Part.Index })
		snaps = append(snaps, ps)
	}
	// Oldest first, so references and sequence numbers resolve in order.
	sort.Slice(snaps, func(i, j int) bool {
		a, b := snaps[i].parts[0].Manifest.Snapshot, snaps[j].parts[0].Manifest.Snapshot
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.Before(b.StartedAt)
		}
		return a.Seq < b.Seq
	})
	n := 0
	for _, ps := range snaps {
		last := ps.parts[len(ps.parts)-1].Manifest.Part
		if !last.Last || len(ps.parts) != last.Index {
			v.Recovered = append(v.Recovered, fmt.Sprintf("skipped incomplete backup in %s (missing parts)", ps.folder))
			continue
		}
		err := v.Catalog.ApplySnapshot(ps.folder, ps.parts)
		if errors.Is(err, catalog.ErrSnapshotExists) {
			continue
		}
		if err != nil {
			v.Recovered = append(v.Recovered, fmt.Sprintf("could not import %s/%s: %v", ps.folder, ps.parts[0].Filename, err))
			continue
		}
		n++
	}
	return n, nil
}

func writeFileSync(p string, b []byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func syncFile(p string) error {
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

const vaultReadme = `This folder contains backups made by bleen.
Bu klasör bleen ile alınmış yedekleri içerir.

Each subfolder is one backed-up location. Every backup is a normal ZIP file.
Her alt klasör yedeklenen bir konumdur. Her yedek normal bir ZIP dosyasıdır.

To restore without bleen / bleen olmadan geri yüklemek için:
 1. Extract the *_FULL.zip file.            *_FULL.zip dosyasını çıkarın.
 2. Extract each *_INCREMENTAL.zip after it, oldest first, overwriting files.
    Ardından *_INCREMENTAL.zip dosyalarını eskiden yeniye, üzerine yazarak çıkarın.
 3. Delete the files listed in each archive's DELETED.txt.
    Her arşivdeki DELETED.txt içinde listelenen dosyaları silin.

Do not rename or edit files in the .bleen folder.
.bleen klasöründeki dosyaları değiştirmeyin.
`
