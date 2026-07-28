package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
)

// callbackTest builds the apex callback with no database behind it. Every case here is
// rejected before persistence, so reaching Begin would itself be the failure.
func callbackTest(t *testing.T, oauth SlackOAuth) http.Handler {
	t.Helper()
	router := chi.NewRouter()
	RegisterSlackCallbackRoute(router, SlackCallbackOptions{
		Slack: oauth, BaseDomain: "fidibeki.test",
		WebPublicBaseURL: "https://fidibeki.test",
		Begin: func(context.Context) (db.Transaction, error) {
			t.Fatal("callback reached the database on a request it should have rejected")
			return nil, nil
		},
	})
	return router
}

func TestSlackCallbackRejectsForgedAndReplayedState(t *testing.T) {
	t.Parallel()
	states := newFakeStateStore()
	oauth := testSlackOAuth(&stubDoer{}, states)
	handler := callbackTest(t, oauth)

	// A state token that was never issued.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, SlackCallbackPath+"?code=c&state=forged", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("forged state status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	// No state at all.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, SlackCallbackPath+"?code=c", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("missing state status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	// A genuine token, replayed. The first use is consumed by the exchange attempt; the
	// second must not reach Slack or the database at all.
	token, err := states.Create(context.Background(), InstallState{
		TenantID: "tenant-1", TenantSlug: "acme", MemberID: "member-1",
	}, slackStateTTL)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := states.Consume(context.Background(), token); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, SlackCallbackPath+"?code=c&state="+token, nil))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("replayed state status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestSlackCallbackRedirectsToTheInstallingWorkspace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		query      string
		doer       *stubDoer
		wantResult string
	}{
		{
			// Slack reports a cancelled install as a query parameter on a 200 redirect.
			name: "user cancelled", query: "error=access_denied", wantResult: "denied",
			doer: &stubDoer{},
		},
		{
			// Slack sends a code or an error. Neither is a malformed callback, not a choice
			// the user made, so it must not be reported to them as a cancellation.
			name: "no code and no error", query: "", wantResult: "failed", doer: &stubDoer{},
		},
		{
			// State from before authority-version capture is deliberately
			// consumed and failed without reaching Slack or persistence.
			name: "legacy state has no authority version", query: "code=bad", wantResult: "failed",
			doer: &stubDoer{response: jsonResponse(http.StatusOK, `{"ok":false,"error":"invalid_code"}`)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			states := newFakeStateStore()
			oauth := testSlackOAuth(tt.doer, states)
			handler := callbackTest(t, oauth)
			token, err := states.Create(context.Background(), InstallState{
				TenantID: "tenant-1", TenantSlug: "acme", MemberID: "member-1",
			}, slackStateTTL)
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(
				http.MethodGet, SlackCallbackPath+"?state="+token+"&"+tt.query, nil))

			if recorder.Code != http.StatusFound {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
			}
			location, err := url.Parse(recorder.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse Location: %v", err)
			}
			// The redirect is built from the slug recorded at install time, never from
			// anything in the request, so this cannot become an open redirect.
			if location.Host != "acme.fidibeki.test" {
				t.Errorf("redirect host = %q, want acme.fidibeki.test", location.Host)
			}
			if location.Path != settingsPath {
				t.Errorf("redirect path = %q, want %q", location.Path, settingsPath)
			}
			if got := location.Query().Get("slack"); got != tt.wantResult {
				t.Errorf("slack result = %q, want %q", got, tt.wantResult)
			}
			// A failure reason from Slack must not be reflected into the browser URL.
			if strings.Contains(recorder.Header().Get("Location"), "invalid_code") {
				t.Error("redirect leaked the raw Slack error code")
			}
		})
	}
}
