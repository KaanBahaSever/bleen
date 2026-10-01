// Package seal encrypts vault files with age (https://age-encryption.org).
//
// Each encrypted vault has an X25519 key pair. The public recipient is
// stored in vault.json; the private identity is itself encrypted with the
// user's password (age scrypt) in .bleen/identity.age. Unlocking costs one
// scrypt derivation; after that every archive is fast to seal and open.
// Recovery without bleen: `age -d identity.age > key.txt` (asks for the
// password), then `age -d -i key.txt archive.zip.age > archive.zip`.
package seal

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

var (
	ErrWrongPassword = errors.New("E_WRONG_PASSWORD")
	ErrWeakPassword  = errors.New("E_WEAK_PASSWORD")
	ErrKeyDamaged    = errors.New("E_KEY_DAMAGED")
)

// MinPasswordLen is the shortest password bleen accepts.
const MinPasswordLen = 8

// Key is an unlocked vault key.
type Key struct {
	id        *age.X25519Identity
	recipient *age.X25519Recipient
}

// NewKey creates a key pair and returns it with the password-protected
// identity file content.
func NewKey(password string) (*Key, []byte, error) {
	if len([]rune(password)) < MinPasswordLen {
		return nil, nil, ErrWeakPassword
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, nil, err
	}
	r, err := age.NewScryptRecipient(password)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(w, "# bleen vault key. Keep your password safe; without it this key cannot be read.\n%s\n", id.String())
	if err := w.Close(); err != nil {
		return nil, nil, err
	}
	return &Key{id: id, recipient: id.Recipient()}, buf.Bytes(), nil
}

// Unlock decrypts an identity file with the password.
func Unlock(identityFile []byte, password string) (*Key, error) {
	sid, err := age.NewScryptIdentity(password)
	if err != nil {
		return nil, err
	}
	r, err := age.Decrypt(bytes.NewReader(identityFile), sid)
	if err != nil {
		var nm *age.NoIdentityMatchError
		if errors.As(err, &nm) || strings.Contains(err.Error(), "incorrect passphrase") {
			return nil, ErrWrongPassword
		}
		return nil, fmt.Errorf("%w: %v", ErrKeyDamaged, err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	ids, err := age.ParseIdentities(bytes.NewReader(b))
	if err != nil || len(ids) != 1 {
		return nil, fmt.Errorf("vault key is damaged: %v", err)
	}
	id, ok := ids[0].(*age.X25519Identity)
	if !ok {
		return nil, errors.New("vault key has an unexpected type")
	}
	return &Key{id: id, recipient: id.Recipient()}, nil
}

// Recipient is the public key string stored in vault.json.
func (k *Key) Recipient() string { return k.recipient.String() }

// Wrap returns a writer that encrypts into w. Close finishes the stream.
func (k *Key) Wrap(w io.Writer) (io.WriteCloser, error) { return age.Encrypt(w, k.recipient) }

// Open returns a reader that decrypts r.
func (k *Key) Open(r io.Reader) (io.Reader, error) { return age.Decrypt(r, k.id) }

// EncryptFile seals src into dst.
func (k *Key) EncryptFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	w, err := k.Wrap(out)
	if err == nil {
		_, err = io.Copy(w, in)
		if err == nil {
			err = w.Close()
		}
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

// DecryptFile opens src into a new plain file dst.
func (k *Key) DecryptFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	r, err := k.Open(in)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

// PlainSHA256 decrypts src as a stream and hashes the plaintext.
func (k *Key) PlainSHA256(src string) ([]byte, error) {
	in, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	r, err := k.Open(in)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
