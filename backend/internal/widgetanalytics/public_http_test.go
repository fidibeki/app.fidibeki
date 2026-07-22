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
		Store:      store, Now: func() time.Time { return now },
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
		`"occurred_at":"` + now.Format(time.RFC3339) + `"}]}`
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
	return configuration
}

type eventStoreStub struct {
	result IngestResult
	events []EventValues
	err    error
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
