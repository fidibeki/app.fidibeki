package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/hibiken/asynq"
	"google.golang.org/genai"
)

func TestResultNormalizeAndGeminiErrorClassification(t *testing.T) {
	t.Parallel()
	result := Result{
		Transcription: "  hello  ", CleanedText: "  clear text ", SentimentScore: 0.5,
		ModerationFlags: []string{" Profanity ", "profanity", "", "PII"},
		SuggestedTitle:  "  Checkout validation fails  ", Actionability: "actionable",
		Category: "  checkout  ", Priority: "high",
	}
	if err := result.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if result.Transcription != "hello" || result.CleanedText != "clear text" ||
		fmt.Sprint(result.ModerationFlags) != "[profanity pii]" ||
		result.SuggestedTitle != "Checkout validation fails" || result.Category != "checkout" {
		t.Errorf("normalized result = %#v", result)
	}
	if err := (&Result{SentimentScore: 1.1}).Normalize(); err == nil {
		t.Fatal("Normalize(out-of-range sentiment) error = nil")
	}
	if err := (&Result{Actionability: "maybe", Priority: "immediate"}).Normalize(); err == nil {
		t.Fatal("Normalize(invalid recommendations) error = nil")
	}
	if !IsPermanent(classifyGeminiError(genai.APIError{Code: 400, Message: "bad media"})) {
		t.Error("400 API error was not permanent")
	}
	if IsPermanent(classifyGeminiError(genai.APIError{Code: 429, Message: "quota"})) {
		t.Error("429 API error was permanent")
	}
	if !IsPermanent(classifyGeminiError(errors.New("content policy blocked"))) {
		t.Error("policy error was not permanent")
	}
	if attachmentPrompt("video") == attachmentPrompt("audio") || attachmentPrompt("screenshot") == "" {
		t.Error("attachment prompts are not modality-specific")
	}
	if attachmentPrompt("unknown") == "" || geminiStatusCode(errors.New("not an API error")) != 0 {
		t.Error("default prompt or non-API status classification is invalid")
	}
	if IsPermanent(classifyGeminiError(context.DeadlineExceeded)) {
		t.Error("deadline error was permanent")
	}
}

func TestProviderDiagnosticReasonUsesStableOperationalCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		failure error
		want    string
	}{
		{name: "unauthenticated", failure: genai.APIError{Code: 401}, want: "provider_unauthenticated"},
		{name: "forbidden", failure: genai.APIError{Code: 403}, want: "provider_forbidden"},
		{name: "not found", failure: genai.APIError{Code: 404}, want: "provider_not_found"},
		{name: "request timeout", failure: genai.APIError{Code: 408}, want: "provider_timeout"},
		{name: "gateway timeout", failure: genai.APIError{Code: 504}, want: "provider_timeout"},
		{name: "conflict", failure: genai.APIError{Code: 409}, want: "provider_conflict"},
		{name: "rate limited", failure: genai.APIError{Code: 429}, want: "provider_rate_limited"},
		{name: "unavailable", failure: genai.APIError{Code: 503}, want: "provider_unavailable"},
		{name: "rejected", failure: genai.APIError{Code: 422}, want: "provider_rejected"},
		{name: "provider error", failure: genai.APIError{Code: 302}, want: "provider_error"},
		{name: "analyzer unavailable", failure: ErrUnavailable, want: "analyzer_unavailable"},
		{name: "deadline", failure: context.DeadlineExceeded, want: "deadline_exceeded"},
		{name: "canceled", failure: context.Canceled, want: "canceled"},
		{name: "network timeout", failure: &net.DNSError{
			Err: "timeout", IsTimeout: true,
		}, want: "network_timeout"},
		{name: "network", failure: &net.DNSError{Err: "unavailable"}, want: "network_error"},
		{name: "permanent", failure: permanent(errors.New("unsupported")), want: "permanent_failure"},
		{name: "internal", failure: errors.New("database unavailable"), want: "internal_error"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := providerDiagnosticReason(test.failure); got != test.want {
				t.Fatalf("providerDiagnosticReason() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGeminiConstructionAndFileTerminalStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ledger := &fakeGeminiLedger{}
	if _, err := NewGeminiAnalyzer(ctx, "", "gemini-2.5-flash", ledger, nil); !errors.Is(err, ErrUnavailable) {
		t.Errorf("NewGeminiAnalyzer(empty key) error = %v", err)
	}
	if _, err := NewGeminiAnalyzer(ctx, "test-key", "", ledger, nil); err == nil {
		t.Error("NewGeminiAnalyzer(empty model) error = nil")
	}
	if _, err := NewGeminiAnalyzer(ctx, "test-key", "gemini-2.5-flash", nil, nil); err == nil {
		t.Error("NewGeminiAnalyzer(nil ledger) error = nil")
	}
	configured, err := NewGeminiAnalyzer(ctx, "test-key", "gemini-2.5-flash", ledger, nil)
	if err != nil || configured.api == nil || configured.model != "gemini-2.5-flash" {
		t.Errorf("NewGeminiAnalyzer(valid) = %#v, %v", configured, err)
	}
	analyzer := &GeminiAnalyzer{pollInterval: time.Millisecond, fileTimeout: time.Millisecond}
	active := &genai.File{Name: "files/active", State: genai.FileStateActive}
	if got, err := analyzer.waitForFile(ctx, active); err != nil || got != active {
		t.Errorf("waitForFile(active) = %#v, %v", got, err)
	}
	_, err = analyzer.waitForFile(ctx, &genai.File{Name: "files/failed", State: genai.FileStateFailed})
	if !IsPermanent(err) {
		t.Errorf("waitForFile(failed) error = %v, want permanent", err)
	}
	if permanentFailure, err := analyzer.deleteFileWithRetry(""); err != nil || permanentFailure {
		t.Errorf("deleteFileWithRetry(empty) = %t, %v", permanentFailure, err)
	}
	if _, err := (UnavailableAnalyzer{}).AnalyzeText(ctx, "text"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UnavailableAnalyzer.AnalyzeText() error = %v", err)
	}
	if _, err := (UnavailableAnalyzer{}).AnalyzeAttachment(ctx, AttachmentWork{}, strings.NewReader("x")); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UnavailableAnalyzer.AnalyzeAttachment() error = %v", err)
	}
}

func TestGeminiAnalyzerProcessesTextAndMediaLifecycle(t *testing.T) {
	t.Parallel()
	api := &fakeGeminiAPI{
		uploadFile: &genai.File{Name: "files/media", State: genai.FileStateProcessing},
		getFiles:   []*genai.File{{Name: "files/media", State: genai.FileStateActive}},
		response: structuredGeminiResponse(`{
			"transcription":"spoken words","cleaned_text":"clear feedback",
			"sentiment_score":-0.25,"moderation_flags":[" Profanity ","profanity"]
		}`),
	}
	analyzer := &GeminiAnalyzer{
		api: api, model: "gemini-2.5-flash", pollInterval: time.Millisecond, fileTimeout: time.Second,
		fileLedger: &fakeGeminiLedger{},
	}
	textResult, err := analyzer.AnalyzeText(context.Background(), "Um, clear feedback")
	if err != nil {
		t.Fatalf("AnalyzeText() error = %v", err)
	}
	if textResult.CleanedText != "clear feedback" || fmt.Sprint(textResult.ModerationFlags) != "[profanity]" {
		t.Errorf("text result = %#v", textResult)
	}
	mediaResult, err := analyzer.AnalyzeAttachment(context.Background(), AttachmentWork{
		AttachmentID: "attachment-id", Kind: "video", ContentType: "video/mp4",
	}, strings.NewReader("video"))
	if err != nil {
		t.Fatalf("AnalyzeAttachment() error = %v", err)
	}
	if mediaResult.Transcription != "spoken words" || api.deletedName != "files/media" {
		t.Errorf("media result = %#v, deleted = %q", mediaResult, api.deletedName)
	}
	if api.uploadConfig == nil || api.uploadConfig.MIMEType != "video/mp4" ||
		api.uploadConfig.DisplayName != "fidibeki-feedback-attachment" ||
		strings.Contains(api.uploadConfig.DisplayName, "attachment-id") ||
		api.generatedModel != "gemini-2.5-flash" || api.generatedConfig.ResponseMIMEType != "application/json" {
		t.Errorf("Gemini calls = upload %#v model %q config %#v",
			api.uploadConfig, api.generatedModel, api.generatedConfig)
	}
}

func TestGeminiAnalyzerMinimizesReporterIdentifiersBeforeProviderRequest(t *testing.T) {
	t.Parallel()
	api := &fakeGeminiAPI{response: structuredGeminiResponse(
		`{"transcription":"","cleaned_text":"[REDACTED]","sentiment_score":0,"moderation_flags":[]}`,
	)}
	analyzer := &GeminiAnalyzer{api: api, model: "gemini-test", fileLedger: &fakeGeminiLedger{}}
	sentinel := "private.person@example.com"
	result, err := analyzer.AnalyzeText(context.Background(),
		"My name is Private Person and my email is "+sentinel,
	)
	if err != nil {
		t.Fatalf("AnalyzeText() error = %v", err)
	}
	var requestText strings.Builder
	for _, content := range api.generatedContents {
		for _, part := range content.Parts {
			requestText.WriteString(part.Text)
		}
	}
	if strings.Contains(requestText.String(), sentinel) ||
		strings.Contains(requestText.String(), "Private Person") {
		t.Fatalf("provider request leaked reporter identity: %s", requestText.String())
	}
	if !strings.Contains(requestText.String(), RedactionLiteral) {
		t.Fatalf("provider request did not preserve redaction marker: %s", requestText.String())
	}
	if result.Redactions["email"] == 0 || result.Redactions["name"] == 0 {
		t.Fatalf("provider input redaction manifest = %#v", result.Redactions)
	}
}

func TestGeminiAnalyzerClassifiesServiceAndResponseFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := []struct {
		name      string
		api       *fakeGeminiAPI
		media     bool
		permanent bool
	}{
		{name: "malformed media", media: true, permanent: true,
			api: &fakeGeminiAPI{uploadErr: genai.APIError{Code: 400, Message: "bad media"}}},
		{name: "quota", api: &fakeGeminiAPI{generateErr: genai.APIError{Code: 429, Message: "quota"}}},
		{name: "policy block", permanent: true, api: &fakeGeminiAPI{response: &genai.GenerateContentResponse{
			PromptFeedback: &genai.GenerateContentResponsePromptFeedback{BlockReason: "SAFETY"},
		}}},
		{name: "candidate safety", permanent: true, api: &fakeGeminiAPI{response: finishReasonResponse(genai.FinishReasonSafety)}},
		{name: "candidate blocklist", permanent: true, api: &fakeGeminiAPI{response: finishReasonResponse(genai.FinishReasonBlocklist)}},
		{name: "candidate prohibited content", permanent: true, api: &fakeGeminiAPI{response: finishReasonResponse(genai.FinishReasonProhibitedContent)}},
		{name: "candidate sensitive PII", permanent: true, api: &fakeGeminiAPI{response: finishReasonResponse(genai.FinishReasonSPII)}},
		{name: "candidate recitation", permanent: true, api: &fakeGeminiAPI{response: finishReasonResponse(genai.FinishReasonRecitation)}},
		{name: "candidate language", permanent: true, api: &fakeGeminiAPI{response: finishReasonResponse(genai.FinishReasonLanguage)}},
		{name: "empty response", permanent: true, api: &fakeGeminiAPI{response: &genai.GenerateContentResponse{}}},
		{name: "invalid JSON", permanent: true, api: &fakeGeminiAPI{response: structuredGeminiResponse(`not-json`)}},
		{name: "invalid score", permanent: true, api: &fakeGeminiAPI{response: structuredGeminiResponse(
			`{"transcription":"","cleaned_text":"x","sentiment_score":2,"moderation_flags":[]}`,
		)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			analyzer := &GeminiAnalyzer{
				api: tt.api, model: "gemini-2.5-flash",
				pollInterval: time.Millisecond, fileTimeout: 10 * time.Millisecond,
				fileLedger: &fakeGeminiLedger{},
			}
			var err error
			if tt.media {
				_, err = analyzer.AnalyzeAttachment(ctx, AttachmentWork{
					AttachmentID: "id", Kind: "audio", ContentType: "audio/webm",
				}, strings.NewReader("audio"))
			} else {
				_, err = analyzer.AnalyzeText(ctx, "feedback")
			}
			if err == nil || IsPermanent(err) != tt.permanent {
				t.Errorf("analysis error = %v, permanent = %t, want %t", err, IsPermanent(err), tt.permanent)
			}
		})
	}

	pollAPI := &fakeGeminiAPI{
		uploadFile: &genai.File{Name: "files/media", State: genai.FileStateProcessing},
		getErr:     genai.APIError{Code: 500, Message: "unavailable"},
	}
	pollAnalyzer := &GeminiAnalyzer{
		api: pollAPI, model: "gemini-2.5-flash", pollInterval: time.Millisecond, fileTimeout: time.Second,
		fileLedger: &fakeGeminiLedger{},
	}
	if _, err := pollAnalyzer.AnalyzeAttachment(ctx, AttachmentWork{
		AttachmentID: "id", ContentType: "audio/webm",
	}, strings.NewReader("audio")); err == nil || IsPermanent(err) {
		t.Errorf("poll error = %v", err)
	}
	timeoutAPI := &fakeGeminiAPI{
		uploadFile: &genai.File{Name: "files/media", State: genai.FileStateProcessing},
	}
	timeoutAnalyzer := &GeminiAnalyzer{
		api: timeoutAPI, model: "gemini-2.5-flash",
		pollInterval: time.Millisecond, fileTimeout: 3 * time.Millisecond,
		fileLedger: &fakeGeminiLedger{},
	}
	if _, err := timeoutAnalyzer.AnalyzeAttachment(ctx, AttachmentWork{
		AttachmentID: "id", ContentType: "audio/webm",
	}, strings.NewReader("audio")); err == nil || IsPermanent(err) {
		t.Errorf("file timeout error = %v", err)
	}
}

func TestGeminiFileDeletionRetriesAndPersistsFailures(t *testing.T) {
	t.Parallel()
	response := structuredGeminiResponse(
		`{"transcription":"words","cleaned_text":"words","sentiment_score":0,"moderation_flags":[]}`,
	)
	successAPI := &fakeGeminiAPI{
		uploadFile: &genai.File{Name: "files/retry", State: genai.FileStateActive},
		response:   response,
		deleteErrors: []error{
			genai.APIError{Code: 500, Message: "unavailable"},
			genai.APIError{Code: 429, Message: "quota"},
			nil,
		},
	}
	successLedger := &fakeGeminiLedger{}
	successAnalyzer := &GeminiAnalyzer{
		api: successAPI, model: "gemini-2.5-flash", fileLedger: successLedger,
		pollInterval: time.Millisecond, fileTimeout: time.Second,
	}
	if _, err := successAnalyzer.AnalyzeAttachment(context.Background(), AttachmentWork{
		TenantID: "tenant", AttachmentID: "attachment", Kind: "audio", ContentType: "audio/webm",
	}, strings.NewReader("audio")); err != nil {
		t.Fatalf("AnalyzeAttachment(retried deletion) error = %v", err)
	}
	if successAPI.deleteCalls != 3 || !successLedger.ready || !successLedger.completed {
		t.Errorf("successful cleanup = calls %d, ledger %#v", successAPI.deleteCalls, successLedger)
	}

	failureAPI := &fakeGeminiAPI{
		uploadFile: &genai.File{Name: "files/persist", State: genai.FileStateActive},
		response:   response,
		deleteErrors: []error{
			genai.APIError{Code: 500, Message: "unavailable"},
			genai.APIError{Code: 500, Message: "unavailable"},
			genai.APIError{Code: 500, Message: "unavailable"},
		},
	}
	failureLedger := &fakeGeminiLedger{}
	failureAnalyzer := &GeminiAnalyzer{
		api: failureAPI, model: "gemini-2.5-flash", fileLedger: failureLedger,
		pollInterval: time.Millisecond, fileTimeout: time.Second,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if _, err := failureAnalyzer.AnalyzeAttachment(context.Background(), AttachmentWork{
		TenantID: "tenant", AttachmentID: "attachment", Kind: "audio", ContentType: "audio/webm",
	}, strings.NewReader("audio")); err != nil {
		t.Fatalf("AnalyzeAttachment(persisted deletion) error = %v", err)
	}
	if failureAPI.deleteCalls != 3 || failureLedger.failure == nil || failureLedger.permanent || failureLedger.completed {
		t.Errorf("persisted cleanup = calls %d, ledger %#v", failureAPI.deleteCalls, failureLedger)
	}

	untrackedAPI := &fakeGeminiAPI{
		uploadFile: &genai.File{Name: "files/untracked", State: genai.FileStateActive},
	}
	if _, err := (&GeminiAnalyzer{api: untrackedAPI}).AnalyzeAttachment(
		context.Background(), AttachmentWork{AttachmentID: "attachment", ContentType: "audio/webm"},
		strings.NewReader("audio"),
	); err == nil || untrackedAPI.deleteCalls != 1 {
		t.Errorf("AnalyzeAttachment(no ledger) = %v, delete calls %d", err, untrackedAPI.deleteCalls)
	}
	trackFailureAPI := &fakeGeminiAPI{
		uploadFile: &genai.File{Name: "files/track-failure", State: genai.FileStateActive},
	}
	if _, err := (&GeminiAnalyzer{
		api: trackFailureAPI, fileLedger: &fakeGeminiLedger{trackErr: errors.New("database unavailable")},
	}).AnalyzeAttachment(
		context.Background(), AttachmentWork{AttachmentID: "attachment", ContentType: "audio/webm"},
		strings.NewReader("audio"),
	); err == nil || trackFailureAPI.deleteCalls != 1 {
		t.Errorf("AnalyzeAttachment(track failure) = %v, delete calls %d", err, trackFailureAPI.deleteCalls)
	}
}

func TestGeminiFileDeleteHandlers(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"tenant_id":"tenant","resource_id":"tracked-file","attempt":2}`)
	task := asynq.NewTask(TaskGeminiFileDelete, payload)
	work := GeminiFileWork{
		ID: "tracked-file", TenantID: "tenant", FileName: "files/media", Attempt: 2,
	}

	successStore := &fakeGeminiFileDeleteStore{work: work}
	successAnalyzer := &GeminiAnalyzer{api: &fakeGeminiAPI{}}
	if err := successAnalyzer.FileDeleteHandler(successStore)(context.Background(), task); err != nil {
		t.Fatalf("FileDeleteHandler(success) error = %v", err)
	}
	if !successStore.completed {
		t.Error("FileDeleteHandler(success) did not complete ledger row")
	}

	transientStore := &fakeGeminiFileDeleteStore{work: work}
	transientAnalyzer := &GeminiAnalyzer{api: &fakeGeminiAPI{deleteErrors: []error{
		genai.APIError{Code: 500, Message: "unavailable"},
	}}}
	if err := transientAnalyzer.FileDeleteHandler(transientStore)(context.Background(), task); err == nil ||
		transientStore.failure == nil || transientStore.permanent {
		t.Errorf("FileDeleteHandler(transient) = %v, store %#v", err, transientStore)
	}

	permanentStore := &fakeGeminiFileDeleteStore{work: work}
	permanentAnalyzer := &GeminiAnalyzer{api: &fakeGeminiAPI{deleteErrors: []error{
		genai.APIError{Code: 403, Message: "forbidden"},
	}}}
	if err := permanentAnalyzer.FileDeleteHandler(permanentStore)(context.Background(), task); !errors.Is(err, asynq.SkipRetry) ||
		!permanentStore.permanent {
		t.Errorf("FileDeleteHandler(permanent) = %v, store %#v", err, permanentStore)
	}

	noWorkStore := &fakeGeminiFileDeleteStore{claimErr: ErrNoWork}
	if err := successAnalyzer.FileDeleteHandler(noWorkStore)(context.Background(), task); err != nil {
		t.Errorf("FileDeleteHandler(no work) error = %v", err)
	}
	claimStore := &fakeGeminiFileDeleteStore{claimErr: errors.New("database unavailable")}
	if err := successAnalyzer.FileDeleteHandler(claimStore)(context.Background(), task); err == nil {
		t.Error("FileDeleteHandler(claim failure) error = nil")
	}
	if err := successAnalyzer.FileDeleteHandler(successStore)(
		context.Background(), asynq.NewTask(TaskGeminiFileDelete, []byte(`{"tenant_id":"tenant"}`)),
	); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("FileDeleteHandler(invalid payload) error = %v", err)
	}

	unavailableStore := &fakeGeminiFileDeleteStore{work: work}
	if err := UnavailableFileDeleteHandler(unavailableStore)(context.Background(), task); !errors.Is(err, ErrUnavailable) {
		t.Errorf("UnavailableFileDeleteHandler() error = %v", err)
	}
	if err := UnavailableFileDeleteHandler(noWorkStore)(context.Background(), task); err != nil {
		t.Errorf("UnavailableFileDeleteHandler(no work) error = %v", err)
	}
	if err := UnavailableFileDeleteHandler(claimStore)(context.Background(), task); err == nil {
		t.Error("UnavailableFileDeleteHandler(claim failure) error = nil")
	}
	if err := UnavailableFileDeleteHandler(unavailableStore)(
		context.Background(), asynq.NewTask(TaskGeminiFileDelete, []byte("{")),
	); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("UnavailableFileDeleteHandler(invalid payload) error = %v", err)
	}
}

func TestQueueBuildsDeterministicTypedTasks(t *testing.T) {
	t.Parallel()
	client := &fakeTaskClient{}
	queue := NewQueue(client)
	now := time.Date(2026, 7, 21, 12, 0, 0, 123, time.UTC)
	prepared := PreparedTask{
		Kind: TaskKindAttachment, TenantID: "tenant", ResourceID: "attachment", Attempt: 2,
		EnqueuedAt: now,
	}
	if err := queue.Enqueue(context.Background(), prepared); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if client.task == nil || client.task.Type() != TaskAttachmentProcess {
		t.Fatalf("task = %#v", client.task)
	}
	var payload taskPayload
	if err := json.Unmarshal(client.task.Payload(), &payload); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if payload.TenantID != "tenant" || payload.ResourceID != "attachment" || payload.Attempt != 2 {
		t.Errorf("payload = %#v", payload)
	}
	if TaskID(prepared) != "ai:attachment:attachment:2" {
		t.Errorf("TaskID() = %q", TaskID(prepared))
	}
	textTask := prepared
	textTask.Kind, textTask.ResourceID, textTask.Generation = TaskKindText, "feedback", 3
	if TaskID(textTask) != "ai:text:feedback:3" {
		t.Errorf("text TaskID() = %q", TaskID(textTask))
	}
	cleanupTask := PreparedTask{
		Kind: TaskKindFileDelete, TenantID: "tenant", ResourceID: "file", Attempt: 4,
	}
	if err := queue.Enqueue(context.Background(), cleanupTask); err != nil {
		t.Fatalf("Enqueue(file cleanup) error = %v", err)
	}
	if client.task.Type() != TaskGeminiFileDelete || TaskID(cleanupTask) != "ai:file-delete:file:4" {
		t.Errorf("file cleanup task = %q/%q", client.task.Type(), TaskID(cleanupTask))
	}
	if err := queue.Close(); err != nil || !client.closed {
		t.Errorf("Close() = %v, closed = %t", err, client.closed)
	}
	if _, err := OpenQueue("://bad"); err == nil {
		t.Error("OpenQueue(invalid URL) error = nil")
	}
	secureQueue, err := OpenQueue("rediss://redis.fidibeki.test:6380/0")
	if err != nil {
		t.Fatalf("OpenQueue(secure URL) error = %v", err)
	}
	if err := secureQueue.Close(); err != nil {
		t.Fatalf("secure queue Close() error = %v", err)
	}
}

func TestTaskDecoderRejectsMalformedPayload(t *testing.T) {
	t.Parallel()
	for _, payload := range [][]byte{[]byte("{"), []byte(`{"tenant_id":""}`)} {
		_, err := decodeTask(asynq.NewTask(TaskTextProcess, payload))
		if !errors.Is(err, asynq.SkipRetry) {
			t.Errorf("decodeTask(%q) error = %v, want SkipRetry", payload, err)
		}
	}
	validIdentifiers := []byte(`{"tenant_id":"tenant","resource_id":"resource"}`)
	if err := (Processor{}).TextHandler()(
		context.Background(), asynq.NewTask(TaskTextProcess, validIdentifiers),
	); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("TextHandler(missing generation) error = %v", err)
	}
	if err := (Processor{}).AttachmentHandler()(
		context.Background(), asynq.NewTask(TaskAttachmentProcess, validIdentifiers),
	); !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("AttachmentHandler(missing attempt) error = %v", err)
	}
	(Processor{}).ErrorHandler().HandleError(
		context.Background(), asynq.NewTask(TaskTextProcess, validIdentifiers), errors.New("failure"),
	)
	if nullableText("  ") != nil || nullableText("value") == nil {
		t.Error("nullableText() did not normalize blank and non-blank values")
	}
}

func TestProcessorErrorHandlerLogsSafeProviderFailure(t *testing.T) {
	t.Parallel()
	const payloadSecret = "sensitive customer words"
	const providerSecret = "provider echoed customer feedback and api key"

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	task := asynq.NewTask(TaskTextProcess, []byte(`{
			"tenant_id":"tenant-1",
			"project_id":"project-1",
			"resource_id":"feedback-1",
		"generation":2,
		"text":"`+payloadSecret+`"
	}`))
	failure := errors.Join(
		permanent(genai.APIError{
			Code: 404, Message: providerSecret, Status: providerSecret,
			Details: []map[string]any{{"debug": providerSecret}},
		}),
		asynq.SkipRetry,
	)

	diagnostics := &capturingDiagnosticRecorder{}
	(Processor{Logger: logger, Diagnostics: diagnostics}).ErrorHandler().HandleError(
		context.Background(), task, failure,
	)

	var logged map[string]any
	if err := json.NewDecoder(&output).Decode(&logged); err != nil {
		t.Fatalf("decode failure log: %v", err)
	}
	expected := map[string]any{
		"level": "ERROR", "msg": "AI task failed", "task_type": TaskTextProcess,
		"task_payload_decoded": true, "tenant_id": "tenant-1", "project_id": "project-1",
		"resource_id":   "feedback-1",
		"failure_class": "provider_not_found", "failure_permanent": true,
		"provider_code": float64(404), "retry_disposition": "skip_retry", "will_retry": false,
	}
	if len(diagnostics.events) != 1 ||
		diagnostics.events[0].ProjectID != "project-1" ||
		diagnostics.events[0].Reason != "provider_not_found" {
		t.Fatalf("provider diagnostics = %#v", diagnostics.events)
	}
	for key, want := range expected {
		if got := logged[key]; got != want {
			t.Errorf("%s = %#v, want %#v", key, got, want)
		}
	}
	if _, exists := logged["error"]; exists {
		t.Errorf("failure log contains raw error: %#v", logged["error"])
	}
	encoded := output.String()
	for _, secret := range []string{payloadSecret, providerSecret} {
		if strings.Contains(encoded, secret) {
			t.Errorf("failure log contains sensitive text %q: %s", secret, encoded)
		}
	}
}

func TestProcessorErrorHandlerOmitsMalformedPayloadMetadata(t *testing.T) {
	t.Parallel()
	const secret = "malformed payload customer text"
	task := asynq.NewTask(TaskTextProcess, []byte(
		`{"tenant_id":"tenant-1","text":"`+secret+`"`,
	))
	_, decodeErr := decodeTask(task)

	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	(Processor{Logger: logger}).ErrorHandler().HandleError(
		context.Background(), task, fmt.Errorf("sensitive wrapper %s: %w", secret, decodeErr),
	)

	var logged map[string]any
	if err := json.NewDecoder(&output).Decode(&logged); err != nil {
		t.Fatalf("decode failure log: %v", err)
	}
	if logged["task_payload_decoded"] != false || logged["retry_disposition"] != "skip_retry" ||
		logged["will_retry"] != false || logged["level"] != "ERROR" {
		t.Errorf("terminal metadata = %#v", logged)
	}
	for _, key := range []string{"tenant_id", "resource_id", "error"} {
		if _, exists := logged[key]; exists {
			t.Errorf("malformed task log contains %q: %#v", key, logged[key])
		}
	}
	if strings.Contains(output.String(), secret) {
		t.Errorf("malformed task log contains sensitive text: %s", output.String())
	}
}

func TestSafeFailureLogAttrsRetryState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		retry       retryMetadata
		wantLevel   slog.Level
		wantExhaust bool
		wantRetry   bool
		wantState   string
	}{
		{
			name: "retry pending",
			retry: retryMetadata{
				Count: 2, CountKnown: true, Maximum: 5, MaxKnown: true,
			},
			wantLevel: slog.LevelWarn, wantRetry: true, wantState: "retry",
		},
		{
			name: "retry exhausted",
			retry: retryMetadata{
				Count: 5, CountKnown: true, Maximum: 5, MaxKnown: true,
			},
			wantLevel: slog.LevelError, wantExhaust: true, wantState: "exhausted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const secret = "unbounded provider failure text"
			level, exhausted, attributes := safeFailureLogAttrs(errors.New(secret), test.retry)
			if level != test.wantLevel || exhausted != test.wantExhaust {
				t.Fatalf("metadata = level %s, exhausted %t", level, exhausted)
			}
			var output bytes.Buffer
			slog.New(slog.NewJSONHandler(&output, nil)).LogAttrs(
				context.Background(), level, "test", attributes...,
			)
			var logged map[string]any
			if err := json.NewDecoder(&output).Decode(&logged); err != nil {
				t.Fatalf("decode metadata log: %v", err)
			}
			if logged["retry_count"] != float64(test.retry.Count) ||
				logged["max_retry"] != float64(test.retry.Maximum) ||
				logged["retry_exhausted"] != test.wantExhaust ||
				logged["will_retry"] != test.wantRetry ||
				logged["retry_disposition"] != test.wantState {
				t.Errorf("retry metadata = %#v", logged)
			}
			if strings.Contains(output.String(), secret) {
				t.Errorf("metadata log contains raw error: %s", output.String())
			}
		})
	}
}

func TestProcessorRateLimitAndClockBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if err := (Processor{}).acquire(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("acquire(nil analyzer) error = %v", err)
	}
	processor := Processor{Analyzer: UnavailableAnalyzer{}, RPM: 15}
	if err := processor.acquire(ctx); err != nil {
		t.Errorf("acquire(no limiter) error = %v", err)
	}
	processor.Limiter = testLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: time.Second}}
	if err := processor.acquire(ctx); err == nil {
		t.Error("acquire(denied) error = nil")
	}
	processor.Limiter = testLimiter{err: errors.New("Redis unavailable")}
	if err := processor.acquire(ctx); err == nil {
		t.Error("acquire(limiter error) error = nil")
	}
	if processor.now().IsZero() {
		t.Error("now() returned zero time")
	}
}

func TestQueueHandlesConflictsAndClientFailures(t *testing.T) {
	t.Parallel()
	prepared := PreparedTask{
		Kind: TaskKindText, TenantID: "tenant", ResourceID: "feedback", EnqueuedAt: time.Now(),
	}
	client := &fakeTaskClient{err: asynq.ErrTaskIDConflict}
	if err := NewQueue(client).Enqueue(context.Background(), prepared); err != nil {
		t.Errorf("Enqueue(conflict) error = %v", err)
	}
	client.err = errors.New("Redis unavailable")
	if err := NewQueue(client).Enqueue(context.Background(), prepared); err == nil {
		t.Error("Enqueue(client failure) error = nil")
	}
	client.closeErr = errors.New("close failed")
	if err := NewQueue(client).Close(); err == nil {
		t.Error("Close(client failure) error = nil")
	}
	if got := truncateError(nil); got != "" {
		t.Errorf("truncateError(nil) = %q", got)
	}
	if got := truncateError(errors.New(strings.Repeat("x", 2100))); len(got) != 2000 {
		t.Errorf("truncateError(long) length = %d", len(got))
	}
	if permanent(nil) != nil {
		t.Error("permanent(nil) != nil")
	}
}

type fakeTaskClient struct {
	task     *asynq.Task
	closed   bool
	err      error
	closeErr error
}

type fakeGeminiAPI struct {
	uploadFile        *genai.File
	uploadErr         error
	uploadConfig      *genai.UploadFileConfig
	getFiles          []*genai.File
	getErr            error
	response          *genai.GenerateContentResponse
	generateErr       error
	generatedModel    string
	generatedConfig   *genai.GenerateContentConfig
	generatedContents []*genai.Content
	deletedName       string
	deleteErrors      []error
	deleteCalls       int
}

type fakeGeminiLedger struct {
	trackedFile string
	ready       bool
	completed   bool
	failure     error
	permanent   bool
	trackErr    error
}

type fakeGeminiFileDeleteStore struct {
	work      GeminiFileWork
	claimErr  error
	completed bool
	failure   error
	requeue   bool
	permanent bool
}

func (s *fakeGeminiFileDeleteStore) ClaimGeminiFile(
	context.Context,
	string,
	string,
	int,
) (GeminiFileWork, error) {
	return s.work, s.claimErr
}

func (s *fakeGeminiFileDeleteStore) CompleteGeminiFileDeletion(
	context.Context,
	GeminiFileWork,
	time.Time,
) error {
	s.completed = true
	return nil
}

func (s *fakeGeminiFileDeleteStore) FailGeminiFileDeletion(
	_ context.Context,
	_ GeminiFileWork,
	failure error,
	requeue bool,
	permanent bool,
	_ time.Time,
) error {
	s.failure, s.requeue, s.permanent = failure, requeue, permanent
	return nil
}

func (l *fakeGeminiLedger) TrackGeminiFile(
	_ context.Context,
	_ AttachmentWork,
	fileName string,
	_ time.Time,
) (string, error) {
	l.trackedFile = fileName
	return "tracked-file", l.trackErr
}

func (l *fakeGeminiLedger) MarkGeminiFileReady(context.Context, string, time.Time) error {
	l.ready = true
	return nil
}

func (l *fakeGeminiLedger) RecordGeminiFileDeleteFailure(
	_ context.Context,
	_ string,
	failure error,
	permanentFailure bool,
	_ time.Time,
) error {
	l.failure, l.permanent = failure, permanentFailure
	return nil
}

func (l *fakeGeminiLedger) CompleteTrackedGeminiFileDeletion(
	context.Context,
	string,
	time.Time,
) error {
	l.completed = true
	return nil
}

func (a *fakeGeminiAPI) Upload(
	_ context.Context,
	_ io.Reader,
	config *genai.UploadFileConfig,
) (*genai.File, error) {
	a.uploadConfig = config
	return a.uploadFile, a.uploadErr
}

func (a *fakeGeminiAPI) GetFile(context.Context, string) (*genai.File, error) {
	if a.getErr != nil {
		return nil, a.getErr
	}
	if len(a.getFiles) == 0 {
		return &genai.File{Name: "files/media", State: genai.FileStateProcessing}, nil
	}
	file := a.getFiles[0]
	a.getFiles = a.getFiles[1:]
	return file, nil
}

func (a *fakeGeminiAPI) DeleteFile(_ context.Context, name string) error {
	a.deletedName = name
	a.deleteCalls++
	if len(a.deleteErrors) > 0 {
		err := a.deleteErrors[0]
		a.deleteErrors = a.deleteErrors[1:]
		return err
	}
	return nil
}

func (a *fakeGeminiAPI) GenerateContent(
	_ context.Context,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) (*genai.GenerateContentResponse, error) {
	a.generatedModel = model
	a.generatedConfig = config
	a.generatedContents = contents
	return a.response, a.generateErr
}

func structuredGeminiResponse(value string) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{
		Content: &genai.Content{Parts: []*genai.Part{genai.NewPartFromText(value)}},
	}}}
}

func finishReasonResponse(reason genai.FinishReason) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: reason}}}
}

func (c *fakeTaskClient) EnqueueContext(
	_ context.Context,
	task *asynq.Task,
	_ ...asynq.Option,
) (*asynq.TaskInfo, error) {
	c.task = task
	return &asynq.TaskInfo{}, c.err
}

func (c *fakeTaskClient) Close() error {
	c.closed = true
	return c.closeErr
}

type testLimiter struct {
	decision ratelimit.Decision
	err      error
}

func (l testLimiter) Allow(context.Context, string, ratelimit.Window) (ratelimit.Decision, error) {
	return l.decision, l.err
}
