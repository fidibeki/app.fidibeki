package project

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

func TestProjectAndWidgetRuleHTTPFlow(t *testing.T) {
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
	migrator, err := db.NewMigrator(directURL, projectMigrationsPath(t))
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
	var acmeID, betaID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'starter', 'active') RETURNING id
	`).Scan(&acmeID); err != nil {
		t.Fatalf("create Acme tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Beta', 'beta', 'growth', 'active') RETURNING id
	`).Scan(&betaID); err != nil {
		t.Fatalf("create Beta tenant: %v", err)
	}
	var betaProjectID string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Beta Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{beta.test}') RETURNING id
	`, betaID).Scan(&betaProjectID); err != nil {
		t.Fatalf("create Beta project: %v", err)
	}

	pools, err := db.OpenPools(
		ctx, projectRoleURL(t, directURL, "fidibeki_app", "app-password"), directURL, 4, 2,
	)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant("owner-id", acmeID, auth.TenantPermissionStrings())
	viewerToken, _, _ := jwtManager.IssueTenant("viewer-id", acmeID, []string{string(auth.PermissionProjectRead)})
	betaToken, _, _ := jwtManager.IssueTenant("beta-id", betaID, auth.TenantPermissionStrings())

	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{Store: Store{}, JWT: jwtManager})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)

	createdResponse := performProjectRequest(handler, "acme", http.MethodPost, "/v1/projects", `{
		"name":"Acme Web","allowed_domains":["Example.com","*.preview.example.com"]
	}`, ownerToken)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create project status = %d, body = %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		Project Project `json:"project"`
	}
	decodeProjectResponse(t, createdResponse, &created)
	if !strings.HasPrefix(created.Project.PublicKey, "pk_live_") || len(created.Project.PublicKey) != 30 {
		t.Errorf("public key = %q", created.Project.PublicKey)
	}
	if len(created.Project.AllowedDomains) != 2 || created.Project.AllowedDomains[0] != "example.com" {
		t.Errorf("allowed domains = %#v", created.Project.AllowedDomains)
	}
	if created.Project.WidgetLastSeenAt != nil {
		t.Errorf("new project widget activity = %s, want nil", created.Project.WidgetLastSeenAt)
	}
	firstSeen := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if err := (Store{}).MarkWidgetSeen(ctx, pools.Admin, created.Project.ID, firstSeen); err != nil {
		t.Fatalf("mark widget seen: %v", err)
	}
	if err := (Store{}).MarkWidgetSeen(ctx, pools.Admin, created.Project.ID, firstSeen.Add(time.Minute)); err != nil {
		t.Fatalf("throttle widget activity: %v", err)
	}
	seenProject, err := (Store{}).GetProject(ctx, pools.Admin, acmeID, created.Project.ID)
	if err != nil {
		t.Fatalf("get project activity: %v", err)
	}
	if seenProject.WidgetLastSeenAt == nil || !seenProject.WidgetLastSeenAt.Equal(firstSeen) {
		t.Fatalf("throttled widget activity = %v, want %s", seenProject.WidgetLastSeenAt, firstSeen)
	}
	refreshedSeen := firstSeen.Add(6 * time.Minute)
	if err := (Store{}).MarkWidgetSeen(ctx, pools.Admin, created.Project.ID, refreshedSeen); err != nil {
		t.Fatalf("refresh widget activity: %v", err)
	}
	seenProject, err = (Store{}).GetProject(ctx, pools.Admin, acmeID, created.Project.ID)
	if err != nil || seenProject.WidgetLastSeenAt == nil || !seenProject.WidgetLastSeenAt.Equal(refreshedSeen) {
		t.Fatalf("refreshed widget activity = %v, error = %v", seenProject.WidgetLastSeenAt, err)
	}

	limitResponse := performProjectRequest(
		handler, "acme", http.MethodPost, "/v1/projects",
		`{"name":"Second","allowed_domains":["second.test"]}`, ownerToken,
	)
	if limitResponse.Code != http.StatusPaymentRequired || !strings.Contains(limitResponse.Body.String(), "plan_limit_exceeded") {
		t.Errorf("project limit status = %d, body = %s", limitResponse.Code, limitResponse.Body.String())
	}

	listed := performProjectRequest(handler, "acme", http.MethodGet, "/v1/projects", "", viewerToken)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), created.Project.ID) ||
		!strings.Contains(listed.Body.String(), `"widget_last_seen_at":"2026-07-21T12:06:00Z"`) {
		t.Errorf("list projects status = %d, body = %s", listed.Code, listed.Body.String())
	}
	viewerCreate := performProjectRequest(
		handler, "acme", http.MethodPost, "/v1/projects", `{"name":"Denied"}`, viewerToken,
	)
	if viewerCreate.Code != http.StatusForbidden {
		t.Errorf("viewer create status = %d, body = %s", viewerCreate.Code, viewerCreate.Body.String())
	}
	wrongTenantToken := performProjectRequest(handler, "acme", http.MethodGet, "/v1/projects", "", betaToken)
	if wrongTenantToken.Code != http.StatusForbidden {
		t.Errorf("cross-tenant token status = %d, body = %s", wrongTenantToken.Code, wrongTenantToken.Body.String())
	}
	crossTenantResource := performProjectRequest(
		handler, "acme", http.MethodGet, "/v1/projects/"+betaProjectID, "", ownerToken,
	)
	if crossTenantResource.Code != http.StatusNotFound {
		t.Errorf("cross-tenant project status = %d, body = %s", crossTenantResource.Code, crossTenantResource.Body.String())
	}
	invalidID := performProjectRequest(handler, "acme", http.MethodGet, "/v1/projects/not-a-uuid", "", ownerToken)
	if invalidID.Code != http.StatusNotFound {
		t.Errorf("invalid project ID status = %d", invalidID.Code)
	}
	invalidProjectUpdate := performProjectRequest(
		handler, "acme", http.MethodPatch, "/v1/projects/"+created.Project.ID,
		`{"name":"","allowed_domains":["https://example.com"]}`, ownerToken,
	)
	if invalidProjectUpdate.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid project update status = %d, body = %s", invalidProjectUpdate.Code, invalidProjectUpdate.Body.String())
	}
	crossTenantRules := performProjectRequest(
		handler, "acme", http.MethodGet, "/v1/projects/"+betaProjectID+"/widget-rules", "", ownerToken,
	)
	if crossTenantRules.Code != http.StatusNotFound {
		t.Errorf("cross-tenant rules status = %d, body = %s", crossTenantRules.Code, crossTenantRules.Body.String())
	}
	malformedRule := performProjectRequest(
		handler, "acme", http.MethodPost, "/v1/projects/"+created.Project.ID+"/widget-rules", `{`, ownerToken,
	)
	if malformedRule.Code != http.StatusBadRequest {
		t.Errorf("malformed rule status = %d, body = %s", malformedRule.Code, malformedRule.Body.String())
	}

	planMismatch := performProjectRequest(
		handler, "acme", http.MethodPost, "/v1/projects/"+created.Project.ID+"/widget-rules",
		`{"name":"Audio","matcher_type":"all_pages","trigger_type":"delay",`+
			`"trigger_delay_seconds":0,"can_record_audio":true}`, ownerToken,
	)
	if planMismatch.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(planMismatch.Body.String(), `"can_record_audio"`) {
		t.Errorf("plan validation status = %d, body = %s", planMismatch.Code, planMismatch.Body.String())
	}
	badRegex := performProjectRequest(
		handler, "acme", http.MethodPost, "/v1/projects/"+created.Project.ID+"/widget-rules",
		`{"name":"Regex","matcher_type":"url_regex","matcher_value":"[",`+
			`"trigger_type":"exit_intent","accent_color":"blue"}`, ownerToken,
	)
	if badRegex.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(badRegex.Body.String(), `"matcher_value"`) ||
		!strings.Contains(badRegex.Body.String(), `"accent_color"`) {
		t.Errorf("field validation status = %d, body = %s", badRegex.Code, badRegex.Body.String())
	}

	ruleResponse := performProjectRequest(
		handler, "acme", http.MethodPost, "/v1/projects/"+created.Project.ID+"/widget-rules",
		`{"name":"Checkout","priority":10,"matcher_type":"url_prefix",`+
			`"matcher_value":"https://example.com/checkout","trigger_type":"scroll",`+
			`"trigger_scroll_percent":60,"theme":"auto","accent_color":"#12AbEf"}`, ownerToken,
	)
	if ruleResponse.Code != http.StatusCreated {
		t.Fatalf("create widget rule status = %d, body = %s", ruleResponse.Code, ruleResponse.Body.String())
	}
	var createdRule struct {
		WidgetRule WidgetRule `json:"widget_rule"`
	}
	decodeProjectResponse(t, ruleResponse, &createdRule)
	if !createdRule.WidgetRule.Enabled || !createdRule.WidgetRule.CanRate || !createdRule.WidgetRule.CanWriteText {
		t.Errorf("widget defaults = %#v", createdRule.WidgetRule)
	}
	publicConfiguration, err := (Store{}).LoadPublicConfiguration(ctx, pools.Admin, created.Project.PublicKey)
	if err != nil {
		t.Fatalf("load public configuration: %v", err)
	}
	if publicConfiguration.ProjectID != created.Project.ID || len(publicConfiguration.Rules) != 1 ||
		publicConfiguration.Rules[0].ID != createdRule.WidgetRule.ID {
		t.Errorf("public configuration = %#v", publicConfiguration)
	}
	gotRule := performProjectRequest(
		handler, "acme", http.MethodGet,
		"/v1/projects/"+created.Project.ID+"/widget-rules/"+createdRule.WidgetRule.ID, "", ownerToken,
	)
	if gotRule.Code != http.StatusOK || !strings.Contains(gotRule.Body.String(), `"name":"Checkout"`) {
		t.Errorf("get widget rule status = %d, body = %s", gotRule.Code, gotRule.Body.String())
	}
	viewerRules := performProjectRequest(
		handler, "acme", http.MethodGet, "/v1/projects/"+created.Project.ID+"/widget-rules", "", viewerToken,
	)
	if viewerRules.Code != http.StatusForbidden {
		t.Errorf("viewer widget rules status = %d", viewerRules.Code)
	}

	if _, err := root.Exec(ctx, `UPDATE tenants SET plan = 'growth' WHERE id = $1`, acmeID); err != nil {
		t.Fatalf("upgrade Acme plan: %v", err)
	}
	updatedRule := performProjectRequest(
		handler, "acme", http.MethodPatch,
		"/v1/projects/"+created.Project.ID+"/widget-rules/"+createdRule.WidgetRule.ID,
		`{"name":"Checkout audio","matcher_type":"all_pages","trigger_type":"delay",`+
			`"trigger_delay_seconds":3,"can_record_audio":true,"can_attach_screenshot":true,`+
			`"replay_mode":"sampling","replay_sampling_rate":20}`, ownerToken,
	)
	if updatedRule.Code != http.StatusOK || !strings.Contains(updatedRule.Body.String(), `"can_record_audio":true`) {
		t.Errorf("update widget rule status = %d, body = %s", updatedRule.Code, updatedRule.Body.String())
	}
	listedRules := performProjectRequest(
		handler, "acme", http.MethodGet, "/v1/projects/"+created.Project.ID+"/widget-rules", "", ownerToken,
	)
	if listedRules.Code != http.StatusOK || !strings.Contains(listedRules.Body.String(), createdRule.WidgetRule.ID) {
		t.Errorf("list widget rules status = %d, body = %s", listedRules.Code, listedRules.Body.String())
	}

	updatedProject := performProjectRequest(
		handler, "acme", http.MethodPatch, "/v1/projects/"+created.Project.ID,
		`{"name":"Acme App","allowed_domains":["app.example.com"]}`, ownerToken,
	)
	if updatedProject.Code != http.StatusOK || !strings.Contains(updatedProject.Body.String(), "app.example.com") {
		t.Errorf("update project status = %d, body = %s", updatedProject.Code, updatedProject.Body.String())
	}
	deleteRule := performProjectRequest(
		handler, "acme", http.MethodDelete,
		"/v1/projects/"+created.Project.ID+"/widget-rules/"+createdRule.WidgetRule.ID, "", ownerToken,
	)
	if deleteRule.Code != http.StatusNoContent {
		t.Errorf("delete rule status = %d, body = %s", deleteRule.Code, deleteRule.Body.String())
	}
	deletedRule := performProjectRequest(
		handler, "acme", http.MethodGet,
		"/v1/projects/"+created.Project.ID+"/widget-rules/"+createdRule.WidgetRule.ID, "", ownerToken,
	)
	if deletedRule.Code != http.StatusNotFound {
		t.Errorf("deleted rule status = %d, body = %s", deletedRule.Code, deletedRule.Body.String())
	}
	deleteProject := performProjectRequest(
		handler, "acme", http.MethodDelete, "/v1/projects/"+created.Project.ID, "", ownerToken,
	)
	if deleteProject.Code != http.StatusNoContent {
		t.Errorf("delete project status = %d, body = %s", deleteProject.Code, deleteProject.Body.String())
	}

	assertProjectRLS(t, ctx, pools, acmeID, betaProjectID)
}

func assertProjectRLS(t *testing.T, ctx context.Context, pools *db.Pools, tenantID, hiddenProjectID string) {
	t.Helper()
	tx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin RLS probe: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var configured string
	if err := tx.QueryRow(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID).Scan(&configured); err != nil {
		t.Fatalf("set RLS tenant: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM projects WHERE id = $1`, hiddenProjectID).Scan(&count); err != nil {
		t.Fatalf("query RLS-hidden project: %v", err)
	}
	if count != 0 {
		t.Errorf("RLS-hidden project count = %d", count)
	}
}

func performProjectRequest(
	handler http.Handler,
	slug string,
	method string,
	path string,
	body string,
	accessToken string,
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

func decodeProjectResponse(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func projectMigrationsPath(t *testing.T) string {
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

func projectRoleURL(t *testing.T, directURL, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}
