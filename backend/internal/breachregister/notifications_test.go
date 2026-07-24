package breachregister

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
)

func TestNormalizeStageControllerNotificationInput(t *testing.T) {
	t.Parallel()
	nextUpdate := "  Another update will follow tomorrow. "
	normalized, err := normalizeStageControllerNotificationInput(
		StageControllerNotificationInput{
			TenantID:         " 11111111-1111-4111-8111-111111111111 ",
			NotificationKind: ControllerNotificationInitial,
			Summary:          "  A database access control failed. ",
			MeasuresTaken:    "  Access was revoked. ", NextUpdate: &nextUpdate,
		},
	)
	if err != nil {
		t.Fatalf("normalizeStageControllerNotificationInput() error = %v", err)
	}
	if normalized.TenantID != "11111111-1111-4111-8111-111111111111" ||
		normalized.Summary != "A database access control failed." ||
		normalized.MeasuresTaken != "Access was revoked." ||
		normalized.NextUpdate == nil || *normalized.NextUpdate != "Another update will follow tomorrow." {
		t.Fatalf("normalized input = %#v", normalized)
	}

	invalid := []StageControllerNotificationInput{
		{},
		{
			TenantID:         "11111111-1111-4111-8111-111111111111",
			NotificationKind: "unknown", Summary: "summary", MeasuresTaken: "measures",
		},
		{
			TenantID:         "11111111-1111-4111-8111-111111111111",
			NotificationKind: ControllerNotificationInitial, MeasuresTaken: "measures",
		},
		{
			TenantID:         "11111111-1111-4111-8111-111111111111",
			NotificationKind: ControllerNotificationInitial, Summary: "summary",
		},
		{
			TenantID:         "11111111-1111-4111-8111-111111111111",
			NotificationKind: ControllerNotificationInitial,
			Summary:          "bad\x00summary", MeasuresTaken: "measures",
		},
	}
	for index, input := range invalid {
		if _, err := normalizeStageControllerNotificationInput(input); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("invalid[%d] error = %v, want ErrInvalidRequest", index, err)
		}
	}
}

func TestControllerNotificationMessageIsMinimizedAndProviderIdempotent(t *testing.T) {
	t.Parallel()
	nextUpdate := "Within 24 hours."
	notification := ControllerNotification{
		ID: "notification-id", NotificationKind: ControllerNotificationUpdate,
		IncidentPublicID: "BR-1234ABCD", WorkspaceName: "Acme",
		RecipientEmail:            "privacy@acme.test",
		AwarenessAt:               time.Date(2026, 7, 24, 8, 30, 0, 0, time.UTC),
		AffectedEnvironments:      []string{"staging"},
		AffectedSystemCategories:  []string{"database"},
		AffectedDataCategories:    []string{"contact_info"},
		AffectedSubjectCategories: []string{"reporters_end_users"},
		ConfidentialityImpact:     true,
		Summary:                   "A bounded incident summary.",
		MeasuresTaken:             "The affected access path was disabled.",
		NextUpdate:                &nextUpdate,
	}
	message, err := controllerNotificationMessage(notification, ControllerNotificationHandler{
		From:          "Fidibeki <updates@fidibeki.com>",
		PublicBaseURL: "https://api.staging.fidibeki.com",
		BaseDomain:    "fidibeki.com",
	})
	if err != nil {
		t.Fatalf("controllerNotificationMessage() error = %v", err)
	}
	if message.To != "privacy@acme.test" ||
		message.IdempotencyKey != "breach-controller-notification:notification-id" ||
		!strings.Contains(message.Subject, "BR-1234ABCD") ||
		!strings.Contains(message.Text, "A bounded incident summary.") ||
		!strings.Contains(message.Text, "contact_info") ||
		!strings.Contains(message.Text, "does not determine") {
		t.Fatalf("message = %#v", message)
	}
	for _, forbidden := range []string{
		"reporter@example.test", "token-secret", "X-Amz-Signature", "user-agent-sentinel",
	} {
		if strings.Contains(message.Text, forbidden) || strings.Contains(message.HTML, forbidden) {
			t.Errorf("message contains excluded sentinel %q", forbidden)
		}
	}
}

func TestControllerNotificationOutboxResolver(t *testing.T) {
	t.Parallel()
	resolver := ControllerNotificationOutboxResolver{}
	destinations, err := resolver.Resolve(context.Background(), outbox.Event{
		Event: controllerNotificationEvent, ResourceType: controllerNotificationResourceType,
		ResourceID: "notification-id",
	})
	if err != nil || len(destinations) != 1 {
		t.Fatalf("Resolve() = %#v, %v", destinations, err)
	}
	if destinations[0].TaskType != TaskControllerNotification ||
		destinations[0].Queue != "email-pool" ||
		destinations[0].ID != "notification-id" {
		t.Fatalf("destination = %#v", destinations[0])
	}
	ignored, err := resolver.Resolve(context.Background(), outbox.Event{Event: "feedback.created"})
	if err != nil || len(ignored) != 0 {
		t.Fatalf("Resolve(unrelated) = %#v, %v", ignored, err)
	}
}
