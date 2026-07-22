package feedback

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
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
	storageClient := &feedbackStorageStub{stat: storage.ObjectInfo{Size: 2048, ContentType: "audio/webm"}}
	scheduler := &attachmentSchedulerStub{}
	authorizer := widgetapi.Authorizer{Store: project.Store{}, Database: pools.Admin, Now: func() time.Time { return now }}
	handler := feedbackHandler(HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Storage: storageClient, Scheduler: scheduler, Now: func() time.Time { return now },
	})

	clientFeedbackID := "57e3ee52-4ebf-43ce-9e31-9f909628634d"
	clientAttachmentID := "61c18d72-2296-43fa-bd21-a1356aa5da1c"
	body := fmt.Sprintf(`{
		"client_feedback_id":%q,"widget_rule_id":%q,"session_id":%q,"rating":5,
		"text":"Checkout was clear","element_attachment":{"selector":"#checkout","html":"<section>Checkout</section>","styles":{"display":"grid"}},
		"attachments":[{"client_attachment_id":%q,"kind":"audio","content_type":"audio/webm","size_bytes":2048}],
		"metadata":{"url":"https://example.com/checkout","referrer":"https://example.com/","user_agent":"browser","os":"Linux","browser":"Firefox","device":"desktop","viewport_width":1280,"viewport_height":720,"language":"en"}
	}`, clientFeedbackID, ruleID, replaySessionID, clientAttachmentID)

	created := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey, body, publicKey, "https://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
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
		    processed_at = $2, upload_refreshed_at = $2
		WHERE id = $1
	`, first.Attachments[0].AttachmentID, now.Add(-time.Hour)); err != nil {
		t.Fatalf("expire attachment before retry: %v", err)
	}

	idempotent := performFeedbackRequest(handler, http.MethodPost, "/v1/feedback?key="+publicKey, body, publicKey, "https://example.com")
	if idempotent.Code != http.StatusOK {
		t.Fatalf("idempotent status = %d, body = %s", idempotent.Code, idempotent.Body.String())
	}
	var second SubmissionResponse
	decodeFeedbackResponse(t, idempotent, &second)
	if second.FeedbackID != first.FeedbackID || second.IssuePublicID != first.IssuePublicID {
		t.Errorf("idempotent response = %#v, first = %#v", second, first)
	}
	if len(second.Attachments) != 1 || second.Attachments[0].CompleteToken == first.Attachments[0].CompleteToken {
		t.Errorf("idempotent attachment token was not rotated")
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
	var completedStatus string
	var completedProcessedAt *time.Time
	if err := root.QueryRow(ctx,
		`SELECT complete_token_hash, completed_at, processing_status, error_message, processed_at
		 FROM attachments WHERE id = $1`,
		second.Attachments[0].AttachmentID,
	).Scan(&tokenHash, &completedAt, &completedStatus, &completedError, &completedProcessedAt); err != nil {
		t.Fatalf("read completed attachment: %v", err)
	}
	if tokenHash != nil || completedAt == nil || completedStatus != "pending" ||
		completedError != nil || completedProcessedAt != nil {
		t.Errorf("completed attachment state = token %#v at %#v status %s error %#v processed %#v",
			tokenHash, completedAt, completedStatus, completedError, completedProcessedAt)
	}
	if scheduler.attachmentID != second.Attachments[0].AttachmentID || scheduler.feedbackID != first.FeedbackID {
		t.Errorf("scheduled attachment/feedback = %q/%q", scheduler.attachmentID, scheduler.feedbackID)
	}
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
	var assigneeID, checkoutTagID, accessibilityTagID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name)
		VALUES ($1, 'triage@acme.test', 'Ada Triage') RETURNING id
	`, tenantID).Scan(&assigneeID); err != nil {
		t.Fatalf("create feedback assignee: %v", err)
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
	ownerToken, _, err := jwtManager.IssueTenant("owner-id", tenantID, auth.TenantPermissionStrings())
	if err != nil {
		t.Fatalf("issue tenant feedback token: %v", err)
	}
	deniedToken, _, err := jwtManager.IssueTenant("viewer-id", tenantID, []string{string(auth.PermissionProjectRead)})
	if err != nil {
		t.Fatalf("issue denied tenant feedback token: %v", err)
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
		!strings.Contains(updated.Body.String(), `"description":"Checkout was clear"`) {
		t.Fatalf("update tenant feedback status = %d, body = %s", updated.Code, updated.Body.String())
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
	stat storage.ObjectInfo
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
	attachmentID string
	feedbackID   string
}

func (s *attachmentSchedulerStub) ScheduleAttachment(
	_ context.Context,
	attachmentID string,
	feedbackID string,
) error {
	s.attachmentID = attachmentID
	s.feedbackID = feedbackID
	return nil
}

func (s *feedbackStorageStub) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://uploads.example.test/" + key, nil
}

func (s *feedbackStorageStub) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return s.stat, nil
}

type staticLimiter struct {
	decision ratelimit.Decision
	err      error
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
	var title string
	var description *string
	if err := root.QueryRow(ctx, `SELECT title, description FROM feedbacks WHERE id = $1`, feedbackID).
		Scan(&title, &description); err != nil {
		t.Fatalf("read feedback copy: %v", err)
	}
	if title != "Checkout was clear" || description == nil || *description != "Checkout was clear" {
		t.Errorf("feedback title/description = %q/%v", title, description)
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
