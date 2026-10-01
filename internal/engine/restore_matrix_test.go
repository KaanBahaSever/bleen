package engine

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// zipTree reads an exported ZIP the way an unzip tool would. Reading every
// entry also checks its CRC-32.
func zipTree(t *testing.T, p string) (tree, map[string]time.Time) {
	t.Helper()
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	tr := tree{files: map[string][]byte{}, dirs: map[string]bool{}}
	mt := map[string]time.Time{}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			tr.dirs[strings.TrimSuffix(f.Name, "/")] = true
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if _, dup := tr.files[f.Name]; dup {
			t.Fatalf("%s is twice in the export", f.Name)
		}
		tr.files[f.Name] = b
		mt[f.Name] = f.Modified
	}
	return tr, mt
}

// subTree keeps what a restore of the chosen paths should produce: the
// paths themselves and everything below them.
func subTree(all tree, paths []string) tree {
	out := tree{files: map[string][]byte{}, dirs: map[string]bool{}}
	fold := func(s string) string {
		if runtime.GOOS != "linux" {
			return strings.ToLower(s)
		}
		return s
	}
	keep := func(p string) bool {
		for _, s := range paths {
			if fold(p) == fold(s) || strings.HasPrefix(fold(p), fold(s)+"/") {
				return true
			}
		}
		return false
	}
	for p, b := range all.files {
		if keep(p) {
			out.files[p] = b
		}
	}
	for p := range all.dirs {
		if keep(p) {
			out.dirs[p] = true
		}
	}
	// Restoring a/b/c.txt also creates a and a/b.
	for p := range out.files {
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			out.dirs[d] = true
		}
	}
	for p := range out.dirs {
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			out.dirs[d] = true
		}
	}
	return out
}

func pickPaths(rnd *rand.Rand, tr tree) []string {
	var all []string
	for p := range tr.files {
		all = append(all, p)
	}
	for p := range tr.dirs {
		all = append(all, p)
	}
	sort.Strings(all)
	if len(all) == 0 {
		return nil
	}
	n := 1 + rnd.IntN(3)
	var out []string
	for range n {
		out = append(out, all[rnd.IntN(len(all))])
	}
	return out
}

// caseRename renames one file to an upper-case name (a case-only rename
// on Windows and macOS).
func (s *sim) caseRename() {
	files := s.files()
	if len(files) == 0 {
		return
	}
	f := files[s.rnd.IntN(len(files))]
	dir, base := path.Split(f)
	up := strings.ToUpper(base)
	if up == base {
		return
	}
	old := filepath.Join(s.dir, filepath.FromSlash(f))
	nw := filepath.Join(s.dir, filepath.FromSlash(dir+up))
	if runtime.GOOS == "linux" {
		if _, err := os.Stat(nw); err == nil {
			return
		}
	}
	os.Rename(old, nw)
}

type matrixCase struct {
	name      string
	password  string
	maxPart   int64
	autoFull  int
	keepGens  int // prune to this many generations at the end (0 = no prune)
	handCheck bool
}

// TestRestoreEveryDay is the restore guarantee: for every backup ever
// taken, restoring by id, restoring by date, exporting as one ZIP and
// restoring a few chosen files all give exactly what the folder held that
// day, before and after rebuilding the catalog from the archives alone.
func TestRestoreEveryDay(t *testing.T) {
	days := 30
	if testing.Short() {
		days = 10
	}
	cases := []matrixCase{
		{name: "plain", handCheck: true},
		{name: "new-full-every-5", autoFull: 5, handCheck: true},
		{name: "split-parts", maxPart: 150000},
		{name: "encrypted-split", password: "çok-gizli-parola-42", maxPart: 180000},
		{name: "pruned", autoFull: 4, keepGens: 2},
	}
	for ci, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "bleen")
			v, err := vault.Create(root, "disk", "test", c.password)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { v.Close() }()
			rnd := rand.New(rand.NewPCG(uint64(100+ci), 5))
			s := &sim{t: t, rnd: rnd, dir: filepath.Join(t.TempDir(), "Proje")}
			os.MkdirAll(s.dir, 0o755)
			for range 20 {
				s.write(s.randomPath(), s.content())
			}
			s.write("bos.txt", nil) // an empty file
			src := source.NewLocal(s.dir, nil)

			var truth []tree
			var mtimes map[string]time.Time
			for day := 0; day < days; day++ {
				s.day = day
				if day > 0 {
					s.mutate()
					if rnd.IntN(4) == 0 {
						s.caseRename()
					}
				}
				opt := testOpts(day)
				opt.MaxPartSize = c.maxPart
				if c.maxPart == 0 {
					opt.MaxPartSize = -1
				}
				opt.AutoFullEvery = c.autoFull
				rep, err := Backup(context.Background(), v, src, opt)
				if err != nil {
					t.Fatalf("day %d: %v", day, err)
				}
				if len(rep.Issues) > 0 {
					t.Fatalf("day %d issues: %+v", day, rep.Issues)
				}
				if !rep.NothingToDo {
					truth = append(truth, readTree(t, s.dir))
				}
			}
			mtimes = fileMTimes(t, s.dir)
			if c.maxPart > 0 {
				parts, _ := filepath.Glob(filepath.Join(root, "Proje", "*.part02.zip*"))
				if len(parts) == 0 {
					t.Fatal("no backup was split into parts")
				}
			}

			srcs, err := v.Catalog.Sources()
			if err != nil || len(srcs) != 1 {
				t.Fatalf("sources: %v %v", srcs, err)
			}
			if c.keepGens > 0 {
				before, _ := v.Catalog.Snapshots(srcs[0].ID)
				if _, err := Prune(v, &srcs[0], c.keepGens); err != nil {
					t.Fatal(err)
				}
				after, _ := v.Catalog.Snapshots(srcs[0].ID)
				if len(after) >= len(before) {
					t.Fatalf("prune removed nothing (%d → %d)", len(before), len(after))
				}
				truth = truth[len(before)-len(after):]
			}

			checkAllDays(t, v, &srcs[0], truth, mtimes, rnd)
			if c.handCheck {
				handRestoreEveryDay(t, v, filepath.Join(root, "Proje"), truth)
			}

			// The catalog is derived data: delete it, rebuild it from the
			// archives alone and check again.
			if err := v.Rebuild(false); err != nil {
				t.Fatal(err)
			}
			srcs, _ = v.Catalog.Sources()
			checkAllDays(t, v, &srcs[0], truth, mtimes, rnd)

			if vr, err := VerifyVault(context.Background(), v, nil); err != nil || len(vr.Problems) > 0 {
				t.Fatalf("verify: %v %+v", err, vr)
			}

			// And again after closing and reopening the disk.
			v.Close()
			v, err = vault.Open(root, vault.OpenOptions{Password: c.password})
			if err != nil {
				t.Fatal(err)
			}
			srcs, _ = v.Catalog.Sources()
			checkAllDays(t, v, &srcs[0], truth, mtimes, rnd)
		})
	}
}

func fileMTimes(t *testing.T, dir string) map[string]time.Time {
	out := map[string]time.Time{}
	for p := range readTree(t, dir).files {
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		out[p] = fi.ModTime()
	}
	return out
}

func checkAllDays(t *testing.T, v *vault.Vault, src *catalog.Source, truth []tree, mtimes map[string]time.Time, rnd *rand.Rand) {
	t.Helper()
	ctx := context.Background()
	snaps, err := v.Catalog.Snapshots(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != len(truth) {
		t.Fatalf("catalog has %d backups, want %d", len(snaps), len(truth))
	}
	for i, sn := range snaps {
		label := fmt.Sprintf("backup #%d (%s, gen %d seq %d)", i, sn.StartedAt.Format("2006-01-02"), sn.Generation, sn.Seq)

		// 1. Restore by id.
		dest := filepath.Join(t.TempDir(), "r")
		rep, err := Restore(ctx, v, src, RestoreOptions{Snapshot: sn.UUID, Dest: dest})
		if err != nil || len(rep.Issues) > 0 {
			t.Fatalf("%s: restore: %v %+v", label, err, rep)
		}
		if d := diffTrees(truth[i], readTree(t, dest)); d != "" {
			t.Fatalf("%s: restore differs:\n%s", label, d)
		}
		if i == len(snaps)-1 {
			for p, want := range mtimes {
				fi, err := os.Stat(filepath.Join(dest, filepath.FromSlash(p)))
				if err != nil || !fi.ModTime().Equal(want) {
					t.Fatalf("%s: %s restored with time %v, want %v (%v)", label, p, fi.ModTime(), want, err)
				}
			}
		}

		// 2. Restore by date: "as it was at this moment".
		dest = filepath.Join(t.TempDir(), "r")
		if _, err := Restore(ctx, v, src, RestoreOptions{Before: sn.StartedAt.Add(time.Minute), Dest: dest}); err != nil {
			t.Fatalf("%s: restore by date: %v", label, err)
		}
		if d := diffTrees(truth[i], readTree(t, dest)); d != "" {
			t.Fatalf("%s: restore by date differs:\n%s", label, d)
		}
		if i > 0 {
			// A moment before this backup started means the previous one.
			dest = filepath.Join(t.TempDir(), "r")
			if _, err := Restore(ctx, v, src, RestoreOptions{Before: sn.StartedAt, Dest: dest}); err != nil {
				t.Fatalf("%s: restore before: %v", label, err)
			}
			if d := diffTrees(truth[i-1], readTree(t, dest)); d != "" {
				t.Fatalf("%s: restore just before differs:\n%s", label, d)
			}
		}

		// 3. Export as one ZIP.
		zp := filepath.Join(t.TempDir(), "export.zip")
		erep, err := Export(ctx, v, src, ExportOptions{Snapshot: sn.UUID, Dest: zp})
		if err != nil || len(erep.Issues) > 0 {
			t.Fatalf("%s: export: %v %+v", label, err, erep)
		}
		got, zmt := zipTree(t, zp)
		if d := diffTrees(truth[i], got); d != "" {
			t.Fatalf("%s: export differs:\n%s", label, d)
		}
		if i == len(snaps)-1 {
			for p, want := range mtimes {
				if !zmt[p].Equal(want.Truncate(time.Second)) {
					t.Fatalf("%s: %s exported with time %v, want %v", label, p, zmt[p], want)
				}
			}
		}

		// 4. A few chosen files and folders, restored and exported.
		paths := pickPaths(rnd, truth[i])
		if len(paths) == 0 {
			continue
		}
		want := subTree(truth[i], paths)
		dest = filepath.Join(t.TempDir(), "r")
		rep, err = Restore(ctx, v, src, RestoreOptions{Snapshot: sn.UUID, Dest: dest, Paths: paths})
		if err != nil || len(rep.Issues) > 0 {
			t.Fatalf("%s: restore %v: %v %+v", label, paths, err, rep)
		}
		if d := diffTrees(want, readTree(t, dest)); d != "" {
			t.Fatalf("%s: restore of %v differs:\n%s", label, paths, d)
		}
		zp = filepath.Join(t.TempDir(), "part.zip")
		if _, err := Export(ctx, v, src, ExportOptions{Snapshot: sn.UUID, Dest: zp, Paths: paths}); err != nil {
			t.Fatalf("%s: export %v: %v", label, paths, err)
		}
		got, _ = zipTree(t, zp)
		wantFiles := tree{files: want.files, dirs: got.dirs} // parent folders are implied in a ZIP
		if d := diffTrees(wantFiles, got); d != "" {
			t.Fatalf("%s: export of %v differs:\n%s", label, paths, d)
		}
	}
}

// handRestoreEveryDay rebuilds every day with nothing but an unzip tool:
// the newest FULL up to that day, then each INCREMENTAL in name order,
// deleting what its DELETED.txt lists.
func handRestoreEveryDay(t *testing.T, v *vault.Vault, dir string, truth []tree) {
	t.Helper()
	zips, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	sort.Strings(zips)
	if len(zips) != len(truth) {
		t.Fatalf("%d archives for %d backups", len(zips), len(truth))
	}
	for i := range zips {
		start := 0
		for j := i; j >= 0; j-- {
			if strings.Contains(filepath.Base(zips[j]), "_FULL") {
				start = j
				break
			}
		}
		dest := t.TempDir()
		for _, z := range zips[start : i+1] {
			handExtract(t, z, dest)
		}
		if d := diffTrees(truth[i], readTree(t, dest)); d != "" {
			t.Fatalf("hand restore of %s differs:\n%s", filepath.Base(zips[i]), d)
		}
	}
}

func handExtract(t *testing.T, z, dest string) {
	t.Helper()
	zr, err := zip.OpenReader(z)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	// 1. Delete what DELETED.txt lists.
	for _, f := range zr.File {
		if f.Name != "DELETED.txt" {
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\r\n") {
			if line != "" {
				os.RemoveAll(filepath.Join(dest, filepath.FromSlash(line)))
			}
		}
	}
	// 2. Extract, replacing files.
	for _, f := range zr.File {
		rel, ok := strings.CutPrefix(f.Name, "files/")
		switch {
		case !ok:
		case strings.HasSuffix(rel, "/"):
			if err := os.MkdirAll(filepath.Join(dest, filepath.FromSlash(rel)), 0o755); err != nil {
				t.Fatalf("hand extract of %s: %v", f.Name, err)
			}
		default:
			p := filepath.Join(dest, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("hand extract of %s: %v", f.Name, err)
			}
			rc, _ := f.Open()
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			os.Remove(p) // replace, as 7-Zip does: a case-only rename gets its new name
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatalf("hand extract of %s: %v", f.Name, err)
			}
		}
	}
}

// TestExportAndRestoreRefusals covers the places a restore or export must
// say no instead of writing.
func TestExportAndRestoreRefusals(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(8, 8)), dir: filepath.Join(t.TempDir(), "src")}
	s.write("a.txt", []byte("hello"))
	if _, err := Backup(context.Background(), v, source.NewLocal(s.dir, nil), testOpts(0)); err != nil {
		t.Fatal(err)
	}
	srcs, _ := v.Catalog.Sources()
	code := func(err error) string {
		var e *Error
		if errors.As(err, &e) {
			return e.Code
		}
		return fmt.Sprint(err)
	}

	existing := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(existing, []byte("keep me"), 0o644)
	if _, err := Export(context.Background(), v, &srcs[0], ExportOptions{Dest: existing}); code(err) != EDestExists {
		t.Fatalf("export over a file: %v", err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "keep me" {
		t.Fatal("export overwrote an existing file")
	}
	if _, err := Export(context.Background(), v, &srcs[0], ExportOptions{Dest: filepath.Join(v.Root, "x.zip")}); code(err) != EDestInVault {
		t.Fatalf("export into the vault: %v", err)
	}
	if _, err := Restore(context.Background(), v, &srcs[0], RestoreOptions{Dest: filepath.Join(v.Root, "geri")}); code(err) != EDestInVault {
		t.Fatalf("restore into the vault: %v", err)
	}
	if _, err := Export(context.Background(), v, &srcs[0], ExportOptions{Dest: filepath.Join(t.TempDir(), "y.zip"), Paths: []string{"yok.txt"}}); code(err) != ENotFound {
		t.Fatalf("export of a missing path: %v", err)
	}

	// The same with relative paths (bleenctl --vault vault --zip vault/x.zip).
	t.Chdir(filepath.Dir(v.Root))
	rel := filepath.Join(filepath.Base(v.Root), "x.zip")
	if !insideDir(filepath.Base(v.Root), rel) || insideDir(filepath.Base(v.Root), "x.zip") {
		t.Fatal("insideDir is wrong for relative paths")
	}
	if _, err := Export(context.Background(), v, &srcs[0], ExportOptions{Dest: rel}); code(err) != EDestInVault {
		t.Fatalf("export into the vault by a relative path: %v", err)
	}

	// A cancelled export leaves nothing behind.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dir := t.TempDir()
	if _, err := Export(ctx, v, &srcs[0], ExportOptions{Dest: filepath.Join(dir, "c.zip")}); code(err) != ECancelled {
		t.Fatalf("cancelled export: %v", err)
	}
	if des, _ := os.ReadDir(dir); len(des) != 0 {
		t.Fatalf("cancelled export left %v", des)
	}
}

// TestDamagedArchiveNeverRestoresBadData flips bytes inside an archive:
// the damaged files must be reported, never written with wrong content,
// and every other file must still restore.
func TestDamagedArchiveNeverRestoresBadData(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(9, 9)), dir: filepath.Join(t.TempDir(), "src")}
	for i := range 6 {
		b := make([]byte, 20000)
		for j := range b {
			b[j] = byte(s.rnd.IntN(256))
		}
		s.write(fmt.Sprintf("f%d.bin", i), b)
	}
	if _, err := Backup(context.Background(), v, source.NewLocal(s.dir, nil), testOpts(0)); err != nil {
		t.Fatal(err)
	}
	s.day = 1
	s.write("later.txt", []byte("written on day two"))
	if _, err := Backup(context.Background(), v, source.NewLocal(s.dir, nil), testOpts(1)); err != nil {
		t.Fatal(err)
	}
	truth := readTree(t, s.dir)

	full, _ := filepath.Glob(filepath.Join(v.Root, "src", "*_FULL.zip"))
	zr, err := zip.OpenReader(full[0])
	if err != nil {
		t.Fatal(err)
	}
	var off int64
	var victim string
	for _, f := range zr.File {
		if f.Name == "files/f3.bin" {
			off, _ = f.DataOffset()
			victim = "f3.bin"
		}
	}
	zr.Close()
	if victim == "" {
		t.Fatal("f3.bin not found in the archive")
	}
	fh, err := os.OpenFile(full[0], os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	fh.WriteAt([]byte("DAMAGED!"), off+5000)
	fh.Close()

	srcs, _ := v.Catalog.Sources()
	dest := filepath.Join(t.TempDir(), "r")
	rep, err := Restore(context.Background(), v, &srcs[0], RestoreOptions{Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Issues) != 1 || rep.Issues[0].Path != victim {
		t.Fatalf("issues: %+v", rep.Issues)
	}
	got := readTree(t, dest)
	if _, ok := got.files[victim]; ok {
		t.Fatal("a damaged file was written")
	}
	delete(truth.files, victim)
	if d := diffTrees(truth, got); d != "" {
		t.Fatalf("the other files differ:\n%s", d)
	}

	zp := filepath.Join(t.TempDir(), "e.zip")
	erep, err := Export(context.Background(), v, &srcs[0], ExportOptions{Dest: zp})
	if err != nil {
		t.Fatal(err)
	}
	if len(erep.Issues) != 1 || erep.Issues[0].Path != victim {
		t.Fatalf("export issues: %+v", erep.Issues)
	}
	gz, _ := zipTree(t, zp)
	if d := diffTrees(truth, gz); d != "" {
		t.Fatalf("export differs:\n%s", d)
	}

	vr, err := VerifyVault(context.Background(), v, nil)
	if err != nil || len(vr.Problems) == 0 {
		t.Fatalf("verify missed the damage: %v %+v", err, vr)
	}
}
