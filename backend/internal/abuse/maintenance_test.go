package abuse

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMaintainerDeletesNinetyDayDataInBoundedBatches(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store := &cleanupStoreStub{deleted: []int64{1000, 25}}
	if err := (Maintainer{Store: store, Now: func() time.Time { return now }}).
		MaintainOnce(context.Background()); err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if store.calls != 2 || !store.before.Equal(now.AddDate(0, 0, -90)) {
		t.Errorf("cleanup calls=%d before=%s", store.calls, store.before)
	}
}

func TestMaintainerReturnsCleanupFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("database unavailable")
	err := (Maintainer{Store: &cleanupStoreStub{err: failure}}).
		MaintainOnce(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("maintain error = %v, want %v", err, failure)
	}
}

func TestMaintainerRunReconcilesBeforeCanceledShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &cleanupStoreStub{err: errors.New("database unavailable")}
	if err := (Maintainer{Store: store}).Run(ctx, 0); err != nil {
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
