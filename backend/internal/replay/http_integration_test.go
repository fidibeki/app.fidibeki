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
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
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
			replay_max_events, replay_mask_all_inputs
		) VALUES ($1, 'Replay', 'all_pages', 'delay', 0, 'full', 0, 1800, 50000, true)
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
	scheduler := Scheduler{
		Store: Store{Database: pools.Admin}, Queue: mergeQueue, Now: func() time.Time { return now },
	}
	authorizer := widgetapi.Authorizer{
		Store: project.Store{}, Database: pools.Admin, Now: func() time.Time { return now },
	}
	router := chi.NewRouter()
	RegisterPublicRoutes(router, HTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Storage: storageClient, Scheduler: scheduler, Now: func() time.Time { return now },
	})

	preflight := replayRequest(router, http.MethodOptions, "/v1/replay/sessions?key="+publicKey, "", "", "https://example.com")
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("preflight status = %d, headers = %#v", preflight.Code, preflight.Header())
	}
	clientSessionID := "d766f8eb-d58d-42ca-b9fd-349cc9e8e3b4"
	createBody := fmt.Sprintf(`{"session_id":%q,"widget_rule_id":%q,"page_url":"https://example.com/checkout"}`,
		clientSessionID, ruleID)
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

	batchData := []byte(`[{"type":2},{}]`)
	batchBody := fmt.Sprintf(`{"batch_index":0,"events_count":2,"size_bytes":%d}`, len(batchData))
	declared := replayRequest(router, http.MethodPost,
		"/v1/replay/sessions/"+createResponse.ID+"/batches?key="+publicKey,
		batchBody, publicKey, "https://example.com")
	if declared.Code != http.StatusCreated {
		t.Fatalf("declare status = %d, body = %s", declared.Code, declared.Body.String())
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
	emptyCreateBody := fmt.Sprintf(`{"session_id":"66d6b8fc-f79d-46bb-9a46-a8db9cb7ea0c","widget_rule_id":%q,"page_url":"https://example.com/empty"}`, ruleID)
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

	processor := Processor{Store: Store{Database: pools.Admin}, Storage: storageClient, Now: func() time.Time { return now }}
	mergeTask := asynq.NewTask(TaskMerge, []byte(fmt.Sprintf(`{"session_id":%q}`, createResponse.ID)))
	storageClient.removeErrors = map[string]error{
		storageClient.presignedKeys[0]: errors.New("storage cleanup unavailable"),
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
	if err := root.QueryRow(ctx, `SELECT count(*) FROM session_batches WHERE session_id = $1`, createResponse.ID).
		Scan(&batchRows); err != nil || batchRows != 0 {
		t.Fatalf("merged batch rows = %d, error = %v", batchRows, err)
	}

	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Hour)
	if err != nil {
		t.Fatalf("create tenant JWT manager: %v", err)
	}
	accessToken, _, err := jwtManager.IssueTenant("member", tenantID, []string{
		string(auth.PermissionSessionRead), string(auth.PermissionSessionDelete),
	})
	if err != nil {
		t.Fatalf("issue tenant access token: %v", err)
	}
	tenantRoutes := chi.NewRouter()
	RegisterTenantRoutes(tenantRoutes, TenantHTTPOptions{
		Store: Store{}, JWT: jwtManager, Storage: storageClient,
	})
	tenantHandler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(tenantRoutes)
	metadata := tenantReplayRequest(tenantHandler, http.MethodGet, "/v1/sessions/"+createResponse.ID, accessToken)
	if metadata.Code != http.StatusOK || !strings.Contains(metadata.Body.String(), `"status":"ready"`) ||
		!strings.Contains(metadata.Body.String(), `"replay_url"`) {
		t.Fatalf("metadata status = %d, body = %s", metadata.Code, metadata.Body.String())
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
	streamAPIKey := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"
	if _, err := root.Exec(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions)
		VALUES ($1, 'Replay reader', $2, 'fk_live_0123...', '{session:read}')
	`, tenantID, auth.HashToken(streamAPIKey)); err != nil {
		t.Fatalf("create replay stream API key: %v", err)
	}
	methodDenied := httptest.NewRecorder()
	methodRequest := httptest.NewRequest(http.MethodPost, "http://acme.localhost/replay", nil)
	streamHandler.ServeHTTP(methodDenied, methodRequest)
	if methodDenied.Code != http.StatusMethodNotAllowed {
		t.Errorf("stream method status = %d", methodDenied.Code)
	}
	unauthenticatedStream := tenantReplayRequest(streamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", "")
	if unauthenticatedStream.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated stream status = %d", unauthenticatedStream.Code)
	}
	readDeniedToken, _, err := jwtManager.IssueTenant("member", tenantID, nil)
	if err != nil {
		t.Fatalf("issue restricted tenant token: %v", err)
	}
	deniedStream := tenantReplayRequest(streamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", readDeniedToken)
	if deniedStream.Code != http.StatusForbidden {
		t.Errorf("permission-denied stream status = %d", deniedStream.Code)
	}
	badStream := tenantReplayRequest(streamRoutes, http.MethodGet, "/v1/sessions/not-a-uuid/replay", accessToken)
	if badStream.Code != http.StatusNotFound {
		t.Errorf("invalid replay stream status = %d", badStream.Code)
	}
	streamed := tenantReplayRequest(streamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", accessToken)
	if streamed.Code != http.StatusOK || streamed.Body.String() != string(batchData) ||
		streamed.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("stream status = %d headers = %#v body = %s", streamed.Code, streamed.Header(), streamed.Body.String())
	}
	streamedWithAPIKey := tenantReplayRequest(streamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", streamAPIKey)
	if streamedWithAPIKey.Code != http.StatusOK || streamedWithAPIKey.Body.String() != string(batchData) {
		t.Errorf("API-key replay stream = %d body=%q", streamedWithAPIKey.Code, streamedWithAPIKey.Body.String())
	}
	delete(storageClient.objects, mergedKey)
	missingObject := tenantReplayRequest(streamRoutes, http.MethodGet,
		"/v1/sessions/"+createResponse.ID+"/replay", accessToken)
	if missingObject.Code != http.StatusServiceUnavailable {
		t.Errorf("missing replay object status = %d", missingObject.Code)
	}
	storageClient.objects[mergedKey] = batchData
	deleted := tenantReplayRequest(tenantHandler, http.MethodDelete, "/v1/sessions/"+createResponse.ID, accessToken)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	if _, exists := storageClient.objects[mergedKey]; exists {
		t.Error("merged object remained after tenant deletion")
	}

	var expiredSessionID string
	expiredKey := "retention/expired.json"
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, ended_at, events_count,
			storage_key, rrweb_version, merged_at, merge_requested_at,
			max_duration_seconds, max_events, created_at
		) VALUES (
			$1, $2, gen_random_uuid(), $3, $3, 1, $4, '2.1.1', $3, $3, 1800, 50000, $3
		) RETURNING id
	`, tenantID, projectID, now.Add(-91*24*time.Hour), expiredKey).Scan(&expiredSessionID); err != nil {
		t.Fatalf("create expired replay: %v", err)
	}
	retentionStore := Store{Database: pools.Admin}
	expired, err := retentionStore.ExpiredSessions(ctx, now, 10)
	if err != nil {
		t.Fatalf("list expired sessions: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != expiredSessionID ||
		len(expired[0].ObjectKeys) != 1 || expired[0].ObjectKeys[0] != expiredKey {
		t.Fatalf("expired sessions = %#v", expired)
	}
	if err := retentionStore.DeleteExpiredSession(ctx, expiredSessionID); err != nil {
		t.Fatalf("delete expired session: %v", err)
	}

	var abandonedSessionID, activeSessionID, renewedSessionID, emptySessionID, incompleteSessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, events_count, batches_count,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, 1, 1, '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now.Add(-20*time.Minute)).Scan(&abandonedSessionID); err != nil {
		t.Fatalf("create abandoned replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, events_count, batches_count,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, 1, 1, '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now.Add(-20*time.Minute)).Scan(&activeSessionID); err != nil {
		t.Fatalf("create active replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, events_count, batches_count,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, 1, 1, '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now.Add(-20*time.Minute)).Scan(&renewedSessionID); err != nil {
		t.Fatalf("create renewed replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now.Add(-20*time.Minute)).Scan(&emptySessionID); err != nil {
		t.Fatalf("create empty abandoned replay: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now.Add(-20*time.Minute)).Scan(&incompleteSessionID); err != nil {
		t.Fatalf("create incomplete abandoned replay: %v", err)
	}
	abandonedCompletedKey := "abandoned/completed.json"
	abandonedIncompleteKey := "abandoned/incomplete.json"
	activeKey := "active/completed.json"
	renewedKey := "active/renewed.json"
	missingIncompleteKey := "abandoned/missing.json"
	if _, err := root.Exec(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, complete_token_hash, completed_at, created_at, upload_refreshed_at
		) VALUES
			($1, $2, $3, 0, $4, 1, 4, NULL, $5, $5, $5),
			($1, $2, $3, 1, $6, 1, 4, 'hash', NULL, $5, $5),
			($1, $2, $7, 0, $8, 1, 4, NULL, $9, $9, $9),
			($1, $2, $10, 0, $11, 1, 4, 'hash', NULL, $5, $5)
	`, tenantID, projectID, abandonedSessionID, abandonedCompletedKey,
		now.Add(-16*time.Minute), abandonedIncompleteKey, activeSessionID, activeKey,
		now.Add(-time.Minute), incompleteSessionID, missingIncompleteKey); err != nil {
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
	if len(got) != 2 || !queued[abandonedSessionID] || !queued[incompleteSessionID] {
		t.Fatalf("recovered replay queue = %#v", got)
	}
	var abandonedEndedAt, abandonedMergeRequestedAt, activeEndedAt, renewedEndedAt *time.Time
	var emptyEndedAt, emptyMergeRequestedAt, incompleteEndedAt, incompleteMergeRequestedAt *time.Time
	var emptyMergeError *string
	var emptyMergeAttempts int
	if err := root.QueryRow(ctx, `
		SELECT abandoned.ended_at, abandoned.merge_requested_at, active.ended_at, renewed.ended_at,
		       empty.ended_at, empty.merge_requested_at, empty.merge_error, empty.merge_attempts,
		       incomplete.ended_at, incomplete.merge_requested_at
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
	); err != nil {
		t.Fatalf("read recovered replay state: %v", err)
	}
	if abandonedEndedAt == nil || abandonedMergeRequestedAt == nil || activeEndedAt != nil || renewedEndedAt != nil ||
		emptyEndedAt == nil || emptyMergeRequestedAt != nil || emptyMergeError == nil ||
		*emptyMergeError != unavailableReplayMessage || emptyMergeAttempts != terminalMergeAttempts ||
		incompleteEndedAt == nil || incompleteMergeRequestedAt == nil {
		t.Errorf("recovered replay state = abandoned %v/%v active %v renewed %v empty %v/%v/%v/%d incomplete %v/%v",
			abandonedEndedAt, abandonedMergeRequestedAt, activeEndedAt, renewedEndedAt, emptyEndedAt,
			emptyMergeRequestedAt, emptyMergeError, emptyMergeAttempts,
			incompleteEndedAt, incompleteMergeRequestedAt)
	}
	if _, err := (Store{}).LockBatch(
		ctx, root, tenantID, projectID, incompleteSessionID, 0,
	); !errors.Is(err, ErrSessionFinalized) {
		t.Errorf("LockBatch(terminal replay) error = %v, want ErrSessionFinalized", err)
	}
	incompleteTask := asynq.NewTask(TaskMerge, []byte(fmt.Sprintf(`{"session_id":%q}`, incompleteSessionID)))
	storageFailure := errors.New("storage unavailable")
	storageClient.statErrors[missingIncompleteKey] = storageFailure
	if err := processor.MergeHandler()(ctx, incompleteTask); !errors.Is(err, storageFailure) || errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("transient replay recovery error = %v, want retryable storage failure", err)
	}
	var retryMergeError *string
	var retryMergeAttempts int
	if err := root.QueryRow(ctx, `
		SELECT merge_error, merge_attempts FROM sessions WHERE id = $1
	`, incompleteSessionID).Scan(&retryMergeError, &retryMergeAttempts); err != nil {
		t.Fatalf("read retryable replay state: %v", err)
	}
	if retryMergeError == nil || !strings.Contains(*retryMergeError, storageFailure.Error()) || retryMergeAttempts != 1 {
		t.Fatalf("retryable replay state = %v/%d", retryMergeError, retryMergeAttempts)
	}
	delete(storageClient.statErrors, missingIncompleteKey)
	if err := processor.MergeHandler()(ctx, incompleteTask); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("unavailable replay merge error = %v, want SkipRetry", err)
	}
	var incompleteMergeError *string
	var incompleteMergeAttempts int
	if err := root.QueryRow(ctx, `
		SELECT merge_error, merge_attempts FROM sessions WHERE id = $1
	`, incompleteSessionID).Scan(&incompleteMergeError, &incompleteMergeAttempts); err != nil {
		t.Fatalf("read unavailable replay state: %v", err)
	}
	if incompleteMergeError == nil || *incompleteMergeError != unavailableReplayMessage ||
		incompleteMergeAttempts != terminalMergeAttempts {
		t.Errorf("unavailable replay state = %v/%d", incompleteMergeError, incompleteMergeAttempts)
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
	storageClient.objects[abandonedCompletedKey] = []byte(`[{}]`)
	storageClient.contentTypes[abandonedCompletedKey] = "application/json"
	storageClient.objects[abandonedIncompleteKey] = []byte(`[{}]`)
	storageClient.contentTypes[abandonedIncompleteKey] = "application/json"
	abandonedTask := asynq.NewTask(TaskMerge, []byte(fmt.Sprintf(`{"session_id":%q}`, abandonedSessionID)))
	if err := processor.MergeHandler()(ctx, abandonedTask); err != nil {
		t.Fatalf("merge recovered replay: %v", err)
	}
	if _, exists := storageClient.objects[abandonedIncompleteKey]; exists {
		t.Error("incomplete recovered replay object was not removed")
	}
	recoveredKey := "tenants/" + tenantID + "/sessions/" + abandonedSessionID + "/replay.json"
	if got := string(storageClient.objects[recoveredKey]); got != `[{},{}]` {
		t.Errorf("recovered replay = %s, want two uploaded batches", got)
	}
	if err := root.QueryRow(ctx, `SELECT count(*) FROM session_batches WHERE session_id = $1`, abandonedSessionID).
		Scan(&batchRows); err != nil || batchRows != 0 {
		t.Fatalf("recovered replay batch rows = %d, error = %v", batchRows, err)
	}
	if err := root.QueryRow(ctx, `SELECT events_count, batches_count FROM sessions WHERE id = $1`, abandonedSessionID).
		Scan(&events, &batches); err != nil || events != 2 || batches != 2 {
		t.Errorf("recovered replay aggregates = %d/%d, error = %v", events, batches, err)
	}

	var failedSessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, ended_at, rrweb_version,
			merge_requested_at, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, $3, '2.1.1', $3, 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now).Scan(&failedSessionID); err != nil {
		t.Fatalf("create failed merge session: %v", err)
	}
	failedBatchKey := "failed/batch.json"
	failedBatchData := []byte(`[{}]`)
	if _, err := root.Exec(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, completed_at
		) VALUES ($1, $2, $3, 0, $4, 2, $5, $6)
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
	if mergeAttempts != 1 || mergeError == nil || !strings.Contains(*mergeError, "declared 2 events") {
		t.Errorf("failed merge attempts/error = %d/%v", mergeAttempts, mergeError)
	}
	if _, err := root.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, failedSessionID); err != nil {
		t.Fatalf("delete failed merge session: %v", err)
	}
	delete(storageClient.objects, failedBatchKey)
	delete(storageClient.contentTypes, failedBatchKey)

	var pendingSessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, ended_at, rrweb_version,
			merge_requested_at, max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, $3, '2.1.1', $3, 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now).Scan(&pendingSessionID); err != nil {
		t.Fatalf("create pending merge session: %v", err)
	}
	if err := scheduler.ReconcileOnce(ctx); err != nil {
		t.Fatalf("reconcile pending merges: %v", err)
	}
	if mergeQueue.sessionIDs[len(mergeQueue.sessionIDs)-1] != pendingSessionID {
		t.Errorf("reconciled sessions = %#v", mergeQueue.sessionIDs)
	}
	pendingStream := tenantReplayRequest(streamRoutes, http.MethodGet,
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
	sessionIDs []string
}

func (s *replayMergeQueueStub) EnqueueMerge(_ context.Context, sessionID string) error {
	s.sessionIDs = append(s.sessionIDs, sessionID)
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
