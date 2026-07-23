package abuse

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const defaultDiagnosticBufferKeys = 10_000

type DiagnosticSink interface {
	Record(context.Context, DiagnosticEvent) error
}

type diagnosticBufferKey struct {
	tenantID  string
	projectID string
	category  Category
	reason    string
	bucket    time.Time
}

type BufferedRecorder struct {
	Sink    DiagnosticSink
	Logger  *slog.Logger
	MaxKeys int

	mu      sync.Mutex
	pending map[diagnosticBufferKey]DiagnosticEvent
}

func NewBufferedRecorder(sink DiagnosticSink, logger *slog.Logger) *BufferedRecorder {
	return &BufferedRecorder{
		Sink: sink, Logger: logger, MaxKeys: defaultDiagnosticBufferKeys,
		pending: make(map[diagnosticBufferKey]DiagnosticEvent),
	}
}

func (recorder *BufferedRecorder) Record(_ context.Context, event DiagnosticEvent) error {
	if recorder == nil || recorder.Sink == nil {
		return errors.New("abuse diagnostic sink is not configured")
	}
	if err := event.validate(); err != nil {
		return err
	}
	event.Count = max(event.Count, 1)
	event.OccurredAt = event.OccurredAt.UTC()
	if event.FirstOccurredAt.IsZero() {
		event.FirstOccurredAt = event.OccurredAt
	} else {
		event.FirstOccurredAt = event.FirstOccurredAt.UTC()
	}
	event.Reason = strings.TrimSpace(event.Reason)
	key := diagnosticBufferKey{
		tenantID: event.TenantID, projectID: event.ProjectID,
		category: event.Category, reason: event.Reason,
		bucket: event.OccurredAt.Truncate(time.Hour),
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if current, exists := recorder.pending[key]; exists {
		current.Count += event.Count
		if event.FirstOccurredAt.Before(current.FirstOccurredAt) {
			current.FirstOccurredAt = event.FirstOccurredAt
		}
		if event.OccurredAt.After(current.OccurredAt) {
			current.OccurredAt = event.OccurredAt
		}
		recorder.pending[key] = current
		return nil
	}
	limit := recorder.MaxKeys
	if limit <= 0 {
		limit = defaultDiagnosticBufferKeys
	}
	if len(recorder.pending) >= limit {
		return nil
	}
	recorder.pending[key] = event
	return nil
}

func (recorder *BufferedRecorder) Flush(ctx context.Context) error {
	if recorder == nil || recorder.Sink == nil {
		return errors.New("abuse diagnostic sink is not configured")
	}
	recorder.mu.Lock()
	pending := recorder.pending
	recorder.pending = make(map[diagnosticBufferKey]DiagnosticEvent, len(pending))
	recorder.mu.Unlock()

	var result error
	for key, event := range pending {
		if err := recorder.Sink.Record(ctx, event); err != nil {
			result = errors.Join(result, err)
			recorder.mu.Lock()
			if current, exists := recorder.pending[key]; exists {
				current.Count += event.Count
				if event.FirstOccurredAt.Before(current.FirstOccurredAt) {
					current.FirstOccurredAt = event.FirstOccurredAt
				}
				if event.OccurredAt.After(current.OccurredAt) {
					current.OccurredAt = event.OccurredAt
				}
				recorder.pending[key] = current
			} else if len(recorder.pending) < recorder.maxKeys() {
				recorder.pending[key] = event
			}
			recorder.mu.Unlock()
		}
	}
	return result
}

func (recorder *BufferedRecorder) maxKeys() int {
	if recorder.MaxKeys > 0 {
		return recorder.MaxKeys
	}
	return defaultDiagnosticBufferKeys
}

func (recorder *BufferedRecorder) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := recorder.Flush(flushCtx)
			cancel()
			return err
		case <-ticker.C:
			if err := recorder.Flush(ctx); err != nil {
				recorder.logger().ErrorContext(ctx, "flush abuse diagnostics", "error", err)
			}
		}
	}
}

func (recorder *BufferedRecorder) logger() *slog.Logger {
	if recorder.Logger != nil {
		return recorder.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
