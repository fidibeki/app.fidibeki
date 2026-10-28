package platformadmin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
)

func TestMaintainerDrainsBatchesUntilExhausted(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	store := &platformMagicLinkOperationsStub{results: []int64{2, 2, 1}}
	health := &platformMagicLinkHealthRecorderStub{}
	var logs bytes.Buffer
	maintainer := Maintainer{
		Store: store, Health: health, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Now: func() time.Time { return now }, BatchSize: 2,
	}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if store.calls != 3 || !store.lastNow.Equal(now) {
		t.Fatalf("calls = %d, lastNow = %v", store.calls, store.lastNow)
	}
	if !strings.Contains(logs.String(), `"magic_links_deleted":5`) {
		t.Fatalf("retention logs = %s", logs.String())
	}
	if len(health.runs) != 1 ||
		health.runs[0].Operation != retentionhealth.OperationPlatformAdminMagicLinks ||
		health.runs[0].Result != retentionhealth.ResultSucceeded ||
		health.runs[0].Processed != 5 || health.runs[0].BudgetExhausted ||
		!health.runs[0].AttemptedAt.Equal(now) {
		t.Fatalf("health runs = %#v", health.runs)
	}
}

func TestMaintainerBoundsWorkAndWarnsOnExhaustedBudget(t *testing.T) {
	t.Parallel()
	store := &platformMagicLinkOperationsStub{results: []int64{1, 1, 1}}
	health := &platformMagicLinkHealthRecorderStub{}
	var logs bytes.Buffer
	maintainer := Maintainer{
		Store: store, Health: health, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		BatchSize: 1, MaxBatches: 2,
	}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("calls = %d, want 2 (bounded by MaxBatches)", store.calls)
	}
	if !strings.Contains(logs.String(), `"msg":"platform admin magic link retention budget exhausted"`) {
		t.Fatalf("budget logs = %s", logs.String())
	}
	if len(health.runs) != 1 || !health.runs[0].BudgetExhausted ||
		health.runs[0].Processed != 2 {
		t.Fatalf("health runs = %#v", health.runs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := maintainer.Run(ctx, time.Hour); err != nil {
		t.Fatalf("Run(cancelled) error = %v", err)
	}
}

func TestMaintainerSurfacesStoreErrorsAndValidatesConfiguration(t *testing.T) {
	t.Parallel()
	if err := (Maintainer{}).MaintainOnce(context.Background()); err == nil {
		t.Fatal("MaintainOnce(no store) error = nil")
	}
	if err := (Maintainer{Store: &platformMagicLinkOperationsStub{}}).
		MaintainOnce(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "health recorder is unavailable") {
		t.Fatalf("MaintainOnce(no health recorder) error = %v", err)
	}
	want := errors.New("database unavailable")
	store := &platformMagicLinkOperationsStub{err: want}
	health := &platformMagicLinkHealthRecorderStub{}
	err := (Maintainer{Store: store, Health: health}).MaintainOnce(context.Background())
	if !errors.Is(err, want) || store.calls != 1 {
		t.Fatalf("MaintainOnce(store error) error=%v calls=%d", err, store.calls)
	}
	if len(health.runs) != 1 || health.runs[0].Result != retentionhealth.ResultFailed {
		t.Fatalf("failed health runs = %#v", health.runs)
	}
	recordFailure := errors.New("health database unavailable")
	err = (Maintainer{
		Store:  &platformMagicLinkOperationsStub{},
		Health: &platformMagicLinkHealthRecorderStub{err: recordFailure},
	}).MaintainOnce(context.Background())
	if !errors.Is(err, recordFailure) {
		t.Fatalf("MaintainOnce(record failure) error = %v", err)
	}
	if (Maintainer{}).now().Location() != time.UTC {
		t.Fatal("default retention clock is not UTC")
	}
}

type platformMagicLinkHealthRecorderStub struct {
	runs []retentionhealth.Run
	err  error
}

func (recorder *platformMagicLinkHealthRecorderStub) RecordRun(
	_ context.Context,
	run retentionhealth.Run,
) error {
	recorder.runs = append(recorder.runs, run)
	return recorder.err
}

type platformMagicLinkOperationsStub struct {
	mu      sync.Mutex
	results []int64
	err     error
	calls   int
	lastNow time.Time
}

func (store *platformMagicLinkOperationsStub) ExpiredMagicLinks(
	_ context.Context,
	now time.Time,
	_ int,
) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.calls++
	store.lastNow = now
	if store.err != nil {
		return 0, store.err
	}
	if len(store.results) == 0 {
		return 0, nil
	}
	result := store.results[0]
	store.results = store.results[1:]
	return result, nil
}
