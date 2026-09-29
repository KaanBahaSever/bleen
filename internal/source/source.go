// Package source reads the folders bleen protects: local folders, UNC paths
// on Windows and mounted network shares elsewhere.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kaanbahasever/bleen/internal/archive"
)

// Entry is one filesystem entry, with a vault-style relative path
// ("klasor/resim.jpg": forward slashes, no leading slash).
type Entry struct {
	Path    string
	Kind    archive.Kind
	Size    int64
	ModTime time.Time
	Target  string // symlink target
}

// WalkIssue is a path that could not be read during a walk.
type WalkIssue struct {
	Path string
	Code string
	Err  error
}

// DefaultExcludes are skipped unless the user removes them.
var DefaultExcludes = []string{
	"~$*", "Thumbs.db", "desktop.ini", ".DS_Store", "*.tmp",
	"$RECYCLE.BIN/", "System Volume Information/",
}

// LocalFS is a source rooted at a local, UNC or mounted path.
type LocalFS struct {
	root     string
	Parallel int // concurrent directory listings
	excl     *Matcher
}

func NewLocal(root string, excludes []string) *LocalFS {
	par := 4
	if IsNetworkPath(root) {
		par = 8
	}
	return &LocalFS{root: filepath.Clean(root), Parallel: par, excl: NewMatcher(excludes)}
}

func (s *LocalFS) Root() string { return s.root }

// IsNetworkPath reports whether p is a UNC path.
func IsNetworkPath(p string) bool {
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}

// Probe checks that the root exists and is a directory.
func (s *LocalFS) Probe() error {
	fi, err := os.Stat(s.root)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", s.root)
	}
	return nil
}

func (s *LocalFS) abs(rel string) string {
	if rel == "" {
		return s.root
	}
	return filepath.Join(s.root, filepath.FromSlash(rel))
}

// Walk lists the tree in parallel. fn and issue are never called
// concurrently. An error is returned only if the root cannot be listed.
func (s *LocalFS) Walk(ctx context.Context, fn func(Entry), issue func(WalkIssue)) error {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		rootErr error
		sem     = make(chan struct{}, max(1, s.Parallel))
	)
	var visit func(rel string)
	visit = func(rel string) {
		defer wg.Done()
		if ctx.Err() != nil {
			return
		}
		sem <- struct{}{}
		des, err := os.ReadDir(s.abs(rel))
		<-sem
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if rel == "" {
				rootErr = err
			} else {
				issue(WalkIssue{Path: rel, Code: ErrCode(err), Err: err})
			}
			// os.ReadDir returns the entries it read before the error.
		}
		for _, de := range des {
			name := de.Name()
			child := name
			if rel != "" {
				child = rel + "/" + name
			}
			isDir := de.IsDir()
			if s.excl.Match(child, name, isDir) {
				continue
			}
			info, err := de.Info()
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					issue(WalkIssue{Path: child, Code: ErrCode(err), Err: err})
				}
				continue
			}
			mode := info.Mode()
			switch {
			case mode&fs.ModeSymlink != 0:
				target, err := os.Readlink(s.abs(child))
				if err != nil {
					issue(WalkIssue{Path: child, Code: ErrCode(err), Err: err})
					continue
				}
				fn(Entry{Path: child, Kind: archive.KindSymlink, ModTime: info.ModTime(), Target: filepath.ToSlash(target)})
			case isDir:
				fn(Entry{Path: child, Kind: archive.KindDir, ModTime: info.ModTime()})
				wg.Add(1)
				go visit(child)
			case mode.IsRegular():
				if isCloudPlaceholder(info) {
					issue(WalkIssue{Path: child, Code: "E_CLOUD_PLACEHOLDER",
						Err: errors.New("online-only cloud file; skipped so the backup does not download it")})
					continue
				}
				fn(Entry{Path: child, Kind: archive.KindFile, Size: info.Size(), ModTime: info.ModTime()})
			default:
				// Junctions, devices, sockets: not followed, not stored.
			}
		}
	}
	wg.Add(1)
	go visit("")
	wg.Wait()
	if rootErr != nil {
		return rootErr
	}
	return ctx.Err()
}

// Stat returns the current size and mtime of a file.
func (s *LocalFS) Stat(rel string) (Entry, error) {
	fi, err := os.Lstat(s.abs(rel))
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: rel, Kind: archive.KindFile, Size: fi.Size(), ModTime: fi.ModTime()}, nil
}

// Open opens a file for reading without blocking other users of the file.
func (s *LocalFS) Open(rel string) (io.ReadCloser, error) {
	return openShared(s.abs(rel))
}

// Matcher implements gitignore-style exclude patterns: a pattern without "/"
// matches a name at any depth; a pattern containing "/" matches the whole
// relative path; a trailing "/" restricts it to directories. Matching is
// case-insensitive on Windows and macOS.
type Matcher struct {
	pats []pattern
	fold bool
}

type pattern struct {
	glob    string
	dirOnly bool
	full    bool
}

func NewMatcher(patterns []string) *Matcher {
	m := &Matcher{fold: runtime.GOOS != "linux"}
	for _, p := range patterns {
		p = strings.TrimSpace(filepath.ToSlash(p))
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		pt := pattern{}
		if strings.HasSuffix(p, "/") {
			pt.dirOnly = true
			p = strings.TrimSuffix(p, "/")
		}
		if strings.Contains(p, "/") {
			pt.full = true
			p = strings.TrimPrefix(p, "/")
		}
		if m.fold {
			p = strings.ToLower(p)
		}
		pt.glob = p
		m.pats = append(m.pats, pt)
	}
	return m
}

func (m *Matcher) Match(rel, name string, isDir bool) bool {
	if m == nil {
		return false
	}
	if m.fold {
		rel, name = strings.ToLower(rel), strings.ToLower(name)
	}
	for _, p := range m.pats {
		if p.dirOnly && !isDir {
			continue
		}
		subject := name
		if p.full {
			subject = rel
		}
		if ok, _ := path.Match(p.glob, subject); ok {
			return true
		}
	}
	return false
}

// ErrCode maps an OS error to a stable bleen error code.
func ErrCode(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "E_ACCESS_DENIED"
	case errors.Is(err, fs.ErrNotExist):
		return "E_NOT_FOUND"
	case isSharingViolation(err):
		return "E_FILE_LOCKED"
	default:
		return "E_READ_FAILED"
	}
}

// IsSharingViolation reports whether err means another program has the
// file open exclusively (Windows).
func IsSharingViolation(err error) bool { return isSharingViolation(err) }
