package membernotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// countingQuerier records whether the endpoint touched the database at all,
// which is how the tests prove an invalid token never reaches persistence.
type countingQuerier struct{ writes int }

func (q *countingQuerier) Exec(
	context.Context, string, ...any,
) (pgconn.CommandTag, error) {
	q.writes++
	return pgconn.CommandTag{}, errors.New("database unavailable")
}

func (q *countingQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	q.writes++
	return nil, errors.New("database unavailable")
}

func (q *countingQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	q.writes++
	return errorRow{}
}

type errorRow struct{}

func (errorRow) Scan(...any) error { return errors.New("database unavailable") }

func unsubscribeHandler(t *testing.T, database *countingQuerier) (http.Handler, *auth.JWTManager) {
	t.Helper()
	tokens, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Hour)
	if err != nil {
		t.Fatalf("create token manager: %v", err)
	}
	router := chi.NewRouter()
	RegisterPublicRoutes(router, PublicHTTPOptions{
		Store: Store{}, Database: database, Tokens: tokens,
	})
	return router, tokens
}

func performUnsubscribe(
	handler http.Handler,
	method string,
	token string,
) (*httptest.ResponseRecorder, string) {
	var request *http.Request
	if method == http.MethodPost {
		body := strings.NewReader(url.Values{"token": {token}}.Encode())
		request = httptest.NewRequest(method, UnsubscribePath, body)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		request = httptest.NewRequest(
			method, UnsubscribePath+"?token="+url.QueryEscape(token), nil,
		)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder, recorder.Body.String()
}

// The one-click link must answer identically whatever it is given: a forged,
// expired, or well-formed token for a member that does not exist all produce the
// same page, so the endpoint cannot be used to discover members or workspaces.
func TestUnsubscribeResponseRevealsNothing(t *testing.T) {
	t.Parallel()
	database := &countingQuerier{}
	handler, tokens := unsubscribeHandler(t, database)
	valid, _, err := tokens.IssueMemberNotificationUnsubscribe(
		"22222222-2222-4222-8222-222222222222", "11111111-1111-4111-8111-111111111111",
	)
	if err != nil {
		t.Fatalf("issue unsubscribe token: %v", err)
	}
	sessionToken, _, err := tokens.IssueTenant(
		"22222222-2222-4222-8222-222222222222", "11111111-1111-4111-8111-111111111111",
		[]string{string(auth.PermissionIssueRead)},
	)
	if err != nil {
		t.Fatalf("issue tenant token: %v", err)
	}
	responses := make([]string, 0, 4)
	statuses := make([]int, 0, 4)
	for _, token := range []string{valid, "not-a-token", "", sessionToken} {
		recorder, body := performUnsubscribe(handler, http.MethodPost, token)
		responses = append(responses, body)
		statuses = append(statuses, recorder.Code)
	}
	for index := range responses {
		if statuses[index] != http.StatusOK {
			t.Fatalf("status for token %d = %d, want 200", index, statuses[index])
		}
		if responses[index] != responses[0] {
			t.Fatalf("response for token %d differs from the valid one", index)
		}
	}
	for _, leak := range []string{"22222222", "11111111", "@"} {
		if strings.Contains(responses[0], leak) {
			t.Fatalf("unsubscribe page leaked %q", leak)
		}
	}
	// A tenant session token must not be accepted here: the audience binds the
	// link to the single unsubscribe action.
	if database.writes != 1 {
		t.Fatalf("database writes = %d, want exactly the one valid token", database.writes)
	}
}

// A mail scanner that follows links in a message must not be able to change
// anyone's settings, so GET only renders the confirmation form.
func TestUnsubscribeGetDoesNotApplyTheChange(t *testing.T) {
	t.Parallel()
	database := &countingQuerier{}
	handler, tokens := unsubscribeHandler(t, database)
	token, _, err := tokens.IssueMemberNotificationUnsubscribe(
		"22222222-2222-4222-8222-222222222222", "11111111-1111-4111-8111-111111111111",
	)
	if err != nil {
		t.Fatalf("issue unsubscribe token: %v", err)
	}
	recorder, body := performUnsubscribe(handler, http.MethodGet, token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if database.writes != 0 {
		t.Fatalf("a GET wrote to the database %d times", database.writes)
	}
	if !strings.Contains(body, `method="post"`) {
		t.Fatalf("confirmation page has no POST form:\n%s", body)
	}
	if policy := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "form-action 'self'") {
		t.Fatalf("content security policy = %q", policy)
	}
}

func TestUnsubscribeEscapesTheEchoedToken(t *testing.T) {
	t.Parallel()
	database := &countingQuerier{}
	handler, _ := unsubscribeHandler(t, database)
	_, body := performUnsubscribe(handler, http.MethodGet, `"><script>alert(1)</script>`)
	if strings.Contains(body, "<script>") {
		t.Fatalf("token was echoed unescaped:\n%s", body)
	}
}
