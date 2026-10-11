// Package secretbox encrypts small secrets, such as OAuth tokens, for storage
// at rest with AES-256-GCM.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Prefix marks a sealed value; values without it are legacy plaintext.
const Prefix = "enc:v1:"

// KeySize is the required key length in bytes (AES-256).
const KeySize = 32

// ErrNoKey is returned when a sealed value must be opened without a key.
var ErrNoKey = errors.New("value is encrypted but no TOKEN_ENCRYPTION_KEY is configured")

// Box seals and opens values with one key.
type Box struct {
	aead cipher.AEAD
}

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", KeySize, len(key))
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

// ParseKey decodes a key given as 64 hex characters or as base64 (standard or
// URL alphabet, padded or not) of 32 bytes.
func ParseKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == hex.EncodedLen(KeySize) {
		if key, err := hex.DecodeString(raw); err == nil {
			return key, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(raw); err == nil && len(key) == KeySize {
			return key, nil
		}
	}
	return nil, fmt.Errorf("encryption key must be %d bytes, encoded as 64 hex characters or base64 (e.g. `openssl rand -base64 32`)", KeySize)
}

// IsSealed reports whether value was produced by Seal.
func IsSealed(value string) bool {
	return strings.HasPrefix(value, Prefix)
}

// Seal encrypts plaintext with a random nonce.
func (b *Box) Seal(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return Prefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a sealed value. Legacy plaintext (no Prefix) is returned as is,
// so existing rows keep working until they are re-encrypted. A nil Box can only
// open plaintext.
func (b *Box) Open(value string) (string, error) {
	if !IsSealed(value) {
		return value, nil
	}
	if b == nil {
		return "", ErrNoKey
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("decoding encrypted value: %w", err)
	}
	size := b.aead.NonceSize()
	if len(raw) < size {
		return "", errors.New("encrypted value is too short")
	}
	plaintext, err := b.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", errors.New("decrypting value failed: wrong TOKEN_ENCRYPTION_KEY or corrupted data")
	}
	return string(plaintext), nil
}

// SealPtr seals a nullable value; a nil Box stores it unchanged.
func (b *Box) SealPtr(value *string) (*string, error) {
	if b == nil || value == nil || *value == "" || IsSealed(*value) {
		return value, nil
	}
	sealed, err := b.Seal(*value)
	if err != nil {
		return nil, err
	}
	return &sealed, nil
}

// OpenPtr opens a nullable value.
func (b *Box) OpenPtr(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	opened, err := b.Open(*value)
	if err != nil {
		return nil, err
	}
	return &opened, nil
}
