package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/observability"
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

type captureTransport struct {
	event *sentry.Event
}

func (*captureTransport) Flush(time.Duration) bool              { return true }
func (*captureTransport) FlushWithContext(context.Context) bool { return true }
func (*captureTransport) Configure(sentry.ClientOptions)        {}
func (t *captureTransport) SendEvent(event *sentry.Event)       { t.event = event }
func (*captureTransport) Close()                                {}
