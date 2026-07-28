package mobilecapture

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cleanupStoreStub struct {
	results []int64
	err     error
	calls   int
	now     time.Time
	limits  []int
}

func (store *cleanupStoreStub) DeleteExpired(
	_ context.Context,
	now time.Time,
	limit int,
) (int64, error) {
	store.calls++
	store.now = now
	store.limits = append(store.limits, limit)
	if store.err != nil {
		return 0, store.err
	}
	result := store.results[store.calls-1]
	return result, nil
}

func TestCleanerDrainsBoundedBatches(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store := &cleanupStoreStub{results: []int64{cleanupBatchSize, 3}}
	cleaner := Cleaner{Store: store, Now: func() time.Time { return now }}

	if err := cleaner.CleanOnce(context.Background()); err != nil {
		t.Fatalf("CleanOnce() error = %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("DeleteExpired() calls = %d, want 2", store.calls)
	}
	if !store.now.Equal(now) {
		t.Fatalf("DeleteExpired() now = %v, want %v", store.now, now)
	}
	for _, limit := range store.limits {
		if limit != cleanupBatchSize {
			t.Fatalf("DeleteExpired() limit = %d, want %d", limit, cleanupBatchSize)
		}
	}
}

func TestCleanerPropagatesStoreFailure(t *testing.T) {
	store := &cleanupStoreStub{err: errors.New("database unavailable")}
	err := (Cleaner{Store: store}).CleanOnce(context.Background())
	if err == nil {
		t.Fatal("CleanOnce() error = nil")
	}
}
