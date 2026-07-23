package feedback

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/hibiken/asynq"
)

func TestSignatureScanner(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		kind        AttachmentKind
		contentType string
		body        []byte
		declared    int64
		wantType    string
		wantReason  string
	}{
		{name: "png", kind: AttachmentScreenshot, contentType: "image/png",
			body:     append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{1}, 600)...),
			wantType: "image/png"},
		{name: "jpeg", kind: AttachmentScreenshot, contentType: "image/jpeg",
			body: []byte{0xff, 0xd8, 0xff, 0xdb}, wantType: "image/jpeg"},
		{name: "webp", kind: AttachmentScreenshot, contentType: "image/webp",
			body: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}, wantType: "image/webp"},
		{name: "webm", kind: AttachmentVideo, contentType: "video/webm",
			body: []byte{0x1a, 0x45, 0xdf, 0xa3}, wantType: "video/webm"},
		{name: "ogg", kind: AttachmentAudio, contentType: "audio/ogg",
			body: []byte("OggS"), wantType: "audio/ogg"},
		{name: "wav", kind: AttachmentAudio, contentType: "audio/wav",
			body: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E'}, wantType: "audio/wav"},
		{name: "id3 mp3", kind: AttachmentAudio, contentType: "audio/mpeg",
			body: []byte("ID3media"), wantType: "audio/mpeg"},
		{name: "frame mp3", kind: AttachmentAudio, contentType: "audio/mpeg",
			body: []byte{0xff, 0xe3, 0, 0}, wantType: "audio/mpeg"},
		{name: "audio mp4", kind: AttachmentAudio, contentType: "audio/mp4",
			body: []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'}, wantType: "video/mp4"},
		{name: "replay gzip", kind: AttachmentReplay, contentType: "application/x-gzip",
			body: []byte{0x1f, 0x8b, 0, 0}, wantType: "application/gzip"},
		{name: "replay json", kind: AttachmentReplay, contentType: "application/json",
			body: []byte(`[{"type":2},{"type":3}]`), wantType: "application/json"},
		{name: "forged declaration", kind: AttachmentScreenshot, contentType: "image/png",
			body: []byte{0xff, 0xd8, 0xff, 0xdb}, wantReason: "signature_mismatch"},
		{name: "wrong kind", kind: AttachmentScreenshot, contentType: "audio/ogg",
			body: []byte("OggS"), wantReason: "kind_mismatch"},
		{name: "unknown", kind: AttachmentAudio, contentType: "audio/mpeg",
			body: []byte("not media"), wantReason: "unknown_signature"},
		{name: "malformed replay", kind: AttachmentReplay, contentType: "application/json",
			body: []byte(`{"not":"an array"}`), wantReason: "invalid_json"},
		{name: "truncated", kind: AttachmentScreenshot, contentType: "image/png",
			body: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, declared: 9,
			wantReason: "size_mismatch"},
		{name: "oversized", kind: AttachmentScreenshot, contentType: "image/png",
			body: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}, declared: 8,
			wantReason: "size_mismatch"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			size := test.declared
			if size == 0 {
				size = int64(len(test.body))
			}
			detected, err := (SignatureScanner{}).Scan(context.Background(), VerificationWork{
				Kind: test.kind, ContentType: test.contentType, SizeBytes: size,
			}, bytes.NewReader(test.body))
			if test.wantReason == "" {
				if err != nil || detected != test.wantType {
					t.Fatalf("Scan() = %q, %v, want %q, nil", detected, err, test.wantType)
				}
				return
			}
			var failure safetyFailure
			if !errors.As(err, &failure) || failure.reason != test.wantReason ||
				err.Error() != test.wantReason {
				t.Fatalf("Scan() error = %v, want safety reason %q", err, test.wantReason)
			}
		})
	}
}

func TestVerificationProcessorMarksOnlyVerifiedObjectsSafe(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 2}
	work := VerificationWork{
		VerificationTask: task, FeedbackID: "feedback", ProjectID: "project",
		Kind: AttachmentScreenshot, StorageKey: "object.png", ContentType: "image/png",
		SizeBytes: 8, SafetyStatus: AttachmentSafetyQuarantined,
	}
	store := &verificationStoreStub{work: work}
	storage := &verificationStorageStub{objects: map[string][]byte{
		work.StorageKey: {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
	}}
	ai := &verifiedAISchedulerStub{}
	processor := VerificationProcessor{
		Store: store, Storage: storage, Scanner: SignatureScanner{}, AI: ai,
		Now: func() time.Time { return now },
	}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":2}`)
	if err := processor.Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if store.safeType != "image/png" || store.safeAt != now || store.rejectedReason != "" {
		t.Fatalf("verification store state = safe %q at %v rejected %q",
			store.safeType, store.safeAt, store.rejectedReason)
	}
	if storage.removed || ai.attachmentID != task.AttachmentID || ai.feedbackID != work.FeedbackID {
		t.Fatalf("verified side effects = removed %v AI %q/%q",
			storage.removed, ai.attachmentID, ai.feedbackID)
	}
}

func TestVerificationProcessorRejectsAndRemovesUnsafeObjects(t *testing.T) {
	t.Parallel()
	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 1}
	work := VerificationWork{
		VerificationTask: task, FeedbackID: "feedback", ProjectID: "project",
		Kind: AttachmentScreenshot, StorageKey: "forged.png", ContentType: "image/png",
		SizeBytes: 4, SafetyStatus: AttachmentSafetyQuarantined,
	}
	storage := &verificationStorageStub{objects: map[string][]byte{
		work.StorageKey: {0xff, 0xd8, 0xff, 0xdb},
	}}
	store := &verificationStoreStub{work: work, removed: func() bool { return storage.removed }}
	processor := VerificationProcessor{Store: store, Storage: storage, Scanner: SignatureScanner{}}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`)
	if err := processor.Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if !storage.removed || store.rejectedReason != "signature_mismatch" ||
		store.removedBeforeReject || !store.cleanupCompleted {
		t.Fatalf("rejection state = removed %v reason %q removed-before-reject %v cleanup %v",
			storage.removed, store.rejectedReason, store.removedBeforeReject, store.cleanupCompleted)
	}
	if store.safeType != "" {
		t.Fatalf("unsafe attachment marked safe as %q", store.safeType)
	}
}

func TestVerificationProcessorDoesNotSilentlyTrustScannerFailures(t *testing.T) {
	t.Parallel()
	work := VerificationWork{
		VerificationTask: VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 1},
		StorageKey:       "object", SizeBytes: 1, SafetyStatus: AttachmentSafetyQuarantined,
	}
	store := &verificationStoreStub{work: work}
	storage := &verificationStorageStub{objects: map[string][]byte{"object": {1}}}
	processor := VerificationProcessor{
		Store: store, Storage: storage,
		Scanner: scannerFunc(func(context.Context, VerificationWork, io.Reader) (string, error) {
			return "", errors.New("scanner unavailable")
		}),
	}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`)
	err := processor.Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload))
	if err == nil || store.safeType != "" || store.rejectedReason != "" || storage.removed {
		t.Fatalf("operational failure = %v, safe %q, rejected %q, removed %v",
			err, store.safeType, store.rejectedReason, storage.removed)
	}
}

func TestVerificationProcessorResumesRejectedObjectCleanup(t *testing.T) {
	t.Parallel()
	work := VerificationWork{
		VerificationTask: VerificationTask{
			TenantID: "tenant", AttachmentID: "attachment", Generation: 1,
		},
		StorageKey: "forged", Kind: AttachmentScreenshot, ContentType: "image/png",
		SizeBytes: 4, SafetyStatus: AttachmentSafetyQuarantined,
	}
	store := &verificationStoreStub{work: work}
	storage := &verificationStorageStub{
		objects:   map[string][]byte{"forged": {0xff, 0xd8, 0xff, 0xdb}},
		removeErr: errors.New("storage unavailable"),
	}
	processor := VerificationProcessor{Store: store, Storage: storage, Scanner: SignatureScanner{}}
	task := asynq.NewTask(
		TaskAttachmentVerify,
		[]byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`),
	)
	if err := processor.Handler()(context.Background(), task); err == nil {
		t.Fatal("first cleanup attempt error = nil")
	}
	if store.work.SafetyStatus != AttachmentSafetyRejected || storage.removed {
		t.Fatalf("first cleanup state = %q removed %v", store.work.SafetyStatus, storage.removed)
	}
	storage.removeErr = nil
	if err := processor.Handler()(context.Background(), task); err != nil {
		t.Fatalf("resumed cleanup error = %v", err)
	}
	if !storage.removed || !store.cleanupCompleted {
		t.Fatalf("resumed cleanup = removed %v completed %v", storage.removed, store.cleanupCompleted)
	}
}

func TestVerificationProcessorHandlesWorkerBoundaries(t *testing.T) {
	t.Parallel()

	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 1}
	payload := []byte(`{"tenant_id":"tenant","attachment_id":"attachment","generation":1}`)
	work := VerificationWork{
		VerificationTask: task, FeedbackID: "feedback", ProjectID: "project",
		Kind: AttachmentScreenshot, StorageKey: "object.png", ContentType: "image/png",
		SizeBytes: 8, SafetyStatus: AttachmentSafetyQuarantined,
	}
	if err := (VerificationProcessor{}).Handler()(
		context.Background(), asynq.NewTask(TaskAttachmentVerify, []byte(`{`)),
	); err == nil || !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("invalid payload error = %v", err)
	}
	if err := (VerificationProcessor{Store: &verificationStoreStub{
		claimErr: ErrNoVerificationWork,
	}}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("no work error = %v", err)
	}
	claimFailure := errors.New("database unavailable")
	if err := (VerificationProcessor{Store: &verificationStoreStub{
		claimErr: claimFailure,
	}}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); !errors.Is(err, claimFailure) {
		t.Fatalf("claim failure = %v", err)
	}
	if err := (VerificationProcessor{
		Store: &verificationStoreStub{work: work},
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err == nil {
		t.Fatal("missing scanner error = nil")
	}
	openFailure := errors.New("storage unavailable")
	if err := (VerificationProcessor{
		Store:   &verificationStoreStub{work: work},
		Storage: &verificationStorageStub{openErr: openFailure},
		Scanner: SignatureScanner{},
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); !errors.Is(err, openFailure) {
		t.Fatalf("open failure = %v", err)
	}

	aiFailure := errors.New("AI queue unavailable")
	ai := &verifiedAISchedulerStub{err: aiFailure}
	if err := (VerificationProcessor{
		Store: &verificationStoreStub{work: work},
		Storage: &verificationStorageStub{objects: map[string][]byte{
			work.StorageKey: {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		}},
		Scanner: SignatureScanner{}, AI: ai,
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("optional AI failure = %v", err)
	}
	if ai.attachmentID != task.AttachmentID {
		t.Fatalf("AI scheduler attachment = %q", ai.attachmentID)
	}

	diagnosticFailure := errors.New("diagnostics unavailable")
	rejectedStore := &verificationStoreStub{work: work}
	if err := (VerificationProcessor{
		Store: rejectedStore,
		Storage: &verificationStorageStub{objects: map[string][]byte{
			work.StorageKey: {0xff, 0xd8, 0xff, 0xdb, 0, 0, 0, 0},
		}},
		Scanner: SignatureScanner{},
		Diagnostics: abuseDiagnosticRecorderFunc(func(
			context.Context,
			abuse.DiagnosticEvent,
		) error {
			return diagnosticFailure
		}),
	}).Handler()(context.Background(), asynq.NewTask(TaskAttachmentVerify, payload)); err != nil {
		t.Fatalf("optional diagnostic failure = %v", err)
	}
	if rejectedStore.rejectedReason != "signature_mismatch" {
		t.Fatalf("rejected reason = %q", rejectedStore.rejectedReason)
	}
}

type verificationStoreStub struct {
	work                VerificationWork
	claimErr            error
	safeType            string
	safeAt              time.Time
	rejectedReason      string
	removed             func() bool
	removedBeforeReject bool
	cleanupCompleted    bool
}

func (store *verificationStoreStub) Claim(context.Context, VerificationTask) (VerificationWork, error) {
	return store.work, store.claimErr
}

func (store *verificationStoreStub) MarkSafe(
	_ context.Context,
	_ VerificationWork,
	detected string,
	at time.Time,
) error {
	store.safeType, store.safeAt = detected, at
	return nil
}

func (store *verificationStoreStub) MarkRejected(
	_ context.Context,
	_ VerificationWork,
	reason string,
	_ *string,
	_ time.Time,
) error {
	store.rejectedReason = reason
	store.work.SafetyStatus = AttachmentSafetyRejected
	if store.removed != nil {
		store.removedBeforeReject = store.removed()
	}
	return nil
}

func (store *verificationStoreStub) CompleteRejectedCleanup(
	context.Context,
	VerificationWork,
) error {
	store.cleanupCompleted = true
	return nil
}

type verificationStorageStub struct {
	objects   map[string][]byte
	openErr   error
	removeErr error
	removed   bool
}

func (storage *verificationStorageStub) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	if storage.openErr != nil {
		return nil, storage.openErr
	}
	return io.NopCloser(bytes.NewReader(storage.objects[key])), nil
}

func (storage *verificationStorageStub) RemoveObject(_ context.Context, key string) error {
	if storage.removeErr != nil {
		return storage.removeErr
	}
	delete(storage.objects, key)
	storage.removed = true
	return nil
}

type verifiedAISchedulerStub struct {
	attachmentID string
	feedbackID   string
	err          error
}

func (scheduler *verifiedAISchedulerStub) ScheduleAttachment(
	_ context.Context,
	attachmentID string,
	feedbackID string,
) error {
	scheduler.attachmentID, scheduler.feedbackID = attachmentID, feedbackID
	return scheduler.err
}

type scannerFunc func(context.Context, VerificationWork, io.Reader) (string, error)

func (scanner scannerFunc) Scan(
	ctx context.Context,
	work VerificationWork,
	reader io.Reader,
) (string, error) {
	return scanner(ctx, work, reader)
}

type abuseDiagnosticRecorderFunc func(context.Context, abuse.DiagnosticEvent) error

func (record abuseDiagnosticRecorderFunc) Record(
	ctx context.Context,
	event abuse.DiagnosticEvent,
) error {
	return record(ctx, event)
}
