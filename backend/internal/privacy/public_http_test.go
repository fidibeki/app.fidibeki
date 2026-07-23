package privacy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/go-chi/chi/v5"
)

func TestPublicReporterRequestRateLimitFailsClosed(t *testing.T) {
	t.Parallel()
	token, _, err := reporterRequestToken()
	if err != nil {
		t.Fatalf("reporterRequestToken() error = %v", err)
	}
	routes := chi.NewRouter()
	RegisterPublicRoutes(routes, PublicHTTPOptions{
		Limiter: reporterLimiterStub{decision: ratelimit.Decision{
			Allowed: false, RetryAfter: 1500 * time.Millisecond,
		}},
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/privacy/requests/"+token, nil)
	recorder := httptest.NewRecorder()
	routes.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "2" {
		t.Fatalf("limited request = %d headers=%#v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestReporterVerificationMessageUsesTenantURLAndRotatedIdempotency(t *testing.T) {
	t.Parallel()
	emailAddress := "reporter@example.test"
	request := ReporterRequest{
		ID: "request-id", Action: ReporterAnonymize, ReporterEmail: &emailAddress,
	}
	message, err := reporterVerificationMessage(
		request, "Acme", "acme", strings.Repeat("a", 43), HTTPOptions{
			PublicBaseURL: "https://api.staging.fidibeki.com",
			BaseDomain:    "staging.fidibeki.com", From: "Fidibeki <auth@fidibeki.com>",
		},
	)
	if err != nil {
		t.Fatalf("reporterVerificationMessage() error = %v", err)
	}
	if message.To != emailAddress || !strings.Contains(message.Text,
		"https://acme.staging.fidibeki.com/privacy/"+strings.Repeat("a", 43)) ||
		!strings.Contains(message.Text, "Confirm anonymization") ||
		!strings.HasPrefix(message.IdempotencyKey, "privacy-verification:request-id:") {
		t.Fatalf("verification message = %#v text=%s", message, message.Text)
	}
	if strings.Contains(message.Text, "api.staging") {
		t.Errorf("verification URL used API host: %s", message.Text)
	}
}

type reporterLimiterStub struct {
	decision ratelimit.Decision
	err      error
}

func (limiter reporterLimiterStub) Allow(
	context.Context,
	string,
	ratelimit.Window,
) (ratelimit.Decision, error) {
	return limiter.decision, limiter.err
}

func TestPublicReporterRequestLimiterFailureIsUnavailable(t *testing.T) {
	t.Parallel()
	token, _, _ := reporterRequestToken()
	routes := chi.NewRouter()
	RegisterPublicRoutes(routes, PublicHTTPOptions{
		Limiter: reporterLimiterStub{err: errors.New("redis unavailable")},
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/privacy/requests/"+token, nil)
	recorder := httptest.NewRecorder()
	routes.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("limiter failure = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPublicReporterRequestRejectsInvalidOrUnavailableDependencies(t *testing.T) {
	t.Parallel()
	token, _, _ := reporterRequestToken()
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{
			name: "malformed token", method: http.MethodGet,
			path: "/v1/privacy/requests/not-a-token", want: http.StatusNotFound,
		},
		{
			name: "download without storage", method: http.MethodPost,
			path: "/v1/privacy/requests/" + token + "/download",
			want: http.StatusServiceUnavailable,
		},
		{
			name: "verify without transaction", method: http.MethodPost,
			path: "/v1/privacy/requests/" + token + "/verify",
			want: http.StatusServiceUnavailable,
		},
		{
			name: "status without database", method: http.MethodGet,
			path: "/v1/privacy/requests/" + token,
			want: http.StatusServiceUnavailable,
		},
	}
	routes := chi.NewRouter()
	RegisterPublicRoutes(routes, PublicHTTPOptions{})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("request = %d body=%s, want %d",
					recorder.Code, recorder.Body.String(), test.want)
			}
		})
	}
}

func TestPrivacyHandlersRejectMissingContextAndInvalidEmailConfiguration(t *testing.T) {
	t.Parallel()
	handler := &HTTPHandler{}
	request := httptest.NewRequest(http.MethodGet, "/v1/privacy/retention", nil)
	recorder := httptest.NewRecorder()
	handler.retention(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("retention without tenant = %d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	if _, _, _, ok := privacyRequestState(recorder, request); ok ||
		recorder.Code != http.StatusInternalServerError {
		t.Fatalf("privacy state without context ok=%t status=%d", ok, recorder.Code)
	}
	if handler.now().Location() != time.UTC {
		t.Fatal("default HTTP privacy clock is not UTC")
	}

	_, err := reporterVerificationMessage(
		ReporterRequest{ID: "request", Action: ReporterExport},
		"Acme", "acme", strings.Repeat("a", 43),
		HTTPOptions{
			PublicBaseURL: "https://api.staging.fidibeki.com",
			BaseDomain:    "staging.fidibeki.com",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "email is unavailable") {
		t.Fatalf("verification message without email error = %v", err)
	}
	if _, err := reporterPrivacyURL("", "", "", ""); err == nil {
		t.Fatal("reporterPrivacyURL(invalid) error = nil")
	}
}

func TestPrivacyHTTPErrorMappingAndTransactionContextGuards(t *testing.T) {
	t.Parallel()
	handler := &HTTPHandler{logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	for name, serve := range map[string]func(http.ResponseWriter, *http.Request){
		"create export":          handler.createExport,
		"list exports":           handler.listExports,
		"list reporter requests": handler.listReporterRequests,
	} {
		recorder := httptest.NewRecorder()
		serve(recorder, request)
		if recorder.Code != http.StatusInternalServerError {
			t.Errorf("%s without context = %d body=%s",
				name, recorder.Code, recorder.Body.String())
		}
	}
	for name, test := range map[string]struct {
		err  error
		want int
	}{
		"not found":       {err: ErrNotFound, want: http.StatusNotFound},
		"unavailable":     {err: ErrJobUnavailable, want: http.StatusConflict},
		"human required":  {err: ErrHumanRequired, want: http.StatusForbidden},
		"invalid request": {err: ErrInvalidRequest, want: http.StatusUnprocessableEntity},
		"dependency":      {err: errors.New("dependency unavailable"), want: http.StatusServiceUnavailable},
	} {
		recorder := httptest.NewRecorder()
		handler.writeError(recorder, request, test.err)
		if recorder.Code != test.want {
			t.Errorf("%s error mapping = %d body=%s, want %d",
				name, recorder.Code, recorder.Body.String(), test.want)
		}
	}
	if (Exporter{}).logger() == nil || (Scheduler{}).logger() == nil {
		t.Fatal("default privacy loggers are nil")
	}
}
