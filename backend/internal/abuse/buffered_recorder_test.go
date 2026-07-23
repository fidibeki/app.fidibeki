package abuse

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBufferedRecorderAggregatesWithoutCallingSinkOnRequest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 12, 30, 0, 0, time.UTC)
	sink := &diagnosticSinkStub{}
	recorder := NewBufferedRecorder(sink, nil)
	event := DiagnosticEvent{
		TenantID: "tenant", ProjectID: "project", Category: CategoryRateLimit,
		Reason: "submission_rate_limited", OccurredAt: now,
	}
	for index := range 3 {
		recorded := event
		recorded.OccurredAt = now.Add(time.Duration(index) * time.Minute)
		if err := recorder.Record(context.Background(), recorded); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	if len(sink.events) != 0 {
		t.Fatalf("request path wrote diagnostics = %#v", sink.events)
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if len(sink.events) != 1 || sink.events[0].Count != 3 ||
		!sink.events[0].FirstOccurredAt.Equal(now) ||
		!sink.events[0].OccurredAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("flushed diagnostics = %#v", sink.events)
	}
}

func TestBufferedRecorderRetriesFailedFlushAndBoundsKeys(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 12, 30, 0, 0, time.UTC)
	want := errors.New("database unavailable")
	sink := &diagnosticSinkStub{err: want}
	recorder := NewBufferedRecorder(sink, nil)
	recorder.MaxKeys = 1
	first := DiagnosticEvent{
		TenantID: "tenant", ProjectID: "project", Category: CategoryProvider,
		Reason: "first", OccurredAt: now,
	}
	second := first
	second.Reason = "second"
	_ = recorder.Record(context.Background(), first)
	_ = recorder.Record(context.Background(), second)
	if err := recorder.Flush(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Flush() error = %v", err)
	}
	sink.err = nil
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatalf("retry Flush() error = %v", err)
	}
	if len(sink.events) != 1 || sink.events[0].Reason != first.Reason {
		t.Fatalf("bounded/retried diagnostics = %#v", sink.events)
	}
}

func TestBufferedRecorderRunRetriesAndFlushesOnShutdown(t *testing.T) {
	t.Parallel()
	want := errors.New("database unavailable")
	sink := &diagnosticSinkStub{err: want, called: make(chan struct{}, 2)}
	recorder := NewBufferedRecorder(sink, nil)
	if err := recorder.Record(context.Background(), DiagnosticEvent{
		TenantID: "tenant", ProjectID: "project", Category: CategoryProvider,
		Reason: "provider_error", OccurredAt: time.Now(),
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- recorder.Run(ctx, time.Millisecond)
	}()
	select {
	case <-sink.called:
	case <-time.After(time.Second):
		t.Fatal("periodic diagnostic flush did not run")
	}
	cancel()
	if err := <-result; !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if recorder.logger() == nil {
		t.Fatal("default logger = nil")
	}
}

func TestBufferedRecorderRejectsInvalidConfigurationAndEvents(t *testing.T) {
	t.Parallel()
	var recorder *BufferedRecorder
	if err := recorder.Record(context.Background(), DiagnosticEvent{}); err == nil {
		t.Fatal("Record(nil recorder) error = nil")
	}
	if err := recorder.Flush(context.Background()); err == nil {
		t.Fatal("Flush(nil recorder) error = nil")
	}
	recorder = NewBufferedRecorder(&diagnosticSinkStub{}, nil)
	if err := recorder.Record(context.Background(), DiagnosticEvent{}); err == nil {
		t.Fatal("Record(invalid event) error = nil")
	}
}

type diagnosticSinkStub struct {
	mu     sync.Mutex
	events []DiagnosticEvent
	err    error
	called chan struct{}
}

func (sink *diagnosticSinkStub) Record(_ context.Context, event DiagnosticEvent) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.called != nil {
		select {
		case sink.called <- struct{}{}:
		default:
		}
	}
	if sink.err != nil {
		return sink.err
	}
	sink.events = append(sink.events, event)
	return nil
}
