package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Part is one finished archive file, still carrying its temporary name.
type Part struct {
	TempPath    string // ".../2026-09-26_1800_INCREMENTAL.zip.partial"
	FinalName   string // "2026-09-26_1800_INCREMENTAL.zip", "...part02.zip" or "….zip.age"
	Manifest    *Manifest
	Sealed      bool   // the file on disk is age-encrypted
	PlainSHA256 []byte // SHA-256 of the ZIP bytes before encryption
}

// Sealer encrypts an archive stream (see internal/seal). Nil means plain ZIP.
type Sealer interface {
	Wrap(w io.Writer) (io.WriteCloser, error)
}

// SealedExt is appended to encrypted archive names.
const SealedExt = ".age"

// Blob is compressed entry data ready to be appended with CreateRaw.
type Blob struct {
	Method           uint16 // zip.Store or zip.Deflate
	CRC32            uint32
	CompressedSize   int64
	UncompressedSize int64
	Data             io.Reader
}

// Writer produces the parts of one snapshot. It is not safe for concurrent
// use: bleen compresses in parallel and appends from a single goroutine.
type Writer struct {
	dir      string
	base     string
	maxPart  int64
	template Manifest
	sealer   Sealer

	cur   *partWriter
	parts []*partWriter
}

type partWriter struct {
	path     string
	f        *os.File
	enc      io.WriteCloser // non-nil when sealed
	plain    hash.Hash
	cw       *countWriter
	zw       *zip.Writer
	manifest Manifest
	files    int
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// NewWriter starts a snapshot archive in dir. base is the file name without
// extension. maxPart <= 0 means a single part of unlimited size.
func NewWriter(dir, base string, maxPart int64, template Manifest, sealer Sealer) (*Writer, error) {
	w := &Writer{dir: dir, base: base, maxPart: maxPart, template: template, sealer: sealer}
	w.template.Format = FormatV1
	w.template.Entries = nil
	w.template.Issues = nil
	if err := w.openPart(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) openPart() error {
	idx := len(w.parts) + 1
	ext := ".zip"
	if w.sealer != nil {
		ext += SealedExt
	}
	p := filepath.Join(w.dir, fmt.Sprintf("%s.part%02d%s.partial", w.base, idx, ext))
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	// zip → count → (plain hash, [encrypt] → file). Plain bytes never reach
	// the disk when the vault is encrypted.
	pw := &partWriter{path: p, f: f, plain: sha256.New(), manifest: w.template}
	var sink io.Writer = f
	if w.sealer != nil {
		if pw.enc, err = w.sealer.Wrap(f); err != nil {
			f.Close()
			os.Remove(p)
			return err
		}
		sink = pw.enc
	}
	pw.cw = &countWriter{w: io.MultiWriter(pw.plain, sink)}
	pw.zw = zip.NewWriter(pw.cw)
	pw.manifest.Part = PartInfo{Index: idx}
	w.parts = append(w.parts, pw)
	w.cur = pw
	return nil
}

// AddMeta records an entry that carries no bytes in this archive
// (deletions, directories, symlinks, references).
func (w *Writer) AddMeta(e Entry) error {
	if e.Kind == KindDir && e.Op != OpDeleted {
		// A directory entry keeps empty folders when the ZIP is extracted by hand.
		fh := &zip.FileHeader{Name: FilesPrefix + e.Path + "/", Method: zip.Store, Modified: e.MTime}
		fh.SetMode(0o755 | fs.ModeDir)
		if _, err := w.cur.zw.CreateHeader(fh); err != nil {
			return err
		}
	}
	w.cur.manifest.Entries = append(w.cur.manifest.Entries, e)
	return nil
}

// AddIssue records a per-file problem in the manifest.
func (w *Writer) AddIssue(is Issue) {
	w.cur.manifest.Issues = append(w.cur.manifest.Issues, is)
}

// AddFile appends pre-compressed file data and records the entry. It returns
// the part-relative archive name ("<final part name>") and the ZIP entry name.
func (w *Writer) AddFile(e Entry, b Blob) (partIndex int, zipName string, err error) {
	if w.maxPart > 0 && w.cur.files > 0 && w.cur.cw.n+b.CompressedSize+4096 > w.maxPart {
		if err := w.closePart(false); err != nil {
			return 0, "", err
		}
		if err := w.openPart(); err != nil {
			return 0, "", err
		}
	}
	zipName = FilesPrefix + e.Path
	fh := &zip.FileHeader{
		Name:               zipName,
		Method:             b.Method,
		CRC32:              b.CRC32,
		CompressedSize64:   uint64(b.CompressedSize),
		UncompressedSize64: uint64(b.UncompressedSize),
	}
	prepareRawHeader(fh, e.MTime)
	dst, err := w.cur.zw.CreateRaw(fh)
	if err != nil {
		return 0, "", err
	}
	n, err := io.Copy(dst, b.Data)
	if err != nil {
		return 0, "", err
	}
	if n != b.CompressedSize {
		return 0, "", fmt.Errorf("%s: wrote %d compressed bytes, expected %d", e.Path, n, b.CompressedSize)
	}
	e.Zip = zipName
	w.cur.manifest.Entries = append(w.cur.manifest.Entries, e)
	w.cur.files++
	return w.cur.manifest.Part.Index, zipName, nil
}

// PartName returns the final file name of part idx given the final part count.
func PartName(base string, idx, count int) string {
	if count == 1 {
		return base + ".zip"
	}
	return fmt.Sprintf("%s.part%02d.zip", base, idx)
}

// CurrentPart is the index of the part that receives the next entry.
func (w *Writer) CurrentPart() int { return w.cur.manifest.Part.Index }

// Close finishes every part (manifest, DELETED.txt, README.txt) and fsyncs.
// finishedAt is stamped on all manifests.
func (w *Writer) Close(finishedAt time.Time) ([]Part, error) {
	for _, p := range w.parts {
		p.manifest.Snapshot.FinishedAt = finishedAt
	}
	if err := w.closePart(true); err != nil {
		return nil, err
	}
	out := make([]Part, 0, len(w.parts))
	for _, p := range w.parts {
		m := p.manifest
		name := PartName(w.base, m.Part.Index, len(w.parts))
		if w.sealer != nil {
			name += SealedExt
		}
		out = append(out, Part{
			TempPath:    p.path,
			FinalName:   name,
			Manifest:    &m,
			Sealed:      w.sealer != nil,
			PlainSHA256: p.plain.Sum(nil),
		})
	}
	return out, nil
}

// Abort closes and removes every part written so far.
func (w *Writer) Abort() {
	for _, p := range w.parts {
		if p.zw != nil {
			p.zw.Close()
		}
		p.f.Close()
		os.Remove(p.path)
	}
}

func (w *Writer) closePart(last bool) error {
	p := w.cur
	// Parts closed before the run ended carry a zero FinishedAt; readers
	// take it from the last part.
	p.manifest.Part.Last = last
	var deleted []string
	for _, e := range p.manifest.Entries {
		if e.Op == OpDeleted {
			deleted = append(deleted, e.Path)
		}
	}
	if len(deleted) > 0 {
		sort.Strings(deleted)
		if err := writeSmall(p.zw, DeletedName, []byte(strings.Join(deleted, "\r\n")+"\r\n")); err != nil {
			return err
		}
	}
	if err := writeSmall(p.zw, ReadmeName, []byte(readmeText)); err != nil {
		return err
	}
	mb, err := json.MarshalIndent(p.manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeSmall(p.zw, ManifestName, mb); err != nil {
		return err
	}
	if err := p.zw.Close(); err != nil {
		return err
	}
	p.zw = nil
	if p.enc != nil {
		if err := p.enc.Close(); err != nil {
			return err
		}
	}
	if err := p.f.Sync(); err != nil {
		return err
	}
	return p.f.Close()
}

func writeSmall(zw *zip.Writer, name string, data []byte) error {
	fh := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()}
	w, err := zw.CreateHeader(fh)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, bytes.NewReader(data))
	return err
}

// prepareRawHeader does for CreateRaw what zip.Writer.CreateHeader does
// implicitly: UTF-8 flag, version fields and the extended-timestamp field.
func prepareRawHeader(fh *zip.FileHeader, mtime time.Time) {
	for i := 0; i < len(fh.Name); i++ {
		if fh.Name[i] >= 0x80 {
			fh.Flags |= 0x800
			break
		}
	}
	fh.CreatorVersion = 20
	fh.ReaderVersion = 20
	if fh.UncompressedSize64 >= 0xffffffff || fh.CompressedSize64 >= 0xffffffff {
		fh.ReaderVersion = 45
	}
	if mtime.IsZero() {
		mtime = time.Now()
	}
	fh.SetModTime(mtime) //nolint:staticcheck // sets the MS-DOS fields CreateRaw would otherwise leave empty
	var eb [9]byte
	binary.LittleEndian.PutUint16(eb[0:], 0x5455) // extended timestamp
	binary.LittleEndian.PutUint16(eb[2:], 5)
	eb[4] = 1 // mtime present
	binary.LittleEndian.PutUint32(eb[5:], uint32(mtime.Unix()))
	fh.Extra = append(fh.Extra, eb[:]...)
}

const readmeText = `bleen backup archive
====================

EN  This ZIP file was made by bleen (https://github.com/kaanbahasever/bleen).
    files/        the files that were new or changed at this backup
    DELETED.txt   paths that were deleted since the previous backup
    bleen-manifest.json   exact details (sizes, dates, SHA-256)

    To restore without bleen: extract the FULL archive, then every
    INCREMENTAL archive in date order (overwrite when asked), and delete
    the paths listed in each DELETED.txt.

TR  Bu ZIP dosyası bleen tarafından oluşturuldu.
    files/        bu yedekte yeni olan veya değişen dosyalar
    DELETED.txt   önceki yedekten bu yana silinen dosyalar
    bleen-manifest.json   ayrıntılar (boyut, tarih, SHA-256)

    bleen olmadan geri yüklemek için: önce FULL arşivini, sonra tüm
    INCREMENTAL arşivlerini tarih sırasıyla çıkarın (üzerine yazın) ve
    her DELETED.txt içindeki dosyaları silin.
`
