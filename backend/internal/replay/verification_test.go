package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/abuse"
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
	}, "https://example.com/chart", func(event []byte) error {
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
	validEvent := `{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0}}}`
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
			}, "https://example.com/chart", nil)
			var unsafe unsafeReplayBatch
			if err == nil || !errors.As(err, &unsafe) || unsafe.reason != test.reason {
				t.Fatalf("decodeVerifiedBatch() error = %#v, want reason %q", err, test.reason)
			}
		})
	}
}

func TestDecodeVerifiedBatchRejectsUntrustedSessionLocation(t *testing.T) {
	body := `[
		{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0}}}
	]`
	_, err := decodeVerifiedBatch(
		strings.NewReader(body),
		Batch{Index: 1, SizeBytes: int64(len(body)), EventsCount: 1},
		"https://user:pass@example.com/accounts?token=secret",
		nil,
	)
	var unsafe unsafeReplayBatch
	if err == nil || !errors.As(err, &unsafe) ||
		unsafe.reason != "invalid_session_page" {
		t.Fatalf("decodeVerifiedBatch() error = %#v, want invalid_session_page", err)
	}
}

func TestDecodeVerifiedBatchClassifiesRecognizedMediaSchemaDrift(t *testing.T) {
	body := `[{"type":3,"timestamp":1784800000000,"data":{"source":7,"type":0,"id":2,"currentTime":12.5,"volume":0.5,"muted":true,"playbackRate":1,"loop":true,"futureMediaState":false}}]`
	_, err := decodeVerifiedBatch(
		strings.NewReader(body),
		Batch{Index: 1, SizeBytes: int64(len(body)), EventsCount: 1},
		"https://example.com/chart",
		nil,
	)
	var violation unsafeReplayBatch
	if err == nil || !errors.As(err, &violation) ||
		violation.failureCode != FailureContractViolation ||
		violation.violationCode != ViolationMediaFieldsNotSupported ||
		violation.eventType != 3 || violation.incrementalSource != 7 {
		t.Fatalf("decodeVerifiedBatch() violation = %#v, error = %v", violation, err)
	}
}

func TestDecodeVerifiedBatchTreatsArbitraryMediaTextAsUnsafe(t *testing.T) {
	body := `[{"type":3,"timestamp":1784800000000,"data":{"source":7,"type":0,"id":2,"playbackRate":1,"loop":true,"futureMediaState":"person@example.com"}}]`
	_, err := decodeVerifiedBatch(
		strings.NewReader(body),
		Batch{Index: 1, SizeBytes: int64(len(body)), EventsCount: 1},
		"https://example.com/chart",
		nil,
	)
	var unsafe unsafeReplayBatch
	if err == nil || !errors.As(err, &unsafe) || unsafe.code() != FailureUnsafePayload ||
		unsafe.violationCode != "" {
		t.Fatalf("decodeVerifiedBatch() error = %#v, want unsafe payload", err)
	}
}

func TestValidRRWebEventEnforcesAnonymousReplayContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		event string
		valid bool
	}{
		{
			name:  "lifecycle",
			event: `{"type":0,"timestamp":1784800000000,"data":{}}`,
			valid: true,
		},
		{
			name: "masked snapshot with explicit safe text",
			event: `{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0,"id":1,"childNodes":[` +
				`{"type":2,"id":2,"tagName":"div","attributes":{"style":"display:grid;width:320px;background-color:rgb(255, 255, 255)"},` +
				`"childNodes":[{"type":3,"id":3,"textContent":"**** ***"}]},` +
				`{"type":2,"id":4,"tagName":"span","attributes":{"data-fidibeki-safe-text":""},` +
				`"childNodes":[{"type":3,"id":5,"textContent":"Powered by Fidibeki"}]}]}}}`,
			valid: true,
		},
		{
			name:  "canonical metadata",
			event: `{"type":4,"timestamp":1784800000000,"data":{"href":"https://example.com/chart","width":1280,"height":720}}`,
			valid: true,
		},
		{
			name:  "canonical navigation",
			event: `{"type":5,"timestamp":1784800000000,"data":{"tag":"navigation","payload":{"url":"https://example.com/chart"}}}`,
			valid: true,
		},
		{
			name: "masked mutation",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":0,` +
				`"adds":[{"parentId":1,"nextId":null,"node":{"type":2,"id":2,"tagName":"div",` +
				`"attributes":{"data-fidibeki-safe-text":"","style":"display:flex;column-gap:10px"}}}],` +
				`"removes":[{"parentId":1,"id":3}],` +
				`"texts":[{"id":4,"value":"****"}],` +
				`"attributes":[{"id":2,"attributes":{"data-fidibeki-safe-text":null,"style":"display:grid"}}],` +
				`"isAttachIframe":false}}`,
			valid: true,
		},
		{
			name: "shadow dom removal",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":0,` +
				`"removes":[{"parentId":1,"id":3,"isShadow":true}]}}`,
			valid: true,
		},
		{
			name: "cleared text mutation",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":0,` +
				`"texts":[{"id":4,"value":null}]}}`,
			valid: true,
		},
		{
			name: "shadow text node added under shadow root",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":0,` +
				`"adds":[{"parentId":160,"nextId":null,"node":{"type":3,"id":163,"textContent":"****","isShadow":true}}]}}`,
			valid: true,
		},
		{
			name: "non-boolean shadow marker",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":0,` +
				`"removes":[{"parentId":1,"id":3,"isShadow":"yes"}]}}`,
		},
		{
			name: "non-boolean shadow marker on text node",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":0,` +
				`"adds":[{"parentId":160,"nextId":null,"node":{"type":3,"id":163,"textContent":"****","isShadow":"yes"}}]}}`,
		},
		{
			name: "raw text mutation",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":0,` +
				`"texts":[{"id":4,"value":"jane@example.com"}]}}`,
		},
		{
			name:  "mouse movement",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":1,"positions":[{"x":10,"y":20,"id":2,"timeOffset":5}]}}`,
			valid: true,
		},
		{
			name:  "mouse interaction",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":2,"type":0,"id":2,"x":10,"y":20,"pointerType":"mouse"}}`,
			valid: true,
		},
		{
			name:  "scroll position",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":3,"id":2,"x":10,"y":20}}`,
			valid: true,
		},
		{
			name:  "viewport resize",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":4,"width":1280,"height":720}}`,
			valid: true,
		},
		{
			name:  "masked input",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":5,"id":2,"text":"****","isChecked":true,"userTriggered":false}}`,
			valid: true,
		},
		{
			name:  "masked input from an unserialized blocked element",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":5,"id":-1,"text":"****","isChecked":false}}`,
			valid: true,
		},
		{
			name:  "input with impossible node id",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":5,"id":-2,"text":"****"}}`,
		},
		{
			name:  "media interaction",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":7,"type":0,"id":2,"currentTime":12.5,"volume":0.5,"muted":false}}`,
			valid: true,
		},
		{
			name:  "rrweb media playback state",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":7,"type":0,"id":2,"currentTime":12.5,"volume":0.5,"muted":true,"playbackRate":1,"loop":true}}`,
			valid: true,
		},
		{
			name:  "media playback rate must be numeric",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":7,"type":0,"id":2,"playbackRate":"1","loop":true}}`,
		},
		{
			name:  "media loop must be boolean",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":7,"type":0,"id":2,"playbackRate":1,"loop":"yes"}}`,
		},
		{
			name:  "selection",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":14,"ranges":[{"start":2,"startOffset":0,"end":2,"endOffset":4}]}}`,
			valid: true,
		},
		{
			name:  "different canonical metadata path",
			event: `{"type":4,"timestamp":1784800000000,"data":{"href":"https://example.com/users/jane-private"}}`,
		},
		{
			name:  "different canonical navigation path",
			event: `{"type":5,"timestamp":1784800000000,"data":{"tag":"navigation","payload":{"url":"https://example.com/users/jane-private"}}}`,
		},
		{
			name:  "raw visible text",
			event: `{"type":2,"timestamp":1784800000000,"data":{"node":{"type":3,"id":1,"textContent":"jane@example.com"}}}`,
		},
		{
			name:  "identity attribute",
			event: `{"type":2,"timestamp":1784800000000,"data":{"node":{"type":2,"id":1,"tagName":"img","attributes":{"alt":"Jane Doe"}}}}`,
		},
		{
			name:  "unsafe presentation URL",
			event: `{"type":2,"timestamp":1784800000000,"data":{"node":{"type":2,"id":1,"tagName":"div","attributes":{"style":"background-color:url(https://example.com/jane)"}}}}`,
		},
		{
			name:  "invalid tag name",
			event: `{"type":2,"timestamp":1784800000000,"data":{"node":{"type":2,"id":1,"tagName":"-img","attributes":{}}}}`,
		},
		{
			name:  "raw input",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":5,"id":1,"text":"jane@example.com"}}`,
		},
		{
			name:  "canvas",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":9,"id":1,"commands":[{"property":"drawImage"}]}}`,
		},
		{
			name:  "stylesheet text",
			event: `{"type":3,"timestamp":1784800000000,"data":{"source":8,"id":1,"adds":[{"rule":"body{content:'Jane'}"}]}}`,
		},
		{
			name:  "uncanonical metadata",
			event: `{"type":4,"timestamp":1784800000000,"data":{"href":"https://user:pass@example.com/chart/?token=secret#private"}}`,
		},
		{
			name:  "unknown custom event",
			event: `{"type":5,"timestamp":1784800000000,"data":{"tag":"identity","payload":{"email":"jane@example.com"}}}`,
		},
		{
			name:  "plugin event",
			event: `{"type":6,"timestamp":1784800000000,"data":{"plugin":"identity","payload":"jane@example.com"}}`,
		},
		{
			name:  "unknown top-level field",
			event: `{"type":0,"timestamp":1784800000000,"data":{},"reporter":"jane@example.com"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validRRWebEvent(
				json.RawMessage(test.event),
				"https://example.com/chart",
			); got != test.valid {
				t.Fatalf("validRRWebEvent() = %t, want %t for %s", got, test.valid, test.event)
			}
		})
	}
}

func TestBatchVerificationProcessorRejectsIdentityBearingBatch(t *testing.T) {
	body := []byte(`[{"type":2,"timestamp":1784800000000,"data":{"node":` +
		`{"type":2,"id":1,"tagName":"img","attributes":{"src":"https://cdn.example/avatar/jane@example.com"}}}}]`)
	store := &batchVerificationStoreStub{work: BatchVerificationWork{Batch: Batch{
		ID: "batch", TenantID: "tenant", ProjectID: "project", SessionID: "session",
		Index: 0, StorageKey: "batch.json", EventsCount: 1, SizeBytes: int64(len(body)),
		SafetyStatus: BatchSafetyQuarantined, Generation: 1,
	}, PageURL: "https://example.com/chart"}}
	storage := &batchVerificationStorageStub{body: body}
	processor := BatchVerificationProcessor{Store: store, Storage: storage}
	task := asynq.NewTask(TaskVerifyBatch, []byte(
		`{"tenant_id":"tenant","batch_id":"batch","generation":1}`,
	))
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if store.rejectedReason != "invalid_event" || store.safeAt != nil || !storage.removed {
		t.Fatalf(
			"identity batch state = rejected %q safe %v removed %t",
			store.rejectedReason, store.safeAt, storage.removed,
		)
	}
}

func TestBatchVerificationProcessorPersistsContractFailureClassification(t *testing.T) {
	body := []byte(`[{"type":3,"timestamp":1784800000000,"data":{"source":7,"type":0,"id":2,"playbackRate":1,"loop":true,"futureMediaState":false}}]`)
	store := &batchVerificationStoreStub{work: BatchVerificationWork{Batch: Batch{
		ID: "batch", TenantID: "tenant", ProjectID: "project", SessionID: "session",
		Index: 0, StorageKey: "batch.json", EventsCount: 1, SizeBytes: int64(len(body)),
		SafetyStatus: BatchSafetyQuarantined, Generation: 1,
	}, PageURL: "https://example.com/chart", RRWebVersion: "2.1.1"}}
	storage := &batchVerificationStorageStub{body: body}
	reporter := &failureReporterStub{}
	processor := BatchVerificationProcessor{Store: store, Storage: storage, Reporter: reporter}
	task := asynq.NewTask(TaskVerifyBatch, []byte(
		`{"tenant_id":"tenant","batch_id":"batch","generation":1}`,
	))
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if store.rejectedCode != FailureContractViolation ||
		store.rejectedReason != string(ViolationMediaFieldsNotSupported) || !storage.removed {
		t.Fatalf(
			"contract rejection = code %q reason %q removed %t",
			store.rejectedCode, store.rejectedReason, storage.removed,
		)
	}
	if len(reporter.reports) != 1 ||
		reporter.reports[0].Operation != FailureOperationVerifyBatch ||
		reporter.reports[0].Code != FailureContractViolation ||
		reporter.reports[0].Violation != ViolationMediaFieldsNotSupported ||
		reporter.reports[0].RRWebVersion != "2.1.1" ||
		reporter.reports[0].EventType != 3 || reporter.reports[0].IncrementalSource != 7 {
		t.Fatalf("contract telemetry = %#v", reporter.reports)
	}
}

func TestBatchVerificationProcessorPromotesOnlyValidBatches(t *testing.T) {
	body := []byte(`[{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0}}}]`)
	store := &batchVerificationStoreStub{work: BatchVerificationWork{Batch: Batch{
		ID: "batch", TenantID: "tenant", ProjectID: "project", SessionID: "session",
		Index: 0, StorageKey: "batch.json", EventsCount: 1, SizeBytes: int64(len(body)),
		SafetyStatus: BatchSafetyQuarantined, Generation: 2, VerificationTries: 1,
		UploadExpiresAt: time.Date(2026, 7, 23, 10, 10, 0, 0, time.UTC),
	}, PageURL: "https://example.com/chart"}}
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
	}, PageURL: "https://example.com/chart"}}
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
	}, PageURL: "https://example.com/chart"}}
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
	}, PageURL: "https://example.com/chart"}}
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
	rejectedCode        FailureCode
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
	code FailureCode,
	reason string,
	_ time.Time,
) error {
	store.rejectedCode = code
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

type failureReporterStub struct {
	reports []FailureReport
}

func (reporter *failureReporterStub) ReportReplayFailure(_ context.Context, report FailureReport) {
	reporter.reports = append(reporter.reports, report)
}

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
