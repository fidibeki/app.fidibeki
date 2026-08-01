package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestReplayHTTPFlow(t *testing.T) {
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
	migrator, err := db.NewMigrator(directURL, replayMigrationsPath(t))
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
	publicKey := "pk_live_REPLAYABCDEFGHIJKLMNOP"
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', $2, '{example.com}') RETURNING id
	`, tenantID, publicKey).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds,
			replay_mode, replay_sampling_rate, replay_max_duration_seconds,
			replay_max_events, replay_mask_all_inputs, replay_consent
		) VALUES (
			$1, 'Replay', 'all_pages', 'delay', 0, 'full', 0, 1800, 50000, true,
			'{"mode":"customer_cmp","notice_version":"2026-07","supported_locales":["en"],"expiry_days":30}'::jsonb
		)
		RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create widget rule: %v", err)
	}

	pools, err := db.OpenPools(ctx, replayRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	storageClient := &replayStorageStub{
		objects: map[string][]byte{}, contentTypes: map[string]string{}, statErrors: map[string]error{},
	}
	mergeQueue := &replayMergeQueueStub{}
	abuseEvaluator := &replayAbuseEvaluatorStub{}
	diagnostics := &replayDiagnosticRecorderStub{}
	scheduler := Scheduler{
		Store: Store{Database: pools.Admin}, Queue: mergeQueue, VerificationQueue: mergeQueue,
		Storage: storageClient, Now: func() time.Time { return now },
	}
	authorizer := widgetapi.Authorizer{
		Store: project.Store{}, Database: pools.Admin, Now: func() time.Time { return now },
	}
	router := chi.NewRouter()
	RegisterPublicRoutes(router, HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Storage: storageClient, Scheduler: scheduler, Verifier: scheduler,
		Abuse: abuseEvaluator, Diagnostics: diagnostics,
		Now: func() time.Time { return now },
	})

	preflight := replayRequest(router, http.MethodOptions, "/v1/replay/sessions?key="+publicKey, "", "", "https://example.com")
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("preflight status = %d, headers = %#v", preflight.Code, preflight.Header())
	}
	clientSessionID := "d766f8eb-d58d-42ca-b9fd-349cc9e8e3b4"
	visitorID := "e8b3bb8c-e7ce-4c06-b804-c40f1db21355"
	missingConsentBody := fmt.Sprintf(
		`{"session_id":%q,"widget_rule_id":%q,"page_url":"https://example.com/checkout","visitor_id":%q}`,
		clientSessionID, ruleID, visitorID,
	)
	missingConsent := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		missingConsentBody, publicKey, "https://example.com")
	if missingConsent.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(missingConsent.Body.String(), `"field":"consent_token"`) {
		t.Fatalf("missing replay consent = %d/%s", missingConsent.Code, missingConsent.Body.String())
	}
	var sessionsWithoutConsent int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&sessionsWithoutConsent); err != nil ||
		sessionsWithoutConsent != 0 {
		t.Fatalf("sessions without consent = %d, error = %v", sessionsWithoutConsent, err)
	}
	consentToken, _, err := auth.NewOpaqueToken()
	if err != nil {
		t.Fatalf("generate replay consent token: %v", err)
	}
	malformedConsent := replayRequest(
		router,
		http.MethodPost,
		"/v1/replay/consents?key="+publicKey,
		`{`,
		publicKey,
		"https://example.com",
	)
	if malformedConsent.Code != http.StatusBadRequest {
		t.Fatalf(
			"malformed replay consent = %d/%s",
			malformedConsent.Code,
			malformedConsent.Body.String(),
		)
	}
	invalidConsent := replayRequest(
		router,
		http.MethodPost,
		"/v1/replay/consents?key="+publicKey,
		fmt.Sprintf(
			`{"widget_rule_id":%q,"purpose":"analytics","notice_version":"outdated",`+
				`"locale":"fr","source":"fidibeki_managed","state":"granted","grant_token":%q}`,
			ruleID,
			consentToken,
		),
		publicKey,
		"https://example.com",
	)
	if invalidConsent.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"invalid replay consent = %d/%s",
			invalidConsent.Code,
			invalidConsent.Body.String(),
		)
	}
	for _, field := range []string{"purpose", "notice_version", "locale", "source"} {
		if !strings.Contains(invalidConsent.Body.String(), `"field":"`+field+`"`) {
			t.Fatalf("invalid replay consent omitted %s: %s", field, invalidConsent.Body.String())
		}
	}
	consentRequestBody := fmt.Sprintf(
		`{"widget_rule_id":%q,"purpose":"session_replay","notice_version":"2026-07",`+
			`"locale":"en","source":"customer_cmp","consent_id":"cmp-reference-1",`+
			`"state":"granted","grant_token":%q}`,
		ruleID, consentToken,
	)
	consent := replayRequest(router, http.MethodPost, "/v1/replay/consents?key="+publicKey,
		consentRequestBody, publicKey, "https://example.com")
	if consent.Code != http.StatusCreated {
		t.Fatalf("grant replay consent = %d/%s", consent.Code, consent.Body.String())
	}
	var consentResponse struct {
		Token     string    `json:"token"`
		State     string    `json:"state"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	decodeReplayResponse(t, consent, &consentResponse)
	if consentResponse.Token != consentToken || consentResponse.State != "active" ||
		!consentResponse.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("consent response = %#v", consentResponse)
	}
	repeatedConsent := replayRequest(
		router,
		http.MethodPost,
		"/v1/replay/consents?key="+publicKey,
		consentRequestBody,
		publicKey,
		"https://example.com",
	)
	if repeatedConsent.Code != http.StatusCreated {
		t.Fatalf(
			"idempotent replay consent = %d/%s",
			repeatedConsent.Code,
			repeatedConsent.Body.String(),
		)
	}
	conflictingConsent := replayRequest(
		router,
		http.MethodPost,
		"/v1/replay/consents?key="+publicKey,
		strings.Replace(consentRequestBody, "cmp-reference-1", "cmp-reference-2", 1),
		publicKey,
		"https://example.com",
	)
	if conflictingConsent.Code != http.StatusConflict {
		t.Fatalf(
			"conflicting idempotency scope = %d/%s",
			conflictingConsent.Code,
			conflictingConsent.Body.String(),
		)
	}
	var consentRows int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM replay_consents WHERE token_hash = $1
	`, auth.HashToken(consentToken)).Scan(&consentRows); err != nil || consentRows != 1 {
		t.Fatalf("idempotent replay consent rows = %d, error = %v", consentRows, err)
	}
	var replayConsentID string
	if err := root.QueryRow(ctx, `
		SELECT id FROM replay_consents WHERE token_hash = $1
	`, auth.HashToken(consentToken)).Scan(&replayConsentID); err != nil {
		t.Fatalf("read replay consent evidence: %v", err)
	}
	createBody := fmt.Sprintf(
		`{"session_id":%q,"widget_rule_id":%q,"page_url":"https://example.com/checkout",`+
			`"visitor_id":%q,"consent_token":%q}`,
		clientSessionID, ruleID, visitorID, consentResponse.Token,
	)
	created := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		createBody, publicKey, "https://example.com")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createResponse struct {
		ID                 string `json:"id"`
		SessionID          string `json:"session_id"`
		RRWebVersion       string `json:"rrweb_version"`
		MaxDurationSeconds int    `json:"max_duration_seconds"`
		MaxEvents          int    `json:"max_events"`
	}
	decodeReplayResponse(t, created, &createResponse)
	if createResponse.ID == "" || createResponse.SessionID != clientSessionID ||
		createResponse.RRWebVersion != RRWebVersion || createResponse.MaxDurationSeconds != 1800 ||
		createResponse.MaxEvents != 50000 {
		t.Fatalf("create response = %#v", createResponse)
	}
	if len(abuseEvaluator.calls) != 1 ||
		abuseEvaluator.calls[0].policy.Name != abuse.ReplayStartPolicy.Name ||
		abuseEvaluator.calls[0].request.Identity.TenantID != tenantID ||
		abuseEvaluator.calls[0].request.Identity.ProjectID != projectID ||
		abuseEvaluator.calls[0].request.Identity.Origin != "example.com" ||
		abuseEvaluator.calls[0].request.Identity.Network != "192.0.2.1" ||
		abuseEvaluator.calls[0].request.Identity.VisitorID != visitorID ||
		abuseEvaluator.calls[0].request.Identity.SessionID != clientSessionID {
		t.Fatalf("replay start abuse call = %#v", abuseEvaluator.calls)
	}
	var storedVisitorIDColumns int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sessions' AND column_name = 'visitor_id'
	`).Scan(&storedVisitorIDColumns); err != nil || storedVisitorIDColumns != 0 {
		t.Fatalf("sessions table visitor_id columns = %d, error = %v, want 0 (persistent visitor ID must stay isolated from replay artifacts)",
			storedVisitorIDColumns, err)
	}
	if strings.Contains(created.Body.String(), visitorID) {
		t.Fatalf("replay session creation response leaked the visitor ID: %s", created.Body.String())
	}

	// The widget should already canonicalize page_url, but the backend enforces the same
	// contract independently (issue #53): credentials, query parameters, and the fragment must
	// never reach persistence or the response even if an old/compromised client sends them raw.
	canonicalizationSessionID := "a24b6e05-6a94-4fd0-9c4c-2a4e7cf1c8f0"
	dirtyURL := "https://sentinel-user:sentinel-pass@example.com/checkout/" +
		"?email=jane.doe%40example.com&token=secret-token#fragment-secret"
	canonicalizationBody := fmt.Sprintf(
		`{"session_id":%q,"widget_rule_id":%q,"page_url":%q,"consent_token":%q}`,
		canonicalizationSessionID, ruleID, dirtyURL, consentResponse.Token,
	)
	canonicalized := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		canonicalizationBody, publicKey, "https://example.com")
	if canonicalized.Code != http.StatusCreated {
		t.Fatalf("canonicalization create status = %d, body = %s", canonicalized.Code, canonicalized.Body.String())
	}
	var storedPageURL string
	if err := root.QueryRow(ctx,
		`SELECT page_url FROM sessions WHERE session_id = $1`, canonicalizationSessionID,
	).Scan(&storedPageURL); err != nil {
		t.Fatalf("read canonicalized replay page URL: %v", err)
	}
	if storedPageURL != "https://example.com/" {
		t.Fatalf("stored replay page URL = %q, want the all-pages rule-safe origin", storedPageURL)
	}
	for _, sentinel := range []string{"sentinel-user", "sentinel-pass", "jane.doe", "secret-token", "fragment-secret"} {
		if strings.Contains(canonicalized.Body.String(), sentinel) {
			t.Fatalf("replay session response leaked sentinel URL content %q: %s", sentinel, canonicalized.Body.String())
		}
	}
	foreignPageSessionID := "af32b9ea-c011-42fd-8cdb-c214a26b4959"
	foreignPageBody := fmt.Sprintf(
		`{"session_id":%q,"widget_rule_id":%q,"page_url":"https://other.example/private","consent_token":%q}`,
		foreignPageSessionID, ruleID, consentResponse.Token,
	)
	foreignPage := replayRequest(
		router,
		http.MethodPost,
		"/v1/replay/sessions?key="+publicKey,
		foreignPageBody,
		publicKey,
		"https://example.com",
	)
	if foreignPage.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(foreignPage.Body.String(), `"field":"page_url"`) {
		t.Fatalf(
			"foreign replay page = %d/%s, want page_url validation",
			foreignPage.Code,
			foreignPage.Body.String(),
		)
	}
	rateLimitedSessionID := "5bc697f3-229d-44b8-b9ca-d3e236ad592b"
	rateLimitedBody := strings.Replace(createBody, clientSessionID, rateLimitedSessionID, 1)
	abuseEvaluator.denyPolicy = abuse.ReplayStartPolicy.Name
	rateLimited := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		rateLimitedBody, publicKey, "https://example.com")
	abuseEvaluator.denyPolicy = ""
	if rateLimited.Code != http.StatusTooManyRequests ||
		rateLimited.Header().Get("Retry-After") != "3" ||
		!strings.Contains(rateLimited.Body.String(), `"reason":"replay_rate_limited"`) {
		t.Fatalf("rate-limited replay start = %d/%s/%s",
			rateLimited.Code, rateLimited.Header().Get("Retry-After"), rateLimited.Body.String())
	}
	var rateLimitedRows int
	if err := root.QueryRow(ctx,
		`SELECT count(*) FROM sessions WHERE session_id = $1`, rateLimitedSessionID,
	).Scan(&rateLimitedRows); err != nil || rateLimitedRows != 0 {
		t.Fatalf("rate-limited replay rows = %d, error = %v", rateLimitedRows, err)
	}
	unavailableSessionID := "da3a13c1-538e-4308-b6f4-22760b7d9186"
	unavailableBody := strings.Replace(createBody, clientSessionID, unavailableSessionID, 1)
	abuseEvaluator.failPolicy = abuse.ReplayStartPolicy.Name
	unavailable := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		unavailableBody, publicKey, "https://example.com")
	abuseEvaluator.failPolicy = ""
	if unavailable.Code != http.StatusServiceUnavailable ||
		unavailable.Header().Get("Retry-After") != "5" ||
		!strings.Contains(unavailable.Body.String(), `"reason":"replay_rate_limited"`) {
		t.Fatalf("unavailable replay limiter = %d/%s/%s",
			unavailable.Code, unavailable.Header().Get("Retry-After"), unavailable.Body.String())
	}
	if len(diagnostics.events) != 2 ||
		diagnostics.events[0].Reason != abuse.ReasonReplayRateLimited ||
		diagnostics.events[1].Reason != abuse.ReplayStartPolicy.Name+"_unavailable" {
		t.Fatalf("replay abuse diagnostics = %#v", diagnostics.events)
	}
	invalidOrigin := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		createBody, publicKey, "https://evil.example")
	if invalidOrigin.Code != http.StatusForbidden {
		t.Errorf("invalid origin status = %d", invalidOrigin.Code)
	}
	malformed := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		`{`, publicKey, "https://example.com")
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("malformed create status = %d", malformed.Code)
	}
	invalidCreate := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		`{"session_id":"bad","widget_rule_id":"bad","page_url":"javascript:alert(1)"}`,
		publicKey, "https://example.com")
	if invalidCreate.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(invalidCreate.Body.String(), `"field":"session_id"`) {
		t.Errorf("invalid create status = %d, body = %s", invalidCreate.Code, invalidCreate.Body.String())
	}
	missingBatch := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/00000000-0000-4000-8000-000000000001/batches?key="+publicKey,
		`{"batch_index":0,"events_count":1,"size_bytes":2}`, publicKey, "https://example.com")
	if missingBatch.Code != http.StatusNotFound {
		t.Errorf("missing session batch status = %d", missingBatch.Code)
	}
	invalidBatch := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+createResponse.ID+"/batches?key="+publicKey,
		`{"batch_index":1000,"events_count":51,"size_bytes":0}`, publicKey, "https://example.com")
	if invalidBatch.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid batch status = %d, body = %s", invalidBatch.Code, invalidBatch.Body.String())
	}

	batchData := []byte(`[{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0}}},{"type":3,"timestamp":1784800000100,"data":{"source":5}}]`)
	batchBody := fmt.Sprintf(`{"batch_index":0,"events_count":2,"size_bytes":%d}`, len(batchData))
	declared := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+createResponse.ID+"/batches?key="+publicKey,
		batchBody, publicKey, "https://example.com")
	if declared.Code != http.StatusCreated {
		t.Fatalf("declare status = %d, body = %s", declared.Code, declared.Body.String())
	}
	if len(abuseEvaluator.calls) < 2 {
		t.Fatalf("replay batch abuse calls = %#v", abuseEvaluator.calls)
	}
	batchCalls := abuseEvaluator.calls[len(abuseEvaluator.calls)-2:]
	if batchCalls[0].policy.Name != abuse.ReplayBatchPolicy.Name ||
		batchCalls[0].request.Identity.SessionID != createResponse.ID ||
		batchCalls[0].request.Cost != 1 ||
		batchCalls[1].policy.Name != abuse.ReplayBytePolicy.Name ||
		batchCalls[1].request.Cost != 1 {
		t.Fatalf("replay batch abuse calls = %#v", batchCalls)
	}
	var upload BatchUpload
	decodeReplayResponse(t, declared, &upload)
	if upload.CompleteToken == "" || upload.Method != http.MethodPut ||
		!strings.Contains(upload.PresignedURL, createResponse.ID) {
		t.Fatalf("batch upload = %#v", upload)
	}
	if len(storageClient.presignedKeys) != 1 {
		t.Fatalf("presigned keys = %#v", storageClient.presignedKeys)
	}
	storageClient.objects[storageClient.presignedKeys[0]] = batchData
	storageClient.contentTypes[storageClient.presignedKeys[0]] = "application/json"

	completePath := "/v1/replay/sessions/" + createResponse.ID + "/batches/0/complete?key=" + publicKey
	denied := replayRequest(router, http.MethodPost, completePath,
		`{"complete_token":"wrong"}`, publicKey, "https://example.com")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("invalid token status = %d, body = %s", denied.Code, denied.Body.String())
	}
	completed := replayRequest(router, http.MethodPost, completePath,
		fmt.Sprintf(`{"complete_token":%q}`, upload.CompleteToken), publicKey, "https://example.com")
	if completed.Code != http.StatusNoContent {
		t.Fatalf("complete status = %d, body = %s", completed.Code, completed.Body.String())
	}
	if len(mergeQueue.verificationTasks) != 1 {
		t.Fatalf("scheduled replay batch verifications = %#v", mergeQueue.verificationTasks)
	}
	var quarantinedStatus string
	var quarantinedEvents, quarantinedBatches int
	if err := root.QueryRow(ctx, `
		SELECT batch.safety_status::text, session.events_count, session.batches_count
		FROM session_batches AS batch
		JOIN sessions AS session ON session.id = batch.session_id
		WHERE batch.session_id = $1 AND batch.batch_index = 0
	`, createResponse.ID).Scan(
		&quarantinedStatus, &quarantinedEvents, &quarantinedBatches,
	); err != nil {
		t.Fatalf("read quarantined replay batch: %v", err)
	}
	if quarantinedStatus != string(BatchSafetyQuarantined) ||
		quarantinedEvents != 0 || quarantinedBatches != 0 {
		t.Fatalf("quarantined replay state = %s/%d/%d",
			quarantinedStatus, quarantinedEvents, quarantinedBatches)
	}
	quarantinedFinalize := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+createResponse.ID+"/finalize?key="+publicKey,
		`{}`, publicKey, "https://example.com")
	if quarantinedFinalize.Code != http.StatusAccepted ||
		!strings.Contains(quarantinedFinalize.Body.String(), `"status":"processing"`) {
		t.Fatalf("quarantined replay finalize = %d, body = %s",
			quarantinedFinalize.Code, quarantinedFinalize.Body.String())
	}
	sealedDeclaration := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+createResponse.ID+"/batches?key="+publicKey,
		fmt.Sprintf(`{"batch_index":1,"events_count":1,"size_bytes":%d}`, len(batchData)),
		publicKey, "https://example.com")
	if sealedDeclaration.Code != http.StatusConflict {
		t.Fatalf("declare batch after finalization request = %d, body = %s",
			sealedDeclaration.Code, sealedDeclaration.Body.String())
	}
	verificationProcessor := BatchVerificationProcessor{
		Store: Store{Database: pools.Admin}, Storage: storageClient,
		Now: func() time.Time { return now },
	}
	verificationPayload, err := json.Marshal(mergeQueue.verificationTasks[0])
	if err != nil {
		t.Fatalf("encode replay batch verification task: %v", err)
	}
	if err := verificationProcessor.Handler()(
		ctx, asynq.NewTask(TaskVerifyBatch, verificationPayload),
	); err != nil {
		t.Fatalf("verify replay batch: %v", err)
	}
	verifiedKey := verifiedBatchKey(
		storageClient.presignedKeys[0], mergeQueue.verificationTasks[0].Generation, 1,
	)
	var persistedVerifiedKey string
	var safeCleanupEnqueuedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT storage_key, verification_enqueued_at
		FROM session_batches
		WHERE session_id = $1 AND batch_index = 0
	`, createResponse.ID).Scan(&persistedVerifiedKey, &safeCleanupEnqueuedAt); err != nil {
		t.Fatalf("read verified replay batch storage: %v", err)
	}
	if persistedVerifiedKey != verifiedKey || safeCleanupEnqueuedAt == nil ||
		!bytes.Equal(storageClient.objects[verifiedKey], batchData) {
		t.Fatalf("verified replay promotion = key %q marker %v body %q",
			persistedVerifiedKey, safeCleanupEnqueuedAt, storageClient.objects[verifiedKey])
	}
	if _, exists := storageClient.objects[storageClient.presignedKeys[0]]; exists {
		t.Fatal("replay staging object remained after promotion")
	}
	storageClient.objects[storageClient.presignedKeys[0]] =
		[]byte(`[{"type":99,"timestamp":1784800000400,"data":{}}]`)
	storageClient.contentTypes[storageClient.presignedKeys[0]] = "application/json"
	if err := scheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("reconcile verified replay finalization: %v", err)
	}
	alreadyCompleted := replayRequest(router, http.MethodPost, completePath,
		fmt.Sprintf(`{"complete_token":%q}`, upload.CompleteToken), publicKey, "https://example.com")
	if alreadyCompleted.Code != http.StatusConflict {
		t.Errorf("already-complete status = %d, body = %s", alreadyCompleted.Code, alreadyCompleted.Body.String())
	}
	finalized := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+createResponse.ID+"/finalize?key="+publicKey,
		`{}`, publicKey, "https://example.com")
	if finalized.Code != http.StatusAccepted || !strings.Contains(finalized.Body.String(), `"status":"processing"`) {
		t.Fatalf("finalize status = %d, body = %s", finalized.Code, finalized.Body.String())
	}
	repeatedFinalize := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+createResponse.ID+"/finalize?key="+publicKey,
		`{}`, publicKey, "https://example.com")
	if repeatedFinalize.Code != http.StatusAccepted {
		t.Errorf("repeat finalize status = %d, body = %s", repeatedFinalize.Code, repeatedFinalize.Body.String())
	}
	if len(mergeQueue.sessionIDs) != 1 || mergeQueue.sessionIDs[0] != createResponse.ID {
		t.Errorf("scheduled sessions = %#v, want %q", mergeQueue.sessionIDs, createResponse.ID)
	}
	var events, batches int
	var endedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT events_count, batches_count, ended_at FROM sessions WHERE id = $1
	`, createResponse.ID).Scan(&events, &batches, &endedAt); err != nil {
		t.Fatalf("read finalized session: %v", err)
	}
	if events != 2 || batches != 1 || endedAt == nil {
		t.Errorf("session aggregates = events %d batches %d ended %v", events, batches, endedAt)
	}
	processor := Processor{
		Store: Store{Database: pools.Admin}, Storage: storageClient,
		Now: func() time.Time { return now },
	}
	mergeTask := asynq.NewTask(
		TaskMerge, []byte(fmt.Sprintf(`{"session_id":%q}`, createResponse.ID)),
	)
	storageClient.removeErrors = map[string]error{
		verifiedKey: errors.New("storage cleanup unavailable"),
	}
	if err := processor.MergeHandler()(ctx, mergeTask); err == nil ||
		!strings.Contains(err.Error(), "remove merged replay batch") {
		t.Fatalf("merge cleanup failure = %v", err)
	}
	storageClient.removeErrors = nil
	if err := processor.MergeHandler()(ctx, mergeTask); err != nil {
		t.Fatalf("retry merged replay task: %v", err)
	}
	mergedKey := "tenants/" + tenantID + "/sessions/" + createResponse.ID + "/replay.json"
	if string(storageClient.objects[mergedKey]) != string(batchData) {
		t.Fatalf("merged replay = %s, want %s", storageClient.objects[mergedKey], batchData)
	}
	var batchRows int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM session_batches WHERE session_id = $1
	`, createResponse.ID).Scan(&batchRows); err != nil || batchRows != 1 {
		t.Fatalf("batch row before upload URL expiry = %d, error = %v", batchRows, err)
	}
	var concurrentSessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, rrweb_version,
			max_duration_seconds, max_events, finalize_requested_at, replay_consent_id
		) VALUES ($1, $2, gen_random_uuid(), $3, '2.1.1', 1800, 50000, $4, $5)
		RETURNING id
	`, tenantID, projectID, now.Add(-time.Minute), now, replayConsentID).Scan(&concurrentSessionID); err != nil {
		t.Fatalf("create concurrent verification session: %v", err)
	}
	works := make([]BatchVerificationWork, 0, 2)
	for index, eventCount := range []int{2, 3} {
		batch, err := scanBatchRow(root.QueryRow(ctx, `
			INSERT INTO session_batches (
				tenant_id, project_id, session_id, batch_index, storage_key,
				events_count, size_bytes, completed_at, upload_refreshed_at,
				safety_status, upload_expires_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, 64, $7, $7,
				'quarantined', $7::timestamptz + interval '10 minutes'
			)
			RETURNING id, tenant_id, project_id, session_id, batch_index, storage_key,
			          events_count, size_bytes, content_type, complete_token_hash, completed_at,
			          safety_status, upload_expires_at, safety_checked_at, safety_error_reason,
			          verification_generation, verification_enqueued_at, verification_attempts
		`, tenantID, projectID, concurrentSessionID, index,
			fmt.Sprintf("replay/concurrent-%d.json", index), eventCount, now,
		))
		if err != nil {
			t.Fatalf("create concurrent verification batch: %v", err)
		}
		works = append(works, BatchVerificationWork{Batch: batch})
	}
	concurrentStore := Store{Database: pools.Admin}
	var wait sync.WaitGroup
	errorsByWorker := make(chan error, len(works))
	for _, work := range works {
		wait.Add(1)
		go func(work BatchVerificationWork) {
			defer wait.Done()
			errorsByWorker <- concurrentStore.MarkSafe(
				ctx, work, verifiedBatchKey(work.StorageKey, work.Generation, 1), now,
			)
		}(work)
	}
	wait.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		if err != nil {
			t.Fatalf("concurrent MarkSafe() error = %v", err)
		}
	}
	var concurrentEvents, concurrentBatches int
	var concurrentEndedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT events_count, batches_count, ended_at
		FROM sessions WHERE id = $1
	`, concurrentSessionID).Scan(
		&concurrentEvents, &concurrentBatches, &concurrentEndedAt,
	); err != nil {
		t.Fatalf("read concurrent replay counters: %v", err)
	}
	if concurrentEvents != 5 || concurrentBatches != 2 || concurrentEndedAt == nil {
		t.Fatalf("concurrent replay state = events %d batches %d ended %v",
			concurrentEvents, concurrentBatches, concurrentEndedAt)
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, concurrentSessionID); err != nil {
		t.Fatalf("delete concurrent verification session: %v", err)
	}
	emptyCreateBody := fmt.Sprintf(
		`{"session_id":"66d6b8fc-f79d-46bb-9a46-a8db9cb7ea0c","widget_rule_id":%q,`+
			`"page_url":"https://example.com/empty","consent_token":%q}`,
		ruleID, consentResponse.Token,
	)
	emptyCreated := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		emptyCreateBody, publicKey, "https://example.com")
	if emptyCreated.Code != http.StatusCreated {
		t.Fatalf("create empty session status = %d, body = %s", emptyCreated.Code, emptyCreated.Body.String())
	}
	var emptySession struct {
		ID string `json:"id"`
	}
	decodeReplayResponse(t, emptyCreated, &emptySession)
	emptyFinalize := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+emptySession.ID+"/finalize?key="+publicKey,
		`{}`, publicKey, "https://example.com")
	if emptyFinalize.Code != http.StatusConflict {
		t.Errorf("empty finalize status = %d, body = %s", emptyFinalize.Code, emptyFinalize.Body.String())
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, emptySession.ID); err != nil {
		t.Fatalf("delete empty session: %v", err)
	}

	expiredClientSessionID := "081a7356-97ee-4c89-b7da-44a07a94b80f"
	expiredCreateBody := strings.Replace(createBody, clientSessionID, expiredClientSessionID, 1)
	expiredCreated := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		expiredCreateBody, publicKey, "https://example.com")
	if expiredCreated.Code != http.StatusCreated {
		t.Fatalf("create expiring replay status = %d, body = %s",
			expiredCreated.Code, expiredCreated.Body.String())
	}
	var expiringSession struct {
		ID string `json:"id"`
	}
	decodeReplayResponse(t, expiredCreated, &expiringSession)
	expiringData := []byte(`[{"type":3,"timestamp":1784800000200,"data":{"source":5}}]`)
	expiringDeclared := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+expiringSession.ID+"/batches?key="+publicKey,
		fmt.Sprintf(`{"batch_index":0,"events_count":1,"size_bytes":%d}`, len(expiringData)),
		publicKey, "https://example.com")
	if expiringDeclared.Code != http.StatusCreated {
		t.Fatalf("declare expiring replay status = %d, body = %s",
			expiringDeclared.Code, expiringDeclared.Body.String())
	}
	var expiringUpload BatchUpload
	decodeReplayResponse(t, expiringDeclared, &expiringUpload)
	expiringKey := storageClient.presignedKeys[len(storageClient.presignedKeys)-1]
	storageClient.objects[expiringKey] = expiringData
	storageClient.contentTypes[expiringKey] = "application/json"
	now = now.Add(replayUploadTTL + time.Second)
	expiredCompletion := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+expiringSession.ID+"/batches/0/complete?key="+publicKey,
		fmt.Sprintf(`{"complete_token":%q}`, expiringUpload.CompleteToken),
		publicKey, "https://example.com")
	if expiredCompletion.Code != http.StatusGone ||
		!strings.Contains(expiredCompletion.Body.String(), `"reason":"upload_expired"`) {
		t.Fatalf("expired replay completion = %d, body = %s",
			expiredCompletion.Code, expiredCompletion.Body.String())
	}
	var expiredStatus string
	var expiredToken *string
	var expiredCompletedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT safety_status::text, complete_token_hash, completed_at
		FROM session_batches WHERE session_id = $1 AND batch_index = 0
	`, expiringSession.ID).Scan(&expiredStatus, &expiredToken, &expiredCompletedAt); err != nil {
		t.Fatalf("read expired replay batch: %v", err)
	}
	if expiredStatus != string(BatchSafetyCleaning) ||
		expiredToken != nil || expiredCompletedAt != nil {
		t.Fatalf("claimed replay expiry state = %s/%v/%v",
			expiredStatus, expiredToken, expiredCompletedAt)
	}
	if _, exists := storageClient.objects[expiringKey]; !exists ||
		slices.Contains(storageClient.removedKeys, expiringKey) {
		t.Fatalf("HTTP expiry cleanup = exists %v removed %#v",
			exists, storageClient.removedKeys)
	}
	now = now.Add(uploadCleanupGrace)
	if err := scheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("reconcile expired replay cleanup: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT safety_status::text
		FROM session_batches WHERE session_id = $1 AND batch_index = 0
	`, expiringSession.ID).Scan(&expiredStatus); err != nil {
		t.Fatalf("read reconciled replay expiry: %v", err)
	}
	if _, exists := storageClient.objects[expiringKey]; expiredStatus != string(BatchSafetyExpired) ||
		exists || !slices.Contains(storageClient.removedKeys, expiringKey) {
		t.Fatalf("reconciled replay expiry = status %s exists %v removed %#v",
			expiredStatus, exists, storageClient.removedKeys)
	}
	var verifiedCleanupTask BatchVerificationTask
	for _, candidate := range mergeQueue.verificationTasks {
		if candidate.BatchID == mergeQueue.verificationTasks[0].BatchID &&
			candidate.Generation > mergeQueue.verificationTasks[0].Generation {
			verifiedCleanupTask = candidate
		}
	}
	if verifiedCleanupTask.BatchID == "" {
		t.Fatalf("verified replay cleanup task was not scheduled: %#v",
			mergeQueue.verificationTasks)
	}
	verifiedCleanupPayload, err := json.Marshal(verifiedCleanupTask)
	if err != nil {
		t.Fatalf("encode verified replay cleanup task: %v", err)
	}
	if err := verificationProcessor.Handler()(
		ctx, asynq.NewTask(TaskVerifyBatch, verifiedCleanupPayload),
	); err != nil {
		t.Fatalf("complete verified replay cleanup: %v", err)
	}
	if _, exists := storageClient.objects[storageClient.presignedKeys[0]]; exists {
		t.Fatal("reused replay staging object remained after presigned URL expiry")
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM session_batches WHERE session_id = $1
	`, createResponse.ID).Scan(&batchRows); err != nil || batchRows != 0 {
		t.Fatalf("merged batch rows after staging cleanup = %d, error = %v", batchRows, err)
	}
	redeclaredExpired := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+expiringSession.ID+"/batches?key="+publicKey,
		fmt.Sprintf(`{"batch_index":0,"events_count":1,"size_bytes":%d}`, len(expiringData)),
		publicKey, "https://example.com")
	if redeclaredExpired.Code != http.StatusCreated {
		t.Fatalf("redeclare expired replay = %d, body = %s",
			redeclaredExpired.Code, redeclaredExpired.Body.String())
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, expiringSession.ID); err != nil {
		t.Fatalf("delete expired replay session: %v", err)
	}

	rejectedClientSessionID := "2fc177cf-a0b5-4237-9a52-a06655945e70"
	rejectedCreateBody := strings.Replace(createBody, clientSessionID, rejectedClientSessionID, 1)
	rejectedCreated := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		rejectedCreateBody, publicKey, "https://example.com")
	if rejectedCreated.Code != http.StatusCreated {
		t.Fatalf("create rejected replay status = %d, body = %s",
			rejectedCreated.Code, rejectedCreated.Body.String())
	}
	var rejectedSession struct {
		ID string `json:"id"`
	}
	decodeReplayResponse(t, rejectedCreated, &rejectedSession)
	rejectedData := []byte(
		`[{"type":2,"timestamp":1784800000300,"data":{"node":` +
			`{"type":2,"id":1,"tagName":"img","attributes":` +
			`{"alt":"SENTINEL-Jane-Doe","src":"https://cdn.example/SENTINEL-jane@example.com"}}}}]`,
	)
	rejectedDeclared := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+rejectedSession.ID+"/batches?key="+publicKey,
		fmt.Sprintf(`{"batch_index":0,"events_count":1,"size_bytes":%d}`, len(rejectedData)),
		publicKey, "https://example.com")
	if rejectedDeclared.Code != http.StatusCreated {
		t.Fatalf("declare rejected replay status = %d, body = %s",
			rejectedDeclared.Code, rejectedDeclared.Body.String())
	}
	var rejectedUpload BatchUpload
	decodeReplayResponse(t, rejectedDeclared, &rejectedUpload)
	rejectedKey := storageClient.presignedKeys[len(storageClient.presignedKeys)-1]
	storageClient.objects[rejectedKey] = rejectedData
	storageClient.contentTypes[rejectedKey] = "application/json"
	rejectedCompletion := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+rejectedSession.ID+"/batches/0/complete?key="+publicKey,
		fmt.Sprintf(`{"complete_token":%q}`, rejectedUpload.CompleteToken),
		publicKey, "https://example.com")
	if rejectedCompletion.Code != http.StatusNoContent {
		t.Fatalf("complete rejected replay status = %d, body = %s",
			rejectedCompletion.Code, rejectedCompletion.Body.String())
	}
	rejectedPendingFinalize := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+rejectedSession.ID+"/finalize?key="+publicKey,
		`{}`, publicKey, "https://example.com")
	if rejectedPendingFinalize.Code != http.StatusAccepted ||
		!strings.Contains(rejectedPendingFinalize.Body.String(), `"status":"processing"`) {
		t.Fatalf("quarantined rejected replay finalize = %d, body = %s",
			rejectedPendingFinalize.Code, rejectedPendingFinalize.Body.String())
	}
	rejectedTask := mergeQueue.verificationTasks[len(mergeQueue.verificationTasks)-1]
	rejectedPayload, err := json.Marshal(rejectedTask)
	if err != nil {
		t.Fatalf("encode rejected replay task: %v", err)
	}
	rejectionProcessor := BatchVerificationProcessor{
		Store: Store{Database: pools.Admin}, Storage: storageClient, Diagnostics: diagnostics,
		Now: func() time.Time { return now },
	}
	if err := rejectionProcessor.Handler()(
		ctx, asynq.NewTask(TaskVerifyBatch, rejectedPayload),
	); err != nil {
		t.Fatalf("reject unsafe replay batch: %v", err)
	}
	var rejectedStatus, rejectedReason, rejectedFailureCode string
	var rejectedEnqueuedAt, rejectedEndedAt *time.Time
	var rejectedMergeAttempts int
	if err := root.QueryRow(ctx, `
		SELECT batch.safety_status::text, batch.safety_error_reason,
		       batch.verification_enqueued_at, session.failure_code,
		       session.ended_at, session.merge_attempts
		FROM session_batches AS batch
		JOIN sessions AS session ON session.id = batch.session_id
		WHERE batch.session_id = $1 AND batch.batch_index = 0
	`, rejectedSession.ID).Scan(
		&rejectedStatus, &rejectedReason, &rejectedEnqueuedAt, &rejectedFailureCode,
		&rejectedEndedAt, &rejectedMergeAttempts,
	); err != nil {
		t.Fatalf("read rejected replay batch: %v", err)
	}
	if rejectedStatus != string(BatchSafetyRejected) ||
		rejectedReason != "invalid_event" || rejectedEnqueuedAt == nil ||
		rejectedFailureCode != string(FailureUnsafePayload) || rejectedEndedAt == nil ||
		rejectedMergeAttempts != terminalMergeAttempts {
		t.Fatalf("rejected replay state = %s/%s/%v/%s/%v/%d",
			rejectedStatus, rejectedReason, rejectedEnqueuedAt, rejectedFailureCode,
			rejectedEndedAt, rejectedMergeAttempts)
	}
	if _, exists := storageClient.objects[rejectedKey]; exists {
		t.Fatal("unsafe replay object was not removed")
	}
	rejectedFinalize := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+rejectedSession.ID+"/finalize?key="+publicKey,
		`{}`, publicKey, "https://example.com")
	if rejectedFinalize.Code != http.StatusAccepted ||
		!strings.Contains(rejectedFinalize.Body.String(), `"status":"failed"`) {
		t.Fatalf("rejected replay finalize = %d, body = %s",
			rejectedFinalize.Code, rejectedFinalize.Body.String())
	}
	redeclaredRejected := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+rejectedSession.ID+"/batches?key="+publicKey,
		fmt.Sprintf(`{"batch_index":0,"events_count":1,"size_bytes":%d}`, len(rejectedData)),
		publicKey, "https://example.com")
	if redeclaredRejected.Code != http.StatusConflict {
		t.Fatalf("redeclare rejected replay before cleanup = %d, body = %s",
			redeclaredRejected.Code, redeclaredRejected.Body.String())
	}
	now = now.Add(replayUploadTTL + uploadCleanupGrace)
	if err := scheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("schedule rejected replay final cleanup: %v", err)
	}
	var cleanupTask BatchVerificationTask
	for _, candidate := range mergeQueue.verificationTasks {
		if candidate.BatchID == rejectedTask.BatchID && candidate.Generation > rejectedTask.Generation {
			cleanupTask = candidate
		}
	}
	if cleanupTask.BatchID == "" {
		t.Fatalf("rejected replay cleanup task was not scheduled: %#v",
			mergeQueue.verificationTasks)
	}
	cleanupPayload, err := json.Marshal(cleanupTask)
	if err != nil {
		t.Fatalf("encode rejected replay cleanup task: %v", err)
	}
	if err := rejectionProcessor.Handler()(
		ctx, asynq.NewTask(TaskVerifyBatch, cleanupPayload),
	); err != nil {
		t.Fatalf("complete rejected replay cleanup: %v", err)
	}
	redeclaredRejected = replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+rejectedSession.ID+"/batches?key="+publicKey,
		fmt.Sprintf(`{"batch_index":0,"events_count":1,"size_bytes":%d}`, len(rejectedData)),
		publicKey, "https://example.com")
	if redeclaredRejected.Code != http.StatusConflict {
		t.Fatalf("redeclare rejected replay after cleanup = %d, body = %s",
			redeclaredRejected.Code, redeclaredRejected.Body.String())
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, rejectedSession.ID); err != nil {
		t.Fatalf("delete rejected replay session: %v", err)
	}

	var aggregateSessionID string
	aggregateClientSessionID := "f571c651-e016-4760-90b5-c5d7df660fc7"
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, page_url, started_at,
			rrweb_version, max_duration_seconds, max_events, replay_consent_id
		) VALUES ($1, $2, $3, 'https://example.com/aggregate', $4, $5, 1800, 50000, $6)
		RETURNING id
	`, tenantID, projectID, aggregateClientSessionID, now, RRWebVersion, replayConsentID).Scan(&aggregateSessionID); err != nil {
		t.Fatalf("create aggregate-limit replay: %v", err)
	}
	if _, err := root.Exec(ctx, `
			INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, content_type, complete_token_hash,
			upload_refreshed_at, upload_expires_at
		)
		SELECT $1::uuid, $2::uuid, $3::uuid, value,
		       'aggregate/' || ($3::uuid)::text || '/' || value::text || '.json',
		       1, $4, 'application/json', 'seed-token-' || value::text,
		       $5, $5::timestamptz + interval '10 minutes'
		FROM generate_series(0, 62) AS value
	`, tenantID, projectID, aggregateSessionID, maximumBatchSize, now); err != nil {
		t.Fatalf("seed aggregate-limit replay batches: %v", err)
	}
	startConcurrentDeclarations := make(chan struct{})
	declarationResults := make(chan error, 2)
	for _, batchIndex := range []int{63, 64} {
		go func(index int) {
			<-startConcurrentDeclarations
			tx, err := pools.Admin.Begin(ctx)
			if err == nil {
				_, _, err = (Store{}).DeclareBatch(
					ctx, tx, tenantID, projectID, aggregateSessionID,
					index, 1, maximumBatchSize, now,
				)
			}
			if err == nil {
				err = tx.Commit(ctx)
			} else if tx != nil {
				_ = tx.Rollback(ctx)
			}
			declarationResults <- err
		}(batchIndex)
	}
	close(startConcurrentDeclarations)
	var successfulDeclarations, byteLimitRejections int
	for range 2 {
		switch err := <-declarationResults; {
		case err == nil:
			successfulDeclarations++
		case errors.Is(err, ErrByteLimit):
			byteLimitRejections++
		default:
			t.Fatalf("concurrent aggregate-limit declaration: %v", err)
		}
	}
	var aggregateBatches int
	var aggregateBytes int64
	if err := root.QueryRow(ctx, `
		SELECT count(*), sum(size_bytes)
		FROM session_batches WHERE session_id = $1
	`, aggregateSessionID).Scan(&aggregateBatches, &aggregateBytes); err != nil {
		t.Fatalf("read aggregate-limit replay: %v", err)
	}
	if successfulDeclarations != 1 || byteLimitRejections != 1 ||
		aggregateBatches != 64 || aggregateBytes != maximumSessionBytes {
		t.Fatalf("aggregate-limit result = success %d rejected %d batches %d bytes %d",
			successfulDeclarations, byteLimitRejections, aggregateBatches, aggregateBytes)
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, aggregateSessionID); err != nil {
		t.Fatalf("delete aggregate-limit replay session: %v", err)
	}

	var replayMemberID, restrictedMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES (
			$1, 'replay@acme.test',
			ARRAY['session:read', 'session:delete', 'widget_rule:read', 'feedback:update']
		) RETURNING id
	`, tenantID).Scan(&replayMemberID); err != nil {
		t.Fatalf("create replay member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'restricted@acme.test', '{}') RETURNING id
	`, tenantID).Scan(&restrictedMemberID); err != nil {
		t.Fatalf("create restricted replay member: %v", err)
	}

	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Hour)
	if err != nil {
		t.Fatalf("create tenant JWT manager: %v", err)
	}
	accessToken, _, err := jwtManager.IssueTenant(replayMemberID, tenantID, []string{
		string(auth.PermissionSessionRead), string(auth.PermissionSessionDelete),
		string(auth.PermissionWidgetRuleRead), string(auth.PermissionFeedbackUpdate),
	})
	if err != nil {
		t.Fatalf("issue tenant access token: %v", err)
	}
	tenantRoutes := chi.NewRouter()
	RegisterTenantRoutes(tenantRoutes, TenantHTTPOptions{
		Store: Store{}, JWT: jwtManager, Storage: storageClient, Scheduler: &scheduler,
		Now: func() time.Time { return now },
	})
	controlPlaneLimiter := &replayAPIKeyLimiterStub{counts: map[string]int{}}
	tenantHandler := appmiddleware.TenantAPIKeyRateLimit(
		appmiddleware.TenantAPIKeyRateLimitOptions{Limiter: controlPlaneLimiter},
	)(appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(tenantRoutes))
	metadata := tenantReplayRequest(tenantHandler, http.MethodGet, "/v1/sessions/"+createResponse.ID, accessToken)
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"status":"ready"`) ||
		!strings.Contains(metadata.Body.String(), `"replay_url"`) {
		t.Fatalf("metadata status = %d, body = %s", metadata.Code, metadata.Body.String())
	}
	if strings.Contains(metadata.Body.String(), "visitor_id") || strings.Contains(metadata.Body.String(), visitorID) {
		t.Fatalf("backoffice replay session metadata leaked the visitor ID: %s", metadata.Body.String())
	}
	diagnosticPath := "/v1/projects/" + projectID + "/widget-rules/" + ruleID +
		"/replay-consent-diagnostic"
	diagnostic := tenantReplayRequest(tenantHandler, http.MethodGet, diagnosticPath, accessToken)
	if diagnostic.Code != http.StatusOK ||
		!strings.Contains(diagnostic.Body.String(), `"state":"active"`) ||
		!strings.Contains(diagnostic.Body.String(), `"notice_version":"2026-07"`) ||
		!strings.Contains(diagnostic.Body.String(), `"latest_session_started_at"`) {
		t.Fatalf("replay consent diagnostic = %d/%s", diagnostic.Code, diagnostic.Body.String())
	}
	for _, forbidden := range []string{"token", "customer_consent_reference", visitorID} {
		if strings.Contains(diagnostic.Body.String(), forbidden) {
			t.Fatalf("replay consent diagnostic leaked %q: %s", forbidden, diagnostic.Body.String())
		}
	}
	controlAPIKey := "fk_live_" + strings.Repeat("C", 32)
	if _, err := root.Exec(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions)
		VALUES (
			$1, 'Replay controller', $2, 'fk_live_CCCC...',
			ARRAY['session:read', 'session:delete', 'widget_rule:read']
		)
	`, tenantID, auth.HashToken(controlAPIKey)); err != nil {
		t.Fatalf("create replay control-plane API key: %v", err)
	}
	removedBeforeStatusDenials := len(storageClient.removedKeys)
	expiredTrial := now.Add(-time.Hour)
	statusTests := []struct {
		name      string
		status    string
		trialEnds *time.Time
	}{
		{name: "suspended", status: "suspended"},
		{name: "cancelled", status: "cancelled"},
		{name: "expired trial", status: "trial", trialEnds: &expiredTrial},
	}
	for _, testCase := range statusTests {
		t.Run("control plane rejects "+testCase.name, func(t *testing.T) {
			if _, err := root.Exec(ctx, `
				UPDATE tenants
				SET status = $2, trial_ends_at = $3, deletion_requested_at = NULL
				WHERE id = $1
			`, tenantID, testCase.status, testCase.trialEnds); err != nil {
				t.Fatalf("set tenant status: %v", err)
			}
			requests := []struct {
				name   string
				method string
				path   string
				token  string
			}{
				{
					name: "JWT metadata", method: http.MethodGet,
					path: "/v1/sessions/" + createResponse.ID, token: accessToken,
				},
				{
					name: "API key diagnostic", method: http.MethodGet,
					path: diagnosticPath, token: controlAPIKey,
				},
				{
					name: "API key deletion", method: http.MethodDelete,
					path: "/v1/sessions/" + createResponse.ID, token: controlAPIKey,
				},
			}
			for _, request := range requests {
				response := tenantReplayRequest(
					tenantHandler, request.method, request.path, request.token,
				)
				if response.Code != http.StatusForbidden ||
					!strings.Contains(response.Body.String(), `"code":"tenant_suspended"`) {
					t.Errorf("%s response = %d/%s, want tenant_suspended",
						request.name, response.Code, response.Body.String())
				}
			}
			var sessionExists bool
			if err := root.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM sessions WHERE tenant_id = $1 AND id = $2)
			`, tenantID, createResponse.ID).Scan(&sessionExists); err != nil {
				t.Fatalf("check replay after status denial: %v", err)
			}
			if !sessionExists || len(storageClient.removedKeys) != removedBeforeStatusDenials {
				t.Fatalf("status denial changed replay: exists=%t removed=%#v",
					sessionExists, storageClient.removedKeys)
			}
		})
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants
		SET status = 'suspended', trial_ends_at = NULL, deletion_requested_at = $2
		WHERE id = $1
	`, tenantID, now); err != nil {
		t.Fatalf("enter replay deletion grace: %v", err)
	}
	deletionGraceMetadata := tenantReplayRequest(
		tenantHandler, http.MethodGet, "/v1/sessions/"+createResponse.ID, accessToken,
	)
	deletionGraceDiagnostic := tenantReplayRequest(
		tenantHandler, http.MethodGet, diagnosticPath, controlAPIKey,
	)
	deletionGraceDelete := tenantReplayRequest(
		tenantHandler, http.MethodDelete, "/v1/sessions/"+createResponse.ID, controlAPIKey,
	)
	if deletionGraceMetadata.Code != http.StatusOK ||
		deletionGraceDiagnostic.Code != http.StatusOK ||
		deletionGraceDelete.Code != http.StatusForbidden ||
		strings.Contains(deletionGraceDelete.Body.String(), `"code":"tenant_suspended"`) {
		t.Fatalf("deletion-grace replay policy = metadata:%d diagnostic:%d delete:%d/%s",
			deletionGraceMetadata.Code, deletionGraceDiagnostic.Code,
			deletionGraceDelete.Code, deletionGraceDelete.Body.String())
	}
	var deletionGraceSessionExists bool
	if err := root.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM sessions WHERE tenant_id = $1 AND id = $2)
	`, tenantID, createResponse.ID).Scan(&deletionGraceSessionExists); err != nil {
		t.Fatalf("check replay during deletion grace: %v", err)
	}
	if !deletionGraceSessionExists ||
		len(storageClient.removedKeys) != removedBeforeStatusDenials {
		t.Fatalf("deletion-grace denial changed replay: exists=%t removed=%#v",
			deletionGraceSessionExists, storageClient.removedKeys)
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants
		SET status = 'active', trial_ends_at = NULL, deletion_requested_at = NULL
		WHERE id = $1
	`, tenantID); err != nil {
		t.Fatalf("restore active replay tenant: %v", err)
	}
	invalidMetadata := tenantReplayRequest(tenantHandler, http.MethodGet, "/v1/sessions/not-a-uuid", accessToken)
	if invalidMetadata.Code != http.StatusNotFound {
		t.Errorf("invalid metadata status = %d", invalidMetadata.Code)
	}
	missingMetadata := tenantReplayRequest(tenantHandler, http.MethodGet,
		"/v1/sessions/00000000-0000-4000-8000-000000000002", accessToken)
	if missingMetadata.Code != http.StatusNotFound {
		t.Errorf("missing metadata status = %d", missingMetadata.Code)
	}

	streamHandler := NewStreamHTTPHandler(StreamHTTPOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Store: Store{}, JWT: jwtManager, Storage: storageClient,
		Now: func() time.Time { return now },
	})
	streamRoutes := chi.NewRouter()
	streamRoutes.Get("/v1/sessions/{sessionID}/replay", streamHandler.ServeHTTP)
	credentialLimiter := &replayAPIKeyLimiterStub{counts: map[string]int{}}
	limitedStreamRoutes := appmiddleware.TenantAPIKeyRateLimit(
		appmiddleware.TenantAPIKeyRateLimitOptions{Limiter: credentialLimiter},
	)(streamRoutes)
	streamAPIKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"
	var streamAPIKeyID string
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions, rate_limit_per_min
		)
		VALUES ($1, 'Replay reader', $2, 'fk_live_0123...', '{session:read}', 1)
		RETURNING id
	`, tenantID, auth.HashToken(streamAPIKey)).Scan(&streamAPIKeyID); err != nil {
		t.Fatalf("create replay stream API key: %v", err)
	}
	methodDenied := httptest.NewRecorder()
	methodRequest := httptest.NewRequest(http.MethodPost, "http://acme.localhost/replay", nil)
	streamHandler.ServeHTTP(methodDenied, methodRequest)
	if methodDenied.Code != http.StatusMethodNotAllowed {
		t.Errorf("stream method status = %d", methodDenied.Code)
	}
	unauthenticatedStream := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", "")
	if unauthenticatedStream.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated stream status = %d", unauthenticatedStream.Code)
	}
	readDeniedToken, _, err := jwtManager.IssueTenant(restrictedMemberID, tenantID, nil)
	if err != nil {
		t.Fatalf("issue restricted tenant token: %v", err)
	}
	deniedStream := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", readDeniedToken)
	if deniedStream.Code != http.StatusForbidden {
		t.Errorf("permission-denied stream status = %d", deniedStream.Code)
	}
	badStream := tenantReplayRequest(limitedStreamRoutes, http.MethodGet, "/v1/sessions/not-a-uuid/replay", accessToken)
	if badStream.Code != http.StatusNotFound {
		t.Errorf("invalid replay stream status = %d", badStream.Code)
	}
	streamed := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", accessToken)
	if streamed.Code != http.StatusOK || streamed.Body.String() != string(batchData) ||
		streamed.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("stream status = %d headers = %#v body = %s", streamed.Code, streamed.Header(), streamed.Body.String())
	}
	streamedAgain := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", accessToken)
	if streamedAgain.Code != http.StatusOK {
		t.Errorf("repeated stream status = %d", streamedAgain.Code)
	}
	streamedWithAPIKey := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", streamAPIKey)
	if streamedWithAPIKey.Code != http.StatusOK || streamedWithAPIKey.Body.String() != string(batchData) {
		t.Errorf("API-key replay stream = %d body=%q", streamedWithAPIKey.Code, streamedWithAPIKey.Body.String())
	}
	rateLimitedReplay := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", streamAPIKey)
	if rateLimitedReplay.Code != http.StatusTooManyRequests ||
		rateLimitedReplay.Header().Get("Retry-After") == "" {
		t.Errorf("API-key replay limit = %d headers=%v body=%q",
			rateLimitedReplay.Code, rateLimitedReplay.Header(), rateLimitedReplay.Body.String())
	}
	limiterKey := "tenant-api-key:" + streamAPIKeyID
	if credentialLimiter.callCount(limiterKey) != 2 ||
		credentialLimiter.callCount(streamAPIKey) != 0 {
		t.Errorf("replay limiter calls = %#v, want one immutable-ID charge per request",
			credentialLimiter.snapshot())
	}
	credentialLimiter.reset()
	headRequest := httptest.NewRequest(http.MethodHead, "http://acme.localhost/v1/sessions/"+createResponse.ID+"/replay", nil)
	headRequest.Header.Set("Authorization", "Bearer "+accessToken)
	headResponse := httptest.NewRecorder()
	limitedStreamRoutes.ServeHTTP(headResponse, headRequest)
	if headResponse.Code != http.StatusMethodNotAllowed {
		t.Errorf("replay HEAD status = %d", headResponse.Code)
	}
	delete(storageClient.objects, mergedKey)
	missingObject := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", accessToken)
	if missingObject.Code != http.StatusServiceUnavailable {
		t.Errorf("missing replay object status = %d", missingObject.Code)
	}
	storageClient.objects[mergedKey] = batchData

	// Access-audit assertions (issue #65): human, API-key, and denied access
	// are each recorded once; the repeated playback request and HEAD probe
	// must not flood the ledger, and no evidence content or credentials may
	// appear in the stored payload.
	var replayEventCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'replay_session.accessed' AND resource_id = $2
	`, tenantID, createResponse.ID).Scan(&replayEventCount); err != nil {
		t.Fatalf("count replay access events: %v", err)
	}
	// human success (streamedAgain and missingObject dedupe into it) +
	// api-key success + human denied = 3; HEAD and the unauthenticated/bad
	// session-id/method-not-allowed requests never reach the audit trail.
	if replayEventCount != 3 {
		t.Fatalf("replay access event count = %d, want 3", replayEventCount)
	}
	var humanSuccessCount, apiKeySuccessCount, deniedCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND resource_id = $2 AND outcome = 'success' AND actor_type = 'human'
		  AND actor_member_id = $3 AND action = 'playback' AND permission_path = 'session:read'
	`, tenantID, createResponse.ID, replayMemberID).Scan(&humanSuccessCount); err != nil {
		t.Fatalf("count human replay success events: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND resource_id = $2 AND outcome = 'success' AND actor_type = 'api_key'
	`, tenantID, createResponse.ID).Scan(&apiKeySuccessCount); err != nil {
		t.Fatalf("count api-key replay success events: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND resource_id = $2 AND outcome = 'denied' AND actor_member_id = $3
	`, tenantID, createResponse.ID, restrictedMemberID).Scan(&deniedCount); err != nil {
		t.Fatalf("count denied replay events: %v", err)
	}
	if humanSuccessCount != 1 || apiKeySuccessCount != 1 || deniedCount != 1 {
		t.Fatalf("replay access event breakdown = human:%d api_key:%d denied:%d, want 1/1/1",
			humanSuccessCount, apiKeySuccessCount, deniedCount)
	}
	var replayAuditText string
	if err := root.QueryRow(ctx, `
		SELECT coalesce(string_agg(data::text, ' '), '') FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'replay_session.accessed'
	`, tenantID).Scan(&replayAuditText); err != nil {
		t.Fatalf("read replay audit payloads: %v", err)
	}
	for _, forbidden := range []string{string(batchData), streamAPIKey, accessToken, readDeniedToken, "fk_live_"} {
		if strings.Contains(replayAuditText, forbidden) {
			t.Errorf("replay audit payload leaked %q", forbidden)
		}
	}
	deleted := tenantReplayRequest(tenantHandler, http.MethodDelete, "/v1/sessions/"+createResponse.ID, accessToken)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	if _, exists := storageClient.objects[mergedKey]; exists {
		t.Error("merged object remained after tenant deletion")
	}

	var expiredSessionID string
	expiredKey := "retention/expired.json"
	expiredBatchKey := "retention/expired-batch.json"
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, ended_at, events_count, batches_count,
			storage_key, rrweb_version, merged_at, merge_requested_at,
			max_duration_seconds, max_events, created_at, replay_consent_id
		) VALUES (
			$1, $2, gen_random_uuid(), $3, $3, 1, 1, $4, '2.1.1', $3, $3, 1800, 50000, $3, $5
		) RETURNING id
	`, tenantID, projectID, now.Add(-91*24*time.Hour), expiredKey, replayConsentID).Scan(&expiredSessionID); err != nil {
		t.Fatalf("create expired replay: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, completed_at, created_at,
			safety_status, safety_checked_at
		) VALUES ($1, $2, $3, 0, $4, 1, 128, $5, $5, 'safe', $5)
	`, tenantID, projectID, expiredSessionID, expiredBatchKey, now.Add(-91*24*time.Hour)); err != nil {
		t.Fatalf("create expired replay batch: %v", err)
	}
	var legacySessionID string
	legacyKey := "retention/legacy-privacy.json"
	legacyBatchKey := "retention/legacy-privacy-batch.json"
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, started_at, ended_at, events_count, batches_count,
				storage_key, rrweb_version, merged_at, merge_requested_at,
				max_duration_seconds, max_events, replay_privacy_version
			) VALUES (
				$1, $2, gen_random_uuid(), $3, $3, 1, 1, $4, '2.1.1', $3, $3,
				1800, 50000, 0
			) RETURNING id
		`, tenantID, projectID, now, legacyKey).Scan(&legacySessionID); err != nil {
		t.Fatalf("create legacy-privacy replay: %v", err)
	}
	if _, err := root.Exec(ctx, `
			INSERT INTO session_batches (
				tenant_id, project_id, session_id, batch_index, storage_key,
				events_count, size_bytes, completed_at, safety_status, safety_checked_at
			) VALUES ($1, $2, $3, 0, $4, 1, 128, $5, 'safe', $5)
		`, tenantID, projectID, legacySessionID, legacyBatchKey, now); err != nil {
		t.Fatalf("create legacy-privacy replay batch: %v", err)
	}
	storageClient.objects[legacyKey] = []byte(`[{"identity":"jane@example.com"}]`)
	storageClient.objects[legacyBatchKey] = []byte(`[{"identity":"jane@example.com"}]`)
	legacyMetadata := tenantReplayRequest(
		tenantHandler, http.MethodGet, "/v1/sessions/"+legacySessionID, accessToken,
	)
	if legacyMetadata.Code != http.StatusOK ||
		!strings.Contains(legacyMetadata.Body.String(), `"status":"expired"`) ||
		strings.Contains(legacyMetadata.Body.String(), `"replay_url"`) {
		t.Fatalf(
			"legacy replay metadata status/body = %d/%s",
			legacyMetadata.Code, legacyMetadata.Body.String(),
		)
	}
	legacyStream := tenantReplayRequest(
		limitedStreamRoutes, http.MethodGet, "/v1/sessions/"+legacySessionID+"/replay", accessToken,
	)
	if legacyStream.Code != http.StatusNotFound ||
		strings.Contains(legacyStream.Body.String(), "jane@example.com") {
		t.Fatalf("legacy replay stream status/body = %d/%s", legacyStream.Code, legacyStream.Body.String())
	}
	retentionStore := Store{Database: pools.Admin}
	expired, err := retentionStore.ExpiredSessions(ctx, now, 10)
	if err != nil {
		t.Fatalf("list expired sessions: %v", err)
	}
	if len(expired) != 2 || expired[0].ID != expiredSessionID ||
		expired[0].TenantID != tenantID || len(expired[0].ObjectKeys) != 2 ||
		expired[0].ObjectKeys[0] != expiredKey || expired[0].ObjectKeys[1] != expiredBatchKey ||
		expired[1].ID != legacySessionID || len(expired[1].ObjectKeys) != 2 ||
		expired[1].ObjectKeys[0] != legacyKey || expired[1].ObjectKeys[1] != legacyBatchKey {
		t.Fatalf("expired sessions = %#v", expired)
	}
	if err := retentionStore.MarkSessionRetentionDeleted(
		ctx, tenantID, expiredSessionID, now,
	); err != nil {
		t.Fatalf("preserve expired session manifest: %v", err)
	}
	var retainedKey *string
	var retentionDeletedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT storage_key, retention_deleted_at
		FROM sessions WHERE tenant_id = $1 AND id = $2
	`, tenantID, expiredSessionID).Scan(&retainedKey, &retentionDeletedAt); err != nil {
		t.Fatalf("read expired session manifest: %v", err)
	}
	if retainedKey != nil || retentionDeletedAt == nil || !retentionDeletedAt.Equal(now) {
		t.Fatalf("expired session manifest key=%v retention_deleted_at=%v", retainedKey, retentionDeletedAt)
	}
	var retainedBatchKey *string
	var batchRetentionDeletedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT storage_key, retention_deleted_at
		FROM session_batches WHERE tenant_id = $1 AND session_id = $2
	`, tenantID, expiredSessionID).Scan(&retainedBatchKey, &batchRetentionDeletedAt); err != nil {
		t.Fatalf("read expired batch manifest: %v", err)
	}
	if retainedBatchKey != nil || batchRetentionDeletedAt == nil ||
		!batchRetentionDeletedAt.Equal(now) {
		t.Fatalf("expired batch manifest key=%v retention_deleted_at=%v",
			retainedBatchKey, batchRetentionDeletedAt)
	}
	if err := (Cleaner{
		Store: retentionStore, Storage: storageClient, Health: &replayHealthRecorderStub{},
		Now: func() time.Time { return now },
	}).CleanupOnce(ctx); err != nil {
		t.Fatalf("remove legacy-privacy replay objects: %v", err)
	}
	if _, exists := storageClient.objects[legacyKey]; exists {
		t.Fatal("legacy merged replay object remained after privacy cleanup")
	}
	if _, exists := storageClient.objects[legacyBatchKey]; exists {
		t.Fatal("legacy replay batch remained after privacy cleanup")
	}
	if err := root.QueryRow(ctx, `
			SELECT storage_key, retention_deleted_at
			FROM sessions WHERE tenant_id = $1 AND id = $2
		`, tenantID, legacySessionID).Scan(&retainedKey, &retentionDeletedAt); err != nil {
		t.Fatalf("read cleaned legacy replay manifest: %v", err)
	}
	if retainedKey != nil || retentionDeletedAt == nil || !retentionDeletedAt.Equal(now) {
		t.Fatalf(
			"legacy replay manifest key=%v retention_deleted_at=%v",
			retainedKey, retentionDeletedAt,
		)
	}

	var abandonedSessionID, activeSessionID, renewedSessionID, emptySessionID, incompleteSessionID string
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, page_url, started_at, events_count, batches_count,
				rrweb_version, max_duration_seconds, max_events, replay_consent_id
			) VALUES ($1, $2, gen_random_uuid(), 'https://example.com/', $3, 1, 1, '2.1.1', 1800, 50000, $4)
			RETURNING id
		`, tenantID, projectID, now.Add(-20*time.Minute), replayConsentID).Scan(&abandonedSessionID); err != nil {
		t.Fatalf("create abandoned replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, started_at, events_count, batches_count,
				rrweb_version, max_duration_seconds, max_events, replay_consent_id
			) VALUES ($1, $2, gen_random_uuid(), $3, 1, 1, '2.1.1', 1800, 50000, $4)
			RETURNING id
		`, tenantID, projectID, now.Add(-20*time.Minute), replayConsentID).Scan(&activeSessionID); err != nil {
		t.Fatalf("create active replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, started_at, events_count, batches_count,
				rrweb_version, max_duration_seconds, max_events, replay_consent_id
			) VALUES ($1, $2, gen_random_uuid(), $3, 1, 1, '2.1.1', 1800, 50000, $4)
			RETURNING id
		`, tenantID, projectID, now.Add(-20*time.Minute), replayConsentID).Scan(&renewedSessionID); err != nil {
		t.Fatalf("create renewed replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, started_at,
				rrweb_version, max_duration_seconds, max_events, replay_consent_id
			) VALUES ($1, $2, gen_random_uuid(), $3, '2.1.1', 1800, 50000, $4)
			RETURNING id
		`, tenantID, projectID, now.Add(-20*time.Minute), replayConsentID).Scan(&emptySessionID); err != nil {
		t.Fatalf("create empty abandoned replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, started_at,
				rrweb_version, max_duration_seconds, max_events, replay_consent_id
			) VALUES ($1, $2, gen_random_uuid(), $3, '2.1.1', 1800, 50000, $4)
			RETURNING id
		`, tenantID, projectID, now.Add(-20*time.Minute), replayConsentID).Scan(&incompleteSessionID); err != nil {
		t.Fatalf("create incomplete abandoned replay: %v", err)
	}
	abandonedCompletedKey := "abandoned/completed.json"
	activeKey := "active/completed.json"
	renewedKey := "active/renewed.json"
	missingIncompleteKey := "abandoned/missing.json"
	abandonedData := []byte(`[{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0}}}]`)
	if _, err := root.Exec(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, complete_token_hash, completed_at, created_at,
			upload_refreshed_at, upload_expires_at, safety_status, safety_checked_at
		) VALUES
			($1, $2, $3, 0, $4, 1, $13, NULL, $5, $5, $5, $6, 'safe', $5),
			($1, $2, $7, 0, $8, 1, 4, NULL, $9, $9, $9, $10, 'safe', $9),
			($1, $2, $11, 0, $12, 1, 4, 'hash', NULL, $5, $5, $6, 'pending', NULL)
	`, tenantID, projectID, abandonedSessionID, abandonedCompletedKey,
		now.Add(-16*time.Minute), now.Add(-6*time.Minute), activeSessionID, activeKey,
		now.Add(-time.Minute), now.Add(9*time.Minute), incompleteSessionID,
		missingIncompleteKey, len(abandonedData)); err != nil {
		t.Fatalf("create replay recovery batches: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, complete_token_hash, created_at, upload_refreshed_at
		) VALUES ($1, $2, $3, 0, $4, 1, 4, 'old-hash', $5, $5)
	`, tenantID, projectID, renewedSessionID, renewedKey, now.Add(-16*time.Minute)); err != nil {
		t.Fatalf("create renewed replay batch: %v", err)
	}
	if _, _, err := (Store{}).DeclareBatch(
		ctx, root, tenantID, projectID, renewedSessionID, 0, 1, 4, now.Add(-time.Minute),
	); err != nil {
		t.Fatalf("renew replay batch: %v", err)
	}
	queuedBeforeRecovery := len(mergeQueue.sessionIDs)
	if err := scheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("recover abandoned replay: %v", err)
	}
	got := mergeQueue.sessionIDs[queuedBeforeRecovery:]
	queued := make(map[string]bool, len(got))
	for _, sessionID := range got {
		queued[sessionID] = true
	}
	if len(got) != 1 || !queued[abandonedSessionID] {
		t.Fatalf("recovered replay queue = %#v", got)
	}
	var abandonedEndedAt, abandonedMergeRequestedAt, activeEndedAt, renewedEndedAt *time.Time
	var emptyEndedAt, emptyMergeRequestedAt, incompleteEndedAt, incompleteMergeRequestedAt *time.Time
	var emptyMergeError, incompleteMergeError *string
	var emptyMergeAttempts, incompleteMergeAttempts int
	if err := root.QueryRow(ctx, `
		SELECT abandoned.ended_at, abandoned.merge_requested_at, active.ended_at, renewed.ended_at,
		       empty.ended_at, empty.merge_requested_at, empty.merge_error, empty.merge_attempts,
		       incomplete.ended_at, incomplete.merge_requested_at,
		       incomplete.merge_error, incomplete.merge_attempts
		FROM sessions AS abandoned
		JOIN sessions AS active ON active.id = $2
		JOIN sessions AS renewed ON renewed.id = $3
		JOIN sessions AS empty ON empty.id = $4
		JOIN sessions AS incomplete ON incomplete.id = $5
		WHERE abandoned.id = $1
	`, abandonedSessionID, activeSessionID, renewedSessionID, emptySessionID, incompleteSessionID).Scan(
		&abandonedEndedAt, &abandonedMergeRequestedAt, &activeEndedAt, &renewedEndedAt,
		&emptyEndedAt, &emptyMergeRequestedAt, &emptyMergeError, &emptyMergeAttempts,
		&incompleteEndedAt, &incompleteMergeRequestedAt,
		&incompleteMergeError, &incompleteMergeAttempts,
	); err != nil {
		t.Fatalf("read recovered replay state: %v", err)
	}
	if abandonedEndedAt == nil || abandonedMergeRequestedAt == nil || activeEndedAt != nil || renewedEndedAt != nil ||
		emptyEndedAt == nil || emptyMergeRequestedAt != nil || emptyMergeError == nil ||
		*emptyMergeError != unavailableReplayMessage || emptyMergeAttempts != terminalMergeAttempts ||
		incompleteEndedAt == nil || incompleteMergeRequestedAt != nil ||
		incompleteMergeError == nil || *incompleteMergeError != unavailableReplayMessage ||
		incompleteMergeAttempts != terminalMergeAttempts {
		t.Errorf("recovered replay state = abandoned %v/%v active %v renewed %v empty %v/%v/%v/%d incomplete %v/%v/%v/%d",
			abandonedEndedAt, abandonedMergeRequestedAt, activeEndedAt, renewedEndedAt, emptyEndedAt,
			emptyMergeRequestedAt, emptyMergeError, emptyMergeAttempts,
			incompleteEndedAt, incompleteMergeRequestedAt, incompleteMergeError, incompleteMergeAttempts)
	}
	if _, err := (Store{}).LockBatch(
		ctx, root, tenantID, projectID, incompleteSessionID, 0,
	); !errors.Is(err, ErrSessionFinalized) {
		t.Errorf("LockBatch(terminal replay) error = %v, want ErrSessionFinalized", err)
	}
	pendingAfterTerminal, err := retentionStore.PendingMerges(ctx, 100, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("list pending merges after terminal replay: %v", err)
	}
	for _, pending := range pendingAfterTerminal {
		if pending.ID == incompleteSessionID {
			t.Errorf("terminal replay remained pending: %#v", pending)
		}
	}
	storageClient.objects[abandonedCompletedKey] = abandonedData
	storageClient.contentTypes[abandonedCompletedKey] = "application/json"
	abandonedTask := asynq.NewTask(TaskMerge, []byte(fmt.Sprintf(`{"session_id":%q}`, abandonedSessionID)))
	if err := processor.MergeHandler()(ctx, abandonedTask); err != nil {
		t.Fatalf("merge recovered replay: %v", err)
	}
	recoveredKey := "tenants/" + tenantID + "/sessions/" + abandonedSessionID + "/replay.json"
	if got := string(storageClient.objects[recoveredKey]); got != string(abandonedData) {
		t.Errorf("recovered replay = %s, want %s", got, abandonedData)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM session_batches WHERE session_id = $1`, abandonedSessionID).
		Scan(&batchRows); err != nil || batchRows != 0 {
		t.Fatalf("recovered replay batch rows = %d, error = %v", batchRows, err)
	}
	if err := root.QueryRow(ctx, `SELECT events_count, batches_count FROM sessions WHERE id = $1`, abandonedSessionID).
		Scan(&events, &batches); err != nil || events != 1 || batches != 1 {
		t.Errorf("recovered replay aggregates = %d/%d, error = %v", events, batches, err)
	}

	var failedSessionID string
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, page_url, started_at, ended_at, rrweb_version,
				merge_requested_at, max_duration_seconds, max_events, replay_consent_id
			) VALUES (
				$1, $2, gen_random_uuid(), 'https://example.com/',
				$3, $3, '2.1.1', $3, 1800, 50000, $4
			)
			RETURNING id
		`, tenantID, projectID, now, replayConsentID).Scan(&failedSessionID); err != nil {
		t.Fatalf("create failed merge session: %v", err)
	}
	failedBatchKey := "failed/batch.json"
	failedBatchData := []byte(`[{"type":2,"timestamp":1784800000000,"data":{"node":{"type":0}}}]`)
	if _, err := root.Exec(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, completed_at, safety_status, safety_checked_at
		) VALUES ($1, $2, $3, 0, $4, 2, $5, $6, 'safe', $6)
	`, tenantID, projectID, failedSessionID, failedBatchKey, len(failedBatchData), now); err != nil {
		t.Fatalf("create failed merge batch: %v", err)
	}
	storageClient.objects[failedBatchKey] = failedBatchData
	storageClient.contentTypes[failedBatchKey] = "application/json"
	failedTask := asynq.NewTask(TaskMerge, []byte(fmt.Sprintf(`{"session_id":%q}`, failedSessionID)))
	if err := processor.MergeHandler()(ctx, failedTask); err == nil {
		t.Fatal("invalid replay merge error = nil")
	}
	var mergeAttempts int
	var mergeError *string
	if err := root.QueryRow(ctx, `SELECT merge_attempts, merge_error FROM sessions WHERE id = $1`, failedSessionID).
		Scan(&mergeAttempts, &mergeError); err != nil {
		t.Fatalf("read failed merge state: %v", err)
	}
	if mergeAttempts != 1 || mergeError == nil || !strings.Contains(*mergeError, "event_count_mismatch") {
		t.Errorf("failed merge attempts/error = %d/%v", mergeAttempts, mergeError)
	}
	for mergeAttempts < terminalMergeAttempts {
		if err := retentionStore.MarkMergeFailed(ctx, failedSessionID, "storage unavailable"); err != nil {
			t.Fatalf("exhaust failed replay merge: %v", err)
		}
		mergeAttempts++
	}
	failedDetail := tenantReplayRequest(
		tenantHandler, http.MethodGet, "/v1/sessions/"+failedSessionID, accessToken,
	)
	if failedDetail.Code != http.StatusOK ||
		!strings.Contains(failedDetail.Body.String(), `"status":"failed"`) ||
		!strings.Contains(failedDetail.Body.String(),
			`"failure":{"code":"processing_failed","message":"Replay processing failed after several attempts. The recorded data is still available.","retryable":true}`) ||
		strings.Contains(failedDetail.Body.String(), "merge_error") ||
		strings.Contains(failedDetail.Body.String(), "storage unavailable") {
		t.Fatalf("failed replay detail = %d/%s", failedDetail.Code, failedDetail.Body.String())
	}
	deniedRetryQueueBefore := len(mergeQueue.sessionIDs)
	deniedRetry := tenantReplayRequest(
		tenantHandler, http.MethodPost, "/v1/sessions/"+failedSessionID+"/retry", controlAPIKey,
	)
	if deniedRetry.Code != http.StatusForbidden ||
		len(mergeQueue.sessionIDs) != deniedRetryQueueBefore {
		t.Fatalf("permission-denied replay retry = %d/%s queue %#v",
			deniedRetry.Code, deniedRetry.Body.String(), mergeQueue.sessionIDs)
	}
	retryQueueBefore := len(mergeQueue.sessionIDs)
	retryResponse := tenantReplayRequest(
		tenantHandler, http.MethodPost, "/v1/sessions/"+failedSessionID+"/retry", accessToken,
	)
	if retryResponse.Code != http.StatusAccepted ||
		!strings.Contains(retryResponse.Body.String(), `"status":"processing"`) {
		t.Fatalf("retry failed replay = %d/%s", retryResponse.Code, retryResponse.Body.String())
	}
	var retriedAttempts, retriedGeneration int
	var retriedError, retriedFailure *string
	if err := root.QueryRow(ctx, `
		SELECT merge_attempts, merge_error, failure_code, merge_generation
		FROM sessions WHERE id = $1
	`, failedSessionID).Scan(
		&retriedAttempts, &retriedError, &retriedFailure, &retriedGeneration,
	); err != nil {
		t.Fatalf("read retried replay state: %v", err)
	}
	if retriedAttempts != 0 || retriedError != nil || retriedFailure != nil || retriedGeneration != 2 ||
		len(mergeQueue.sessionIDs) != retryQueueBefore+1 ||
		mergeQueue.generations[len(mergeQueue.generations)-1] != 2 {
		t.Fatalf("retried replay = attempts %d error %v failure %v generation %d queue %#v/%#v",
			retriedAttempts, retriedError, retriedFailure, retriedGeneration,
			mergeQueue.sessionIDs, mergeQueue.generations)
	}
	repeatedRetry := tenantReplayRequest(
		tenantHandler, http.MethodPost, "/v1/sessions/"+failedSessionID+"/retry", accessToken,
	)
	if repeatedRetry.Code != http.StatusAccepted || len(mergeQueue.sessionIDs) != retryQueueBefore+1 {
		t.Fatalf("repeated replay retry = %d/%s queue %#v",
			repeatedRetry.Code, repeatedRetry.Body.String(), mergeQueue.sessionIDs)
	}
	if err := processor.MergeHandler()(ctx, failedTask); err == nil || !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("stale replay merge task error = %v, want SkipRetry", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE session_batches SET events_count = 1 WHERE session_id = $1
	`, failedSessionID); err != nil {
		t.Fatalf("repair retry fixture batch manifest: %v", err)
	}
	currentTask := asynq.NewTask(TaskMerge, []byte(fmt.Sprintf(
		`{"session_id":%q,"generation":2}`, failedSessionID,
	)))
	if err := processor.MergeHandler()(ctx, currentTask); err != nil {
		t.Fatalf("merge manually retried replay: %v", err)
	}
	delete(storageClient.objects, failedBatchKey)
	delete(storageClient.contentTypes, failedBatchKey)

	var pendingSessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, ended_at, rrweb_version,
			merge_requested_at, max_duration_seconds, max_events, replay_consent_id
		) VALUES ($1, $2, gen_random_uuid(), $3, $3, '2.1.1', $3, 1800, 50000, $4)
		RETURNING id
	`, tenantID, projectID, now, replayConsentID).Scan(&pendingSessionID); err != nil {
		t.Fatalf("create pending merge session: %v", err)
	}
	if err := scheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("reconcile pending merges: %v", err)
	}
	if mergeQueue.sessionIDs[len(mergeQueue.sessionIDs)-1] != pendingSessionID {
		t.Errorf("reconciled sessions = %#v", mergeQueue.sessionIDs)
	}
	pendingStream := tenantReplayRequest(limitedStreamRoutes, http.MethodGet,
		"/v1/sessions/"+pendingSessionID+"/replay", accessToken)
	if pendingStream.Code != http.StatusNotFound {
		t.Errorf("pending replay stream status = %d", pendingStream.Code)
	}
	reconcileContext, stopReconcile := context.WithCancel(ctx)
	stopReconcile()
	if err := scheduler.RunReconciler(reconcileContext, time.Hour); err != nil {
		t.Fatalf("run canceled merge reconciler: %v", err)
	}
	pendingTask := asynq.NewTask(TaskMerge, []byte(fmt.Sprintf(`{"session_id":%q}`, pendingSessionID)))
	if err := processor.MergeHandler()(ctx, pendingTask); !errors.Is(err, asynq.SkipRetry) ||
		!strings.Contains(err.Error(), "replay unavailable") {
		t.Errorf("empty merge task error = %v", err)
	}
	if err := retentionStore.MarkMergeFailed(ctx, pendingSessionID, strings.Repeat("x", 2100)); err != nil {
		t.Fatalf("mark long replay merge failure: %v", err)
	}
	missingTask := asynq.NewTask(TaskMerge, []byte(`{"session_id":"00000000-0000-4000-8000-000000000099"}`))
	if err := processor.MergeHandler()(ctx, missingTask); err == nil || !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("missing replay merge task error = %v, want SkipRetry", err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, pendingSessionID); err != nil {
		t.Fatalf("delete pending merge session: %v", err)
	}

	revokedToken, _, err := auth.NewOpaqueToken()
	if err != nil {
		t.Fatalf("generate revocable replay consent token: %v", err)
	}
	revocableConsent := replayRequest(router, http.MethodPost, "/v1/replay/consents?key="+publicKey,
		fmt.Sprintf(
			`{"widget_rule_id":%q,"purpose":"session_replay","notice_version":"2026-07",`+
				`"locale":"en","source":"customer_cmp","consent_id":"cmp-revocation",`+
				`"state":"granted","grant_token":%q}`,
			ruleID, revokedToken,
		), publicKey, "https://example.com")
	if revocableConsent.Code != http.StatusCreated {
		t.Fatalf("grant revocable replay consent = %d/%s", revocableConsent.Code, revocableConsent.Body.String())
	}
	revokedSessionClientID := "752a0475-30f0-4c0e-9cac-19dc286e3756"
	revokedSessionBody := fmt.Sprintf(
		`{"session_id":%q,"widget_rule_id":%q,"page_url":"https://example.com/revoked",`+
			`"consent_token":%q}`,
		revokedSessionClientID, ruleID, revokedToken,
	)
	revokedSessionResponse := replayRequest(
		router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		revokedSessionBody, publicKey, "https://example.com",
	)
	if revokedSessionResponse.Code != http.StatusCreated {
		t.Fatalf("create revocable replay = %d/%s", revokedSessionResponse.Code, revokedSessionResponse.Body.String())
	}
	var revokedSession struct {
		ID string `json:"id"`
	}
	decodeReplayResponse(t, revokedSessionResponse, &revokedSession)
	revoked := replayRequest(router, http.MethodPost, "/v1/replay/consents/revoke?key="+publicKey,
		fmt.Sprintf(`{"token":%q}`, revokedToken), publicKey, "https://example.com")
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke replay consent = %d/%s", revoked.Code, revoked.Body.String())
	}
	revokedRestart := replayRequest(
		router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		strings.Replace(revokedSessionBody, revokedSessionClientID, "ee426b41-af60-46c1-9a3a-79a634008f26", 1),
		publicKey, "https://example.com",
	)
	if revokedRestart.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(revokedRestart.Body.String(), `"field":"consent_token"`) {
		t.Fatalf("restart with revoked consent = %d/%s", revokedRestart.Code, revokedRestart.Body.String())
	}
	revokedBatch := replayRequest(
		router, http.MethodPost,
		"/v1/replay/sessions/"+revokedSession.ID+"/batches?key="+publicKey,
		`{"batch_index":0,"events_count":1,"size_bytes":2}`,
		publicKey, "https://example.com",
	)
	if revokedBatch.Code != http.StatusForbidden {
		t.Fatalf("batch after replay consent revocation = %d/%s", revokedBatch.Code, revokedBatch.Body.String())
	}
	withdrawnDiagnostic := tenantReplayRequest(
		tenantHandler, http.MethodGet, diagnosticPath, accessToken,
	)
	if withdrawnDiagnostic.Code != http.StatusOK ||
		!strings.Contains(withdrawnDiagnostic.Body.String(), `"state":"withdrawn"`) ||
		!strings.Contains(withdrawnDiagnostic.Body.String(), `"withdrawn_at"`) {
		t.Fatalf(
			"withdrawn replay consent diagnostic = %d/%s",
			withdrawnDiagnostic.Code,
			withdrawnDiagnostic.Body.String(),
		)
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, revokedSession.ID); err != nil {
		t.Fatalf("delete revoked replay session: %v", err)
	}

	configurationSessionID := "706bd54d-8fb2-4759-a47f-1c87cdd0bb20"
	configurationSessionBody := fmt.Sprintf(
		`{"session_id":%q,"widget_rule_id":%q,"page_url":"https://example.com/configuration",`+
			`"consent_token":%q}`,
		configurationSessionID, ruleID, consentToken,
	)
	configurationSessionResponse := replayRequest(
		router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		configurationSessionBody, publicKey, "https://example.com",
	)
	if configurationSessionResponse.Code != http.StatusCreated {
		t.Fatalf(
			"create configuration-change replay = %d/%s",
			configurationSessionResponse.Code, configurationSessionResponse.Body.String(),
		)
	}
	var configurationSession struct {
		ID string `json:"id"`
	}
	decodeReplayResponse(t, configurationSessionResponse, &configurationSession)
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET replay_consent = jsonb_set(replay_consent, '{notice_version}', '"2026-08"')
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("change replay notice version: %v", err)
	}
	afterConfigurationChange := replayRequest(
		router, http.MethodPost,
		"/v1/replay/sessions/"+configurationSession.ID+"/batches?key="+publicKey,
		`{"batch_index":0,"events_count":1,"size_bytes":2}`,
		publicKey, "https://example.com",
	)
	if afterConfigurationChange.Code != http.StatusForbidden {
		t.Fatalf(
			"batch after replay notice change = %d/%s",
			afterConfigurationChange.Code, afterConfigurationChange.Body.String(),
		)
	}
	restartAfterConfigurationChange := replayRequest(
		router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		strings.Replace(
			configurationSessionBody,
			configurationSessionID,
			"f4d9675e-765f-4e75-9842-91518ba02ea1",
			1,
		),
		publicKey, "https://example.com",
	)
	if restartAfterConfigurationChange.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"restart after replay notice change = %d/%s",
			restartAfterConfigurationChange.Code, restartAfterConfigurationChange.Body.String(),
		)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET replay_consent = jsonb_set(replay_consent, '{notice_version}', '"2026-07"')
		WHERE id = $1
	`, ruleID); err != nil {
		t.Fatalf("restore replay notice version: %v", err)
	}
	now = consentResponse.ExpiresAt.Add(time.Second)
	expiredGrant := replayRequest(
		router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		strings.Replace(
			configurationSessionBody,
			configurationSessionID,
			"36a5fb02-c465-4a1e-bde6-e9eaa8f6e24b",
			1,
		),
		publicKey, "https://example.com",
	)
	if expiredGrant.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expired replay grant = %d/%s", expiredGrant.Code, expiredGrant.Body.String())
	}
	var expiredConsentState string
	if err := root.QueryRow(ctx, `
		SELECT state::text FROM replay_consents WHERE id = $1
	`, replayConsentID).Scan(&expiredConsentState); err != nil || expiredConsentState != "expired" {
		t.Fatalf("expired replay consent state = %q, error = %v", expiredConsentState, err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, configurationSession.ID); err != nil {
		t.Fatalf("delete configuration-change replay session: %v", err)
	}

	if _, err := root.Exec(ctx, `UPDATE tenants SET plan = 'starter' WHERE id = $1`, tenantID); err != nil {
		t.Fatalf("set starter plan: %v", err)
	}
	planDenied := replayRequest(router, http.MethodPost, "/v1/replay/sessions?key="+publicKey,
		strings.Replace(createBody, clientSessionID, "7d74b678-4dce-49ee-8e66-77e01f905255", 1),
		publicKey, "https://example.com")
	if planDenied.Code != http.StatusPaymentRequired || !strings.Contains(planDenied.Body.String(), "plan_feature_unavailable") {
		t.Errorf("plan denial status = %d, body = %s", planDenied.Code, planDenied.Body.String())
	}
}

type replayAPIKeyLimiterStub struct {
	mu     sync.Mutex
	counts map[string]int
}

func (l *replayAPIKeyLimiterStub) Allow(
	_ context.Context,
	key string,
	window ratelimit.Window,
) (ratelimit.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts[key]++
	return ratelimit.Decision{
		Allowed: l.counts[key] <= window.Limit, Limit: window.Limit,
		Remaining: max(window.Limit-l.counts[key], 0), RetryAfter: 30 * time.Second,
	}, nil
}

func (l *replayAPIKeyLimiterStub) callCount(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[key]
}

func (l *replayAPIKeyLimiterStub) snapshot() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[string]int, len(l.counts))
	for key, value := range l.counts {
		snapshot[key] = value
	}
	return snapshot
}

func (l *replayAPIKeyLimiterStub) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counts = map[string]int{}
}

type replayStorageStub struct {
	objects       map[string][]byte
	contentTypes  map[string]string
	presignedKeys []string
	removedKeys   []string
	removeErrors  map[string]error
	statErrors    map[string]error
}

func (s *replayStorageStub) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	s.presignedKeys = append(s.presignedKeys, key)
	return "https://uploads.example.test/" + key, nil
}

func (s *replayStorageStub) Stat(_ context.Context, key string) (storage.ObjectInfo, error) {
	if err := s.statErrors[key]; err != nil {
		return storage.ObjectInfo{}, err
	}
	value, exists := s.objects[key]
	if !exists {
		return storage.ObjectInfo{}, fmt.Errorf("object %q: %w", key, storage.ErrObjectNotFound)
	}
	return storage.ObjectInfo{Size: int64(len(value)), ContentType: s.contentTypes[key]}, nil
}

func (s *replayStorageStub) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	value, exists := s.objects[key]
	if !exists {
		return nil, fmt.Errorf("object %q not found", key)
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (s *replayStorageStub) PutObject(
	_ context.Context,
	key string,
	reader io.Reader,
	_ int64,
	contentType string,
) error {
	value, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.objects[key] = value
	s.contentTypes[key] = contentType
	return nil
}

func (s *replayStorageStub) RemoveObject(_ context.Context, key string) error {
	s.removedKeys = append(s.removedKeys, key)
	if err := s.removeErrors[key]; err != nil {
		return err
	}
	delete(s.objects, key)
	delete(s.contentTypes, key)
	return nil
}

type replayMergeQueueStub struct {
	sessionIDs        []string
	generations       []int
	verificationTasks []BatchVerificationTask
}

type replayAbuseCall struct {
	policy  abuse.Policy
	request abuse.Request
}

type replayAbuseEvaluatorStub struct {
	calls      []replayAbuseCall
	denyPolicy string
	failPolicy string
}

func (stub *replayAbuseEvaluatorStub) Evaluate(
	_ context.Context,
	policy abuse.Policy,
	request abuse.Request,
) (abuse.Decision, error) {
	stub.calls = append(stub.calls, replayAbuseCall{policy: policy, request: request})
	if policy.Name == stub.failPolicy {
		return abuse.Decision{Allowed: false, Reason: policy.Reason}, errors.New("redis unavailable")
	}
	if policy.Name == stub.denyPolicy {
		return abuse.Decision{
			Allowed: false, Reason: policy.Reason, RetryAfter: 3 * time.Second,
			Limit: 10, Remaining: 0,
		}, nil
	}
	return abuse.Decision{Allowed: true, Reason: policy.Reason, Limit: 100, Remaining: 99}, nil
}

type replayDiagnosticRecorderStub struct {
	events []abuse.DiagnosticEvent
}

func (stub *replayDiagnosticRecorderStub) Record(
	_ context.Context,
	event abuse.DiagnosticEvent,
) error {
	stub.events = append(stub.events, event)
	return nil
}

func (s *replayMergeQueueStub) EnqueueMerge(_ context.Context, sessionID string, generation int) error {
	s.sessionIDs = append(s.sessionIDs, sessionID)
	s.generations = append(s.generations, generation)
	return nil
}

func (s *replayMergeQueueStub) EnqueueVerification(
	_ context.Context,
	task BatchVerificationTask,
) error {
	s.verificationTasks = append(s.verificationTasks, task)
	return nil
}

func replayRequest(
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
	if publicKey != "" {
		request.Header.Set(widgetapi.PublicKeyHeader, publicKey)
	}
	request.Header.Set("Origin", origin)
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeReplayResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, recorder.Body.String())
	}
}

func tenantReplayRequest(handler http.Handler, method string, path string, token string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://acme.localhost"+path, nil)
	request.Host = "acme.localhost"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func replayMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	return path
}

func replayRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
