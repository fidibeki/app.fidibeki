package testimonial

import (
	"regexp"
	"testing"
)

// The formats are enforced by CHECK constraints in migrations 000117 to 000120,
// so a generator that drifts from them fails at insert time rather than here.
// Pinning them means it fails here instead.
func TestGeneratePublicIDMatchesTheSchemaConstraints(t *testing.T) {
	t.Parallel()
	patterns := map[string]*regexp.Regexp{
		PrefixTestimonial: regexp.MustCompile(`^ts_[A-Za-z0-9]{16}$`),
		PrefixWall:        regexp.MustCompile(`^wl_[A-Za-z0-9]{16}$`),
		PrefixForm:        regexp.MustCompile(`^tf_[A-Za-z0-9]{16}$`),
		PrefixMedia:       regexp.MustCompile(`^tm_[A-Za-z0-9]{16}$`),
	}
	for prefix, pattern := range patterns {
		id, err := GeneratePublicID(prefix)
		if err != nil {
			t.Fatalf("GeneratePublicID(%q): %v", prefix, err)
		}
		if !pattern.MatchString(id) {
			t.Errorf("GeneratePublicID(%q) = %q, does not match %s", prefix, id, pattern)
		}
	}
}

func TestGeneratePublicIDDoesNotRepeat(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, 500)
	for range 500 {
		id, err := GeneratePublicID(PrefixWall)
		if err != nil {
			t.Fatalf("GeneratePublicID: %v", err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("GeneratePublicID returned a duplicate: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerateTokenReturnsAPlaintextAndItsDigest(t *testing.T) {
	t.Parallel()
	token, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken returned an empty token")
	}
	if len(hash) != 64 {
		t.Fatalf("hash length = %d, want 64 to satisfy the schema CHECK", len(hash))
	}
	if HashToken(token) != hash {
		t.Fatal("HashToken did not reproduce the digest returned by GenerateToken")
	}
	// The plaintext must never be derivable from what we store.
	if token == hash {
		t.Fatal("GenerateToken returned the digest as the token")
	}
}

func TestGenerateTokenDoesNotRepeat(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{}, 200)
	for range 200 {
		_, hash, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		if _, exists := seen[hash]; exists {
			t.Fatalf("GenerateToken returned a duplicate digest: %q", hash)
		}
		seen[hash] = struct{}{}
	}
}
