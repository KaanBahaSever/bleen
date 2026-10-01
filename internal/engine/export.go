package engine

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/vault"
)

type ExportOptions struct {
	Snapshot string    // snapshot id or id prefix; overrides Before
	Before   time.Time // newest snapshot that started before this; zero = latest
	Dest     string    // the ZIP file to create; must not exist
	Paths    []string  // export only these files/folders (empty = everything)
	Progress Progress
}

// Export writes the state of a source at one snapshot into a single new
// ZIP file, with paths relative to the source folder. Compressed data is
// copied as is; every file is checked against its SHA-256 first. The ZIP
// is never encrypted, even when the vault is.
func Export(ctx context.Context, v *vault.Vault, src *catalog.Source, opt ExportOptions) (*RestoreReport, error) {
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
	if _, err := os.Lstat(dest); err == nil {
		return nil, errorf(EDestExists, nil, "%s already exists", dest)
	}
	if insideDir(v.Root, dest) {
		return nil, errorf(EDestInVault, nil, "%s is inside the backup folder", dest)
	}
	versions, err := stateFor(v, src, snap, opt.Paths)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(dest), ".bleen-export-*.partial")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(f)

	rep := &RestoreReport{Snapshot: *snap, Dest: dest}
	issue := func(p, code string, err error) {
		is := archive.Issue{Path: p, Code: code, Message: err.Error()}
		rep.Issues = append(rep.Issues, is)
		prog.Issue(is)
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
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Path < dirs[j].Path })
	for _, d := range dirs {
		fh := &zip.FileHeader{Name: d.Path + "/", Method: zip.Store}
		if d.MTimeNS != 0 {
			fh.Modified = time.Unix(0, d.MTimeNS)
		}
		fh.SetMode(os.ModeDir | 0o755)
		if _, err := zw.CreateHeader(fh); err != nil {
			return nil, err
		}
		rep.Dirs++
	}
	for _, l := range links {
		fh := &zip.FileHeader{Name: l.Path, Method: zip.Store}
		fh.SetMode(os.ModeSymlink | 0o777)
		w, err := zw.CreateHeader(fh)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, l.LinkTarget); err != nil {
			return nil, err
		}
	}

	cp := CopyProgress{FilesTotal: len(versions) - len(dirs) - len(links), BytesTotal: bytesTotal}
	names := make([]string, 0, len(byArchive))
	for a := range byArchive {
		names = append(names, a)
	}
	sort.Strings(names)
	for _, a := range names {
		if ctx.Err() != nil {
			return nil, errorf(ECancelled, nil, "cancelled")
		}
		plain, cleanup, err := archive.Plain(v.ArchivePath(a), v.Opener(), "")
		if err != nil {
			for _, it := range byArchive[a] {
				issue(it.Path, "E_ARCHIVE_MISSING", err)
			}
			continue
		}
		err = exportArchive(ctx, zw, plain, byArchive[a], issue, func(ver catalog.Version) {
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
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, err
	}
	ok = true
	return rep, nil
}

func exportArchive(ctx context.Context, zw *zip.Writer, path string, items []catalog.Version,
	issue func(string, string, error), done func(catalog.Version)) error {
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
	for _, it := range items {
		if ctx.Err() != nil {
			return errorf(ECancelled, nil, "cancelled")
		}
		f := byName[it.EntryName]
		if f == nil {
			issue(it.Path, "E_ENTRY_MISSING", fmt.Errorf("%s is missing from %s", it.EntryName, filepath.Base(path)))
			continue
		}
		if err := checkEntry(f, it); err != nil {
			issue(it.Path, EChecksumMismatch, err)
			continue
		}
		var mt time.Time
		if it.MTimeNS != 0 {
			mt = time.Unix(0, it.MTimeNS)
		}
		if err := archive.CopyEntry(zw, f, it.Path, mt); err != nil {
			return errorf(EWriteFailed, err, "writing %s", it.Path)
		}
		done(it)
	}
	return nil
}

// checkEntry reads a stored file once and compares its SHA-256.
func checkEntry(f *zip.File, it catalog.Version) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return err
	}
	if it.SHA256 != "" && hex.EncodeToString(h.Sum(nil)) != it.SHA256 {
		return errChecksum
	}
	return nil
}
