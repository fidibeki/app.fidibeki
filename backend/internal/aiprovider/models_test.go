package aiprovider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestCredentialEnvelopeBindsWorkspaceConnectionAndVersion(t *testing.T) {
	keys := map[string]string{"old": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), "new": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))}
	ring, err := NewKeyring("old", keys)
	if err != nil {
		t.Fatal(err)
	}
	aad := CredentialAAD("workspace-a", "connection-a", 1)
	encrypted, err := ring.Encrypt("secret-api-key", aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("secret-api-key")) {
		t.Fatal("plaintext credential in envelope")
	}
	rotated, err := NewKeyring("new", keys)
	if err != nil {
		t.Fatal(err)
	}
	value, err := rotated.Decrypt(encrypted, aad)
	if err != nil || value != "secret-api-key" {
		t.Fatalf("old key during rotation: %q %v", value, err)
	}
	for _, wrong := range []string{CredentialAAD("workspace-b", "connection-a", 1), CredentialAAD("workspace-a", "connection-b", 1), CredentialAAD("workspace-a", "connection-a", 2)} {
		if _, err = ring.Decrypt(encrypted, wrong); err == nil {
			t.Fatal("cross-boundary credential decryption accepted")
		}
	}
	var envelopeData envelope
	if err = json.Unmarshal(encrypted, &envelopeData); err != nil {
		t.Fatal(err)
	}
	envelopeData.Data[len(envelopeData.Data)-1] ^= 1
	tampered, _ := json.Marshal(envelopeData)
	if _, err = ring.Decrypt(tampered, aad); err == nil {
		t.Fatal("tampered credential accepted")
	}
}

func TestConnectionInputRejectsUnsafeURLForms(t *testing.T) {
	for _, endpoint := range []string{"http://api.example.com/v1", "https://user:secret@api.example.com/v1", "https://api.example.com/v1?key=secret", "https://api.example.com/v1#secret", "https://api.example.com:8443/v1", "https:///v1"} {
		_, err := ValidateInput(Input{Name: "Test", Provider: CustomOpenAI, BaseURL: endpoint})
		if err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	v, err := ValidateInput(Input{Name: "Test", Provider: CustomOpenAI, BaseURL: "https://api.example.com/v1/"})
	if err != nil || v.BaseURL != "https://api.example.com/v1" || v.RPM != 15 {
		t.Fatalf("valid input: %#v %v", v, err)
	}
	// Official endpoints cannot be replaced by a customer-supplied host.
	v, err = ValidateInput(Input{Name: "Test", Provider: OpenAI, BaseURL: "https://attacker.example"})
	if err != nil || v.BaseURL != "" {
		t.Fatal("official endpoint override survived")
	}
}

func TestPublicConnectionCannotSerializeCredentials(t *testing.T) {
	data, err := json.Marshal(Connection{ID: "id", TenantID: "private-tenant", Name: "Production", CredentialVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("private-tenant")) || bytes.Contains(data, []byte("envelope")) {
		t.Fatalf("private connection fields exposed: %s", data)
	}
}

func TestExpiredBedrockKeyCanBeDisabledButNotEnabled(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	input := Input{Name: "Pilot", Provider: Bedrock, Region: "eu-west-1", AuthMode: "api_key", CredentialExpiresAt: &expired, Enabled: true}
	if _, err := ValidateInput(input); err == nil {
		t.Fatal("expired key enabled")
	}
	input.Enabled = false
	if _, err := ValidateInput(input); err != nil {
		t.Fatal("expired connection cannot be disabled", err)
	}
}
