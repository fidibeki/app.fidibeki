package tenantmember

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMaintainerDrainsBatchesUntilExhausted(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	store := &magicLinkOperationsStub{results: []int64{2, 2, 1}}
	var logs bytes.Buffer
	maintainer := Maintainer{
		Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
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
}

func TestMaintainerBoundsWorkAndWarnsOnExhaustedBudget(t *testing.T) {
	t.Parallel()
	store := &magicLinkOperationsStub{results: []int64{1, 1, 1}}
	var logs bytes.Buffer
	maintainer := Maintainer{
		Store: store, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		BatchSize: 1, MaxBatches: 2,
	}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if store.calls != 2 {
		t.Fatalf("calls = %d, want 2 (bounded by MaxBatches)", store.calls)
	}
	if !strings.Contains(logs.String(), `"msg":"tenant magic link retention budget exhausted"`) {
		t.Fatalf("budget logs = %s", logs.String())
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
	want := errors.New("database unavailable")
	store := &magicLinkOperationsStub{err: want}
	err := (Maintainer{Store: store}).MaintainOnce(context.Background())
	if !errors.Is(err, want) || store.calls != 1 {
		t.Fatalf("MaintainOnce(store error) error=%v calls=%d", err, store.calls)
	}
	if (Maintainer{}).now().Location() != time.UTC {
		t.Fatal("default retention clock is not UTC")
	}
}

type magicLinkOperationsStub struct {
	mu      sync.Mutex
	results []int64
	err     error
	calls   int
	lastNow time.Time
}

func (store *magicLinkOperationsStub) ExpiredMagicLinks(
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
