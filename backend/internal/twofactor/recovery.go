package twofactor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// RecoveryCodeCount is how many single-use codes an enrolment receives. Ten is
// enough for a member to keep a printed copy and still recover repeatedly
// without regenerating.
const RecoveryCodeCount = 10

// recoveryAlphabet is Crockford-style: no I, L, O, U, so a code read off paper
// cannot be transcribed ambiguously.
const recoveryAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const recoveryGroupSize = 5

// GenerateRecoveryCodes returns freshly generated codes in their display form
// (`XXXXX-XXXXX`). The plaintext exists only in this return value; callers store
// HashRecoveryCode output and show the codes exactly once.
func GenerateRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, RecoveryCodeCount)
	seen := make(map[string]struct{}, RecoveryCodeCount)
	for len(codes) < RecoveryCodeCount {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[code]; duplicate {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes, nil
}

func generateRecoveryCode() (string, error) {
	raw := make([]byte, recoveryGroupSize*2)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("generate recovery code: %w", err)
	}
	characters := make([]byte, 0, len(raw)+1)
	for index, value := range raw {
		if index == recoveryGroupSize {
			characters = append(characters, '-')
		}
		// len(recoveryAlphabet) is 32, an exact divisor of 256, so indexing a
		// uniform byte stays uniform.
		characters = append(characters, recoveryAlphabet[int(value)%len(recoveryAlphabet)])
	}
	return string(characters), nil
}

// NormalizeRecoveryCode accepts the forms a member is likely to type — spaces,
// lowercase, missing hyphen — and returns the canonical display form.
func NormalizeRecoveryCode(code string) (string, error) {
	cleaned := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(code)))
	if len(cleaned) != recoveryGroupSize*2 {
		return "", ErrCodeInvalid
	}
	for _, character := range cleaned {
		if !strings.ContainsRune(recoveryAlphabet, character) {
			return "", ErrCodeInvalid
		}
	}
	return cleaned[:recoveryGroupSize] + "-" + cleaned[recoveryGroupSize:], nil
}

// HashRecoveryCode is the non-reversible form stored in the database. The codes
// carry 50 bits of entropy each, so a plain SHA-256 is not brute-forceable the
// way a password digest would be.
func HashRecoveryCode(code string) string {
	digest := sha256.Sum256([]byte("fidibeki-recovery-v1\x00" + code))
	return hex.EncodeToString(digest[:])
}
