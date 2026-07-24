package privacy

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestEffectiveRetentionForPlan(t *testing.T) {
	t.Parallel()
	starter, err := EffectiveRetentionForPlan(tenant.PlanStarter)
	if err != nil {
		t.Fatalf("EffectiveRetentionForPlan(starter) error = %v", err)
	}
	if starter.Plan != tenant.PlanStarter || len(starter.Policies) != 9 {
		t.Fatalf("starter retention = %#v", starter)
	}
	if starter.Policies[1].Category != "auth_magic_links" || starter.Policies[1].DurationDays != nil {
		t.Fatalf("starter auth magic link policy = %#v", starter.Policies[1])
	}
	if starter.Policies[2].Category != "attachments" ||
		starter.Policies[2].DurationDays == nil || *starter.Policies[2].DurationDays != 30 {
		t.Fatalf("starter attachment policy = %#v", starter.Policies[2])
	}
	enterprise, err := EffectiveRetentionForPlan(tenant.PlanEnterprise)
	if err != nil || enterprise.Policies[2].Duration != "Custom" ||
		enterprise.Policies[2].DurationDays != nil {
		t.Fatalf("enterprise retention = %#v, error=%v", enterprise, err)
	}
	if _, err := EffectiveRetentionForPlan(tenant.Plan("unknown")); err == nil {
		t.Fatal("EffectiveRetentionForPlan(unknown) error = nil")
	}
}

func TestRetentionMaintainerKeepsFailedObjectsRetryableAndContinues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	store := &retentionOperationsStub{
		attachmentBatches: [][]RetentionObjectCandidate{{
			{TenantID: "one", ID: "attachment-one", ObjectKey: "attachment-one"},
			{TenantID: "one", ID: "attachment-two", ObjectKey: "attachment-two"},
		}},
		archiveBatches: [][]RetentionObjectCandidate{{
			{TenantID: "one", ID: "archive-one", ObjectKey: "archive-one"},
		}},
		requestResults: []int64{2},
	}
	storage := &retentionStorageStub{
		failures: map[string]error{"attachment-two": errors.New("storage unavailable")},
	}
	var logs bytes.Buffer
	maintainer := RetentionMaintainer{
		Store: store, Storage: storage,
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Now:    func() time.Time { return now }, BatchSize: 10,
	}
	err := maintainer.MaintainOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "storage unavailable") {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if !reflect.DeepEqual(storage.removed, []string{
		"attachment-one", "attachment-two", "archive-one",
	}) {
		t.Fatalf("removed objects = %#v", storage.removed)
	}
	if !reflect.DeepEqual(store.markedAttachments, []string{"attachment-one"}) ||
		!reflect.DeepEqual(store.markedArchives, []string{"archive-one"}) {
		t.Fatalf("marked attachments=%#v archives=%#v",
			store.markedAttachments, store.markedArchives)
	}
	if store.requestCalls != 1 || !store.lastNow.Equal(now) {
		t.Fatalf("request calls=%d now=%v", store.requestCalls, store.lastNow)
	}
	if !strings.Contains(logs.String(), `"attachment_cleanup_failed":true`) ||
		!strings.Contains(logs.String(), `"requests_expired":2`) {
		t.Fatalf("retention logs = %s", logs.String())
	}
}

func TestRetentionMaintainerBoundsWorkAndSupportsShutdown(t *testing.T) {
	t.Parallel()
	store := &retentionOperationsStub{
		attachmentBatches: [][]RetentionObjectCandidate{
			{{ID: "a1", ObjectKey: "a1"}},
			{{ID: "a2", ObjectKey: "a2"}},
		},
		archiveBatches: [][]RetentionObjectCandidate{
			{{ID: "e1", ObjectKey: "e1"}},
			{{ID: "e2", ObjectKey: "e2"}},
		},
		requestResults: []int64{1, 1},
	}
	var logs bytes.Buffer
	maintainer := RetentionMaintainer{
		Store: store, Storage: &retentionStorageStub{},
		Logger:    slog.New(slog.NewJSONHandler(&logs, nil)),
		BatchSize: 1, MaxBatches: 2,
	}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"msg":"privacy retention budget exhausted"`) ||
		!strings.Contains(logs.String(), `"request_budget_exhausted":true`) {
		t.Fatalf("budget logs = %s", logs.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := maintainer.Run(ctx, time.Hour); err != nil {
		t.Fatalf("Run(cancelled) error = %v", err)
	}
}

func TestRetentionMaintainerValidationAndPhaseErrors(t *testing.T) {
	t.Parallel()
	if err := (RetentionMaintainer{}).MaintainOnce(context.Background()); err == nil {
		t.Fatal("MaintainOnce(no store) error = nil")
	}
	if err := (RetentionMaintainer{Store: &retentionOperationsStub{}}).
		MaintainOnce(context.Background()); err == nil {
		t.Fatal("MaintainOnce(no storage) error = nil")
	}
	want := errors.New("database unavailable")
	store := &retentionOperationsStub{
		attachmentErr: want, archiveErr: want, requestErr: want,
	}
	err := (RetentionMaintainer{Store: store, Storage: &retentionStorageStub{}}).
		MaintainOnce(context.Background())
	if !errors.Is(err, want) || store.archiveCalls != 1 || store.requestCalls != 1 {
		t.Fatalf("MaintainOnce(independent failures) error=%v calls=%d/%d",
			err, store.archiveCalls, store.requestCalls)
	}
	if (RetentionMaintainer{}).now().Location() != time.UTC {
		t.Fatal("default retention clock is not UTC")
	}
}

type retentionOperationsStub struct {
	mu                sync.Mutex
	attachmentBatches [][]RetentionObjectCandidate
	archiveBatches    [][]RetentionObjectCandidate
	requestResults    []int64
	attachmentErr     error
	archiveErr        error
	requestErr        error
	markedAttachments []string
	markedArchives    []string
	archiveCalls      int
	requestCalls      int
	lastNow           time.Time
}

func (store *retentionOperationsStub) ExpiredAttachments(
	_ context.Context,
	now time.Time,
	_ int,
) ([]RetentionObjectCandidate, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.lastNow = now
	if store.attachmentErr != nil {
		return nil, store.attachmentErr
	}
	if len(store.attachmentBatches) == 0 {
		return nil, nil
	}
	result := store.attachmentBatches[0]
	store.attachmentBatches = store.attachmentBatches[1:]
	return result, nil
}

func (store *retentionOperationsStub) MarkAttachmentRetentionDeleted(
	_ context.Context,
	candidate RetentionObjectCandidate,
	_ time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.markedAttachments = append(store.markedAttachments, candidate.ID)
	return nil
}

func (store *retentionOperationsStub) ExpiredExportArchives(
	_ context.Context,
	now time.Time,
	_ int,
) ([]RetentionObjectCandidate, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.archiveCalls++
	store.lastNow = now
	if store.archiveErr != nil {
		return nil, store.archiveErr
	}
	if len(store.archiveBatches) == 0 {
		return nil, nil
	}
	result := store.archiveBatches[0]
	store.archiveBatches = store.archiveBatches[1:]
	return result, nil
}

func (store *retentionOperationsStub) MarkExportArchiveExpired(
	_ context.Context,
	candidate RetentionObjectCandidate,
	_ time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.markedArchives = append(store.markedArchives, candidate.ID)
	return nil
}

func (store *retentionOperationsStub) ExpireReporterRequests(
	_ context.Context,
	now time.Time,
	_ int,
) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.requestCalls++
	store.lastNow = now
	if store.requestErr != nil {
		return 0, store.requestErr
	}
	if len(store.requestResults) == 0 {
		return 0, nil
	}
	result := store.requestResults[0]
	store.requestResults = store.requestResults[1:]
	return result, nil
}

type retentionStorageStub struct {
	mu       sync.Mutex
	removed  []string
	failures map[string]error
}

func (storage *retentionStorageStub) RemoveObject(_ context.Context, key string) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.removed = append(storage.removed, key)
	return storage.failures[key]
}
