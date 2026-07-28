package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/netip"
	"strings"
)

const (
	ProxyHostHeader   = "X-Fidibeki-Proxy-Host"
	ProxySecretHeader = "X-Fidibeki-Proxy-Secret"
	// BackofficeClientIPHeader is set by the isolated backoffice proxy. It is
	// intentionally promoted to request context only after proxy authentication.
	BackofficeClientIPHeader = "X-Fidibeki-Backoffice-Client-IP"
)

type backofficeClientIPContextKey struct{}

// BackofficeClientIP returns the client address asserted by the authenticated
// internal backoffice proxy. A browser-supplied header is never trusted.
func BackofficeClientIP(r *http.Request) (string, bool) {
	ip, ok := r.Context().Value(backofficeClientIPContextKey{}).(string)
	return ip, ok
}

// TrustedProxyHost restores the public host after a server-side web proxy has
// routed a request to the API's private service address.
func TrustedProxyHost(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			forwardedHost := strings.TrimSpace(r.Header.Get(ProxyHostHeader))
			providedSecret := r.Header.Get(ProxySecretHeader)
			request := r.Clone(r.Context())
			request.Header.Del(ProxyHostHeader)
			request.Header.Del(ProxySecretHeader)
			clientIP := strings.TrimSpace(request.Header.Get(BackofficeClientIPHeader))
			request.Header.Del(BackofficeClientIPHeader)

			if forwardedHost == "" && providedSecret == "" {
				next.ServeHTTP(w, request)
				return
			}
			trusted := forwardedHost != "" && secret != "" &&
				subtle.ConstantTimeCompare([]byte(providedSecret), []byte(secret)) == 1
			if trusted {
				request.Host = forwardedHost
				if address, err := netip.ParseAddr(clientIP); err == nil {
					request = request.WithContext(context.WithValue(request.Context(), backofficeClientIPContextKey{}, address.Unmap().String()))
				}
			}
			next.ServeHTTP(w, request)
		})
	}
}
