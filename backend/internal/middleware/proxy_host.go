package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const (
	ProxyHostHeader   = "X-Fidibeki-Proxy-Host"
	ProxySecretHeader = "X-Fidibeki-Proxy-Secret"
)

// TrustedProxyHost restores the public host after a server-side web proxy has
// routed a request to the API's private service address.
func TrustedProxyHost(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			forwardedHost := strings.TrimSpace(r.Header.Get(ProxyHostHeader))
			providedSecret := r.Header.Get(ProxySecretHeader)

			if forwardedHost == "" && providedSecret == "" {
				next.ServeHTTP(w, r)
				return
			}

			request := r.Clone(r.Context())
			request.Header.Del(ProxyHostHeader)
			request.Header.Del(ProxySecretHeader)
			if forwardedHost != "" && secret != "" &&
				subtle.ConstantTimeCompare([]byte(providedSecret), []byte(secret)) == 1 {
				request.Host = forwardedHost
			}
			next.ServeHTTP(w, request)
		})
	}
}
