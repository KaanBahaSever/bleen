// Package archive implements the bleen archive v1 format: one standard ZIP
// file (or several standalone parts) per backup run, each carrying a
// machine-readable manifest so the vault catalog can always be rebuilt.
package archive

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	FormatV1     = "bleen.archive/v1"
	ManifestName = "bleen-manifest.json"
	DeletedName  = "DELETED.txt"
	FilesPrefix  = "files/"
)

// Op is what happened to a path in this snapshot.
type Op string

const (
	OpAdded    Op = "added"
	OpModified Op = "modified"
	OpDeleted  Op = "deleted"
)

// Kind is the type of a filesystem entry.
type Kind string

const (
	KindFile    Kind = "file"
	KindDir     Kind = "dir"
	KindSymlink Kind = "symlink"
)

// Manifest describes one archive part. The parts of a snapshot share every
// field except Part, Entries and Issues.
type Manifest struct {
	Format   string       `json:"format"`
	VaultID  string       `json:"vault_id"`
	Source   SourceInfo   `json:"source"`
	Snapshot SnapshotInfo `json:"snapshot"`
	Part     PartInfo     `json:"part"`
	Entries  []Entry      `json:"entries"`
	Issues   []Issue      `json:"issues,omitempty"`
}

type SourceInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Host   string `json:"host"`
}

type SnapshotInfo struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"` // "full" | "incremental"
	Generation int       `json:"generation"`
	Seq        int       `json:"seq"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type PartInfo struct {
	Index int  `json:"index"`
	Last  bool `json:"last"`
}

// Entry is one change. A file entry has either Zip (bytes stored in this
// part) or Ref (bytes already stored in an earlier archive: a move, a copy,
// or a metadata-only change).
type Entry struct {
	Op     Op        `json:"op"`
	Path   string    `json:"path"`
	Kind   Kind      `json:"kind,omitempty"`
	Size   int64     `json:"size,omitempty"`
	MTime  time.Time `json:"mtime,omitzero"`
	SHA256 string    `json:"sha256,omitempty"`
	Zip    string    `json:"zip,omitempty"`
	Ref    *Ref      `json:"ref,omitempty"`
	Target string    `json:"target,omitempty"` // symlink target

	// KeepOnDisk leaves a deletion out of DELETED.txt: the path was only
	// renamed in letter case and the new name is in the same backup.
	KeepOnDisk bool `json:"-"`
}

// Ref points at bytes stored in another archive of the same vault.
// Archive is relative to the vault root, e.g. "_proje/2026-09-25_1830_FULL.zip".
type Ref struct {
	Archive string `json:"archive"`
	Zip     string `json:"zip"`
}

type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e Entry) EntryKind() Kind {
	if e.Kind == "" {
		return KindFile
	}
	return e.Kind
}

func (m *Manifest) Validate() error {
	if m.Format != FormatV1 {
		return fmt.Errorf("unsupported archive format %q", m.Format)
	}
	if m.Snapshot.ID == "" || m.Source.ID == "" {
		return fmt.Errorf("manifest is missing snapshot or source id")
	}
	for _, e := range m.Entries {
		if e.Path == "" {
			return fmt.Errorf("manifest entry with empty path")
		}
		switch e.Op {
		case OpAdded, OpModified, OpDeleted:
		default:
			return fmt.Errorf("manifest entry %q has unknown op %q", e.Path, e.Op)
		}
	}
	return nil
}

func DecodeManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
