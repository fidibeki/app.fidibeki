package testimonial

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

const publicIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// publicIDLength is 16 rather than the 8 used for internal identifiers such as
// fb_. These identifiers appear in published snapshots and in embed snippets on
// customer sites, so they are enumerable across every tenant on the platform.
// Eight characters is roughly 47 bits, which is thin protection against someone
// harvesting testimonials wholesale; 16 is roughly 95 bits.
const publicIDLength = 16

// Public identifier prefixes, matching the CHECK constraints in migrations
// 000117 to 000120.
const (
	PrefixTestimonial = "ts_"
	PrefixWall        = "wl_"
	PrefixForm        = "tf_"
	PrefixMedia       = "tm_"
)

// GeneratePublicID returns a prefixed, unguessable public identifier.
func GeneratePublicID(prefix string) (string, error) {
	value := make([]byte, publicIDLength)
	ceiling := big.NewInt(int64(len(publicIDAlphabet)))
	for index := range value {
		random, err := rand.Int(rand.Reader, ceiling)
		if err != nil {
			return "", fmt.Errorf("generate testimonial public ID: %w", err)
		}
		value[index] = publicIDAlphabet[random.Int64()]
	}
	return prefix + string(value), nil
}

// GenerateToken returns a URL-safe token and the SHA-256 hex digest to store.
//
// The plaintext is returned once, for the email that carries it. Only the digest
// is persisted, matching reporter_access_tokens and tenant_magic_links, so a
// database disclosure does not hand over working collection or revocation links.
func GenerateToken() (token string, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate testimonial token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken derives the stored digest for a token presented by a caller.
func HashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
