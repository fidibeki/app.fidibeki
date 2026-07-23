package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/hibiken/asynq"
)

func TestDecodeVerifiedBatchAcceptsBoundedRRWebEvents(t *testing.T) {
	body := []byte(`[
		{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0}}},
		{"type":3,"timestamp":1784800000100,"data":{"source":5}}
	]`)
	var decoded [][]byte
	count, err := decodeVerifiedBatch(bytes.NewReader(body), Batch{
		Index: 4, SizeBytes: int64(len(body)), EventsCount: 2,
	}, func(event []byte) error {
		decoded = append(decoded, append([]byte(nil), event...))
		return nil
	})
	if err != nil {
		t.Fatalf("decodeVerifiedBatch() error = %v", err)
	}
	if count != 2 || len(decoded) != 2 ||
		!strings.Contains(string(decoded[1]), `"source":5`) {
		t.Fatalf("decoded count/events = %d/%q", count, decoded)
	}
}

func TestDecodeVerifiedBatchRejectsUnsafePayloads(t *testing.T) {
	validEvent := `{"type":2,"timestamp":1784800000000,"data":{}}`
	tests := []struct {
		name   string
		body   string
		size   int64
		count  int
		reason string
	}{
		{name: "truncated", body: `[`, size: 2, count: 1, reason: "size_mismatch"},
		{name: "oversized", body: `[] `, size: 2, count: 0, reason: "size_mismatch"},
		{name: "not array", body: `{}`, size: 2, count: 0, reason: "expected_array"},
		{name: "trailing JSON", body: `[] {}`, size: 5, count: 0, reason: "trailing_json"},
		{name: "wrong count", body: `[` + validEvent + `]`, count: 2, reason: "event_count_mismatch"},
		{name: "missing type", body: `[{"timestamp":1784800000000,"data":{}}]`, count: 1, reason: "invalid_event"},
		{name: "invalid type", body: `[{"type":7,"timestamp":1784800000000,"data":{}}]`, count: 1, reason: "invalid_event"},
		{name: "missing timestamp", body: `[{"type":2,"data":{}}]`, count: 1, reason: "invalid_event"},
		{name: "missing data", body: `[{"type":2,"timestamp":1784800000000}]`, count: 1, reason: "invalid_event"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.size == 0 {
				test.size = int64(len(test.body))
			}
			_, err := decodeVerifiedBatch(strings.NewReader(test.body), Batch{
				Index: 1, SizeBytes: test.size, EventsCount: test.count,
			}, nil)
			var unsafe unsafeReplayBatch
			if err == nil || !errors.As(err, &unsafe) || unsafe.reason != test.reason {
				t.Fatalf("decodeVerifiedBatch() error = %#v, want reason %q", err, test.reason)
			}
		})
	}
}

func TestBatchVerificationProcessorPromotesOnlyValidBatches(t *testing.T) {
	body := []byte(`[{"type":2,"timestamp":1784800000000,"data":{}}]`)
	store := &batchVerificationStoreStub{work: BatchVerificationWork{Batch: Batch{
		ID: "batch", TenantID: "tenant", ProjectID: "project", SessionID: "session",
		Index: 0, StorageKey: "batch.json", EventsCount: 1, SizeBytes: int64(len(body)),
		SafetyStatus: BatchSafetyQuarantined, Generation: 2, VerificationTries: 1,
		UploadExpiresAt: time.Date(2026, 7, 23, 10, 10, 0, 0, time.UTC),
	}}}
	storage := &batchVerificationStorageStub{body: body}
	checkedAt := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	current := checkedAt
	processor := BatchVerificationProcessor{
		Store: store, Storage: storage, Now: func() time.Time { return current },
	}
	stagingKey := store.work.StorageKey
	task := asynq.NewTask(TaskVerifyBatch, []byte(
		`{"tenant_id":"tenant","batch_id":"batch","generation":2}`,
	))
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if store.safeAt == nil || !store.safeAt.Equal(checkedAt) || store.rejectedReason != "" {
		t.Fatalf("verification state = safe %v rejected %q", store.safeAt, store.rejectedReason)
	}
	verifiedKey := verifiedBatchKey(
		stagingKey, store.work.Generation, store.work.VerificationTries,
	)
	if !storage.removed || store.safeCleanupComplete ||
		!bytes.Equal(storage.objects[verifiedKey], body) {
		t.Fatalf("valid replay promotion = removed %t cleanup %t objects %#v",
			storage.removed, store.safeCleanupComplete, storage.objects)
	}
	current = store.work.UploadExpiresAt.Add(uploadCleanupGrace)
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("Handler(final cleanup) error = %v", err)
	}
	if !store.safeCleanupComplete {
		t.Fatal("verified replay staging cleanup was not sealed after upload expiry")
	}
}

func TestVerifiedReplayStorageKeysRejectMalformedValues(t *testing.T) {
	t.Parallel()
	firstAttempt := verifiedBatchKey("batch.json", 12, 1)
	secondAttempt := verifiedBatchKey("batch.json", 12, 2)
	if firstAttempt == secondAttempt {
		t.Fatalf("verified keys must differ by claim attempt: %q", firstAttempt)
	}
	for _, key := range []string{
		"batch.json",
		"batch.json.verified-zero",
		"batch.json.verified-0",
		"batch.json.verified-1-zero",
		"batch.json.verified-1-0",
		"batch.json.verified-1-2-3",
	} {
		if _, err := stagingBatchKey(key); err == nil {
			t.Fatalf("stagingBatchKey(%q) error = nil", key)
		}
	}
	if got, err := stagingBatchKey("batch.json.verified-12"); err != nil ||
		got != "batch.json" {
		t.Fatalf("stagingBatchKey(valid) = %q, %v", got, err)
	}
	if got, err := stagingBatchKey(secondAttempt); err != nil ||
		got != "batch.json" {
		t.Fatalf("stagingBatchKey(unique attempt) = %q, %v", got, err)
	}
}

func TestReplayVerificationStagingRejectsInvalidSize(t *testing.T) {
	t.Parallel()
	if _, err := stageBatchVerificationObject(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("stageBatchVerificationObject(zero size) error = nil")
	}
}

func TestBatchVerificationProcessorPersistsRejectionBeforeCleanup(t *testing.T) {
	store := &batchVerificationStoreStub{work: BatchVerificationWork{Batch: Batch{
		ID: "batch", TenantID: "tenant", ProjectID: "project", SessionID: "session",
		Index: 0, StorageKey: "batch.json", EventsCount: 1, SizeBytes: 2,
		SafetyStatus: BatchSafetyQuarantined, Generation: 1,
	}}}
	storage := &batchVerificationStorageStub{body: []byte(`{}`), removeErr: errors.New("storage unavailable")}
	processor := BatchVerificationProcessor{Store: store, Storage: storage}
	task := asynq.NewTask(TaskVerifyBatch, []byte(
		`{"tenant_id":"tenant","batch_id":"batch","generation":1}`,
	))
	err := processor.Handler()(context.Background(), task)
	if !errors.Is(err, storage.removeErr) || store.rejectedReason != "expected_array" ||
		store.cleanupComplete {
		t.Fatalf(
			"Handler() error/state = %v rejected %q cleanup %t",
			err, store.rejectedReason, store.cleanupComplete,
		)
	}

	storage.removeErr = nil
	store.work.SafetyStatus = BatchSafetyRejected
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("Handler(retry) error = %v", err)
	}
	if !storage.removed || !store.cleanupComplete {
		t.Fatalf("rejection retry = removed %t cleanup %t", storage.removed, store.cleanupComplete)
	}
}

func TestBatchVerificationProcessorLeavesOperationalFailuresQuarantined(t *testing.T) {
	want := errors.New("storage unavailable")
	store := &batchVerificationStoreStub{work: BatchVerificationWork{Batch: Batch{
		ID: "batch", TenantID: "tenant", StorageKey: "batch.json",
		SafetyStatus: BatchSafetyQuarantined, Generation: 1,
	}}}
	processor := BatchVerificationProcessor{
		Store: store, Storage: &batchVerificationStorageStub{openErr: want},
	}
	task := asynq.NewTask(TaskVerifyBatch, []byte(
		`{"tenant_id":"tenant","batch_id":"batch","generation":1}`,
	))
	if err := processor.Handler()(context.Background(), task); !errors.Is(err, want) {
		t.Fatalf("Handler() error = %v, want %v", err, want)
	}
	if store.safeAt != nil || store.rejectedReason != "" {
		t.Fatalf("operational failure changed safety = %v/%q", store.safeAt, store.rejectedReason)
	}

	diagnosticFailure := errors.New("diagnostics unavailable")
	rejectedStore := &batchVerificationStoreStub{work: BatchVerificationWork{Batch: Batch{
		ID: "batch", TenantID: "tenant", ProjectID: "project", SessionID: "session",
		StorageKey: "batch.json", SizeBytes: 2, EventsCount: 0,
		SafetyStatus: BatchSafetyQuarantined, Generation: 1,
	}}}
	if err := (BatchVerificationProcessor{
		Store: rejectedStore, Storage: &batchVerificationStorageStub{body: []byte(`{}`)},
		Diagnostics: batchDiagnosticRecorderFunc(func(
			context.Context,
			abuse.DiagnosticEvent,
		) error {
			return diagnosticFailure
		}),
	}).Handler()(context.Background(), task); err != nil {
		t.Fatalf("optional diagnostic failure = %v", err)
	}
	if rejectedStore.rejectedReason != "expected_array" {
		t.Fatalf("rejected reason = %q", rejectedStore.rejectedReason)
	}
}

type batchVerificationStoreStub struct {
	work                BatchVerificationWork
	claimErr            error
	safeAt              *time.Time
	safeKey             string
	rejectedReason      string
	cleanupComplete     bool
	safeCleanupComplete bool
}

func (store *batchVerificationStoreStub) Claim(
	context.Context,
	BatchVerificationTask,
) (BatchVerificationWork, error) {
	return store.work, store.claimErr
}

func (store *batchVerificationStoreStub) MarkSafe(
	_ context.Context,
	_ BatchVerificationWork,
	verifiedKey string,
	at time.Time,
) error {
	store.safeKey = verifiedKey
	store.safeAt = &at
	store.work.SafetyStatus = BatchSafetySafe
	store.work.StorageKey = verifiedKey
	return nil
}

func (store *batchVerificationStoreStub) CompleteSafeCleanup(
	context.Context,
	BatchVerificationWork,
	time.Time,
) error {
	store.safeCleanupComplete = true
	return nil
}

func (store *batchVerificationStoreStub) MarkRejected(
	_ context.Context,
	_ BatchVerificationWork,
	reason string,
	_ time.Time,
) error {
	store.rejectedReason = reason
	return nil
}

func (store *batchVerificationStoreStub) CompleteRejectedCleanup(
	context.Context,
	BatchVerificationWork,
	time.Time,
) error {
	store.cleanupComplete = true
	return nil
}

type batchVerificationStorageStub struct {
	body      []byte
	objects   map[string][]byte
	openErr   error
	removeErr error
	removed   bool
}

type batchDiagnosticRecorderFunc func(context.Context, abuse.DiagnosticEvent) error

func (record batchDiagnosticRecorderFunc) Record(
	ctx context.Context,
	event abuse.DiagnosticEvent,
) error {
	return record(ctx, event)
}

func (storage *batchVerificationStorageStub) OpenObject(
	context.Context,
	string,
) (io.ReadCloser, error) {
	if storage.openErr != nil {
		return nil, storage.openErr
	}
	return io.NopCloser(bytes.NewReader(storage.body)), nil
}

func (storage *batchVerificationStorageStub) PutObject(
	_ context.Context,
	key string,
	reader io.Reader,
	_ int64,
	_ string,
) error {
	value, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if storage.objects == nil {
		storage.objects = make(map[string][]byte)
	}
	storage.objects[key] = value
	return nil
}

func (storage *batchVerificationStorageStub) RemoveObject(context.Context, string) error {
	if storage.removeErr != nil {
		return storage.removeErr
	}
	storage.removed = true
	return nil
}
