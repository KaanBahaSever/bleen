package archive

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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

// Opener decrypts sealed archives (implemented by *seal.Key).
type Opener interface {
	DecryptFile(src, dst string) error
}

// IsSealed reports whether an archive file is age-encrypted.
func IsSealed(path string) bool { return strings.HasSuffix(path, SealedExt) }

// Plain returns a path to the plain ZIP for path. Sealed archives are
// decrypted to a temporary file on the local disk; call cleanup when done.
func Plain(path string, key Opener, tmpDir string) (plain string, cleanup func(), err error) {
	if !IsSealed(path) {
		return path, func() {}, nil
	}
	if key == nil {
		return "", nil, errors.New("E_PASSWORD_REQUIRED")
	}
	f, err := os.CreateTemp(tmpDir, TempPattern("open", ".zip"))
	if err != nil {
		return "", nil, err
	}
	f.Close()
	if err := key.DecryptFile(path, f.Name()); err != nil {
		os.Remove(f.Name())
		return "", nil, err
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// ReadManifestAny reads the manifest of a plain or sealed archive.
func ReadManifestAny(path string, key Opener) (*Manifest, error) {
	p, cleanup, err := Plain(path, key, "")
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return ReadManifest(p)
}

// TempPattern names bleen's temporary files "bleen-<kind>-<pid>-*<ext>", so
// that files left by a bleen that was killed can be recognised and removed.
func TempPattern(kind, ext string) string {
	return fmt.Sprintf("bleen-%s-%d-*%s", kind, os.Getpid(), ext)
}
