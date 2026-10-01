// Package secret encrypts what GOtome keeps for others: provider keys,
// webhook tokens, OIDC client secrets. They are sealed with AES-256-GCM
// under a key that lives outside the database, so that a copy of the
// database alone gives none of them away.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeyLen is the length of a key in bytes: AES-256.
const KeyLen = 32

// version leads every sealed value, so that another scheme can follow
// without guessing what an old value is.
const version byte = 1

// ErrOpen is a sealed value that this key did not seal, or that was changed
// since, or that was sealed for another name.
var ErrOpen = errors.New("the value cannot be decrypted with this key")

// NewKey returns a random key, encoded as ParseKey reads it.
func NewKey() (string, error) {
	b := make([]byte, KeyLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ParseKey reads a key written as base64, padded or not, standard or URL
// alphabet. Surrounding whitespace, such as a file's last newline, is
// ignored.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.RawStdEncoding} {
		if key, err := enc.DecodeString(s); err == nil {
			if len(key) != KeyLen {
				return nil, fmt.Errorf("the key is %d bytes long, want %d", len(key), KeyLen)
			}
			return key, nil
		}
	}
	return nil, errors.New("the key is not base64")
}

// Box seals and opens values under one key.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for the key, which must be KeyLen bytes.
func New(key []byte) (*Box, error) {
	if len(key) != KeyLen {
		return nil, fmt.Errorf("the key is %d bytes long, want %d", len(key), KeyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts the value for the name it is kept under. The name is
// authenticated with it: a sealed value copied to another name does not
// open there.
func (b *Box) Seal(name string, value []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize(), 1+b.aead.NonceSize()+len(value)+b.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{version}, nonce...)
	return b.aead.Seal(out, nonce, value, []byte(name)), nil
}

// Open decrypts what Seal returned for the same name.
func (b *Box) Open(name string, sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < 1+n+b.aead.Overhead() || sealed[0] != version {
		return nil, ErrOpen
	}
	value, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(name))
	if err != nil {
		return nil, ErrOpen
	}
	return value, nil
}
