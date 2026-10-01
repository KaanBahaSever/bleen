package vault_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kaanbahasever/bleen/internal/engine"
	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

type fixture struct {
	t    *testing.T
	root string
	src  string
	v    *vault.Vault
	day  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, root: filepath.Join(t.TempDir(), "bleen"), src: filepath.Join(t.TempDir(), "src")}
	os.MkdirAll(f.src, 0o755)
	v, err := vault.Create(f.root, "disk", "test", "")
	if err != nil {
		t.Fatal(err)
	}
	f.v = v
	t.Cleanup(func() {
		if f.v != nil {
			f.v.Close()
		}
	})
	return f
}

func (f *fixture) write(rel string, b []byte) {
	p := filepath.Join(f.src, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) backup(full bool, maxPart int64) {
	f.t.Helper()
	now := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC).AddDate(0, 0, f.day)
	f.day++
	if maxPart == 0 {
		maxPart = -1
	}
	_, err := engine.Backup(context.Background(), f.v, source.NewLocal(f.src, nil), engine.BackupOptions{
		Now: func() time.Time { return now }, AllowMassChange: true, RetryDelays: []time.Duration{},
		MaxPartSize: maxPart, Full: full,
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) reopen() {
	f.t.Helper()
	f.v.Close()
	v, err := vault.Open(f.root, vault.OpenOptions{})
	if err != nil {
		f.t.Fatal(err)
	}
	f.v = v
}

func (f *fixture) snapshots() int {
	srcs, err := f.v.Catalog.Sources()
	if err != nil {
		f.t.Fatal(err)
	}
	n := 0
	for _, s := range srcs {
		sn, _ := f.v.Catalog.Snapshots(s.ID)
		n += len(sn)
	}
	return n
}

func random(rnd *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rnd.IntN(256))
	}
	return b
}

func archives(t *testing.T, dir string) []string {
	des, _ := os.ReadDir(dir)
	var out []string
	for _, de := range des {
		if strings.Contains(de.Name(), ".zip") {
			out = append(out, de.Name())
		}
	}
	sort.Strings(out)
	return out
}

// TestRebuildWithMoreThan99Parts: part 100 and later are archives too.
func TestRebuildWithMoreThan99Parts(t *testing.T) {
	f := newFixture(t)
	rnd := rand.New(rand.NewPCG(1, 1))
	for i := range 220 {
		f.write(fmt.Sprintf("f%03d.bin", i), random(rnd, 2000))
	}
	f.backup(false, 4000)
	if n := len(archives(t, filepath.Join(f.root, "src"))); n < 100 {
		t.Fatalf("only %d parts; the test needs 100 or more", n)
	}
	if err := f.v.Rebuild(false); err != nil {
		t.Fatal(err)
	}
	if f.snapshots() != 1 {
		t.Fatalf("rebuilt catalog has %d backups; recovered: %v", f.snapshots(), f.v.Recovered)
	}
	if _, err := os.Stat(filepath.Join(f.root, ".bleen", "incomplete")); err == nil {
		t.Fatal("parts were moved aside")
	}
}

// TestUnreadablePartNeverMovesItsSiblings: one damaged part must not send
// the healthy parts of the same backup away, in Rebuild or in recovery.
func TestUnreadablePartNeverMovesItsSiblings(t *testing.T) {
	f := newFixture(t)
	rnd := rand.New(rand.NewPCG(2, 2))
	for i := range 8 {
		f.write(fmt.Sprintf("f%d.bin", i), random(rnd, 30000))
	}
	f.backup(false, 100000)
	dir := filepath.Join(f.root, "src")
	parts := archives(t, dir)
	if len(parts) < 3 {
		t.Fatalf("parts: %v", parts)
	}
	os.WriteFile(filepath.Join(dir, parts[1]), []byte("damaged"), 0o644)

	if err := f.v.Rebuild(false); err == nil {
		t.Fatal("rebuild succeeded with a damaged part")
	}
	if got := archives(t, dir); len(got) != len(parts) {
		t.Fatalf("rebuild moved files: %v", got)
	}
	if f.snapshots() != 1 {
		t.Fatal("a failed rebuild replaced the catalog in memory")
	}

	// Recovery with no catalog at all must also leave the parts in place.
	f.v.Close()
	f.v = nil
	os.Remove(filepath.Join(f.root, ".bleen", "catalog.db"))
	os.Remove(filepath.Join(f.root, ".bleen", "catalog.db.1"))
	v, err := vault.Open(f.root, vault.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.v = v
	if got := archives(t, dir); len(got) != len(parts) {
		t.Fatalf("recovery moved files: %v (recovered: %v)", got, v.Recovered)
	}
}

// TestPruneLeftoversAreHarmless: archives that survive a prune (a delete
// that failed, a power cut) never block recovery or Rebuild.
func TestPruneLeftoversAreHarmless(t *testing.T) {
	f := newFixture(t)
	for gen := 0; gen < 3; gen++ {
		for i := 0; i < 3; i++ {
			f.write("a.txt", []byte(fmt.Sprint(gen, i)))
			f.backup(i == 0 && gen > 0, 0)
		}
	}
	dir := filepath.Join(f.root, "src")
	before := archives(t, dir)
	// Keep copies of the oldest generation, prune it, then put back only
	// its last incremental: an orphan whose full backup is gone.
	orphan := before[2]
	b, _ := os.ReadFile(filepath.Join(dir, orphan))
	srcs, _ := f.v.Catalog.Sources()
	if _, err := engine.Prune(f.v, &srcs[0], 2); err != nil {
		t.Fatal(err)
	}
	if got := archives(t, dir); len(got) != 6 {
		t.Fatalf("after prune: %v", got)
	}
	os.WriteFile(filepath.Join(dir, orphan), b, 0o644)

	f.reopen()
	if f.snapshots() != 6 {
		t.Fatalf("%d backups after reopen; recovered: %v", f.snapshots(), f.v.Recovered)
	}
	if err := f.v.Rebuild(false); err != nil {
		t.Fatalf("rebuild with a leftover: %v", err)
	}
	if f.snapshots() != 6 {
		t.Fatalf("%d backups after rebuild", f.snapshots())
	}
}

// TestRebuildAfterFolderRename: renaming a source's folder on the disk
// (even only its letter case) must not break Rebuild.
func TestRebuildAfterFolderRename(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", []byte("one"))
	f.write("b.txt", []byte("same"))
	f.backup(false, 0)
	f.write("a.txt", []byte("two"))
	now := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(f.src, "b.txt"), now, now) // same content, new date: a reference
	f.backup(false, 0)
	f.v.Close()
	f.v = nil
	os.Rename(filepath.Join(f.root, "src"), filepath.Join(f.root, "tmp"))
	os.Rename(filepath.Join(f.root, "tmp"), filepath.Join(f.root, "Src"))
	v, err := vault.Open(f.root, vault.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.v = v
	if err := v.Rebuild(false); err != nil {
		t.Fatalf("rebuild after rename: %v", err)
	}
	if f.snapshots() != 2 {
		t.Fatalf("%d backups", f.snapshots())
	}
}

// TestRebuildWhenCatalogUnreadable: the advice in E_CATALOG_UNREADABLE
// (bleenctl rebuild-catalog) must actually work.
func TestRebuildWhenCatalogUnreadable(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", []byte("one"))
	f.backup(false, 0)
	f.write("a.txt", []byte("two"))
	f.backup(false, 0)
	f.v.Close()
	f.v = nil
	cat := filepath.Join(f.root, ".bleen", "catalog.db")
	os.WriteFile(cat, []byte("this is not a database, but it is long enough to look like a file header....."), 0o644)
	if _, err := vault.Open(f.root, vault.OpenOptions{}); err == nil || !strings.Contains(err.Error(), "E_CATALOG_UNREADABLE") {
		t.Fatalf("open: %v", err)
	}
	v, err := vault.Open(f.root, vault.OpenOptions{FreshCatalog: true})
	if err != nil {
		t.Fatal(err)
	}
	f.v = v
	if err := v.Rebuild(false); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	if f.snapshots() != 2 {
		t.Fatalf("%d backups after rebuild", f.snapshots())
	}
}

// TestPublishNeedsTheLock: after another bleen took the disk over, this one
// must not overwrite the catalog.
func TestPublishNeedsTheLock(t *testing.T) {
	f := newFixture(t)
	lock := filepath.Join(f.root, ".bleen", "lock")
	os.WriteFile(lock, []byte(`{"host":"OTHER-PC","pid":1}`), 0o644)
	if err := f.v.Publish(); err == nil {
		t.Fatal("published without holding the lock")
	}
	if b, _ := os.ReadFile(lock); !strings.Contains(string(b), "OTHER-PC") {
		t.Fatal("the other bleen's lock was touched")
	}
}

// TestBreakLockRefusesRunningBleen: the lock of a bleen still running on
// this computer (here: this test process) is never removed.
func TestBreakLockRefusesRunningBleen(t *testing.T) {
	f := newFixture(t)
	if err := vault.BreakLock(f.root); err == nil {
		t.Fatal("broke the lock of a running bleen")
	}
	if _, err := vault.Open(f.root, vault.OpenOptions{BreakLock: true}); err == nil {
		t.Fatal("--break-lock took the disk from a running bleen")
	}
}

// TestStaleLockWithReusedPID: after a crash or reboot the lock's process ID
// can belong to another program. That program started after the lock was
// written, so the lock is stale and must not block the disk.
func TestStaleLockWithReusedPID(t *testing.T) {
	f := newFixture(t)
	f.v.Close()
	f.v = nil
	cmd := exec.Command("ping", "-n", "30", "127.0.0.1")
	if runtime.GOOS != "windows" {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	host, _ := os.Hostname()
	lock := fmt.Sprintf(`{"host":%q,"pid":%d,"since":%q}`, host, cmd.Process.Pid, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano))
	os.WriteFile(filepath.Join(f.root, ".bleen", "lock"), []byte(lock), 0o644)
	v, err := vault.Open(f.root, vault.OpenOptions{})
	if err != nil {
		t.Fatalf("a lock with a reused process id blocked the disk: %v", err)
	}
	f.v = v
}

// TestIncrementalsStayWhenTheirFullIsIncomplete: a missing part of a full
// backup must not send that generation's incrementals away as orphans.
func TestIncrementalsStayWhenTheirFullIsIncomplete(t *testing.T) {
	f := newFixture(t)
	rnd := rand.New(rand.NewPCG(3, 3))
	for i := range 8 {
		f.write(fmt.Sprintf("f%d.bin", i), random(rnd, 30000))
	}
	f.backup(false, 100000)
	f.write("new.txt", []byte("later"))
	f.backup(false, 100000)
	f.v.Close()
	f.v = nil
	dir := filepath.Join(f.root, "src")
	all := archives(t, dir)
	var incr string
	for _, a := range all {
		if strings.Contains(a, "INCREMENTAL") {
			incr = a
		}
	}
	os.Remove(filepath.Join(f.root, ".bleen", "catalog.db"))
	os.Remove(filepath.Join(f.root, ".bleen", "catalog.db.1"))
	for _, a := range all {
		if strings.Contains(a, "FULL.part02") {
			os.Remove(filepath.Join(dir, a))
		}
	}
	v, err := vault.Open(f.root, vault.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.v = v
	if _, err := os.Stat(filepath.Join(dir, incr)); err != nil {
		t.Fatalf("the incremental was moved: %v (recovered: %v)", err, v.Recovered)
	}
}
