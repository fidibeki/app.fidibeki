package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const opaqueTokenBytes = 32

func NewOpaqueToken() (string, string, error) {
	bytes := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate opaque token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(bytes)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}
