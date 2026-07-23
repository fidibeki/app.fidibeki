package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

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
		SafetyStatus: BatchSafetyQuarantined, Generation: 2,
	}}}
	storage := &batchVerificationStorageStub{body: body}
	checkedAt := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	processor := BatchVerificationProcessor{
		Store: store, Storage: storage, Now: func() time.Time { return checkedAt },
	}
	task := asynq.NewTask(TaskVerifyBatch, []byte(
		`{"tenant_id":"tenant","batch_id":"batch","generation":2}`,
	))
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if store.safeAt == nil || !store.safeAt.Equal(checkedAt) || store.rejectedReason != "" {
		t.Fatalf("verification state = safe %v rejected %q", store.safeAt, store.rejectedReason)
	}
	if storage.removed {
		t.Fatal("valid replay batch object was removed")
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
}

type batchVerificationStoreStub struct {
	work            BatchVerificationWork
	claimErr        error
	safeAt          *time.Time
	rejectedReason  string
	cleanupComplete bool
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
	at time.Time,
) error {
	store.safeAt = &at
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
) error {
	store.cleanupComplete = true
	return nil
}

type batchVerificationStorageStub struct {
	body      []byte
	openErr   error
	removeErr error
	removed   bool
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

func (storage *batchVerificationStorageStub) RemoveObject(context.Context, string) error {
	if storage.removeErr != nil {
		return storage.removeErr
	}
	storage.removed = true
	return nil
}
