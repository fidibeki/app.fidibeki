package integration

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

type Cipher struct {
	aead cipher.AEAD
	rand io.Reader
}

func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode integration encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("integration encryption key must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize integration cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize integration GCM: %w", err)
	}
	return &Cipher{aead: aead, rand: rand.Reader}, nil
}

func (c *Cipher) Encrypt(plaintext []byte, associatedData string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("integration cipher is unavailable")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return nil, fmt.Errorf("generate integration encryption nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, []byte(associatedData)), nil
}

func (c *Cipher) Decrypt(ciphertext []byte, associatedData string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("integration cipher is unavailable")
	}
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, errors.New("integration ciphertext is truncated")
	}
	nonce := ciphertext[:c.aead.NonceSize()]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext[c.aead.NonceSize():], []byte(associatedData))
	if err != nil {
		return nil, fmt.Errorf("decrypt integration value: %w", err)
	}
	return plaintext, nil
}

func GenerateWebhookSecret() (string, error) {
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

func SecretFingerprint(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return "sha256:" + hex.EncodeToString(digest[:8])
}

func encryptionAAD(tenantID, integrationID, field string) string {
	return tenantID + "\x00" + integrationID + "\x00" + field
}
