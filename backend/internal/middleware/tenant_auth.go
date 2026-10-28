package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type tenantClaimsContextKey struct{}

var (
	ErrTenantCredentialInvalid   = errors.New("tenant credential is invalid")
	ErrTenantCredentialForbidden = errors.New("tenant credential belongs to another tenant")
	// ErrTenantTwoFactorRequired is returned for a member session in a
	// workspace whose two-factor policy is required while that member has no
	// confirmed enrolment. The session keeps its identity but reaches nothing
	// beyond the enrolment endpoints until it completes setup.
	ErrTenantTwoFactorRequired = errors.New("tenant member must complete two-factor enrollment")
)

type TenantCredentialVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

// RequireTenantMember authenticates a tenant credential and, for member
// sessions, enforces the workspace two-factor policy. Every tenant route is
// registered through it, so a workspace that requires two-factor authentication
// is closed to unenrolled members by construction rather than route by route.
func RequireTenantMember(verifier accessTokenVerifier) func(http.Handler) http.Handler {
	return requireTenantMember(verifier, true)
}

// RequireTenantMemberForTwoFactorSetup authenticates the same credential but
// skips the policy gate, so a member caught by a newly required policy can
// still reach the enrolment endpoints. It must never guard a route that
// exposes workspace data.
func RequireTenantMemberForTwoFactorSetup(verifier accessTokenVerifier) func(http.Handler) http.Handler {
	return requireTenantMember(verifier, false)
}

func requireTenantMember(verifier accessTokenVerifier, enforceTwoFactor bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resolved, ok := TenantFromContext(r.Context())
			if !ok {
				api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
				return
			}
			query, _ := QuerierFromContext(r.Context())
			claims, err := authenticateTenantCredential(
				r.Context(), query, verifier, resolved.ID, r.Header.Get("Authorization"),
				resolved.DeletionRequestedAt == nil, enforceTwoFactor,
			)
			if WriteTenantAPIKeyRateLimitError(w, r.Context(), err) {
				return
			}
			if errors.Is(err, ErrTenantCredentialInvalid) {
				api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "authentication required", nil)
				return
			}
			if err != nil {
				if errors.Is(err, ErrTenantTwoFactorRequired) {
					WriteTwoFactorEnrollmentRequired(w)
					return
				}
				if errors.Is(err, ErrTenantCredentialForbidden) {
					api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "tenant access denied", nil)
					return
				}
				api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
				return
			}
			WriteTenantAPIKeyRateLimitHeaders(w, r.Context())
			ctx := context.WithValue(r.Context(), tenantClaimsContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WriteTwoFactorEnrollmentRequired is the single response shape for a session
// that is authenticated but not yet compliant, so every surface routes the
// member into the same enrolment flow.
func WriteTwoFactorEnrollmentRequired(w http.ResponseWriter) {
	api.WriteError(
		w, http.StatusForbidden, api.CodeTwoFactorEnrollmentRequired,
		"two-factor enrollment is required", nil,
	)
}

func AuthenticateTenantCredential(
	ctx context.Context,
	query db.Querier,
	verifier TenantCredentialVerifier,
	tenantID string,
	authorization string,
	recordUsage bool,
) (auth.Claims, error) {
	return authenticateTenantCredential(ctx, query, verifier, tenantID, authorization, recordUsage, true)
}

func authenticateTenantCredential(
	ctx context.Context,
	query db.Querier,
	verifier TenantCredentialVerifier,
	tenantID string,
	authorization string,
	recordUsage bool,
	enforceTwoFactor bool,
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
		var rateLimitPerMin int
		statement := `
			SELECT id, permissions, rate_limit_per_min FROM api_keys
			WHERE tenant_id = $1 AND token_hash = $2
			  AND (expires_at IS NULL OR expires_at > now())
		`
		if recordUsage {
			statement = `
				UPDATE api_keys SET last_used_at = now()
				WHERE tenant_id = $1 AND token_hash = $2
				  AND (expires_at IS NULL OR expires_at > now())
				RETURNING id, permissions, rate_limit_per_min
			`
		}
		err := query.QueryRow(ctx, statement, tenantID, auth.HashToken(raw)).Scan(
			&keyID, &permissions, &rateLimitPerMin,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Claims{}, ErrTenantCredentialInvalid
		}
		if err != nil {
			return auth.Claims{}, fmt.Errorf("authenticate tenant API key: %w", err)
		}
		if err := enforceTenantAPIKeyRateLimit(ctx, tenantID, keyID, rateLimitPerMin); err != nil {
			return auth.Claims{}, err
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
	var twoFactorRequired bool
	var twoFactorEnrolled bool
	err = query.QueryRow(ctx, `
		SELECT member.permissions, member.status, member.authorization_version,
		       tenant.two_factor_policy = 'required',
		       EXISTS (
		           SELECT 1 FROM tenant_member_totp AS enrolment
		           WHERE enrolment.tenant_id = member.tenant_id
		             AND enrolment.member_id = member.id
		             AND enrolment.confirmed_at IS NOT NULL
		       )
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		WHERE member.tenant_id = $1 AND member.id = $2
	`, tenantID, claims.Subject).Scan(
		&permissions, &status, &authorizationVersion, &twoFactorRequired, &twoFactorEnrolled,
	)
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
	claims.TwoFactorRequired = twoFactorRequired
	claims.TwoFactorEnrolled = twoFactorEnrolled
	if enforceTwoFactor && twoFactorRequired && !twoFactorEnrolled {
		return auth.Claims{}, ErrTenantTwoFactorRequired
	}
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

// RequireTenantAPIKey keeps service-to-service routes from accepting a human
// bearer even when that member has the same permission set.
func RequireTenantAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := TenantClaimsFromContext(r.Context())
		if !ok {
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}
		if !claims.IsAPIKey {
			api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "API key authentication required", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
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
