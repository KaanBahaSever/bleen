package archive

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// ReadManifest returns the manifest embedded in the ZIP file at path.
func ReadManifest(path string) (*Manifest, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return manifestFrom(&zr.Reader)
}

func manifestFrom(zr *zip.Reader) (*Manifest, error) {
	for _, f := range zr.File {
		if f.Name != ManifestName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, 512<<20))
		if err != nil {
			return nil, err
		}
		return DecodeManifest(b)
	}
	return nil, fmt.Errorf("%s not found", ManifestName)
}

// VerifyPart re-reads every stored entry of one archive part from disk and
// checks the CRC-32 (done by archive/zip) and the SHA-256 recorded in the
// manifest. It returns the SHA-256 of the whole archive file.
func VerifyPart(path string) (fileSHA []byte, err error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	m, err := manifestFrom(&zr.Reader)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	for _, e := range m.Entries {
		if e.Zip == "" {
			continue
		}
		f, ok := byName[e.Zip]
		if !ok {
			return nil, fmt.Errorf("%s: entry %q is missing", path, e.Zip)
		}
		if err := checkEntry(f, e.SHA256); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Path, err)
		}
	}
	return FileSHA256(path)
}

func checkEntry(f *zip.File, wantHex string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil { // io.Copy surfaces zip.ErrChecksum
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantHex {
		return fmt.Errorf("checksum mismatch: archive has %s, expected %s", got, wantHex)
	}
	return nil
}

// FileSHA256 hashes a whole file.
func FileSHA256(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
