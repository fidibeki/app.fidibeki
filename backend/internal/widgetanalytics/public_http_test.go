package widgetanalytics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPublicHTTPIngestsValidatedEventsInTenantTransaction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	tx := &analyticsTransaction{}
	store := &eventStoreStub{result: IngestResult{
		AcceptedEventIDs: []string{"00000000-0000-4000-8000-000000000001"},
	}}
	handler := publicAnalyticsHandler(PublicHTTPOptions{
		Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
		Begin:      func(context.Context) (db.Transaction, error) { return tx, nil },
		Store:      store, Consent: &consentAuthorizerStub{allow: true}, Now: func() time.Time { return now },
	})

	response := performAnalyticsRequest(handler, http.MethodPost, "/v1/widget/events?key=pk_live_test", validIngestBody(now), "", "https://example.com")
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if tx.configuredTenant != analyticsHTTPConfiguration().TenantID || tx.commitCalls != 1 || tx.rollbackCalls != 0 {
		t.Fatalf("transaction = tenant %q commits %d rollbacks %d", tx.configuredTenant, tx.commitCalls, tx.rollbackCalls)
	}
	if len(store.events) != 1 || store.events[0].PageURL != "https://example.com/chart" || store.events[0].RuleName != "Checkout" {
		t.Fatalf("stored events = %#v", store.events)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
	if !strings.Contains(response.Body.String(), `"accepted_event_ids":["00000000-0000-4000-8000-000000000001"]`) ||
		!strings.Contains(response.Body.String(), `"duplicate_event_ids":[]`) {
		t.Fatalf("response body = %s", response.Body.String())
	}
}

func TestPublicHTTPRejectsInvalidAccessAndPayloadBeforeTransaction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		path       string
		headerKey  string
		origin     string
		body       string
		authorizer analyticsAuthorizer
		wantStatus int
	}{
		{name: "key mismatch", path: "/v1/widget/events?key=one", headerKey: "two", origin: "https://example.com", body: validIngestBody(now), authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()}, wantStatus: http.StatusForbidden},
		{name: "origin denied", path: "/v1/widget/events?key=one", origin: "https://other.test", body: validIngestBody(now), authorizer: analyticsAuthorizer{err: widgetapi.ErrAccessDenied}, wantStatus: http.StatusForbidden},
		{name: "invalid event", path: "/v1/widget/events?key=one", origin: "https://example.com", body: `{"events":[]}`, authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()}, wantStatus: http.StatusUnprocessableEntity},
		{name: "unknown field", path: "/v1/widget/events?key=one", origin: "https://example.com", body: `{"events":[],"extra":true}`, authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()}, wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			beginCalls := 0
			handler := publicAnalyticsHandler(PublicHTTPOptions{
				Authorizer: test.authorizer,
				Begin: func(context.Context) (db.Transaction, error) {
					beginCalls++
					return &analyticsTransaction{}, nil
				},
				Store: &eventStoreStub{}, Now: func() time.Time { return now },
			})
			response := performAnalyticsRequest(handler, http.MethodPost, test.path, test.body, test.headerKey, test.origin)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if beginCalls != 0 {
				t.Fatalf("begin calls = %d, want 0", beginCalls)
			}
		})
	}
}

func TestPublicHTTPEnforcesBodyAndEventRateLimitsBeforeTransaction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	t.Run("body larger than 64 KiB", func(t *testing.T) {
		t.Parallel()
		beginCalls := 0
		handler := publicAnalyticsHandler(PublicHTTPOptions{
			Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
			Begin: func(context.Context) (db.Transaction, error) {
				beginCalls++
				return &analyticsTransaction{}, nil
			},
			Store: &eventStoreStub{}, Now: func() time.Time { return now },
		})
		body := `{"events":[],"padding":"` + strings.Repeat("x", int(maximumBodySize)) + `"}`
		response := performAnalyticsRequest(handler, http.MethodPost,
			"/v1/widget/events?key=pk_live_test", body, "", "https://example.com")
		if response.Code != http.StatusRequestEntityTooLarge || beginCalls != 0 {
			t.Fatalf("oversized response = status %d body %s begin %d", response.Code, response.Body.String(), beginCalls)
		}
	})

	t.Run("three hundred events per minute by project and source IP", func(t *testing.T) {
		t.Parallel()
		beginCalls := 0
		limiter := &analyticsLimiterStub{decisions: []ratelimit.Decision{
			{Allowed: true, Limit: 300, Remaining: 1},
			{Allowed: false, Limit: 300, Remaining: 0, RetryAfter: 1500 * time.Millisecond},
		}}
		handler := publicAnalyticsHandler(PublicHTTPOptions{
			Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
			Begin: func(context.Context) (db.Transaction, error) {
				beginCalls++
				return &analyticsTransaction{}, nil
			},
			Store: &eventStoreStub{}, Limiter: limiter, Now: func() time.Time { return now },
		})
		response := performAnalyticsRequest(handler, http.MethodPost,
			"/v1/widget/events?key=pk_live_test", `{"events":[{},{}]}`, "", "https://example.com")
		if response.Code != http.StatusTooManyRequests || beginCalls != 0 || limiter.calls != 2 {
			t.Fatalf("rate response = status %d body %s begin %d calls %d",
				response.Code, response.Body.String(), beginCalls, limiter.calls)
		}
		if limiter.window != (ratelimit.Window{Limit: 300, Period: time.Minute}) ||
			!strings.Contains(limiter.key, analyticsHTTPConfiguration().ProjectID+":192.0.2.1") {
			t.Fatalf("rate limiter = key %q window %#v", limiter.key, limiter.window)
		}
		if response.Header().Get("Retry-After") != "2" ||
			response.Header().Get("X-RateLimit-Limit") != "300;w=60" {
			t.Fatalf("rate headers = %#v", response.Header())
		}
	})

	t.Run("limiter outage fails open", func(t *testing.T) {
		t.Parallel()
		limiter := &analyticsLimiterStub{err: errors.New("redis unavailable")}
		handler := publicAnalyticsHandler(PublicHTTPOptions{
			Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
			Begin:      func(context.Context) (db.Transaction, error) { return &analyticsTransaction{}, nil },
			Store:      &eventStoreStub{}, Consent: &consentAuthorizerStub{allow: true},
			Limiter: limiter, Now: func() time.Time { return now },
		})
		response := performAnalyticsRequest(handler, http.MethodPost,
			"/v1/widget/events?key=pk_live_test", validIngestBody(now), "", "https://example.com")
		if response.Code != http.StatusAccepted || limiter.calls != 1 ||
			response.Header().Get("X-RateLimit-Remaining") != "unknown" {
			t.Fatalf("limiter outage response = status %d body %s calls %d headers %#v",
				response.Code, response.Body.String(), limiter.calls, response.Header())
		}
	})
}

// A funnel event whose "matched" opener never arrived can never succeed on replay. Answering it
// with 5xx made the widget outbox classify the batch as retryable and resend it every five
// seconds until the events aged out, so the status must stay in the non-retryable 4xx range.
func TestPublicHTTPRejectsUnmatchedFunnelEventAsValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	tx := &analyticsTransaction{}
	handler := publicAnalyticsHandler(PublicHTTPOptions{
		Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
		Begin:      func(context.Context) (db.Transaction, error) { return tx, nil },
		Store: &eventStoreStub{err: &MatchedEventRequiredError{
			Index: 0, EventID: "00000000-0000-4000-8000-000000000001",
		}},
		Consent: &consentAuthorizerStub{allow: true}, Now: func() time.Time { return now },
	})

	response := performAnalyticsRequest(handler, http.MethodPost,
		"/v1/widget/events?key=pk_live_test", validIngestBody(now), "", "https://example.com")
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"validation_error"`) ||
		!strings.Contains(response.Body.String(), `events[0].event_type`) {
		t.Fatalf("response body = %s", response.Body.String())
	}
	if tx.commitCalls != 0 || tx.rollbackCalls != 1 {
		t.Fatalf("transaction = commits %d rollbacks %d", tx.commitCalls, tx.rollbackCalls)
	}
}

// Genuine infrastructure failures must keep their retryable 503 so the outbox replays them.
func TestPublicHTTPKeepsServiceUnavailableForStoreOutage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	handler := publicAnalyticsHandler(PublicHTTPOptions{
		Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
		Begin:      func(context.Context) (db.Transaction, error) { return &analyticsTransaction{}, nil },
		Store:      &eventStoreStub{err: errors.New("connection reset by peer")},
		Consent:    &consentAuthorizerStub{allow: true}, Now: func() time.Time { return now },
	})

	response := performAnalyticsRequest(handler, http.MethodPost,
		"/v1/widget/events?key=pk_live_test", validIngestBody(now), "", "https://example.com")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestSummaryCalculatesRatesWithNullZeroDenominators(t *testing.T) {
	t.Parallel()
	summary := Summary{Matched: 8, Impressions: 4, Interactions: 2, Submissions: 1}
	summary.CalculateRates()
	if summary.ImpressionRate == nil || *summary.ImpressionRate != .5 ||
		summary.InteractionRate == nil || *summary.InteractionRate != .5 ||
		summary.CompletionRate == nil || *summary.CompletionRate != .5 ||
		summary.SubmitRate == nil || *summary.SubmitRate != .25 {
		t.Fatalf("rates = %#v", summary)
	}
	empty := Summary{}
	empty.CalculateRates()
	if empty.ImpressionRate != nil || empty.InteractionRate != nil || empty.CompletionRate != nil || empty.SubmitRate != nil {
		t.Fatalf("empty rates = %#v, want nil", empty)
	}
}

func publicAnalyticsHandler(options PublicHTTPOptions) http.Handler {
	router := chi.NewRouter()
	RegisterPublicRoutes(router, options)
	return router
}

func validIngestBody(now time.Time) string {
	return `{"events":[{` +
		`"event_id":"00000000-0000-4000-8000-000000000001",` +
		`"event_type":"matched",` +
		`"visitor_id":"00000000-0000-4000-8000-000000000002",` +
		`"page_view_id":"00000000-0000-4000-8000-000000000003",` +
		`"rule_id":"00000000-0000-4000-8000-000000000004",` +
		`"page_url":"https://example.com/chart/?asset=BAI#price",` +
		`"occurred_at":"` + now.Format(time.RFC3339) + `",` +
		`"consent_token":"` + testAnalyticsConsentToken + `"}]}`
}

func performAnalyticsRequest(handler http.Handler, method, path, body, publicKey, origin string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://widget.localhost"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	if publicKey != "" {
		request.Header.Set(widgetapi.PublicKeyHeader, publicKey)
	}
	handler.ServeHTTP(response, request)
	return response
}

type analyticsAuthorizer struct {
	configuration project.PublicConfiguration
	err           error
}

func (a analyticsAuthorizer) Authorize(context.Context, string, string) (project.PublicConfiguration, error) {
	return a.configuration, a.err
}

func analyticsHTTPConfiguration() project.PublicConfiguration {
	configuration := analyticsConfiguration()
	configuration.AllowedDomains = []string{"example.com"}
	configuration.Plan = tenant.PlanGrowth
	configuration.Status = tenant.StatusActive
	configuration.Rules[0].AnalyticsConsent = project.AnalyticsConsentConfig{
		Mode: project.AnalyticsConsentCustomerManaged, NoticeVersion: "2026-07",
		SupportedLocales: []string{"en"}, ExpiryDays: 30,
	}
	return configuration
}

const testAnalyticsConsentToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type consentAuthorizerStub struct {
	allow bool
	err   error
}

func (s *consentAuthorizerStub) RecordConsent(
	context.Context, db.Querier, string, string, string, string, string, string, *string, *string,
	ConsentState, time.Time, *time.Time,
) (Consent, error) {
	return Consent{}, errors.New("unexpected RecordConsent call")
}

func (s *consentAuthorizerStub) AuthorizeConsent(
	context.Context, db.Querier, string, string, string, string, string, string, time.Time,
) (Consent, error) {
	if s.err != nil {
		return Consent{}, s.err
	}
	if !s.allow {
		return Consent{}, ErrConsentRequired
	}
	return Consent{State: ConsentActive}, nil
}

func (s *consentAuthorizerStub) RevokeConsent(
	context.Context, db.Querier, string, string, string, time.Time,
) (Consent, error) {
	return Consent{}, errors.New("unexpected RevokeConsent call")
}

type eventStoreStub struct {
	result IngestResult
	events []EventValues
	err    error
}

type analyticsLimiterStub struct {
	decisions []ratelimit.Decision
	err       error
	key       string
	window    ratelimit.Window
	calls     int
}

func (l *analyticsLimiterStub) Allow(_ context.Context, key string, window ratelimit.Window) (ratelimit.Decision, error) {
	l.calls++
	l.key = key
	l.window = window
	if len(l.decisions) == 0 {
		return ratelimit.Decision{Allowed: true, Limit: window.Limit, Remaining: window.Limit}, l.err
	}
	index := l.calls - 1
	if index >= len(l.decisions) {
		index = len(l.decisions) - 1
	}
	return l.decisions[index], l.err
}

func (s *eventStoreStub) InsertEvents(_ context.Context, _ db.Querier, events []EventValues) (IngestResult, error) {
	s.events = append([]EventValues(nil), events...)
	return s.result, s.err
}

type analyticsTransaction struct {
	configuredTenant string
	commitCalls      int
	rollbackCalls    int
}

func (*analyticsTransaction) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (*analyticsTransaction) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call")
}

func (t *analyticsTransaction) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if !strings.Contains(query, "set_config") || len(args) != 1 {
		return analyticsRow{err: errors.New("unexpected QueryRow call")}
	}
	t.configuredTenant, _ = args[0].(string)
	return analyticsRow{value: t.configuredTenant}
}

func (t *analyticsTransaction) Commit(context.Context) error {
	t.commitCalls++
	return nil
}

func (t *analyticsTransaction) Rollback(context.Context) error {
	t.rollbackCalls++
	return nil
}

type analyticsRow struct {
	value string
	err   error
}

func (r analyticsRow) Scan(destinations ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected scan destination count")
	}
	destination, ok := destinations[0].(*string)
	if !ok {
		return errors.New("unexpected scan destination type")
	}
	*destination = r.value
	return nil
}
