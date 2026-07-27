package platformadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestTenantCreationRequiresPermissionAndValidatesPlan(t *testing.T) {
	t.Parallel()

	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	store := &fakeTenantStore{}
	handler := NewHTTPHandler(HTTPOptions{Tenants: store, JWT: jwtManager})
	body := []byte(`{"name":"Acme","slug":"acme","plan":"growth","owner_email":"owner@acme.test"}`)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/admin/tenants", bytes.NewReader(body)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", unauthenticated.Code)
	}

	readOnlyToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantRead),
	})
	forbidden := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/tenants", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+readOnlyToken)
	handler.ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("forbidden status = %d", forbidden.Code)
	}

	createToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantCreate),
	})
	created := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/admin/tenants", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+createToken)
	handler.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	if store.created.Plan != tenant.PlanGrowth || store.created.Status != tenant.StatusTrial || store.created.TrialEndsAt == nil {
		t.Errorf("create input = %#v", store.created)
	}
	if store.created.OwnerEmail != "owner@acme.test" || store.created.ActorPlatformAdminID != "admin-id" {
		t.Errorf("owner-backed create input = %#v", store.created)
	}
}

func TestLoginDoesNotRevealWhetherAdminExists(t *testing.T) {
	t.Parallel()

	store := &fakeAuthStore{findErr: ErrAdminNotFound}
	handler := NewHTTPHandler(HTTPOptions{Auth: Service{
		Store: store, Email: &captureEnqueuer{}, PublicBaseURL: "https://api.fidibeki.com",
		AuthFrom: "Fidibeki <auth@fidibeki.com>",
	}})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost, "/admin/auth/login", strings.NewReader(`{"email":"missing@example.com"}`),
	))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestBackofficeLoginIsNonEnumeratingAndUsesConfiguredVerificationOrigin(t *testing.T) {
	t.Parallel()

	queue := &captureEnqueuer{}
	known := NewHTTPHandler(HTTPOptions{Auth: Service{
		Store: &fakeAuthStore{admin: Admin{ID: "admin-id", Email: "admin@example.com"}},
		Email: queue, PublicBaseURL: "https://api.fidibeki.com",
		BackofficeURL: "https://admin.fidibeki.com",
		AuthFrom:      "Fidibeki <auth@fidibeki.com>",
	}})
	knownResponse := httptest.NewRecorder()
	known.ServeHTTP(knownResponse, httptest.NewRequest(
		http.MethodPost, "/admin/backoffice/auth/login", strings.NewReader(`{"email":"admin@example.com"}`),
	))
	if knownResponse.Code != http.StatusAccepted {
		t.Fatalf("known status = %d, body = %s", knownResponse.Code, knownResponse.Body.String())
	}
	if len(queue.messages) != 1 {
		t.Fatalf("known queued messages = %d, want 1", len(queue.messages))
	}
	var verificationURL *url.URL
	for _, field := range strings.Fields(queue.messages[0].Text) {
		candidate, err := url.Parse(field)
		if err == nil && candidate.Query().Get("token") != "" {
			verificationURL = candidate
			break
		}
	}
	if verificationURL == nil {
		t.Fatalf("backoffice verification link missing from email: %s", queue.messages[0].Text)
	}
	if verificationURL.Scheme != "https" || verificationURL.Host != "admin.fidibeki.com" ||
		verificationURL.Path != "/auth/verify" || verificationURL.Query().Get("redirect_uri") != "" {
		t.Errorf("verification URL = %s", verificationURL)
	}

	unknownQueue := &captureEnqueuer{}
	unknown := NewHTTPHandler(HTTPOptions{Auth: Service{
		Store: &fakeAuthStore{findErr: ErrAdminNotFound}, Email: unknownQueue,
		PublicBaseURL: "https://api.fidibeki.com", BackofficeURL: "https://admin.fidibeki.com",
		AuthFrom: "Fidibeki <auth@fidibeki.com>",
	}})
	unknownResponse := httptest.NewRecorder()
	unknown.ServeHTTP(unknownResponse, httptest.NewRequest(
		http.MethodPost, "/admin/backoffice/auth/login", strings.NewReader(`{"email":"missing@example.com"}`),
	))
	if unknownResponse.Code != knownResponse.Code || unknownResponse.Body.String() != knownResponse.Body.String() {
		t.Errorf("unknown response = %d %s; known response = %d %s",
			unknownResponse.Code, unknownResponse.Body.String(), knownResponse.Code, knownResponse.Body.String())
	}
	if len(unknownQueue.messages) != 0 {
		t.Errorf("unknown admin queued %d messages", len(unknownQueue.messages))
	}
}

func TestBackofficeOverviewRequiresPlatformReadPermission(t *testing.T) {
	t.Parallel()

	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	store := &fakeBackofficeStore{overview: BackofficeOverview{
		WorkspaceCount: 12, ActiveWorkspaceCount: 7, TrialWorkspaceCount: 3,
		SuspendedWorkspaceCount: 1, CancelledWorkspaceCount: 1,
		TrialsEndingSoonCount: 2, RecentWorkspaceCount: 4,
		Plans: []BackofficePlanCount{{Plan: tenant.PlanGrowth, WorkspaceCount: 5}},
	}}
	handler := NewHTTPHandler(HTTPOptions{Backoffice: store, JWT: jwtManager})

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/admin/backoffice/overview", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", unauthenticated.Code)
	}

	wrongPermission, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantCreate),
	})
	forbidden := httptest.NewRecorder()
	forbiddenRequest := httptest.NewRequest(http.MethodGet, "/admin/backoffice/overview", nil)
	forbiddenRequest.Header.Set("Authorization", "Bearer "+wrongPermission)
	handler.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Errorf("forbidden status = %d, body = %s", forbidden.Code, forbidden.Body.String())
	}

	readToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantRead),
	})
	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest(http.MethodGet, "/admin/backoffice/overview", nil)
	allowedRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, body = %s", allowed.Code, allowed.Body.String())
	}
	if !strings.Contains(allowed.Body.String(), `"workspace_count":12`) ||
		strings.Contains(allowed.Body.String(), "admin@example.com") {
		t.Errorf("overview body = %s", allowed.Body.String())
	}
}

func TestBackofficeTenantDirectoryValidatesFiltersAndReturnsCursorPage(t *testing.T) {
	t.Parallel()

	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	readToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantRead),
	})
	createdAt := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	store := &fakeBackofficeStore{tenantPage: BackofficeTenantPage{
		Tenants: []BackofficeTenantSummary{{
			ID: "d0f62b82-8347-45e6-8f35-b0cc4114a75c", Name: "Acme", Slug: "acme",
			Plan: tenant.PlanGrowth, Status: tenant.StatusActive, CreatedAt: createdAt, UpdatedAt: createdAt,
		}},
		NextCursor: "opaque-next-cursor",
	}}
	handler := NewHTTPHandler(HTTPOptions{Backoffice: store, JWT: jwtManager})

	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodGet, "/admin/backoffice/tenants?status=unknown", nil)
	invalidRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusUnprocessableEntity || store.tenantListCalled {
		t.Fatalf("invalid filter status = %d, called = %t, body = %s", invalid.Code, store.tenantListCalled, invalid.Body.String())
	}

	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest(http.MethodGet,
		"/admin/backoffice/tenants?q=acme&status=active&plan=growth&limit=1", nil)
	allowedRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, body = %s", allowed.Code, allowed.Body.String())
	}
	if store.tenantList.Query != "acme" || store.tenantList.Status != tenant.StatusActive ||
		store.tenantList.Plan != tenant.PlanGrowth || store.tenantList.Limit != 1 ||
		!strings.Contains(allowed.Body.String(), `"next_cursor":"opaque-next-cursor"`) {
		t.Errorf("list params = %#v, body = %s", store.tenantList, allowed.Body.String())
	}
}

func TestBackofficeTenantDetailAndAuditRequireReadPermissionAndRejectInvalidIDs(t *testing.T) {
	t.Parallel()

	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	store := &fakeBackofficeStore{detail: BackofficeTenantDetail{
		Tenant:      TenantRecord{ID: "d0f62b82-8347-45e6-8f35-b0cc4114a75c", Name: "Acme", Slug: "acme"},
		MemberCount: 4, ProjectCount: 2, FeedbackCount: 9,
	}, auditPage: BackofficeAccessEventPage{Events: []BackofficeAccessEvent{{
		ID: "27a8e82a-8347-45e6-8f35-b0cc4114a75c", Kind: "ownership.provisioned", ActorType: "platform_admin",
	}}}}
	handler := NewHTTPHandler(HTTPOptions{Backoffice: store, JWT: jwtManager})

	readToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{string(auth.PermissionPlatformTenantRead)})
	invalid := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodGet, "/admin/backoffice/tenants/not-a-uuid", nil)
	invalidRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusUnprocessableEntity || store.detailCalled {
		t.Fatalf("invalid detail status = %d, called = %t", invalid.Code, store.detailCalled)
	}

	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest(http.MethodGet, "/admin/backoffice/tenants/d0f62b82-8347-45e6-8f35-b0cc4114a75c", nil)
	allowedRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusOK || !strings.Contains(allowed.Body.String(), `"feedback_count":9`) || strings.Contains(allowed.Body.String(), "token") {
		t.Fatalf("detail status = %d, body = %s", allowed.Code, allowed.Body.String())
	}

	audit := httptest.NewRecorder()
	auditRequest := httptest.NewRequest(http.MethodGet, "/admin/backoffice/tenants/d0f62b82-8347-45e6-8f35-b0cc4114a75c/access-events?limit=10", nil)
	auditRequest.Header.Set("Authorization", "Bearer "+readToken)
	handler.ServeHTTP(audit, auditRequest)
	if audit.Code != http.StatusOK || !strings.Contains(audit.Body.String(), `"ownership.provisioned"`) {
		t.Fatalf("audit status = %d, body = %s", audit.Code, audit.Body.String())
	}
}

func TestVerifySetsStrictHTTPOnlyRefreshCookie(t *testing.T) {
	t.Parallel()

	admin := Admin{ID: "admin-id", Email: "admin@example.com", Permissions: auth.PlatformPermissionStrings()}
	store := &fakeAuthStore{admin: admin}
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	handler := NewHTTPHandler(HTTPOptions{
		Auth: Service{Store: store, JWT: jwtManager}, JWT: jwtManager,
		SecureCookies: true, RefreshTTL: 30 * 24 * time.Hour,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/auth/verify", strings.NewReader(`{"token":"magic"}`))
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 3 {
		t.Errorf("cookies = %#v", cookies)
	}
	for _, cookie := range cookies {
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
			t.Errorf("cookie = %#v", cookie)
		}
	}
	queueCookie := cookieByName(cookies, PlatformQueueAccessCookie)
	if queueCookie == nil || queueCookie.Path != "/admin/queues" {
		t.Errorf("queue access cookie = %#v", queueCookie)
	}
	operationsCookie := cookieByName(cookies, PlatformOperationsAccessCookie)
	if operationsCookie == nil || operationsCookie.Path != "/admin/operations" {
		t.Errorf("operations access cookie = %#v", operationsCookie)
	}
	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["access_token"] == "" {
		t.Error("access token missing")
	}
}

func TestPlatformAdminMagicLinkRequiresConfirmationBeforeRedirectingToQueues(t *testing.T) {
	t.Parallel()

	admin := Admin{ID: "admin-id", Email: "admin@example.com", Permissions: auth.PlatformPermissionStrings()}
	store := &fakeAuthStore{admin: admin}
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	handler := NewHTTPHandler(HTTPOptions{
		Auth: Service{Store: store, JWT: jwtManager}, JWT: jwtManager,
		SecureCookies: true, RefreshTTL: 30 * 24 * time.Hour,
	})

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/admin/auth/verify?token=magic%3Ctoken%3E", nil))
	if page.Code != http.StatusOK || store.consumedHash != "" {
		t.Fatalf("GET status = %d, consumed hash = %q", page.Code, store.consumedHash)
	}
	if !strings.Contains(page.Body.String(), `value="magic&lt;token&gt;"`) ||
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "form-action 'self'") {
		t.Errorf("confirmation page or headers are incomplete: headers=%v body=%s", page.Header(), page.Body.String())
	}

	confirmed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/auth/verify", strings.NewReader("token=magic-token"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(confirmed, request)
	if confirmed.Code != http.StatusSeeOther || confirmed.Header().Get("Location") != "/admin/queues/" {
		t.Fatalf("POST status = %d, location = %q, body = %s", confirmed.Code, confirmed.Header().Get("Location"), confirmed.Body.String())
	}
	if store.consumedHash != auth.HashToken("magic-token") {
		t.Errorf("consumed hash = %q", store.consumedHash)
	}
	if cookieByName(confirmed.Result().Cookies(), PlatformQueueAccessCookie) == nil {
		t.Error("queue access cookie missing")
	}
	if cookieByName(confirmed.Result().Cookies(), PlatformOperationsAccessCookie) == nil {
		t.Error("operations access cookie missing")
	}
}

func TestPlatformAdminMagicLinkPageRejectsMissingToken(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(HTTPOptions{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/auth/verify", nil))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "does not include a verification token") {
		t.Errorf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func cookieByName(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestAuthenticatedTenantManagementAndSessionRoutes(t *testing.T) {
	t.Parallel()

	admin := Admin{ID: "admin-id", Email: "admin@example.com", Permissions: auth.PlatformPermissionStrings()}
	authStore := &fakeAuthStore{admin: admin}
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	tenantStore := &fakeTenantStore{record: TenantRecord{
		ID: "tenant-id", Name: "Acme", Slug: "acme", Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
	}}
	handler := NewHTTPHandler(HTTPOptions{
		Auth: Service{Store: authStore, JWT: jwtManager}, Tenants: tenantStore, JWT: jwtManager,
		RefreshTTL: 30 * 24 * time.Hour,
	})
	accessToken, _, _ := jwtManager.Issue(admin.ID, auth.AudiencePlatform, admin.Permissions)

	for _, path := range []string{"/admin/tenants", "/admin/tenants/tenant-id"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+accessToken)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, body = %s", path, recorder.Code, recorder.Body.String())
		}
	}

	update := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPatch, "/admin/tenants/tenant-id", strings.NewReader(
		`{"name":"Beta","slug":"beta","plan":"scale","status":"suspended","billing_email":"billing@example.com"}`,
	))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(update, request)
	if update.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body = %s", update.Code, update.Body.String())
	}
	if tenantStore.updated.Name != "Beta" || tenantStore.updated.Status != tenant.StatusSuspended || tenantStore.updated.Plan != tenant.PlanScale {
		t.Errorf("updated tenant = %#v", tenantStore.updated)
	}

	deleted := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/admin/tenants/tenant-id", nil)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(deleted, request)
	if deleted.Code != http.StatusNoContent || tenantStore.deletedID != "tenant-id" {
		t.Errorf("DELETE status = %d, deleted ID = %q", deleted.Code, tenantStore.deletedID)
	}

	refreshed := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/admin/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: platformRefreshCookie, Value: "refresh-token"})
	handler.ServeHTTP(refreshed, request)
	if refreshed.Code != http.StatusOK || authStore.currentRefreshHash != auth.HashToken("refresh-token") {
		t.Errorf("refresh status = %d, current hash = %q", refreshed.Code, authStore.currentRefreshHash)
	}

	loggedOut := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/admin/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: platformRefreshCookie, Value: "refresh-token"})
	handler.ServeHTTP(loggedOut, request)
	if loggedOut.Code != http.StatusNoContent || authStore.revokedHash != auth.HashToken("refresh-token") {
		t.Errorf("logout status = %d, revoked hash = %q", loggedOut.Code, authStore.revokedHash)
	}
	operationsCookie := cookieByName(loggedOut.Result().Cookies(), PlatformOperationsAccessCookie)
	if operationsCookie == nil || operationsCookie.Path != "/admin/operations" ||
		operationsCookie.MaxAge != -1 {
		t.Errorf("cleared operations access cookie = %#v", operationsCookie)
	}
}

func TestTenantManagementMapsDomainErrors(t *testing.T) {
	t.Parallel()

	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	accessToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, auth.PlatformPermissionStrings())
	store := &fakeTenantStore{createErr: ErrSlugConflict, getErr: ErrTenantNotFound, deleteErr: ErrTenantNotFound}
	handler := NewHTTPHandler(HTTPOptions{Tenants: store, JWT: jwtManager})

	tests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{method: http.MethodPost, path: "/admin/tenants", body: `{"name":"Acme","slug":"acme","status":"active","owner_email":"owner@acme.test"}`, status: http.StatusConflict},
		{method: http.MethodGet, path: "/admin/tenants/missing", status: http.StatusNotFound},
		{method: http.MethodDelete, path: "/admin/tenants/missing", status: http.StatusNotFound},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		request.Header.Set("Authorization", "Bearer "+accessToken)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tt.status {
			t.Errorf("%s %s status = %d, want %d; body = %s", tt.method, tt.path, recorder.Code, tt.status, recorder.Body.String())
		}
	}
}

func TestPlatformAdminRecoveryRequiresEvidenceAndPassesActor(t *testing.T) {
	t.Parallel()

	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	accessToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantUpdate),
	})
	store := &fakeTenantStore{}
	handler := NewHTTPHandler(HTTPOptions{Tenants: store, JWT: jwtManager})
	tenantID := "d0f62b82-8347-45e6-8f35-b0cc4114a75c"
	targetID := "44bbb4e5-7cf5-4550-8f42-d68688bc41c8"

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost, "/admin/tenants/"+tenantID+"/ownership/recover",
		strings.NewReader(`{"target_member_id":"`+targetID+`","confirmation":"acme","verification_reference":"support-2481","reason":"Verified billing email and incorporation record."}`),
	)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"recovered":true`) {
		t.Fatalf("recovery status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if store.recovered.TenantID != tenantID || store.recovered.TargetMemberID != targetID ||
		store.recovered.ActorPlatformAdminID != "admin-id" || store.recovered.VerificationReference != "support-2481" {
		t.Fatalf("recovery input = %#v", store.recovered)
	}
}

func TestPlatformOwnershipRecoveryErrorsHaveStableHTTPContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid confirmation", err: ErrOwnershipConfirmation, status: http.StatusUnprocessableEntity, code: "invalid_ownership_confirmation"},
		{name: "recent authentication", err: ErrRecentAuthenticationRequired, status: http.StatusForbidden, code: "recent_authentication_required"},
		{name: "invalid target", err: ErrOwnershipTargetInvalid, status: http.StatusUnprocessableEntity, code: "invalid_ownership_target"},
		{name: "invalid evidence", err: ErrRecoveryVerificationInvalid, status: http.StatusUnprocessableEntity, code: "validation_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			writeTenantError(response, test.err)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status = %d body = %s, want %d/%s", response.Code, response.Body.String(), test.status, test.code)
			}
		})
	}
}

func TestPlatformOwnershipRecoveryRejectsInvalidRequestsAndMapsStoreErrors(t *testing.T) {
	t.Parallel()

	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	accessToken, _, _ := jwtManager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantUpdate),
	})
	tenantID := "d0f62b82-8347-45e6-8f35-b0cc4114a75c"
	targetID := "44bbb4e5-7cf5-4550-8f42-d68688bc41c8"
	validBody := `{"target_member_id":"` + targetID + `","confirmation":"acme","verification_reference":"support-2481","reason":"Verified billing email and incorporation record."}`

	store := &fakeTenantStore{}
	handler := NewHTTPHandler(HTTPOptions{Tenants: store, JWT: jwtManager})
	for _, test := range []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{name: "malformed body", path: "/admin/tenants/" + tenantID + "/ownership/recover", body: `{`, status: http.StatusBadRequest},
		{name: "invalid tenant", path: "/admin/tenants/not-a-uuid/ownership/recover", body: validBody, status: http.StatusUnprocessableEntity},
		{name: "invalid evidence", path: "/admin/tenants/" + tenantID + "/ownership/recover", body: `{"target_member_id":"bad","confirmation":"acme","verification_reference":"x","reason":"short"}`, status: http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+accessToken)
			handler.ServeHTTP(response, request)
			if response.Code != test.status || store.recovered.TenantID != "" {
				t.Fatalf("status = %d body = %s recovered = %#v", response.Code, response.Body.String(), store.recovered)
			}
		})
	}

	store.recoverErr = ErrRecentAuthenticationRequired
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/tenants/"+tenantID+"/ownership/recover", strings.NewReader(validBody))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"recent_authentication_required"`) {
		t.Fatalf("store recovery error status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestInvalidAuthAndRequestBodiesFailClosed(t *testing.T) {
	t.Parallel()

	store := &fakeAuthStore{consumeErr: ErrMagicLinkInvalid, rotateErr: ErrRefreshReuse}
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("s", 32), DefaultIssuer, 10*time.Minute)
	handler := NewHTTPHandler(HTTPOptions{Auth: Service{Store: store, JWT: jwtManager}, JWT: jwtManager})

	tests := []struct {
		path   string
		body   string
		cookie bool
		status int
	}{
		{path: "/admin/auth/login", body: `{"email":"invalid"}`, status: http.StatusUnprocessableEntity},
		{path: "/admin/auth/login", body: `{"email":"admin@example.com"} {}`, status: http.StatusBadRequest},
		{path: "/admin/auth/verify", body: `{"token":"invalid"}`, status: http.StatusUnauthorized},
		{path: "/admin/auth/refresh", cookie: true, status: http.StatusUnauthorized},
		{path: "/admin/auth/refresh", status: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
		if tt.cookie {
			request.AddCookie(&http.Cookie{Name: platformRefreshCookie, Value: "reused"})
		}
		handler.ServeHTTP(recorder, request)
		if recorder.Code != tt.status {
			t.Errorf("POST %s status = %d, want %d; body = %s", tt.path, recorder.Code, tt.status, recorder.Body.String())
		}
	}
}

type fakeTenantStore struct {
	created    CreateTenantParams
	record     TenantRecord
	updated    TenantRecord
	deletedID  string
	createErr  error
	getErr     error
	deleteErr  error
	recovered  RecoverOwnershipParams
	recoverErr error
}

type fakeBackofficeStore struct {
	overview         BackofficeOverview
	tenantPage       BackofficeTenantPage
	tenantList       BackofficeTenantListParams
	tenantListCalled bool
	detail           BackofficeTenantDetail
	detailCalled     bool
	auditPage        BackofficeAccessEventPage
	err              error
}

func (s *fakeBackofficeStore) Overview(context.Context) (BackofficeOverview, error) {
	return s.overview, s.err
}

func (s *fakeBackofficeStore) ListBackofficeTenants(_ context.Context, params BackofficeTenantListParams) (BackofficeTenantPage, error) {
	s.tenantListCalled = true
	s.tenantList = params
	return s.tenantPage, s.err
}
func (s *fakeBackofficeStore) GetBackofficeTenant(_ context.Context, _ string) (BackofficeTenantDetail, error) {
	s.detailCalled = true
	return s.detail, s.err
}
func (s *fakeBackofficeStore) ListBackofficeAccessEvents(_ context.Context, _ string, _ int) (BackofficeAccessEventPage, error) {
	return s.auditPage, s.err
}

func (s *fakeTenantStore) CreateTenant(_ context.Context, input CreateTenantParams) (TenantRecord, error) {
	s.created = input
	if s.createErr != nil {
		return TenantRecord{}, s.createErr
	}
	return TenantRecord{
		ID: "tenant-id", Name: input.Name, Slug: input.Slug, Plan: input.Plan,
		Status: input.Status, TrialEndsAt: input.TrialEndsAt,
	}, nil
}

func (s *fakeTenantStore) GetTenant(context.Context, string) (TenantRecord, error) {
	return s.record, s.getErr
}

func (s *fakeTenantStore) ListTenants(context.Context) ([]TenantRecord, error) {
	return []TenantRecord{s.record}, nil
}

func (s *fakeTenantStore) UpdateTenant(_ context.Context, input TenantRecord) (TenantRecord, error) {
	s.updated = input
	return input, nil
}

func (s *fakeTenantStore) DeleteTenant(_ context.Context, id string) error {
	s.deletedID = id
	return s.deleteErr
}

func (s *fakeTenantStore) RecoverOwnership(_ context.Context, input RecoverOwnershipParams) error {
	s.recovered = input
	return s.recoverErr
}
