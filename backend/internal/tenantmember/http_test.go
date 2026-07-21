package tenantmember

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTenantRefreshCookieSupportsTopLevelProviderReturns(t *testing.T) {
	handler := HTTPHandler{options: HTTPOptions{
		SecureCookies: true,
		RefreshTTL:    30 * 24 * time.Hour,
	}}

	issued := httptest.NewRecorder()
	handler.setCookie(issued, "refresh-token")
	assertTenantRefreshCookie(t, responseCookie(t, issued, tenantRefreshCookie), 30*24*time.Hour)

	cleared := httptest.NewRecorder()
	handler.clearCookie(cleared)
	assertTenantRefreshCookie(t, responseCookie(t, cleared, tenantRefreshCookie), -time.Second)
}

func assertTenantRefreshCookie(t *testing.T, cookie *http.Cookie, expectedTTL time.Duration) {
	t.Helper()
	if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("tenant refresh cookie security attributes = %#v", cookie)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("tenant refresh cookie SameSite = %v, want Lax for provider return navigation", cookie.SameSite)
	}
	if expectedTTL > 0 && cookie.MaxAge != int(expectedTTL.Seconds()) {
		t.Fatalf("tenant refresh cookie MaxAge = %d, want %d", cookie.MaxAge, int(expectedTTL.Seconds()))
	}
	if expectedTTL < 0 && cookie.MaxAge != -1 {
		t.Fatalf("cleared tenant refresh cookie MaxAge = %d, want -1", cookie.MaxAge)
	}
}
