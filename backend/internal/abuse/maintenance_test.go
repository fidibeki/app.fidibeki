package abuse

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

func TestMaintainerDeletesNinetyDayDataInBoundedBatches(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store := &cleanupStoreStub{deleted: []int64{1000, 25}}
	health := &diagnosticHealthRecorderStub{}
	if err := (Maintainer{Store: store, Health: health, Now: func() time.Time { return now }}).
		MaintainOnce(context.Background()); err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if store.calls != 2 || !store.before.Equal(now.AddDate(0, 0, -90)) {
		t.Errorf("cleanup calls=%d before=%s", store.calls, store.before)
	}
	if len(health.runs) != 1 ||
		health.runs[0].Operation != retentionhealth.OperationAbuseDiagnostics ||
		health.runs[0].Result != retentionhealth.ResultSucceeded ||
		health.runs[0].Processed != 1025 || health.runs[0].BudgetExhausted ||
		!health.runs[0].AttemptedAt.Equal(now) {
		t.Fatalf("health runs = %#v", health.runs)
	}
}

func TestMaintainerReturnsCleanupFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("database unavailable")
	health := &diagnosticHealthRecorderStub{}
	err := (Maintainer{Store: &cleanupStoreStub{err: failure}, Health: health}).
		MaintainOnce(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("maintain error = %v, want %v", err, failure)
	}
	if len(health.runs) != 1 || health.runs[0].Result != retentionhealth.ResultFailed {
		t.Fatalf("failed health runs = %#v", health.runs)
	}
	if err := (Maintainer{Store: &cleanupStoreStub{}}).
		MaintainOnce(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "health recorder is unavailable") {
		t.Fatalf("maintain(no health recorder) error = %v", err)
	}
	recordFailure := errors.New("health database unavailable")
	err = (Maintainer{
		Store:  &cleanupStoreStub{},
		Health: &diagnosticHealthRecorderStub{err: recordFailure},
	}).MaintainOnce(context.Background())
	if !errors.Is(err, recordFailure) {
		t.Fatalf("maintain(record failure) error = %v", err)
	}
}

func TestMaintainerRunReconcilesBeforeCanceledShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &cleanupStoreStub{err: errors.New("database unavailable")}
	if err := (Maintainer{Store: store, Health: &diagnosticHealthRecorderStub{}}).
		Run(ctx, 0); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("Run() cleanup calls = %d, want 1", store.calls)
	}
}

func TestMaintenanceStoreRequiresConfiguredRecorder(t *testing.T) {
	t.Parallel()

	store := MaintenanceStore{}
	if _, err := store.DeleteExpired(context.Background(), time.Now(), 0); err == nil {
		t.Fatal("DeleteExpired() error = nil")
	}
}

type diagnosticHealthRecorderStub struct {
	runs []retentionhealth.Run
	err  error
}

func (recorder *diagnosticHealthRecorderStub) RecordRun(
	_ context.Context,
	run retentionhealth.Run,
) error {
	recorder.runs = append(recorder.runs, run)
	return recorder.err
}

type cleanupStoreStub struct {
	deleted []int64
	err     error
	calls   int
	before  time.Time
}

func (store *cleanupStoreStub) DeleteExpired(
	_ context.Context,
	before time.Time,
	_ int,
) (int64, error) {
	store.calls++
	store.before = before
	if store.err != nil {
		return 0, store.err
	}
	if len(store.deleted) == 0 {
		return 0, nil
	}
	value := store.deleted[0]
	store.deleted = store.deleted[1:]
	return value, nil
}
