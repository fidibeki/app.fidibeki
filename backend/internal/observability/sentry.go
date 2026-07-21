package observability

import (
	"context"
	"fmt"
	"time"

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
