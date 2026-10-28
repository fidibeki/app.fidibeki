package samlsso

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func testHandler() *HTTPHandler {
	return &HTTPHandler{options: HTTPOptions{
		Service: Service{Endpoints: testEndpoints()},
		Logger:  discardLogger(),
	}}
}

// Every configuration error must reach the administrator as a specific,
// actionable status rather than a generic failure.
func TestWriteErrorHasStableHTTPContracts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{
			name:   "validation",
			err:    ValidationError{Fields: map[string]string{"idp_sso_url": "required"}},
			status: http.StatusUnprocessableEntity, code: "validation_error",
		},
		{
			name: "inactive tenant", err: tenant.ErrInactive,
			status: http.StatusForbidden, code: "tenant_suspended",
		},
		{name: "not configured", err: ErrNotConfigured, status: http.StatusNotFound, code: "not_found"},
		{name: "disabled", err: ErrSSODisabled, status: http.StatusConflict, code: "conflict"},
		{
			name: "test required", err: ErrTestRequired,
			status: http.StatusConflict, code: "sso_test_required",
		},
		{
			name: "certificate required", err: ErrCertificateRequired,
			status: http.StatusConflict, code: "invalid_sso_configuration",
		},
		{
			name: "last certificate", err: ErrLastCertificate,
			status: http.StatusConflict, code: "invalid_sso_configuration",
		},
		{
			name: "certificate not found", err: ErrCertificateNotFound,
			status: http.StatusNotFound, code: "not_found",
		},
		{
			name: "ineligible member", err: ErrMemberNotEligible,
			status: http.StatusUnauthorized, code: "unauthenticated",
		},
		{
			name: "invalid assertion", err: ErrAssertionInvalid,
			status: http.StatusUnauthorized, code: "unauthenticated",
		},
		{
			name: "replayed assertion", err: ErrAssertionReplayed,
			status: http.StatusUnauthorized, code: "unauthenticated",
		},
		{
			name: "uncorrelated request", err: ErrRequestNotFound,
			status: http.StatusUnauthorized, code: "unauthenticated",
		},
		{
			name: "request capacity", err: ErrAuthRequestCapacity,
			status: http.StatusServiceUnavailable, code: "service_unavailable",
		},
		{
			name: "unexpected", err: errors.New("boom"),
			status: http.StatusInternalServerError, code: "internal_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			testHandler().writeError(response, httptest.NewRequest(http.MethodGet, "/", nil), test.err)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d (body %s)", response.Code, test.status, response.Body)
			}
			if !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("body = %s, want code %q", response.Body, test.code)
			}
		})
	}
}

// An unexpected failure must not leak its cause to the caller.
func TestWriteErrorKeepsUnexpectedCausesInternal(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	testHandler().writeError(
		response, httptest.NewRequest(http.MethodGet, "/", nil),
		errors.New("connection to idp-secrets.internal refused"),
	)
	if strings.Contains(response.Body.String(), "idp-secrets.internal") {
		t.Fatalf("body = %s, want the underlying cause withheld", response.Body)
	}
}

// The codes a rejected sign-in redirects with are deliberately coarse: they
// must never let a caller tell a bad signature from an unknown member.
func TestAssertionFailureReasonIsCoarse(t *testing.T) {
	t.Parallel()
	tests := map[string]error{
		"assertion_replayed":     ErrAssertionReplayed,
		"request_not_correlated": ErrRequestNotFound,
		"member_not_eligible":    ErrMemberNotEligible,
		"sso_unavailable":        ErrNotConfigured,
		"assertion_invalid":      ErrAssertionInvalid,
		"sso_failed":             errors.New("unexpected"),
	}
	for want, err := range tests {
		if got := assertionFailureReason(err); got != want {
			t.Errorf("assertionFailureReason(%v) = %q, want %q", err, got, want)
		}
	}
	if got := assertionFailureReason(ErrSSODisabled); got != "sso_unavailable" {
		t.Errorf("disabled reason = %q, want sso_unavailable", got)
	}
	if got := assertionFailureReason(tenant.ErrInactive); got != "sso_unavailable" {
		t.Errorf("inactive tenant reason = %q, want sso_unavailable", got)
	}
}

// A rejected sign-in returns the member to their own login page carrying only
// the coarse reason code.
func TestRedirectFailureReturnsToTheWorkspaceLogin(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	testHandler().redirectFailure(
		response, httptest.NewRequest(http.MethodPost, "/auth/sso/saml/acs", nil),
		tenant.Tenant{ID: "tenant-1", Slug: testSlug},
		"assertion_invalid", errors.New("signature could not be verified"),
	)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "https://acme.fidibeki.test/login?") ||
		!strings.Contains(location, "sso_error=assertion_invalid") {
		t.Fatalf("Location = %q, want the workspace login with the reason code", location)
	}
	if strings.Contains(location, "signature could not be verified") {
		t.Fatalf("Location = %q, want the underlying cause withheld", location)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
}

// Without a resolved tenant no handler may act; each must fail closed rather
// than operate on an unscoped request.
func TestHandlersRequireAResolvedTenant(t *testing.T) {
	t.Parallel()
	handler := testHandler()
	// Handlers that take a body decode it first, so each needs one this handler
	// accepts; otherwise the decode rejects the request before the tenant check
	// under test is reached.
	routes := []struct {
		name  string
		route func(http.ResponseWriter, *http.Request)
		body  string
	}{
		{name: "metadata", route: handler.metadata},
		{name: "status", route: handler.status},
		{name: "start", route: handler.start},
		{name: "acs", route: handler.assertionConsumer},
		{name: "show", route: handler.show},
		{name: "startTest", route: handler.startTest},
		{name: "removeCert", route: handler.removeCertificate},
		{name: "remove", route: handler.remove},
		{name: "setMode", route: handler.setMode, body: `{"mode":"optional"}`},
		{name: "addCert", route: handler.addCertificate, body: `{"certificate":"not-a-certificate"}`},
		{name: "configure", route: handler.configure, body: `{"idp_entity_id":"https://idp.test"}`},
	}
	for _, test := range routes {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			body := test.body
			if body == "" {
				body = "{}"
			}
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			test.route(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 without a resolved tenant", response.Code)
			}
		})
	}
}

func TestDecodeJSONRejectsMalformedAndUnknownFields(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{`, `{"unexpected":true}`, ``} {
		var request struct {
			Mode string `json:"mode"`
		}
		response := httptest.NewRecorder()
		if decodeJSON(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &request) {
			t.Errorf("decodeJSON(%q) accepted the body", body)
		}
		if response.Code != http.StatusBadRequest {
			t.Errorf("decodeJSON(%q) status = %d, want 400", body, response.Code)
		}
	}
	var request struct {
		Mode string `json:"mode"`
	}
	response := httptest.NewRecorder()
	if !decodeJSON(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"mode":"required"}`)), &request) {
		t.Fatal("decodeJSON rejected a valid body")
	}
	if request.Mode != "required" {
		t.Fatalf("Mode = %q, want required", request.Mode)
	}
}

// The identity provider reaches the assertion consumer without a session, so
// the routes it needs must be registered outside the authenticated group.
func TestRegisterRoutesExposesTheIdentityProviderEndpoints(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	RegisterRoutes(router, HTTPOptions{Service: Service{Endpoints: testEndpoints()}})

	registered := map[string]bool{}
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	for _, route := range []string{
		"GET " + MetadataPath, "GET " + StatusPath, "GET " + StartPath, "POST " + ACSPath,
		"GET /v1/sso/saml", "PUT /v1/sso/saml", "PATCH /v1/sso/saml/mode",
		"DELETE /v1/sso/saml", "POST /v1/sso/saml/certificates", "POST /v1/sso/saml/test",
	} {
		if !registered[route] {
			t.Errorf("route %q is not registered", route)
		}
	}
}

func TestMetadataFetcherReadsALocalDocument(t *testing.T) {
	t.Parallel()
	document := `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.test"/>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(document))
	}))
	defer server.Close()

	fetcher := MetadataFetcher{AllowInsecure: true}
	fetched, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if string(fetched) != document {
		t.Fatalf("Fetch() = %q, want the served document", fetched)
	}
}

func TestMetadataFetcherRejectsUnusableResponses(t *testing.T) {
	t.Parallel()
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxMetadataBytes+1))
	}))
	defer oversized.Close()
	// A redirect must not be followed: a public host could otherwise bounce the
	// request into private address space after the destination check passed.
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/metadata", http.StatusFound)
	}))
	defer redirecting.Close()

	fetcher := MetadataFetcher{AllowInsecure: true}
	for name, target := range map[string]string{
		"not found": notFound.URL, "oversized": oversized.URL, "redirect": redirecting.URL,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fetcher.Fetch(context.Background(), target); !errors.Is(err, ErrValidation) {
				t.Fatalf("Fetch() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestMaintainerRequiresItsCollaborators(t *testing.T) {
	t.Parallel()
	if err := (Maintainer{}).MaintainOnce(context.Background()); err == nil {
		t.Fatal("MaintainOnce() error = nil without a store")
	}
	maintainer := Maintainer{Store: MaintenanceStore{Database: stubQuerier{}}}
	if err := maintainer.MaintainOnce(context.Background()); err == nil {
		t.Fatal("MaintainOnce() error = nil without a health recorder")
	}
}

// The maintainer's tick must survive a sweep failure rather than take the
// worker down with it.
func TestMaintainerRunStopsWithItsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	maintainer := Maintainer{
		Store:  MaintenanceStore{Database: stubQuerier{}},
		Health: stubRecorder{}, Logger: discardLogger(),
	}
	if err := maintainer.Run(ctx, time.Hour); err != nil {
		t.Fatalf("Run() error = %v, want a clean stop", err)
	}
}

func TestWriteConfigurationReportsWarningsAndReadiness(t *testing.T) {
	t.Parallel()
	now := time.Now()
	response := httptest.NewRecorder()
	testHandler().writeConfiguration(
		response, httptest.NewRequest(http.MethodGet, "/", nil),
		tenant.Tenant{ID: "tenant-1", Slug: testSlug},
		Configuration{Mode: "required", Certificates: []Certificate{
			{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)},
		}},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", response.Code, response.Body)
	}
	var payload struct {
		Configured bool     `json:"configured"`
		CanRequire bool     `json:"can_require"`
		Warnings   []string `json:"warnings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Configured {
		t.Error("configured = false, want true")
	}
	// Untested configurations can never be required, whatever their mode says.
	if payload.CanRequire {
		t.Error("can_require = true for an untested configuration")
	}
	var managed bool
	for _, warning := range payload.Warnings {
		if warning == "mfa_managed_by_idp" {
			managed = true
		}
	}
	if !managed {
		t.Errorf("warnings = %v, want the IdP-managed MFA notice under required mode", payload.Warnings)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// stubQuerier stands in for a pool the maintainer never reaches, so the
// collaborator guards can be exercised without a database.
type stubQuerier struct{}

func (stubQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unavailable")
}
func (stubQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unavailable")
}
func (stubQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return stubRow{} }

type stubRow struct{}

func (stubRow) Scan(...any) error { return errors.New("unavailable") }

type stubRecorder struct{}

func (stubRecorder) RecordRun(context.Context, retentionhealth.Run) error { return nil }

func TestValidationErrorCarriesItsFieldsAndUnwraps(t *testing.T) {
	t.Parallel()
	err := ValidationError{Fields: map[string]string{"idp_sso_url": "must use HTTPS"}}
	if !errors.Is(err, ErrValidation) {
		t.Fatal("ValidationError does not unwrap to ErrValidation")
	}
	if err.Error() != ErrValidation.Error() {
		t.Fatalf("Error() = %q, want %q", err.Error(), ErrValidation.Error())
	}
	if err.Fields["idp_sso_url"] != "must use HTTPS" {
		t.Fatalf("Fields = %v, want the rejected field preserved", err.Fields)
	}
}

// The metadata URL doubles as the entity ID, so the two must never drift.
func TestEndpointsMetadataMatchesTheEntityID(t *testing.T) {
	t.Parallel()
	endpoints := testEndpoints()
	metadata, err := endpoints.Metadata(testSlug)
	if err != nil {
		t.Fatalf("Metadata() error = %v", err)
	}
	entityID, err := endpoints.EntityID(testSlug)
	if err != nil {
		t.Fatalf("EntityID() error = %v", err)
	}
	if metadata != entityID {
		t.Fatalf("Metadata() = %q, EntityID() = %q, want them identical", metadata, entityID)
	}
	if _, err := endpoints.Metadata("not a slug"); err == nil {
		t.Error("Metadata() error = nil for an invalid slug")
	}
}

// A test sign-in returns the administrator to the settings tab carrying its
// outcome, so the result is visible without a second lookup.
func TestTestResultURLCarriesTheOutcome(t *testing.T) {
	t.Parallel()
	target, err := testEndpoints().TestResult(testSlug, "passed", "")
	if err != nil {
		t.Fatalf("TestResult() error = %v", err)
	}
	if !strings.Contains(target, "saml_test=passed") || !strings.Contains(target, "tab=sso") {
		t.Fatalf("TestResult() = %q, want the outcome and the settings tab", target)
	}
	detailed, err := testEndpoints().TestResult(testSlug, "failed", "assertion_invalid")
	if err != nil {
		t.Fatalf("TestResult(detail) error = %v", err)
	}
	if !strings.Contains(detailed, "saml_test_detail=assertion_invalid") {
		t.Fatalf("TestResult(detail) = %q, want the detail preserved", detailed)
	}
}
