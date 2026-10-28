package replay

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/accessaudit"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestPlatformSupportReplayStream exercises the platform-admin/support access
// path to a tenant's merged replay session (issue #65): a mandatory,
// validated support reason/case supplied only through the
// X-Fidibeki-Support-Reason/X-Fidibeki-Support-Case headers (never the URL),
// strict tenant/resource authorization reusing the same RLS and ready-session
// contract as the tenant-facing stream, actor_type=platform_admin audit
// identity, fail-closed denial auditing, dedup of repeated invalid attempts,
// and that the recorded metadata never carries replay content or a storage
// key.
func TestPlatformSupportReplayStream(t *testing.T) {
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
	pools, err := db.OpenPools(ctx, replayRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)

	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

	var tenantID, otherTenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status) VALUES ('Acme', 'acme-replay-support', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status) VALUES ('Globex', 'globex-replay-support', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	var projectID string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_REPLAYSUPPORTABCDEFGHI', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	mergedKey := "tenants/" + tenantID + "/replay/support-session.json"
	var sessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, ended_at, events_count, batches_count,
			storage_key, rrweb_version, merged_at, merge_requested_at, max_duration_seconds, max_events
		) VALUES (
			$1, $2, gen_random_uuid(), $3, $3, 5, 1, $4, $5, $3, $3, 1800, 50000
		) RETURNING id
	`, tenantID, projectID, now, mergedKey, RRWebVersion).Scan(&sessionID); err != nil {
		t.Fatalf("create ready replay session: %v", err)
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

	jwtManager, err := auth.NewJWTManager(strings.Repeat("r", 32), "fidibeki", 10*time.Minute)
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

	storageStub := &replayStorageStub{
		objects:      map[string][]byte{mergedKey: []byte(`[{"type":2,"data":{}}]`)},
		contentTypes: map[string]string{},
		statErrors:   map[string]error{},
	}
	router := chi.NewRouter()
	RegisterPlatformSupportRoutes(router, PlatformSupportStreamHTTPOptions{
		Begin: db.BeginFromPool(pools.App), Store: Store{}, JWT: jwtManager,
		Storage: storageStub, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})

	path := replaySupportAccessPath(tenantID, sessionID)

	unauthenticated := performReplaySupportRequest(
		router, path, "", "investigating reported checkout replay", "CASE-1",
	)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	deniedPermission := performReplaySupportRequest(
		router, path, limitedToken, "investigating reported checkout replay", "CASE-1",
	)
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

	missingReason := performReplaySupportRequest(router, path, supportToken, "", "CASE-1")
	if missingReason.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-reason status = %d, body = %s", missingReason.Code, missingReason.Body.String())
	}
	missingCase := performReplaySupportRequest(
		router, path, supportToken, "investigating reported checkout replay", "",
	)
	if missingCase.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-case status = %d, body = %s", missingCase.Code, missingCase.Body.String())
	}
	queryCredentials := performReplaySupportRequest(
		router,
		path+"?reason=investigating+reported+checkout+replay&case_reference=CASE-1",
		supportToken, "", "",
	)
	if queryCredentials.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"query-credential status = %d, want support metadata excluded from URLs",
			queryCredentials.Code,
		)
	}
	var deniedRequestCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'replay_session.support_accessed' AND outcome = 'denied'
	`, tenantID).Scan(&deniedRequestCount); err != nil {
		t.Fatalf("count denied support access events: %v", err)
	}
	// All three invalid attempts target the same resource/actor/action/outcome
	// within the dedup window, so they collapse into a single ledger row — the
	// same anti-flood behavior the tenant-facing stream and the platform
	// attachment support path rely on.
	if deniedRequestCount != 1 {
		t.Fatalf("denied support access event count = %d, want 1", deniedRequestCount)
	}

	unknownSession := performReplaySupportRequest(router,
		replaySupportAccessPath(tenantID, "00000000-0000-4000-8000-000000000000"),
		supportToken, "investigating reported checkout replay", "CASE-1")
	if unknownSession.Code != http.StatusNotFound {
		t.Fatalf("unknown-session status = %d", unknownSession.Code)
	}
	wrongTenant := performReplaySupportRequest(router,
		replaySupportAccessPath(otherTenantID, sessionID),
		supportToken, "investigating reported checkout replay", "CASE-1")
	if wrongTenant.Code != http.StatusNotFound {
		t.Fatalf("wrong-tenant status = %d", wrongTenant.Code)
	}
	var otherTenantEventCount int
	if err := root.QueryRow(ctx, `SELECT count(*) FROM tenant_access_events WHERE tenant_id = $1`, otherTenantID).
		Scan(&otherTenantEventCount); err != nil {
		t.Fatalf("count other-tenant access events: %v", err)
	}
	if otherTenantEventCount != 1 {
		t.Fatalf("other-tenant access event count = %d, want 1 (the cross-tenant not_found attempt, isolated to its own tenant)", otherTenantEventCount)
	}

	var notReadySessionID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, events_count, batches_count, rrweb_version,
			max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), $3, 0, 0, $4, 1800, 50000)
		RETURNING id
	`, tenantID, projectID, now, RRWebVersion).Scan(&notReadySessionID); err != nil {
		t.Fatalf("create not-ready replay session: %v", err)
	}
	notReady := performReplaySupportRequest(router,
		replaySupportAccessPath(tenantID, notReadySessionID),
		supportToken, "investigating reported checkout replay", "CASE-1")
	if notReady.Code != http.StatusNotFound {
		t.Fatalf("not-ready session status = %d, want 404 (unmerged replay must never stream)", notReady.Code)
	}

	delete(storageStub.objects, mergedKey)
	storageUnavailable := performReplaySupportRequest(router,
		path, supportToken, "investigating reported checkout replay", "CASE-1")
	if storageUnavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("storage-unavailable status = %d, want 503", storageUnavailable.Code)
	}
	var prematureSuccessCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'replay_session.support_accessed'
		  AND resource_id = $2 AND outcome = 'success'
	`, tenantID, sessionID).Scan(&prematureSuccessCount); err != nil {
		t.Fatalf("count premature support access successes: %v", err)
	}
	if prematureSuccessCount != 0 {
		t.Fatalf("support access success count before storage opens = %d, want 0", prematureSuccessCount)
	}
	storageStub.objects[mergedKey] = []byte(`[{"type":2,"data":{}}]`)

	success := performReplaySupportRequest(router,
		path, supportToken, "investigating reported checkout replay", "CASE-1")
	if success.Code != http.StatusOK || success.Body.String() != `[{"type":2,"data":{}}]` ||
		success.Header().Get("Content-Type") != "application/json" ||
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
		WHERE tenant_id = $1 AND kind = 'replay_session.support_accessed' AND outcome = 'success'
	`, tenantID).Scan(
		&actorType, &actorPlatformAdminID, &permissionPath, &kind, &resourceType, &resourceID,
		&outcome, &action, &actorMemberID, &subjectMemberID, &dataText,
	); err != nil {
		t.Fatalf("read support access success event: %v", err)
	}
	if actorType != "platform_admin" || actorPlatformAdminID != supportAdminID ||
		permissionPath != "platform_admin:support_access" || kind != "replay_session.support_accessed" ||
		resourceType != "replay_session" || resourceID != sessionID || outcome != "success" || action != "playback" {
		t.Fatalf("support access identity = actor_type=%q actor_platform_admin_id=%q permission_path=%q kind=%q resource_type=%q resource_id=%q outcome=%q action=%q",
			actorType, actorPlatformAdminID, permissionPath, kind, resourceType, resourceID, outcome, action)
	}
	// A platform-admin/support event never carries a workspace-member
	// identity, so it structurally cannot match the member-export
	// access-events allowlist predicate for any member — it is excluded by
	// construction rather than by an ad hoc filter.
	if actorMemberID != nil || subjectMemberID != nil {
		t.Fatalf("support access event member linkage = actor_member_id=%v subject_member_id=%v, want both nil", actorMemberID, subjectMemberID)
	}
	if !strings.Contains(dataText, "support_reason") || !strings.Contains(dataText, "investigating reported checkout replay") ||
		!strings.Contains(dataText, "support_case_reference") || !strings.Contains(dataText, "CASE-1") {
		t.Fatalf("support access event data = %s, want support_reason/support_case_reference present", dataText)
	}
	if strings.Contains(dataText, "type\":2") || strings.Contains(dataText, mergedKey) ||
		strings.Contains(dataText, "storage_key") || strings.Contains(dataText, supportToken) {
		t.Fatalf("support access event data leaked sensitive payload: %s", dataText)
	}
}

func replaySupportAccessPath(tenantID, sessionID string) string {
	return fmt.Sprintf("/admin/tenants/%s/sessions/%s/support-access", tenantID, sessionID)
}

func performReplaySupportRequest(
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
