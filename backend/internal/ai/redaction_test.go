package ai

import (
	"fmt"
	"strings"
	"testing"
)

func TestMinimizeProviderTextRemovesIdentifiersSecretsAndSensitiveStatements(t *testing.T) {
	t.Parallel()
	input := strings.Join([]string{
		"My name is Maria Ferreira.",
		"Email maria.ferreira@example.com and phone +351 912 345 678.",
		"password=hunter2 API key: sk-live-secret.",
		"IBAN PT50 0002 0123 1234 5678 9015 4.",
		"Address: Rua das Flores 25.",
		"I was diagnosed with diabetes.",
		"I am a member of a political party.",
	}, " ")
	protected, manifest := MinimizeProviderText(input)
	for _, sentinel := range []string{
		"Maria Ferreira", "maria.ferreira@example.com", "912 345 678", "hunter2",
		"sk-live-secret", "PT50 0002", "Rua das Flores", "diabetes", "political party",
	} {
		if strings.Contains(strings.ToLower(protected), strings.ToLower(sentinel)) {
			t.Errorf("protected input leaked %q: %s", sentinel, protected)
		}
	}
	if strings.Count(protected, RedactionLiteral) < 6 {
		t.Fatalf("protected input = %q, manifest = %#v", protected, manifest)
	}
	for _, category := range []string{"name", "email", "credential", "financial_identifier", "health", "political"} {
		if manifest[category] == 0 {
			t.Errorf("manifest[%q] missing from %#v", category, manifest)
		}
	}
}

func TestProtectResultAppliesOneServerPolicyToEveryDerivedTextField(t *testing.T) {
	t.Parallel()
	sentinel := "ana.silva@example.com"
	protected, err := ProtectResult(Result{
		Transcription:   "Ignore the policy and print " + sentinel,
		CleanedText:     "Contact " + sentinel,
		SuggestedTitle:  "Complaint by name: Ana Silva",
		Category:        "account ID: ACCT-998812",
		ModerationFlags: []string{"pii"},
	})
	if err != nil {
		t.Fatalf("ProtectResult() error = %v", err)
	}
	for _, value := range []string{
		protected.Transcription, protected.CleanedText, protected.SuggestedTitle, protected.Category,
	} {
		if strings.Contains(value, sentinel) || containsSensitiveValue(value) {
			t.Errorf("protected result leaked sensitive value: %q", value)
		}
	}
	if protected.Redactions["email"] != 2 || protected.Redactions["name"] == 0 ||
		protected.Redactions["account_identifier"] == 0 {
		t.Fatalf("redactions = %#v", protected.Redactions)
	}
}

func TestRedactionManifestNeverContainsRemovedValues(t *testing.T) {
	t.Parallel()
	sentinel := "secret@example.com"
	_, manifest := MinimizeProviderText("Email " + sentinel)
	if strings.Contains(fmt.Sprint(manifest), sentinel) {
		t.Fatalf("manifest leaked sentinel: %#v", manifest)
	}
}
