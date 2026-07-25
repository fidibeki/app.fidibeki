// Package twofactor implements Fidibeki-managed two-factor authentication:
// RFC 6238 time-based one-time passwords, the otpauth provisioning URI and its
// QR rendering, single-use recovery codes, and the AES-256-GCM envelope that
// keeps shared secrets encrypted at rest. Nothing in this package logs or
// formats a secret, a provisioning payload, a one-time code, or a recovery
// code, so callers cannot leak them by accident.
package twofactor

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	// Digits, PeriodSeconds, and the SHA-1 algorithm are the values every
	// mainstream authenticator app assumes when the otpauth URI omits them.
	Digits = 6
	// PeriodSeconds is the TOTP time step.
	PeriodSeconds = 30
	// SecretBytes is the shared-secret length recommended by RFC 4226 §4.
	SecretBytes = 20
	// SkewSteps accepts the immediately previous and next time step so a
	// member whose clock drifts by a few seconds can still authenticate.
	SkewSteps = 1
)

var (
	ErrSecretInvalid = errors.New("two-factor secret is invalid")
	ErrCodeInvalid   = errors.New("two-factor code is invalid")
	// ErrCodeReused rejects a code that was already accepted for its time
	// step, so an observed code cannot be replayed inside its own window.
	ErrCodeReused = errors.New("two-factor code was already used")
)

var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret returns a fresh base32-encoded shared secret.
func GenerateSecret() (string, error) {
	secret := make([]byte, SecretBytes)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return "", fmt.Errorf("generate two-factor secret: %w", err)
	}
	return secretEncoding.EncodeToString(secret), nil
}

// DecodeSecret parses a base32 secret, tolerating the spacing and lowercase
// forms authenticator apps display.
func DecodeSecret(secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "=", "").Replace(secret))
	if normalized == "" {
		return nil, ErrSecretInvalid
	}
	decoded, err := secretEncoding.DecodeString(normalized)
	if err != nil || len(decoded) < 10 {
		return nil, ErrSecretInvalid
	}
	return decoded, nil
}

// Step converts a wall-clock instant into its TOTP time step.
func Step(now time.Time) int64 {
	return now.UTC().Unix() / PeriodSeconds
}

// Code derives the one-time password for a single time step.
func Code(secret string, step int64) (string, error) {
	key, err := DecodeSecret(secret)
	if err != nil {
		return "", err
	}
	if step < 0 {
		return "", ErrCodeInvalid
	}
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(step))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", Digits, truncated%1_000_000), nil
}

// NormalizeCode strips the separators authenticator apps and password managers
// paste alongside a code and rejects anything that is not exactly six digits.
func NormalizeCode(code string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
	if len(cleaned) != Digits {
		return "", ErrCodeInvalid
	}
	for _, character := range cleaned {
		if character < '0' || character > '9' {
			return "", ErrCodeInvalid
		}
	}
	return cleaned, nil
}

// Verify checks a submitted code against the secret within the accepted skew
// and returns the matched time step. lastUsedStep, when set, rejects a code at
// or below a step that was already accepted for this enrolment.
func Verify(secret, code string, now time.Time, lastUsedStep *int64) (int64, error) {
	normalized, err := NormalizeCode(code)
	if err != nil {
		return 0, err
	}
	current := Step(now)
	matched := int64(0)
	found := false
	// Every candidate step is evaluated so verification takes the same work
	// regardless of which step matched.
	for offset := -SkewSteps; offset <= SkewSteps; offset++ {
		candidate := current + int64(offset)
		if candidate < 0 {
			continue
		}
		expected, err := Code(secret, candidate)
		if err != nil {
			return 0, err
		}
		if hmac.Equal([]byte(expected), []byte(normalized)) {
			matched = candidate
			found = true
		}
	}
	if !found {
		return 0, ErrCodeInvalid
	}
	if lastUsedStep != nil && matched <= *lastUsedStep {
		return 0, ErrCodeReused
	}
	return matched, nil
}
