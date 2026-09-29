package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// tree is the expected content of a folder: files by relative path, plus
// directories (including empty ones).
type tree struct {
	files map[string][]byte
	dirs  map[string]bool
}

func readTree(t *testing.T, root string) tree {
	t.Helper()
	tr := tree{files: map[string][]byte{}, dirs: map[string]bool{}}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			tr.dirs[rel] = true
			return nil
		}
		b, err := os.ReadFile(p)
		tr.files[rel] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func diffTrees(want, got tree) string {
	var out []string
	for p, b := range want.files {
		g, ok := got.files[p]
		switch {
		case !ok:
			out = append(out, "missing file "+p)
		case !bytes.Equal(b, g):
			out = append(out, "different content "+p)
		}
	}
	for p := range got.files {
		if _, ok := want.files[p]; !ok {
			out = append(out, "unexpected file "+p)
		}
	}
	for p := range want.dirs {
		if !got.dirs[p] {
			out = append(out, "missing dir "+p)
		}
	}
	for p := range got.dirs {
		if !want.dirs[p] {
			out = append(out, "unexpected dir "+p)
		}
	}
	sort.Strings(out)
	if len(out) > 10 {
		out = append(out[:10], fmt.Sprintf("… and %d more", len(out)-10))
	}
	return strings.Join(out, "\n")
}

type sim struct {
	t   *testing.T
	rnd *rand.Rand
	dir string
	day int
	seq int
}

var names = []string{"rapor.xlsx", "teklif.docx", "çalışma ğüş.txt", "notes.md", "Resim ÖRNEK.jpg", "data.bin", "a b.txt", "İzmir.csv"}

func (s *sim) mtime() time.Time {
	s.seq++
	return time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC).AddDate(0, 0, s.day).Add(time.Duration(s.seq) * time.Second)
}

func (s *sim) write(rel string, b []byte) {
	p := filepath.Join(s.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		s.t.Fatal(err)
	}
	mt := s.mtime()
	os.Chtimes(p, mt, mt)
}

func (s *sim) content() []byte {
	n := s.rnd.IntN(3) * s.rnd.IntN(40000)
	b := make([]byte, n)
	if s.rnd.IntN(2) == 0 {
		for i := range b {
			b[i] = byte(s.rnd.IntN(256)) // incompressible
		}
	} else {
		for i := range b {
			b[i] = "abcabc\n"[i%7] // compressible
		}
	}
	return b
}

func (s *sim) files() []string {
	var out []string
	filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(s.dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func (s *sim) randomPath() string {
	dirs := []string{"", "klasor/", "klasor/alt/", "Muhasebe 2026/", "x/y/z/"}
	return dirs[s.rnd.IntN(len(dirs))] + fmt.Sprintf("%d-", s.rnd.IntN(50)) + names[s.rnd.IntN(len(names))]
}

// mutate applies one day of random edits.
func (s *sim) mutate() {
	ops := 1 + s.rnd.IntN(8)
	for range ops {
		files := s.files()
		switch r := s.rnd.IntN(10); {
		case r < 3 || len(files) == 0: // add
			s.write(s.randomPath(), s.content())
		case r < 5: // modify
			s.write(files[s.rnd.IntN(len(files))], s.content())
		case r < 6: // touch: same content, new mtime
			f := files[s.rnd.IntN(len(files))]
			p := filepath.Join(s.dir, filepath.FromSlash(f))
			mt := s.mtime()
			os.Chtimes(p, mt, mt)
		case r < 7: // delete
			os.Remove(filepath.Join(s.dir, filepath.FromSlash(files[s.rnd.IntN(len(files))])))
		case r < 8: // move / rename
			f := files[s.rnd.IntN(len(files))]
			dst := s.randomPath()
			os.MkdirAll(filepath.Join(s.dir, filepath.Dir(filepath.FromSlash(dst))), 0o755)
			if _, err := os.Stat(filepath.Join(s.dir, filepath.FromSlash(dst))); err != nil {
				os.Rename(filepath.Join(s.dir, filepath.FromSlash(f)), filepath.Join(s.dir, filepath.FromSlash(dst)))
			}
		case r < 9: // copy
			f := files[s.rnd.IntN(len(files))]
			b, _ := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(f)))
			s.write(s.randomPath(), b)
		default: // empty dir, or remove a whole dir
			if s.rnd.IntN(2) == 0 {
				os.MkdirAll(filepath.Join(s.dir, fmt.Sprintf("bos-%d", s.rnd.IntN(5))), 0o755)
			} else {
				os.RemoveAll(filepath.Join(s.dir, "x"))
			}
		}
	}
}

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.Create(filepath.Join(t.TempDir(), "bleen"), "test disk", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v
}

func testOpts(day int) BackupOptions {
	now := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC).AddDate(0, 0, day)
	return BackupOptions{
		Now:             func() time.Time { return now },
		AllowMassChange: true,
		RetryDelays:     []time.Duration{},
		MaxPartSize:     -1,
	}
}

func restoreAll(t *testing.T, v *vault.Vault, truth []tree) {
	t.Helper()
	srcs, err := v.Catalog.Sources()
	if err != nil || len(srcs) != 1 {
		t.Fatalf("sources: %v %v", srcs, err)
	}
	snaps, err := v.Catalog.Snapshots(srcs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != len(truth) {
		t.Fatalf("catalog has %d snapshots, want %d", len(snaps), len(truth))
	}
	for i, sn := range snaps {
		dest := filepath.Join(t.TempDir(), "r")
		rep, err := Restore(context.Background(), v, &srcs[0], RestoreOptions{Snapshot: sn.UUID, Dest: dest})
		if err != nil {
			t.Fatalf("restore #%d: %v", i, err)
		}
		if len(rep.Issues) > 0 {
			t.Fatalf("restore #%d issues: %+v", i, rep.Issues)
		}
		if d := diffTrees(truth[i], readTree(t, dest)); d != "" {
			t.Fatalf("restore of snapshot #%d (gen %d seq %d) differs:\n%s", i, sn.Generation, sn.Seq, d)
		}
	}
}

// TestTimeTravel evolves a folder at random for many days, backs it up every
// day and checks that every single day restores byte for byte.
func TestTimeTravel(t *testing.T) {
	days := 40
	if testing.Short() {
		days = 12
	}
	for _, seed := range []uint64{1, 2, 3} {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			v := newVault(t)
			s := &sim{t: t, rnd: rand.New(rand.NewPCG(seed, 99)), dir: filepath.Join(t.TempDir(), "_proje")}
			os.MkdirAll(s.dir, 0o755)
			for range 15 {
				s.write(s.randomPath(), s.content())
			}
			var truth []tree
			src := source.NewLocal(s.dir, nil)
			for day := 0; day < days; day++ {
				s.day = day
				if day > 0 {
					s.mutate()
				}
				opt := testOpts(day)
				opt.Full = day == days/2 // a fresh full backup mid-way starts generation 2
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
			restoreAll(t, v, truth)

			// The catalog is derived data: rebuilding it from the archives
			// must give the same restores.
			if err := v.Rebuild(); err != nil {
				t.Fatal(err)
			}
			restoreAll(t, v, truth)

			vr, err := VerifyVault(context.Background(), v, nil)
			if err != nil || len(vr.Problems) > 0 {
				t.Fatalf("verify: %v %+v", err, vr)
			}
		})
	}
}

func TestIncrementalStoresOnlyChanges(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(7, 7)), dir: filepath.Join(t.TempDir(), "src")}
	s.write("a.txt", []byte("hello"))
	s.write("big/b.bin", bytes.Repeat([]byte("x"), 100000))
	s.write("c.txt", []byte("same"))
	src := source.NewLocal(s.dir, nil)

	rep, err := Backup(context.Background(), v, src, testOpts(0))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Plan.Kind != "full" || rep.FilesStored != 3 {
		t.Fatalf("first backup: kind=%s stored=%d", rep.Plan.Kind, rep.FilesStored)
	}

	s.day = 1
	s.write("a.txt", []byte("hello, world")) // modified
	s.write("new.txt", []byte("new"))        // new
	os.Remove(filepath.Join(s.dir, "c.txt")) // deleted
	os.Rename(filepath.Join(s.dir, "big", "b.bin"), filepath.Join(s.dir, "moved.bin"))

	rep, err = Backup(context.Background(), v, src, testOpts(1))
	if err != nil {
		t.Fatal(err)
	}
	p := rep.Plan
	if p.Kind != "incremental" || p.New != 2 || p.Modified != 1 || p.Deleted != 2 {
		t.Fatalf("plan: %+v", p)
	}
	if rep.FilesStored != 2 || rep.FilesDeduped != 1 {
		t.Fatalf("stored=%d deduped=%d, want 2 and 1 (the move is a reference)", rep.FilesStored, rep.FilesDeduped)
	}

	rep, err = Backup(context.Background(), v, src, testOpts(2))
	if err != nil || !rep.NothingToDo {
		t.Fatalf("third backup should find nothing to do: %+v %v", rep, err)
	}
}

func TestSourceEmptyGuard(t *testing.T) {
	v := newVault(t)
	dir := filepath.Join(t.TempDir(), "share")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644)
	src := source.NewLocal(dir, nil)
	if _, err := Backup(context.Background(), v, src, testOpts(0)); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "f.txt"))
	_, err := Backup(context.Background(), v, src, testOpts(1))
	var e *Error
	if !errors.As(err, &e) || e.Code != ESourceEmpty {
		t.Fatalf("want %s, got %v", ESourceEmpty, err)
	}

	os.RemoveAll(dir)
	_, err = Backup(context.Background(), v, src, testOpts(2))
	if !errors.As(err, &e) || e.Code != ESourceUnreachable {
		t.Fatalf("want %s, got %v", ESourceUnreachable, err)
	}
}

func TestMassChangeGuard(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(5, 5)), dir: filepath.Join(t.TempDir(), "src")}
	for i := range 30 {
		s.write(fmt.Sprintf("f%02d.txt", i), []byte(fmt.Sprint("v1-", i)))
	}
	src := source.NewLocal(s.dir, nil)
	opt := testOpts(0)
	opt.AllowMassChange = false
	if _, err := Backup(context.Background(), v, src, opt); err != nil {
		t.Fatal(err)
	}
	s.day = 1
	for i := range 20 { // "ransomware" rewrites two thirds of the files
		s.write(fmt.Sprintf("f%02d.txt", i), []byte(fmt.Sprint("encrypted-", i)))
	}
	opt = testOpts(1)
	opt.AllowMassChange = false
	_, err := Backup(context.Background(), v, src, opt)
	var e *Error
	if !errors.As(err, &e) || e.Code != EMassChange {
		t.Fatalf("want %s, got %v", EMassChange, err)
	}
	opt.Confirm = func(p *Plan) bool { return p.MassChange }
	if _, err := Backup(context.Background(), v, src, opt); err != nil {
		t.Fatalf("confirmed run: %v", err)
	}
}

// TestCrashBeforeCatalogSave simulates a power cut after the archive was
// renamed into place but before the catalog was saved.
func TestCrashBeforeCatalogSave(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bleen")
	v, err := vault.Create(root, "disk", "test")
	if err != nil {
		t.Fatal(err)
	}
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(9, 9)), dir: filepath.Join(t.TempDir(), "src")}
	s.write("a.txt", []byte("one"))
	src := source.NewLocal(s.dir, nil)
	if _, err := Backup(context.Background(), v, src, testOpts(0)); err != nil {
		t.Fatal(err)
	}
	truth0 := readTree(t, s.dir)
	s.day = 1
	s.write("b.txt", []byte("two"))
	truth1 := readTree(t, s.dir)
	opt := testOpts(1)
	crash := errors.New("power cut")
	opt.afterRename = func() error { return crash }
	if _, err := Backup(context.Background(), v, src, opt); !errors.Is(err, crash) {
		t.Fatalf("want simulated crash, got %v", err)
	}
	v.Close()

	// Leave a stray partial file too, as a crash mid-write would.
	srcs, _ := filepath.Glob(filepath.Join(root, "src", "*.zip"))
	if len(srcs) != 2 {
		t.Fatalf("expected 2 archives on disk, got %v", srcs)
	}
	os.WriteFile(filepath.Join(root, "src", "2026-09-03_1800_INCREMENTAL.part01.zip.partial"), []byte("junk"), 0o644)

	v, err = vault.Open(root, vault.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if len(v.Recovered) < 2 {
		t.Fatalf("expected recovery actions, got %v", v.Recovered)
	}
	restoreAll(t, v, []tree{truth0, truth1})
}

func TestSplitParts(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(3, 3)), dir: filepath.Join(t.TempDir(), "src")}
	for i := range 12 {
		b := make([]byte, 30000)
		for j := range b {
			b[j] = byte(s.rnd.IntN(256))
		}
		s.write(fmt.Sprintf("d%d/f%d.bin", i%3, i), b)
	}
	opt := testOpts(0)
	opt.MaxPartSize = 100000
	rep, err := Backup(context.Background(), v, source.NewLocal(s.dir, nil), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Archives) < 3 || !strings.Contains(rep.Archives[0], ".part01.zip") {
		t.Fatalf("expected several parts, got %v", rep.Archives)
	}
	restoreAll(t, v, []tree{readTree(t, s.dir)})
	if err := v.Rebuild(); err != nil {
		t.Fatal(err)
	}
	restoreAll(t, v, []tree{readTree(t, s.dir)})
}

func TestRestoreRefusesNonEmptyDest(t *testing.T) {
	v := newVault(t)
	dir := filepath.Join(t.TempDir(), "src")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	if _, err := Backup(context.Background(), v, source.NewLocal(dir, nil), testOpts(0)); err != nil {
		t.Fatal(err)
	}
	srcs, _ := v.Catalog.Sources()
	dest := t.TempDir()
	os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("mine"), 0o644)
	_, err := Restore(context.Background(), v, &srcs[0], RestoreOptions{Dest: dest})
	var e *Error
	if !errors.As(err, &e) || e.Code != EDestNotEmpty {
		t.Fatalf("want %s, got %v", EDestNotEmpty, err)
	}
}

func TestSafeJoin(t *testing.T) {
	dest := t.TempDir()
	for _, bad := range []string{"../x", "a/../../x", "/etc/passwd", "", "a//b", "a/./b"} {
		if _, _, err := SafeJoin(dest, bad); err == nil {
			t.Errorf("SafeJoin(%q) should fail", bad)
		}
	}
	if p, _, err := SafeJoin(dest, "klasör/çalışma.txt"); err != nil || !strings.HasPrefix(p, dest) {
		t.Errorf("good path rejected: %v %v", p, err)
	}
}
