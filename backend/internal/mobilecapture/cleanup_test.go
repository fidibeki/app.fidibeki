package mobilecapture

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

type cleanupStoreStub struct {
	results []int64
	err     error
	calls   int
	now     time.Time
	limits  []int
}

type cleanupHealthStub struct {
	runs []retentionhealth.Run
	err  error
}

func (health *cleanupHealthStub) RecordRun(
	_ context.Context,
	run retentionhealth.Run,
) error {
	health.runs = append(health.runs, run)
	return health.err
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
	health := &cleanupHealthStub{}
	cleaner := Cleaner{
		Store: store, Health: health, Now: func() time.Time { return now },
	}

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
	if len(health.runs) != 1 || health.runs[0] != (retentionhealth.Run{
		Operation: retentionhealth.OperationMobileCaptureSessions, AttemptedAt: now,
		Result: retentionhealth.ResultSucceeded, Processed: cleanupBatchSize + 3,
	}) {
		t.Fatalf("retention health runs = %#v", health.runs)
	}
}

func TestCleanerPropagatesStoreFailure(t *testing.T) {
	store := &cleanupStoreStub{err: errors.New("database unavailable")}
	health := &cleanupHealthStub{}
	err := (Cleaner{Store: store, Health: health}).CleanOnce(context.Background())
	if err == nil {
		t.Fatal("CleanOnce() error = nil")
	}
	if len(health.runs) != 1 ||
		health.runs[0].Result != retentionhealth.ResultFailed {
		t.Fatalf("retention health runs = %#v", health.runs)
	}
}

func TestCleanerReportsBudgetExhaustion(t *testing.T) {
	results := make([]int64, cleanupBatchLimit)
	for index := range results {
		results[index] = cleanupBatchSize
	}
	store := &cleanupStoreStub{results: results}
	health := &cleanupHealthStub{}

	if err := (Cleaner{Store: store, Health: health}).CleanOnce(
		context.Background(),
	); err != nil {
		t.Fatalf("CleanOnce() error = %v", err)
	}
	if len(health.runs) != 1 || !health.runs[0].BudgetExhausted ||
		health.runs[0].Processed != cleanupBatchSize*cleanupBatchLimit {
		t.Fatalf("retention health runs = %#v", health.runs)
	}
}

func TestCleanerPropagatesHealthFailure(t *testing.T) {
	store := &cleanupStoreStub{results: []int64{0}}
	health := &cleanupHealthStub{err: errors.New("health unavailable")}

	err := (Cleaner{Store: store, Health: health}).CleanOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "record mobile capture retention health") {
		t.Fatalf("CleanOnce() error = %v", err)
	}
}
