package triage

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

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestTagCatalogueHTTPFlow(t *testing.T) {
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
	migrator, err := db.NewMigrator(directURL, triageMigrationsPath(t))
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
		t.Fatalf("enable app role: %v", err)
	}

	var tenantID, otherTenantID, memberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Other', 'other', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2) RETURNING id
	`, tenantID, auth.TenantPermissionStrings()).Scan(&memberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	var otherTagID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color)
		VALUES ($1, 'Private', '#111827') RETURNING id
	`, otherTenantID).Scan(&otherTagID); err != nil {
		t.Fatalf("create other tenant tag: %v", err)
	}

	pools, err := db.OpenPools(ctx, triageRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, err := auth.NewJWTManager(strings.Repeat("t", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	ownerToken, _, err := jwtManager.IssueTenant(memberID, tenantID, auth.TenantPermissionStrings())
	if err != nil {
		t.Fatalf("issue owner token: %v", err)
	}
	viewerToken, _, err := jwtManager.IssueTenant(memberID, tenantID, nil)
	if err != nil {
		t.Fatalf("issue viewer token: %v", err)
	}
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{Store: Store{}, JWT: jwtManager})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	empty := triageRequest(handler, ownerToken, http.MethodGet, "/v1/tags", "")
	if empty.Code != http.StatusOK || empty.Body.String() != "{\"tags\":[]}\n" {
		t.Fatalf("empty tag list = %d body=%s", empty.Code, empty.Body.String())
	}
	created := triageRequest(
		handler, ownerToken, http.MethodPost, "/v1/tags", `{"name":"Billing","color":"#167d68"}`,
	)
	if created.Code != http.StatusCreated || created.Header().Get("Location") == "" {
		t.Fatalf("create tag = %d headers=%#v body=%s", created.Code, created.Header(), created.Body.String())
	}
	var createdEnvelope struct {
		Tag Tag `json:"tag"`
	}
	decodeTriageResponse(t, created, &createdEnvelope)
	if createdEnvelope.Tag.ID == "" || createdEnvelope.Tag.Name != "Billing" ||
		createdEnvelope.Tag.Color != "#167D68" {
		t.Fatalf("created tag = %#v", createdEnvelope.Tag)
	}

	duplicate := triageRequest(
		handler, ownerToken, http.MethodPost, "/v1/tags", `{"name":"billing","color":"#3B82F6"}`,
	)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate tag = %d body=%s", duplicate.Code, duplicate.Body.String())
	}
	invalid := triageRequest(
		handler, ownerToken, http.MethodPost, "/v1/tags", `{"name":" ","color":"teal"}`,
	)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid tag = %d body=%s", invalid.Code, invalid.Body.String())
	}
	denied := triageRequest(
		handler, viewerToken, http.MethodPost, "/v1/tags", `{"name":"Denied","color":"#167D68"}`,
	)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer tag creation = %d body=%s", denied.Code, denied.Body.String())
	}

	updated := triageRequest(
		handler, ownerToken, http.MethodPatch, "/v1/tags/"+createdEnvelope.Tag.ID,
		`{"name":"Payments","color":"#DC6B2F"}`,
	)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"name":"Payments"`) ||
		!strings.Contains(updated.Body.String(), `"color":"#DC6B2F"`) {
		t.Fatalf("update tag = %d body=%s", updated.Code, updated.Body.String())
	}
	crossTenant := triageRequest(
		handler, ownerToken, http.MethodPatch, "/v1/tags/"+otherTagID, `{"name":"Leaked"}`,
	)
	if crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant tag update = %d body=%s", crossTenant.Code, crossTenant.Body.String())
	}
	listed := triageRequest(handler, viewerToken, http.MethodGet, "/v1/tags", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"name":"Payments"`) ||
		strings.Contains(listed.Body.String(), "Private") {
		t.Fatalf("tag list = %d body=%s", listed.Code, listed.Body.String())
	}
	deleted := triageRequest(
		handler, ownerToken, http.MethodDelete, "/v1/tags/"+createdEnvelope.Tag.ID, "",
	)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete tag = %d body=%s", deleted.Code, deleted.Body.String())
	}
	afterDelete := triageRequest(handler, ownerToken, http.MethodGet, "/v1/tags", "")
	if afterDelete.Code != http.StatusOK || afterDelete.Body.String() != "{\"tags\":[]}\n" {
		t.Fatalf("tag list after delete = %d body=%s", afterDelete.Code, afterDelete.Body.String())
	}
}

func triageRequest(handler http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://acme.localhost"+path, strings.NewReader(body))
	request.Host = "acme.localhost"
	request.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeTriageResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
}

func triageMigrationsPath(t *testing.T) string {
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

func triageRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
