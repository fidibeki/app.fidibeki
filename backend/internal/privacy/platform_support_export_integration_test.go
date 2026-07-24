package privacy

import (
	"bytes"
	"context"
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

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestPlatformSupportExportStream exercises the platform-admin/support access
// path to a tenant's completed workspace-export archive (issue #65): a
// mandatory, validated support reason/case, strict tenant/resource
// authorization, actor_type=platform_admin audit identity, fail-closed denial
// auditing, that a not-yet-downloadable (pending/expired) export never
// discloses more than "not found", that a storage-open failure is never
// recorded as a successful access, and that the recorded metadata never
// carries the archive content, object key, a signed URL, or a member
// identity.
func TestPlatformSupportExportStream(t *testing.T) {
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
	migrator, err := db.NewMigrator(directURL, platformSupportExportMigrationsPath(t))
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
	pools, err := db.OpenPools(ctx, platformSupportExportRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)

	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

	var tenantID, otherTenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status) VALUES ('Acme', 'acme-export-support', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status) VALUES ('Globex', 'globex-export-support', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}

	completedKey := "tenants/" + tenantID + "/exports/completed.zip"
	var completedJobID string
	if err := root.QueryRow(ctx, `
		INSERT INTO privacy_jobs (
			tenant_id, kind, status, started_at, heartbeat_at, completed_at, expires_at,
			object_key, object_size_bytes, context_privacy_version, created_at, updated_at
		) VALUES (
			$1, 'workspace_export', 'completed', $2, $2, $2, $2::timestamptz + interval '7 days',
			$3, 2048, 1, $2, $2
		) RETURNING id
	`, tenantID, now, completedKey).Scan(&completedJobID); err != nil {
		t.Fatalf("create completed export job: %v", err)
	}
	var pendingJobID string
	if err := root.QueryRow(ctx, `
		INSERT INTO privacy_jobs (tenant_id, kind, status, created_at, updated_at)
		VALUES ($1, 'workspace_export', 'pending', $2, $2) RETURNING id
	`, otherTenantID, now).Scan(&pendingJobID); err != nil {
		t.Fatalf("create pending export job: %v", err)
	}
	expiredKey := "tenants/" + tenantID + "/exports/expired.zip"
	var expiredJobID string
	if err := root.QueryRow(ctx, `
		INSERT INTO privacy_jobs (
			tenant_id, kind, status, started_at, heartbeat_at, completed_at, expires_at,
			object_key, object_size_bytes, context_privacy_version, created_at, updated_at
		) VALUES (
			$1, 'workspace_export', 'completed',
			$2::timestamptz - interval '10 days', $2::timestamptz - interval '10 days',
			$2::timestamptz - interval '10 days', $2::timestamptz - interval '3 days',
			$3, 2048, 1, $2, $2
		) RETURNING id
	`, tenantID, now, expiredKey).Scan(&expiredJobID); err != nil {
		t.Fatalf("create expired export job: %v", err)
	}

	var supportAdminID, limitedAdminID string
	if err := root.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions) VALUES ('support@fidibeki.test', $1) RETURNING id
	`, []string{string(auth.PermissionPlatformTenantSupportAccess)}).Scan(&supportAdminID); err != nil {
		t.Fatalf("create support platform admin: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions) VALUES ('limited@fidibeki.test', $1) RETURNING id
	`, []string{string(auth.PermissionPlatformTenantRead)}).Scan(&limitedAdminID); err != nil {
		t.Fatalf("create limited platform admin: %v", err)
	}

	jwtManager, err := auth.NewJWTManager(strings.Repeat("f", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create platform JWT manager: %v", err)
	}
	supportToken, _, err := jwtManager.Issue(supportAdminID, auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantSupportAccess),
	})
	if err != nil {
		t.Fatalf("issue support platform token: %v", err)
	}
	limitedToken, _, err := jwtManager.Issue(limitedAdminID, auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantRead),
	})
	if err != nil {
		t.Fatalf("issue limited platform token: %v", err)
	}

	storageStub := &platformSupportExportStorageStub{
		objects: map[string][]byte{completedKey: []byte("zip-archive-bytes"), expiredKey: []byte("zip-archive-bytes")},
	}
	router := chi.NewRouter()
	RegisterPlatformSupportRoutes(router, PlatformSupportExportHTTPOptions{
		Begin: db.BeginFromPool(pools.App), Store: Store{}, JWT: jwtManager,
		Storage: storageStub, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Now: func() time.Time { return now },
	})

	path := supportExportAccessPath(tenantID, completedJobID)

	unauthenticated := performExportSupportRequest(router, path, "", "investigating reported export request", "CASE-1")
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	deniedPermission := performExportSupportRequest(router, path, limitedToken, "investigating reported export request", "CASE-1")
	if deniedPermission.Code != http.StatusForbidden {
		t.Fatalf("denied-permission status = %d", deniedPermission.Code)
	}
	var preAttemptEventCount int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM tenant_access_events WHERE tenant_id = $1`, tenantID).
		Scan(&preAttemptEventCount); err != nil {
		t.Fatalf("count pre-attempt access events: %v", err)
	}
	if preAttemptEventCount != 0 {
		t.Fatalf("pre-attempt access event count = %d, want 0 (platform-admin permission denial must not reach the handler)", preAttemptEventCount)
	}

	missingReason := performExportSupportRequest(router, path, supportToken, "", "CASE-1")
	if missingReason.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-reason status = %d, body = %s", missingReason.Code, missingReason.Body.String())
	}
	missingCase := performExportSupportRequest(router, path, supportToken, "investigating reported export request", "")
	if missingCase.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-case status = %d, body = %s", missingCase.Code, missingCase.Body.String())
	}
	queryCredentials := performExportSupportRequest(
		router, path+"?reason=investigating+reported+export+request&case_reference=CASE-1", supportToken, "", "",
	)
	if queryCredentials.Code != http.StatusUnprocessableEntity {
		t.Fatalf("query-credential status = %d, want support metadata excluded from URLs", queryCredentials.Code)
	}
	var deniedRequestCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy_export.support_accessed' AND outcome = 'denied'
	`, tenantID).Scan(&deniedRequestCount); err != nil {
		t.Fatalf("count denied support access events: %v", err)
	}
	if deniedRequestCount != 1 {
		t.Fatalf("denied support access event count = %d, want 1 (dedup collapses the invalid attempts)", deniedRequestCount)
	}

	unknownJob := performExportSupportRequest(router,
		supportExportAccessPath(tenantID, "00000000-0000-4000-8000-000000000000"),
		supportToken, "investigating reported export request", "CASE-1")
	if unknownJob.Code != http.StatusNotFound {
		t.Fatalf("unknown-job status = %d", unknownJob.Code)
	}
	wrongTenant := performExportSupportRequest(router,
		supportExportAccessPath(otherTenantID, completedJobID),
		supportToken, "investigating reported export request", "CASE-1")
	if wrongTenant.Code != http.StatusNotFound {
		t.Fatalf("wrong-tenant status = %d", wrongTenant.Code)
	}
	notReady := performExportSupportRequest(router,
		supportExportAccessPath(otherTenantID, pendingJobID),
		supportToken, "investigating reported export request", "CASE-1")
	if notReady.Code != http.StatusNotFound {
		t.Fatalf("not-ready status = %d, want 404 (a pending export must never stream)", notReady.Code)
	}
	expired := performExportSupportRequest(router,
		supportExportAccessPath(tenantID, expiredJobID),
		supportToken, "investigating reported export request", "CASE-1")
	if expired.Code != http.StatusNotFound {
		t.Fatalf("expired status = %d, want 404 (an expired export must never stream)", expired.Code)
	}

	delete(storageStub.objects, completedKey)
	storageUnavailable := performExportSupportRequest(router,
		path, supportToken, "investigating reported export request", "CASE-1")
	if storageUnavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("storage-unavailable status = %d", storageUnavailable.Code)
	}
	var prematureSuccessCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy_export.support_accessed'
		  AND resource_id = $2 AND outcome = 'success'
	`, tenantID, completedJobID).Scan(&prematureSuccessCount); err != nil {
		t.Fatalf("count premature support access successes: %v", err)
	}
	if prematureSuccessCount != 0 {
		t.Fatalf("support access success count before storage opens = %d, want 0", prematureSuccessCount)
	}
	storageStub.objects[completedKey] = []byte("zip-archive-bytes")

	success := performExportSupportRequest(router,
		path, supportToken, "investigating reported export request", "CASE-1")
	if success.Code != http.StatusOK || success.Body.String() != "zip-archive-bytes" ||
		success.Header().Get("Content-Type") != "application/zip" ||
		!strings.HasPrefix(success.Header().Get("Content-Disposition"), "attachment;") ||
		success.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("success status = %d headers = %#v body = %q", success.Code, success.Header(), success.Body.String())
	}

	var actorType, actorPlatformAdminID, permissionPath, kind, resourceType, resourceID, outcome, action string
	var actorMemberID, subjectMemberID *string
	var dataText string
	if err := root.QueryRow(ctx, `
		SELECT actor_type, actor_platform_admin_id::text, permission_path, kind, resource_type, resource_id::text,
		       outcome, action, actor_member_id::text, subject_member_id::text, data::text
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy_export.support_accessed' AND outcome = 'success'
	`, tenantID).Scan(
		&actorType, &actorPlatformAdminID, &permissionPath, &kind, &resourceType, &resourceID,
		&outcome, &action, &actorMemberID, &subjectMemberID, &dataText,
	); err != nil {
		t.Fatalf("read support access success event: %v", err)
	}
	if actorType != "platform_admin" || actorPlatformAdminID != supportAdminID ||
		permissionPath != "platform_admin:support_access" || kind != "privacy_export.support_accessed" ||
		resourceType != "privacy_export" || resourceID != completedJobID || outcome != "success" || action != "download" {
		t.Fatalf("support access identity = actor_type=%q actor_platform_admin_id=%q permission_path=%q kind=%q resource_type=%q resource_id=%q outcome=%q action=%q",
			actorType, actorPlatformAdminID, permissionPath, kind, resourceType, resourceID, outcome, action)
	}
	if actorMemberID != nil || subjectMemberID != nil {
		t.Fatalf("support access event member linkage = actor_member_id=%v subject_member_id=%v, want both nil", actorMemberID, subjectMemberID)
	}
	if !strings.Contains(dataText, "support_reason") || !strings.Contains(dataText, "investigating reported export request") ||
		!strings.Contains(dataText, "support_case_reference") || !strings.Contains(dataText, "CASE-1") {
		t.Fatalf("support access event data = %s, want support_reason/support_case_reference present", dataText)
	}
	if strings.Contains(dataText, "zip-archive-bytes") || strings.Contains(dataText, completedKey) ||
		strings.Contains(dataText, "object_key") || strings.Contains(dataText, supportToken) ||
		strings.Contains(dataText, "http") {
		t.Fatalf("support access event data leaked sensitive payload: %s", dataText)
	}

	var otherTenantEventCount int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM tenant_access_events WHERE tenant_id = $1`, otherTenantID).
		Scan(&otherTenantEventCount); err != nil {
		t.Fatalf("count other-tenant access events: %v", err)
	}
	// The cross-tenant not_found attempt and the pending-job not-ready attempt
	// both target otherTenantID and are isolated to it.
	if otherTenantEventCount != 2 {
		t.Fatalf("other-tenant access event count = %d, want 2, isolated to its own tenant", otherTenantEventCount)
	}
}

type platformSupportExportStorageStub struct {
	objects map[string][]byte
}

func (s *platformSupportExportStorageStub) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	value, exists := s.objects[key]
	if !exists {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func supportExportAccessPath(tenantID, jobID string) string {
	return fmt.Sprintf("/admin/tenants/%s/privacy-exports/%s/support-access", tenantID, jobID)
}

func performExportSupportRequest(
	handler http.Handler,
	path string,
	token string,
	reason string,
	caseReference string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://platform.localhost"+path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if reason != "" {
		request.Header.Set(accessaudit.SupportReasonHeader, reason)
	}
	if caseReference != "" {
		request.Header.Set(accessaudit.SupportCaseReferenceHeader, caseReference)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func platformSupportExportMigrationsPath(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve platform support export test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	return path
}

func platformSupportExportRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
