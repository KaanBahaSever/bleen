package engine

import (
	"archive/zip"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kaanbahasever/bleen/internal/source"
)

// TestHandRestoreKindAndCaseChanges: a file that becomes a folder, a folder
// that becomes a file and a rename that only changes letter case must all
// come out right with nothing but an unzip tool, and with bleen.
func TestHandRestoreKindAndCaseChanges(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(31, 31)), dir: filepath.Join(t.TempDir(), "src")}
	src := source.NewLocal(s.dir, nil)
	var truth []tree
	backup := func(day int) {
		t.Helper()
		s.day = day
		rep, err := Backup(context.Background(), v, src, testOpts(day))
		if err != nil || len(rep.Issues) > 0 {
			t.Fatalf("day %d: %v %+v", day, err, rep)
		}
		truth = append(truth, readTree(t, s.dir))
	}
	at := func(rel string) string { return filepath.Join(s.dir, filepath.FromSlash(rel)) }

	s.write("x", []byte("x was a file"))
	s.write("d/a.txt", []byte("inside d"))
	s.write("Report.pdf", []byte("report"))
	s.write("Docs/plan.txt", []byte("plan"))
	backup(0)

	os.Remove(at("x"))
	s.write("x/y.txt", []byte("x is a folder now"))
	os.RemoveAll(at("d"))
	s.write("d", []byte("d is a file now"))
	os.Rename(at("Report.pdf"), at("report.pdf"))
	os.Rename(at("Docs"), at("docs"))
	backup(1)

	os.RemoveAll(at("x"))
	s.write("x", []byte("x is a file again"))
	os.Rename(at("report.pdf"), at("REPORT.pdf"))
	backup(2)

	restoreAll(t, v, truth)
	handRestoreEveryDay(t, v, filepath.Join(v.Root, "src"), truth)
}

// TestLongFileNames: names up to 255 characters back up, restore and export.
func TestLongFileNames(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(32, 32)), dir: filepath.Join(t.TempDir(), "src")}
	long := strings.Repeat("ç", 100) + strings.Repeat("a", 151) + ".pdf" // 255 characters
	s.write(long, []byte("a long name"))
	s.write("k/"+long, []byte("a long name in a folder"))
	if _, err := Backup(context.Background(), v, source.NewLocal(s.dir, nil), testOpts(0)); err != nil {
		t.Fatal(err)
	}
	truth := readTree(t, s.dir)
	restoreAll(t, v, []tree{truth})
	srcs, _ := v.Catalog.Sources()
	zp := filepath.Join(t.TempDir(), "e.zip")
	if rep, err := Export(context.Background(), v, &srcs[0], ExportOptions{Dest: zp}); err != nil || len(rep.Issues) > 0 {
		t.Fatalf("export: %v %+v", err, rep)
	}
	got, _ := zipTree(t, zp)
	if d := diffTrees(tree{files: truth.files, dirs: got.dirs}, got); d != "" {
		t.Fatal(d)
	}
}

// TestPruneNotHeldByFileThatIsNeverStored: a file that can never be stored
// (too large for the disk) must not stop retention forever.
func TestPruneNotHeldByFileThatIsNeverStored(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(33, 33)), dir: filepath.Join(t.TempDir(), "src")}
	big := make([]byte, 300000)
	for i := range big {
		big[i] = byte(s.rnd.IntN(256))
	}
	s.write("video.mp4", big)
	src := source.NewLocal(s.dir, nil)
	for day := 0; day < 4; day++ {
		s.day = day
		s.write("a.txt", []byte(fmt.Sprint("day ", day)))
		opt := testOpts(day)
		opt.Full = true
		opt.MaxPartSize = 100000
		rep, err := Backup(context.Background(), v, src, opt)
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Issues) != 1 || rep.Issues[0].Code != ETooLarge {
			t.Fatalf("day %d issues: %+v", day, rep.Issues)
		}
	}
	srcs, _ := v.Catalog.Sources()
	pr, err := Prune(v, &srcs[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Held != "" || pr.Generations != 3 {
		t.Fatalf("prune: %+v", pr)
	}
}

// TestArchiveNamesFollowBackupOrder: after the clock goes back (a time-zone
// change, a fixed clock), a later backup's name must still sort after the
// earlier ones, because a hand restore goes by name.
func TestArchiveNamesFollowBackupOrder(t *testing.T) {
	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(34, 34)), dir: filepath.Join(t.TempDir(), "src")}
	src := source.NewLocal(s.dir, nil)
	base := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC)
	var truth []tree
	for i, at := range []time.Time{base, base.Add(-time.Hour), base.Add(-2 * time.Hour), base.Add(-2 * time.Hour)} {
		s.write("a.txt", []byte(fmt.Sprint("version ", i)))
		opt := testOpts(0)
		opt.Now = func() time.Time { return at }
		if _, err := Backup(context.Background(), v, src, opt); err != nil {
			t.Fatal(err)
		}
		truth = append(truth, readTree(t, s.dir))
	}
	zips, _ := filepath.Glob(filepath.Join(v.Root, "src", "*.zip"))
	sort.Strings(zips)
	if len(zips) != 4 || !strings.Contains(zips[0], "_FULL") {
		t.Fatalf("archives: %v", zips)
	}
	handRestoreEveryDay(t, v, filepath.Join(v.Root, "src"), truth)
}

// TestZipTimesAreLocal: the MS-DOS time fields hold local time, like every
// other ZIP tool writes them.
func TestZipTimesAreLocal(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("TRT", 3*3600)
	defer func() { time.Local = old }()

	v := newVault(t)
	s := &sim{t: t, rnd: rand.New(rand.NewPCG(35, 35)), dir: filepath.Join(t.TempDir(), "src")}
	s.write("a.txt", []byte("a"))
	mt := time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC) // 12:30 in Türkiye
	os.Chtimes(filepath.Join(s.dir, "a.txt"), mt, mt)
	if _, err := Backup(context.Background(), v, source.NewLocal(s.dir, nil), testOpts(0)); err != nil {
		t.Fatal(err)
	}
	zips, _ := filepath.Glob(filepath.Join(v.Root, "src", "*.zip"))
	zr, err := zip.OpenReader(zips[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "files/a.txt" {
			if h, m := f.ModifiedTime>>11, (f.ModifiedTime>>5)&63; h != 12 || m != 30 {
				t.Fatalf("DOS time %02d:%02d, want 12:30", h, m)
			}
			if !f.Modified.Equal(mt) {
				t.Fatalf("Modified %v, want %v", f.Modified, mt)
			}
			return
		}
	}
	t.Fatal("files/a.txt not found")
}
