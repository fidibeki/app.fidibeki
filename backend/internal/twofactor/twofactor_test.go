package twofactor

import (
	"encoding/base32"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCodeMatchesRFC6238Vectors(t *testing.T) {
	// RFC 6238 Appendix B publishes its SHA-1 vectors for the ASCII seed
	// "12345678901234567890"; the eight-digit values there truncate to the six
	// digits Fidibeki issues.
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString([]byte("12345678901234567890"))
	for _, vector := range []struct {
		seconds int64
		code    string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		code, err := Code(secret, Step(time.Unix(vector.seconds, 0)))
		if err != nil {
			t.Fatalf("code at %d: %v", vector.seconds, err)
		}
		if code != vector.code {
			t.Fatalf("code at %d = %s, want %s", vector.seconds, code, vector.code)
		}
	}
}

func TestVerifyAcceptsAdjacentStepsAndRejectsOthers(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	for _, offset := range []int64{-1, 0, 1} {
		code, err := Code(secret, Step(now)+offset)
		if err != nil {
			t.Fatalf("code: %v", err)
		}
		if _, err := Verify(secret, code, now, nil); err != nil {
			t.Fatalf("offset %d rejected: %v", offset, err)
		}
	}
	for _, offset := range []int64{-2, 2, 60} {
		code, err := Code(secret, Step(now)+offset)
		if err != nil {
			t.Fatalf("code: %v", err)
		}
		if _, err := Verify(secret, code, now, nil); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("offset %d error = %v, want %v", offset, err, ErrCodeInvalid)
		}
	}
}

func TestVerifyRejectsReplayWithinTheSameWindow(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := Code(secret, Step(now))
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	step, err := Verify(secret, code, now, nil)
	if err != nil {
		t.Fatalf("first verification: %v", err)
	}
	if _, err := Verify(secret, code, now, &step); !errors.Is(err, ErrCodeReused) {
		t.Fatalf("replay error = %v, want %v", err, ErrCodeReused)
	}
	// A code from the following step is still accepted after a replay attempt.
	next, err := Code(secret, step+1)
	if err != nil {
		t.Fatalf("next code: %v", err)
	}
	if _, err := Verify(secret, next, now.Add(30*time.Second), &step); err != nil {
		t.Fatalf("next step rejected: %v", err)
	}
}

func TestNormalizeCodeAcceptsDisplayFormatting(t *testing.T) {
	for _, input := range []string{"123456", " 123456 ", "123 456", "123-456"} {
		normalized, err := NormalizeCode(input)
		if err != nil {
			t.Fatalf("normalize %q: %v", input, err)
		}
		if normalized != "123456" {
			t.Fatalf("normalize %q = %q", input, normalized)
		}
	}
	for _, input := range []string{"", "12345", "1234567", "12345a", "abcdef"} {
		if _, err := NormalizeCode(input); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("normalize %q error = %v, want %v", input, err, ErrCodeInvalid)
		}
	}
}

func TestDecodeSecretRejectsWeakOrMalformedInput(t *testing.T) {
	if _, err := DecodeSecret(""); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("empty secret error = %v", err)
	}
	if _, err := DecodeSecret("not base32!"); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("malformed secret error = %v", err)
	}
	// Fewer than 80 bits of shared secret is below the RFC 4226 minimum.
	if _, err := DecodeSecret("MZXW6YQ"); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("short secret error = %v", err)
	}
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	if len(secret) != 32 {
		t.Fatalf("secret length = %d, want 32", len(secret))
	}
	decoded, err := DecodeSecret(strings.ToLower(secret[:16] + " " + secret[16:]))
	if err != nil {
		t.Fatalf("decode spaced secret: %v", err)
	}
	if len(decoded) != SecretBytes {
		t.Fatalf("decoded length = %d, want %d", len(decoded), SecretBytes)
	}
}

func TestGenerateSecretIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 32)
	for range 32 {
		secret, err := GenerateSecret()
		if err != nil {
			t.Fatalf("generate secret: %v", err)
		}
		if _, duplicate := seen[secret]; duplicate {
			t.Fatal("generated a duplicate secret")
		}
		seen[secret] = struct{}{}
	}
}

func TestProvisioningURIStatesEveryParameter(t *testing.T) {
	uri := ProvisioningURI("Fidibeki", "member@example.com", "JBSWY3DPEHPK3PXP")
	if escaped := ProvisioningURI("Acme/Team", "member@example.com", "JBSWY3DPEHPK3PXP"); !strings.HasPrefix(
		escaped, "otpauth://totp/Acme%2FTeam:member@example.com?",
	) {
		t.Fatalf("unescaped label separator in %q", escaped)
	}
	for _, expected := range []string{
		"otpauth://totp/Fidibeki:member@example.com?",
		"algorithm=SHA1",
		"digits=6",
		"issuer=Fidibeki",
		"period=30",
		"secret=JBSWY3DPEHPK3PXP",
	} {
		if !strings.Contains(uri, expected) {
			t.Fatalf("provisioning URI %q is missing %q", uri, expected)
		}
	}
}

func TestGenerateRecoveryCodesAreDistinctAndNormalizable(t *testing.T) {
	codes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("generate recovery codes: %v", err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("code count = %d, want %d", len(codes), RecoveryCodeCount)
	}
	seen := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if len(code) != 11 || code[5] != '-' {
			t.Fatalf("unexpected recovery code shape %q", code)
		}
		for _, character := range code[:5] + code[6:] {
			if !strings.ContainsRune(recoveryAlphabet, character) {
				t.Fatalf("recovery code %q uses an ambiguous character", code)
			}
		}
		if _, duplicate := seen[code]; duplicate {
			t.Fatalf("duplicate recovery code %q", code)
		}
		seen[code] = struct{}{}
		normalized, err := NormalizeRecoveryCode(strings.ToLower(strings.ReplaceAll(code, "-", " ")))
		if err != nil {
			t.Fatalf("normalize %q: %v", code, err)
		}
		if normalized != code {
			t.Fatalf("normalize %q = %q", code, normalized)
		}
	}
}

func TestNormalizeRecoveryCodeRejectsMalformedInput(t *testing.T) {
	for _, input := range []string{"", "ABCDE-ABCD", "ABCDE-ABCDEF", "ABCDI-ABCDE", "ABCDE_ABCDE"} {
		if _, err := NormalizeRecoveryCode(input); !errors.Is(err, ErrCodeInvalid) {
			t.Fatalf("normalize %q error = %v, want %v", input, err, ErrCodeInvalid)
		}
	}
}

func TestHashRecoveryCodeIsStableAndDistinct(t *testing.T) {
	first := HashRecoveryCode("ABCDE-FGHJK")
	if len(first) != 64 {
		t.Fatalf("hash length = %d, want 64", len(first))
	}
	if first != HashRecoveryCode("ABCDE-FGHJK") {
		t.Fatal("hash is not stable")
	}
	if first == HashRecoveryCode("ABCDE-FGHJM") {
		t.Fatal("distinct codes share a hash")
	}
	if strings.Contains(first, "ABCDE") {
		t.Fatal("hash leaks the plaintext code")
	}
}

const testEncryptionKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestCipherRoundTripsAndBindsToTenantAndMember(t *testing.T) {
	cipher, err := NewCipher(testEncryptionKey)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	ciphertext, err := cipher.Encrypt([]byte(secret), "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if strings.Contains(string(ciphertext), secret) {
		t.Fatal("ciphertext contains the plaintext secret")
	}
	plaintext, err := cipher.Decrypt(ciphertext, "tenant-1", "member-1")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(plaintext) != secret {
		t.Fatal("round trip changed the secret")
	}
	if _, err := cipher.Decrypt(ciphertext, "tenant-2", "member-1"); err == nil {
		t.Fatal("expected a ciphertext replayed into another tenant to fail")
	}
	if _, err := cipher.Decrypt(ciphertext, "tenant-1", "member-2"); err == nil {
		t.Fatal("expected a ciphertext replayed onto another member to fail")
	}
	if _, err := cipher.Decrypt(ciphertext[:8], "tenant-1", "member-1"); err == nil {
		t.Fatal("expected a truncated ciphertext to fail")
	}
}

func TestNewCipherRejectsUnusableKeys(t *testing.T) {
	if _, err := NewCipher("not base64"); err == nil {
		t.Fatal("expected a malformed key to be rejected")
	}
	if _, err := NewCipher("c2hvcnQ="); err == nil {
		t.Fatal("expected a short key to be rejected")
	}
	var missing *Cipher
	if _, err := missing.Encrypt([]byte("value"), "tenant", "member"); err == nil {
		t.Fatal("expected an unconfigured cipher to fail closed")
	}
	if _, err := missing.Decrypt([]byte("value"), "tenant", "member"); err == nil {
		t.Fatal("expected an unconfigured cipher to fail closed")
	}
}
