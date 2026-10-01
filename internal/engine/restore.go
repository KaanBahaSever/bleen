package engine

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/vault"
)

type RestoreOptions struct {
	Snapshot  string    // snapshot id or id prefix; overrides Before
	Before    time.Time // newest snapshot that started before this; zero = latest
	Dest      string
	Overwrite bool     // allow a non-empty destination
	Paths     []string // restore only these files/folders (empty = everything)
	Progress  Progress
}

type RestoreReport struct {
	Snapshot catalog.Snapshot
	Dest     string
	Files    int
	Dirs     int
	Bytes    int64
	Issues   []archive.Issue
}

// SelectSnapshot picks the snapshot a restore would use.
func SelectSnapshot(v *vault.Vault, src *catalog.Source, id string, before time.Time) (*catalog.Snapshot, error) {
	snaps, err := v.Catalog.Snapshots(src.ID)
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, errorf(ENotFound, nil, "%s has no backups yet", src.Name)
	}
	var pick *catalog.Snapshot
	for i := range snaps {
		s := &snaps[i]
		switch {
		case id != "":
			if strings.HasPrefix(s.UUID, id) {
				if pick != nil {
					return nil, errorf(ENotFound, nil, "backup id %q is ambiguous", id)
				}
				pick = s
			}
		case before.IsZero() || s.StartedAt.Before(before):
			pick = s // snapshots are ordered oldest first
		}
	}
	if pick == nil {
		return nil, errorf(ENotFound, nil, "no backup of %s matches", src.Name)
	}
	return pick, nil
}

// Restore rebuilds a source folder exactly as it was at one snapshot. The
// final state is resolved first, so every file is extracted exactly once.
func Restore(ctx context.Context, v *vault.Vault, src *catalog.Source, opt RestoreOptions) (*RestoreReport, error) {
	prog := opt.Progress
	if prog == nil {
		prog = NopProgress{}
	}
	snap, err := SelectSnapshot(v, src, opt.Snapshot, opt.Before)
	if err != nil {
		return nil, err
	}
	dest, err := filepath.Abs(opt.Dest)
	if err != nil {
		return nil, err
	}
	if des, err := os.ReadDir(dest); err == nil && len(des) > 0 && !opt.Overwrite {
		return nil, errorf(EDestNotEmpty, nil, "%s is not empty; choose an empty folder or allow overwriting", dest)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	versions, err := v.Catalog.StateAt(src.ID, snap.GenerationID, snap.Seq)
	if err != nil {
		return nil, err
	}
	if len(opt.Paths) > 0 {
		versions = selectPaths(versions, opt.Paths)
		if len(versions) == 0 {
			return nil, errorf(ENotFound, nil, "none of the selected paths exist in this backup")
		}
	}

	rep := &RestoreReport{Snapshot: *snap, Dest: dest}
	issue := func(p, code string, err error) {
		is := archive.Issue{Path: p, Code: code, Message: err.Error()}
		rep.Issues = append(rep.Issues, is)
		prog.Issue(is)
	}
	used := map[string]string{} // target (folded on Windows/macOS) → source path
	fold := func(s string) string {
		if runtime.GOOS != "linux" {
			return strings.ToLower(s)
		}
		return s
	}
	target := func(p string) (string, bool) {
		abs, renamed, err := SafeJoin(dest, p)
		if err != nil {
			issue(p, "E_UNSAFE_PATH", err)
			return "", false
		}
		// Two backed-up names can map to one name here (a:b and a_b, or
		// Foo and foo from a Linux server): never let one overwrite the other.
		if prev, ok := used[fold(abs)]; ok && prev != p {
			ext := filepath.Ext(abs)
			for n := 2; ; n++ {
				cand := fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(abs, ext), n, ext)
				if _, taken := used[fold(cand)]; !taken {
					abs, renamed = cand, true
					break
				}
			}
		}
		used[fold(abs)] = p
		if renamed {
			issue(p, ERenamed, fmt.Errorf("restored as %s (name not allowed on this system or already used)", abs))
		}
		return abs, true
	}

	prog.Phase("restoring")
	byArchive := map[string][]catalog.Version{}
	var dirs, links []catalog.Version
	var bytesTotal int64
	for _, ver := range versions {
		switch ver.Kind {
		case archive.KindDir:
			dirs = append(dirs, ver)
		case archive.KindSymlink:
			links = append(links, ver)
		default:
			byArchive[ver.Archive] = append(byArchive[ver.Archive], ver)
			bytesTotal += ver.Size
		}
	}
	for _, d := range dirs {
		if p, ok := target(d.Path); ok {
			if err := os.MkdirAll(p, 0o755); err != nil {
				issue(d.Path, "E_WRITE_FAILED", err)
			}
		}
	}
	cp := CopyProgress{FilesTotal: len(versions) - len(dirs) - len(links), BytesTotal: bytesTotal}
	names := make([]string, 0, len(byArchive))
	for a := range byArchive {
		names = append(names, a)
	}
	sort.Strings(names)
	for _, a := range names {
		plain, cleanup, err := archive.Plain(v.ArchivePath(a), v.Opener(), "")
		if err != nil {
			for _, it := range byArchive[a] {
				issue(it.Path, "E_ARCHIVE_MISSING", err)
			}
			continue
		}
		err = extractArchive(ctx, plain, byArchive[a], target, issue, func(ver catalog.Version) {
			rep.Files++
			rep.Bytes += ver.Size
			cp.FilesDone++
			cp.BytesDone += ver.Size
			cp.Current = ver.Path
			prog.Copying(cp)
		})
		cleanup()
		if err != nil {
			return nil, err
		}
	}
	for _, l := range links {
		if p, ok := target(l.Path); ok {
			os.Remove(p)
			if err := os.Symlink(filepath.FromSlash(l.LinkTarget), p); err != nil {
				issue(l.Path, "E_SYMLINK", err)
			}
		}
	}
	// Directory times last, deepest first, so file writes don't change them.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i].Path) > len(dirs[j].Path) })
	for _, d := range dirs {
		if d.MTimeNS == 0 {
			continue
		}
		if p, _, err := SafeJoin(dest, d.Path); err == nil {
			t := time.Unix(0, d.MTimeNS)
			os.Chtimes(p, t, t)
		}
		rep.Dirs++
	}
	return rep, ctx.Err()
}

func extractArchive(ctx context.Context, path string, items []catalog.Version,
	target func(string) (string, bool), issue func(string, string, error), done func(catalog.Version)) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		for _, it := range items {
			issue(it.Path, "E_ARCHIVE_MISSING", err)
		}
		return nil
	}
	defer zr.Close()
	byName := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	// Read in on-disk order: sequential I/O matters on USB hard disks.
	offset := func(v catalog.Version) int64 {
		if f := byName[v.EntryName]; f != nil {
			o, _ := f.DataOffset()
			return o
		}
		return 0
	}
	sort.Slice(items, func(i, j int) bool { return offset(items[i]) < offset(items[j]) })
	for _, it := range items {
		if ctx.Err() != nil {
			return errorf(ECancelled, nil, "cancelled")
		}
		f := byName[it.EntryName]
		if f == nil {
			issue(it.Path, "E_ENTRY_MISSING", fmt.Errorf("%s is missing from %s", it.EntryName, filepath.Base(path)))
			continue
		}
		dst, ok := target(it.Path)
		if !ok {
			continue
		}
		if err := extractOne(f, dst, it); err != nil {
			code := "E_WRITE_FAILED"
			if errors.Is(err, errChecksum) || errors.Is(err, zip.ErrChecksum) {
				code = EChecksumMismatch
			}
			issue(it.Path, code, err)
			continue
		}
		done(it)
	}
	return nil
}

// selectPaths keeps the chosen files and folders (with their contents).
func selectPaths(all []catalog.Version, paths []string) []catalog.Version {
	var out []catalog.Version
	norm := func(s string) string {
		s = strings.Trim(strings.ReplaceAll(s, `\`, "/"), "/")
		if runtime.GOOS != "linux" {
			s = strings.ToLower(s)
		}
		return s
	}
	for _, v := range all {
		vp := norm(v.Path)
		for _, p := range paths {
			p = norm(p)
			if vp == p || strings.HasPrefix(vp, p+"/") {
				out = append(out, v)
				break
			}
		}
	}
	return out
}

var errChecksum = errors.New("restored file does not match its SHA-256")

func extractOne(f *zip.File, dst string, it catalog.Version) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := dst + ".bleen-tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && it.SHA256 != "" && hex.EncodeToString(h.Sum(nil)) != it.SHA256 {
		err = errChecksum
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	if it.MTimeNS != 0 {
		t := time.Unix(0, it.MTimeNS)
		os.Chtimes(dst, t, t)
	}
	return nil
}

var reservedWin = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}

// SafeJoin joins a vault-relative path under dest. It refuses anything that
// could escape dest and, on Windows, renames components the OS forbids.
func SafeJoin(dest, rel string) (string, bool, error) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.ContainsRune(rel, 0) {
		return "", false, fmt.Errorf("unsafe path %q", rel)
	}
	parts := strings.Split(rel, "/")
	renamed := false
	for i, c := range parts {
		if c == "" || c == "." || c == ".." {
			return "", false, fmt.Errorf("unsafe path %q", rel)
		}
		if runtime.GOOS == "windows" {
			fixed := fixWindowsName(c)
			if fixed != c {
				parts[i], renamed = fixed, true
			}
		}
	}
	out := filepath.Join(append([]string{dest}, parts...)...)
	if r, err := filepath.Rel(dest, out); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false, fmt.Errorf("unsafe path %q", rel)
	}
	return out, renamed, nil
}

func fixWindowsName(c string) string {
	var b strings.Builder
	for _, r := range c {
		if r < 32 || strings.ContainsRune(`<>:"\|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") {
		s = s[:len(s)-1] + "_"
	}
	stem := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if reservedWin[stem] {
		s = "_" + s
	}
	return s
}
