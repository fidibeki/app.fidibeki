package samlsso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

// TestSAMLSSOHTTPFlow drives the SSO endpoints through the real tenant
// middleware against PostgreSQL, so route registration, subdomain resolution,
// the RLS-scoped transaction, permission enforcement, and the handlers are
// exercised together rather than in isolation.
func TestSAMLSSOHTTPFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newSAMLHarness(ctx, t)
	// Deliberately not the auto-configured workspace: this test drives the
	// configuration lifecycle from nothing.
	acme := harness.workspace(ctx, t, "globex", "Globex")

	jwtManager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	routes := chi.NewRouter()
	RegisterRoutes(routes, HTTPOptions{
		Service: harness.service, Store: harness.store, JWT: jwtManager, Logger: discardLogger(),
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: harness.pools.Admin},
		Begin: db.BeginFromPool(harness.pools.App), Logger: discardLogger(),
	})(routes)

	adminToken, _, err := jwtManager.IssueTenantMember(
		acme.ownerID, acme.id, auth.TenantPermissionStrings(), 0,
	)
	if err != nil {
		t.Fatalf("issue administrator token: %v", err)
	}
	// A member with no tenant:update permission, to prove the configuration
	// surface is closed to ordinary members.
	memberID := harness.member(ctx, t, acme, "member@globex.test", []string{"feedback:read"})
	memberToken, _, err := jwtManager.IssueTenantMember(
		memberID, acme.id, []string{"feedback:read"}, 0,
	)
	if err != nil {
		t.Fatalf("issue member token: %v", err)
	}

	t.Run("policy discovery is public and reports the disabled default", func(t *testing.T) {
		response := samlRequest(handler, http.MethodGet, StatusPath, "", "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		var policy Policy
		if err := json.Unmarshal(response.Body.Bytes(), &policy); err != nil {
			t.Fatalf("decode policy: %v", err)
		}
		if policy.Mode != auth.SSOModeDisabled || !policy.LocalLoginEnabled {
			t.Fatalf("policy = %#v, want the disabled local-login default", policy)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
		}
	})

	t.Run("configuration requires workspace administration", func(t *testing.T) {
		for _, request := range []struct {
			method, path, body string
		}{
			{http.MethodGet, "/v1/sso/saml", ""},
			{http.MethodPut, "/v1/sso/saml", `{"idp_entity_id":"https://idp.test"}`},
			{http.MethodPatch, "/v1/sso/saml/mode", `{"mode":"optional"}`},
			{http.MethodDelete, "/v1/sso/saml", ""},
			{http.MethodPost, "/v1/sso/saml/test", ""},
		} {
			anonymous := samlRequest(handler, request.method, request.path, request.body, "")
			if anonymous.Code != http.StatusUnauthorized {
				t.Errorf("%s %s anonymous = %d, want 401", request.method, request.path, anonymous.Code)
			}
			member := samlRequest(handler, request.method, request.path, request.body, memberToken)
			if member.Code != http.StatusForbidden {
				t.Errorf("%s %s member = %d, want 403", request.method, request.path, member.Code)
			}
		}
	})

	t.Run("an unconfigured workspace reports its service provider identity", func(t *testing.T) {
		response := samlRequest(handler, http.MethodGet, "/v1/sso/saml", "", adminToken)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		var payload struct {
			Configured      bool `json:"configured"`
			ServiceProvider struct {
				EntityID string `json:"entity_id"`
				ACSURL   string `json:"acs_url"`
			} `json:"service_provider"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// The values an administrator needs to set up their identity provider
		// must be available before anything is configured.
		if payload.Configured {
			t.Error("configured = true before any configuration")
		}
		if !strings.HasSuffix(payload.ServiceProvider.EntityID, MetadataPath) ||
			!strings.HasSuffix(payload.ServiceProvider.ACSURL, ACSPath) {
			t.Fatalf("service provider = %#v, want the derived endpoints", payload.ServiceProvider)
		}
	})

	t.Run("an administrator configures the identity provider", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"idp_entity_id": acme.idp.entityID,
			"idp_sso_url":   "https://idp.test/sso",
			"certificates":  []string{acme.idp.storedCertificate(t).Certificate},
		})
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		response := samlRequest(handler, http.MethodPut, "/v1/sso/saml", string(body), adminToken)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		var payload struct {
			Configured bool     `json:"configured"`
			CanRequire bool     `json:"can_require"`
			Warnings   []string `json:"warnings"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !payload.Configured || payload.CanRequire {
			t.Fatalf("payload = %#v, want configured but not yet requirable", payload)
		}
	})

	t.Run("manual configuration rejects an insecure identity provider endpoint", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"idp_entity_id": acme.idp.entityID,
			"idp_sso_url":   "http://idp.test/sso",
			"certificates":  []string{acme.idp.storedCertificate(t).Certificate},
		})
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		response := samlRequest(handler, http.MethodPut, "/v1/sso/saml", string(body), adminToken)
		if response.Code != http.StatusUnprocessableEntity ||
			!strings.Contains(response.Body.String(), `"idp_sso_url"`) ||
			!strings.Contains(response.Body.String(), "must use HTTPS") {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}

		stored := samlRequest(handler, http.MethodGet, "/v1/sso/saml", "", adminToken)
		if stored.Code != http.StatusOK ||
			strings.Contains(stored.Body.String(), `"idp_sso_url":"http://idp.test/sso"`) {
			t.Fatalf("stored configuration after rejection = %d/%s", stored.Code, stored.Body.String())
		}
	})

	t.Run("invalid configuration is rejected field by field", func(t *testing.T) {
		response := samlRequest(handler, http.MethodPut, "/v1/sso/saml",
			`{"idp_entity_id":"https://idp.test","idp_sso_url":"not a url","certificates":["nonsense"]}`,
			adminToken)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), "idp_sso_url") {
			t.Errorf("body = %s, want the offending field named", response.Body)
		}
	})

	t.Run("metadata is served to the identity provider without a session", func(t *testing.T) {
		response := samlRequest(handler, http.MethodGet, MetadataPath, "", "")
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		if !strings.Contains(response.Header().Get("Content-Type"), "samlmetadata+xml") {
			t.Errorf("Content-Type = %q", response.Header().Get("Content-Type"))
		}
		if !strings.Contains(response.Body.String(), "AssertionConsumerService") {
			t.Errorf("metadata body = %s", response.Body)
		}
	})

	t.Run("required mode is refused until a test succeeds", func(t *testing.T) {
		response := samlRequest(handler, http.MethodPatch, "/v1/sso/saml/mode",
			`{"mode":"required"}`, adminToken)
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), "sso_test_required") {
			t.Errorf("body = %s, want the sso_test_required code", response.Body)
		}
		invalid := samlRequest(handler, http.MethodPatch, "/v1/sso/saml/mode",
			`{"mode":"sideways"}`, adminToken)
		if invalid.Code != http.StatusUnprocessableEntity {
			t.Errorf("invalid mode status = %d, want 422", invalid.Code)
		}
	})

	t.Run("a test sign-in returns the identity provider redirect", func(t *testing.T) {
		response := samlRequest(handler, http.MethodPost, "/v1/sso/saml/test", "", adminToken)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		var payload struct {
			StartURL string `json:"start_url"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.HasPrefix(payload.StartURL, "https://idp.test/sso?") {
			t.Fatalf("start_url = %q, want the configured endpoint", payload.StartURL)
		}
	})

	t.Run("optional mode advertises the sign-in entry point", func(t *testing.T) {
		response := samlRequest(handler, http.MethodPatch, "/v1/sso/saml/mode",
			`{"mode":"optional"}`, adminToken)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		status := samlRequest(handler, http.MethodGet, StatusPath, "", "")
		var policy Policy
		if err := json.Unmarshal(status.Body.Bytes(), &policy); err != nil {
			t.Fatalf("decode policy: %v", err)
		}
		if policy.Mode != auth.SSOModeOptional || policy.StartURL == "" || !policy.LocalLoginEnabled {
			t.Fatalf("policy = %#v, want optional with both paths open", policy)
		}
	})

	t.Run("the start endpoint redirects the browser to the identity provider", func(t *testing.T) {
		response := samlRequest(handler, http.MethodGet, StartPath, "", "")
		if response.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		if !strings.HasPrefix(response.Header().Get("Location"), "https://idp.test/sso?") {
			t.Fatalf("Location = %q", response.Header().Get("Location"))
		}
	})

	// The identity provider posts here from its own origin, so a rejection must
	// return the browser to the login page rather than answer with JSON.
	t.Run("a rejected assertion returns the browser to the login page", func(t *testing.T) {
		response := samlRequest(handler, http.MethodPost, ACSPath, "SAMLResponse=not-base64", "")
		if response.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		location := response.Header().Get("Location")
		if !strings.Contains(location, "/login?") || !strings.Contains(location, "sso_error=") {
			t.Fatalf("Location = %q, want the login page with a reason code", location)
		}
		missing := samlRequest(handler, http.MethodPost, ACSPath, "", "")
		if missing.Code != http.StatusSeeOther ||
			!strings.Contains(missing.Header().Get("Location"), "sso_error=missing_response") {
			t.Fatalf("missing response = %d %q", missing.Code, missing.Header().Get("Location"))
		}
	})

	t.Run("certificates are added and retired through the API", func(t *testing.T) {
		successor := newTestIdP(t, acme.idp.entityID).storedCertificate(t)
		body, err := json.Marshal(map[string]string{"certificate": successor.Certificate})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		added := samlRequest(handler, http.MethodPost, "/v1/sso/saml/certificates", string(body), adminToken)
		if added.Code != http.StatusOK {
			t.Fatalf("add status = %d, body = %s", added.Code, added.Body)
		}
		var payload struct {
			SSO struct {
				Certificates []Certificate `json:"certificates"`
			} `json:"sso"`
		}
		if err := json.Unmarshal(added.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(payload.SSO.Certificates) != 2 {
			t.Fatalf("certificates = %d, want both trusted during rotation", len(payload.SSO.Certificates))
		}
		removed := samlRequest(handler, http.MethodDelete,
			"/v1/sso/saml/certificates/"+payload.SSO.Certificates[0].ID, "", adminToken)
		if removed.Code != http.StatusOK {
			t.Fatalf("remove status = %d, body = %s", removed.Code, removed.Body)
		}
		unknown := samlRequest(handler, http.MethodDelete,
			"/v1/sso/saml/certificates/00000000-0000-0000-0000-000000000000", "", adminToken)
		if unknown.Code != http.StatusNotFound {
			t.Errorf("unknown certificate status = %d, want 404", unknown.Code)
		}
		malformed := samlRequest(handler, http.MethodPost, "/v1/sso/saml/certificates",
			`{"certificate":"not-a-certificate"}`, adminToken)
		if malformed.Code != http.StatusUnprocessableEntity {
			t.Errorf("malformed certificate status = %d, want 422", malformed.Code)
		}
	})

	t.Run("removing the configuration restores the local default", func(t *testing.T) {
		response := samlRequest(handler, http.MethodDelete, "/v1/sso/saml", "", adminToken)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body)
		}
		status := samlRequest(handler, http.MethodGet, StatusPath, "", "")
		var policy Policy
		if err := json.Unmarshal(status.Body.Bytes(), &policy); err != nil {
			t.Fatalf("decode policy: %v", err)
		}
		if policy.Mode != auth.SSOModeDisabled || !policy.LocalLoginEnabled {
			t.Fatalf("policy = %#v, want the local default restored", policy)
		}
		// The start endpoint must stop working the moment SSO is removed.
		start := samlRequest(handler, http.MethodGet, StartPath, "", "")
		if start.Code != http.StatusSeeOther ||
			!strings.Contains(start.Header().Get("Location"), "sso_error=") {
			t.Fatalf("start after removal = %d %q", start.Code, start.Header().Get("Location"))
		}
		again := samlRequest(handler, http.MethodDelete, "/v1/sso/saml", "", adminToken)
		if again.Code != http.StatusNotFound {
			t.Errorf("second removal status = %d, want 404", again.Code)
		}
	})
}

func samlRequest(
	handler http.Handler, method, path, body, accessToken string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://globex.localhost"+path, strings.NewReader(body))
	if body != "" {
		if strings.HasPrefix(body, "SAMLResponse=") {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			request.Header.Set("Content-Type", "application/json")
		}
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}
