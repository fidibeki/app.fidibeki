package privacy

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestWorkspaceExportTracer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(t, ctx, database, "acme-privacy", "Acme secret feedback")
	otherTenantID, _ := seedPrivacyWorkspace(t, ctx, database, "beta-privacy", "Beta must never leak")

	store := Store{Database: database}
	job, created, err := store.CreateWorkspaceExport(ctx, database, tenantID, memberID, now)
	if err != nil || !created || job.Status != JobPending {
		t.Fatalf("CreateWorkspaceExport() = %#v, created=%t, error=%v", job, created, err)
	}
	duplicate, duplicateCreated, err := store.CreateWorkspaceExport(ctx, database, tenantID, memberID, now)
	if err != nil || duplicateCreated || duplicate.ID != job.ID {
		t.Fatalf("duplicate export = %#v, created=%t, error=%v", duplicate, duplicateCreated, err)
	}
	if _, _, err := store.CreateWorkspaceExport(ctx, database, tenantID, "00000000-0000-0000-0000-000000000000", now); !errors.Is(err, ErrHumanRequired) {
		t.Fatalf("machine export error = %v, want ErrHumanRequired", err)
	}
	foreignPayload, _ := json.Marshal(map[string]any{
		"job_id": job.ID, "tenant_id": otherTenantID, "generation": job.Generation,
	})
	if err := (Exporter{Database: database, Store: store, Storage: &privacyStorageCapture{}}).
		Handler()(ctx, asynq.NewTask(TaskExport, foreignPayload)); err != nil {
		t.Fatalf("foreign-tenant export task error = %v", err)
	}
	stillPending, err := store.GetJob(ctx, database, tenantID, job.ID)
	if err != nil || stillPending.Status != JobPending || stillPending.Attempts != 0 {
		t.Fatalf("job after foreign-tenant task = %#v, error=%v", stillPending, err)
	}

	prepared, err := store.PrepareJobs(ctx, now, 10)
	if err != nil || len(prepared) != 1 || prepared[0].ID != job.ID {
		t.Fatalf("PrepareJobs() = %#v, %v", prepared, err)
	}
	storage := &privacyStorageCapture{}
	exporter := Exporter{Database: database, Store: store, Storage: storage, Now: func() time.Time { return now }}
	payload, _ := json.Marshal(map[string]any{
		"job_id": job.ID, "tenant_id": tenantID, "generation": job.Generation,
	})
	if err := exporter.Handler()(ctx, asynq.NewTask(TaskExport, payload)); err != nil {
		t.Fatalf("export handler error = %v", err)
	}
	completed, err := store.GetJob(ctx, database, tenantID, job.ID)
	if err != nil || completed.Status != JobCompleted || completed.ExpiresAt == nil ||
		!completed.ExpiresAt.Equal(now.AddDate(0, 0, 7)) {
		t.Fatalf("completed export = %#v, error=%v", completed, err)
	}
	if storage.key != fmt.Sprintf("tenants/%s/exports/%s.zip", tenantID, job.ID) ||
		storage.contentType != "application/zip" || len(storage.body) == 0 {
		t.Fatalf("stored export key=%q type=%q bytes=%d", storage.key, storage.contentType, len(storage.body))
	}

	files := readPrivacyArchive(t, storage.body)
	wantFiles := []string{
		"attachments.jsonl", "feedback-tags.jsonl", "feedback.jsonl", "integrations.jsonl",
		"issue-activities.jsonl", "issue-comments.jsonl", "issue-tags.jsonl", "issue-updates.jsonl",
		"issues.jsonl", "manifest.json", "projects.jsonl", "replays.jsonl", "tags.jsonl",
		"widget-analytics-daily.jsonl", "widget-rules.jsonl", "widget-target-conditions.jsonl",
		"workspace.json",
	}
	gotFiles := make([]string, 0, len(files))
	for name := range files {
		gotFiles = append(gotFiles, name)
	}
	sort.Strings(gotFiles)
	if strings.Join(gotFiles, ",") != strings.Join(wantFiles, ",") {
		t.Fatalf("archive files = %v, want %v", gotFiles, wantFiles)
	}
	archiveText := string(bytes.Join(mapValues(files), nil))
	for _, forbidden := range []string{
		"Beta must never leak", otherTenantID, "https://hooks.secret.example/acme",
		"encrypted-secret", "fk_live_", "refresh_hash",
	} {
		if strings.Contains(archiveText, forbidden) {
			t.Errorf("archive leaked %q", forbidden)
		}
	}
	for _, required := range []string{
		"Acme secret feedback", "fidibeki.workspace-export.v1", "available_not_included",
		"credential_values_omitted",
	} {
		if !strings.Contains(archiveText, required) {
			t.Errorf("archive does not contain %q", required)
		}
	}
	for name, body := range files {
		if name == "manifest.json" || name == "workspace.json" {
			if !json.Valid(body) {
				t.Errorf("%s is not valid JSON", name)
			}
			continue
		}
		for lineNumber, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
			if len(line) > 0 && !json.Valid(line) {
				t.Errorf("%s line %d is not valid JSON: %s", name, lineNumber+1, line)
			}
		}
	}
	var auditCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind IN ('privacy.export_requested', 'privacy.export_completed')
	`, tenantID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("privacy audit count = %d, error=%v", auditCount, err)
	}

	jobs, err := store.ListJobs(ctx, database, otherTenantID, 50)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("other tenant jobs = %#v, error=%v", jobs, err)
	}
}

func TestWorkspaceExportFailureIsRetryable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(t, ctx, database, "retry-privacy", "Retry me")
	store := Store{Database: database}
	job, _, err := store.CreateWorkspaceExport(ctx, database, tenantID, memberID, now)
	if err != nil {
		t.Fatalf("create retry export: %v", err)
	}
	queueFailure := errors.New("redis unavailable")
	if err := (Scheduler{
		Store: store, Queue: privacyQueueFailure{err: queueFailure}, Now: func() time.Time { return now },
	}).ReconcileOnce(ctx); !errors.Is(err, queueFailure) {
		t.Fatalf("reconcile queue failure = %v, want %v", err, queueFailure)
	}
	afterQueueFailure, err := store.GetJob(ctx, database, tenantID, job.ID)
	if err != nil || afterQueueFailure.Status != JobPending || afterQueueFailure.EnqueuedAt != nil ||
		afterQueueFailure.Attempts != 0 {
		t.Fatalf("job after queue failure = %#v, error=%v", afterQueueFailure, err)
	}
	if _, err := store.PrepareJobs(ctx, now, 10); err != nil {
		t.Fatalf("prepare retry export: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"job_id": job.ID, "tenant_id": tenantID, "generation": job.Generation,
	})
	want := errors.New("storage unavailable")
	exporter := Exporter{
		Database: database, Store: store, Storage: &privacyStorageCapture{err: want},
		Now: func() time.Time { return now },
	}
	if err := exporter.Handler()(ctx, asynq.NewTask(TaskExport, payload)); err != nil {
		t.Fatalf("handled export failure error = %v", err)
	}
	retryable, err := store.GetJob(ctx, database, tenantID, job.ID)
	if err != nil || retryable.Status != JobPending || retryable.Generation != job.Generation+1 ||
		retryable.Attempts != 1 || retryable.ErrorMessage == nil || !strings.Contains(*retryable.ErrorMessage, want.Error()) {
		t.Fatalf("retryable export = %#v, error=%v", retryable, err)
	}
	if _, err := store.RetryJob(ctx, database, tenantID, memberID, job.ID, now); !errors.Is(err, ErrJobUnavailable) {
		t.Fatalf("retry pending export error = %v, want ErrJobUnavailable", err)
	}
}

type privacyQueueFailure struct {
	err error
}

func (queue privacyQueueFailure) Enqueue(context.Context, PreparedJob) error {
	return queue.err
}

func TestWorkspaceExportHTTPAuthorizationAndDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 23, 9, 30, 0, 0, time.UTC)
	tenantID, ownerID := seedPrivacyWorkspace(t, ctx, database, "http-privacy", "HTTP export")
	otherTenantID, otherOwnerID := seedPrivacyWorkspace(t, ctx, database, "other-http-privacy", "Other export")
	var viewerID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, 'viewer@http-privacy.test', ARRAY['tenant:read'], 'active') RETURNING id
	`, tenantID).Scan(&viewerID); err != nil {
		t.Fatalf("create privacy viewer: %v", err)
	}
	rawAPIKey := "fk_live_" + strings.Repeat("K", 32)
	if _, err := database.Exec(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions)
		VALUES ($1, 'Privacy machine', $2, 'fk_live_KKKK...', ARRAY['tenant:update'])
	`, tenantID, auth.HashToken(rawAPIKey)); err != nil {
		t.Fatalf("create privacy API key: %v", err)
	}

	jwtManager, err := auth.NewJWTManager(strings.Repeat("p", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create privacy JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant(ownerID, tenantID, auth.TenantPermissionStrings())
	viewerToken, _, _ := jwtManager.IssueTenant(viewerID, tenantID, []string{string(auth.PermissionTenantRead)})
	otherToken, _, _ := jwtManager.IssueTenant(otherOwnerID, otherTenantID, auth.TenantPermissionStrings())
	downloads := &privacyStorageCapture{downloadURL: "https://downloads.example.test/export.zip?signature=private"}
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{Database: database}, JWT: jwtManager, Storage: downloads,
		Now: func() time.Time { return now },
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
			if slug != "http-privacy" {
				return tenant.Tenant{}, tenant.ErrUnknownTenant
			}
			return tenant.Tenant{
				ID: tenantID, Slug: slug, Name: "HTTP Privacy", Plan: tenant.PlanGrowth,
				Status: tenant.StatusActive,
			}, nil
		}),
		Begin: db.BeginFromPool(database), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	createdResponse := privacyHTTPRequest(handler, ownerToken, http.MethodPost, "/v1/privacy/exports")
	if createdResponse.Code != http.StatusAccepted {
		t.Fatalf("create export = %d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var createdEnvelope struct {
		Export  Job  `json:"export"`
		Created bool `json:"created"`
	}
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode created export: %v", err)
	}
	if !createdEnvelope.Created || createdEnvelope.Export.ID == "" {
		t.Fatalf("created export = %#v", createdEnvelope)
	}
	if strings.Contains(createdResponse.Body.String(), "object_key") ||
		strings.Contains(createdResponse.Body.String(), "tenants/") {
		t.Fatalf("create response exposed storage metadata: %s", createdResponse.Body.String())
	}
	duplicate := privacyHTTPRequest(handler, ownerToken, http.MethodPost, "/v1/privacy/exports")
	if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), createdEnvelope.Export.ID) ||
		!strings.Contains(duplicate.Body.String(), `"created":false`) {
		t.Fatalf("duplicate export = %d body=%s", duplicate.Code, duplicate.Body.String())
	}
	viewer := privacyHTTPRequest(handler, viewerToken, http.MethodPost, "/v1/privacy/exports")
	if viewer.Code != http.StatusForbidden {
		t.Errorf("viewer export = %d body=%s", viewer.Code, viewer.Body.String())
	}
	machine := privacyHTTPRequest(handler, rawAPIKey, http.MethodGet, "/v1/privacy/exports")
	if machine.Code != http.StatusForbidden || !strings.Contains(machine.Body.String(), "active human member required") {
		t.Errorf("machine export list = %d body=%s", machine.Code, machine.Body.String())
	}
	crossTenant := privacyHTTPRequest(handler, otherToken, http.MethodGet, "/v1/privacy/exports")
	if crossTenant.Code != http.StatusForbidden {
		t.Errorf("cross-tenant export list = %d body=%s", crossTenant.Code, crossTenant.Body.String())
	}
	pendingDownload := privacyHTTPRequest(
		handler, ownerToken, http.MethodPost, "/v1/privacy/exports/"+createdEnvelope.Export.ID+"/download",
	)
	if pendingDownload.Code != http.StatusConflict {
		t.Fatalf("pending download = %d body=%s", pendingDownload.Code, pendingDownload.Body.String())
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_jobs
		SET status = 'completed', started_at = $2::timestamptz, heartbeat_at = $2::timestamptz,
		    completed_at = $2::timestamptz, expires_at = $2::timestamptz + interval '7 days',
		    object_key = 'tenants/' || tenant_id::text || '/exports/' || id::text || '.zip',
		    object_size_bytes = 2048
		WHERE id = $1
	`, createdEnvelope.Export.ID, now); err != nil {
		t.Fatalf("complete HTTP privacy export: %v", err)
	}
	download := privacyHTTPRequest(
		handler, ownerToken, http.MethodPost, "/v1/privacy/exports/"+createdEnvelope.Export.ID+"/download",
	)
	if download.Code != http.StatusOK || download.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(download.Body.String(), downloads.downloadURL) {
		t.Fatalf("completed download = %d headers=%#v body=%s", download.Code, download.Header(), download.Body.String())
	}
	if downloads.downloadKey != fmt.Sprintf("tenants/%s/exports/%s.zip", tenantID, createdEnvelope.Export.ID) ||
		downloads.downloadFilename != "fidibeki-http-privacy-workspace-export-2026-07-23.zip" ||
		downloads.downloadTTL != downloadLinkTTL {
		t.Errorf("download request = key %q filename %q ttl %s", downloads.downloadKey, downloads.downloadFilename, downloads.downloadTTL)
	}
	var grants int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy.export_download_granted'
	`, tenantID).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("download grant audits = %d, error=%v", grants, err)
	}
	missing := privacyHTTPRequest(
		handler, ownerToken, http.MethodPost,
		"/v1/privacy/exports/00000000-0000-4000-8000-000000000099/download",
	)
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing export download = %d body=%s", missing.Code, missing.Body.String())
	}
}

type privacyStorageCapture struct {
	key              string
	body             []byte
	contentType      string
	err              error
	downloadURL      string
	downloadKey      string
	downloadFilename string
	downloadTTL      time.Duration
}

func (storage *privacyStorageCapture) PresignGet(
	_ context.Context,
	key string,
	filename string,
	ttl time.Duration,
) (string, error) {
	if storage.err != nil {
		return "", storage.err
	}
	storage.downloadKey, storage.downloadFilename, storage.downloadTTL = key, filename, ttl
	return storage.downloadURL, nil
}

func privacyHTTPRequest(
	handler http.Handler,
	token string,
	method string,
	path string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://http-privacy.localhost"+path, nil)
	request.Host = "http-privacy.localhost"
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func (storage *privacyStorageCapture) PutObject(
	_ context.Context,
	key string,
	reader io.Reader,
	_ int64,
	contentType string,
) error {
	if storage.err != nil {
		return storage.err
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	storage.key, storage.body, storage.contentType = key, body, contentType
	return nil
}

func readPrivacyArchive(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open privacy archive: %v", err)
	}
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatalf("open archive file %s: %v", file.Name, err)
		}
		contents, readErr := io.ReadAll(opened)
		closeErr := opened.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read archive file %s: read=%v close=%v", file.Name, readErr, closeErr)
		}
		files[file.Name] = contents
	}
	return files
}

func mapValues(values map[string][]byte) [][]byte {
	result := make([][]byte, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func seedPrivacyWorkspace(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	slug string,
	feedbackTitle string,
) (string, string) {
	t.Helper()
	var tenantID, memberID, projectID, ruleID, feedbackID, issueID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ($1, $2, 'growth', 'active') RETURNING id
	`, strings.ToUpper(slug), slug).Scan(&tenantID); err != nil {
		t.Fatalf("create privacy tenant: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status, last_login_at)
		VALUES ($1, $2, ARRAY['tenant:read', 'tenant:update'], 'active', now()) RETURNING id
	`, tenantID, "owner@"+slug+".test").Scan(&memberID); err != nil {
		t.Fatalf("create privacy member: %v", err)
	}
	if _, err := database.Exec(ctx, `UPDATE tenants SET owner_member_id = $2 WHERE id = $1`, tenantID, memberID); err != nil {
		t.Fatalf("assign privacy owner: %v", err)
	}
	publicKeyDigest := sha256.Sum256([]byte(slug))
	publicKeySuffix := fmt.Sprintf("%x", publicKeyDigest)[:22]
	if err := database.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Storefront', $2, ARRAY['shop.example.test']) RETURNING id
	`, tenantID, "pk_live_"+publicKeySuffix).Scan(&projectID); err != nil {
		t.Fatalf("create privacy project: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, matcher_value, trigger_type, trigger_delay_seconds,
			collect_contact, response_blocks
		) VALUES ($1, 'Checkout pulse', 'url_prefix', 'https://shop.example.test/checkout', 'delay', 2, true,
		          '[{"key":"experience","type":"free_text","required":false}]')
		RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create privacy rule: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO widget_rule_target_conditions (
			widget_rule_id, kind, matcher_type, matcher_value, position
		) VALUES ($1, 'exclude', 'url_prefix', 'https://shop.example.test/admin', 0)
	`, ruleID); err != nil {
		t.Fatalf("create privacy target: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO issues (
			tenant_id, project_id, public_id, title, description, reporter_email, notify_reporter
		) VALUES ($1, $2, 'WEB-2026-10000', $3, 'Issue description', 'reporter@example.test', true)
		RETURNING id
	`, tenantID, projectID, feedbackTitle).Scan(&issueID); err != nil {
		t.Fatalf("create privacy issue: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, widget_rule_id, public_id, issue_id, client_feedback_id,
			rating, text, title, description, reporter_email, notify_reporter, page_url,
			text_process_status, responses
		) VALUES ($1, $2, $3, 'fb_AbCd1234', $4, gen_random_uuid(), 2, $5::text, $5::text, $5::text,
		          'reporter@example.test', true, 'https://shop.example.test/checkout', 'completed',
		          jsonb_build_object('experience', $5::text))
		RETURNING id
	`, tenantID, projectID, ruleID, issueID, feedbackTitle).Scan(&feedbackID); err != nil {
		t.Fatalf("create privacy feedback: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at, transcription
		) VALUES ($1, $2, 'audio', gen_random_uuid(), $3, 'audio/webm', 42,
		          'completed', now(), 'Checkout audio transcript')
	`, tenantID, feedbackID, "tenants/"+tenantID+"/feedback/audio.webm"); err != nil {
		t.Fatalf("create privacy attachment: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO issue_updates (tenant_id, issue_id, member_id, old_status, new_status, comment)
		VALUES ($1, $2, $3, 'open', 'in_progress', 'Investigating')
	`, tenantID, issueID, memberID); err != nil {
		t.Fatalf("create privacy issue update: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO issue_activities (tenant_id, issue_id, actor_member_id, kind, data)
		VALUES ($1, $2, $3::uuid, 'issue.assigned', jsonb_build_object('assignee_id', $3::text))
	`, tenantID, issueID, memberID); err != nil {
		t.Fatalf("create privacy activity: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO issue_comments (tenant_id, issue_id, author_member_id, body)
		VALUES ($1, $2, $3, 'Operational comment')
	`, tenantID, issueID, memberID); err != nil {
		t.Fatalf("create privacy comment: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO integrations (tenant_id, kind, name, webhook_url, channel, events, is_active)
		VALUES ($1, 'slack', 'Private ops', 'https://hooks.secret.example/acme', '#ops',
		        ARRAY['feedback.created']::event_type[], true)
	`, tenantID); err != nil {
		t.Fatalf("create privacy integration: %v", err)
	}
	return tenantID, memberID
}

func privacyTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start privacy PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("privacy PostgreSQL URL: %v", err)
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve privacy test path")
	}
	migrations := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations")
	migrator, err := db.NewMigrator(directURL, migrations)
	if err != nil {
		t.Fatalf("create privacy migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate privacy database: %v", err)
	}
	config, err := pgxpool.ParseConfig(directURL)
	if err != nil {
		t.Fatalf("parse privacy database URL: %v", err)
	}
	config.MaxConns = 8
	database, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open privacy database: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}
