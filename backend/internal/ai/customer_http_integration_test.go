package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/aiprovider"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

type rejectedValidation struct{}

func (rejectedValidation) Models(context.Context, aiprovider.Connection, aiprovider.Secret) ([]aiprovider.Model, error) {
	return nil, &aiprovider.Failure{Code: "models_unavailable"}
}
func (rejectedValidation) Validate(context.Context, aiprovider.Connection, aiprovider.Secret, string, []string) (aiprovider.Model, error) {
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
	cs := aiprovider.Store{Database: pool, Keys: keys}
	aiprovider.RegisterRoutes(router, aiprovider.HTTPOptions{Store: cs, JWT: jwt, Tester: rejectedValidation{}, Allowed: func(string, string) bool { return true }, ManagedAvailable: true})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{BaseDomain: "localhost", Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
		return tenant.Tenant{ID: testTenantID, Slug: "ai-tenant", Plan: tenant.PlanGrowth, Status: tenant.StatusActive}, nil
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
	if response = request("PATCH", "/v1/ai/connections/"+created.Connection.ID, body, permissions); response.Code != 409 {
		t.Fatalf("stale update accepted: %d %s", response.Code, response.Body)
	}
	if response = request("GET", "/v1/ai/connections", "", permissions); strings.Contains(response.Body.String(), "never-return-this-secret") || strings.Contains(response.Body.String(), "envelope") {
		t.Fatal("secret leaked in connection list")
	}
}
