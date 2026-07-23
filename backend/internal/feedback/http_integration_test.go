package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetanalytics"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestFeedbackHTTPFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, feedbackMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}

	var tenantID, projectID, ruleID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	publicKey := "pk_live_ABCDEFGHIJKLMNOPQRSTUV"
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', $2, '{example.com}') RETURNING id
	`, tenantID, publicKey).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds,
			can_attach_element, can_attach_screenshot, can_record_audio,
			replay_mode, replay_sampling_rate
		) VALUES ($1, 'All pages', 'all_pages', 'delay', 0, true, true, true, 'full', 0)
		RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create widget rule: %v", err)
	}
	var replaySessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, page_url, started_at,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), 'https://example.com/checkout', now(), '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantID, projectID).Scan(&replaySessionID); err != nil {
		t.Fatalf("create replay session: %v", err)
	}

	pools, err := db.OpenPools(ctx, feedbackRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	analyticsConfiguration := project.PublicConfiguration{
		TenantID: tenantID, ProjectID: projectID, Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
		Rules: []project.WidgetRule{{ID: ruleID, Name: "All pages", Enabled: true}},
	}
	matchedValues, analyticsFields := widgetanalytics.ValidateEvents([]widgetanalytics.EventInput{{
		EventID: "00000000-0000-4000-8000-000000000101", EventType: widgetanalytics.EventMatched,
		VisitorID: "00000000-0000-4000-8000-000000000102", PageViewID: "00000000-0000-4000-8000-000000000103",
		RuleID: ruleID, PageURL: "https://example.com/checkout", OccurredAt: now,
	}}, analyticsConfiguration, "https://example.com", now)
	if len(analyticsFields) > 0 {
		t.Fatalf("validate feedback matched analytics: %#v", analyticsFields)
	}
	analyticsTx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin feedback analytics transaction: %v", err)
	}
	if _, err := analyticsTx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		t.Fatalf("set feedback analytics tenant: %v", err)
	}
	if _, err := (widgetanalytics.Store{}).InsertEvents(ctx, analyticsTx, matchedValues); err != nil {
		t.Fatalf("seed feedback matched analytics: %v", err)
	}
	if err := analyticsTx.Commit(ctx); err != nil {
		t.Fatalf("commit feedback matched analytics: %v", err)
	}
	audioBody := validWebMFixtureOfSize(2048)
	if len(audioBody) != 2048 {
		t.Fatalf("audio fixture size = %d, want 2048", len(audioBody))
	}
	storageClient := &feedbackStorageStub{
		stat: storage.ObjectInfo{Size: 2048, ContentType: "audio/webm"},
		data: audioBody,
	}
	scheduler := &attachmentSchedulerStub{}
	var handlerLogs bytes.Buffer
	authorizer := widgetapi.Authorizer{Store: project.Store{}, Database: pools.Admin, Now: func() time.Time { return now }}
	handler := feedbackHandler(HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Storage: storageClient, Scheduler: scheduler, Analytics: widgetanalytics.Store{},
		Now:    func() time.Time { return now },
		Logger: slog.New(slog.NewTextHandler(&handlerLogs, nil)),
	})

	clientFeedbackID := "57e3ee52-4ebf-43ce-9e31-9f909628634d"
	clientAttachmentID := "61c18d72-2296-43fa-bd21-a1356aa5da1c"
	body := fmt.Sprintf(`{
		"client_feedback_id":%q,"widget_rule_id":%q,"session_id":%q,"rating":5,
		"text":"Checkout was clear","element_attachment":{"selector":"#checkout","html":"<section>Checkout</section>","styles":{"display":"grid"}},
		"responses":{"rating":5,"feedback":"Checkout was clear"},
		"attachments":[{"client_attachment_id":%q,"kind":"audio","content_type":"audio/webm","size_bytes":2048}],
		"metadata":{"url":"https://example.com/checkout","referrer":"https://example.com/","user_agent":"browser","os":"Linux","browser":"Firefox","device":"desktop","viewport_width":1280,"viewport_height":720,"language":"en"},
		"analytics":{"event_id":"00000000-0000-4000-8000-000000000104","visitor_id":"00000000-0000-4000-8000-000000000102","page_view_id":"00000000-0000-4000-8000-000000000103","rule_id":%q,"page_url":"https://example.com/checkout?step=payment#card","occurred_at":"2026-07-21T12:00:00Z"}
	}`, clientFeedbackID, ruleID, replaySessionID, clientAttachmentID, ruleID)

	created := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey, body, publicKey, "https://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s, logs = %s",
			created.Code, created.Body.String(), handlerLogs.String())
	}
	var first SubmissionResponse
	decodeFeedbackResponse(t, created, &first)
	if first.FeedbackID == "" || first.FeedbackPublicID == "" || first.IssuePublicID != "" {
		t.Errorf("created response = %#v", first)
	}
	if len(first.Attachments) != 1 || first.Attachments[0].CompleteToken == "" ||
		!strings.Contains(first.Attachments[0].PresignedURL, "/tenants/"+tenantID+"/feedbacks/"+first.FeedbackID) {
		t.Errorf("attachment response = %#v", first.Attachments)
	}
	assertFeedbackRows(t, ctx, root, first.FeedbackID, tenantID, projectID)
	var submittedCount int
	var submittedFeedbackID string
	if err := root.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(feedback_id::text), '')
		FROM widget_funnel_events
		WHERE project_id = $1 AND event_type = 'submitted'
	`, projectID).Scan(&submittedCount, &submittedFeedbackID); err != nil {
		t.Fatalf("read submitted analytics: %v", err)
	}
	if submittedCount != 1 || submittedFeedbackID != first.FeedbackID {
		t.Fatalf("submitted analytics = count %d feedback %v", submittedCount, submittedFeedbackID)
	}
	var feedbackSessionID, sessionFeedbackID *string
	if err := root.QueryRow(ctx, `SELECT session_id FROM feedbacks WHERE id = $1`, first.FeedbackID).
		Scan(&feedbackSessionID); err != nil {
		t.Fatalf("read feedback replay link: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT feedback_id FROM sessions WHERE id = $1`, replaySessionID).
		Scan(&sessionFeedbackID); err != nil {
		t.Fatalf("read session feedback link: %v", err)
	}
	if feedbackSessionID == nil || *feedbackSessionID != replaySessionID ||
		sessionFeedbackID == nil || *sessionFeedbackID != first.FeedbackID {
		t.Errorf("replay links = feedback %v session %v", feedbackSessionID, sessionFeedbackID)
	}
	dispatchQueue := &outboxEnqueuerStub{}
	dispatcher := outbox.Dispatcher{
		Store: outbox.Store{Database: root}, Resolver: outbox.FeedbackResolver{},
		Enqueuer: dispatchQueue, Now: func() time.Time { return now },
	}
	if err := dispatcher.RunOnce(ctx); err != nil {
		t.Fatalf("dispatch feedback outbox: %v", err)
	}
	if len(dispatchQueue.taskIDs) != 1 || !strings.HasPrefix(dispatchQueue.taskIDs[0], "outbox:") {
		t.Errorf("dispatched task IDs = %#v", dispatchQueue.taskIDs)
	}
	var dispatchedAt *time.Time
	var dispatchAttempts int
	if err := root.QueryRow(ctx, `
		SELECT dispatched_at, attempts FROM outbox_events WHERE resource_id = $1
	`, first.FeedbackID).Scan(&dispatchedAt, &dispatchAttempts); err != nil {
		t.Fatalf("read dispatched event: %v", err)
	}
	if dispatchedAt == nil || dispatchAttempts != 1 {
		t.Errorf("dispatched_at = %#v, attempts = %d", dispatchedAt, dispatchAttempts)
	}
	if _, err := root.Exec(ctx, `
		UPDATE attachments
		SET processing_status = 'failed', error_message = 'attachment upload expired before completion',
		    processed_at = $2, upload_refreshed_at = $2::timestamptz - interval '10 minutes',
		    upload_expires_at = $2
		WHERE id = $1
	`, first.Attachments[0].AttachmentID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("expire attachment before retry: %v", err)
	}
	expiredCompletion := performFeedbackRequest(handler, http.MethodPost,
		"/v1/attachments/"+first.Attachments[0].AttachmentID+"/complete?key="+publicKey,
		fmt.Sprintf(`{"complete_token":%q}`, first.Attachments[0].CompleteToken),
		publicKey, "https://example.com")
	if expiredCompletion.Code != http.StatusGone ||
		!strings.Contains(expiredCompletion.Body.String(), `"reason":"upload_expired"`) {
		t.Fatalf("expired completion status = %d, body = %s",
			expiredCompletion.Code, expiredCompletion.Body.String())
	}
	if storageClient.removed {
		t.Fatal("expired attachment object was removed inside the HTTP transaction")
	}
	expiryScheduler := VerificationScheduler{
		Store: VerificationStore{Database: pools.Admin}, Queue: &verificationQueueStub{},
		Storage: storageClient, Now: func() time.Time { return now },
	}
	if err := expiryScheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("reconcile expired attachment cleanup: %v", err)
	}
	if !storageClient.removed {
		t.Fatal("expired attachment object was not removed by the reconciler")
	}

	idempotent := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey, body, publicKey, "https://example.com")
	if idempotent.Code != http.StatusOK {
		t.Fatalf("idempotent status = %d, body = %s, logs = %s",
			idempotent.Code, idempotent.Body.String(), handlerLogs.String())
	}
	var second SubmissionResponse
	decodeFeedbackResponse(t, idempotent, &second)
	if second.FeedbackID != first.FeedbackID || second.IssuePublicID != first.IssuePublicID {
		t.Errorf("idempotent response = %#v, first = %#v", second, first)
	}
	if len(second.Attachments) != 1 || second.Attachments[0].CompleteToken == first.Attachments[0].CompleteToken {
		t.Errorf("idempotent attachment token was not rotated")
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_events
		WHERE project_id = $1 AND event_type = 'submitted'
	`, projectID).Scan(&submittedCount); err != nil || submittedCount != 1 {
		t.Fatalf("submitted analytics after retry = %d, error %v, want 1", submittedCount, err)
	}

	rollbackBody := fmt.Sprintf(`{
		"client_feedback_id":"57e3ee52-4ebf-43ce-9e31-9f9096286399","widget_rule_id":%q,
		"rating":4,"metadata":{"url":"https://example.com/checkout","viewport_width":1280,"viewport_height":720},
		"analytics":{"event_id":"00000000-0000-4000-8000-000000000199","visitor_id":"00000000-0000-4000-8000-000000000102","page_view_id":"00000000-0000-4000-8000-000000000103","rule_id":%q,"page_url":"https://example.com/checkout","occurred_at":"2026-07-21T12:00:00Z"}
	}`, ruleID, ruleID)
	rollbackBody = strings.Replace(rollbackBody, `"session_id":"`+replaySessionID+`"`, `"session_id":null`, 1)
	analyticsFailure := feedbackHandler(HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Storage: storageClient, Analytics: submissionAnalyticsStoreStub{err: errors.New("analytics unavailable")},
		Now: func() time.Time { return now },
	})
	rolledBack := performFeedbackRequest(analyticsFailure, http.MethodPost,
		"/v1/feedback?key="+publicKey, rollbackBody, publicKey, "https://example.com")
	if rolledBack.Code != http.StatusServiceUnavailable {
		t.Fatalf("analytics failure status = %d, body = %s", rolledBack.Code, rolledBack.Body.String())
	}
	var rolledBackFeedbacks int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks WHERE project_id = $1
	`, projectID).Scan(&rolledBackFeedbacks); err != nil || rolledBackFeedbacks != 1 {
		t.Fatalf("feedback count after analytics rollback = %d, error %v, want 1", rolledBackFeedbacks, err)
	}

	eventIDCollisionBody := fmt.Sprintf(`{
		"client_feedback_id":"57e3ee52-4ebf-43ce-9e31-9f9096286377","widget_rule_id":%q,
		"rating":3,"metadata":{"url":"https://example.com/checkout","viewport_width":1280,"viewport_height":720},
		"analytics":{"event_id":"00000000-0000-4000-8000-000000000101","visitor_id":"00000000-0000-4000-8000-000000000102","page_view_id":"00000000-0000-4000-8000-000000000103","rule_id":%q,"page_url":"https://example.com/checkout","occurred_at":"2026-07-21T12:00:00Z"}
	}`, ruleID, ruleID)
	collision := performFeedbackRequest(handler, http.MethodPost,
		"/v1/feedback?key="+publicKey, eventIDCollisionBody, publicKey, "https://example.com")
	if collision.Code != http.StatusServiceUnavailable {
		t.Fatalf("analytics event ID collision status = %d, body = %s", collision.Code, collision.Body.String())
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM feedbacks WHERE project_id = $1
	`, projectID).Scan(&rolledBackFeedbacks); err != nil || rolledBackFeedbacks != 1 {
		t.Fatalf("feedback count after event ID collision = %d, error %v, want 1", rolledBackFeedbacks, err)
	}

	legacyBody := fmt.Sprintf(`{
		"client_feedback_id":"57e3ee52-4ebf-43ce-9e31-9f9096286388","widget_rule_id":%q,
		"rating":4,"metadata":{"url":"https://example.com/legacy","viewport_width":1280,"viewport_height":720}
	}`, ruleID)
	legacyCreated := performFeedbackRequest(handler, http.MethodPost,
		"/v1/feedback?key="+publicKey, legacyBody, publicKey, "https://example.com")
	if legacyCreated.Code != http.StatusCreated {
		t.Fatalf("legacy feedback status = %d, body = %s", legacyCreated.Code, legacyCreated.Body.String())
	}
	var legacySubmission SubmissionResponse
	decodeFeedbackResponse(t, legacyCreated, &legacySubmission)
	if _, err := root.Exec(ctx, `DELETE FROM outbox_events WHERE resource_id = $1`, legacySubmission.FeedbackID); err != nil {
		t.Fatalf("clean legacy feedback outbox: %v", err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM feedbacks WHERE id = $1`, legacySubmission.FeedbackID); err != nil {
		t.Fatalf("clean legacy feedback: %v", err)
	}
	var retriedStatus string
	var retriedError *string
	var retriedProcessedAt *time.Time
	var uploadRefreshedAt time.Time
	if err := root.QueryRow(ctx, `
		SELECT processing_status, error_message, processed_at, upload_refreshed_at
		FROM attachments WHERE id = $1
	`, second.Attachments[0].AttachmentID).Scan(
		&retriedStatus, &retriedError, &retriedProcessedAt, &uploadRefreshedAt,
	); err != nil {
		t.Fatalf("read retried attachment state: %v", err)
	}
	if retriedStatus != "pending" || retriedError != nil || retriedProcessedAt != nil || !uploadRefreshedAt.Equal(now) {
		t.Errorf("retried attachment state = %s/%v/%v/%v", retriedStatus, retriedError,
			retriedProcessedAt, uploadRefreshedAt)
	}

	completeBody := fmt.Sprintf(`{"complete_token":%q}`, second.Attachments[0].CompleteToken)
	invalidCompletion := performFeedbackRequest(handler, http.MethodPost,
		"/v1/attachments/"+second.Attachments[0].AttachmentID+"/complete?key="+publicKey,
		`{"complete_token":"wrong"}`, publicKey, "https://example.com")
	if invalidCompletion.Code != http.StatusForbidden {
		t.Errorf("invalid completion status = %d, body = %s", invalidCompletion.Code, invalidCompletion.Body.String())
	}
	storageClient.stat = storage.ObjectInfo{Size: 2047, ContentType: "audio/ogg"}
	mismatchedUpload := performFeedbackRequest(handler, http.MethodPost,
		"/v1/attachments/"+second.Attachments[0].AttachmentID+"/complete?key="+publicKey,
		completeBody, publicKey, "https://example.com")
	if mismatchedUpload.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(mismatchedUpload.Body.String(), `"field":"size_bytes"`) ||
		!strings.Contains(mismatchedUpload.Body.String(), `"field":"content_type"`) {
		t.Errorf("mismatched upload status = %d, body = %s", mismatchedUpload.Code, mismatchedUpload.Body.String())
	}
	if _, err := root.Exec(ctx, `
		UPDATE attachments
		SET processing_status = 'failed', error_message = 'attachment upload expired before completion',
		    processed_at = $2
		WHERE id = $1
	`, second.Attachments[0].AttachmentID, now.Add(time.Minute)); err != nil {
		t.Fatalf("expire attachment before late completion: %v", err)
	}
	storageClient.stat = storage.ObjectInfo{Size: 2048, ContentType: "audio/webm"}
	completed := performFeedbackRequest(handler, http.MethodPost,
		"/v1/attachments/"+second.Attachments[0].AttachmentID+"/complete?key="+publicKey,
		completeBody, publicKey, "https://example.com")
	if completed.Code != http.StatusNoContent {
		t.Fatalf("complete status = %d, body = %s", completed.Code, completed.Body.String())
	}
	var tokenHash, completedError *string
	var completedAt *time.Time
	var completedStatus, completedSafetyStatus string
	var completedProcessedAt *time.Time
	if err := root.QueryRow(ctx,
		`SELECT complete_token_hash, completed_at, processing_status, error_message, processed_at,
			        safety_status
			 FROM attachments WHERE id = $1`,
		second.Attachments[0].AttachmentID,
	).Scan(&tokenHash, &completedAt, &completedStatus, &completedError, &completedProcessedAt,
		&completedSafetyStatus); err != nil {
		t.Fatalf("read completed attachment: %v", err)
	}
	if tokenHash != nil || completedAt == nil || completedStatus != "pending" ||
		completedError != nil || completedProcessedAt != nil || completedSafetyStatus != "quarantined" {
		t.Errorf("completed attachment state = token %#v at %#v status %s safety %s error %#v processed %#v",
			tokenHash, completedAt, completedStatus, completedSafetyStatus, completedError, completedProcessedAt)
	}
	if scheduler.task.AttachmentID != second.Attachments[0].AttachmentID ||
		scheduler.task.TenantID != tenantID || scheduler.task.Generation < 1 {
		t.Errorf("scheduled verification = %#v", scheduler.task)
	}
	var persistedGeneration int
	var verificationEnqueuedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT verification_generation, verification_enqueued_at
		FROM attachments
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, second.Attachments[0].AttachmentID).Scan(
		&persistedGeneration, &verificationEnqueuedAt,
	); err != nil {
		t.Fatalf("read scheduled attachment verification state: %v", err)
	}
	if persistedGeneration != scheduler.task.Generation {
		t.Fatalf("scheduled verification generation = %d, persisted = %d",
			scheduler.task.Generation, persistedGeneration)
	}
	if _, err := (Store{}).GetTenantAttachmentObject(
		ctx, root, tenantID, first.FeedbackID, second.Attachments[0].AttachmentID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("quarantined attachment stream lookup error = %v, want ErrNotFound", err)
	}
	aiAttachmentScheduler := &verifiedAISchedulerStub{}
	verificationProcessor := VerificationProcessor{
		Store: VerificationStore{Database: root}, Storage: storageClient,
		Scanner: SignatureScanner{}, AI: aiAttachmentScheduler,
		Now: func() time.Time { return now },
	}
	verificationPayload, err := json.Marshal(scheduler.task)
	if err != nil {
		t.Fatalf("encode verification task: %v", err)
	}
	if err := verificationProcessor.Handler()(
		ctx, asynq.NewTask(TaskAttachmentVerify, verificationPayload),
	); err != nil {
		t.Fatalf("verify uploaded attachment: %v", err)
	}
	if aiAttachmentScheduler.attachmentID != second.Attachments[0].AttachmentID ||
		aiAttachmentScheduler.feedbackID != first.FeedbackID {
		t.Fatalf("verified attachment AI schedule = %q/%q",
			aiAttachmentScheduler.attachmentID, aiAttachmentScheduler.feedbackID)
	}
	verifiedAttachment, err := (Store{}).GetTenantAttachmentObject(
		ctx, root, tenantID, first.FeedbackID, second.Attachments[0].AttachmentID,
	)
	if err != nil {
		t.Fatalf("read verified attachment object: %v", err)
	}
	wantVerifiedKey := verifiedAttachmentKey(
		storageClient.presignedKeys[0], scheduler.task.Generation, 1,
	)
	if verifiedAttachment.StorageKey != wantVerifiedKey ||
		!bytes.Equal(storageClient.objects[wantVerifiedKey], audioBody) {
		t.Fatalf("verified attachment promotion = key %q body size %d",
			verifiedAttachment.StorageKey, len(storageClient.objects[wantVerifiedKey]))
	}
	storageClient.objects[storageClient.presignedKeys[0]] = []byte("overwritten staging object")
	verifiedAttachment, err = (Store{}).GetTenantAttachmentObject(
		ctx, root, tenantID, first.FeedbackID, second.Attachments[0].AttachmentID,
	)
	if err != nil || verifiedAttachment.StorageKey != wantVerifiedKey {
		t.Fatalf("verified attachment after staging overwrite = %#v, %v",
			verifiedAttachment, err)
	}
	assertAttachmentVerificationStoreLifecycle(
		t, ctx, root, tenantID, first.FeedbackID, now,
	)
	invalidOrigin := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey,
		body, publicKey, "https://evil.test")
	if invalidOrigin.Code != http.StatusForbidden || invalidOrigin.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("invalid origin status = %d, headers = %#v", invalidOrigin.Code, invalidOrigin.Header())
	}
	malformed := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey,
		`{`, publicKey, "https://example.com")
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("malformed status = %d, body = %s", malformed.Code, malformed.Body.String())
	}
	invalid := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey,
		`{"client_feedback_id":"bad","widget_rule_id":"bad","metadata":{}}`, publicKey, "https://example.com")
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), `"field":"metadata.url"`) {
		t.Errorf("invalid status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	honeypotBody := addHoneypot(t, body)
	honeypot := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey,
		honeypotBody, publicKey, "https://example.com")
	if honeypot.Code != http.StatusOK {
		t.Errorf("honeypot status = %d, body = %s", honeypot.Code, honeypot.Body.String())
	}
	var feedbackCount int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM feedbacks WHERE project_id = $1`, projectID).
		Scan(&feedbackCount); err != nil {
		t.Fatalf("count feedback after honeypot: %v", err)
	}
	if feedbackCount != 1 {
		t.Errorf("feedback count after honeypot = %d", feedbackCount)
	}
	assertTenantFeedbackReadFlow(
		t, ctx, root, pools, tenantID, projectID, ruleID, replaySessionID,
		first.FeedbackID, second.Attachments[0].AttachmentID, now,
	)

	deniedByRate := feedbackHandler(HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{}, Storage: storageClient,
		Limiter: staticLimiter{decision: ratelimit.Decision{Allowed: false, Limit: 10, RetryAfter: 7 * time.Second}},
	})
	rateResponse := performFeedbackRequest(deniedByRate, http.MethodPost, "/v1/feedback?key="+publicKey,
		body, publicKey, "https://example.com")
	if rateResponse.Code != http.StatusTooManyRequests || rateResponse.Header().Get("Retry-After") != "7" ||
		rateResponse.Header().Get("X-RateLimit-Limit") == "" {
		t.Errorf("rate response status = %d, headers = %#v", rateResponse.Code, rateResponse.Header())
	}
	diagnostics := &abuseDiagnosticRecorderStub{}
	deniedByCoordinatedRate := feedbackHandler(HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Storage: storageClient, Diagnostics: diagnostics,
		Abuse: abuseEvaluatorFunc(func(
			_ context.Context,
			policy abuse.Policy,
			_ abuse.Request,
		) (abuse.Decision, error) {
			return abuse.Decision{
				Allowed: false, Reason: policy.Reason, RetryAfter: 6500 * time.Millisecond,
				Limit: 10, Remaining: 0,
			}, nil
		}),
	})
	coordinatedRate := performFeedbackRequest(
		deniedByCoordinatedRate, http.MethodPost, "/v1/feedback?key="+publicKey,
		body, publicKey, "https://example.com",
	)
	if coordinatedRate.Code != http.StatusTooManyRequests ||
		coordinatedRate.Header().Get("Retry-After") != "7" ||
		!strings.Contains(coordinatedRate.Body.String(), `"reason":"submission_rate_limited"`) ||
		!strings.Contains(coordinatedRate.Body.String(), `"retry_after_seconds":7`) {
		t.Errorf("coordinated rate response status=%d headers=%#v body=%s",
			coordinatedRate.Code, coordinatedRate.Header(), coordinatedRate.Body.String())
	}
	if len(diagnostics.events) != 1 ||
		diagnostics.events[0].Reason != abuse.ReasonSubmissionRateLimited {
		t.Errorf("rate diagnostics = %#v", diagnostics.events)
	}

	degradedBody := fmt.Sprintf(`{
		"client_feedback_id":"00000000-0000-4000-8000-000000000211",
		"widget_rule_id":%q,"rating":4,"text":"Keep this feedback",
		"attachments":[{
			"client_attachment_id":"00000000-0000-4000-8000-000000000212",
			"kind":"audio","content_type":"audio/webm","size_bytes":2048
		}],
		"metadata":{"url":"https://example.com/checkout","viewport_width":390,"viewport_height":844}
	}`, ruleID)
	limiterFailure := errors.New("redis unavailable")
	degraded := feedbackHandler(HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Storage: storageClient, Now: func() time.Time { return now },
		Abuse: abuseEvaluatorFunc(func(
			_ context.Context,
			policy abuse.Policy,
			_ abuse.Request,
		) (abuse.Decision, error) {
			switch policy.Name {
			case abuse.SubmissionPolicy.Name:
				return abuse.Decision{
					Allowed: true, Unavailable: true, Reason: policy.Reason,
				}, limiterFailure
			case abuse.AttachmentGrantPolicy.Name:
				return abuse.Decision{
					Allowed: false, Unavailable: true, Reason: policy.Reason,
				}, limiterFailure
			default:
				return abuse.Decision{Allowed: true, Reason: policy.Reason}, nil
			}
		}),
		Diagnostics: diagnostics,
	})
	degradedResponse := performFeedbackRequest(
		degraded, http.MethodPost, "/v1/feedback?key="+publicKey,
		degradedBody, publicKey, "https://example.com",
	)
	if degradedResponse.Code != http.StatusCreated {
		t.Fatalf("degraded submission status=%d body=%s",
			degradedResponse.Code, degradedResponse.Body.String())
	}
	var degradedSubmission SubmissionResponse
	decodeFeedbackResponse(t, degradedResponse, &degradedSubmission)
	if len(degradedSubmission.Attachments) != 0 || len(degradedSubmission.Warnings) != 1 ||
		degradedSubmission.Warnings[0].Reason != abuse.ReasonUploadRateLimited {
		t.Errorf("degraded submission = %#v", degradedSubmission)
	}
	var savedText string
	if err := root.QueryRow(ctx,
		`SELECT description FROM feedbacks WHERE id = $1`,
		degradedSubmission.FeedbackID,
	).Scan(&savedText); err != nil || savedText != "Keep this feedback" {
		t.Errorf("degraded saved text=%q error=%v", savedText, err)
	}

	if _, err := root.Exec(ctx, `UPDATE tenants SET plan = 'starter' WHERE id = $1`, tenantID); err != nil {
		t.Fatalf("set starter plan: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, widget_rule_id, public_id, client_feedback_id, page_url, created_at
		)
		SELECT $1, $2, $3, 'fb_' || substr(md5(value::text), 1, 8), gen_random_uuid(),
		       'https://example.com', $4
		FROM generate_series(1, 500) AS value
	`, tenantID, projectID, ruleID, now); err != nil {
		t.Fatalf("seed monthly plan usage: %v", err)
	}
	limitBody := fmt.Sprintf(`{
		"client_feedback_id":"e5409823-01a8-44cc-80b8-6bc914bd0164","widget_rule_id":%q,
		"rating":4,"metadata":{"url":"https://example.com","viewport_width":390,"viewport_height":844}
	}`, ruleID)
	planLimit := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey,
		limitBody, publicKey, "https://example.com")
	if planLimit.Code != http.StatusPaymentRequired || !strings.Contains(planLimit.Body.String(), "plan_limit_exceeded") {
		t.Errorf("plan limit status = %d, body = %s", planLimit.Code, planLimit.Body.String())
	}
}

func assertTenantFeedbackReadFlow(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	pools *db.Pools,
	tenantID string,
	projectID string,
	ruleID string,
	replaySessionID string,
	feedbackID string,
	attachmentID string,
	now time.Time,
) {
	t.Helper()
	firstCreatedAt := now.Add(-30 * time.Minute)
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks
		SET rating = 2, text_cleaned = 'Checkout total changed unexpectedly',
		    text_sentiment_score = -0.8, text_moderation_flags = '{unsafe}',
		    text_process_status = 'completed', ai_processed_at = $2, created_at = $2
		WHERE id = $1
	`, feedbackID, firstCreatedAt); err != nil {
		t.Fatalf("prepare readable feedback: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE attachments
		SET processing_status = 'completed', transcription = 'The total changed at checkout',
		    sentiment_score = -0.7, moderation_flags = '{personal_data}', processed_at = $2
		WHERE id = $1
	`, attachmentID, firstCreatedAt); err != nil {
		t.Fatalf("prepare readable attachment: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE sessions
		SET started_at = $2::timestamptz - interval '5 minutes', ended_at = $2,
		    merge_requested_at = $2, merged_at = $2,
		    storage_key = 'tenants/acme/replays/ready.json', events_count = 42, batches_count = 1
		WHERE id = $1
	`, replaySessionID, firstCreatedAt); err != nil {
		t.Fatalf("prepare readable replay: %v", err)
	}
	var secondFeedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, widget_rule_id, public_id, client_feedback_id,
			rating, text, text_cleaned, text_sentiment_score, text_process_status,
			page_url, status, created_at
		) VALUES (
			$1, $2, $3, 'fb_1234abcd', gen_random_uuid(), 5, 'Fast checkout',
			'Checkout was fast', 0.9, 'completed', 'https://example.com/checkout',
			'archived', $4
		) RETURNING id
	`, tenantID, projectID, ruleID, now.Add(-time.Hour)).Scan(&secondFeedbackID); err != nil {
		t.Fatalf("create second readable feedback: %v", err)
	}
	var unavailableSessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, feedback_id, session_id, started_at, ended_at,
			rrweb_version, merge_attempts, merge_error, max_duration_seconds, max_events
		) VALUES (
			$1, $2, $3, gen_random_uuid(), $4, $4, '2.1.1', 12,
			'No replay events were uploaded before the recording ended.', 1800, 50000
		) RETURNING id
	`, tenantID, projectID, secondFeedbackID, now.Add(-time.Hour)).Scan(&unavailableSessionID); err != nil {
		t.Fatalf("create unavailable feedback replay: %v", err)
	}
	if _, err := root.Exec(ctx,
		`UPDATE feedbacks SET session_id = $2 WHERE id = $1`, secondFeedbackID, unavailableSessionID,
	); err != nil {
		t.Fatalf("link unavailable feedback replay: %v", err)
	}
	var assigneeID, viewerID, feedbackWriterID, checkoutTagID, accessibilityTagID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions)
		VALUES ($1, 'triage@acme.test', 'Ada Triage', $2) RETURNING id
	`, tenantID, auth.TenantPermissionStrings()).Scan(&assigneeID); err != nil {
		t.Fatalf("create feedback assignee: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'viewer@acme.test', ARRAY['project:read']) RETURNING id
	`, tenantID).Scan(&viewerID); err != nil {
		t.Fatalf("create feedback viewer: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'writer@acme.test', ARRAY['feedback:read', 'feedback:update']) RETURNING id
	`, tenantID).Scan(&feedbackWriterID); err != nil {
		t.Fatalf("create feedback writer: %v", err)
	}
	if _, err := root.Exec(ctx,
		`UPDATE feedbacks SET priority = 'urgent', assignee_id = $2 WHERE id = $1`,
		feedbackID, assigneeID,
	); err != nil {
		t.Fatalf("add readable feedback triage metadata: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color) VALUES ($1, 'Checkout', '#167D68') RETURNING id
	`, tenantID).Scan(&checkoutTagID); err != nil {
		t.Fatalf("create checkout feedback tag: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color) VALUES ($1, 'Accessibility', '#3B82F6') RETURNING id
	`, tenantID).Scan(&accessibilityTagID); err != nil {
		t.Fatalf("create accessibility feedback tag: %v", err)
	}
	for _, tagID := range []string{checkoutTagID, accessibilityTagID} {
		if _, err := root.Exec(ctx, `
			INSERT INTO feedback_tags (tenant_id, feedback_id, tag_id) VALUES ($1, $2, $3)
		`, tenantID, feedbackID, tagID); err != nil {
			t.Fatalf("assign readable feedback tag: %v", err)
		}
	}

	jwtManager, err := auth.NewJWTManager(strings.Repeat("f", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create tenant feedback JWT manager: %v", err)
	}
	ownerToken, _, err := jwtManager.IssueTenant(assigneeID, tenantID, auth.TenantPermissionStrings())
	if err != nil {
		t.Fatalf("issue tenant feedback token: %v", err)
	}
	deniedToken, _, err := jwtManager.IssueTenant(viewerID, tenantID, []string{string(auth.PermissionProjectRead)})
	if err != nil {
		t.Fatalf("issue denied tenant feedback token: %v", err)
	}
	feedbackOnlyToken, _, err := jwtManager.IssueTenant(feedbackWriterID, tenantID, []string{
		string(auth.PermissionFeedbackRead), string(auth.PermissionFeedbackUpdate),
	})
	if err != nil {
		t.Fatalf("issue feedback-only tenant token: %v", err)
	}

	routes := chi.NewRouter()
	RegisterTenantRoutes(routes, TenantHTTPOptions{
		Store: Store{}, JWT: jwtManager, Now: func() time.Time { return now.Add(time.Hour) },
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	listed := performTenantFeedbackRequest(handler, http.MethodGet, "/v1/feedbacks?limit=1", "", ownerToken)
	if listed.Code != http.StatusOK {
		t.Fatalf("list tenant feedback status = %d, body = %s", listed.Code, listed.Body.String())
	}
	var firstPage struct {
		Feedbacks  []Item `json:"feedbacks"`
		Pagination struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"pagination"`
	}
	decodeFeedbackResponse(t, listed, &firstPage)
	if len(firstPage.Feedbacks) != 1 || firstPage.Feedbacks[0].ID != feedbackID ||
		!firstPage.Pagination.HasMore || firstPage.Pagination.NextCursor == nil {
		t.Fatalf("first tenant feedback page = %#v", firstPage)
	}
	assertFeedbackTriageRead(t, firstPage.Feedbacks[0], assigneeID)
	secondPageResponse := performTenantFeedbackRequest(
		handler, http.MethodGet, "/v1/feedbacks?limit=1&cursor="+url.QueryEscape(*firstPage.Pagination.NextCursor),
		"", ownerToken,
	)
	var secondPage struct {
		Feedbacks []Item `json:"feedbacks"`
	}
	decodeFeedbackResponse(t, secondPageResponse, &secondPage)
	if secondPageResponse.Code != http.StatusOK || len(secondPage.Feedbacks) != 1 ||
		secondPage.Feedbacks[0].ID != secondFeedbackID {
		t.Fatalf("second tenant feedback page = %d %#v", secondPageResponse.Code, secondPage)
	}
	if secondPage.Feedbacks[0].Priority != nil || secondPage.Feedbacks[0].Assignee != nil ||
		len(secondPage.Feedbacks[0].Tags) != 0 {
		t.Fatalf("legacy feedback triage metadata = %#v", secondPage.Feedbacks[0])
	}

	filters := url.Values{
		"project_id": {projectID}, "status": {"open"}, "rating": {"2"},
		"attachment": {"audio"}, "moderation": {"flagged"}, "issue": {"unlinked"},
		"sentiment": {"negative"}, "date_from": {now.Add(-time.Hour).Format(time.RFC3339)},
	}.Encode()
	filtered := performTenantFeedbackRequest(handler, http.MethodGet, "/v1/feedbacks?"+filters, "", ownerToken)
	var filteredPage struct {
		Feedbacks []Item `json:"feedbacks"`
	}
	decodeFeedbackResponse(t, filtered, &filteredPage)
	if filtered.Code != http.StatusOK || len(filteredPage.Feedbacks) != 1 ||
		filteredPage.Feedbacks[0].ID != feedbackID || filteredPage.Feedbacks[0].AIStatus != "completed" {
		t.Fatalf("filtered tenant feedback = %d %#v", filtered.Code, filteredPage)
	}

	detailResponse := performTenantFeedbackRequest(
		handler, http.MethodGet, "/v1/feedbacks/"+feedbackID, "", ownerToken,
	)
	var detailEnvelope struct {
		Feedback Detail `json:"feedback"`
	}
	decodeFeedbackResponse(t, detailResponse, &detailEnvelope)
	if detailResponse.Code != http.StatusOK || len(detailEnvelope.Feedback.Attachments) != 1 ||
		detailEnvelope.Feedback.Attachments[0].Transcription == nil ||
		detailEnvelope.Feedback.Attachments[0].ContentURL == nil ||
		detailEnvelope.Feedback.Session == nil || !detailEnvelope.Feedback.Session.ReplayReady ||
		detailEnvelope.Feedback.Session.Status != "ready" || detailEnvelope.Feedback.Session.ReplayURL == nil {
		t.Fatalf("tenant feedback detail = %d %#v", detailResponse.Code, detailEnvelope.Feedback)
	}
	assertFeedbackTriageRead(t, detailEnvelope.Feedback.Item, assigneeID)
	unavailableDetailResponse := performTenantFeedbackRequest(
		handler, http.MethodGet, "/v1/feedbacks/"+secondFeedbackID, "", ownerToken,
	)
	var unavailableDetailEnvelope struct {
		Feedback Detail `json:"feedback"`
	}
	decodeFeedbackResponse(t, unavailableDetailResponse, &unavailableDetailEnvelope)
	unavailableSession := unavailableDetailEnvelope.Feedback.Session
	if unavailableDetailResponse.Code != http.StatusOK || unavailableSession == nil ||
		unavailableSession.Status != "failed" || unavailableSession.ReplayReady ||
		unavailableSession.MergeError == nil || unavailableSession.ReplayURL != nil {
		t.Fatalf("unavailable tenant feedback replay = %d %#v",
			unavailableDetailResponse.Code, unavailableSession)
	}

	streamStorage := &attachmentStreamStorageStub{data: []byte("audio-evidence")}
	streamHandler := NewAttachmentStreamHTTPHandler(AttachmentStreamHTTPOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Store: Store{}, JWT: jwtManager,
		Storage: streamStorage,
	})
	streamRoutes := chi.NewRouter()
	streamRoutes.Get("/v1/feedbacks/{feedbackID}/attachments/{attachmentID}/content", streamHandler.ServeHTTP)
	streamPath := "/v1/feedbacks/" + feedbackID + "/attachments/" + attachmentID + "/content"
	streamAPIKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"
	if _, err := root.Exec(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions)
		VALUES ($1, 'Attachment reader', $2, 'fk_live_0123...', '{feedback:read}')
	`, tenantID, auth.HashToken(streamAPIKey)); err != nil {
		t.Fatalf("create attachment stream API key: %v", err)
	}
	streamed := performTenantFeedbackRequest(streamRoutes, http.MethodGet, streamPath, "", ownerToken)
	if streamed.Code != http.StatusOK || streamed.Body.String() != "audio-evidence" ||
		streamed.Header().Get("Content-Type") != "audio/webm" ||
		streamed.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("attachment stream = %d headers=%#v body=%q", streamed.Code, streamed.Header(), streamed.Body.String())
	}
	if streamStorage.key == "" || !strings.Contains(streamStorage.key, attachmentID) {
		t.Errorf("streamed storage key = %q", streamStorage.key)
	}
	streamedWithAPIKey := performTenantFeedbackRequest(streamRoutes, http.MethodGet, streamPath, "", streamAPIKey)
	if streamedWithAPIKey.Code != http.StatusOK || streamedWithAPIKey.Body.String() != "audio-evidence" {
		t.Errorf("API-key attachment stream = %d body=%q", streamedWithAPIKey.Code, streamedWithAPIKey.Body.String())
	}
	rangeRequest := httptest.NewRequest(http.MethodGet, "http://acme.localhost"+streamPath, nil)
	rangeRequest.Header.Set("Authorization", "Bearer "+ownerToken)
	rangeRequest.Header.Set("Range", "bytes=0-4")
	rangeResponse := httptest.NewRecorder()
	streamRoutes.ServeHTTP(rangeResponse, rangeRequest)
	if rangeResponse.Code != http.StatusPartialContent || rangeResponse.Body.String() != "audio" {
		t.Errorf("attachment range = %d body=%q", rangeResponse.Code, rangeResponse.Body.String())
	}
	streamDenied := performTenantFeedbackRequest(streamRoutes, http.MethodGet, streamPath, "", deniedToken)
	if streamDenied.Code != http.StatusForbidden {
		t.Errorf("denied attachment stream status = %d", streamDenied.Code)
	}
	crossFeedback := performTenantFeedbackRequest(
		streamRoutes, http.MethodGet,
		"/v1/feedbacks/"+secondFeedbackID+"/attachments/"+attachmentID+"/content", "", ownerToken,
	)
	if crossFeedback.Code != http.StatusNotFound {
		t.Errorf("cross-feedback attachment status = %d", crossFeedback.Code)
	}

	dashboardResponse := performTenantFeedbackRequest(handler, http.MethodGet, "/v1/dashboard?range=7d", "", ownerToken)
	var dashboardEnvelope struct {
		Dashboard Dashboard `json:"dashboard"`
	}
	decodeFeedbackResponse(t, dashboardResponse, &dashboardEnvelope)
	if dashboardResponse.Code != http.StatusOK || dashboardEnvelope.Dashboard.FeedbackCount != 2 ||
		dashboardEnvelope.Dashboard.OpenIssueCount != 0 || dashboardEnvelope.Dashboard.AttentionCount != 1 ||
		dashboardEnvelope.Dashboard.Satisfaction == nil || *dashboardEnvelope.Dashboard.Satisfaction != 3.5 {
		t.Fatalf("tenant dashboard = %d %#v", dashboardResponse.Code, dashboardEnvelope.Dashboard)
	}

	updated := performTenantFeedbackRequest(
		handler, http.MethodPatch, "/v1/feedbacks/"+feedbackID,
		`{"status":"archived","title":"Checkout total changes unexpectedly"}`, ownerToken,
	)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"status":"archived"`) ||
		!strings.Contains(updated.Body.String(), `"title":"Checkout total changes unexpectedly"`) ||
		!strings.Contains(updated.Body.String(), `"description":"[MODERATED]"`) {
		t.Fatalf("update tenant feedback status = %d, body = %s", updated.Code, updated.Body.String())
	}
	var needsReviewTagID, otherTenantID, otherTagID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color)
		VALUES ($1, 'Needs review', '#DC6B2F') RETURNING id
	`, tenantID).Scan(&needsReviewTagID); err != nil {
		t.Fatalf("create bulk feedback tag: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Other', 'other-feedback', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant for bulk feedback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color)
		VALUES ($1, 'Private', '#111827') RETURNING id
	`, otherTenantID).Scan(&otherTagID); err != nil {
		t.Fatalf("create other tenant bulk feedback tag: %v", err)
	}
	bulkBody := fmt.Sprintf(`{
		"feedback_ids":[%q,%q],"status":"archived","priority":"high","assignee_id":%q,
		"add_tag_ids":[%q],"remove_tag_ids":[%q]
	}`, feedbackID, secondFeedbackID, assigneeID, needsReviewTagID, checkoutTagID)
	bulk := performTenantFeedbackRequest(
		handler, http.MethodPatch, "/v1/feedbacks/bulk", bulkBody, ownerToken,
	)
	if bulk.Code != http.StatusOK {
		t.Fatalf("bulk feedback update = %d body=%s", bulk.Code, bulk.Body.String())
	}
	var bulkEnvelope struct {
		UpdatedCount int    `json:"updated_count"`
		Feedbacks    []Item `json:"feedbacks"`
	}
	decodeFeedbackResponse(t, bulk, &bulkEnvelope)
	if bulkEnvelope.UpdatedCount != 2 || len(bulkEnvelope.Feedbacks) != 2 {
		t.Fatalf("bulk feedback result = %#v", bulkEnvelope)
	}
	for _, item := range bulkEnvelope.Feedbacks {
		if item.Status != StatusArchived || item.Priority == nil || string(*item.Priority) != "high" ||
			item.Assignee == nil || item.Assignee.ID != assigneeID {
			t.Fatalf("bulk feedback metadata = %#v", item)
		}
		if item.ID == feedbackID && (len(item.Tags) != 2 || item.Tags[0].Name != "Accessibility" ||
			item.Tags[1].Name != "Needs review") {
			t.Fatalf("first bulk feedback tags = %#v", item.Tags)
		}
		if item.ID == secondFeedbackID && (len(item.Tags) != 1 || item.Tags[0].Name != "Needs review") {
			t.Fatalf("second bulk feedback tags = %#v", item.Tags)
		}
	}
	failedBulk := performTenantFeedbackRequest(
		handler, http.MethodPatch, "/v1/feedbacks/bulk",
		fmt.Sprintf(`{"feedback_ids":[%q,%q],"status":"spam","priority":"urgent","unassign":true,"add_tag_ids":[%q]}`,
			feedbackID, secondFeedbackID, otherTagID), ownerToken,
	)
	if failedBulk.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant bulk feedback update = %d body=%s", failedBulk.Code, failedBulk.Body.String())
	}
	for _, itemID := range []string{feedbackID, secondFeedbackID} {
		response := performTenantFeedbackRequest(
			handler, http.MethodGet, "/v1/feedbacks/"+itemID, "", ownerToken,
		)
		var envelope struct {
			Feedback Detail `json:"feedback"`
		}
		decodeFeedbackResponse(t, response, &envelope)
		if response.Code != http.StatusOK || envelope.Feedback.Status != StatusArchived ||
			envelope.Feedback.Priority == nil || string(*envelope.Feedback.Priority) != "high" ||
			envelope.Feedback.Assignee == nil || envelope.Feedback.Assignee.ID != assigneeID {
			t.Fatalf("feedback changed after failed bulk update = %d %#v", response.Code, envelope.Feedback)
		}
	}
	singleTriage := performTenantFeedbackRequest(
		handler, http.MethodPatch, "/v1/feedbacks/"+feedbackID,
		fmt.Sprintf(`{"priority":"none","unassign":true,"add_tag_ids":[%q],"remove_tag_ids":[%q]}`,
			checkoutTagID, accessibilityTagID), ownerToken,
	)
	var singleEnvelope struct {
		Feedback Detail `json:"feedback"`
	}
	decodeFeedbackResponse(t, singleTriage, &singleEnvelope)
	if singleTriage.Code != http.StatusOK || singleEnvelope.Feedback.Priority != nil ||
		singleEnvelope.Feedback.Assignee != nil || len(singleEnvelope.Feedback.Tags) != 2 ||
		singleEnvelope.Feedback.Tags[0].Name != "Checkout" ||
		singleEnvelope.Feedback.Tags[1].Name != "Needs review" {
		t.Fatalf("single feedback triage update = %d %#v", singleTriage.Code, singleEnvelope.Feedback)
	}
	duplicateBulk := performTenantFeedbackRequest(
		handler, http.MethodPatch, "/v1/feedbacks/bulk",
		fmt.Sprintf(`{"feedback_ids":[%q,%q],"status":"open"}`, feedbackID, feedbackID), ownerToken,
	)
	if duplicateBulk.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate bulk feedback IDs = %d body=%s", duplicateBulk.Code, duplicateBulk.Body.String())
	}
	var linkTargetID, conflictIssueID, mergeTargetID, mergedIssueID string
	for publicID, destination := range map[string]*string{
		"ACME-2026-0100": &linkTargetID,
		"ACME-2026-0101": &conflictIssueID,
		"ACME-2026-0102": &mergeTargetID,
		"ACME-2026-0103": &mergedIssueID,
	} {
		if err := root.QueryRow(ctx, `
			INSERT INTO issues (tenant_id, project_id, public_id, title)
			VALUES ($1, $2, $3, 'Feedback link test') RETURNING id
		`, tenantID, projectID, publicID).Scan(destination); err != nil {
			t.Fatalf("create feedback link issue %s: %v", publicID, err)
		}
	}
	if _, err := root.Exec(ctx, `
		UPDATE issues SET merged_into_id = $2, merged_at = $3 WHERE id = $1
	`, mergedIssueID, mergeTargetID, now); err != nil {
		t.Fatalf("prepare merged feedback link issue: %v", err)
	}
	linkBody := fmt.Sprintf(`{"feedback_ids":[%q,%q],"issue_id":%q}`,
		feedbackID, secondFeedbackID, linkTargetID)
	linked := performTenantFeedbackRequest(handler, http.MethodPost, "/v1/feedbacks/link", linkBody, ownerToken)
	if linked.Code != http.StatusOK || !strings.Contains(linked.Body.String(), `"updated_count":2`) {
		t.Fatalf("link feedback = %d body=%s", linked.Code, linked.Body.String())
	}
	idempotentLink := performTenantFeedbackRequest(
		handler, http.MethodPost, "/v1/feedbacks/link", linkBody, ownerToken,
	)
	if idempotentLink.Code != http.StatusOK {
		t.Fatalf("idempotent feedback link = %d body=%s", idempotentLink.Code, idempotentLink.Body.String())
	}
	var linkActivityCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issue_activities
		WHERE tenant_id = $1 AND issue_id = $2 AND kind = 'feedback.linked'
	`, tenantID, linkTargetID).Scan(&linkActivityCount); err != nil || linkActivityCount != 1 {
		t.Fatalf("feedback link activity count = %d, error = %v, want 1", linkActivityCount, err)
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET issue_id = $2 WHERE id = $1`, secondFeedbackID, conflictIssueID); err != nil {
		t.Fatalf("prepare conflicting feedback link: %v", err)
	}
	conflictingLink := performTenantFeedbackRequest(
		handler, http.MethodPost, "/v1/feedbacks/link", linkBody, ownerToken,
	)
	if conflictingLink.Code != http.StatusConflict {
		t.Fatalf("conflicting feedback link = %d body=%s", conflictingLink.Code, conflictingLink.Body.String())
	}
	var firstIssueID, secondIssueID *string
	if err := root.QueryRow(ctx, `
		SELECT
			(SELECT issue_id FROM feedbacks WHERE id = $1),
			(SELECT issue_id FROM feedbacks WHERE id = $2)
	`, feedbackID, secondFeedbackID).Scan(&firstIssueID, &secondIssueID); err != nil {
		t.Fatalf("read feedback links after conflict: %v", err)
	}
	if firstIssueID == nil || *firstIssueID != linkTargetID || secondIssueID == nil || *secondIssueID != conflictIssueID {
		t.Fatalf("feedback links after conflict = %v/%v", firstIssueID, secondIssueID)
	}
	unlinkBody := fmt.Sprintf(`{"feedback_ids":[%q,%q]}`, feedbackID, secondFeedbackID)
	unlinked := performTenantFeedbackRequest(
		handler, http.MethodPost, "/v1/feedbacks/unlink", unlinkBody, ownerToken,
	)
	if unlinked.Code != http.StatusOK || !strings.Contains(unlinked.Body.String(), `"updated_count":2`) {
		t.Fatalf("unlink feedback = %d body=%s", unlinked.Code, unlinked.Body.String())
	}
	idempotentUnlink := performTenantFeedbackRequest(
		handler, http.MethodPost, "/v1/feedbacks/unlink", unlinkBody, ownerToken,
	)
	if idempotentUnlink.Code != http.StatusOK {
		t.Fatalf("idempotent feedback unlink = %d body=%s", idempotentUnlink.Code, idempotentUnlink.Body.String())
	}
	var unlinkActivityCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM issue_activities
		WHERE tenant_id = $1 AND kind = 'feedback.unlinked'
	`, tenantID).Scan(&unlinkActivityCount); err != nil || unlinkActivityCount != 2 {
		t.Fatalf("feedback unlink activity count = %d, error = %v, want 2", unlinkActivityCount, err)
	}
	var otherProjectID, otherProjectFeedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Other project', 'pk_live_ZYXWVUTSRQPONMLKJIHGFE', '{other.example.com}') RETURNING id
	`, tenantID).Scan(&otherProjectID); err != nil {
		t.Fatalf("create other feedback project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, page_url, text_process_status
		) VALUES ($1, $2, 'fb_Other001', gen_random_uuid(), 'https://other.example.com', 'skipped')
		RETURNING id
	`, tenantID, otherProjectID).Scan(&otherProjectFeedbackID); err != nil {
		t.Fatalf("create other project feedback: %v", err)
	}
	mixedProjectLink := performTenantFeedbackRequest(
		handler, http.MethodPost, "/v1/feedbacks/link",
		fmt.Sprintf(`{"feedback_ids":[%q,%q],"issue_id":%q}`,
			feedbackID, otherProjectFeedbackID, linkTargetID), ownerToken,
	)
	if mixedProjectLink.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mixed-project feedback link = %d body=%s", mixedProjectLink.Code, mixedProjectLink.Body.String())
	}
	mergedLink := performTenantFeedbackRequest(
		handler, http.MethodPost, "/v1/feedbacks/link",
		fmt.Sprintf(`{"feedback_ids":[%q],"issue_id":%q}`, feedbackID, mergedIssueID), ownerToken,
	)
	if mergedLink.Code != http.StatusConflict {
		t.Fatalf("merged issue feedback link = %d body=%s", mergedLink.Code, mergedLink.Body.String())
	}
	deniedLink := performTenantFeedbackRequest(
		handler, http.MethodPost, "/v1/feedbacks/link",
		fmt.Sprintf(`{"feedback_ids":[%q],"issue_id":%q}`, feedbackID, linkTargetID), feedbackOnlyToken,
	)
	if deniedLink.Code != http.StatusForbidden {
		t.Fatalf("feedback link without issue:update = %d body=%s", deniedLink.Code, deniedLink.Body.String())
	}
	invalidFilter := performTenantFeedbackRequest(handler, http.MethodGet, "/v1/feedbacks?rating=9", "", ownerToken)
	if invalidFilter.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid tenant feedback filter status = %d", invalidFilter.Code)
	}
	denied := performTenantFeedbackRequest(handler, http.MethodGet, "/v1/feedbacks", "", deniedToken)
	if denied.Code != http.StatusForbidden {
		t.Errorf("denied tenant feedback status = %d", denied.Code)
	}
}

func assertFeedbackTriageRead(t *testing.T, item Item, assigneeID string) {
	t.Helper()
	if item.Priority == nil || string(*item.Priority) != "urgent" || item.Assignee == nil ||
		item.Assignee.ID != assigneeID || item.Assignee.Name == nil || *item.Assignee.Name != "Ada Triage" ||
		item.Assignee.Email != "triage@acme.test" || len(item.Tags) != 2 ||
		item.Tags[0].Name != "Accessibility" || item.Tags[1].Name != "Checkout" {
		t.Fatalf("feedback triage metadata = %#v", item)
	}
}

type feedbackStorageStub struct {
	stat          storage.ObjectInfo
	data          []byte
	objects       map[string][]byte
	presignedKeys []string
	removedKeys   []string
	removed       bool
}

type attachmentStreamStorageStub struct {
	data []byte
	key  string
}

type seekReadCloser struct{ *bytes.Reader }

func (*seekReadCloser) Close() error { return nil }

func (s *attachmentStreamStorageStub) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	s.key = key
	return &seekReadCloser{Reader: bytes.NewReader(s.data)}, nil
}

type attachmentSchedulerStub struct {
	task VerificationTask
}

type submissionAnalyticsStoreStub struct {
	err error
}

func (s submissionAnalyticsStoreStub) InsertSubmittedEvent(
	context.Context,
	db.Querier,
	widgetanalytics.EventValues,
) (bool, error) {
	return false, s.err
}

func (s *attachmentSchedulerStub) ScheduleVerification(
	_ context.Context,
	task VerificationTask,
) error {
	s.task = task
	return nil
}

func (s *feedbackStorageStub) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	s.presignedKeys = append(s.presignedKeys, key)
	return "https://uploads.example.test/" + key, nil
}

func (s *feedbackStorageStub) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return s.stat, nil
}

func (s *feedbackStorageStub) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	if value, exists := s.objects[key]; exists {
		return io.NopCloser(bytes.NewReader(value)), nil
	}
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func (s *feedbackStorageStub) PutObject(
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
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = value
	return nil
}

func (s *feedbackStorageStub) RemoveObject(_ context.Context, key string) error {
	s.removed = true
	s.removedKeys = append(s.removedKeys, key)
	delete(s.objects, key)
	return nil
}

type staticLimiter struct {
	decision ratelimit.Decision
	err      error
}

type abuseEvaluatorFunc func(
	context.Context,
	abuse.Policy,
	abuse.Request,
) (abuse.Decision, error)

func (evaluate abuseEvaluatorFunc) Evaluate(
	ctx context.Context,
	policy abuse.Policy,
	request abuse.Request,
) (abuse.Decision, error) {
	return evaluate(ctx, policy, request)
}

type abuseDiagnosticRecorderStub struct {
	events []abuse.DiagnosticEvent
}

func (recorder *abuseDiagnosticRecorderStub) Record(
	_ context.Context,
	event abuse.DiagnosticEvent,
) error {
	recorder.events = append(recorder.events, event)
	return nil
}

type outboxEnqueuerStub struct {
	taskIDs []string
}

func (e *outboxEnqueuerStub) Enqueue(
	_ context.Context,
	event outbox.Event,
	destination outbox.Destination,
) error {
	e.taskIDs = append(e.taskIDs, outbox.TaskID(event, destination))
	return nil
}

func (l staticLimiter) Allow(context.Context, string, ratelimit.Window) (ratelimit.Decision, error) {
	return l.decision, l.err
}

// TestFeedbackConsentContractPersistsExactEvidenceAndFailsClosedOnContradiction proves the
// end-to-end explicit consent contract at the HTTP boundary: an opted-in submission persists the
// exact structured evidence (purpose, notice version, locale, source, server-authoritative
// timestamp), an anonymous submission persists no email and no consent, and every contradictory
// payload shape is rejected before anything is written.
func TestFeedbackConsentContractPersistsExactEvidenceAndFailsClosedOnContradiction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, feedbackMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}

	var tenantID, projectID, ruleID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	publicKey := "pk_live_CONSENTCONTRACTTEST001"
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', $2, '{example.com}') RETURNING id
	`, tenantID, publicKey).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds,
			can_write_text, collect_contact
		) VALUES ($1, 'All pages', 'all_pages', 'delay', 0, true, true)
		RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create widget rule: %v", err)
	}

	pools, err := db.OpenPools(ctx, feedbackRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	authorizer := widgetapi.Authorizer{Store: project.Store{}, Database: pools.Admin, Now: func() time.Time { return now }}
	handler := feedbackHandler(HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Now: func() time.Time { return now },
	})
	metadata := `"metadata":{"url":"https://example.com/checkout","viewport_width":1280,"viewport_height":720}`
	testStart := time.Now().Add(-time.Second)

	submit := func(clientFeedbackID, extra string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"client_feedback_id":%q,"widget_rule_id":%q,%s%s}`,
			clientFeedbackID, ruleID, metadata, extra)
		return performFeedbackRequest(
			handler, http.MethodPost, "/v1/feedback?key="+publicKey, body, publicKey, "https://example.com",
		)
	}

	optedIn := submit("00000000-0000-4000-8000-000000000001",
		`,"text":"Great!","reporter_email":"Reporter@Example.com","notify_reporter":true,`+
			`"consent":{"purpose":"feedback_updates","notice_version":"2026-07","locale":"en-US","granted":true}`,
	)
	if optedIn.Code != http.StatusCreated {
		t.Fatalf("opted-in submission status = %d, body = %s", optedIn.Code, optedIn.Body.String())
	}
	var optedInResponse SubmissionResponse
	decodeFeedbackResponse(t, optedIn, &optedInResponse)
	if !optedInResponse.NotifyReporter {
		t.Fatalf("opted-in response notify_reporter = %#v", optedInResponse)
	}

	var reporterEmail, purpose, noticeVersion, locale, source *string
	var notifyReporter bool
	var grantedAt, withdrawnAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT reporter_email, notify_reporter, reporter_consent_granted_at, reporter_consent_withdrawn_at,
		       reporter_consent_purpose, reporter_consent_notice_version, reporter_consent_locale,
		       reporter_consent_source
		FROM feedbacks WHERE id = $1
	`, optedInResponse.FeedbackID).Scan(
		&reporterEmail, &notifyReporter, &grantedAt, &withdrawnAt,
		&purpose, &noticeVersion, &locale, &source,
	); err != nil {
		t.Fatalf("read opted-in feedback row: %v", err)
	}
	if reporterEmail == nil || *reporterEmail != "reporter@example.com" || !notifyReporter ||
		grantedAt == nil || withdrawnAt != nil ||
		purpose == nil || *purpose != "feedback_updates" ||
		noticeVersion == nil || *noticeVersion != "2026-07" ||
		locale == nil || *locale != "en-US" ||
		source == nil || *source != "widget_submission" {
		t.Fatalf("opted-in consent evidence = email %v notify %t granted %v withdrawn %v "+
			"purpose %v version %v locale %v source %v",
			reporterEmail, notifyReporter, grantedAt, withdrawnAt, purpose, noticeVersion, locale, source)
	}
	// The client never sends a timestamp (ReporterConsentInput has no time field at all); the
	// database trigger stamps reporter_consent_granted_at from its own clock at INSERT time, so
	// this must land at real wall-clock submission time, never at the test's simulated business
	// clock (`now`, fixed to 2026-07-23T12:00:00Z) which only drives unrelated validation logic.
	if grantedAt.Before(testStart) {
		t.Errorf("consent granted_at = %v, want a server-authoritative timestamp at or after %v", grantedAt, testStart)
	}

	anonymous := submit("00000000-0000-4000-8000-000000000002", `,"text":"No contact please."`)
	if anonymous.Code != http.StatusCreated {
		t.Fatalf("anonymous submission status = %d, body = %s", anonymous.Code, anonymous.Body.String())
	}
	var anonymousResponse SubmissionResponse
	decodeFeedbackResponse(t, anonymous, &anonymousResponse)
	var anonymousEmail, anonymousPurpose *string
	var anonymousNotify bool
	if err := root.QueryRow(ctx, `
		SELECT reporter_email, notify_reporter, reporter_consent_purpose FROM feedbacks WHERE id = $1
	`, anonymousResponse.FeedbackID).Scan(&anonymousEmail, &anonymousNotify, &anonymousPurpose); err != nil {
		t.Fatalf("read anonymous feedback row: %v", err)
	}
	if anonymousEmail != nil || anonymousNotify || anonymousPurpose != nil {
		t.Fatalf("anonymous submission persisted = email %v notify %t purpose %v",
			anonymousEmail, anonymousNotify, anonymousPurpose)
	}

	contradictoryCases := []struct {
		name  string
		extra string
		field string
	}{
		{
			name:  "email without granted consent",
			extra: `,"reporter_email":"leftover@example.com","notify_reporter":false`,
			field: "reporter_email",
		},
		{
			name:  "consent without notify_reporter",
			extra: `,"consent":{"purpose":"feedback_updates","notice_version":"2026-07","locale":"en","granted":true}`,
			field: "consent",
		},
		{
			name:  "notify_reporter true without evidence",
			extra: `,"reporter_email":"reporter@example.com","notify_reporter":true`,
			field: "consent",
		},
		{
			name: "unknown notice version",
			extra: `,"reporter_email":"reporter@example.com","notify_reporter":true,` +
				`"consent":{"purpose":"feedback_updates","notice_version":"1999-01","locale":"en","granted":true}`,
			field: "consent.notice_version",
		},
	}
	for index, testCase := range contradictoryCases {
		t.Run(testCase.name, func(t *testing.T) {
			clientFeedbackID := fmt.Sprintf("00000000-0000-4000-8000-0000000000%02d", 10+index)
			rejected := submit(clientFeedbackID, testCase.extra)
			if rejected.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, body = %s, want 422", rejected.Code, rejected.Body.String())
			}
			var errorBody struct {
				Error struct {
					Details []FieldError `json:"details"`
				} `json:"error"`
			}
			decodeFeedbackResponse(t, rejected, &errorBody)
			if !hasField(errorBody.Error.Details, testCase.field) {
				t.Fatalf("fields = %#v, missing %q", errorBody.Error.Details, testCase.field)
			}
			var count int
			if err := root.QueryRow(ctx, `
				SELECT count(*) FROM feedbacks WHERE tenant_id = $1 AND client_feedback_id = $2
			`, tenantID, clientFeedbackID).Scan(&count); err != nil {
				t.Fatalf("count rejected feedback rows: %v", err)
			}
			if count != 0 {
				t.Fatalf("rejected payload %q persisted a row", testCase.name)
			}
		})
	}
}

func feedbackHandler(options HTTPOptions) http.Handler {
	router := chi.NewRouter()
	RegisterRoutes(router, options)
	return router
}

func performFeedbackRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	publicKey string,
	origin string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://widget.localhost"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(widgetapi.PublicKeyHeader, publicKey)
	request.Header.Set("Origin", origin)
	handler.ServeHTTP(recorder, request)
	return recorder
}

func performTenantFeedbackRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	accessToken string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://acme.localhost"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeFeedbackResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, recorder.Body.String())
	}
}

func addHoneypot(t *testing.T, body string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode feedback fixture: %v", err)
	}
	payload["company_website"] = "https://bot.test"
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode feedback fixture: %v", err)
	}
	return string(encoded)
}

func assertAttachmentVerificationStoreLifecycle(
	t *testing.T,
	ctx context.Context,
	database *pgx.Conn,
	tenantID string,
	feedbackID string,
	now time.Time,
) {
	t.Helper()
	store := VerificationStore{Database: database}
	var attachmentID string
	if err := database.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at, safety_status,
			upload_refreshed_at, upload_expires_at, verification_generation
			) VALUES (
				$1, $2, 'screenshot', gen_random_uuid(), $3,
				'image/png', 8, 'pending', $4, 'quarantined',
				$4::timestamptz - interval '10 minutes', $4::timestamptz - interval '2 minutes', 1
		)
		RETURNING id
	`, tenantID, feedbackID, "verification/quarantined.png", now).Scan(&attachmentID); err != nil {
		t.Fatalf("create quarantined attachment: %v", err)
	}
	task := VerificationTask{
		TenantID: tenantID, AttachmentID: attachmentID, Generation: 1,
	}
	if err := store.MarkEnqueued(ctx, task, now.Add(-20*time.Minute)); err != nil {
		t.Fatalf("MarkEnqueued() error = %v", err)
	}
	prepared, err := store.Prepare(ctx, now, 10)
	if err != nil || len(prepared) != 1 || prepared[0].AttachmentID != attachmentID ||
		prepared[0].Generation != 2 {
		t.Fatalf("Prepare() = %#v, %v", prepared, err)
	}
	task = prepared[0]
	if err := store.UndoEnqueued(ctx, task); err != nil {
		t.Fatalf("UndoEnqueued() error = %v", err)
	}
	if err := store.MarkEnqueued(ctx, task, now); err != nil {
		t.Fatalf("second MarkEnqueued() error = %v", err)
	}
	work, err := store.Claim(ctx, task)
	if err != nil || work.AttachmentID != attachmentID || work.Attempt != 1 ||
		work.SafetyStatus != AttachmentSafetyQuarantined {
		t.Fatalf("Claim() = %#v, %v", work, err)
	}
	detected := "image/jpeg"
	if err := store.MarkRejected(ctx, work, "signature_mismatch", &detected, now); err != nil {
		t.Fatalf("MarkRejected() error = %v", err)
	}
	if err := store.CompleteRejectedCleanup(ctx, work, now); err != nil {
		t.Fatalf("CompleteRejectedCleanup() error = %v", err)
	}
	noWork := task
	noWork.Generation++
	if err := store.MarkEnqueued(ctx, noWork, now); !errors.Is(err, ErrNoVerificationWork) {
		t.Fatalf("stale MarkEnqueued() error = %v", err)
	}
	if err := store.CompleteRejectedCleanup(ctx, VerificationWork{
		VerificationTask: noWork,
	}, now); !errors.Is(err, ErrNoVerificationWork) {
		t.Fatalf("stale CompleteRejectedCleanup() error = %v", err)
	}
	var safeAttachmentID string
	if err := database.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at, safety_status,
			upload_refreshed_at, upload_expires_at, safety_checked_at,
			detected_content_type, verification_generation, verification_enqueued_at
		) VALUES (
			$1, $2, 'screenshot', gen_random_uuid(), $3,
			'image/png', 8, 'pending', $4, 'safe',
			$4::timestamptz - interval '10 minutes', $4::timestamptz - interval '2 minutes', $4,
			'image/png', 1, $4::timestamptz - interval '2 minutes'
		)
		RETURNING id
	`, tenantID, feedbackID, "verification/safe.png.verified-1", now).Scan(
		&safeAttachmentID,
	); err != nil {
		t.Fatalf("create safe attachment cleanup fixture: %v", err)
	}
	safeWork := VerificationWork{VerificationTask: VerificationTask{
		TenantID: tenantID, AttachmentID: safeAttachmentID, Generation: 1,
	}}
	if err := store.CompleteSafeCleanup(ctx, safeWork, now); err != nil {
		t.Fatalf("CompleteSafeCleanup() error = %v", err)
	}
	if err := store.CompleteSafeCleanup(ctx, safeWork, now); !errors.Is(err, ErrNoVerificationWork) {
		t.Fatalf("repeated CompleteSafeCleanup() error = %v", err)
	}
	if _, err := database.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, safeAttachmentID); err != nil {
		t.Fatalf("delete safe attachment cleanup fixture: %v", err)
	}
	if _, err := store.Prepare(ctx, now, 0); err == nil {
		t.Fatal("Prepare(invalid limit) error = nil")
	}

	var pendingID string
	if err := database.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, complete_token_hash, safety_status,
			upload_refreshed_at, upload_expires_at, verification_generation
		) VALUES (
			$1, $2, 'audio', gen_random_uuid(), $3,
			'audio/webm', 4, 'pending', 'hash', 'pending',
			$4::timestamptz - interval '20 minutes', $4::timestamptz - interval '10 minutes', 1
		)
		RETURNING id
	`, tenantID, feedbackID, "verification/expired.webm", now).Scan(&pendingID); err != nil {
		t.Fatalf("create pending attachment: %v", err)
	}
	if _, err := store.ClaimExpired(ctx, now, 1001); err == nil {
		t.Fatal("ClaimExpired(invalid limit) error = nil")
	}
	expired, err := store.ClaimExpired(ctx, now, 10)
	if err != nil || len(expired) != 1 || expired[0].AttachmentID != pendingID {
		t.Fatalf("ClaimExpired() = %#v, %v", expired, err)
	}
	var cleaningStatus string
	if err := database.QueryRow(ctx,
		`SELECT safety_status FROM attachments WHERE id = $1`, pendingID,
	).Scan(&cleaningStatus); err != nil || cleaningStatus != "cleaning" {
		t.Fatalf("claimed attachment status = %q, %v", cleaningStatus, err)
	}
	if err := store.CompleteExpiry(ctx, expired[0], now); err != nil {
		t.Fatalf("CompleteExpiry() error = %v", err)
	}
	var safetyStatus, processingStatus string
	var completeTokenHash *string
	if err := database.QueryRow(ctx, `
		SELECT safety_status, processing_status, complete_token_hash
		FROM attachments WHERE id = $1
	`, pendingID).Scan(&safetyStatus, &processingStatus, &completeTokenHash); err != nil {
		t.Fatalf("read expired attachment: %v", err)
	}
	if safetyStatus != "expired" || processingStatus != "failed" || completeTokenHash != nil {
		t.Fatalf("expired attachment state = %s/%s/%v",
			safetyStatus, processingStatus, completeTokenHash)
	}
	if _, err := database.Exec(ctx, `
		DELETE FROM attachments WHERE id IN ($1, $2)
	`, attachmentID, pendingID); err != nil {
		t.Fatalf("remove verification lifecycle fixtures: %v", err)
	}
}

func assertFeedbackRows(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	feedbackID string,
	tenantID string,
	projectID string,
) {
	t.Helper()
	var issues, feedbacks, attachments, events, issueEvents int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM issues WHERE tenant_id = $1 AND project_id = $2`, tenantID, projectID).
		Scan(&issues); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM feedbacks WHERE id = $1`, feedbackID).Scan(&feedbacks); err != nil {
		t.Fatalf("count feedback: %v", err)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM attachments WHERE feedback_id = $1`, feedbackID).Scan(&attachments); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events
		WHERE event = 'feedback.created' AND resource_type = 'feedback' AND resource_id = $1
	`, feedbackID).Scan(&events); err != nil {
		t.Fatalf("count outbox events: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM outbox_events AS event
		JOIN issues AS issue ON issue.id = event.resource_id
		WHERE event.event = 'issue.created' AND event.resource_type = 'issue'
		  AND issue.tenant_id = $1 AND issue.project_id = $2
	`, tenantID, projectID).Scan(&issueEvents); err != nil {
		t.Fatalf("count issue-created outbox events: %v", err)
	}
	var title, responses string
	var description *string
	var definitionVersion *int
	if err := root.QueryRow(ctx, `SELECT title, description, responses::text, widget_definition_version FROM feedbacks WHERE id = $1`, feedbackID).
		Scan(&title, &description, &responses, &definitionVersion); err != nil {
		t.Fatalf("read feedback copy: %v", err)
	}
	if title != "Checkout was clear" || description == nil || *description != "Checkout was clear" {
		t.Errorf("feedback title/description = %q/%v", title, description)
	}
	if !strings.Contains(responses, `"rating": 5`) || !strings.Contains(responses, `"feedback": "Checkout was clear"`) ||
		definitionVersion == nil || *definitionVersion != 1 {
		t.Errorf("feedback response context = %s version %v", responses, definitionVersion)
	}
	if issues != 0 || feedbacks != 1 || attachments != 1 || events != 1 || issueEvents != 0 {
		t.Fatalf("row counts: issues=%d feedbacks=%d attachments=%d events=%d issue_events=%d",
			issues, feedbacks, attachments, events, issueEvents)
	}
}

func feedbackMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	return path
}

func feedbackRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
