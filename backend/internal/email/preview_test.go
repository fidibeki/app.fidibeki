package email

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteEmailPreviews(t *testing.T) {
	directory := os.Getenv("EMAIL_PREVIEW_DIR")
	if directory == "" {
		t.Skip("set EMAIL_PREVIEW_DIR to render browser preview fixtures")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create email preview directory: %v", err)
	}
	handler := LifecycleHandler{
		PublicBaseURL: "https://api.staging.example.test", BaseDomain: "staging.example.test",
		AuthFrom:       "Fidibeki <auth@fidibeki.com>",
		WelcomeFrom:    "Fidibeki <welcome@example.test>",
		WelcomeReplyTo: "welcome@example.test",
	}
	previews := map[string]struct {
		event     lifecycleEvent
		recipient lifecycleRecipient
	}{
		"welcome.html": {
			event: lifecycleEvent{
				ID: "welcome-preview", TenantName: "Acme Research", TenantSlug: "acme-research",
				Event: EventOwnerActivated,
			},
			recipient: lifecycleRecipient{Email: "owner@acme.test", Name: "Sofia"},
		},
		"payment-failed.html": {
			event: lifecycleEvent{
				ID: "billing-preview", TenantName: "Acme Research", TenantSlug: "acme-research",
				Event: EventBillingStateChanged,
				Data: lifecycleData{
					PreviousStatus: "active", Status: "suspended",
					PreviousPlan: "growth", Plan: "growth", PreviousSubID: "sub_1", SubscriptionID: "sub_1",
				},
			},
			recipient: lifecycleRecipient{Email: "billing@acme.test"},
		},
	}
	for name, preview := range previews {
		message, err := handler.message(preview.event, preview.recipient)
		if err != nil {
			t.Fatalf("compose %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), []byte(message.HTML), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}
