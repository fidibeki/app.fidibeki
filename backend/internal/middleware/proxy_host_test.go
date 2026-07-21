package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
)

func TestTrustedProxyHostRequiresTheSharedSecretAndStripsInternalHeaders(t *testing.T) {
	t.Parallel()

	const secret = "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name       string
		configured string
		provided   string
		wantHost   string
	}{
		{name: "authenticated", configured: secret, provided: secret, wantHost: "acme.localhost:4332"},
		{name: "wrong secret", configured: secret, provided: "wrong", wantHost: "api:8080"},
		{name: "proxy disabled", provided: secret, wantHost: "api:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotHost, gotForwarded, gotSecret string
			handler := appmiddleware.TrustedProxyHost(tt.configured)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				gotHost = r.Host
				gotForwarded = r.Header.Get(appmiddleware.ProxyHostHeader)
				gotSecret = r.Header.Get(appmiddleware.ProxySecretHeader)
			}))
			request := httptest.NewRequest(http.MethodGet, "http://api:8080/v1/config", nil)
			request.Header.Set(appmiddleware.ProxyHostHeader, "acme.localhost:4332")
			request.Header.Set(appmiddleware.ProxySecretHeader, tt.provided)
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if gotHost != tt.wantHost {
				t.Errorf("host = %q, want %q", gotHost, tt.wantHost)
			}
			if gotForwarded != "" || gotSecret != "" {
				t.Errorf("internal headers reached handler: host=%q secret=%q", gotForwarded, gotSecret)
			}
		})
	}
}
