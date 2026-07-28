package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
)

func TestStrictTransportSecurityAddsTheExactPolicy(t *testing.T) {
	t.Parallel()

	handler := appmiddleware.StrictTransportSecurity(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		},
	))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/error", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Header().Get("Strict-Transport-Security"); got != appmiddleware.StrictTransportSecurityPolicy {
		t.Errorf(
			"Strict-Transport-Security = %q, want %q",
			got,
			appmiddleware.StrictTransportSecurityPolicy,
		)
	}
}
