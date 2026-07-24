package feedback

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

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestPlatformSupportAttachmentStream exercises the platform-admin/support
// access path to a tenant's feedback attachment (issue #65): a mandatory,
// validated support reason/case, strict tenant/resource authorization,
// actor_type=platform_admin audit identity, fail-closed denial auditing, and
// that the recorded metadata never carries the attachment content, a storage
// key, or a member identity that would let it leak into a member's privacy
// export via the existing sanitized access-events allowlist.
func TestPlatformSupportAttachmentStream(t *testing.T) {
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
	pools, err := db.OpenPools(ctx, feedbackRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)

	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

	var tenantID, otherTenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status) VALUES ('Acme', 'acme-support', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status) VALUES ('Globex', 'globex-support', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	var projectID string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	var ruleID string
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
	var feedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (tenant_id, project_id, widget_rule_id, public_id, client_feedback_id, page_url, created_at)
		VALUES ($1, $2, $3, 'fb_a1b2c3d4', gen_random_uuid(), 'https://example.com/checkout', $4)
		RETURNING id
	`, tenantID, projectID, ruleID, now).Scan(&feedbackID); err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	var attachmentID string
	if err := root.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, completed_at, safety_status,
			safety_checked_at, detected_content_type, upload_refreshed_at, upload_expires_at
		) VALUES (
			$1, $2, 'screenshot', gen_random_uuid(), $3,
			'image/png', 2048, $4, 'safe',
			$4, 'image/png', $4::timestamptz - interval '1 hour', $4::timestamptz - interval '50 minutes'
		) RETURNING id
	`, tenantID, feedbackID, "tenants/"+tenantID+"/feedbacks/"+feedbackID+"/attachments/evidence.png", now).
		Scan(&attachmentID); err != nil {
		t.Fatalf("create attachment: %v", err)
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

	storageStub := &attachmentStreamStorageStub{data: []byte("investigation-evidence")}
	router := chi.NewRouter()
	RegisterPlatformSupportRoutes(router, PlatformSupportAttachmentHTTPOptions{
		Begin: db.BeginFromPool(pools.App), Store: Store{}, JWT: jwtManager,
		Storage: storageStub, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})

	path := supportAccessPath(tenantID, feedbackID, attachmentID)

	unauthenticated := performPlatformSupportRequest(
		router, path, "", "investigating reported checkout bug", "CASE-1",
	)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	deniedPermission := performPlatformSupportRequest(
		router, path, limitedToken, "investigating reported checkout bug", "CASE-1",
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

	missingReason := performPlatformSupportRequest(router, path, supportToken, "", "CASE-1")
	if missingReason.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-reason status = %d, body = %s", missingReason.Code, missingReason.Body.String())
	}
	missingCase := performPlatformSupportRequest(
		router, path, supportToken, "investigating reported checkout bug", "",
	)
	if missingCase.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-case status = %d, body = %s", missingCase.Code, missingCase.Body.String())
	}
	queryCredentials := performPlatformSupportRequest(
		router,
		path+"?reason=investigating+reported+checkout+bug&case_reference=CASE-1",
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
		WHERE tenant_id = $1 AND kind = 'feedback_attachment.support_accessed' AND outcome = 'denied'
	`, tenantID).Scan(&deniedRequestCount); err != nil {
		t.Fatalf("count denied support access events: %v", err)
	}
	// Both invalid attempts target the same resource/actor/action/outcome
	// within the dedup window, so they collapse into a single ledger row —
	// the same anti-flood behavior the tenant-facing stream relies on.
	if deniedRequestCount != 1 {
		t.Fatalf("denied support access event count = %d, want 1", deniedRequestCount)
	}

	unknownAttachment := performPlatformSupportRequest(router,
		supportAccessPath(tenantID, feedbackID, "00000000-0000-4000-8000-000000000000"),
		supportToken, "investigating reported checkout bug", "CASE-1")
	if unknownAttachment.Code != http.StatusNotFound {
		t.Fatalf("unknown-attachment status = %d", unknownAttachment.Code)
	}
	wrongTenant := performPlatformSupportRequest(router,
		supportAccessPath(otherTenantID, feedbackID, attachmentID),
		supportToken, "investigating reported checkout bug", "CASE-1")
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

	success := performPlatformSupportRequest(router,
		path, supportToken, "investigating reported checkout bug", "CASE-1")
	if success.Code != http.StatusOK || success.Body.String() != "investigation-evidence" ||
		success.Header().Get("Content-Type") != "image/png" ||
		!strings.HasPrefix(success.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("success status = %d headers = %#v body = %q", success.Code, success.Header(), success.Body.String())
	}
	download := performPlatformSupportRequest(router,
		path+"?mode=download", supportToken, "investigating reported checkout bug", "CASE-1")
	if download.Code != http.StatusOK || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("download status = %d disposition = %q", download.Code, download.Header().Get("Content-Disposition"))
	}

	var actorType, actorPlatformAdminID, permissionPath, kind, resourceType, resourceID, outcome string
	var actorMemberID, subjectMemberID *string
	var dataText string
	if err := root.QueryRow(ctx, `
		SELECT actor_type, actor_platform_admin_id::text, permission_path, kind, resource_type, resource_id::text,
		       outcome, actor_member_id::text, subject_member_id::text, data::text
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'feedback_attachment.support_accessed' AND outcome = 'success' AND action = 'preview'
	`, tenantID).Scan(
		&actorType, &actorPlatformAdminID, &permissionPath, &kind, &resourceType, &resourceID,
		&outcome, &actorMemberID, &subjectMemberID, &dataText,
	); err != nil {
		t.Fatalf("read support access success event: %v", err)
	}
	if actorType != "platform_admin" || actorPlatformAdminID != supportAdminID ||
		permissionPath != "platform_admin:support_access" || kind != "feedback_attachment.support_accessed" ||
		resourceType != "feedback_attachment" || resourceID != attachmentID || outcome != "success" {
		t.Fatalf("support access identity = actor_type=%q actor_platform_admin_id=%q permission_path=%q kind=%q resource_type=%q resource_id=%q outcome=%q",
			actorType, actorPlatformAdminID, permissionPath, kind, resourceType, resourceID, outcome)
	}
	// A platform-admin/support event never carries a workspace-member
	// identity, so it structurally cannot match the member-export
	// access-events allowlist predicate (actor_member_id = $2 OR
	// subject_member_id = $2 in privacy/member_export.go) for any member —
	// it is excluded by construction rather than by an ad hoc filter.
	if actorMemberID != nil || subjectMemberID != nil {
		t.Fatalf("support access event member linkage = actor_member_id=%v subject_member_id=%v, want both nil", actorMemberID, subjectMemberID)
	}
	if !strings.Contains(dataText, "support_reason") || !strings.Contains(dataText, "investigating reported checkout bug") ||
		!strings.Contains(dataText, "support_case_reference") || !strings.Contains(dataText, "CASE-1") {
		t.Fatalf("support access event data = %s, want support_reason/support_case_reference present", dataText)
	}
	if strings.Contains(dataText, "investigation-evidence") || strings.Contains(dataText, "evidence.png") ||
		strings.Contains(dataText, "storage_key") || strings.Contains(dataText, supportToken) {
		t.Fatalf("support access event data leaked sensitive payload: %s", dataText)
	}
}

func supportAccessPath(tenantID, feedbackID, attachmentID string) string {
	return fmt.Sprintf("/admin/tenants/%s/feedbacks/%s/attachments/%s/support-access", tenantID, feedbackID, attachmentID)
}

func performPlatformSupportRequest(
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
