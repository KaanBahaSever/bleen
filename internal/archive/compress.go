package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"hash/crc32"
	"io"
	"os"
	"path"
	"strings"

	"github.com/klauspost/compress/flate"
)

// MemSpoolLimit is the largest input kept in memory while compressing;
// bigger files spool to a temporary file on the local disk.
const MemSpoolLimit = 4 << 20

// Spool holds compressed entry data until the single archive writer takes it.
type Spool struct {
	buf  *bytes.Buffer
	file *os.File
}

// Reader rewinds and returns the compressed data.
func (s *Spool) Reader() (io.Reader, error) {
	if s.file != nil {
		if _, err := s.file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return s.file, nil
	}
	return bytes.NewReader(s.buf.Bytes()), nil
}

// Close releases memory or deletes the temporary file.
func (s *Spool) Close() {
	if s == nil {
		return
	}
	if s.file != nil {
		name := s.file.Name()
		s.file.Close()
		os.Remove(name)
	}
	s.buf = nil
}

// Compressed is the result of reading and compressing one file.
type Compressed struct {
	SHA256 [32]byte
	Blob   Blob
	Spool  *Spool
}

// Compress reads src once, hashing (SHA-256, CRC-32) and compressing it.
// sizeHint decides between a memory and a file spool; tmpDir is used for
// the latter. store selects zip.Store instead of Deflate.
func Compress(src io.Reader, name string, sizeHint int64, tmpDir string) (*Compressed, error) {
	sp := &Spool{}
	var out io.Writer
	if sizeHint > MemSpoolLimit {
		f, err := os.CreateTemp(tmpDir, "bleen-spool-*")
		if err != nil {
			return nil, err
		}
		sp.file = f
		out = f
	} else {
		sp.buf = bytes.NewBuffer(make([]byte, 0, min(sizeHint, MemSpoolLimit)/2+512))
		out = sp.buf
	}
	cnt := &countWriter{w: out}

	method := zip.Deflate
	if ShouldStore(name) {
		method = zip.Store
	}
	sha := sha256.New()
	crc := crc32.NewIEEE()
	var n int64
	var err error
	if method == zip.Store {
		n, err = io.Copy(io.MultiWriter(cnt, sha, crc), src)
	} else {
		fw, ferr := flate.NewWriter(cnt, 6)
		if ferr != nil {
			sp.Close()
			return nil, ferr
		}
		n, err = io.Copy(io.MultiWriter(fw, sha, crc), src)
		if err == nil {
			err = fw.Close()
		}
	}
	if err != nil {
		sp.Close()
		return nil, err
	}
	c := &Compressed{Spool: sp}
	copy(c.SHA256[:], sha.Sum(nil))
	c.Blob = Blob{
		Method:           method,
		CRC32:            crc.Sum32(),
		CompressedSize:   cnt.n,
		UncompressedSize: n,
	}
	return c, nil
}

// storedExt lists formats that are already compressed; deflating them again
// costs CPU and saves nothing.
var storedExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".heic": true, ".avif": true,
	".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".flac": true,
	".mp4": true, ".m4v": true, ".mov": true, ".mkv": true, ".avi": true, ".webm": true,
	".zip": true, ".7z": true, ".rar": true, ".gz": true, ".tgz": true, ".bz2": true, ".xz": true, ".zst": true,
	".docx": true, ".xlsx": true, ".pptx": true, ".odt": true, ".ods": true, ".odp": true,
	".pdf": true, ".jar": true, ".apk": true, ".epub": true,
}

func ShouldStore(name string) bool {
	return storedExt[strings.ToLower(path.Ext(name))]
}
