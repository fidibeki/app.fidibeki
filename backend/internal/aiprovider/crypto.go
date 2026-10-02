package aiprovider

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Keyring struct {
	Active string
	keys   map[string]cipher.AEAD
}
type envelope struct {
	Format int    `json:"v"`
	Key    string `json:"key"`
	Data   []byte `json:"data"`
}

func NewKeyring(active string, keys map[string]string) (*Keyring, error) {
	ring := &Keyring{Active: active, keys: make(map[string]cipher.AEAD)}
	for id, value := range keys {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(raw) != 32 {
			return nil, errors.New("AI encryption keys must be base64-encoded 32-byte keys")
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		ring.keys[id] = aead
	}
	if ring.keys[active] == nil {
		return nil, errors.New("active AI encryption key is not configured")
	}
	return ring, nil
}
func (k *Keyring) Encrypt(value, aad string) ([]byte, error) {
	if k == nil {
		return nil, errors.New("AI credential encryption is unavailable")
	}
	a := k.keys[k.Active]
	nonce := make([]byte, a.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return json.Marshal(envelope{1, k.Active, a.Seal(nonce, nonce, []byte(value), []byte(aad))})
}
func (k *Keyring) Decrypt(data []byte, aad string) (string, error) {
	if k == nil {
		return "", errors.New("AI credential decryption is unavailable")
	}
	var e envelope
	if err := json.Unmarshal(data, &e); err != nil || e.Format != 1 {
		return "", errors.New("invalid AI credential envelope")
	}
	a := k.keys[e.Key]
	if a == nil || len(e.Data) < a.NonceSize() {
		return "", errors.New("AI credential key is unavailable")
	}
	value, err := a.Open(nil, e.Data[:a.NonceSize()], e.Data[a.NonceSize():], []byte(aad))
	if err != nil {
		return "", errors.New("AI credential authentication failed")
	}
	return string(value), nil
}
func CredentialAAD(tenant, id string, version int64) string {
	return fmt.Sprintf("ai:%s:%s:%d", tenant, id, version)
}
