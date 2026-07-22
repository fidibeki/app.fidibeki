package widgetanalytics

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMaintainerReconcilesBeforeBoundedRetention(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 14, 30, 0, 0, time.UTC)
	store := &maintenanceStoreStub{}
	maintainer := Maintainer{Store: store, Now: func() time.Time { return now }, BatchSize: 37}

	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	wantCalls := []string{
		"reconcile:2026-07-22T14:30:00Z:3",
		"events:2026-04-23T14:30:00Z:37",
		"daily:2024-07-22:37",
	}
	if !reflect.DeepEqual(store.calls, wantCalls) {
		t.Fatalf("maintenance calls = %#v, want %#v", store.calls, wantCalls)
	}

	store.calls = nil
	store.reconcileErr = errors.New("database unavailable")
	if err := maintainer.MaintainOnce(context.Background()); !errors.Is(err, store.reconcileErr) {
		t.Fatalf("MaintainOnce(reconcile failure) error = %v", err)
	}
	if len(store.calls) != 1 || store.calls[0] != "reconcile:2026-07-22T14:30:00Z:3" {
		t.Fatalf("calls after reconcile failure = %#v", store.calls)
	}
}

func TestMaintainerLogsFailureAndContinuesUntilShutdown(t *testing.T) {
	t.Parallel()
	store := &maintenanceLoopStore{called: make(chan int, 3)}
	var logs bytes.Buffer
	maintainer := Maintainer{
		Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Now: func() time.Time { return time.Date(2026, 7, 22, 14, 30, 0, 0, time.UTC) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- maintainer.Run(ctx, time.Millisecond) }()

	for range 2 {
		select {
		case <-store.called:
		case <-time.After(time.Second):
			t.Fatal("maintenance loop did not continue after failure")
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"msg":"widget analytics maintenance failed"`) ||
		!strings.Contains(logs.String(), "temporary reconciliation failure") {
		t.Fatalf("maintenance logs = %s", logs.String())
	}
}

func TestMaintainerResumesAfterPartialCleanup(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 14, 30, 0, 0, time.UTC)
	store := &maintenanceStoreStub{dailyErr: errors.New("daily cleanup unavailable")}
	maintainer := Maintainer{Store: store, Now: func() time.Time { return now }}

	if err := maintainer.MaintainOnce(context.Background()); !errors.Is(err, store.dailyErr) {
		t.Fatalf("MaintainOnce(partial cleanup) error = %v", err)
	}
	if len(store.calls) != 3 {
		t.Fatalf("partial cleanup calls = %#v", store.calls)
	}
	store.dailyErr = nil
	store.calls = nil
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce(resume) error = %v", err)
	}
	if len(store.calls) != 3 {
		t.Fatalf("resumed cleanup calls = %#v", store.calls)
	}
}

func TestMaintainerDrainsBatchesAndKeepsCleanupStagesIndependent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 14, 30, 0, 0, time.UTC)
	var logs bytes.Buffer
	store := &maintenanceStoreStub{
		eventsResults: []int64{10, 10, 3},
		dailyResults:  []int64{10, 2},
	}
	maintainer := Maintainer{
		Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Now: func() time.Time { return now }, BatchSize: 10, MaxBatches: 5,
	}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce(multiple batches) error = %v", err)
	}
	if strings.Count(strings.Join(store.calls, "\n"), "events:") != 3 ||
		strings.Count(strings.Join(store.calls, "\n"), "daily:") != 2 {
		t.Fatalf("multiple-batch calls = %#v", store.calls)
	}
	if !strings.Contains(logs.String(), `"events_deleted":23`) ||
		!strings.Contains(logs.String(), `"daily_aggregates_deleted":12`) {
		t.Fatalf("multiple-batch logs = %s", logs.String())
	}

	store = &maintenanceStoreStub{eventsErr: errors.New("raw cleanup unavailable")}
	maintainer.Store = store
	if err := maintainer.MaintainOnce(context.Background()); !errors.Is(err, store.eventsErr) {
		t.Fatalf("MaintainOnce(raw failure) error = %v", err)
	}
	if len(store.calls) != 3 || !strings.HasPrefix(store.calls[2], "daily:") {
		t.Fatalf("calls after raw cleanup failure = %#v", store.calls)
	}
}

func TestMaintainerSkipsCleanupWithoutReconciliationLease(t *testing.T) {
	t.Parallel()
	store := &maintenanceStoreStub{reconcileResult: ReconcileResult{Skipped: true}}
	maintainer := Maintainer{Store: store}

	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce(skipped) error = %v", err)
	}
	if len(store.calls) != 1 || !strings.HasPrefix(store.calls[0], "reconcile:") {
		t.Fatalf("skipped maintenance calls = %#v", store.calls)
	}
}

func TestMaintainerBoundsPhaseTimeAndRetentionWork(t *testing.T) {
	t.Parallel()
	maintainer := Maintainer{
		Store: blockingMaintenanceStore{}, PhaseTimeout: time.Millisecond,
	}
	if err := maintainer.MaintainOnce(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("MaintainOnce(timeout) error = %v", err)
	}

	var logs bytes.Buffer
	store := &maintenanceStoreStub{eventsResults: []int64{10, 10}}
	maintainer = Maintainer{
		Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		BatchSize: 10, MaxBatches: 2,
	}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce(work budget) error = %v", err)
	}
	if !strings.Contains(logs.String(), `"msg":"widget analytics retention budget exhausted"`) ||
		!strings.Contains(logs.String(), `"event_budget_exhausted":true`) ||
		!strings.Contains(logs.String(), `"backlog_possible":true`) {
		t.Fatalf("retention budget logs = %s", logs.String())
	}
}

type maintenanceStoreStub struct {
	calls           []string
	reconcileErr    error
	eventsErr       error
	dailyErr        error
	reconcileResult ReconcileResult
	eventsResults   []int64
	dailyResults    []int64
}

type maintenanceLoopStore struct {
	mu     sync.Mutex
	calls  int
	called chan int
}

type blockingMaintenanceStore struct{}

func (blockingMaintenanceStore) ReconcileRecent(ctx context.Context, _ time.Time, _ int) (ReconcileResult, error) {
	<-ctx.Done()
	return ReconcileResult{}, ctx.Err()
}

func (blockingMaintenanceStore) DeleteExpiredEvents(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func (blockingMaintenanceStore) DeleteExpiredDaily(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func (store *maintenanceLoopStore) ReconcileRecent(_ context.Context, _ time.Time, _ int) (ReconcileResult, error) {
	store.mu.Lock()
	store.calls++
	call := store.calls
	store.mu.Unlock()
	select {
	case store.called <- call:
	default:
	}
	if call == 1 {
		return ReconcileResult{}, errors.New("temporary reconciliation failure")
	}
	return ReconcileResult{Upserted: 1}, nil
}

func (*maintenanceLoopStore) DeleteExpiredEvents(context.Context, time.Time, int) (int64, error) {
	return 1, nil
}

func (*maintenanceLoopStore) DeleteExpiredDaily(context.Context, time.Time, int) (int64, error) {
	return 1, nil
}

func (store *maintenanceStoreStub) ReconcileRecent(_ context.Context, now time.Time, days int) (ReconcileResult, error) {
	store.calls = append(store.calls, "reconcile:"+now.Format(time.RFC3339)+":"+strconv.Itoa(days))
	return store.reconcileResult, store.reconcileErr
}

func (store *maintenanceStoreStub) DeleteExpiredEvents(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	store.calls = append(store.calls, "events:"+cutoff.Format(time.RFC3339)+":"+strconv.Itoa(limit))
	if len(store.eventsResults) > 0 {
		result := store.eventsResults[0]
		store.eventsResults = store.eventsResults[1:]
		return result, store.eventsErr
	}
	return 0, store.eventsErr
}

func (store *maintenanceStoreStub) DeleteExpiredDaily(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	store.calls = append(store.calls, "daily:"+cutoff.Format(time.DateOnly)+":"+strconv.Itoa(limit))
	if len(store.dailyResults) > 0 {
		result := store.dailyResults[0]
		store.dailyResults = store.dailyResults[1:]
		return result, store.dailyErr
	}
	return 0, store.dailyErr
}
