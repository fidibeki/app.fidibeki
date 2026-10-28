package email

import (
	"context"
	"strings"
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/outbox"
)

func TestLifecycleResolverRoutesOnlyTenantEmailEvents(t *testing.T) {
	t.Parallel()
	for _, eventName := range []string{EventOwnerActivated, EventDeletionCancelled, EventBillingStateChanged} {
		destinations, err := (LifecycleResolver{}).Resolve(context.Background(), outbox.Event{
			ID: "event", Event: eventName, ResourceType: "tenant", ResourceID: "tenant",
		})
		if err != nil || len(destinations) != 1 || destinations[0].TaskType != TaskLifecycleEmail ||
			destinations[0].Queue != QueueName {
			t.Fatalf("Resolve(%q) = %#v, %v", eventName, destinations, err)
		}
	}
	destinations, err := (LifecycleResolver{}).Resolve(context.Background(), outbox.Event{
		Event: EventOwnerActivated, ResourceType: "feedback",
	})
	if err != nil || len(destinations) != 0 {
		t.Fatalf("Resolve(wrong resource) = %#v, %v", destinations, err)
	}
}

func TestLifecycleMessagesCoverFounderAndBillingTransitions(t *testing.T) {
	t.Parallel()
	handler := LifecycleHandler{
		PublicBaseURL: "https://api.fidibeki.com", BaseDomain: "fidibeki.com",
		AuthFrom:       "Fidibeki <auth@fidibeki.com>",
		WelcomeFrom:    "Fidibeki <welcome@example.test>",
		WelcomeReplyTo: "welcome@example.test",
	}
	welcome, err := handler.message(lifecycleEvent{
		ID: "welcome-event", TenantID: "tenant", TenantName: "Acme", TenantSlug: "acme",
		Event: EventOwnerActivated,
	}, lifecycleRecipient{Email: "owner@acme.test", Name: "Sam"})
	if err != nil {
		t.Fatalf("welcome message error = %v", err)
	}
	if welcome.Subject != "Welcome to Fidibeki" ||
		welcome.From != "Fidibeki <welcome@example.test>" ||
		welcome.ReplyTo != "welcome@example.test" || strings.Contains(welcome.Text, "token=") ||
		!strings.Contains(welcome.Text, "I'm Acidiney") ||
		!strings.Contains(welcome.Text, "https://acme.fidibeki.com/getting-started") ||
		len(welcome.IdempotencyKey) > 256 || strings.Contains(welcome.IdempotencyKey, "owner@acme.test") {
		t.Fatalf("welcome message = %#v", welcome)
	}

	failed, err := handler.message(lifecycleEvent{
		ID: "billing-event", TenantName: "Acme", TenantSlug: "acme",
		Event: EventBillingStateChanged,
		Data:  lifecycleData{PreviousStatus: "active", Status: "suspended", PreviousPlan: "growth", Plan: "growth"},
	}, lifecycleRecipient{Email: "billing@acme.test"})
	if err != nil || failed.Subject != "Action required: Fidibeki payment failed" ||
		!strings.Contains(failed.Text, "active -> suspended") ||
		!strings.Contains(failed.HTML, "Open billing settings") {
		t.Fatalf("payment failure message = %#v, %v", failed, err)
	}
	for _, test := range []struct {
		name    string
		data    lifecycleData
		subject string
		heading string
	}{
		{
			name: "payment recovered", subject: "Fidibeki payment recovered", heading: "Payment recovered",
			data: lifecycleData{PreviousStatus: "suspended", Status: "active", PreviousPlan: "growth", Plan: "growth"},
		},
		{
			name: "subscription activated", subject: "Fidibeki subscription activated", heading: "Subscription activated",
			data: lifecycleData{PreviousStatus: "trial", Status: "active", PreviousPlan: "growth", Plan: "growth", SubscriptionID: "sub_1"},
		},
		{
			name: "subscription cancelled", subject: "Fidibeki subscription cancelled", heading: "Subscription cancelled",
			data: lifecycleData{PreviousStatus: "active", Status: "cancelled", PreviousPlan: "growth", Plan: "growth", PreviousSubID: "sub_1", SubscriptionID: "sub_1"},
		},
		{
			name: "plan changed", subject: "Fidibeki subscription updated", heading: "Subscription updated",
			data: lifecycleData{PreviousStatus: "active", Status: "active", PreviousPlan: "growth", Plan: "scale", PreviousSubID: "sub_1", SubscriptionID: "sub_1"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			message, err := handler.message(lifecycleEvent{
				ID: "billing-event", TenantName: "Acme", TenantSlug: "acme",
				Event: EventBillingStateChanged, Data: test.data,
			}, lifecycleRecipient{Email: "billing@acme.test"})
			if err != nil || message.Subject != test.subject || !strings.Contains(message.Text, test.heading) {
				t.Fatalf("message = %#v, %v", message, err)
			}
		})
	}

	cancelled, err := handler.message(lifecycleEvent{
		ID: "deletion-event", TenantName: "Acme", TenantSlug: "acme",
		ActorName: "Sam Owner", Event: EventDeletionCancelled,
		Data: lifecycleData{DeletionRequested: "2026-07-22T12:00:00Z"},
	}, lifecycleRecipient{Email: "owner@acme.test"})
	if err != nil || !strings.Contains(cancelled.Text, "Cancelled by: Sam Owner") ||
		!strings.Contains(cancelled.Text, "Original request: 2026-07-22T12:00:00Z") {
		t.Fatalf("deletion cancellation message = %#v, %v", cancelled, err)
	}
}
