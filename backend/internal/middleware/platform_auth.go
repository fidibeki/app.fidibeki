package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
)

type accessTokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

type platformClaimsContextKey struct{}

func RequirePlatformAdmin(
	verifier accessTokenVerifier,
	required auth.Permission,
) func(http.Handler) http.Handler {
	return requirePlatformAdmin(verifier, required, func(r *http.Request) (string, error) {
		return bearerToken(r.Header.Get("Authorization"))
	})
}

func RequirePlatformAdminCookie(
	verifier accessTokenVerifier,
	cookieName string,
	required auth.Permission,
) func(http.Handler) http.Handler {
	return requirePlatformAdmin(verifier, required, func(r *http.Request) (string, error) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			return "", errors.New("invalid platform access cookie")
		}
		return cookie.Value, nil
	})
}

func requirePlatformAdmin(
	verifier accessTokenVerifier,
	required auth.Permission,
	token func(*http.Request) (string, error),
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, err := token(r)
			if err != nil {
				api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "authentication required", nil)
				return
			}
			claims, err := verifier.Verify(raw, auth.AudiencePlatform)
			if err != nil {
				api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "authentication required", nil)
				return
			}
			if required != "" && !auth.HasPermission(claims.Permissions, required) {
				api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "permission denied", nil)
				return
			}
			ctx := context.WithValue(r.Context(), platformClaimsContextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func PlatformClaimsFromContext(ctx context.Context) (auth.Claims, bool) {
	claims, ok := ctx.Value(platformClaimsContextKey{}).(auth.Claims)
	return claims, ok
}

func bearerToken(header string) (string, error) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", errors.New("invalid bearer token")
	}
	return parts[1], nil
}
