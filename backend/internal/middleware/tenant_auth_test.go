package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

func TestRequireTenantMemberRejectsCrossTenantAndPlatformTokens(t *testing.T) {
	t.Parallel()

	manager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	valid, _, _ := manager.IssueTenant("member-id", "tenant-id", []string{"project:read"})
	otherTenant, _, _ := manager.IssueTenant("member-id", "other-tenant", []string{"project:read"})
	platform, _, _ := manager.Issue("admin-id", auth.AudiencePlatform, []string{"project:read"})
	now := time.Now()
	legacyToken, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, auth.Claims{
		Permissions: []string{"project:read"}, TenantID: "tenant-id",
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer: "fidibeki", Subject: "member-id", Audience: jwtlib.ClaimStrings{auth.AudienceTenant},
			IssuedAt: jwtlib.NewNumericDate(now), NotBefore: jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(10 * time.Minute)),
		},
	}).SignedString([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatalf("sign legacy tenant token: %v", err)
	}

	tests := []struct {
		name          string
		token         string
		permission    auth.Permission
		memberVersion int
		wantStatus    int
	}{
		{name: "valid", token: valid, permission: auth.PermissionProjectRead, wantStatus: http.StatusNoContent},
		{name: "legacy version zero", token: legacyToken, permission: auth.PermissionProjectRead, wantStatus: http.StatusNoContent},
		{name: "legacy token after authority change", token: legacyToken, permission: auth.PermissionProjectRead, memberVersion: 1, wantStatus: http.StatusUnauthorized},
		{name: "missing permission", token: valid, permission: auth.PermissionProjectUpdate, wantStatus: http.StatusForbidden},
		{name: "cross tenant", token: otherTenant, permission: auth.PermissionProjectRead, wantStatus: http.StatusForbidden},
		{name: "platform audience", token: platform, permission: auth.PermissionProjectRead, wantStatus: http.StatusUnauthorized},
		{name: "missing token", permission: auth.PermissionProjectRead, wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tx := &fakeTransaction{memberAuthVersion: tt.memberVersion}
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

func TestRequireAnyPermission(t *testing.T) {
	t.Parallel()

	manager, _ := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", 10*time.Minute)
	token, _, _ := manager.IssueTenant("member-id", "tenant-id", []string{"project:read"})
	resolve := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost",
		Resolver: tenant.ResolverFunc(func(context.Context, string) (tenant.Tenant, error) {
			return tenant.Tenant{ID: "tenant-id", Slug: "acme", Status: tenant.StatusActive}, nil
		}),
		Begin: func(context.Context) (db.Transaction, error) { return &fakeTransaction{}, nil },
	})

	for _, test := range []struct {
		name       string
		required   []auth.Permission
		wantStatus int
	}{
		{
			name: "one required permission matches", required: []auth.Permission{
				auth.PermissionProjectUpdate, auth.PermissionProjectRead,
			}, wantStatus: http.StatusNoContent,
		},
		{
			name: "no required permission matches", required: []auth.Permission{
				auth.PermissionProjectUpdate, auth.PermissionProjectDelete,
			}, wantStatus: http.StatusForbidden,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := resolve(appmiddleware.RequireTenantMember(manager)(
				appmiddleware.RequireAnyPermission(test.required...)(http.HandlerFunc(
					func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
				)),
			))
			request := httptest.NewRequest(http.MethodGet, "http://acme.localhost/v1/projects", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}

	response := httptest.NewRecorder()
	appmiddleware.RequireAnyPermission(auth.PermissionProjectRead)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
	)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/projects", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("missing claims status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
