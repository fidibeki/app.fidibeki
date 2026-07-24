package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type tenantClaimsContextKey struct{}

var (
	ErrTenantCredentialInvalid   = errors.New("tenant credential is invalid")
	ErrTenantCredentialForbidden = errors.New("tenant credential belongs to another tenant")
)

type TenantCredentialVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

func RequireTenantMember(verifier accessTokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resolved, ok := TenantFromContext(r.Context())
			if !ok {
				api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
				return
			}
			query, _ := QuerierFromContext(r.Context())
			claims, err := AuthenticateTenantCredential(
				r.Context(), query, verifier, resolved.ID, r.Header.Get("Authorization"),
				resolved.DeletionRequestedAt == nil,
			)
			if errors.Is(err, ErrTenantCredentialInvalid) {
				api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "authentication required", nil)
				return
			}
			if err != nil {
				if errors.Is(err, ErrTenantCredentialForbidden) {
					api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "tenant access denied", nil)
					return
				}
				api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
				return
			}
			ctx := context.WithValue(r.Context(), tenantClaimsContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AuthenticateTenantCredential(
	ctx context.Context,
	query db.Querier,
	verifier TenantCredentialVerifier,
	tenantID string,
	authorization string,
	recordUsage bool,
) (auth.Claims, error) {
	raw, err := bearerToken(authorization)
	if err != nil {
		return auth.Claims{}, ErrTenantCredentialInvalid
	}
	if strings.HasPrefix(raw, "fk_live_") {
		if !validAPIKeyToken(raw) || query == nil {
			return auth.Claims{}, ErrTenantCredentialInvalid
		}
		var keyID string
		var permissions []string
		statement := `
			SELECT id, permissions FROM api_keys
			WHERE tenant_id = $1 AND token_hash = $2
			  AND (expires_at IS NULL OR expires_at > now())
		`
		if recordUsage {
			statement = `
				UPDATE api_keys SET last_used_at = now()
				WHERE tenant_id = $1 AND token_hash = $2
				  AND (expires_at IS NULL OR expires_at > now())
				RETURNING id, permissions
			`
		}
		err := query.QueryRow(ctx, statement, tenantID, auth.HashToken(raw)).Scan(&keyID, &permissions)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Claims{}, ErrTenantCredentialInvalid
		}
		if err != nil {
			return auth.Claims{}, fmt.Errorf("authenticate tenant API key: %w", err)
		}
		claims := auth.Claims{TenantID: tenantID, Permissions: permissions, IsAPIKey: true}
		claims.Subject = keyID
		return claims, nil
	}
	claims, err := verifier.Verify(raw, auth.AudienceTenant)
	if err != nil {
		return auth.Claims{}, ErrTenantCredentialInvalid
	}
	if claims.TenantID == "" || claims.TenantID != tenantID {
		return auth.Claims{}, ErrTenantCredentialForbidden
	}
	if query == nil {
		return auth.Claims{}, ErrTenantCredentialInvalid
	}
	expectedAuthorizationVersion := 0
	if claims.AuthorizationVersion != nil {
		expectedAuthorizationVersion = *claims.AuthorizationVersion
	}
	var permissions []string
	var status string
	var authorizationVersion int
	err = query.QueryRow(ctx, `
		SELECT permissions, status, authorization_version
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, claims.Subject).Scan(&permissions, &status, &authorizationVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Claims{}, ErrTenantCredentialInvalid
	}
	if err != nil {
		return auth.Claims{}, fmt.Errorf("authenticate tenant member: %w", err)
	}
	if status != "active" || authorizationVersion != expectedAuthorizationVersion {
		return auth.Claims{}, ErrTenantCredentialInvalid
	}
	claims.Permissions = permissions
	return claims, nil
}

func validAPIKeyToken(raw string) bool {
	if len(raw) != 40 || !strings.HasPrefix(raw, "fk_live_") {
		return false
	}
	for _, character := range raw[len("fk_live_"):] {
		if character >= '0' && character <= '9' ||
			character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' {
			continue
		}
		return false
	}
	return true
}

func RequirePermission(required auth.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := TenantClaimsFromContext(r.Context())
			if !ok {
				api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
				return
			}
			if !auth.HasPermission(claims.Permissions, required) {
				api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "permission denied", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAnyPermission(required ...auth.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := TenantClaimsFromContext(r.Context())
			if !ok {
				api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
				return
			}
			for _, permission := range required {
				if auth.HasPermission(claims.Permissions, permission) {
					next.ServeHTTP(w, r)
					return
				}
			}
			api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "permission denied", nil)
		})
	}
}

func TenantClaimsFromContext(ctx context.Context) (auth.Claims, bool) {
	claims, ok := ctx.Value(tenantClaimsContextKey{}).(auth.Claims)
	return claims, ok
}
