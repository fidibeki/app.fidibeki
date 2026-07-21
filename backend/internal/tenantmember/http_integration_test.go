package tenantmember

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestTenantAuthenticationInvitationAndPermissionsHTTPFlow(t *testing.T) {
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
	migrator, err := db.NewMigrator(directURL, tenantMigrationsPath(t))
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
	var tenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'owner@acme.test', $2)
	`, tenantID, auth.TenantPermissionStrings()); err != nil {
		t.Fatalf("create owner: %v", err)
	}

	pools, err := db.OpenPools(
		ctx,
		tenantRoleURL(t, directURL, "fidibeki_app", "app-password"),
		directURL,
		4,
		2,
	)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	queue := &messageCapture{}
	store := Store{RefreshTTL: 30 * 24 * time.Hour}
	service := Service{
		Store: store, JWT: jwtManager, PublicBaseURL: "http://api.localhost:8088",
		BaseDomain: "localhost", AuthFrom: "Fidibeki <auth@fidibeki.com>",
	}
	probe, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin auth probe: %v", err)
	}
	var configuredTenant string
	if err := probe.QueryRow(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID).Scan(&configuredTenant); err != nil {
		t.Fatalf("set auth probe tenant: %v", err)
	}
	if _, err := service.RequestMagicLink(ctx, probe, tenant.Tenant{ID: tenantID, Slug: "acme", Name: "Acme"}, "owner@acme.test"); err != nil {
		t.Fatalf("direct RequestMagicLink() error = %v", err)
	}
	_ = probe.Rollback(ctx)
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Service: service, Store: store, JWT: jwtManager, Email: queue,
		RefreshTTL: 30 * 24 * time.Hour,
	})
	var middlewareLogs bytes.Buffer
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: slog.New(slog.NewJSONHandler(&middlewareLogs, nil)),
	})(routes)

	login := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"owner@acme.test"}`, "", nil)
	if login.Code != http.StatusAccepted || len(queue.messages) != 1 {
		t.Fatalf("login status = %d, messages = %d, body = %s, logs = %s", login.Code, len(queue.messages), login.Body.String(), middlewareLogs.String())
	}
	ownerMagicToken := messageToken(t, queue.messages[0])
	if !strings.Contains(queue.messages[0].Text, "http://acme.localhost:8088/auth/verify") {
		t.Errorf("tenant magic link = %s", queue.messages[0].Text)
	}

	verified := performTenantRequest(handler, http.MethodPost, "/auth/verify", `{"token":"`+ownerMagicToken+`"}`, "", nil)
	ownerSession := decodeTenantSession(t, verified)
	ownerCookie := responseCookie(t, verified, tenantRefreshCookie)
	if ownerCookie.Path != "/" {
		t.Fatalf("tenant refresh cookie path = %q, want / for server-rendered route refresh", ownerCookie.Path)
	}
	secondVerify := performTenantRequest(handler, http.MethodPost, "/auth/verify", `{"token":"`+ownerMagicToken+`"}`, "", nil)
	if secondVerify.Code != http.StatusUnauthorized {
		t.Errorf("second verify status = %d", secondVerify.Code)
	}

	invite := performTenantRequest(
		handler, http.MethodPost, "/auth/invite",
		`{"email":"viewer@acme.test","permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if invite.Code != http.StatusAccepted || len(queue.messages) != 2 {
		t.Fatalf("invite status = %d, messages = %d, body = %s", invite.Code, len(queue.messages), invite.Body.String())
	}
	viewerMagicToken := messageToken(t, queue.messages[1])
	viewerVerified := performTenantRequest(handler, http.MethodPost, "/auth/verify", `{"token":"`+viewerMagicToken+`"}`, "", nil)
	viewerSession := decodeTenantSession(t, viewerVerified)
	viewerCookie := responseCookie(t, viewerVerified, tenantRefreshCookie)
	if len(viewerSession.Member.Permissions) != 1 || viewerSession.Member.Permissions[0] != "project:read" {
		t.Errorf("viewer permissions = %#v", viewerSession.Member.Permissions)
	}

	members := performTenantRequest(handler, http.MethodGet, "/v1/members", "", ownerSession.AccessToken, nil)
	if members.Code != http.StatusOK || !strings.Contains(members.Body.String(), "viewer@acme.test") {
		t.Errorf("members status = %d, body = %s", members.Code, members.Body.String())
	}
	viewerForbidden := performTenantRequest(handler, http.MethodGet, "/v1/members", "", viewerSession.AccessToken, nil)
	if viewerForbidden.Code != http.StatusForbidden {
		t.Errorf("viewer members status = %d", viewerForbidden.Code)
	}

	updatedViewer := performTenantRequest(
		handler, http.MethodPatch, "/v1/members/"+viewerSession.Member.ID,
		`{"permissions":["project:read","feedback:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if updatedViewer.Code != http.StatusOK || !strings.Contains(updatedViewer.Body.String(), "feedback:read") {
		t.Errorf("update member status = %d, body = %s", updatedViewer.Code, updatedViewer.Body.String())
	}
	duplicateInvite := performTenantRequest(
		handler, http.MethodPost, "/auth/invite",
		`{"email":"viewer@acme.test","permissions":["project:read"]}`,
		ownerSession.AccessToken, nil,
	)
	if duplicateInvite.Code != http.StatusConflict {
		t.Errorf("duplicate invite status = %d, body = %s", duplicateInvite.Code, duplicateInvite.Body.String())
	}
	viewerLogout := performTenantRequest(handler, http.MethodPost, "/auth/logout", "", "", viewerCookie)
	if viewerLogout.Code != http.StatusNoContent {
		t.Errorf("viewer logout status = %d, body = %s", viewerLogout.Code, viewerLogout.Body.String())
	}
	invalidLogin := performTenantRequest(handler, http.MethodPost, "/auth/login", `{"email":"invalid"}`, "", nil)
	if invalidLogin.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid login status = %d", invalidLogin.Code)
	}

	refreshed := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", ownerCookie)
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refreshed.Code, refreshed.Body.String())
	}
	rotatedCookie := responseCookie(t, refreshed, tenantRefreshCookie)
	reused := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", ownerCookie)
	if reused.Code != http.StatusUnauthorized {
		t.Errorf("reused refresh status = %d", reused.Code)
	}
	revokedFamily := performTenantRequest(handler, http.MethodPost, "/auth/refresh", "", "", rotatedCookie)
	if revokedFamily.Code != http.StatusUnauthorized {
		t.Errorf("revoked family refresh status = %d", revokedFamily.Code)
	}
}

type tenantSessionResponse struct {
	AccessToken string `json:"access_token"`
	Member      Member `json:"member"`
}

func decodeTenantSession(t *testing.T, recorder *httptest.ResponseRecorder) tenantSessionResponse {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("session status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response tenantSessionResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return response
}

func performTenantRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	accessToken string,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://acme.localhost"+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}

func responseCookie(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response cookie %q missing", name)
	return nil
}

func messageToken(t *testing.T, message email.Message) string {
	t.Helper()
	for _, field := range strings.Fields(message.Text) {
		parsed, err := url.Parse(field)
		if err != nil || parsed.RawQuery != "" {
			continue
		}
		fragment, err := url.ParseQuery(parsed.Fragment)
		if err == nil && fragment.Get("token") != "" {
			return fragment.Get("token")
		}
	}
	t.Fatal("message token missing")
	return ""
}

type messageCapture struct {
	messages []email.Message
}

func (c *messageCapture) Enqueue(_ context.Context, message email.Message) error {
	c.messages = append(c.messages, message)
	return nil
}

func tenantMigrationsPath(t *testing.T) string {
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

func tenantRoleURL(t *testing.T, directURL, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}
