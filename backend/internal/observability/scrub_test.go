package observability_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/observability"
	"github.com/getsentry/sentry-go"
)

func TestScrubEventRemovesRequestUserBreadcrumbsContextsAndTags(t *testing.T) {
	t.Parallel()

	event := &sentry.Event{
		Request: &sentry.Request{
			URL:         "https://app.fidibeki.test/tenants/acme/reports?token=secret#fragment",
			Method:      "POST",
			Data:        `{"reporter_email":"person@example.com"}`,
			QueryString: "token=secret",
			Cookies:     "session=secret",
			Headers:     map[string]string{"Authorization": "Bearer secret", "User-Agent": "Mozilla/5.0"},
		},
		User: sentry.User{
			ID: "user-1", Email: "person@example.com", IPAddress: "203.0.113.9", Username: "person",
		},
		Breadcrumbs: []*sentry.Breadcrumb{{
			Category: "http", Message: "GET /tenants/acme/reports?token=secret",
			Data: map[string]any{"body": "reporter feedback text"},
		}},
		Contexts:   map[string]sentry.Context{"custom": {"customer_field": "sensitive"}},
		Tags:       map[string]string{"customer_id": "acme"},
		ServerName: "customer-acme-host",
		Modules:    map[string]string{"customer-module": "person@example.com"},
	}

	scrubbed := observability.ScrubEvent(event, nil)

	if scrubbed.Request != nil {
		t.Errorf("Request = %#v, want nil", scrubbed.Request)
	}
	if scrubbed.User.ID != "" || scrubbed.User.Email != "" || scrubbed.User.IPAddress != "" ||
		scrubbed.User.Username != "" || scrubbed.User.Name != "" || scrubbed.User.Data != nil {
		t.Errorf("User = %#v, want zero value", scrubbed.User)
	}
	if scrubbed.Breadcrumbs != nil {
		t.Errorf("Breadcrumbs = %#v, want nil", scrubbed.Breadcrumbs)
	}
	if scrubbed.Contexts != nil {
		t.Errorf("Contexts = %#v, want nil", scrubbed.Contexts)
	}
	if scrubbed.Tags != nil {
		t.Errorf("Tags = %#v, want nil", scrubbed.Tags)
	}
	if scrubbed.ServerName != "" || scrubbed.Modules != nil {
		t.Errorf("server/module metadata survived: ServerName=%q Modules=%#v",
			scrubbed.ServerName, scrubbed.Modules)
	}

	serialized, err := json.Marshal(scrubbed)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, leak := range []string{"person@example.com", "secret", "203.0.113.9", "reporter feedback", "acme"} {
		if strings.Contains(string(serialized), leak) {
			t.Errorf("scrubbed event serialization contains %q: %s", leak, serialized)
		}
	}
}

func TestScrubEventKeepsOnlyValidatedReplayTelemetryTags(t *testing.T) {
	t.Parallel()

	event := &sentry.Event{
		Tags: map[string]string{
			"subsystem":          "replay",
			"operation":          "verify_batch",
			"failure_code":       "contract_violation",
			"violation_code":     "media_fields_not_supported",
			"rrweb_version":      "2.1.1",
			"event_type":         "3",
			"incremental_source": "7",
			"retry_exhausted":    "false",
			"customer_id":        "acme-sensitive",
		},
		Fingerprint: []string{"attacker-controlled", "person@example.com"},
	}

	scrubbed := observability.ScrubEvent(event, nil)
	want := map[string]string{
		"subsystem":          "replay",
		"operation":          "verify_batch",
		"failure_code":       "contract_violation",
		"violation_code":     "media_fields_not_supported",
		"rrweb_version":      "2.1.1",
		"event_type":         "3",
		"incremental_source": "7",
		"retry_exhausted":    "false",
	}
	if !reflect.DeepEqual(scrubbed.Tags, want) {
		t.Fatalf("Tags = %#v, want %#v", scrubbed.Tags, want)
	}
	if !reflect.DeepEqual(scrubbed.Fingerprint, []string{
		"replay", "verify_batch", "contract_violation", "media_fields_not_supported",
	}) {
		t.Fatalf("Fingerprint = %#v", scrubbed.Fingerprint)
	}
}

func TestScrubEventDropsMalformedReplayTelemetryTags(t *testing.T) {
	t.Parallel()

	event := &sentry.Event{Tags: map[string]string{
		"subsystem":          "replay-other",
		"operation":          "merge/customer-1",
		"failure_code":       "database_error",
		"violation_code":     "field-person@example.com",
		"rrweb_version":      "2.1.1+customer@example.com",
		"event_type":         "300",
		"incremental_source": "-1",
		"retry_exhausted":    "yes",
	}}

	scrubbed := observability.ScrubEvent(event, nil)
	if scrubbed.Tags != nil || scrubbed.Fingerprint != nil {
		t.Fatalf("malformed replay telemetry survived: tags=%#v fingerprint=%#v",
			scrubbed.Tags, scrubbed.Fingerprint)
	}
}

func TestScrubEventGenericizesMessageAndExceptionValue(t *testing.T) {
	t.Parallel()

	event := &sentry.Event{
		Message: "panic: reporter email person@example.com submitted invalid token=abc123",
		Exception: []sentry.Exception{{
			Type:  "*errors.errorString",
			Value: "failed for person@example.com with token=abc123",
		}},
	}

	scrubbed := observability.ScrubEvent(event, nil)

	if scrubbed.Message != observability.RedactedTelemetryValue {
		t.Errorf("Message = %q, want redacted placeholder", scrubbed.Message)
	}
	if strings.Contains(scrubbed.Message, "person@example.com") || strings.Contains(scrubbed.Message, "abc123") {
		t.Errorf("Message leaked customer data: %q", scrubbed.Message)
	}
	if len(scrubbed.Exception) != 1 {
		t.Fatalf("Exception length = %d, want 1", len(scrubbed.Exception))
	}
	if scrubbed.Exception[0].Type != "*errors.errorString" {
		t.Errorf("Exception[0].Type = %q, want preserved", scrubbed.Exception[0].Type)
	}
	if scrubbed.Exception[0].Value != observability.RedactedTelemetryValue {
		t.Errorf("Exception[0].Value = %q, want redacted placeholder", scrubbed.Exception[0].Value)
	}
}

func TestScrubEventDropsMechanismFreeTextAndDataButKeepsStructuralMetadata(t *testing.T) {
	t.Parallel()

	handled := true
	parentID := 7
	event := &sentry.Event{
		Exception: []sentry.Exception{{
			Type: "*runtime.PanicNilError",
			Mechanism: &sentry.Mechanism{
				Type: "generic", Handled: &handled, ParentID: &parentID, ExceptionID: 8,
				Description: "failed for person@example.com", HelpLink: "https://example.test/?token=secret",
				Source: "reporter feedback text",
				Data:   map[string]any{"customer_field": "person@example.com"},
			},
		}},
	}

	scrubbed := observability.ScrubEvent(event, nil)

	mechanism := scrubbed.Exception[0].Mechanism
	if mechanism == nil {
		t.Fatal("Mechanism = nil, want preserved")
	}
	if mechanism.Type != "generic" || mechanism.Handled == nil || !*mechanism.Handled ||
		mechanism.ParentID == nil || *mechanism.ParentID != 7 || mechanism.ExceptionID != 8 {
		t.Errorf("Mechanism metadata not preserved: %#v", mechanism)
	}
	if mechanism.Description != "" || mechanism.HelpLink != "" || mechanism.Source != "" ||
		mechanism.Data != nil {
		t.Errorf("Mechanism free text/data survived: %#v", mechanism)
	}
	handled = false
	parentID = 9
	if mechanism.Handled == nil || !*mechanism.Handled ||
		mechanism.ParentID == nil || *mechanism.ParentID != 7 {
		t.Errorf("Mechanism retained mutable pointers from source: %#v", mechanism)
	}
}

func TestScrubEventDeepScrubsStackFramesAndPreservesStructuralEvidence(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)
	event := &sentry.Event{
		EventID:     "abc123",
		Level:       sentry.LevelFatal,
		Timestamp:   now,
		Release:     "fidibeki-api@1.2.3",
		Environment: "staging",
		Exception: []sentry.Exception{{
			Type: "*fmt.wrapError",
			Stacktrace: &sentry.Stacktrace{
				Frames: []sentry.Frame{{
					Function: "handleSubmit", Symbol: "submit", Module: "widgetapi",
					Filename: "handler.go", AbsPath: "/Users/person@example.com/private/handler.go",
					Lineno: 42, Colno: 7, InApp: true, Package: "widgetapi", Platform: "go",
					PreContext: []string{"token=before-secret"}, ContextLine: "email := person@example.com",
					PostContext: []string{"token=after-secret"},
					Vars:        map[string]any{"authorization": "Bearer secret"},
				}},
				FramesOmitted: []uint{1, 2},
			},
		}},
		Threads: []sentry.Thread{{
			ID: "9", Name: "customer-person@example.com",
			Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{
				Function: "worker", Filename: "worker.go", Lineno: 17,
				ContextLine: "token=thread-secret", Vars: map[string]any{"email": "thread@example.com"},
			}}},
			Crashed: true, Current: true,
		}},
	}

	scrubbed := observability.ScrubEvent(event, nil)

	if scrubbed.EventID != "abc123" || scrubbed.Level != sentry.LevelFatal || !scrubbed.Timestamp.Equal(now) ||
		scrubbed.Release != "fidibeki-api@1.2.3" || scrubbed.Environment != "staging" {
		t.Errorf("evidence fields not preserved: %#v", scrubbed)
	}
	if scrubbed.Exception[0].Stacktrace == nil || len(scrubbed.Exception[0].Stacktrace.Frames) != 1 {
		t.Errorf("Stacktrace not preserved: %#v", scrubbed.Exception[0].Stacktrace)
	}
	if scrubbed.Exception[0].Stacktrace.Frames[0].Function != "handleSubmit" {
		t.Errorf("Stacktrace frame function = %q, want preserved", scrubbed.Exception[0].Stacktrace.Frames[0].Function)
	}
	frame := scrubbed.Exception[0].Stacktrace.Frames[0]
	if frame.Filename != "handler.go" || frame.Lineno != 42 || frame.Colno != 7 ||
		!frame.InApp || frame.Module != "widgetapi" {
		t.Errorf("safe frame evidence not preserved: %#v", frame)
	}
	if frame.AbsPath != "" || frame.PreContext != nil || frame.ContextLine != "" ||
		frame.PostContext != nil || frame.Vars != nil {
		t.Errorf("frame free text survived: %#v", frame)
	}
	if scrubbed.Exception[0].Stacktrace == event.Exception[0].Stacktrace {
		t.Error("exception stacktrace pointer was reused")
	}
	if len(scrubbed.Threads) != 1 || scrubbed.Threads[0].Name != "" ||
		scrubbed.Threads[0].Stacktrace == nil ||
		scrubbed.Threads[0].Stacktrace.Frames[0].Function != "worker" {
		t.Errorf("thread structural evidence = %#v", scrubbed.Threads)
	}
	serialized, err := json.Marshal(scrubbed)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, leak := range []string{
		"person@example.com", "before-secret", "after-secret", "Bearer secret",
		"thread-secret", "thread@example.com",
	} {
		if strings.Contains(string(serialized), leak) {
			t.Errorf("scrubbed stack serialization contains %q: %s", leak, serialized)
		}
	}
}

// TestSentryPipelineScrubsRecoveredPanicsEndToEnd proves the BeforeSend wiring, not just the pure
// function: a client configured the same way InitSentry configures it, feeding a panic through the
// same RecoverWithContext path middleware.Recovery uses, must not deliver customer data to the
// transport.
func TestSentryPipelineScrubsRecoveredPanicsEndToEnd(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              "https://public@example.test/1",
		Transport:        transport,
		AttachStacktrace: true,
		BeforeSend:       observability.ScrubEvent,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	reporter := observability.SentryPanicReporter{Hub: sentry.NewHub(client, sentry.NewScope())}

	// A bare string panic (the common `panic("...")` shape) exercises client.EventFromMessage,
	// which attaches a real runtime stacktrace via Threads regardless of the panic value's type —
	// distinct from the Exception.Stacktrace path already covered by the pure-function tests above.
	recovered := "checkout failed for person@example.com with token=abc123 invalid"
	eventID := reporter.CapturePanic(context.Background(), recovered)
	if eventID == "" {
		t.Fatal("CapturePanic() event ID is empty")
	}

	if transport.event == nil {
		t.Fatal("transport did not receive an event")
	}
	serialized, err := json.Marshal(transport.event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, leak := range []string{"person@example.com", "abc123"} {
		if strings.Contains(string(serialized), leak) {
			t.Errorf("delivered event leaked customer data %q: %s", leak, serialized)
		}
	}
	if len(transport.event.Threads) == 0 || transport.event.Threads[0].Stacktrace == nil ||
		len(transport.event.Threads[0].Stacktrace.Frames) == 0 {
		t.Error("delivered event lost stacktrace evidence")
	}
}
