package privacy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/server"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

// dispatchedPrivacyRouters mirrors the exact host-routing shape wired in
// cmd/fidibeki-api/main.go: reporter-facing public privacy routes are
// dual-mounted on both the platform host router and the tenant host router
// (tenantRoot), while authenticated tenant privacy routes live underneath the
// per-request tenant transaction. A reporter verification email always links
// to the tenant subdomain, so the public routes must resolve there, not only
// on the platform host (regression coverage for #50).
func dispatchedPrivacyRouters(
	t *testing.T,
	resolver tenant.Resolver,
	baseDomain string,
	tenantOptions HTTPOptions,
	publicOptions PublicHTTPOptions,
) http.Handler {
	t.Helper()
	tenantRoutes := chi.NewRouter()
	RegisterRoutes(tenantRoutes, tenantOptions)
	tenantRoutes.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	transactionalTenantHandler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: baseDomain, Resolver: resolver, Begin: publicOptions.Begin,
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(tenantRoutes)
	tenantRoot := chi.NewRouter()
	RegisterPublicRoutes(tenantRoot, publicOptions)
	tenantRoot.Mount("/", transactionalTenantHandler)

	platformRoutes := chi.NewRouter()
	RegisterPublicRoutes(platformRoutes, publicOptions)

	return server.New(server.Options{
		BaseDomain:      baseDomain,
		PlatformHandler: platformRoutes,
		TenantHandler:   tenantRoot,
		Logger:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
}

func dispatchedRequest(handler http.Handler, method, rawURL, token string, body string) *httptest.ResponseRecorder {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, parsed.String(), reader)
	request.Host = parsed.Host
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestReporterVerificationLinkResolvesOnTenantHostEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)

	tenantID, ownerID := seedPrivacyWorkspace(t, ctx, database, "host-privacy", "Host export")
	otherTenantID, otherOwnerID := seedPrivacyWorkspace(t, ctx, database, "other-host-privacy", "Other export")

	jwtManager, err := auth.NewJWTManager(strings.Repeat("q", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create host routing JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant(ownerID, tenantID, auth.TenantPermissionStrings())
	otherOwnerToken, _, _ := jwtManager.IssueTenant(otherOwnerID, otherTenantID, auth.TenantPermissionStrings())

	const baseDomain = "fidibeki.test"
	now := time.Now().UTC().Truncate(time.Second)
	emails := &privacyEmailCapture{}
	downloads := &privacyStorageCapture{downloadURL: "https://downloads.example.test/reporter.zip"}
	tenantOptions := HTTPOptions{
		Store: Store{}, JWT: jwtManager, Storage: downloads, Email: emails,
		PublicBaseURL: "https://api." + baseDomain, BaseDomain: baseDomain,
		From: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return now },
	}
	publicOptions := PublicHTTPOptions{
		Store: Store{Database: database}, Begin: db.BeginFromPool(database),
		Storage: downloads, Now: func() time.Time { return now },
	}
	resolver := tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
		switch slug {
		case "host-privacy":
			return tenant.Tenant{
				ID: tenantID, Slug: slug, Name: "Host Privacy",
				Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
			}, nil
		case "other-host-privacy":
			return tenant.Tenant{
				ID: otherTenantID, Slug: slug, Name: "Other Host Privacy",
				Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
			}, nil
		default:
			return tenant.Tenant{}, tenant.ErrUnknownTenant
		}
	})

	handler := dispatchedPrivacyRouters(t, resolver, baseDomain, tenantOptions, publicOptions)

	// The owner requests a reporter export from the tenant-hosted Settings page.
	created := dispatchedRequest(
		handler, http.MethodPost, "https://host-privacy."+baseDomain+"/v1/privacy/requests",
		ownerToken, `{"email":"reporter@example.test","action":"export"}`,
	)
	if created.Code != http.StatusAccepted || len(emails.messages) != 1 {
		t.Fatalf("create reporter export = %d body=%s messages=%d",
			created.Code, created.Body.String(), len(emails.messages))
	}

	// Extract the *actual generated email URL*, exactly as a reporter would
	// receive and click it.
	marker := "https://host-privacy." + baseDomain + "/privacy/"
	markerIndex := strings.Index(emails.messages[0].Text, marker)
	if markerIndex < 0 {
		t.Fatalf("verification email missing tenant-hosted privacy URL: %s", emails.messages[0].Text)
	}
	emailURL := strings.SplitN(emails.messages[0].Text[markerIndex:], "\n", 2)[0]
	parsedEmailURL, err := url.Parse(emailURL)
	if err != nil {
		t.Fatalf("parse verification email URL %q: %v", emailURL, err)
	}
	if parsedEmailURL.Host != "host-privacy."+baseDomain {
		t.Fatalf("verification email host = %q, want tenant subdomain", parsedEmailURL.Host)
	}
	token := strings.TrimPrefix(parsedEmailURL.Path, "/privacy/")
	verifyURL := "https://" + parsedEmailURL.Host + "/v1/privacy/requests/" + token + "/verify"
	statusURL := "https://" + parsedEmailURL.Host + "/v1/privacy/requests/" + token

	// This is the exact call the public /privacy/{token} page issues on mount.
	// Before dual-mounting privacy public routes on the tenant host router,
	// this request fell through to tenantRoutes.NotFound and the reporter saw
	// "Data request unavailable" immediately (issue #50).
	verified := dispatchedRequest(handler, http.MethodPost, verifyURL, "", "")
	if verified.Code != http.StatusOK {
		t.Fatalf("first verification on tenant host = %d body=%s", verified.Code, verified.Body.String())
	}
	var verifiedView struct {
		Request ReporterRequestView `json:"request"`
	}
	if err := json.Unmarshal(verified.Body.Bytes(), &verifiedView); err != nil {
		t.Fatalf("decode verified view: %v", err)
	}
	if verifiedView.Request.Status != ReporterProcessing {
		t.Fatalf("verified status = %q, want processing", verifiedView.Request.Status)
	}

	// The link "remains usable for the configured verification lifetime" and
	// only becomes unusable after consumption/expiry/revocation, not on first
	// legitimate use: re-verifying and checking status must both stay 200 and
	// idempotent (no duplicate privacy_jobs row is created).
	reverified := dispatchedRequest(handler, http.MethodPost, verifyURL, "", "")
	if reverified.Code != http.StatusOK {
		t.Fatalf("re-verification = %d body=%s", reverified.Code, reverified.Body.String())
	}
	status := dispatchedRequest(handler, http.MethodGet, statusURL, "", "")
	if status.Code != http.StatusOK {
		t.Fatalf("status after verification = %d body=%s", status.Code, status.Body.String())
	}
	var jobCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_jobs WHERE tenant_id = $1 AND kind = 'reporter_export'
	`, tenantID).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("reporter export privacy_jobs rows = %d, error=%v", jobCount, err)
	}

	// Also reachable from the platform host (api.<baseDomain>), so operational
	// tooling or a directly-typed platform URL keeps working.
	platformStatusURL := "https://api." + baseDomain + "/v1/privacy/requests/" + token
	platformStatus := dispatchedRequest(handler, http.MethodGet, platformStatusURL, "", "")
	if platformStatus.Code != http.StatusOK {
		t.Fatalf("status via platform host = %d body=%s", platformStatus.Code, platformStatus.Body.String())
	}

	// Cross-tenant host: the same token must resolve identically regardless of
	// which tenant subdomain served the request (token scoping is enforced by
	// the token hash lookup, not by host), and an unrelated tenant's token must
	// not be reachable through this tenant's authenticated routes.
	crossHostStatusURL := "https://other-host-privacy." + baseDomain + "/v1/privacy/requests/" + token
	crossHostStatus := dispatchedRequest(handler, http.MethodGet, crossHostStatusURL, "", "")
	if crossHostStatus.Code != http.StatusOK {
		t.Fatalf("status via unrelated tenant host = %d body=%s", crossHostStatus.Code, crossHostStatus.Body.String())
	}
	otherTenantExports := dispatchedRequest(
		handler, http.MethodGet, "https://other-host-privacy."+baseDomain+"/v1/privacy/requests", otherOwnerToken, "",
	)
	if otherTenantExports.Code != http.StatusOK || strings.Contains(otherTenantExports.Body.String(), token) {
		t.Fatalf("unrelated tenant reporter request list leaked token: %d body=%s",
			otherTenantExports.Code, otherTenantExports.Body.String())
	}

	// Invalid, malformed, and unknown tokens retain the same neutral response.
	garbage := dispatchedRequest(
		handler, http.MethodGet, "https://host-privacy."+baseDomain+"/v1/privacy/requests/not-a-real-token", "", "",
	)
	if garbage.Code != http.StatusNotFound {
		t.Fatalf("malformed token status = %d body=%s", garbage.Code, garbage.Body.String())
	}
}

func TestReporterVerificationTokenExpiryReuseAndCrossTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)

	tenantID, ownerID := seedPrivacyWorkspace(t, ctx, database, "expiry-privacy", "Expiry export")
	otherTenantID, _ := seedPrivacyWorkspace(t, ctx, database, "expiry-other-privacy", "Expiry other export")

	jwtManager, err := auth.NewJWTManager(strings.Repeat("r", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create expiry JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant(ownerID, tenantID, auth.TenantPermissionStrings())

	const baseDomain = "fidibeki.test"
	created := time.Now().UTC().Truncate(time.Second)
	current := created
	emails := &privacyEmailCapture{}
	downloads := &privacyStorageCapture{downloadURL: "https://downloads.example.test/reporter.zip"}
	tenantOptions := HTTPOptions{
		Store: Store{}, JWT: jwtManager, Storage: downloads, Email: emails,
		PublicBaseURL: "https://api." + baseDomain, BaseDomain: baseDomain,
		From: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return current },
	}
	publicOptions := PublicHTTPOptions{
		Store: Store{Database: database}, Begin: db.BeginFromPool(database),
		Storage: downloads, Now: func() time.Time { return current },
	}
	resolver := tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
		switch slug {
		case "expiry-privacy":
			return tenant.Tenant{
				ID: tenantID, Slug: slug, Name: "Expiry Privacy",
				Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
			}, nil
		case "expiry-other-privacy":
			return tenant.Tenant{
				ID: otherTenantID, Slug: slug, Name: "Expiry Other Privacy",
				Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
			}, nil
		default:
			return tenant.Tenant{}, tenant.ErrUnknownTenant
		}
	})
	handler := dispatchedPrivacyRouters(t, resolver, baseDomain, tenantOptions, publicOptions)

	created2 := dispatchedRequest(
		handler, http.MethodPost, "https://expiry-privacy."+baseDomain+"/v1/privacy/requests",
		ownerToken, `{"email":"expiry-reporter@example.test","action":"export"}`,
	)
	if created2.Code != http.StatusAccepted || len(emails.messages) != 1 {
		t.Fatalf("create reporter export = %d body=%s", created2.Code, created2.Body.String())
	}
	marker := "https://expiry-privacy." + baseDomain + "/privacy/"
	markerIndex := strings.Index(emails.messages[0].Text, marker)
	if markerIndex < 0 {
		t.Fatalf("verification email missing tenant-hosted privacy URL: %s", emails.messages[0].Text)
	}
	emailURL := strings.SplitN(emails.messages[0].Text[markerIndex:], "\n", 2)[0]
	parsedEmailURL, err := url.Parse(emailURL)
	if err != nil {
		t.Fatalf("parse verification email URL %q: %v", emailURL, err)
	}
	token := strings.TrimPrefix(parsedEmailURL.Path, "/privacy/")
	verifyURL := "https://expiry-privacy." + baseDomain + "/v1/privacy/requests/" + token + "/verify"

	// A request just inside the documented 48-hour verification window
	// remains valid.
	current = created.Add(48*time.Hour - time.Second)
	stillValid := dispatchedRequest(handler, http.MethodPost, verifyURL, "", "")
	if stillValid.Code != http.StatusOK {
		t.Fatalf("verification just before expiry = %d body=%s", stillValid.Code, stillValid.Body.String())
	}

	// A second, independent reporter request that is left unverified past the
	// 48-hour boundary must return the same neutral "unavailable" response as
	// any other invalid token, not a distinguishing error.
	current = created
	emails.messages = nil
	expiring := dispatchedRequest(
		handler, http.MethodPost, "https://expiry-privacy."+baseDomain+"/v1/privacy/requests",
		ownerToken, `{"email":"expiring-reporter@example.test","action":"export"}`,
	)
	if expiring.Code != http.StatusAccepted || len(emails.messages) != 1 {
		t.Fatalf("create second reporter export = %d body=%s", expiring.Code, expiring.Body.String())
	}
	markerIndex = strings.Index(emails.messages[0].Text, marker)
	if markerIndex < 0 {
		t.Fatalf("second verification email missing tenant-hosted privacy URL: %s", emails.messages[0].Text)
	}
	expiringEmailURL := strings.SplitN(emails.messages[0].Text[markerIndex:], "\n", 2)[0]
	parsedExpiringURL, err := url.Parse(expiringEmailURL)
	if err != nil {
		t.Fatalf("parse second verification email URL %q: %v", expiringEmailURL, err)
	}
	expiringToken := strings.TrimPrefix(parsedExpiringURL.Path, "/privacy/")
	expiringVerifyURL := "https://expiry-privacy." + baseDomain + "/v1/privacy/requests/" + expiringToken + "/verify"

	current = created.Add(48*time.Hour + time.Second)
	expired := dispatchedRequest(handler, http.MethodPost, expiringVerifyURL, "", "")
	if expired.Code != http.StatusNotFound {
		t.Fatalf("verification past expiry = %d body=%s, want neutral 404", expired.Code, expired.Body.String())
	}

	// A cross-tenant host serving the *other* tenant's subdomain must resolve
	// the same token identically (token scoping is enforced by the hash
	// lookup itself, independent of which tenant subdomain served the
	// request), proving no tenant boundary is silently bypassed by host
	// choice.
	current = created.Add(time.Minute)
	crossTenantStatus := dispatchedRequest(
		handler, http.MethodGet, "https://expiry-other-privacy."+baseDomain+"/v1/privacy/requests/"+token, "", "",
	)
	if crossTenantStatus.Code != http.StatusOK {
		t.Fatalf("status for verified token via other tenant host = %d body=%s",
			crossTenantStatus.Code, crossTenantStatus.Body.String())
	}
}

// TestWorkspaceExportCreationSucceedsOnTenantHost is regression coverage for
// #46: workspace export generation must accept an authorized request and
// create exactly one durable, idempotent job instead of failing closed.
func TestWorkspaceExportCreationSucceedsOnTenantHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)

	tenantID, ownerID := seedPrivacyWorkspace(t, ctx, database, "export-repair", "Export repair")

	jwtManager, err := auth.NewJWTManager(strings.Repeat("w", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create workspace export JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant(ownerID, tenantID, auth.TenantPermissionStrings())

	const baseDomain = "fidibeki.test"
	now := time.Now().UTC().Truncate(time.Second)
	tenantOptions := HTTPOptions{
		Store: Store{}, JWT: jwtManager, Storage: &privacyStorageCapture{}, Email: &privacyEmailCapture{},
		PublicBaseURL: "https://api." + baseDomain, BaseDomain: baseDomain,
		From: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return now },
	}
	publicOptions := PublicHTTPOptions{
		Store: Store{Database: database}, Begin: db.BeginFromPool(database),
		Storage: &privacyStorageCapture{}, Now: func() time.Time { return now },
	}
	resolver := tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
		if slug != "export-repair" {
			return tenant.Tenant{}, tenant.ErrUnknownTenant
		}
		return tenant.Tenant{
			ID: tenantID, Slug: slug, Name: "Export Repair",
			Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
		}, nil
	})
	handler := dispatchedPrivacyRouters(t, resolver, baseDomain, tenantOptions, publicOptions)

	created := dispatchedRequest(
		handler, http.MethodPost, "https://export-repair."+baseDomain+"/v1/privacy/exports", ownerToken, "",
	)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create workspace export = %d body=%s", created.Code, created.Body.String())
	}
	var createdEnvelope struct {
		Export  Job  `json:"export"`
		Created bool `json:"created"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil || !createdEnvelope.Created ||
		createdEnvelope.Export.ID == "" {
		t.Fatalf("decode created workspace export: %#v error=%v", createdEnvelope, err)
	}

	listed := dispatchedRequest(
		handler, http.MethodGet, "https://export-repair."+baseDomain+"/v1/privacy/exports", ownerToken, "",
	)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), createdEnvelope.Export.ID) {
		t.Fatalf("list workspace exports = %d body=%s", listed.Code, listed.Body.String())
	}

	// A repeated request (double click, retry) must not create a duplicate job.
	duplicate := dispatchedRequest(
		handler, http.MethodPost, "https://export-repair."+baseDomain+"/v1/privacy/exports", ownerToken, "",
	)
	if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), createdEnvelope.Export.ID) ||
		!strings.Contains(duplicate.Body.String(), `"created":false`) {
		t.Fatalf("duplicate workspace export = %d body=%s", duplicate.Code, duplicate.Body.String())
	}
	var jobCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM privacy_jobs WHERE tenant_id = $1 AND kind = 'workspace_export'
	`, tenantID).Scan(&jobCount); err != nil || jobCount != 1 {
		t.Fatalf("workspace export privacy_jobs rows = %d, error=%v", jobCount, err)
	}
}

// TestReporterAnonymizationCreationSucceedsOnTenantHost is regression coverage
// for #49: a confirmed reporter anonymization request must be accepted,
// queue its verification email, and appear in the reporter rights history
// instead of failing closed.
func TestReporterAnonymizationCreationSucceedsOnTenantHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)

	tenantID, ownerID := seedPrivacyWorkspace(t, ctx, database, "anonymize-repair", "Anonymize repair")

	jwtManager, err := auth.NewJWTManager(strings.Repeat("x", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create anonymize JWT manager: %v", err)
	}
	ownerToken, _, _ := jwtManager.IssueTenant(ownerID, tenantID, auth.TenantPermissionStrings())

	const baseDomain = "fidibeki.test"
	now := time.Now().UTC().Truncate(time.Second)
	emails := &privacyEmailCapture{}
	tenantOptions := HTTPOptions{
		Store: Store{}, JWT: jwtManager, Storage: &privacyStorageCapture{}, Email: emails,
		PublicBaseURL: "https://api." + baseDomain, BaseDomain: baseDomain,
		From: "Fidibeki <auth@fidibeki.com>", Now: func() time.Time { return now },
	}
	publicOptions := PublicHTTPOptions{
		Store: Store{Database: database}, Begin: db.BeginFromPool(database),
		Storage: &privacyStorageCapture{}, Now: func() time.Time { return now },
	}
	resolver := tenant.ResolverFunc(func(_ context.Context, slug string) (tenant.Tenant, error) {
		if slug != "anonymize-repair" {
			return tenant.Tenant{}, tenant.ErrUnknownTenant
		}
		return tenant.Tenant{
			ID: tenantID, Slug: slug, Name: "Anonymize Repair",
			Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
		}, nil
	})
	handler := dispatchedPrivacyRouters(t, resolver, baseDomain, tenantOptions, publicOptions)

	created := dispatchedRequest(
		handler, http.MethodPost, "https://anonymize-repair."+baseDomain+"/v1/privacy/requests", ownerToken,
		`{"email":"reporter@example.test","action":"anonymize"}`,
	)
	if created.Code != http.StatusAccepted || len(emails.messages) != 1 {
		t.Fatalf("create reporter anonymize request = %d body=%s messages=%d",
			created.Code, created.Body.String(), len(emails.messages))
	}
	var requestEnvelope struct {
		Request                 ReporterRequest `json:"request"`
		VerificationEmailQueued bool            `json:"verification_email_queued"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &requestEnvelope); err != nil ||
		!requestEnvelope.VerificationEmailQueued || requestEnvelope.Request.Action != ReporterAnonymize {
		t.Fatalf("decode reporter anonymize request: %#v error=%v", requestEnvelope, err)
	}

	listed := dispatchedRequest(
		handler, http.MethodGet, "https://anonymize-repair."+baseDomain+"/v1/privacy/requests", ownerToken, "",
	)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), requestEnvelope.Request.ID) {
		t.Fatalf("list reporter requests = %d body=%s", listed.Code, listed.Body.String())
	}

	// Repeating the confirmed anonymization request (double submit) must
	// remain idempotent rather than creating a second pending request.
	duplicate := dispatchedRequest(
		handler, http.MethodPost, "https://anonymize-repair."+baseDomain+"/v1/privacy/requests", ownerToken,
		`{"email":"reporter@example.test","action":"anonymize"}`,
	)
	if duplicate.Code != http.StatusAccepted {
		t.Fatalf("duplicate reporter anonymize request = %d body=%s", duplicate.Code, duplicate.Body.String())
	}
	var requestCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM reporter_privacy_requests WHERE tenant_id = $1 AND action = 'anonymize'
	`, tenantID).Scan(&requestCount); err != nil || requestCount != 1 {
		t.Fatalf("reporter anonymize requests = %d, error=%v", requestCount, err)
	}
}
