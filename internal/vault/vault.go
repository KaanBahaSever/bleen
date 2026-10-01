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
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/seal"
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
	Format     string      `json:"format"`
	ID         string      `json:"id"`
	Label      string      `json:"label"`
	CreatedAt  time.Time   `json:"created_at"`
	CreatedBy  string      `json:"created_by"`
	Encryption *Encryption `json:"encryption"`
}

// Encryption describes an encrypted vault (see internal/seal).
type Encryption struct {
	Type      string `json:"type"` // "age-x25519"
	Recipient string `json:"recipient"`
}

// ErrPasswordRequired means the vault is encrypted and no key was given.
var ErrPasswordRequired = errors.New("E_PASSWORD_REQUIRED")

const identityFile = "identity.age"

type Vault struct {
	Root    string
	Meta    Meta
	Catalog *catalog.DB

	// Recovered lists what Open repaired (stray partial files, archives
	// re-imported after a crash).
	Recovered []string

	// Key is set for encrypted vaults once unlocked.
	Key *seal.Key

	work     string
	lockData []byte // contents of the lock file we wrote
}

type OpenOptions struct {
	BreakLock bool
	Password  string    // for encrypted vaults
	Key       *seal.Key // alternative to Password (already unlocked)
}

// Encrypted reports whether the vault's archives and catalog are encrypted.
func (m Meta) Encrypted() bool { return m.Encryption != nil }

// ReadMeta reads vault.json without locking or unlocking the vault.
func ReadMeta(root string) (Meta, error) {
	var m Meta
	b, err := os.ReadFile(sys(root, metaFile))
	if errors.Is(err, os.ErrNotExist) {
		return m, fmt.Errorf("%s: %w", root, ErrNotVault)
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("vault.json: %w", err)
	}
	if m.Format != FormatV1 {
		return m, fmt.Errorf("unsupported vault format %q", m.Format)
	}
	return m, nil
}

// Unlock checks a password against an encrypted vault and returns its key.
func Unlock(root, password string) (*seal.Key, error) {
	b, err := os.ReadFile(sys(root, identityFile))
	if err != nil {
		return nil, err
	}
	return seal.Unlock(b, password)
}

// Sealer encrypts new archives, or is nil for a plain vault.
func (v *Vault) Sealer() archive.Sealer {
	if v.Key == nil {
		return nil
	}
	return v.Key
}

// Opener decrypts archives, or is nil for a plain vault.
func (v *Vault) Opener() archive.Opener {
	if v.Key == nil {
		return nil
	}
	return v.Key
}

func (v *Vault) catalogName() string {
	if v.Meta.Encrypted() {
		return catalogFile + archive.SealedExt
	}
	return catalogFile
}

func sys(root string, name ...string) string {
	return filepath.Join(append([]string{root, SysDir}, name...)...)
}

// IsVault reports whether root contains a bleen vault.
func IsVault(root string) bool {
	_, err := os.Stat(sys(root, metaFile))
	return err == nil
}

// Create initializes a new vault at root and opens it. A non-empty password
// makes it an encrypted vault.
func Create(root, label, createdBy, password string) (*Vault, error) {
	if IsVault(root) {
		return nil, fmt.Errorf("%s already contains a bleen backup folder", root)
	}
	// A .bleen folder without vault.json still holds the encryption key or
	// the catalog of earlier backups: never overwrite it.
	for _, name := range []string{identityFile, catalogFile, catalogFile + archive.SealedExt} {
		if _, err := os.Stat(sys(root, name)); err == nil {
			return nil, fmt.Errorf("E_VAULT_DAMAGED: %s has an earlier bleen backup folder whose vault.json is missing; it was left untouched", root)
		}
	}
	if err := os.MkdirAll(sys(root), 0o755); err != nil {
		return nil, err
	}
	hideDir(sys(root))
	if label == "" {
		label = filepath.Base(filepath.Clean(root))
	}
	m := Meta{Format: FormatV1, ID: uuid.NewString(), Label: label, CreatedAt: time.Now().UTC(), CreatedBy: createdBy}
	var key *seal.Key
	if password != "" {
		k, idFile, err := seal.NewKey(password)
		if err != nil {
			return nil, err
		}
		if err := writeFileExcl(sys(root, identityFile), idFile); err != nil {
			return nil, err
		}
		key = k
		m.Encryption = &Encryption{Type: "age-x25519", Recipient: k.Recipient()}
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := writeFileExcl(sys(root, metaFile), b); err != nil {
		return nil, err
	}
	if err := writeFileSync(filepath.Join(root, "README.txt"), []byte(vaultReadme)); err != nil {
		return nil, err
	}
	v, err := Open(root, OpenOptions{Key: key})
	if err != nil {
		return nil, err
	}
	if err := v.Publish(); err != nil {
		v.Close()
		return nil, err
	}
	return v, nil
}

// Open locks the vault, loads a working copy of its catalog and repairs
// anything an interrupted run left behind.
func Open(root string, opt OpenOptions) (*Vault, error) {
	meta, err := ReadMeta(root)
	if err != nil {
		return nil, err
	}
	v := &Vault{Root: root, Meta: meta}
	if meta.Encrypted() {
		switch {
		case opt.Key != nil:
			v.Key = opt.Key
		case opt.Password != "":
			if v.Key, err = Unlock(root, opt.Password); err != nil {
				return nil, err
			}
		default:
			return nil, ErrPasswordRequired
		}
		if v.Key.Recipient() != meta.Encryption.Recipient {
			return nil, seal.ErrWrongPassword
		}
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
	if err := v.loadCatalog(); err != nil {
		return nil, err
	}
	if err := v.recover(); err != nil {
		return nil, fmt.Errorf("recovery: %w", err)
	}
	ok = true
	return v, nil
}

type lockInfo struct {
	Host  string    `json:"host"`
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

func (v *Vault) lock(breakLock bool) error {
	p := sys(v.Root, lockFile)
	host, _ := os.Hostname()
	mine, _ := json.Marshal(lockInfo{Host: host, PID: os.Getpid(), Since: time.Now().UTC()})
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = f.Write(mine)
			f.Close()
			v.lockData = mine
			return err
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		holder, _ := os.ReadFile(p)
		var li lockInfo
		stale := json.Unmarshal(holder, &li) == nil && strings.EqualFold(li.Host, host) && li.PID > 0 && !processAlive(li.PID)
		if !breakLock && !stale {
			return &LockedError{Holder: strings.TrimSpace(string(holder))}
		}
		if stale {
			v.Recovered = append(v.Recovered, "removed a lock left by a bleen that is no longer running")
		}
		os.Remove(p)
	}
	return errors.New("could not lock the backup folder")
}

// BreakLock removes a vault's lock file. Only for locks left by a bleen
// that is certainly not running any more (e.g. on another computer).
func BreakLock(root string) error {
	err := os.Remove(sys(root, lockFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// readCatalogFile copies (or decrypts) a catalog file into the working copy
// and opens it.
func (v *Vault) readCatalogFile(src string) (*catalog.DB, error) {
	os.Remove(v.work)
	var err error
	if v.Key != nil {
		err = v.Key.DecryptFile(src, v.work)
	} else {
		err = copyFile(src, v.work)
	}
	if err != nil {
		return nil, err
	}
	return catalog.Open(v.work)
}

func (v *Vault) loadCatalog() error {
	f, err := os.CreateTemp("", "bleen-catalog-*.db")
	if err != nil {
		return err
	}
	v.work = f.Name()
	f.Close()

	cur := sys(v.Root, v.catalogName())
	if _, err := os.Stat(cur); err == nil {
		// The published catalog exists: it must be readable. Never fall
		// back silently, or the next publish would replace a good catalog
		// that is only unreadable right now (newer bleen, read error).
		db, err := v.readCatalogFile(cur)
		if err != nil {
			return fmt.Errorf("E_CATALOG_UNREADABLE: the catalog on the backup disk can't be read (%v). Your backups are untouched; update bleen, or run 'bleenctl rebuild-catalog'", err)
		}
		v.Catalog = db
		os.Remove(cur + ".tmp")
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// No current catalog: a publish was interrupted between its two renames.
	// The newest complete copy is the .tmp, then the previous revision.
	for _, name := range []string{cur + ".tmp", cur + ".1"} {
		if _, err := os.Stat(name); err != nil {
			continue
		}
		if db, err := v.readCatalogFile(name); err == nil {
			v.Catalog = db
			v.Recovered = append(v.Recovered, "catalog restored from "+filepath.Base(name))
			return nil
		} else {
			v.Recovered = append(v.Recovered, fmt.Sprintf("%s unreadable: %v", filepath.Base(name), err))
		}
	}
	// No catalog at all: start empty; recover() re-imports every archive.
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
	tmp := sys(v.Root, v.catalogName()+".tmp")
	os.Remove(tmp)
	if v.Key != nil {
		// Snapshot to the local disk, then seal onto the vault: the plain
		// catalog (file names!) never touches the backup disk.
		plain := v.work + ".snap"
		os.Remove(plain)
		err := v.Catalog.SnapshotTo(plain)
		if err == nil {
			err = v.Key.EncryptFile(plain, tmp)
		}
		os.Remove(plain)
		if err != nil {
			return err
		}
	} else {
		if err := v.Catalog.SnapshotTo(tmp); err != nil {
			return err
		}
		if err := syncFile(tmp); err != nil {
			return err
		}
	}
	cur := sys(v.Root, v.catalogName())
	if _, err := os.Stat(cur); err == nil {
		prev := cur + ".1"
		os.Remove(prev)
		if err := renameRetry(cur, prev); err != nil {
			return err
		}
	}
	if err := renameRetry(tmp, cur); err != nil {
		return err
	}
	syncDir(sys(v.Root))
	return nil
}

// renameRetry retries briefly: antivirus scanners often hold new files open
// for a moment on Windows.
func renameRetry(from, to string) error {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return err
}

// Close releases the lock (only if it is still ours) and removes the
// working copy.
func (v *Vault) Close() error {
	if v.Catalog != nil {
		v.Catalog.Close()
		v.Catalog = nil
	}
	if v.work != "" {
		os.Remove(v.work)
		os.Remove(v.work + ".snap")
	}
	if v.lockData == nil {
		return nil
	}
	p := sys(v.Root, lockFile)
	if cur, err := os.ReadFile(p); err == nil && string(cur) == string(v.lockData) {
		v.lockData = nil
		return os.Remove(p)
	}
	return nil
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

var reservedNames = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}

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
	s := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if s == "" {
		s = "source"
	}
	if reservedNames[strings.ToUpper(strings.SplitN(s, ".", 2)[0])] {
		s = "_" + s
	}
	return s
}

// Rebuild discards the catalog and re-imports every archive's manifest. If
// any archive cannot be imported, nothing is published and the current
// catalog stays as it is.
func (v *Vault) Rebuild() error {
	v.Catalog.Close()
	os.Remove(v.work)
	db, err := catalog.Open(v.work)
	if err != nil {
		return err
	}
	v.Catalog = db
	n, failed, err := v.importArchives()
	if err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d archive(s) could not be imported, so the catalog was not replaced: %s",
			failed, strings.Join(v.Recovered, "; "))
	}
	v.Recovered = append(v.Recovered, fmt.Sprintf("catalog rebuilt from %d backups", n))
	return v.Publish()
}

// archiveName matches the archive files bleen writes, so recovery never
// touches anything else in the backup folder.
var archiveName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{4,6}_(FULL|INCREMENTAL)(_\d+)?(\.part\d{2})?\.zip(\.age)?(\.partial)?$`)

// sourceDirs lists the vault's source folders (not glob-based: folder names
// may contain [ or *).
func (v *Vault) sourceDirs() []string {
	des, _ := os.ReadDir(v.Root)
	var out []string
	for _, de := range des {
		if de.IsDir() && !strings.EqualFold(de.Name(), SysDir) {
			out = append(out, de.Name())
		}
	}
	return out
}

// recover finishes or removes unfinished archives and imports archives that
// were written but never recorded (a crash between rename and publish).
func (v *Vault) recover() error {
	known := map[string]string{} // lower-case rel → sha256
	if list, err := v.Catalog.Archives(); err == nil {
		for _, a := range list {
			known[strings.ToLower(a.Rel)] = a.SHA256
		}
	}
	for _, folder := range v.sourceDirs() {
		des, _ := os.ReadDir(v.SourceDir(folder))
		for _, de := range des {
			name := de.Name()
			if !strings.HasSuffix(name, ".partial") || !archiveName.MatchString(name) {
				continue
			}
			p := filepath.Join(v.SourceDir(folder), name)
			final := strings.TrimSuffix(name, ".partial")
			// The catalog may already list this archive (its rename was lost
			// on a cached removable disk): finish the rename instead.
			restored := false
			for _, cand := range []string{final, strings.Replace(final, ".part01.", ".", 1)} {
				want, ok := known[strings.ToLower(folder+"/"+cand)]
				if !ok {
					continue
				}
				if _, err := os.Stat(filepath.Join(v.SourceDir(folder), cand)); err == nil {
					continue
				}
				if sum, err := archive.FileSHA256(p); err == nil && fmt.Sprintf("%x", sum) == want {
					if os.Rename(p, filepath.Join(v.SourceDir(folder), cand)) == nil {
						v.Recovered = append(v.Recovered, "finished saving "+cand)
						restored = true
						break
					}
				}
			}
			if !restored && os.Remove(p) == nil {
				v.Recovered = append(v.Recovered, "removed unfinished "+name)
			}
		}
	}
	n, _, err := v.importArchives()
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
	paths  []string
}

// importArchives records archives on disk that the catalog does not know.
// It returns how many snapshots were imported and how many archives failed.
func (v *Vault) importArchives() (imported, failed int, err error) {
	known := map[string]bool{}
	list, err := v.Catalog.Archives()
	if err != nil {
		return 0, 0, err
	}
	for _, a := range list {
		known[strings.ToLower(a.Rel)] = true
	}
	pending := map[string]*pendingSnapshot{}
	for _, folder := range v.sourceDirs() {
		des, _ := os.ReadDir(v.SourceDir(folder))
		for _, de := range des {
			name := de.Name()
			rel := folder + "/" + name
			if de.IsDir() || strings.HasSuffix(name, ".partial") || !archiveName.MatchString(name) || known[strings.ToLower(rel)] {
				continue
			}
			z := filepath.Join(v.SourceDir(folder), name)
			m, err := archive.ReadManifestAny(z, v.Opener())
			if err != nil {
				failed++
				v.Recovered = append(v.Recovered, fmt.Sprintf("skipped %s: %v", rel, err))
				continue
			}
			if m.VaultID != v.Meta.ID {
				v.Recovered = append(v.Recovered, fmt.Sprintf("skipped %s: belongs to another vault", rel))
				continue
			}
			fi, err := os.Stat(z)
			if err != nil {
				return 0, 0, err
			}
			sum, err := archive.FileSHA256(z)
			if err != nil {
				return 0, 0, err
			}
			ps := pending[m.Snapshot.ID]
			if ps == nil {
				ps = &pendingSnapshot{folder: folder}
				pending[m.Snapshot.ID] = ps
			}
			ps.parts = append(ps.parts, catalog.AppliedPart{Filename: name, Size: fi.Size(), SHA256: fmt.Sprintf("%x", sum), Manifest: m})
			ps.paths = append(ps.paths, z)
		}
	}
	snaps := make([]*pendingSnapshot, 0, len(pending))
	for _, ps := range pending {
		sort.Slice(ps.parts, func(i, j int) bool { return ps.parts[i].Manifest.Part.Index < ps.parts[j].Manifest.Part.Index })
		snaps = append(snaps, ps)
	}
	// Generation and sequence order, not clock order: a wrong system clock
	// must not make a rebuild drop snapshots.
	sort.Slice(snaps, func(i, j int) bool {
		a, b := snaps[i].parts[0].Manifest, snaps[j].parts[0].Manifest
		if a.Source.ID != b.Source.ID {
			return a.Source.ID < b.Source.ID
		}
		if a.Snapshot.Generation != b.Snapshot.Generation {
			return a.Snapshot.Generation < b.Snapshot.Generation
		}
		return a.Snapshot.Seq < b.Snapshot.Seq
	})
	for _, ps := range snaps {
		complete := ps.parts[len(ps.parts)-1].Manifest.Part.Last
		for i, p := range ps.parts {
			if p.Manifest.Part.Index != i+1 {
				complete = false
			}
		}
		if !complete {
			// Never committed (the catalog doesn't know it) and unusable: move
			// it aside so that extracting archives by hand stays correct.
			aside := sys(v.Root, "incomplete", ps.folder)
			os.MkdirAll(aside, 0o755)
			for _, p := range ps.paths {
				os.Rename(p, filepath.Join(aside, filepath.Base(p)))
			}
			v.Recovered = append(v.Recovered, fmt.Sprintf("moved an incomplete backup of %s to .bleen/incomplete", ps.folder))
			continue
		}
		err := v.Catalog.ApplySnapshot(ps.folder, ps.parts)
		if errors.Is(err, catalog.ErrSnapshotExists) {
			continue
		}
		if err != nil {
			failed += len(ps.parts)
			v.Recovered = append(v.Recovered, fmt.Sprintf("could not import %s/%s: %v", ps.folder, ps.parts[0].Filename, err))
			continue
		}
		imported++
	}
	return imported, failed, nil
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

// writeFileExcl creates a new file and fails if it already exists.
func writeFileExcl(p string, b []byte) error {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
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
 1. Extract the newest *_FULL.zip file.     En yeni *_FULL.zip dosyasını çıkarın.
 2. For each later *_INCREMENTAL.zip, in name order (oldest first):
    Sonraki her *_INCREMENTAL.zip için, ad sırasıyla (eskiden yeniye):
    a. extract it, overwriting files           üzerine yazarak çıkarın,
    b. then delete the files listed in its DELETED.txt
       ardından içindeki DELETED.txt'de yazan dosyaları silin.

Encrypted backups end with .zip.age. Open them with the free "age" tool
(https://age-encryption.org) and your password:
Şifreli yedekler .zip.age ile biter. Ücretsiz "age" aracı ve parolanla açılır:
   age -d .bleen/identity.age > key.txt        (asks for the password / parolayı sorar)
   age -d -i key.txt FILE.zip.age > FILE.zip
Delete key.txt afterwards. / Sonra key.txt dosyasını sil.

Do not rename or edit files in the .bleen folder.
.bleen klasöründeki dosyaları değiştirmeyin.
`
