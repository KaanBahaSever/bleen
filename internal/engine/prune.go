package engine

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// PruneReport says what retention removed.
type PruneReport struct {
	Generations int
	Archives    int
	Bytes       int64
	// Held explains why nothing was deleted although the limit was reached.
	Held string
}

// Prune keeps the newest keep generations of a source and deletes older
// ones. Whole generations only: no archive is ever rewritten, and the
// newest generation is never touched.
//
// The catalog is published before files are deleted. Files are deleted
// newest first and deleting stops at the first failure, so whatever is left
// is always a full backup with its first incrementals: it is re-imported on
// the next open and pruned again later. Nothing is lost.
func Prune(v *vault.Vault, src *catalog.Source, keep int) (*PruneReport, error) {
	rep := &PruneReport{}
	if keep < 1 {
		return rep, nil
	}
	gens, err := v.Catalog.Generations(src.ID)
	if err != nil || len(gens) <= keep {
		return rep, err
	}
	drop, kept := gens[:len(gens)-keep], gens[len(gens)-keep:]
	if lost, err := wouldLoseSkipped(v, src, drop, kept); err != nil {
		return nil, err
	} else if lost != "" {
		rep.Held = "the newest full backup skipped " + lost + "; older backups that hold it are kept until a backup captures it"
		return rep, nil
	}
	var files [][]string // per generation, oldest generation first
	for _, g := range drop {
		rels, err := v.Catalog.DeleteGeneration(g.ID)
		if err != nil {
			return nil, err
		}
		files = append(files, rels)
		rep.Generations++
		rep.Bytes += g.Bytes
	}
	if err := v.Publish(); err != nil {
		return nil, errorf(EVaultWrite, err, "can't save the catalog")
	}
	for _, rels := range files {
		sort.Sort(sort.Reverse(sort.StringSlice(rels))) // names sort in backup order
		for _, rel := range rels {
			if err := os.Remove(v.ArchivePath(rel)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return rep, nil // the rest stays a complete chain
			}
			rep.Archives++
		}
	}
	return rep, nil
}

// wouldLoseSkipped returns a file that the newest full backup skipped (it
// was locked or unreadable then) whose only copies are in the generations
// about to be deleted. Files that are in no generation at all, such as a
// file too large for FAT32, never hold retention back.
func wouldLoseSkipped(v *vault.Vault, src *catalog.Source, drop, kept []catalog.Generation) (string, error) {
	snaps, err := v.Catalog.Snapshots(src.ID)
	if err != nil {
		return "", err
	}
	newest := kept[len(kept)-1]
	var skipped []string
	for _, s := range snaps {
		if s.GenerationID == newest.ID && s.Seq == 0 && s.FilesSkipped > 0 {
			issues, err := v.Catalog.Issues(s.ID)
			if err != nil {
				return "", err
			}
			for _, is := range issues {
				skipped = append(skipped, is.Path)
			}
		}
	}
	if len(skipped) == 0 {
		return "", nil
	}
	under := func(state map[string]catalog.Version, p string) bool {
		if _, ok := state[p]; ok {
			return true
		}
		for q := range state {
			if strings.HasPrefix(q, p+"/") {
				return true
			}
		}
		return false
	}
	stateOf := func(gens []catalog.Generation) ([]map[string]catalog.Version, error) {
		var out []map[string]catalog.Version
		for _, g := range gens {
			st, err := v.Catalog.CurrentState(src.ID, g.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, st)
		}
		return out, nil
	}
	keptStates, err := stateOf(kept)
	if err != nil {
		return "", err
	}
	dropStates, err := stateOf(drop)
	if err != nil {
		return "", err
	}
next:
	for _, p := range skipped {
		for _, st := range keptStates {
			if under(st, p) {
				continue next // a kept backup still has it
			}
		}
		for _, st := range dropStates {
			if under(st, p) {
				return p, nil
			}
		}
	}
	return "", nil
}
