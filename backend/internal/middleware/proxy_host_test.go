package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
)

func TestTrustedProxyHostRequiresTheSharedSecretAndStripsInternalHeaders(t *testing.T) {
	t.Parallel()

	const secret = "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name       string
		configured string
		provided   string
		wantHost   string
		wantIP     string
	}{
		{name: "authenticated", configured: secret, provided: secret, wantHost: "acme.localhost:4332", wantIP: "203.0.113.5"},
		{name: "wrong secret", configured: secret, provided: "wrong", wantHost: "api:8080"},
		{name: "proxy disabled", provided: secret, wantHost: "api:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotHost, gotForwarded, gotSecret, gotClientIP, gotClientIPHeader string
			handler := appmiddleware.TrustedProxyHost(tt.configured)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				gotHost = r.Host
				gotForwarded = r.Header.Get(appmiddleware.ProxyHostHeader)
				gotSecret = r.Header.Get(appmiddleware.ProxySecretHeader)
				gotClientIPHeader = r.Header.Get(appmiddleware.BackofficeClientIPHeader)
				gotClientIP, _ = appmiddleware.BackofficeClientIP(r)
			}))
			request := httptest.NewRequest(http.MethodGet, "http://api:8080/v1/config", nil)
			request.Header.Set(appmiddleware.ProxyHostHeader, "acme.localhost:4332")
			request.Header.Set(appmiddleware.ProxySecretHeader, tt.provided)
			request.Header.Set(appmiddleware.BackofficeClientIPHeader, "203.0.113.5")
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if gotHost != tt.wantHost {
				t.Errorf("host = %q, want %q", gotHost, tt.wantHost)
			}
			if gotForwarded != "" || gotSecret != "" || gotClientIPHeader != "" {
				t.Errorf("internal headers reached handler: host=%q secret=%q client_ip=%q", gotForwarded, gotSecret, gotClientIPHeader)
			}
			if gotClientIP != tt.wantIP {
				t.Errorf("trusted client IP = %q, want %q", gotClientIP, tt.wantIP)
			}
		})
	}
}

func TestTrustedProxyHostStripsBackofficeClientIPWithoutProxyHeaders(t *testing.T) {
	t.Parallel()

	var rawHeader string
	handler := appmiddleware.TrustedProxyHost("configured-secret")(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		rawHeader = r.Header.Get(appmiddleware.BackofficeClientIPHeader)
		if _, ok := appmiddleware.BackofficeClientIP(r); ok {
			t.Error("header-only request was treated as a trusted proxy assertion")
		}
	}))
	request := httptest.NewRequest(http.MethodGet, "http://api:8080/v1/config", nil)
	request.Header.Set(appmiddleware.BackofficeClientIPHeader, "203.0.113.5")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if rawHeader != "" {
		t.Errorf("untrusted client IP header reached handler: %q", rawHeader)
	}
}
