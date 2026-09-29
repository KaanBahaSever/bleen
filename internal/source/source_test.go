package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaanbahasever/bleen/internal/archive"
)

func TestMatcher(t *testing.T) {
	m := NewMatcher(append([]string{"*.bak", "cache/", "docs/private/*"}, DefaultExcludes...))
	cases := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"a/~$rapor.docx", false, true},
		{"Thumbs.db", false, true},
		{"x/old.bak", false, true},
		{"x/cache", true, true},
		{"x/cache", false, false}, // dir-only pattern
		{"docs/private/secret.txt", false, true},
		{"other/private/secret.txt", false, false},
		{"rapor.docx", false, false},
		{"$RECYCLE.BIN", true, true},
	}
	for _, c := range cases {
		if got := m.Match(c.rel, filepath.Base(c.rel), c.isDir); got != c.want {
			t.Errorf("Match(%q, dir=%v) = %v, want %v", c.rel, c.isDir, got, c.want)
		}
	}
}

func TestWalk(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "klasör", "boş"), 0o755)
	os.WriteFile(filepath.Join(root, "klasör", "çalışma.txt"), []byte("abc"), 0o644)
	os.WriteFile(filepath.Join(root, "~$lock.docx"), []byte("x"), 0o644)

	got := map[string]archive.Kind{}
	err := NewLocal(root, DefaultExcludes).Walk(context.Background(),
		func(e Entry) { got[e.Path] = e.Kind },
		func(wi WalkIssue) { t.Errorf("issue: %+v", wi) })
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]archive.Kind{
		"klasör":             archive.KindDir,
		"klasör/boş":         archive.KindDir,
		"klasör/çalışma.txt": archive.KindFile,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for p, k := range want {
		if got[p] != k {
			t.Errorf("%s: got %q, want %q", p, got[p], k)
		}
	}

	if err := NewLocal(filepath.Join(root, "missing"), nil).Walk(context.Background(), func(Entry) {}, func(WalkIssue) {}); err == nil {
		t.Error("walking a missing root should fail")
	}
}
