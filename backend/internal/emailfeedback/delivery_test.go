package emailfeedback

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
)

func TestInvitationOutboxResolverBuildsEmailDestination(t *testing.T) {
	t.Parallel()
	resolver := OutboxResolver{}
	event := outbox.Event{
		ID: "event-id", TenantID: "tenant-id",
		Event:        EventInvitationCreated,
		ResourceType: "email_feedback_invitation",
		ResourceID:   "invitation-id",
	}
	destinations, err := resolver.Resolve(context.Background(), event)
	if err != nil || len(destinations) != 1 {
		t.Fatalf("Resolve() = %#v, %v", destinations, err)
	}
	destination := destinations[0]
	if destination.Kind != "email-feedback" ||
		destination.ID != "invitation-id" ||
		destination.TaskType != TaskDeliverInvitation ||
		destination.Queue != "email-pool" {
		t.Fatalf("destination = %#v", destination)
	}
	ignored, err := resolver.Resolve(context.Background(), outbox.Event{
		Event: "feedback.created", ResourceType: "feedback",
	})
	if err != nil || len(ignored) != 0 {
		t.Fatalf("ignored event = %#v, %v", ignored, err)
	}
}

func TestComposeInvitationMessageUsesTenantURLsLocaleAndStableIdempotency(t *testing.T) {
	t.Parallel()
	recipient := "customer@example.com"
	delivery := InvitationDelivery{
		InvitationID:   "11111111-1111-4111-8111-111111111111",
		TenantSlug:     "acme",
		RecipientEmail: &recipient,
		Locale:         "pt-PT",
		ExpiresAt:      time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
		CampaignSnapshot: invitationCampaignSnapshot{
			Subject:      "Conte-nos como correu",
			Introduction: "A sua opinião ajuda-nos <muito>.",
		},
		ControllerProfile: invitationControllerSnapshot{
			Name:             "Acme Research",
			PrivacyPolicyURL: "https://acme.example/privacy",
		},
	}
	message, err := composeInvitationMessage(delivery, "efi_secret", invitationMessageOptions{
		From:             "Fidibeki <feedback@fidibeki.test>",
		PublicBaseURL:    "https://api.fidibeki.test",
		WebPublicBaseURL: "https://fidibeki.test",
		BaseDomain:       "fidibeki.test",
	})
	if err != nil {
		t.Fatalf("composeInvitationMessage() error = %v", err)
	}
	if message.To != recipient ||
		message.Subject != "Conte-nos como correu" ||
		message.IdempotencyKey != "email-feedback-invitation:"+delivery.InvitationID {
		t.Fatalf("message envelope = %#v", message)
	}
	for _, value := range []string{
		"Como foi a sua experiência?",
		"1: https://acme.fidibeki.test/email-feedback/efi_secret?rating=1",
		"5: https://acme.fidibeki.test/email-feedback/efi_secret?rating=5",
		"Partilhar feedback: https://acme.fidibeki.test/email-feedback/efi_secret",
		"Política de privacidade: https://acme.example/privacy",
		"Não receber mais estes pedidos: https://acme.fidibeki.test/email-feedback/efi_secret/opt-out",
	} {
		if !strings.Contains(message.Text, value) {
			t.Errorf("text email missing %q:\n%s", value, message.Text)
		}
	}
	if strings.Contains(message.HTML, "<muito>") ||
		!strings.Contains(message.HTML, "A sua opinião ajuda-nos &lt;muito&gt;.") {
		t.Errorf("HTML did not escape campaign copy:\n%s", message.HTML)
	}
}

func TestComposeInvitationMessageUsesEnglishFallbackCopy(t *testing.T) {
	t.Parallel()
	recipient := "customer@example.com"
	delivery := InvitationDelivery{
		InvitationID:   "11111111-1111-4111-8111-111111111111",
		TenantSlug:     "acme",
		RecipientEmail: &recipient,
		Locale:         " en-US ",
		CampaignSnapshot: invitationCampaignSnapshot{
			Subject: "Tell us how it went",
		},
		ControllerProfile: invitationControllerSnapshot{
			Name:             "Acme Research",
			PrivacyPolicyURL: "https://acme.example/privacy",
		},
	}
	message, err := composeInvitationMessage(delivery, "efi_secret", invitationMessageOptions{
		From:             "Fidibeki <feedback@fidibeki.test>",
		PublicBaseURL:    "https://api.fidibeki.test",
		WebPublicBaseURL: "https://fidibeki.test",
		BaseDomain:       "fidibeki.test",
	})
	if err != nil {
		t.Fatalf("composeInvitationMessage() error = %v", err)
	}
	for _, value := range []string{
		"How was your experience?",
		"Share your experience.",
		"Share feedback: https://acme.fidibeki.test/email-feedback/efi_secret",
		"Stop receiving these requests: https://acme.fidibeki.test/email-feedback/efi_secret/opt-out",
	} {
		if !strings.Contains(message.Text, value) {
			t.Errorf("text email missing %q:\n%s", value, message.Text)
		}
	}

	delivery.RecipientEmail = nil
	if _, err := composeInvitationMessage(delivery, "efi_secret", invitationMessageOptions{}); err == nil {
		t.Fatal("composeInvitationMessage() accepted a missing recipient")
	}
}

func TestInvitationDeliveryErrorsAreBounded(t *testing.T) {
	t.Parallel()
	if got := truncateInvitationError(""); got != "email delivery failed" {
		t.Fatalf("empty delivery error = %q", got)
	}
	message := strings.Repeat("é", 2001)
	if got := truncateInvitationError(message); len([]rune(got)) != 2000 {
		t.Fatalf("truncated delivery error contains %d runes", len([]rune(got)))
	}
	if got := truncateInvitationError("temporary failure"); got != "temporary failure" {
		t.Fatalf("short delivery error = %q", got)
	}
}

func TestInvitationTokenShapeValidation(t *testing.T) {
	t.Parallel()
	manager, err := NewInvitationTokenManager(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := manager.Token("11111111-1111-4111-8111-111111111111")
	if err != nil || !validInvitationToken(token) {
		t.Fatalf("validInvitationToken(%q) = false, error=%v", token, err)
	}
	for _, invalid := range []string{"", "efi_short", strings.Repeat("x", 47), token[:46] + "!"} {
		if validInvitationToken(invalid) {
			t.Errorf("validInvitationToken(%q) = true", invalid)
		}
	}
}
