package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidMasterKey = errors.New("invalid secret master key")

type SecretBox struct {
	aead cipher.AEAD
}

func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: expected 32 bytes, got %d", ErrInvalidMasterKey, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

func NewSecretBoxBase64(encoded string) (*SecretBox, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode secret master key: %w", err)
	}
	return NewSecretBox(key)
}

func (b *SecretBox) Seal(plaintext string) (string, error) {
	if b == nil || b.aead == nil {
		return "", ErrInvalidMasterKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (b *SecretBox) Open(ciphertext string) (string, error) {
	if b == nil || b.aead == nil {
		return "", ErrInvalidMasterKey
	}
	data, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode secret ciphertext: %w", err)
	}
	nonceSize := b.aead.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("secret ciphertext is too short")
	}
	plaintext, err := b.aead.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintext), nil
}
