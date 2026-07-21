package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

func TestRequireTenantMemberRejectsCrossTenantAndPlatformTokens(t *testing.T) {
	t.Parallel()

	manager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	valid, _, _ := manager.IssueTenant("member-id", "tenant-id", []string{"project:read"})
	otherTenant, _, _ := manager.IssueTenant("member-id", "other-tenant", []string{"project:read"})
	platform, _, _ := manager.Issue("admin-id", auth.AudiencePlatform, []string{"project:read"})

	tests := []struct {
		name       string
		token      string
		permission auth.Permission
		wantStatus int
	}{
		{name: "valid", token: valid, permission: auth.PermissionProjectRead, wantStatus: http.StatusNoContent},
		{name: "missing permission", token: valid, permission: auth.PermissionProjectUpdate, wantStatus: http.StatusForbidden},
		{name: "cross tenant", token: otherTenant, permission: auth.PermissionProjectRead, wantStatus: http.StatusForbidden},
		{name: "platform audience", token: platform, permission: auth.PermissionProjectRead, wantStatus: http.StatusUnauthorized},
		{name: "missing token", permission: auth.PermissionProjectRead, wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeTransaction{}
			resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
				BaseDomain: "localhost",
				Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
					return tenant.Tenant{ID: "tenant-id", Slug: "acme", Status: tenant.StatusActive}, nil
				}),
				Begin: func(context.Context) (db.Transaction, error) { return tx, nil },
			})
			handler := resolve(appmiddleware.RequireTenantMember(manager)(
				appmiddleware.RequirePermission(tt.permission)(http.HandlerFunc(
					func(w http.ResponseWriter, r *http.Request) {
						claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
						if !ok || claims.Subject != "member-id" {
							t.Errorf("claims = %#v, present = %t", claims, ok)
						}
						w.WriteHeader(http.StatusNoContent)
					},
				)),
			))
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://acme.localhost/v1/projects", nil)
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}
