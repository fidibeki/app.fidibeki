package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/observability"
	"github.com/fidibeki/app.fidibeki/backend/internal/replay"
	"github.com/getsentry/sentry-go"
)

func TestSentryIsOptionalButRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if err := observability.InitSentry(observability.SentryConfig{}); err != nil {
		t.Fatalf("InitSentry(empty) error = %v", err)
	}
	if err := observability.InitSentry(observability.SentryConfig{DSN: "://invalid"}); err == nil {
		t.Fatal("InitSentry(invalid) error = nil")
	}
}

func TestSentryReplayFailureReporterCapturesBoundedDiagnostics(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://public@example.test/1", Transport: transport,
		BeforeSend: observability.ScrubEvent,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	reporter := observability.SentryReplayFailureReporter{
		Hub: sentry.NewHub(client, sentry.NewScope()),
	}
	reporter.ReportReplayFailure(context.Background(), replay.FailureReport{
		Operation: replay.FailureOperationVerifyBatch,
		Code:      replay.FailureContractViolation, Violation: replay.ViolationMediaFieldsNotSupported,
		RRWebVersion: "2.1.1", EventType: 3, IncrementalSource: 7,
	})

	if transport.event == nil {
		t.Fatal("transport did not receive replay failure event")
	}
	if transport.event.Tags["failure_code"] != "contract_violation" ||
		transport.event.Tags["violation_code"] != "media_fields_not_supported" ||
		transport.event.Tags["rrweb_version"] != "2.1.1" ||
		transport.event.Tags["event_type"] != "3" ||
		transport.event.Tags["incremental_source"] != "7" {
		t.Fatalf("replay failure tags = %#v", transport.event.Tags)
	}
	if len(transport.event.Fingerprint) != 4 {
		t.Fatalf("replay failure fingerprint = %#v", transport.event.Fingerprint)
	}
}

func TestSentryPanicReporterCapturesAnEvent(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@example.test/1",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	reporter := observability.SentryPanicReporter{
		Hub: sentry.NewHub(client, sentry.NewScope()),
	}

	eventID := reporter.CapturePanic(context.Background(), "boom")
	if eventID == "" {
		t.Fatal("CapturePanic() event ID is empty")
	}
	if transport.event == nil || string(transport.event.EventID) != eventID {
		t.Errorf("transport event = %#v, event ID = %q", transport.event, eventID)
	}
}

func TestSentryHelpersRemainSafeWithoutAnActiveClient(t *testing.T) {
	t.Parallel()

	if eventID := (observability.SentryPanicReporter{
		Hub: sentry.NewHub(nil, sentry.NewScope()),
	}).CapturePanic(context.Background(), "not configured"); eventID != "" {
		t.Errorf("CapturePanic() event ID = %q, want empty", eventID)
	}
	if observability.FlushSentry(0) {
		t.Error("FlushSentry() = true without an active client")
	}
}

type captureTransport struct {
	event *sentry.Event
}

func (*captureTransport) Flush(time.Duration) bool              { return true }
func (*captureTransport) FlushWithContext(context.Context) bool { return true }
func (*captureTransport) Configure(sentry.ClientOptions)        {}
func (t *captureTransport) SendEvent(event *sentry.Event)       { t.event = event }
func (*captureTransport) Close()                                {}
