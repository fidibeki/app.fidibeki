package privacy

import (
	"context"
	"errors"
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
