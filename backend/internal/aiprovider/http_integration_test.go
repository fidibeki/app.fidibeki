package aiprovider_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/aiprovider"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

type rejectedValidation struct{ ready bool }

func (v *rejectedValidation) Models(context.Context, aiprovider.Connection, aiprovider.Secret) ([]aiprovider.Model, error) {
	if v.ready {
		return []aiprovider.Model{{ID: "fixture-model"}}, nil
	}
	return nil, &aiprovider.Failure{Code: "models_unavailable"}
}
func (v *rejectedValidation) Validate(_ context.Context, _ aiprovider.Connection, _ aiprovider.Secret, model string, inputs []string) (aiprovider.Model, error) {
	if v.ready {
		return aiprovider.Model{ID: model, Inputs: inputs, Validated: true}, nil
	}
	return aiprovider.Model{}, &aiprovider.Failure{Code: "unauthenticated"}
}

func TestCustomerConnectionHTTPPermissionsAndSafeValidationStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)
	keys, _ := aiprovider.NewKeyring("v1", map[string]string{"v1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))})
	jwt, err := auth.NewJWTManager("0123456789abcdef0123456789abcdef", "fidibeki", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	tester := &rejectedValidation{}
	limiter := &setupLimiter{}
	allowed := true
	plan := tenant.PlanGrowth
	cs := aiprovider.Store{Database: pool, Keys: keys}
	aiprovider.RegisterRoutes(router, aiprovider.HTTPOptions{Store: cs, JWT: jwt, Tester: tester, Limiter: limiter, Allowed: func(string, string) bool { return allowed }, ManagedAvailable: true})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{BaseDomain: "localhost", Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
		return tenant.Tenant{ID: testTenantID, Slug: "ai-tenant", Plan: plan, Status: tenant.StatusActive}, nil
	}), Begin: db.BeginFromPool(pool)})(router)
	permissions := []string{string(auth.PermissionAIConnectionRead), string(auth.PermissionAIConnectionManage)}
	if _, err = pool.Exec(ctx, `UPDATE tenant_members SET permissions=$2 WHERE id=$1`, testMemberID, permissions); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, perms []string) *httptest.ResponseRecorder {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE tenant_members SET permissions=$2 WHERE id=$1`, testMemberID, perms); err != nil {
			t.Fatal(err)
		}
		token, _, err := jwt.IssueTenant(testMemberID, testTenantID, perms)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "ai-tenant.localhost"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	body := `{"name":"Test account","provider":"openai","credential":"never-return-this-secret","enabled":true}`
	if response := request("POST", "/v1/ai/connections", body, []string{string(auth.PermissionAIConnectionRead)}); response.Code != 403 {
		t.Fatalf("reader could create: %d %s", response.Code, response.Body)
	}
	response := request("POST", "/v1/ai/connections", body, permissions)
	if response.Code != 201 || strings.Contains(response.Body.String(), "never-return-this-secret") {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}
	var created struct {
		Connection aiprovider.Connection `json:"connection"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	response = request("POST", "/v1/ai/connections/"+created.Connection.ID+"/validate", `{"model":"fixture-model","input_categories":["feedback_text"]}`, permissions)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"valid":false`) {
		t.Fatalf("validation: %d %s", response.Code, response.Body)
	}
	c, err := cs.Get(ctx, pool, testTenantID, created.Connection.ID)
	if err != nil || c.Health != "unauthenticated" {
		t.Fatalf("failed validation rolled back: %#v %v", c, err)
	}
	response = request("GET", "/v1/ai/connections/"+created.Connection.ID+"/models", "", permissions)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"manual_model_supported":true`) {
		t.Fatalf("discovery fallback: %d %s", response.Code, response.Body)
	}
	tester.ready = true
	response = request("GET", "/v1/ai/connections/"+created.Connection.ID+"/models", "", permissions)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"discovery_available":true`) {
		t.Fatalf("model discovery: %s", response.Body)
	}
	for _, invalid := range []string{`{"model":"","input_categories":["feedback_text"]}`, `{"model":"fixture-model","input_categories":["video"]}`} {
		response = request("POST", "/v1/ai/connections/"+created.Connection.ID+"/validate", invalid, permissions)
		if response.Code != 422 {
			t.Fatalf("invalid validation accepted: %d", response.Code)
		}
	}
	response = request("POST", "/v1/ai/connections/"+created.Connection.ID+"/validate", `{"model":"fixture-model","input_categories":["feedback_text"]}`, permissions)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"valid":true`) {
		t.Fatalf("validation success: %s", response.Body)
	}
	if response = request("PATCH", "/v1/ai/connections/"+created.Connection.ID, body, permissions); response.Code != 409 {
		t.Fatalf("stale update accepted: %d %s", response.Code, response.Body)
	}
	if response = request("GET", "/v1/ai/connections", "", permissions); strings.Contains(response.Body.String(), "never-return-this-secret") || strings.Contains(response.Body.String(), "envelope") {
		t.Fatal("secret leaked in connection list")
	}
	response = request("GET", "/v1/ai/providers", "", permissions)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"managed"`) {
		t.Fatalf("catalog: %d %s", response.Code, response.Body)
	}
	for _, invalid := range []string{`{"name":"unknown","provider":"unknown"}`, `{"unexpected":true}`, `{}`} {
		response = request("POST", "/v1/ai/connections", invalid, permissions)
		if response.Code < 400 {
			t.Fatalf("invalid create accepted: %s", invalid)
		}
	}
	for _, route := range []string{"bad-id", "00000000-0000-4000-8000-000000000999"} {
		response = request("GET", "/v1/ai/connections/"+route+"/models", "", permissions)
		if response.Code != 404 {
			t.Fatalf("missing connection: %d", response.Code)
		}
	}
	input := aiprovider.Input{Name: "Renamed account", Provider: aiprovider.OpenAI, Revision: created.Connection.Revision, Enabled: true}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	response = request("PATCH", "/v1/ai/connections/"+created.Connection.ID, string(raw), permissions)
	if response.Code != 200 {
		t.Fatalf("update without replacing secret: %d %s", response.Code, response.Body)
	}
	c, err = cs.Get(ctx, pool, testTenantID, created.Connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cs.Credential(ctx, pool, c)
	if err != nil || secret.Value != "never-return-this-secret" || c.Name != input.Name {
		t.Fatalf("blank credential changed secret: %v", err)
	}
	for _, method := range []string{"PATCH", "DELETE"} {
		response = request(method, "/v1/ai/connections/bad-id", body, permissions)
		if response.Code != 404 {
			t.Fatalf("invalid connection ID: %d", response.Code)
		}
	}
	response = request("PATCH", "/v1/ai/connections/"+c.ID, `{"unexpected":true}`, permissions)
	if response.Code != 400 {
		t.Fatalf("unknown update property: %d", response.Code)
	}
	limiter.blocked = true
	response = request("GET", "/v1/ai/connections/"+c.ID+"/models", "", permissions)
	if response.Code != 429 {
		t.Fatalf("setup rate limit: %d", response.Code)
	}
	limiter.blocked = false
	allowed = false
	response = request("GET", "/v1/ai/connections/"+c.ID+"/models", "", permissions)
	if response.Code != 422 {
		t.Fatalf("disabled rollout discovered models: %d", response.Code)
	}
	response = request("POST", "/v1/ai/connections", body, permissions)
	if response.Code != 422 {
		t.Fatalf("disabled rollout created connection: %d", response.Code)
	}
	response = request("PATCH", "/v1/ai/connections/"+c.ID, body, permissions)
	if response.Code != 422 {
		t.Fatalf("disabled rollout enabled connection: %d", response.Code)
	}
	allowed = true
	plan = tenant.PlanStarter
	response = request("GET", "/v1/ai/connections", "", permissions)
	if response.Code != 402 {
		t.Fatalf("starter plan accessed AI credentials: %d", response.Code)
	}
	plan = tenant.PlanGrowth
	response = request("DELETE", "/v1/ai/connections/"+c.ID, "", permissions)
	if response.Code != 200 {
		t.Fatalf("delete: %d %s", response.Code, response.Body)
	}
	response = request("GET", "/v1/ai/connections", "", permissions)
	if response.Code != 200 || strings.Contains(response.Body.String(), c.ID) {
		t.Fatalf("deleted connection visible: %s", response.Body)
	}
	response = request("DELETE", "/v1/ai/connections/"+c.ID, "", permissions)
	if response.Code != 404 {
		t.Fatalf("repeat delete: %d", response.Code)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ai_connection_credentials WHERE connection_id=$1`, c.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("credential not erased", err)
	}

}

const (
	testTenantID = "10000000-0000-4000-8000-000000000001"
	testMemberID = "10000000-0000-4000-8000-000000000010"
)

func aiTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
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
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(databaseURL, aiMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate AI database: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open AI database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func aiMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve AI test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	return path
}

func seedAIFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id,name,slug,plan,status) VALUES ($1,'AI tenant','ai-tenant','growth','active')`, testTenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tenant_members (id,tenant_id,email,permissions) VALUES ($1,$2,'analyst@ai-tenant.test',ARRAY['feedback:update'])`, testMemberID, testTenantID); err != nil {
		t.Fatal(err)
	}
}

func TestValidatedDestinationChangesRequireFreshAuthorization(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := aiTestDatabase(t, ctx)
	seedAIFeedback(t, ctx, pool)
	keys, err := aiprovider.NewKeyring("v1", map[string]string{"v1": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))})
	if err != nil {
		t.Fatal(err)
	}
	store := aiprovider.Store{Database: pool, Keys: keys}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	input := aiprovider.Input{Name: "Compatible account", Provider: aiprovider.CustomOpenAI, BaseURL: "https://api.example.com/v1", Credential: "fixture-key", Enabled: true, IsDefault: true}
	c, err := store.Create(ctx, tx, testTenantID, testMemberID, input)
	if err != nil {
		t.Fatal(err)
	}
	selection := aiprovider.Selection{BillingMode: aiprovider.Customer, Provider: c.Provider, ConnectionID: c.ID, ConfigVersion: c.ConfigVersion, Model: "model"}
	model := aiprovider.Model{ID: "model", Inputs: []string{"feedback_text"}, Validated: true}
	if err = store.RecordValidation(ctx, tx, c, testMemberID, model, nil); err != nil {
		t.Fatal(err)
	}
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, selection, model.Inputs); err != nil {
		t.Fatal(err)
	}
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, selection, []string{"screenshot"}); !errors.Is(err, aiprovider.ErrUnvalidated) {
		t.Fatal("untested input accepted", err)
	}
	input.BaseURL = "https://new.example.com/v1"
	input.Revision = c.Revision
	input.Credential = ""
	if _, err = store.Update(ctx, tx, testTenantID, testMemberID, c.ID, input); !errors.Is(err, aiprovider.ErrInvalid) {
		t.Fatal("destination reused secret", err)
	}
	input.Credential = "replacement-fixture-key"
	changed, err := store.Update(ctx, tx, testTenantID, testMemberID, c.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ConfigVersion != c.ConfigVersion+1 || changed.CredentialVersion != c.CredentialVersion+1 || len(changed.Models) != 0 {
		t.Fatal("destination retained authorization")
	}
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, selection, model.Inputs); !errors.Is(err, aiprovider.ErrUnvalidated) {
		t.Fatal("old selection survived", err)
	}
	if err = store.RecordValidation(ctx, tx, c, testMemberID, model, nil); !errors.Is(err, aiprovider.ErrConflict) {
		t.Fatal("stale validation accepted", err)
	}
	if err = store.RecordValidation(ctx, tx, changed, testMemberID, model, nil); err != nil {
		t.Fatal(err)
	}
	selection.ConfigVersion = changed.ConfigVersion
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, selection, model.Inputs); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordValidation(ctx, tx, changed, testMemberID, model, &aiprovider.Failure{Code: "model_unavailable"}); err != nil {
		t.Fatal(err)
	}
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, selection, model.Inputs); !errors.Is(err, aiprovider.ErrUnvalidated) {
		t.Fatal("failed revalidation retained access", err)
	}
	input.Name = "Second account"
	second, err := store.Create(ctx, tx, testTenantID, testMemberID, input)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Get(ctx, tx, testTenantID, c.ID)
	if err != nil || first.IsDefault || !second.IsDefault {
		t.Fatal("multiple default accounts", err)
	}
	role, err := store.Create(ctx, tx, testTenantID, testMemberID, aiprovider.Input{Name: "AWS role", Provider: aiprovider.Bedrock, Region: "eu-west-1", AuthMode: "role", RoleARN: "arn:aws:iam::123456789012:role/customer", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if role.ExternalID == "" {
		t.Fatal("role missing external ID")
	}
	secret, err := store.Credential(ctx, tx, role)
	if err != nil || secret.Value != "" {
		t.Fatal("role persisted supplied key", err)
	}
	role.Enabled = false
	if _, err = store.Credential(ctx, tx, role); !errors.Is(err, aiprovider.ErrDisabled) {
		t.Fatal("disabled account credential disclosed", err)
	}
	role.Enabled = true
	expired := time.Now().Add(-time.Minute)
	role.CredentialExpiresAt = &expired
	if _, err = store.Credential(ctx, tx, role); err == nil {
		t.Fatal("expired credential disclosed")
	}
	role.CredentialExpiresAt = nil
	role.CredentialVersion = 999
	if _, err = store.Credential(ctx, tx, role); !errors.Is(err, aiprovider.ErrNotFound) {
		t.Fatal("missing key version disclosed", err)
	}
	for _, invalid := range []aiprovider.Selection{{BillingMode: "unknown"}, {BillingMode: aiprovider.Managed, ConnectionID: c.ID, Provider: aiprovider.Google}, {BillingMode: aiprovider.Customer, ConnectionID: "bad-id", Model: "model"}} {
		if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, invalid, model.Inputs); !errors.Is(err, aiprovider.ErrInvalid) {
			t.Fatal("invalid selection accepted", err)
		}
	}
	if err = aiprovider.ValidateSelection(ctx, tx, testTenantID, aiprovider.Selection{BillingMode: aiprovider.Managed, Provider: aiprovider.Google}, model.Inputs); err != nil {
		t.Fatal("managed selection rejected", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

type setupLimiter struct{ blocked bool }

func (l *setupLimiter) Allow(context.Context, string, ratelimit.Window) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: !l.blocked}, nil
}
