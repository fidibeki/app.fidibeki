package twofactor

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Cipher wraps AES-256-GCM for two-factor shared secrets. The associated data
// binds every ciphertext to one tenant and member, so a row copied between
// members or workspaces fails to decrypt instead of authenticating someone else.
type Cipher struct {
	aead cipher.AEAD
	rand io.Reader
}

func NewCipher(encodedKey string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf("decode two-factor encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("two-factor encryption key must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize two-factor cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize two-factor GCM: %w", err)
	}
	return &Cipher{aead: aead, rand: rand.Reader}, nil
}

func (c *Cipher) Encrypt(plaintext []byte, tenantID, memberID string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("two-factor cipher is unavailable")
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return nil, fmt.Errorf("generate two-factor encryption nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, secretAAD(tenantID, memberID)), nil
}

func (c *Cipher) Decrypt(ciphertext []byte, tenantID, memberID string) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, errors.New("two-factor cipher is unavailable")
	}
	if len(ciphertext) < c.aead.NonceSize() {
		return nil, errors.New("two-factor ciphertext is truncated")
	}
	nonce := ciphertext[:c.aead.NonceSize()]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext[c.aead.NonceSize():], secretAAD(tenantID, memberID))
	if err != nil {
		// The underlying error is deliberately not wrapped: it carries no
		// useful detail and every failure means the same thing to a caller.
		return nil, errors.New("decrypt two-factor secret")
	}
	return plaintext, nil
}

func secretAAD(tenantID, memberID string) []byte {
	return []byte("fidibeki-totp-v1\x00" + tenantID + "\x00" + memberID)
}
