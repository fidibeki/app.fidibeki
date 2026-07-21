package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
)

func TestRequirePlatformAdminEnforcesAudienceAndPermission(t *testing.T) {
	t.Parallel()

	manager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	allowedToken, _, _ := manager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantRead),
	})
	wrongPermissionToken, _, _ := manager.Issue("admin-id", auth.AudiencePlatform, nil)
	wrongAudienceToken, _, _ := manager.Issue("member-id", auth.AudienceTenant, []string{
		string(auth.PermissionPlatformTenantRead),
	})
	handler := appmiddleware.RequirePlatformAdmin(manager, auth.PermissionPlatformTenantRead)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
			if !ok || claims.Subject != "admin-id" {
				t.Errorf("claims = %#v, present = %t", claims, ok)
			}
			w.WriteHeader(http.StatusNoContent)
		},
	))

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "valid", header: "bearer " + allowedToken, wantStatus: http.StatusNoContent},
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "malformed", header: "Bearer", wantStatus: http.StatusUnauthorized},
		{name: "wrong permission", header: "Bearer " + wrongPermissionToken, wantStatus: http.StatusForbidden},
		{name: "wrong audience", header: "Bearer " + wrongAudienceToken, wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/admin/tenants", nil)
			request.Header.Set("Authorization", tt.header)
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestRequirePlatformAdminCookieIsScopedToValidPlatformTokens(t *testing.T) {
	t.Parallel()

	manager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	platformToken, _, _ := manager.Issue("admin-id", auth.AudiencePlatform, []string{
		string(auth.PermissionPlatformTenantRead),
	})
	tenantToken, _, _ := manager.Issue("member-id", auth.AudienceTenant, auth.TenantPermissionStrings())
	handler := appmiddleware.RequirePlatformAdminCookie(
		manager, "platform_access", auth.PermissionPlatformTenantRead,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	tests := []struct {
		name   string
		token  string
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "tenant audience", token: tenantToken, status: http.StatusUnauthorized},
		{name: "platform", token: platformToken, status: http.StatusNoContent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/admin/queues", nil)
			if test.token != "" {
				request.AddCookie(&http.Cookie{Name: "platform_access", Value: test.token})
			}
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Errorf("status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}
