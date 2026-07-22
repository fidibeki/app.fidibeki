package widgetanalytics

import (
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
)

func TestValidateEventsCanonicalizesAndSnapshotsMatchedEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	configuration := analyticsConfiguration()
	values, fields := ValidateEvents([]EventInput{{
		EventID: "00000000-0000-4000-8000-000000000001", EventType: EventMatched,
		VisitorID:  "00000000-0000-4000-8000-000000000002",
		PageViewID: "00000000-0000-4000-8000-000000000003",
		RuleID:     "00000000-0000-4000-8000-000000000004",
		PageURL:    "https://example.com/chart/?asset=BAI#price", OccurredAt: now.Add(-time.Minute),
	}}, configuration, "https://example.com", now)
	if len(fields) > 0 || len(values) != 1 {
		t.Fatalf("ValidateEvents() = %#v, fields %#v", values, fields)
	}
	value := values[0]
	if value.PageURL != "https://example.com/chart" || value.RuleName != "Checkout" ||
		value.TenantID != configuration.TenantID || value.ProjectID != configuration.ProjectID ||
		len(value.PageKey) != 32 || value.ReceivedAt != now {
		t.Fatalf("validated event = %#v", value)
	}
}

func TestValidateEventsRejectsUnsafeOrUntrustedFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	valid := EventInput{
		EventID: "00000000-0000-4000-8000-000000000001", EventType: EventMatched,
		VisitorID:  "00000000-0000-4000-8000-000000000002",
		PageViewID: "00000000-0000-4000-8000-000000000003",
		RuleID:     "00000000-0000-4000-8000-000000000004",
		PageURL:    "https://example.com/chart", OccurredAt: now,
	}
	tests := []struct {
		name  string
		input EventInput
		field string
	}{
		{name: "client submitted", input: withEvent(valid, func(event *EventInput) { event.EventType = EventSubmitted }), field: "events[0].event_type"},
		{name: "invalid event id", input: withEvent(valid, func(event *EventInput) { event.EventID = "bad" }), field: "events[0].event_id"},
		{name: "unknown rule", input: withEvent(valid, func(event *EventInput) { event.RuleID = "00000000-0000-4000-8000-000000000099" }), field: "events[0].rule_id"},
		{name: "cross origin", input: withEvent(valid, func(event *EventInput) { event.PageURL = "https://other.test/chart" }), field: "events[0].page_url"},
		{name: "older than outbox", input: withEvent(valid, func(event *EventInput) { event.OccurredAt = now.Add(-24*time.Hour - time.Second) }), field: "events[0].occurred_at"},
		{name: "too far in future", input: withEvent(valid, func(event *EventInput) { event.OccurredAt = now.Add(5*time.Minute + time.Second) }), field: "events[0].occurred_at"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, fields := ValidateEvents([]EventInput{test.input}, analyticsConfiguration(), "https://example.com", now)
			if fields[test.field] == "" {
				t.Fatalf("fields = %#v, want %s", fields, test.field)
			}
		})
	}

	_, fields := ValidateEvents(make([]EventInput, 21), analyticsConfiguration(), "https://example.com", now)
	if !strings.Contains(fields["events"], "20") {
		t.Fatalf("event count fields = %#v", fields)
	}
}

func TestValidateSubmissionEventRequiresTheSubmittedRuleAndCanonicalPage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	input := EventInput{
		EventID:   "00000000-0000-4000-8000-000000000001",
		VisitorID: "00000000-0000-4000-8000-000000000002", PageViewID: "00000000-0000-4000-8000-000000000003",
		RuleID:  "00000000-0000-4000-8000-000000000004",
		PageURL: "https://example.com/chart/?asset=BAI#price", OccurredAt: now,
	}
	value, fields := ValidateSubmissionEvent(
		input, analyticsConfiguration(), input.RuleID,
		"https://example.com/chart?asset=BIC#details", now,
	)
	if len(fields) > 0 || value.EventType != EventSubmitted || value.PageURL != "https://example.com/chart" {
		t.Fatalf("submission event = %#v, fields %#v", value, fields)
	}

	wrongRule := input
	wrongRule.RuleID = "00000000-0000-4000-8000-000000000099"
	if _, fields := ValidateSubmissionEvent(
		wrongRule, analyticsConfiguration(), input.RuleID, "https://example.com/chart", now,
	); fields["rule_id"] == "" {
		t.Fatalf("wrong rule fields = %#v", fields)
	}
	wrongPage := input
	wrongPage.PageURL = "https://example.com/other"
	if _, fields := ValidateSubmissionEvent(
		wrongPage, analyticsConfiguration(), input.RuleID, "https://example.com/chart", now,
	); fields["page_url"] == "" {
		t.Fatalf("wrong page fields = %#v", fields)
	}
}

func analyticsConfiguration() project.PublicConfiguration {
	return project.PublicConfiguration{
		TenantID:  "00000000-0000-4000-8000-000000000010",
		ProjectID: "00000000-0000-4000-8000-000000000020",
		Rules: []project.WidgetRule{{
			ID: "00000000-0000-4000-8000-000000000004", Name: "Checkout", Enabled: true,
		}},
	}
}

func withEvent(input EventInput, mutate func(*EventInput)) EventInput {
	mutate(&input)
	return input
}
