package tenantmember

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceProtectionErrorsHaveStableHTTPContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "owner protected", err: ErrOwnerProtection, status: http.StatusConflict, code: "owner_protected"},
		{name: "invalid confirmation", err: ErrOwnershipConfirmation, status: http.StatusUnprocessableEntity, code: "invalid_ownership_confirmation"},
		{name: "recent authentication", err: ErrRecentAuthenticationRequired, status: http.StatusForbidden, code: "recent_authentication_required"},
		{name: "invalid target", err: ErrOwnershipTargetInvalid, status: http.StatusUnprocessableEntity, code: "invalid_ownership_target"},
		{name: "last administrator", err: ErrLastAdministrator, status: http.StatusConflict, code: "last_administrator"},
		{name: "already removed", err: ErrMemberAlreadyRemoved, status: http.StatusConflict, code: "member_already_removed"},
		{name: "invalid status", err: ErrMemberStatusInvalid, status: http.StatusUnprocessableEntity, code: "validation_error"},
		{name: "invalid actor", err: ErrMemberActorInvalid, status: http.StatusForbidden, code: "forbidden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			writeError(response, test.err)
			if response.Code != test.status || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status = %d body = %s, want %d/%s", response.Code, response.Body.String(), test.status, test.code)
			}
		})
	}
}

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
