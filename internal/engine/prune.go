package engine

import (
	"os"

	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// PruneReport says what retention removed.
type PruneReport struct {
	Generations int
	Archives    int
	Bytes       int64
}

// Prune keeps the newest keep generations of a source and deletes older
// ones. Whole generations only: no archive is ever rewritten, and the
// newest generation is never touched.
//
// The catalog is published before files are deleted. If bleen stops in
// between, the leftover archives are simply re-imported on the next open
// and pruned again later; nothing is lost.
func Prune(v *vault.Vault, src *catalog.Source, keep int) (*PruneReport, error) {
	rep := &PruneReport{}
	if keep < 1 {
		return rep, nil
	}
	gens, err := v.Catalog.Generations(src.ID)
	if err != nil || len(gens) <= keep {
		return rep, err
	}
	var files []string
	for _, g := range gens[:len(gens)-keep] {
		rels, err := v.Catalog.DeleteGeneration(g.ID)
		if err != nil {
			return nil, err
		}
		files = append(files, rels...)
		rep.Generations++
		rep.Bytes += g.Bytes
	}
	if err := v.Publish(); err != nil {
		return nil, errorf(EVaultWrite, err, "can't save the catalog")
	}
	for _, rel := range files {
		if err := os.Remove(v.ArchivePath(rel)); err == nil {
			rep.Archives++
		}
	}
	return rep, nil
}
