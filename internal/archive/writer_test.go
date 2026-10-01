package archive

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// TestZip64LocalHeader: an entry of 4 GiB or more carries the ZIP64 field
// in its local header too, and the central directory has exactly one.
func TestZip64LocalHeader(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	data := []byte("not really five gigabytes")
	fh := &zip.FileHeader{Name: "big.bin", Method: zip.Store, CompressedSize64: uint64(len(data)), UncompressedSize64: 5 << 30}
	prepareRawHeader(fh, time.Now())
	w, err := createRaw(zw, fh)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if binary.LittleEndian.Uint32(b) != 0x04034b50 {
		t.Fatal("no local header")
	}
	nameLen := int(binary.LittleEndian.Uint16(b[26:]))
	extraLen := int(binary.LittleEndian.Uint16(b[28:]))
	if n := countZip64(b[30+nameLen : 30+nameLen+extraLen]); n != 1 {
		t.Fatalf("local header has %d ZIP64 fields, want 1", n)
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	f := zr.File[0]
	if f.UncompressedSize64 != 5<<30 || f.CompressedSize64 != uint64(len(data)) {
		t.Fatalf("sizes %d/%d", f.UncompressedSize64, f.CompressedSize64)
	}
	if n := countZip64(f.Extra); n != 1 {
		t.Fatalf("central directory has %d ZIP64 fields, want 1", n)
	}
}

func countZip64(extra []byte) int {
	n := 0
	for len(extra) >= 4 {
		id, size := binary.LittleEndian.Uint16(extra), int(binary.LittleEndian.Uint16(extra[2:]))
		if id == 0x0001 {
			n++
		}
		if 4+size > len(extra) {
			break
		}
		extra = extra[4+size:]
	}
	return n
}
