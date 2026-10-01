// Package engine runs backups and restores. It knows nothing about any UI;
// callers observe it through the Progress interface.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kaanbahasever/bleen/internal/archive"
)

// Error codes shared with the UI and CLI.
const (
	ESourceUnreachable = "E_SOURCE_UNREACHABLE"
	ESourceEmpty       = "E_SOURCE_EMPTY"
	EMassChange        = "E_MASS_CHANGE"
	ECancelled         = "E_CANCELLED"
	EVaultFull         = "E_VAULT_FULL"
	EVaultWrite        = "E_VAULT_WRITE"
	EChecksumMismatch  = "E_CHECKSUM_MISMATCH"
	EDestNotEmpty      = "E_DEST_NOT_EMPTY"
	ENotFound          = "E_NOT_FOUND"
	EFileUnstable      = "E_FILE_UNSTABLE"
	ERenamed           = "E_RENAMED"
	ETooLarge          = "E_TOO_LARGE"
	EWriteFailed       = "E_WRITE_FAILED"
	EDestExists        = "E_DEST_EXISTS"
	EDestInVault       = "E_DEST_IN_VAULT"
)

// Error is a run-level failure with a stable code.
type Error struct {
	Code string
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

func errorf(code string, err error, format string, a ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...), Err: err}
}

// Plan is the preflight summary shown before a backup starts.
type Plan struct {
	SourceName   string    `json:"sourceName"`
	Origin       string    `json:"origin"`
	Kind         string    `json:"kind"`
	LastFull     time.Time `json:"lastFull"`
	LastBackup   time.Time `json:"lastBackup"`
	TotalFiles   int       `json:"totalFiles"`
	New          int       `json:"new"`
	Modified     int       `json:"modified"`
	Deleted      int       `json:"deleted"`
	BytesToRead  int64     `json:"bytesToRead"`
	EstStored    int64     `json:"estStored"`
	VaultFree    uint64    `json:"vaultFree"`
	MassChange   bool      `json:"massChange"`
	ScanIssues   int       `json:"scanIssues"`
	NothingToDo  bool      `json:"nothingToDo"`
	ChangedRatio float64   `json:"changedRatio"`
}

// CopyProgress is emitted while files are read and archived.
type CopyProgress struct {
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
	Current    string `json:"current"`
}

// Progress observes a running job. Calls come from a single goroutine.
type Progress interface {
	Scanning(filesSeen int)
	Planned(p *Plan)
	Copying(p CopyProgress)
	Phase(name string) // "verifying", "saving", "restoring"
	Issue(is archive.Issue)
}

// NopProgress ignores every event; embed it to implement only some methods.
type NopProgress struct{}

func (NopProgress) Scanning(int)         {}
func (NopProgress) Planned(*Plan)        {}
func (NopProgress) Copying(CopyProgress) {}
func (NopProgress) Phase(string)         {}
func (NopProgress) Issue(archive.Issue)  {}

// CleanupTemp removes temporary files a killed bleen left behind (spools,
// decrypted archives, catalog working copies). Only files older than a day
// are touched, so another running bleen is never disturbed.
func CleanupTemp() {
	for _, pat := range []string{"bleen-spool-*", "bleen-open-*.zip", "bleen-catalog-*.db*"} {
		files, _ := filepath.Glob(filepath.Join(os.TempDir(), pat))
		for _, f := range files {
			if fi, err := os.Stat(f); err == nil && time.Since(fi.ModTime()) > 24*time.Hour {
				os.Remove(f)
			}
		}
	}
}
