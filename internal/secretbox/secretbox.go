// Package secretbox encrypts the secrets Keyring must read back, such as
// webhook signing secrets and TOTP seeds, so a database dump, replica or
// read-only role does not reveal them. Tokens Keyring only compares are
// hashed instead and never come here.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// prefix starts every sealed value: "enc:v1:<key id>:<base64 nonce||ciphertext>".
const prefix = "enc:v1:"

var ErrUndecryptable = errors.New("secretbox: value is not sealed with ENCRYPTION_KEY or ENCRYPTION_KEY_PREVIOUS")

// Box seals with the current key and opens with the current or previous
// one, so rotating the key is: move it to ENCRYPTION_KEY_PREVIOUS, set a
// new ENCRYPTION_KEY, restart (which reseals every value), then drop the
// previous key.
type Box struct {
	current key
	keys    map[string]key
}

type key struct {
	id   string
	aead cipher.AEAD
}

// ParseKey accepts 32 bytes as 64 hex characters or as standard or URL-safe
// base64, which is what `openssl rand -base64 32` and `openssl rand -hex 32`
// print.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, errors.New("want 32 random bytes as base64 or hex; generate one with openssl rand -base64 32")
}

// New returns a Box sealing with current. previous may be nil.
func New(current []byte, previous ...[]byte) (*Box, error) {
	b := &Box{keys: map[string]key{}}
	for i, raw := range append([][]byte{current}, previous...) {
		if raw == nil {
			continue
		}
		k, err := newKey(raw)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			b.current = k
		}
		b.keys[k.id] = k
	}
	if b.current.aead == nil {
		return nil, errors.New("secretbox: no current key")
	}
	return b, nil
}

func newKey(raw []byte) (key, error) {
	if len(raw) != 32 {
		return key{}, fmt.Errorf("secretbox: key is %d bytes, want 32", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return key{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return key{}, err
	}
	sum := sha256.Sum256(append([]byte("keyring.secretbox.id:"), raw...))
	return key{id: hex.EncodeToString(sum[:4]), aead: aead}, nil
}

// Seal encrypts plaintext. context names where the value lives, such as
// "provider_connections:clerk", and must be given again to Open, so a
// sealed value copied to another row does not open there.
func (b *Box) Seal(plaintext, context string) string {
	nonce := make([]byte, b.current.aead.NonceSize())
	_, _ = rand.Read(nonce)
	out := b.current.aead.Seal(nonce, nonce, []byte(plaintext), []byte(context))
	return prefix + b.current.id + ":" + base64.RawStdEncoding.EncodeToString(out)
}

// Open decrypts a value Seal returned for the same context.
func (b *Box) Open(sealed, context string) (string, error) {
	rest, ok := strings.CutPrefix(sealed, prefix)
	if !ok {
		return "", ErrUndecryptable
	}
	id, data, ok := strings.Cut(rest, ":")
	k, known := b.keys[id]
	if !ok || !known {
		return "", ErrUndecryptable
	}
	raw, err := base64.RawStdEncoding.DecodeString(data)
	n := k.aead.NonceSize()
	if err != nil || len(raw) < n {
		return "", ErrUndecryptable
	}
	plain, err := k.aead.Open(nil, raw[:n], raw[n:], []byte(context))
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plain), nil
}

// Current reports whether sealed was sealed with the current key, so a
// restart after a rotation knows which values to reseal.
func (b *Box) Current(sealed string) bool {
	return strings.HasPrefix(sealed, prefix+b.current.id+":")
}

// Reseal returns value sealed with the current key: plaintext from before
// encryption existed, or a value sealed with the previous key. It returns
// value unchanged when it already uses the current key.
func (b *Box) Reseal(value, context string) (string, bool, error) {
	if b.Current(value) {
		return value, false, nil
	}
	plain := value
	if strings.HasPrefix(value, prefix) {
		var err error
		if plain, err = b.Open(value, context); err != nil {
			return "", false, err
		}
	}
	return b.Seal(plain, context), true, nil
}

// ForTest returns a Box with a fixed key, for tests in other packages.
func ForTest() *Box {
	b, err := New([]byte("keyring test key, 32 bytes long!"))
	if err != nil {
		panic(err)
	}
	return b
}
