// Package catalog is bleen's SQLite index of every file version in a vault.
// It is derived data: every row can be rebuilt from the archive manifests
// with ApplySnapshot, which is also the only way snapshots are recorded.
package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kaanbahasever/bleen/internal/archive"
)

const schemaVersion = 1

var ErrSnapshotExists = errors.New("snapshot already in catalog")

// ErrNoFull means an incremental's generation has no full backup in the
// catalog (for example one left behind when retention deleted the rest).
var ErrNoFull = errors.New("generation has no full backup")

type DB struct{ db *sql.DB }

type Source struct {
	ID     int64
	UUID   string
	Name   string
	Origin string
	Host   string
	Folder string
}

type Snapshot struct {
	ID            int64
	UUID          string
	SourceID      int64
	GenerationID  int64
	Generation    int
	Seq           int
	Kind          string
	StartedAt     time.Time
	FinishedAt    time.Time
	FilesNew      int
	FilesModified int
	FilesDeleted  int
	FilesDeduped  int
	FilesSkipped  int
	BytesSource   int64
	BytesStored   int64
}

type Version struct {
	Path       string
	Kind       archive.Kind
	Size       int64
	MTimeNS    int64
	SHA256     string
	LinkTarget string
	Archive    string // vault-relative archive path, "" for dirs/symlinks
	EntryName  string
}

// AppliedPart is one archive part being recorded.
type AppliedPart struct {
	Filename string // final file name inside the source folder
	Size     int64
	SHA256   string
	Manifest *archive.Manifest
}

type ArchiveInfo struct {
	Rel    string // "_proje/2026-09-25_1830_FULL.zip"
	Size   int64
	SHA256 string
}

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sources (
  id INTEGER PRIMARY KEY,
  uuid TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  origin TEXT NOT NULL,
  host TEXT NOT NULL,
  folder TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS generations (
  id INTEGER PRIMARY KEY,
  source_id INTEGER NOT NULL REFERENCES sources(id),
  number INTEGER NOT NULL,
  started_at INTEGER NOT NULL,
  retired_at INTEGER,
  UNIQUE (source_id, number)
);
CREATE TABLE IF NOT EXISTS snapshots (
  id INTEGER PRIMARY KEY,
  uuid TEXT NOT NULL UNIQUE,
  source_id INTEGER NOT NULL REFERENCES sources(id),
  generation_id INTEGER NOT NULL REFERENCES generations(id),
  seq INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('full','incremental')),
  started_at INTEGER NOT NULL,
  finished_at INTEGER NOT NULL,
  files_new INTEGER NOT NULL,
  files_modified INTEGER NOT NULL,
  files_deleted INTEGER NOT NULL,
  files_deduped INTEGER NOT NULL,
  files_skipped INTEGER NOT NULL,
  bytes_source INTEGER NOT NULL,
  bytes_stored INTEGER NOT NULL,
  UNIQUE (generation_id, seq)
);
CREATE TABLE IF NOT EXISTS archives (
  id INTEGER PRIMARY KEY,
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id),
  source_id INTEGER NOT NULL REFERENCES sources(id),
  part INTEGER NOT NULL,
  filename TEXT NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  UNIQUE (source_id, filename)
);
CREATE TABLE IF NOT EXISTS file_versions (
  id INTEGER PRIMARY KEY,
  source_id INTEGER NOT NULL REFERENCES sources(id),
  generation_id INTEGER NOT NULL REFERENCES generations(id),
  path TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('file','dir','symlink')),
  size INTEGER NOT NULL,
  mtime_ns INTEGER NOT NULL,
  sha256 TEXT,
  link_target TEXT,
  from_seq INTEGER NOT NULL,
  to_seq INTEGER,
  end_reason TEXT CHECK (end_reason IN ('modified','deleted')),
  archive_id INTEGER REFERENCES archives(id),
  entry_name TEXT
);
CREATE INDEX IF NOT EXISTS fv_current ON file_versions(source_id, generation_id, path) WHERE to_seq IS NULL;
CREATE INDEX IF NOT EXISTS fv_at ON file_versions(source_id, generation_id, from_seq, to_seq);
CREATE INDEX IF NOT EXISTS fv_content ON file_versions(size, sha256);
CREATE TABLE IF NOT EXISTS file_issues (
  id INTEGER PRIMARY KEY,
  snapshot_id INTEGER NOT NULL REFERENCES snapshots(id),
  path TEXT NOT NULL,
  code TEXT NOT NULL,
  message TEXT NOT NULL
);
CREATE VIEW IF NOT EXISTS deleted_files AS
  SELECT source_id, generation_id, path, to_seq AS deleted_in_seq
  FROM file_versions WHERE end_reason = 'deleted';
`

// Open opens or creates a catalog database file.
func Open(file string) (*DB, error) {
	// The path goes into a URI: escape characters that end or alter it
	// (a user name like "Ali#1" would otherwise open the wrong file).
	esc := strings.NewReplacer("%", "%25", "#", "%23", "?", "%3F").Replace(file)
	db, err := sql.Open("sqlite", "file:"+esc+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("catalog schema: %w", err)
	}
	c := &DB{db: db}
	v, err := c.meta("schema_version")
	if err != nil {
		db.Close()
		return nil, err
	}
	switch v {
	case "":
		if err := c.setMeta(db, "schema_version", strconv.Itoa(schemaVersion)); err != nil {
			db.Close()
			return nil, err
		}
	case strconv.Itoa(schemaVersion):
	default:
		db.Close()
		return nil, fmt.Errorf("catalog schema version %s is newer than this bleen understands", v)
	}
	return c, nil
}

func (c *DB) Close() error { return c.db.Close() }

// Check runs SQLite's quick integrity check.
func (c *DB) Check() error {
	var res string
	if err := c.db.QueryRow("PRAGMA quick_check").Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("catalog damaged: %s", res)
	}
	return nil
}

// SnapshotTo writes a consistent copy of the database to file.
func (c *DB) SnapshotTo(file string) error {
	_, err := c.db.Exec("VACUUM INTO ?", file)
	return err
}

func (c *DB) meta(key string) (string, error) {
	var v string
	err := c.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (c *DB) setMeta(x execer, key, value string) error {
	_, err := x.Exec("INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// Revision increases with every recorded snapshot.
func (c *DB) Revision() (int64, error) {
	v, err := c.meta("revision")
	if err != nil || v == "" {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

func (c *DB) Sources() ([]Source, error) {
	rows, err := c.db.Query("SELECT id, uuid, name, origin, host, folder FROM sources ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.ID, &s.UUID, &s.Name, &s.Origin, &s.Host, &s.Folder); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// FindSource matches a source by name, folder, uuid (or uuid prefix) or origin.
func (c *DB) FindSource(key string) (*Source, error) {
	all, err := c.Sources()
	if err != nil {
		return nil, err
	}
	var hits []Source
	for _, s := range all {
		if strings.EqualFold(s.Name, key) || strings.EqualFold(s.Folder, key) ||
			strings.EqualFold(s.Origin, key) || (len(key) >= 6 && strings.HasPrefix(s.UUID, key)) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return nil, nil
	case 1:
		return &hits[0], nil
	default:
		return nil, fmt.Errorf("%q matches %d sources; use the folder name or id", key, len(hits))
	}
}

// SourceByOrigin finds the source backed up from origin on host.
func (c *DB) SourceByOrigin(host, origin string) (*Source, error) {
	all, err := c.Sources()
	if err != nil {
		return nil, err
	}
	for _, s := range all {
		if strings.EqualFold(s.Host, host) && strings.EqualFold(s.Origin, origin) {
			return &s, nil
		}
	}
	return nil, nil
}

func (c *DB) FolderTaken(folder string) (bool, error) {
	var n int
	err := c.db.QueryRow("SELECT COUNT(*) FROM sources WHERE folder = ? COLLATE NOCASE", folder).Scan(&n)
	return n > 0, err
}

const snapshotCols = `s.id, s.uuid, s.source_id, s.generation_id, g.number, s.seq, s.kind, s.started_at, s.finished_at,
  s.files_new, s.files_modified, s.files_deleted, s.files_deduped, s.files_skipped, s.bytes_source, s.bytes_stored`

func scanSnapshot(sc interface{ Scan(...any) error }) (Snapshot, error) {
	var s Snapshot
	var st, fin int64
	err := sc.Scan(&s.ID, &s.UUID, &s.SourceID, &s.GenerationID, &s.Generation, &s.Seq, &s.Kind, &st, &fin,
		&s.FilesNew, &s.FilesModified, &s.FilesDeleted, &s.FilesDeduped, &s.FilesSkipped, &s.BytesSource, &s.BytesStored)
	s.StartedAt = time.UnixMilli(st)
	s.FinishedAt = time.UnixMilli(fin)
	return s, err
}

// Snapshots lists a source's snapshots, oldest first.
func (c *DB) Snapshots(sourceID int64) ([]Snapshot, error) {
	rows, err := c.db.Query(`SELECT `+snapshotCols+` FROM snapshots s JOIN generations g ON g.id = s.generation_id
		WHERE s.source_id = ? ORDER BY g.number, s.seq`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// LatestSnapshot returns the newest snapshot of a source, or nil.
func (c *DB) LatestSnapshot(sourceID int64) (*Snapshot, error) {
	s, err := scanSnapshot(c.db.QueryRow(`SELECT `+snapshotCols+` FROM snapshots s JOIN generations g ON g.id = s.generation_id
		WHERE s.source_id = ? ORDER BY g.number DESC, s.seq DESC LIMIT 1`, sourceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *DB) HasSnapshot(uuid string) (bool, error) {
	var n int
	err := c.db.QueryRow("SELECT COUNT(*) FROM snapshots WHERE uuid = ?", uuid).Scan(&n)
	return n > 0, err
}

const versionCols = `v.path, v.kind, v.size, v.mtime_ns, COALESCE(v.sha256, ''), COALESCE(v.link_target, ''),
  COALESCE(src.folder || '/' || a.filename, ''), COALESCE(v.entry_name, '')`

const versionJoin = ` FROM file_versions v
  LEFT JOIN archives a ON a.id = v.archive_id
  LEFT JOIN sources src ON src.id = a.source_id `

func scanVersions(rows *sql.Rows) ([]Version, error) {
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var v Version
		var kind string
		if err := rows.Scan(&v.Path, &kind, &v.Size, &v.MTimeNS, &v.SHA256, &v.LinkTarget, &v.Archive, &v.EntryName); err != nil {
			return nil, err
		}
		v.Kind = archive.Kind(kind)
		out = append(out, v)
	}
	return out, rows.Err()
}

// CurrentState returns the latest version of every path in a generation.
func (c *DB) CurrentState(sourceID, generationID int64) (map[string]Version, error) {
	rows, err := c.db.Query(`SELECT `+versionCols+versionJoin+`
		WHERE v.source_id = ? AND v.generation_id = ? AND v.to_seq IS NULL`, sourceID, generationID)
	if err != nil {
		return nil, err
	}
	list, err := scanVersions(rows)
	if err != nil {
		return nil, err
	}
	m := make(map[string]Version, len(list))
	for _, v := range list {
		m[v.Path] = v
	}
	return m, nil
}

// StateAt returns every path as it was at snapshot seq, grouped by archive.
func (c *DB) StateAt(sourceID, generationID int64, seq int) ([]Version, error) {
	rows, err := c.db.Query(`SELECT `+versionCols+versionJoin+`
		WHERE v.source_id = ? AND v.generation_id = ? AND v.from_seq <= ? AND (v.to_seq IS NULL OR v.to_seq > ?)
		ORDER BY a.id, v.path`, sourceID, generationID, seq, seq)
	if err != nil {
		return nil, err
	}
	return scanVersions(rows)
}

// Archives lists every archive part in the vault.
func (c *DB) Archives() ([]ArchiveInfo, error) {
	rows, err := c.db.Query(`SELECT src.folder || '/' || a.filename, a.size, a.sha256 FROM archives a
		JOIN sources src ON src.id = a.source_id ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveInfo
	for rows.Next() {
		var a ArchiveInfo
		if err := rows.Scan(&a.Rel, &a.Size, &a.SHA256); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Issues returns the per-file problems recorded with a snapshot.
func (c *DB) Issues(snapshotID int64) ([]archive.Issue, error) {
	rows, err := c.db.Query("SELECT path, code, message FROM file_issues WHERE snapshot_id = ? ORDER BY id", snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []archive.Issue
	for rows.Next() {
		var is archive.Issue
		if err := rows.Scan(&is.Path, &is.Code, &is.Message); err != nil {
			return nil, err
		}
		out = append(out, is)
	}
	return out, rows.Err()
}

// Generation summarizes one full backup and its incrementals.
type Generation struct {
	ID        int64
	Number    int
	Snapshots int
	Bytes     int64
	FirstAt   time.Time
	LastAt    time.Time
}

// Generations lists a source's generations, oldest first.
func (c *DB) Generations(sourceID int64) ([]Generation, error) {
	rows, err := c.db.Query(`SELECT g.id, g.number, COUNT(s.id), COALESCE(SUM(s.bytes_stored), 0),
		COALESCE(MIN(s.started_at), 0), COALESCE(MAX(s.finished_at), 0)
		FROM generations g LEFT JOIN snapshots s ON s.generation_id = g.id
		WHERE g.source_id = ? GROUP BY g.id ORDER BY g.number`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Generation
	for rows.Next() {
		var g Generation
		var first, last int64
		if err := rows.Scan(&g.ID, &g.Number, &g.Snapshots, &g.Bytes, &first, &last); err != nil {
			return nil, err
		}
		g.FirstAt, g.LastAt = time.UnixMilli(first), time.UnixMilli(last)
		out = append(out, g)
	}
	return out, rows.Err()
}

// DeleteGeneration forgets a generation and returns the vault-relative
// paths of its archives, which the caller deletes afterwards. References
// never cross generations, so no other snapshot depends on these archives.
func (c *DB) DeleteGeneration(generationID int64) (archives []string, err error) {
	rows, err := c.db.Query(`SELECT src.folder || '/' || a.filename FROM archives a
		JOIN snapshots s ON s.id = a.snapshot_id JOIN sources src ON src.id = a.source_id
		WHERE s.generation_id = ?`, generationID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			rows.Close()
			return nil, err
		}
		archives = append(archives, rel)
	}
	rows.Close()
	tx, err := c.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	for _, q := range []string{
		`DELETE FROM file_versions WHERE generation_id = ?`,
		`DELETE FROM file_issues WHERE snapshot_id IN (SELECT id FROM snapshots WHERE generation_id = ?)`,
		`DELETE FROM archives WHERE snapshot_id IN (SELECT id FROM snapshots WHERE generation_id = ?)`,
		`DELETE FROM snapshots WHERE generation_id = ?`,
		`DELETE FROM generations WHERE id = ?`,
	} {
		if _, err = tx.Exec(q, generationID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(`INSERT INTO meta(key, value) VALUES('revision', '1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1`); err != nil {
		return nil, err
	}
	return archives, tx.Commit()
}

// ApplySnapshot records one snapshot from its archive manifests in a single
// transaction. folder is the source's folder inside the vault. It is used for
// normal commits, crash recovery and full catalog rebuilds alike.
func (c *DB) ApplySnapshot(folder string, parts []AppliedPart) (err error) {
	if len(parts) == 0 {
		return errors.New("no archive parts")
	}
	m0 := parts[0].Manifest
	if ok, err := c.HasSnapshot(m0.Snapshot.ID); err != nil {
		return err
	} else if ok {
		return ErrSnapshotExists
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	var sourceID int64
	err = tx.QueryRow("SELECT id FROM sources WHERE uuid = ?", m0.Source.ID).Scan(&sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		var res sql.Result
		res, err = tx.Exec("INSERT INTO sources(uuid, name, origin, host, folder, created_at) VALUES(?,?,?,?,?,?)",
			m0.Source.ID, m0.Source.Name, m0.Source.Origin, m0.Source.Host, folder, m0.Snapshot.StartedAt.UnixMilli())
		if err != nil {
			return err
		}
		sourceID, _ = res.LastInsertId()
	} else if err != nil {
		return err
	}

	sn := m0.Snapshot
	var genID int64
	err = tx.QueryRow("SELECT id FROM generations WHERE source_id = ? AND number = ?", sourceID, sn.Generation).Scan(&genID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if sn.Seq != 0 {
			return fmt.Errorf("snapshot %s: generation %d: %w", sn.ID, sn.Generation, ErrNoFull)
		}
		var res sql.Result
		res, err = tx.Exec("INSERT INTO generations(source_id, number, started_at) VALUES(?,?,?)", sourceID, sn.Generation, sn.StartedAt.UnixMilli())
		if err != nil {
			return err
		}
		genID, _ = res.LastInsertId()
	case err != nil:
		return err
	default:
		var maxSeq int
		if err = tx.QueryRow("SELECT COALESCE(MAX(seq), -1) FROM snapshots WHERE generation_id = ?", genID).Scan(&maxSeq); err != nil {
			return err
		}
		if sn.Seq != maxSeq+1 {
			return fmt.Errorf("snapshot %s: expected seq %d in generation %d, got %d", sn.ID, maxSeq+1, sn.Generation, sn.Seq)
		}
	}

	var finished time.Time
	var st struct {
		added, modified, deleted, deduped, skipped int
		src, stored                                int64
	}
	for _, p := range parts {
		if p.Manifest.Snapshot.FinishedAt.After(finished) {
			finished = p.Manifest.Snapshot.FinishedAt
		}
		st.stored += p.Size
		st.skipped += len(p.Manifest.Issues)
		for _, e := range p.Manifest.Entries {
			if e.EntryKind() != archive.KindFile {
				continue
			}
			switch e.Op {
			case archive.OpAdded:
				st.added++
			case archive.OpModified:
				st.modified++
			case archive.OpDeleted:
				st.deleted++
			}
			if e.Op != archive.OpDeleted {
				st.src += e.Size
				if e.Ref != nil {
					st.deduped++
				}
			}
		}
	}
	res, err := tx.Exec(`INSERT INTO snapshots(uuid, source_id, generation_id, seq, kind, started_at, finished_at,
		files_new, files_modified, files_deleted, files_deduped, files_skipped, bytes_source, bytes_stored)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sn.ID, sourceID, genID, sn.Seq, sn.Kind, sn.StartedAt.UnixMilli(), finished.UnixMilli(),
		st.added, st.modified, st.deleted, st.deduped, st.skipped, st.src, st.stored)
	if err != nil {
		return err
	}
	snapID, _ := res.LastInsertId()

	partIDs := make(map[int]int64, len(parts))
	for _, p := range parts {
		res, err = tx.Exec("INSERT INTO archives(snapshot_id, source_id, part, filename, size, sha256) VALUES(?,?,?,?,?,?)",
			snapID, sourceID, p.Manifest.Part.Index, p.Filename, p.Size, p.SHA256)
		if err != nil {
			return err
		}
		partIDs[p.Manifest.Part.Index], _ = res.LastInsertId()
	}

	endStmt, err := tx.Prepare(`UPDATE file_versions SET to_seq = ?, end_reason = ?
		WHERE source_id = ? AND generation_id = ? AND path = ? AND to_seq IS NULL`)
	if err != nil {
		return err
	}
	defer endStmt.Close()
	insStmt, err := tx.Prepare(`INSERT INTO file_versions(source_id, generation_id, path, kind, size, mtime_ns, sha256,
		link_target, from_seq, archive_id, entry_name) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insStmt.Close()

	for _, p := range parts {
		for _, e := range p.Manifest.Entries {
			if e.Op == archive.OpDeleted {
				if _, err = endStmt.Exec(sn.Seq, "deleted", sourceID, genID, e.Path); err != nil {
					return err
				}
				continue
			}
			if _, err = endStmt.Exec(sn.Seq, "modified", sourceID, genID, e.Path); err != nil {
				return err
			}
			var archiveID, entryName, sha, target any
			switch {
			case e.Zip != "":
				archiveID, entryName = partIDs[p.Manifest.Part.Index], e.Zip
			case e.Ref != nil:
				id, rerr := archiveIDByRel(tx, sourceID, e.Ref.Archive)
				if rerr != nil {
					return fmt.Errorf("%s: %w", e.Path, rerr)
				}
				archiveID, entryName = id, e.Ref.Zip
			}
			if e.SHA256 != "" {
				sha = e.SHA256
			}
			if e.Target != "" {
				target = e.Target
			}
			var mtime int64
			if !e.MTime.IsZero() {
				mtime = e.MTime.UnixNano()
			}
			if _, err = insStmt.Exec(sourceID, genID, e.Path, string(e.EntryKind()), e.Size, mtime, sha, target,
				sn.Seq, archiveID, entryName); err != nil {
				return err
			}
		}
		for _, is := range p.Manifest.Issues {
			if _, err = tx.Exec("INSERT INTO file_issues(snapshot_id, path, code, message) VALUES(?,?,?,?)",
				snapID, is.Path, is.Code, is.Message); err != nil {
				return err
			}
		}
	}

	// Use the transaction: the pool has a single connection.
	if _, err = tx.Exec(`INSERT INTO meta(key, value) VALUES('revision', '1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1`); err != nil {
		return err
	}
	return tx.Commit()
}

// archiveIDByRel resolves a reference by file name within the source. The
// folder part is ignored: the source folder may have been renamed on disk
// since, and references never leave their source.
func archiveIDByRel(tx *sql.Tx, sourceID int64, rel string) (int64, error) {
	file := path.Base(rel)
	var id int64
	err := tx.QueryRow(`SELECT id FROM archives WHERE source_id = ? AND filename = ?`, sourceID, file).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("referenced archive %s is not in the catalog", rel)
	}
	return id, err
}
