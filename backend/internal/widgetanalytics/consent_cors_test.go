package widgetanalytics

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

// consentRecorderStub lets recordConsent/revokeConsent succeed so the HTTP
// layer reaches its response-writing path, unlike consentAuthorizerStub
// (public_http_test.go) whose RecordConsent/RevokeConsent always error.
type consentRecorderStub struct {
	consent Consent
}

func (s *consentRecorderStub) RecordConsent(
	context.Context, db.Querier, string, string, string, string, string, string, *string, *string,
	ConsentState, time.Time, *time.Time,
) (Consent, error) {
	return s.consent, nil
}

func (s *consentRecorderStub) AuthorizeConsent(
	context.Context, db.Querier, string, string, string, string, string, string, time.Time,
) (Consent, error) {
	return Consent{}, errors.New("unexpected AuthorizeConsent call")
}

func (s *consentRecorderStub) RevokeConsent(
	context.Context, db.Querier, string, string, string, time.Time,
) (Consent, error) {
	return s.consent, nil
}

func TestPublicHTTPRecordConsentSetsCORSHeaders(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	expiresAt := now.AddDate(0, 0, 30)
	handler := publicAnalyticsHandler(PublicHTTPOptions{
		Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
		Begin:      func(context.Context) (db.Transaction, error) { return &analyticsTransaction{}, nil },
		Consent: &consentRecorderStub{
			consent: Consent{State: ConsentActive, ExpiresAt: &expiresAt},
		},
		Now: func() time.Time { return now },
	})

	ruleID := analyticsHTTPConfiguration().Rules[0].ID
	body := `{"widget_rule_id":"` + ruleID + `","purpose":"widget_analytics",` +
		`"notice_version":"2026-07","locale":"en","source":"customer_cmp",` +
		`"consent_id":"visitor-1","state":"granted","grant_token":"` + testAnalyticsConsentToken + `"}`
	response := performAnalyticsRequest(
		handler, http.MethodPost, "/v1/widget/analytics-consents?key=pk_live_test", body, "", "https://example.com",
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
}

func TestPublicHTTPRevokeConsentSetsCORSHeaders(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	handler := publicAnalyticsHandler(PublicHTTPOptions{
		Authorizer: analyticsAuthorizer{configuration: analyticsHTTPConfiguration()},
		Begin:      func(context.Context) (db.Transaction, error) { return &analyticsTransaction{}, nil },
		Consent:    &consentRecorderStub{},
		Now:        func() time.Time { return now },
	})

	body := `{"token":"` + testAnalyticsConsentToken + `"}`
	response := performAnalyticsRequest(
		handler, http.MethodPost, "/v1/widget/analytics-consents/revoke?key=pk_live_test", body, "", "https://example.com",
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://example.com")
	}
}
