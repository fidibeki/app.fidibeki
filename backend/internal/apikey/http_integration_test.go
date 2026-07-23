package apikey

import (
	"context"
	"encoding/json"
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
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestAPIKeyHTTPFlow(t *testing.T) {
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
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, apiKeyMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}
	var acmeID, betaID, betaKeyID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&acmeID); err != nil {
		t.Fatalf("create Acme tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Beta', 'beta', 'growth', 'active') RETURNING id
	`).Scan(&betaID); err != nil {
		t.Fatalf("create Beta tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions)
		VALUES ($1, 'Beta key', $2, 'fk_live_beta...', '{project:read}') RETURNING id
	`, betaID, strings.Repeat("b", 64)).Scan(&betaKeyID); err != nil {
		t.Fatalf("create Beta API key: %v", err)
	}
	var ownerID, readerID, betaMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2) RETURNING id
	`, acmeID, auth.TenantPermissionStrings()).Scan(&ownerID); err != nil {
		t.Fatalf("create Acme owner: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'reader@acme.test', ARRAY['api_key:read']) RETURNING id
	`, acmeID).Scan(&readerID); err != nil {
		t.Fatalf("create Acme reader: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@beta.test', $2) RETURNING id
	`, betaID, auth.TenantPermissionStrings()).Scan(&betaMemberID); err != nil {
		t.Fatalf("create Beta owner: %v", err)
	}

	pools, err := db.OpenPools(ctx, apiKeyRoleURL(t, directURL, "fidibeki_app", "app-password"), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant(ownerID, acmeID, auth.TenantPermissionStrings())
	readerToken, _, _ := jwtManager.IssueTenant(readerID, acmeID, []string{string(auth.PermissionAPIKeyRead)})
	betaToken, _, _ := jwtManager.IssueTenant(betaMemberID, betaID, auth.TenantPermissionStrings())
	raw := "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUV"
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Store: Store{}, JWT: jwtManager,
		GenerateToken: func() (string, string, string, error) {
			return raw, auth.HashToken(raw), raw[:12] + "...", nil
		},
	})
	routes.With(
		appmiddleware.RequireTenantMember(jwtManager),
		appmiddleware.RequirePermission(auth.PermissionProjectRead),
	).Get("/v1/key-probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	routes.With(
		appmiddleware.RequireTenantMember(jwtManager),
		appmiddleware.RequirePermission(auth.PermissionProjectUpdate),
	).Get("/v1/key-write-probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	createdResponse := performAPIKeyRequest(handler, "acme", http.MethodPost, "/v1/api-keys", `{
		"name":"Deploy reader","permissions":["project:read","feedback:read"],
		"rate_limit_per_min":750,"expires_at":null
	}`, ownerToken)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		APIKey APIKey `json:"api_key"`
		Token  string `json:"token"`
	}
	if err := json.NewDecoder(createdResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Token != raw || created.APIKey.TokenPrefix != raw[:12]+"..." || created.APIKey.RateLimitPerMin != 750 {
		t.Errorf("create response = %#v", created)
	}
	var storedHash string
	if err := root.QueryRow(ctx, `SELECT token_hash FROM api_keys WHERE id = $1`, created.APIKey.ID).Scan(&storedHash); err != nil {
		t.Fatalf("load stored token: %v", err)
	}
	if storedHash != auth.HashToken(raw) || storedHash == raw {
		t.Errorf("stored token hash = %q", storedHash)
	}
	keyAccess := performAPIKeyRequest(handler, "acme", http.MethodGet, "/v1/key-probe", "", raw)
	if keyAccess.Code != http.StatusNoContent {
		t.Errorf("API key access status = %d, body = %s", keyAccess.Code, keyAccess.Body.String())
	}
	keyDenied := performAPIKeyRequest(handler, "acme", http.MethodGet, "/v1/key-write-probe", "", raw)
	if keyDenied.Code != http.StatusForbidden {
		t.Errorf("API key permission status = %d, body = %s", keyDenied.Code, keyDenied.Body.String())
	}
	var lastUsedAt *time.Time
	if err := root.QueryRow(ctx, `SELECT last_used_at FROM api_keys WHERE id = $1`, created.APIKey.ID).Scan(&lastUsedAt); err != nil {
		t.Fatalf("load API key usage: %v", err)
	}
	if lastUsedAt == nil {
		t.Error("API key last_used_at was not recorded")
	}
	if _, err := root.Exec(ctx, `UPDATE api_keys SET last_used_at = NULL WHERE id = $1`, created.APIKey.ID); err != nil {
		t.Fatalf("reset API key usage: %v", err)
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET deletion_requested_at = now() WHERE id = $1`, acmeID); err != nil {
		t.Fatalf("schedule tenant deletion: %v", err)
	}
	graceRead := performAPIKeyRequest(handler, "acme", http.MethodGet, "/v1/key-probe", "", raw)
	if graceRead.Code != http.StatusNoContent {
		t.Errorf("API key grace-period read status = %d, body = %s", graceRead.Code, graceRead.Body.String())
	}
	if err := root.QueryRow(ctx, `SELECT last_used_at FROM api_keys WHERE id = $1`, created.APIKey.ID).Scan(&lastUsedAt); err != nil {
		t.Fatalf("load grace-period API key usage: %v", err)
	}
	if lastUsedAt != nil {
		t.Errorf("API key grace-period read updated last_used_at to %s", lastUsedAt)
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET deletion_requested_at = NULL WHERE id = $1`, acmeID); err != nil {
		t.Fatalf("cancel tenant deletion: %v", err)
	}
	crossTenantKey := performAPIKeyRequest(handler, "beta", http.MethodGet, "/v1/key-probe", "", raw)
	if crossTenantKey.Code != http.StatusUnauthorized {
		t.Errorf("cross-tenant API key status = %d, body = %s", crossTenantKey.Code, crossTenantKey.Body.String())
	}
	if _, err := root.Exec(ctx, `UPDATE api_keys SET expires_at = now() - interval '1 minute' WHERE id = $1`, created.APIKey.ID); err != nil {
		t.Fatalf("expire API key: %v", err)
	}
	expiredKey := performAPIKeyRequest(handler, "acme", http.MethodGet, "/v1/key-probe", "", raw)
	if expiredKey.Code != http.StatusUnauthorized {
		t.Errorf("expired API key status = %d, body = %s", expiredKey.Code, expiredKey.Body.String())
	}

	listed := performAPIKeyRequest(handler, "acme", http.MethodGet, "/v1/api-keys", "", readerToken)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), created.APIKey.ID) || strings.Contains(listed.Body.String(), raw) {
		t.Errorf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}
	readOnlyCreate := performAPIKeyRequest(handler, "acme", http.MethodPost, "/v1/api-keys", `{
		"name":"Denied","permissions":["project:read"]
	}`, readerToken)
	if readOnlyCreate.Code != http.StatusForbidden {
		t.Errorf("read-only create status = %d, body = %s", readOnlyCreate.Code, readOnlyCreate.Body.String())
	}
	escalation := performAPIKeyRequest(handler, "acme", http.MethodPost, "/v1/api-keys", `{
		"name":"Escalation","permissions":["api_key:create"]
	}`, readerToken)
	if escalation.Code != http.StatusForbidden {
		t.Errorf("permission escalation status = %d, body = %s", escalation.Code, escalation.Body.String())
	}
	invalid := performAPIKeyRequest(handler, "acme", http.MethodPost, "/v1/api-keys", `{
		"name":"Invalid","permissions":["not:real"]
	}`, ownerToken)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid permissions status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	crossTenantToken := performAPIKeyRequest(handler, "acme", http.MethodGet, "/v1/api-keys", "", betaToken)
	if crossTenantToken.Code != http.StatusForbidden {
		t.Errorf("cross-tenant token status = %d, body = %s", crossTenantToken.Code, crossTenantToken.Body.String())
	}
	crossTenantResource := performAPIKeyRequest(handler, "acme", http.MethodDelete, "/v1/api-keys/"+betaKeyID, "", ownerToken)
	if crossTenantResource.Code != http.StatusNotFound {
		t.Errorf("cross-tenant delete status = %d, body = %s", crossTenantResource.Code, crossTenantResource.Body.String())
	}
	deleted := performAPIKeyRequest(handler, "acme", http.MethodDelete, "/v1/api-keys/"+created.APIKey.ID, "", ownerToken)
	if deleted.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	deletedAgain := performAPIKeyRequest(handler, "acme", http.MethodDelete, "/v1/api-keys/"+created.APIKey.ID, "", ownerToken)
	if deletedAgain.Code != http.StatusNotFound {
		t.Errorf("repeat delete status = %d, body = %s", deletedAgain.Code, deletedAgain.Body.String())
	}
}

func performAPIKeyRequest(
	handler http.Handler,
	slug, method, path, body, accessToken string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://"+slug+".localhost"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func apiKeyMigrationsPath(t *testing.T) string {
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

func apiKeyRoleURL(t *testing.T, directURL, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}
