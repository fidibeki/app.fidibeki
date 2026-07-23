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
	appemail "github.com/acidiney/app.fidibeki/backend/internal/email"
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
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
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
	emails := &privacyEmailCapture{}
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{Database: database}, JWT: jwtManager, Storage: downloads, Email: emails,
		PublicBaseURL: "http://api.localhost:8088", BaseDomain: "localhost",
		From: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return now },
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
	listedExports := privacyHTTPRequest(handler, ownerToken, http.MethodGet, "/v1/privacy/exports")
	if listedExports.Code != http.StatusOK ||
		!strings.Contains(listedExports.Body.String(), createdEnvelope.Export.ID) {
		t.Fatalf("list exports = %d body=%s", listedExports.Code, listedExports.Body.String())
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
	retention := privacyHTTPRequest(handler, viewerToken, http.MethodGet, "/v1/privacy/retention")
	if retention.Code != http.StatusOK ||
		!strings.Contains(retention.Body.String(), `"plan":"growth"`) ||
		!strings.Contains(retention.Body.String(), `"duration_days":90`) ||
		!strings.Contains(retention.Body.String(), `"category":"attachments"`) {
		t.Fatalf("viewer retention = %d body=%s", retention.Code, retention.Body.String())
	}
	machineRetention := privacyHTTPRequest(handler, rawAPIKey, http.MethodGet, "/v1/privacy/retention")
	if machineRetention.Code != http.StatusForbidden {
		t.Errorf("machine retention = %d body=%s",
			machineRetention.Code, machineRetention.Body.String())
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
		downloads.downloadFilename != "fidibeki-http-privacy-workspace-export-"+now.Format("2006-01-02")+".zip" ||
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
	invalidJob := privacyHTTPRequest(
		handler, ownerToken, http.MethodPost, "/v1/privacy/exports/not-a-uuid/retry",
	)
	if invalidJob.Code != http.StatusNotFound {
		t.Errorf("invalid export retry = %d body=%s", invalidJob.Code, invalidJob.Body.String())
	}
	retryCreated := privacyHTTPRequest(handler, ownerToken, http.MethodPost, "/v1/privacy/exports")
	var retryEnvelope struct {
		Export Job `json:"export"`
	}
	if retryCreated.Code != http.StatusAccepted ||
		json.Unmarshal(retryCreated.Body.Bytes(), &retryEnvelope) != nil ||
		retryEnvelope.Export.ID == "" {
		t.Fatalf("create retry export = %d body=%s", retryCreated.Code, retryCreated.Body.String())
	}
	if _, err := database.Exec(ctx, `
		UPDATE privacy_jobs SET status = 'failed', error_message = 'terminal test failure'
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, retryEnvelope.Export.ID); err != nil {
		t.Fatalf("fail retry export: %v", err)
	}
	retried := privacyHTTPRequest(
		handler, ownerToken, http.MethodPost,
		"/v1/privacy/exports/"+retryEnvelope.Export.ID+"/retry",
	)
	if retried.Code != http.StatusAccepted ||
		!strings.Contains(retried.Body.String(), `"status":"pending"`) {
		t.Fatalf("retry export = %d body=%s", retried.Code, retried.Body.String())
	}

	for name, test := range map[string]struct {
		body   string
		status int
	}{
		"malformed":       {body: `{`, status: http.StatusBadRequest},
		"trailing":        {body: `{"email":"a@b.test","action":"export"} {}`, status: http.StatusBadRequest},
		"unknown action":  {body: `{"email":"a@b.test","action":"erase"}`, status: http.StatusUnprocessableEntity},
		"invalid address": {body: `{"email":"invalid","action":"export"}`, status: http.StatusUnprocessableEntity},
	} {
		response := privacyHTTPJSONRequest(
			handler, ownerToken, http.MethodPost, "/v1/privacy/requests", test.body,
		)
		if response.Code != test.status {
			t.Errorf("%s reporter request = %d body=%s",
				name, response.Code, response.Body.String())
		}
	}
	reporterRequestResponse := privacyHTTPJSONRequest(
		handler, ownerToken, http.MethodPost, "/v1/privacy/requests",
		`{"email":" Reporter@Example.Test ","action":"export"}`,
	)
	if reporterRequestResponse.Code != http.StatusAccepted || len(emails.messages) != 1 ||
		emails.messages[0].To != "reporter@example.test" ||
		strings.Contains(reporterRequestResponse.Body.String(), "/privacy/") {
		t.Fatalf("reporter request = %d body=%s messages=%#v",
			reporterRequestResponse.Code, reporterRequestResponse.Body.String(), emails.messages)
	}
	var requestEnvelope struct {
		Request ReporterRequest `json:"request"`
	}
	if err := json.Unmarshal(reporterRequestResponse.Body.Bytes(), &requestEnvelope); err != nil ||
		requestEnvelope.Request.ID == "" {
		t.Fatalf("decode reporter request: %#v error=%v", requestEnvelope, err)
	}
	listedRequests := privacyHTTPRequest(handler, ownerToken, http.MethodGet, "/v1/privacy/requests")
	if listedRequests.Code != http.StatusOK ||
		!strings.Contains(listedRequests.Body.String(), requestEnvelope.Request.ID) ||
		!strings.Contains(listedRequests.Body.String(), "reporter@example.test") {
		t.Fatalf("list reporter requests = %d body=%s",
			listedRequests.Code, listedRequests.Body.String())
	}
	marker := "/privacy/"
	markerIndex := strings.Index(emails.messages[0].Text, marker)
	if markerIndex < 0 {
		t.Fatalf("verification email has no privacy URL: %s", emails.messages[0].Text)
	}
	reporterToken := strings.SplitN(emails.messages[0].Text[markerIndex+len(marker):], "\n", 2)[0]
	if _, ok := parseReporterRequestToken(reporterToken); !ok {
		t.Fatalf("verification email token is invalid: %q", reporterToken)
	}
	machineRequest := privacyHTTPJSONRequest(
		handler, rawAPIKey, http.MethodPost, "/v1/privacy/requests",
		`{"email":"reporter@example.test","action":"anonymize"}`,
	)
	if machineRequest.Code != http.StatusForbidden {
		t.Errorf("machine reporter request = %d body=%s", machineRequest.Code, machineRequest.Body.String())
	}
	viewerRequest := privacyHTTPJSONRequest(
		handler, viewerToken, http.MethodPost, "/v1/privacy/requests",
		`{"email":"reporter@example.test","action":"anonymize"}`,
	)
	if viewerRequest.Code != http.StatusForbidden {
		t.Errorf("viewer reporter request = %d body=%s", viewerRequest.Code, viewerRequest.Body.String())
	}

	publicRoutes := chi.NewRouter()
	RegisterPublicRoutes(publicRoutes, PublicHTTPOptions{
		Store: Store{Database: database}, Begin: db.BeginFromPool(database), Storage: downloads,
		Now: func() time.Time { return now },
	})
	beforeVerification := privacyHTTPRequest(
		publicRoutes, "", http.MethodGet, "/v1/privacy/requests/"+reporterToken,
	)
	if beforeVerification.Code != http.StatusNotFound {
		t.Fatalf("unverified public status = %d body=%s", beforeVerification.Code, beforeVerification.Body.String())
	}
	verified := privacyHTTPRequest(
		publicRoutes, "", http.MethodPost, "/v1/privacy/requests/"+reporterToken+"/verify",
	)
	if verified.Code != http.StatusOK || strings.Contains(verified.Body.String(), "reporter@example.test") ||
		verified.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("public verification = %d headers=%#v body=%s", verified.Code, verified.Header(), verified.Body.String())
	}
	reporterJob := reporterRequestJob(
		t, ctx, database, Store{Database: database}, tenantID, requestEnvelope.Request.ID,
	)
	if err := (Exporter{
		Database: database, Store: Store{Database: database}, Storage: downloads,
		Now: func() time.Time { return now },
	}).Handler()(ctx, reporterJobTask(reporterJob)); err != nil {
		t.Fatalf("HTTP reporter export handler: %v", err)
	}
	publicDownload := privacyHTTPRequest(
		publicRoutes, "", http.MethodPost, "/v1/privacy/requests/"+reporterToken+"/download",
	)
	if publicDownload.Code != http.StatusOK ||
		downloads.downloadFilename != "fidibeki-reporter-data-"+now.Format("2006-01-02")+".zip" ||
		!strings.Contains(publicDownload.Body.String(), downloads.downloadURL) {
		t.Fatalf("public reporter download = %d filename=%q body=%s",
			publicDownload.Code, downloads.downloadFilename, publicDownload.Body.String())
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy.reporter_export_download_granted'
	`, tenantID).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("reporter download grants = %d, error=%v", grants, err)
	}
	cancelledCtx, cancelScheduler := context.WithCancel(context.Background())
	cancelScheduler()
	if err := (Scheduler{Store: Store{Database: database}}).
		Run(cancelledCtx, time.Hour); err != nil {
		t.Fatalf("cancelled privacy scheduler error = %v", err)
	}
}

func TestReporterPrivacyRequestIsHashedDeduplicatedAndAudited(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC)
	tenantID, memberID := seedPrivacyWorkspace(t, ctx, database, "request-privacy", "Privacy request")
	store := Store{Database: database}

	request, token, shouldEmail, err := store.CreateReporterRequest(
		ctx, database, tenantID, memberID, " Reporter@Example.Test ", ReporterExport, now,
	)
	if err != nil || !shouldEmail || request.ReporterEmail == nil ||
		*request.ReporterEmail != "reporter@example.test" || request.Status != ReporterPendingVerification {
		t.Fatalf("CreateReporterRequest() = %#v token=%q email=%t error=%v", request, token, shouldEmail, err)
	}
	if _, ok := parseReporterRequestToken(token); !ok {
		t.Fatalf("created reporter token is invalid: %q", token)
	}
	var storedTokenHash, storedDigest []byte
	var consentGranted *time.Time
	if err := database.QueryRow(ctx, `
		SELECT token_hash, email_digest FROM reporter_privacy_requests WHERE id = $1
	`, request.ID).Scan(&storedTokenHash, &storedDigest); err != nil {
		t.Fatalf("read reporter request hashes: %v", err)
	}
	wantTokenHash := sha256.Sum256([]byte(token))
	wantDigest := reporterEmailDigest("reporter@example.test")
	if !bytes.Equal(storedTokenHash, wantTokenHash[:]) || !bytes.Equal(storedDigest, wantDigest[:]) ||
		bytes.Contains(storedTokenHash, []byte(token)) {
		t.Fatalf("stored token/digest do not match hashes")
	}
	if err := database.QueryRow(ctx, `
		SELECT reporter_consent_granted_at FROM feedbacks
		WHERE tenant_id = $1 AND reporter_email = 'reporter@example.test'
	`, tenantID).Scan(&consentGranted); err != nil || consentGranted == nil {
		t.Fatalf("backfilled feedback consent = %v, error=%v", consentGranted, err)
	}

	duplicate, rotatedToken, duplicateEmail, err := store.CreateReporterRequest(
		ctx, database, tenantID, memberID, "reporter@example.test", ReporterExport, now.Add(time.Hour),
	)
	if err != nil || !duplicateEmail || duplicate.ID != request.ID || rotatedToken == token ||
		!duplicate.VerificationExpires.Equal(now.Add(time.Hour+reporterVerificationTTL)) {
		t.Fatalf("duplicate request = %#v token=%q email=%t error=%v", duplicate, rotatedToken, duplicateEmail, err)
	}
	requests, err := store.ListReporterRequests(ctx, database, tenantID, 50)
	if err != nil || len(requests) != 1 || requests[0].ID != request.ID {
		t.Fatalf("ListReporterRequests() = %#v, error=%v", requests, err)
	}
	oldTokenHash, _ := parseReporterRequestToken(token)
	if _, err := store.VerifyReporterRequest(ctx, database, oldTokenHash, now.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rotated reporter token verification error = %v, want ErrNotFound", err)
	}
	rotatedTokenHash, _ := parseReporterRequestToken(rotatedToken)
	view, err := store.VerifyReporterRequest(ctx, database, rotatedTokenHash, now.Add(time.Hour))
	if err != nil || view.Status != ReporterProcessing || view.Action != ReporterExport ||
		view.JobStatus == nil || *view.JobStatus != JobPending || view.DownloadAvailable {
		t.Fatalf("verified reporter request = %#v, error=%v", view, err)
	}
	idempotentView, err := store.VerifyReporterRequest(ctx, database, rotatedTokenHash, now.Add(2*time.Hour))
	if err != nil || idempotentView.Status != ReporterProcessing {
		t.Fatalf("idempotent reporter verification = %#v, error=%v", idempotentView, err)
	}
	var reporterJobCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_jobs
		WHERE tenant_id = $1 AND reporter_request_id = $2 AND kind = 'reporter_export'
	`, tenantID, request.ID).Scan(&reporterJobCount); err != nil || reporterJobCount != 1 {
		t.Fatalf("reporter export jobs = %d, error=%v", reporterJobCount, err)
	}
	processingDuplicate, _, processingEmail, err := store.CreateReporterRequest(
		ctx, database, tenantID, memberID, "reporter@example.test", ReporterExport, now.Add(2*time.Hour),
	)
	if err != nil || processingEmail || processingDuplicate.ID != request.ID ||
		processingDuplicate.Status != ReporterProcessing {
		t.Fatalf("processing duplicate = %#v email=%t error=%v", processingDuplicate, processingEmail, err)
	}

	expiring, expiringToken, _, err := store.CreateReporterRequest(
		ctx, database, tenantID, memberID, "other-reporter@example.test", ReporterAnonymize, now,
	)
	if err != nil {
		t.Fatalf("create expiring reporter request: %v", err)
	}
	expiringHash, _ := parseReporterRequestToken(expiringToken)
	if _, err := store.VerifyReporterRequest(ctx, database, expiringHash, now.Add(reporterVerificationTTL+time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired reporter verification error = %v, want ErrNotFound", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_jobs WHERE tenant_id = $1 AND reporter_request_id = $2
	`, tenantID, expiring.ID).Scan(&reporterJobCount); err != nil || reporterJobCount != 0 {
		t.Fatalf("expired reporter jobs = %d, error=%v", reporterJobCount, err)
	}
	terminal, terminalToken, _, err := store.CreateReporterRequest(
		ctx, database, tenantID, memberID, "terminal@example.test", ReporterExport, now,
	)
	if err != nil {
		t.Fatalf("create terminal reporter request: %v", err)
	}
	terminalHash, _ := parseReporterRequestToken(terminalToken)
	if _, err := store.VerifyReporterRequest(ctx, database, terminalHash, now); err != nil {
		t.Fatalf("verify terminal reporter request: %v", err)
	}
	terminalJob := reporterRequestJob(t, ctx, database, store, tenantID, terminal.ID)
	if _, err := database.Exec(ctx, `
		UPDATE privacy_jobs SET attempts = 11 WHERE id = $1
	`, terminalJob.ID); err != nil {
		t.Fatalf("prepare terminal reporter job: %v", err)
	}
	terminalJob.Attempts = 11
	if err := (Exporter{
		Database: database, Store: store, Storage: &privacyStorageCapture{err: errors.New("terminal storage failure")},
		Now: func() time.Time { return now },
	}).Handler()(ctx, reporterJobTask(terminalJob)); err != nil {
		t.Fatalf("handle terminal reporter failure: %v", err)
	}
	terminalView, err := store.GetReporterRequestView(ctx, terminalHash, now)
	if err != nil || terminalView.Status != ReporterFailed ||
		terminalView.JobStatus == nil || *terminalView.JobStatus != JobFailed {
		t.Fatalf("terminal reporter request = %#v, error=%v", terminalView, err)
	}
	retriedJob, err := store.RetryJob(
		ctx, database, tenantID, memberID, terminalJob.ID, now.Add(time.Minute),
	)
	if err != nil || retriedJob.Status != JobPending || retriedJob.Attempts != 0 {
		t.Fatalf("retried reporter job = %#v, error=%v", retriedJob, err)
	}
	retriedView, err := store.GetReporterRequestView(ctx, terminalHash, now.Add(time.Minute))
	if err != nil || retriedView.Status != ReporterProcessing ||
		retriedView.JobStatus == nil || *retriedView.JobStatus != JobPending {
		t.Fatalf("retried reporter request = %#v, error=%v", retriedView, err)
	}
	var requestAudits int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy.reporter_request_requested'
	`, tenantID).Scan(&requestAudits); err != nil || requestAudits != 5 {
		t.Fatalf("reporter request audits = %d, error=%v, want 5", requestAudits, err)
	}
	var verifiedAudits int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy.reporter_request_verified'
	`, tenantID).Scan(&verifiedAudits); err != nil || verifiedAudits != 2 {
		t.Fatalf("reporter verification audits = %d, error=%v", verifiedAudits, err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE feedbacks SET notify_reporter = false
		WHERE tenant_id = $1 AND reporter_email = 'reporter@example.test'
	`, tenantID); err != nil {
		t.Fatalf("withdraw reporter consent: %v", err)
	}
	var withdrawn *time.Time
	if err := database.QueryRow(ctx, `
		SELECT reporter_consent_withdrawn_at FROM feedbacks
		WHERE tenant_id = $1 AND reporter_email = 'reporter@example.test'
	`, tenantID).Scan(&withdrawn); err != nil || withdrawn == nil {
		t.Fatalf("withdrawn feedback consent = %v, error=%v", withdrawn, err)
	}
}

func TestReporterExportAndAnonymizationTracer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	tenantID, memberID := seedPrivacyWorkspace(t, ctx, database, "rights-privacy", "Reporter checkout problem")
	otherTenantID, _ := seedPrivacyWorkspace(t, ctx, database, "rights-other", "Other reporter private data")
	var feedbackID, issueID, projectID string
	if err := database.QueryRow(ctx, `
		SELECT feedback.id, feedback.issue_id, feedback.project_id
		FROM feedbacks AS feedback WHERE feedback.tenant_id = $1
	`, tenantID).Scan(&feedbackID, &issueID, &projectID); err != nil {
		t.Fatalf("read reporter fixture IDs: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE feedbacks SET dom_snapshot_html = '<main>Reporter Name</main>',
		                     dom_snapshot_styles = 'main{color:red}'
		WHERE id = $1
	`, feedbackID); err != nil {
		t.Fatalf("add reporter DOM snapshot: %v", err)
	}
	var sessionID string
	if err := database.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, feedback_id, session_id, page_url, started_at,
			ended_at, storage_key, rrweb_version, merged_at, merge_requested_at,
			max_duration_seconds, max_events
		) VALUES ($1, $2, $3, gen_random_uuid(), 'https://shop.example.test/checkout',
		          $4::timestamptz - interval '10 minutes', $4::timestamptz - interval '5 minutes', $5, '2.1.0',
		          $4::timestamptz - interval '4 minutes', $4::timestamptz - interval '6 minutes', 300, 1000)
		RETURNING id
	`, tenantID, projectID, feedbackID, now,
		"tenants/"+tenantID+"/replays/reporter.json").Scan(&sessionID); err != nil {
		t.Fatalf("create reporter replay: %v", err)
	}
	batchKey := "tenants/" + tenantID + "/replays/reporter-batch.json"
	if _, err := database.Exec(ctx, `
			INSERT INTO session_batches (
				tenant_id, project_id, session_id, batch_index, storage_key,
				events_count, size_bytes, completed_at, safety_status, safety_checked_at
			) VALUES (
				$1, $2, $3, 0, $4, 3, 128, $5::timestamptz - interval '7 minutes',
				'safe', $5::timestamptz - interval '7 minutes'
			)
		`, tenantID, projectID, sessionID, batchKey, now); err != nil {
		t.Fatalf("create reporter replay batch: %v", err)
	}
	progressTokenHash := sha256.Sum256([]byte("reporter-progress-token"))
	if _, err := database.Exec(ctx, `
		INSERT INTO reporter_access_tokens (tenant_id, project_id, issue_id, token_hash)
		VALUES ($1, $2, $3, $4)
	`, tenantID, projectID, issueID, progressTokenHash[:]); err != nil {
		t.Fatalf("create reporter progress token: %v", err)
	}

	store := Store{Database: database}
	exportRequest, exportToken, _, err := store.CreateReporterRequest(
		ctx, database, tenantID, memberID, "reporter@example.test", ReporterExport, now,
	)
	if err != nil {
		t.Fatalf("create reporter export request: %v", err)
	}
	exportHash, _ := parseReporterRequestToken(exportToken)
	if _, err := store.VerifyReporterRequest(ctx, database, exportHash, now); err != nil {
		t.Fatalf("verify reporter export request: %v", err)
	}
	exportJob := reporterRequestJob(t, ctx, database, store, tenantID, exportRequest.ID)
	exportStorage := &privacyStorageCapture{}
	exporter := Exporter{
		Database: database, Store: store, Storage: exportStorage, Now: func() time.Time { return now },
	}
	if err := exporter.Handler()(ctx, reporterJobTask(exportJob)); err != nil {
		t.Fatalf("reporter export handler: %v", err)
	}
	exportFiles := readPrivacyArchive(t, exportStorage.body)
	archiveText := string(bytes.Join(mapValues(exportFiles), nil))
	for _, required := range []string{
		"fidibeki.reporter-export.v1", "Reporter checkout problem", "Checkout audio transcript",
		"Operational comment", "Investigating",
	} {
		if !strings.Contains(archiveText, required) {
			t.Errorf("reporter archive does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"Other reporter private data", otherTenantID, "hooks.secret"} {
		if strings.Contains(archiveText, forbidden) {
			t.Errorf("reporter archive leaked %q", forbidden)
		}
	}
	exportView, err := store.GetReporterRequestView(ctx, exportHash, now)
	if err != nil || exportView.Status != ReporterCompleted || !exportView.DownloadAvailable ||
		exportView.RecordCount == nil || *exportView.RecordCount != 2 {
		t.Fatalf("completed reporter export view = %#v, error=%v", exportView, err)
	}

	anonymizeRequest, anonymizeToken, _, err := store.CreateReporterRequest(
		ctx, database, tenantID, memberID, "reporter@example.test", ReporterAnonymize, now,
	)
	if err != nil {
		t.Fatalf("create anonymization request: %v", err)
	}
	anonymizeHash, _ := parseReporterRequestToken(anonymizeToken)
	if _, err := store.VerifyReporterRequest(ctx, database, anonymizeHash, now); err != nil {
		t.Fatalf("verify anonymization request: %v", err)
	}
	anonymizeJob := reporterRequestJob(t, ctx, database, store, tenantID, anonymizeRequest.ID)
	storageFailure := errors.New("object storage unavailable")
	failingStorage := &privacyStorageCapture{err: storageFailure}
	failingExporter := Exporter{
		Database: database, Store: store, Storage: failingStorage, Now: func() time.Time { return now },
	}
	if err := failingExporter.Handler()(ctx, reporterJobTask(anonymizeJob)); err != nil {
		t.Fatalf("handled anonymization storage failure: %v", err)
	}
	var reporterEmail *string
	if err := database.QueryRow(ctx, `SELECT reporter_email FROM feedbacks WHERE id = $1`, feedbackID).
		Scan(&reporterEmail); err != nil || reporterEmail == nil {
		t.Fatalf("reporter email after storage failure = %v, error=%v", reporterEmail, err)
	}
	anonymizeJob, err = store.GetJob(ctx, database, tenantID, anonymizeJob.ID)
	if err != nil || anonymizeJob.Status != JobPending || anonymizeJob.Generation != 2 {
		t.Fatalf("retryable anonymization job = %#v, error=%v", anonymizeJob, err)
	}
	anonymizeStorage := &privacyStorageCapture{}
	successfulAnonymizer := Exporter{
		Database: database, Store: store, Storage: anonymizeStorage, Now: func() time.Time { return now },
	}
	if err := successfulAnonymizer.Handler()(ctx, reporterJobTask(anonymizeJob)); err != nil {
		t.Fatalf("reporter anonymization handler: %v", err)
	}
	wantRemoved := []string{
		"tenants/" + tenantID + "/feedback/audio.webm",
		batchKey,
		"tenants/" + tenantID + "/replays/reporter.json",
	}
	sort.Strings(wantRemoved)
	if strings.Join(anonymizeStorage.removedKeys, ",") != strings.Join(wantRemoved, ",") {
		t.Errorf("removed reporter objects = %v, want %v", anonymizeStorage.removedKeys, wantRemoved)
	}
	var notify bool
	var domHTML, domStyles *string
	var feedbackTitle, feedbackDescription string
	if err := database.QueryRow(ctx, `
		SELECT reporter_email, notify_reporter, dom_snapshot_html, dom_snapshot_styles,
		       title, description
		FROM feedbacks WHERE id = $1
	`, feedbackID).Scan(
		&reporterEmail, &notify, &domHTML, &domStyles, &feedbackTitle, &feedbackDescription,
	); err != nil || reporterEmail != nil || notify || domHTML != nil || domStyles != nil ||
		feedbackTitle != "Reporter checkout problem" || feedbackDescription != "Reporter checkout problem" {
		t.Fatalf("anonymized feedback = email=%v notify=%t dom=%v/%v title=%q description=%q error=%v",
			reporterEmail, notify, domHTML, domStyles, feedbackTitle, feedbackDescription, err)
	}
	var attachmentKey, transcription *string
	var attachmentDeleted *time.Time
	if err := database.QueryRow(ctx, `
		SELECT storage_key, transcription, retention_deleted_at
		FROM attachments WHERE feedback_id = $1
	`, feedbackID).Scan(&attachmentKey, &transcription, &attachmentDeleted); err != nil ||
		attachmentKey != nil || transcription != nil || attachmentDeleted == nil {
		t.Fatalf("anonymized attachment = key=%v transcript=%v deleted=%v error=%v",
			attachmentKey, transcription, attachmentDeleted, err)
	}
	var sessionKey, storedBatchKey *string
	var sessionDeleted, batchDeleted *time.Time
	if err := database.QueryRow(ctx, `
		SELECT session.storage_key, session.retention_deleted_at,
		       batch.storage_key, batch.retention_deleted_at
		FROM sessions AS session
		JOIN session_batches AS batch ON batch.session_id = session.id
		WHERE session.id = $1
	`, sessionID).Scan(&sessionKey, &sessionDeleted, &storedBatchKey, &batchDeleted); err != nil ||
		sessionKey != nil || storedBatchKey != nil || sessionDeleted == nil || batchDeleted == nil {
		t.Fatalf("anonymized replay = session %v/%v batch %v/%v error=%v",
			sessionKey, sessionDeleted, storedBatchKey, batchDeleted, err)
	}
	var progressTokens int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM reporter_access_tokens WHERE tenant_id = $1 AND issue_id = $2
	`, tenantID, issueID).Scan(&progressTokens); err != nil || progressTokens != 0 {
		t.Fatalf("reporter progress tokens after anonymization = %d, error=%v", progressTokens, err)
	}
	anonymizedView, err := store.GetReporterRequestView(ctx, anonymizeHash, now)
	if err != nil || anonymizedView.Status != ReporterCompleted || anonymizedView.DownloadAvailable ||
		anonymizedView.ManualReviewCount == nil || *anonymizedView.ManualReviewCount == 0 {
		t.Fatalf("completed anonymization view = %#v, error=%v", anonymizedView, err)
	}
	var requestEmail *string
	if err := database.QueryRow(ctx, `
		SELECT reporter_email FROM reporter_privacy_requests WHERE id = $1
	`, anonymizeRequest.ID).Scan(&requestEmail); err != nil || requestEmail != nil {
		t.Fatalf("anonymization request email = %v, error=%v", requestEmail, err)
	}
	var otherReporterEmail string
	if err := database.QueryRow(ctx, `
		SELECT reporter_email FROM feedbacks WHERE tenant_id = $1
	`, otherTenantID).Scan(&otherReporterEmail); err != nil || otherReporterEmail != "reporter@example.test" {
		t.Fatalf("other tenant reporter email = %q, error=%v", otherReporterEmail, err)
	}
}

func reporterRequestJob(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	store Store,
	tenantID string,
	requestID string,
) Job {
	t.Helper()
	var jobID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM privacy_jobs WHERE tenant_id = $1 AND reporter_request_id = $2
	`, tenantID, requestID).Scan(&jobID); err != nil {
		t.Fatalf("read reporter privacy job: %v", err)
	}
	job, err := store.GetJob(ctx, database, tenantID, jobID)
	if err != nil {
		t.Fatalf("load reporter privacy job: %v", err)
	}
	return job
}

func reporterJobTask(job Job) *asynq.Task {
	payload, _ := json.Marshal(map[string]any{
		"job_id": job.ID, "tenant_id": job.TenantID, "generation": job.Generation,
	})
	return asynq.NewTask(TaskExport, payload)
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
	removedKeys      []string
}

type privacyEmailCapture struct {
	messages []appemail.Message
}

func (capture *privacyEmailCapture) Enqueue(_ context.Context, message appemail.Message) error {
	capture.messages = append(capture.messages, message)
	return nil
}

func (storage *privacyStorageCapture) RemoveObject(_ context.Context, key string) error {
	if storage.err != nil {
		return storage.err
	}
	storage.removedKeys = append(storage.removedKeys, key)
	return nil
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

func privacyHTTPJSONRequest(
	handler http.Handler,
	token string,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://http-privacy.localhost"+path, strings.NewReader(body))
	request.Host = "http-privacy.localhost"
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
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
			content_type, size_bytes, processing_status, completed_at, transcription,
			safety_status, safety_checked_at, detected_content_type
		) VALUES ($1, $2, 'audio', gen_random_uuid(), $3, 'audio/webm', 42,
		          'completed', now(), 'Checkout audio transcript',
		          'safe', now(), 'audio/webm')
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
