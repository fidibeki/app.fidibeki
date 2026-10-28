package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/replay"
	"github.com/getsentry/sentry-go"
)

type SentryConfig struct {
	DSN         string
	Environment string
	Release     string
	Debug       bool
}

func InitSentry(config SentryConfig) error {
	if config.DSN == "" {
		return nil
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              config.DSN,
		Environment:      config.Environment,
		Release:          config.Release,
		Debug:            config.Debug,
		AttachStacktrace: true,
		SendDefaultPII:   false,
		BeforeSend:       ScrubEvent,
	}); err != nil {
		return fmt.Errorf("initialize Sentry: %w", err)
	}
	return nil
}

func FlushSentry(timeout time.Duration) bool {
	return sentry.Flush(timeout)
}

type SentryPanicReporter struct {
	Hub *sentry.Hub
}

type SentryReplayFailureReporter struct {
	Hub *sentry.Hub
}

func (r SentryReplayFailureReporter) ReportReplayFailure(
	_ context.Context,
	report replay.FailureReport,
) {
	tags := report.TelemetryTags()
	if len(tags) == 0 {
		return
	}
	hub := r.Hub
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Message = "replay background processing failed"
	event.Tags = tags
	event.Fingerprint = replayFailureFingerprint(tags)
	hub.CaptureEvent(event)
}

func replayFailureFingerprint(tags map[string]string) []string {
	fingerprint := []string{tags["subsystem"], tags["operation"], tags["failure_code"]}
	if violation := tags["violation_code"]; violation != "" {
		fingerprint = append(fingerprint, violation)
	}
	return fingerprint
}

func (r SentryPanicReporter) CapturePanic(ctx context.Context, recovered any) string {
	hub := r.Hub
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}
	eventID := hub.RecoverWithContext(ctx, recovered)
	if eventID == nil {
		return ""
	}
	return string(*eventID)
}
