package engine

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/kaanbahasever/bleen/internal/archive"
	"github.com/kaanbahasever/bleen/internal/vault"
)

type VerifyReport struct {
	Archives int
	Bytes    int64
	Problems []archive.Issue
}

// VerifyVault re-reads every archive and checks it against the catalog and
// its own manifest, to catch damage on old disks ("Check backups").
func VerifyVault(ctx context.Context, v *vault.Vault, prog Progress) (*VerifyReport, error) {
	if prog == nil {
		prog = NopProgress{}
	}
	list, err := v.Catalog.Archives()
	if err != nil {
		return nil, err
	}
	rep := &VerifyReport{}
	prog.Phase("verifying")
	for i, a := range list {
		if ctx.Err() != nil {
			return rep, errorf(ECancelled, nil, "cancelled")
		}
		prog.Copying(CopyProgress{FilesDone: i, FilesTotal: len(list), Current: a.Rel})
		problem := func(code string, err error) {
			is := archive.Issue{Path: a.Rel, Code: code, Message: err.Error()}
			rep.Problems = append(rep.Problems, is)
			prog.Issue(is)
		}
		p := v.ArchivePath(a.Rel)
		fi, err := os.Stat(p)
		if err != nil {
			problem("E_ARCHIVE_MISSING", err)
			continue
		}
		if fi.Size() != a.Size {
			problem(EChecksumMismatch, fmt.Errorf("size is %d, catalog says %d", fi.Size(), a.Size))
			continue
		}
		sum, err := archive.FileSHA256(p)
		if err != nil {
			problem(EChecksumMismatch, err)
			continue
		}
		plain, cleanup, err := archive.Plain(p, v.Opener(), "")
		if err == nil {
			_, err = archive.VerifyPart(plain)
			cleanup()
		}
		if err != nil {
			problem(EChecksumMismatch, err)
			continue
		}
		if hex.EncodeToString(sum) != a.SHA256 {
			problem(EChecksumMismatch, fmt.Errorf("archive checksum differs from the catalog"))
			continue
		}
		rep.Archives++
		rep.Bytes += fi.Size()
	}
	return rep, nil
}
