package accessaudit_test

import (
	"context"
	"encoding/json"
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
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestAccessEventsHTTPPermissionAndTenantIsolation(t *testing.T) {
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
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, accessAuditHTTPMigrationsPath(t))
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

	tenantID := seedAccessAuditHTTPTenant(t, ctx, root, "http-audit")
	otherTenantID := seedAccessAuditHTTPTenant(t, ctx, root, "http-audit-other")

	var adminMemberID, plainMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, 'admin@http-audit.test', ARRAY['audit:read'], 'active') RETURNING id
	`, tenantID).Scan(&adminMemberID); err != nil {
		t.Fatalf("create audit-read member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, 'member@http-audit.test', ARRAY['feedback:read'], 'active') RETURNING id
	`, tenantID).Scan(&plainMemberID); err != nil {
		t.Fatalf("create unprivileged member: %v", err)
	}
	var otherAdminMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, 'admin@other.test', ARRAY['audit:read'], 'active') RETURNING id
	`, otherTenantID).Scan(&otherAdminMemberID); err != nil {
		t.Fatalf("create other-tenant audit-read member: %v", err)
	}

	appURL := accessAuditAppRoleURL(t, directURL)
	pools, err := db.OpenPools(ctx, appURL, directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)

	base := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	seedEvent := func(tid, actorMemberID, resourceType, action, outcome string, resourceID string, offset time.Duration) {
		tx, err := pools.App.Begin(ctx)
		if err != nil {
			t.Fatalf("begin seed tx: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tid); err != nil {
			t.Fatalf("set seed tenant context: %v", err)
		}
		if err := accessaudit.Record(ctx, tx, accessaudit.RecordInput{
			TenantID: tid, Kind: resourceType + ".accessed",
			Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: actorMemberID},
			ResourceType: resourceType, ResourceID: resourceID, Action: action, Outcome: outcome,
			Now: base.Add(offset),
		}); err != nil {
			t.Fatalf("seed access event: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit seed tx: %v", err)
		}
	}
	seedEvent(tenantID, adminMemberID, accessaudit.ResourceFeedbackAttachment, "preview", accessaudit.OutcomeSuccess,
		"11111111-1111-1111-1111-111111111111", 0)
	seedEvent(tenantID, adminMemberID, accessaudit.ResourceReplaySession, "playback", accessaudit.OutcomeDenied,
		"22222222-2222-2222-2222-222222222222", time.Second)
	seedEvent(otherTenantID, otherAdminMemberID, accessaudit.ResourceFeedbackAttachment, "preview", accessaudit.OutcomeSuccess,
		"33333333-3333-3333-3333-333333333333", 0)

	jwtManager, err := auth.NewJWTManager(strings.Repeat("a", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	adminToken, _, err := jwtManager.IssueTenant(adminMemberID, tenantID, []string{"audit:read"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	plainToken, _, err := jwtManager.IssueTenant(plainMemberID, tenantID, []string{"feedback:read"})
	if err != nil {
		t.Fatalf("issue plain token: %v", err)
	}
	otherAdminToken, _, err := jwtManager.IssueTenant(otherAdminMemberID, otherTenantID, []string{"audit:read"})
	if err != nil {
		t.Fatalf("issue other-tenant admin token: %v", err)
	}

	routes := chi.NewRouter()
	accessaudit.RegisterTenantRoutes(routes, accessaudit.HTTPOptions{Store: accessaudit.Store{}, JWT: jwtManager})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	forbidden := performAccessEventsRequest(handler, "http-audit", plainToken, "")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("unprivileged access-events status = %d body=%s", forbidden.Code, forbidden.Body.String())
	}

	allowed := performAccessEventsRequest(handler, "http-audit", adminToken, "")
	if allowed.Code != http.StatusOK {
		t.Fatalf("privileged access-events status = %d body=%s", allowed.Code, allowed.Body.String())
	}
	var listEnvelope struct {
		Events []accessaudit.Event `json:"events"`
	}
	if err := json.NewDecoder(allowed.Body).Decode(&listEnvelope); err != nil {
		t.Fatalf("decode access-events response: %v", err)
	}
	if len(listEnvelope.Events) != 2 {
		t.Fatalf("tenant access-events count = %d, want 2 (cross-tenant row must not leak)", len(listEnvelope.Events))
	}
	for _, event := range listEnvelope.Events {
		if event.ResourceID != nil && *event.ResourceID == "33333333-3333-3333-3333-333333333333" {
			t.Fatalf("cross-tenant event leaked into HTTP listing: %+v", event)
		}
	}

	deniedOnly := performAccessEventsRequest(handler, "http-audit", adminToken, "outcome=denied")
	var deniedEnvelope struct {
		Events []accessaudit.Event `json:"events"`
	}
	if err := json.NewDecoder(deniedOnly.Body).Decode(&deniedEnvelope); err != nil {
		t.Fatalf("decode filtered access-events response: %v", err)
	}
	if len(deniedEnvelope.Events) != 1 || deniedEnvelope.Events[0].Outcome != accessaudit.OutcomeDenied {
		t.Fatalf("outcome-filtered access-events = %+v, want a single denied event", deniedEnvelope.Events)
	}

	csvResponse := performAccessEventsRequest(handler, "http-audit", adminToken, "format=csv")
	if csvResponse.Code != http.StatusOK || !strings.HasPrefix(csvResponse.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv export = %d content-type=%q", csvResponse.Code, csvResponse.Header().Get("Content-Type"))
	}
	if !strings.Contains(csvResponse.Body.String(), "resource_id") ||
		!strings.Contains(csvResponse.Body.String(), "11111111-1111-1111-1111-111111111111") {
		t.Fatalf("csv export body = %q", csvResponse.Body.String())
	}

	crossTenant := performAccessEventsRequest(handler, "http-audit", otherAdminToken, "")
	if crossTenant.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant token access-events status = %d body=%s", crossTenant.Code, crossTenant.Body.String())
	}
}

// TestAccessEventsCSVExportSafeMaximum proves the CSV export's documented
// safe maximum (maxCSVRows = 5000) is the actual bound a caller observes,
// closing the gap where the JSON list path's maxListLimit (500) silently
// capped every export far below the intended 5000.
func TestAccessEventsCSVExportSafeMaximum(t *testing.T) {
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
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, accessAuditHTTPMigrationsPath(t))
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

	tenantID := seedAccessAuditHTTPTenant(t, ctx, root, "csv-max-audit")
	var adminMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, 'admin@csv-max-audit.test', ARRAY['audit:read'], 'active') RETURNING id
	`, tenantID).Scan(&adminMemberID); err != nil {
		t.Fatalf("create audit-read member: %v", err)
	}

	// Bulk-seed rows well above the JSON view's maxListLimit (500) and past
	// the CSV export's maxCSVRows (5000), each with a distinct resource_id
	// and a NULL dedup_bucket so the dedup unique index never collapses them.
	const seededRows = 5010
	if _, err := root.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_type, actor_member_id, resource_type, resource_id, action, outcome, created_at
		)
		SELECT $1, 'feedback_attachment.accessed', 'human', $2, 'feedback_attachment', gen_random_uuid(), 'preview',
		       'success', now() - (series * interval '1 second')
		FROM generate_series(1, $3) AS series
	`, tenantID, adminMemberID, seededRows); err != nil {
		t.Fatalf("bulk seed access events: %v", err)
	}

	appURL := accessAuditAppRoleURL(t, directURL)
	pools, err := db.OpenPools(ctx, appURL, directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)

	jwtManager, err := auth.NewJWTManager(strings.Repeat("a", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	adminToken, _, err := jwtManager.IssueTenant(adminMemberID, tenantID, []string{"audit:read"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	routes := chi.NewRouter()
	accessaudit.RegisterTenantRoutes(routes, accessaudit.HTTPOptions{Store: accessaudit.Store{}, JWT: jwtManager})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	// Request well above maxListLimit (500) to prove the JSON path's cap is
	// unchanged by the CSV path's higher MaxLimit override.
	jsonResponse := performAccessEventsRequest(handler, "csv-max-audit", adminToken, "limit=10000")
	if jsonResponse.Code != http.StatusOK {
		t.Fatalf("json access-events status = %d body=%s", jsonResponse.Code, jsonResponse.Body.String())
	}
	var jsonEnvelope struct {
		Events     []accessaudit.Event `json:"events"`
		Pagination struct {
			HasMore bool `json:"has_more"`
			Limit   int  `json:"limit"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(jsonResponse.Body).Decode(&jsonEnvelope); err != nil {
		t.Fatalf("decode json access-events response: %v", err)
	}
	if len(jsonEnvelope.Events) != 500 || !jsonEnvelope.Pagination.HasMore || jsonEnvelope.Pagination.Limit != 500 {
		t.Fatalf("json access-events = %d rows, limit=%d, has_more=%v; want the unchanged 500-row JSON page cap",
			len(jsonEnvelope.Events), jsonEnvelope.Pagination.Limit, jsonEnvelope.Pagination.HasMore)
	}

	csvResponse := performAccessEventsRequest(handler, "csv-max-audit", adminToken, "format=csv")
	if csvResponse.Code != http.StatusOK {
		t.Fatalf("csv access-events status = %d body=%s", csvResponse.Code, csvResponse.Body.String())
	}
	lines := strings.Split(strings.TrimRight(csvResponse.Body.String(), "\n"), "\n")
	dataRows := len(lines) - 1 // drop the header row
	if dataRows != 5000 {
		t.Fatalf("csv export returned %d data rows, want exactly 5000 (the documented maxCSVRows safe maximum)", dataRows)
	}
}

func performAccessEventsRequest(handler http.Handler, hostSlug, token, query string) *httptest.ResponseRecorder {
	path := "/v1/access-events"
	if query != "" {
		path += "?" + query
	}
	request := httptest.NewRequest(http.MethodGet, "http://"+hostSlug+".localhost"+path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func accessAuditAppRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}

func seedAccessAuditHTTPTenant(t *testing.T, ctx context.Context, root *pgx.Conn, slug string) string {
	t.Helper()
	var tenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ($1, $2, 'growth', 'active') RETURNING id
	`, slug, slug).Scan(&tenantID); err != nil {
		t.Fatalf("create access-audit http tenant: %v", err)
	}
	return tenantID
}

func accessAuditHTTPMigrationsPath(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve access-audit http test path")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations")
}
