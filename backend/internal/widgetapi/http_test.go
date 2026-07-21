package widgetapi

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
)

func TestOriginAllowedUsesExactAndLeadingWildcardMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		origin  string
		allowed []string
		want    bool
	}{
		{name: "exact", origin: "https://example.com", allowed: []string{"example.com"}, want: true},
		{name: "exact with port", origin: "http://localhost:3000", allowed: []string{"localhost"}, want: true},
		{name: "wildcard child", origin: "https://app.example.com", allowed: []string{"*.example.com"}, want: true},
		{name: "wildcard nested", origin: "https://a.b.example.com", allowed: []string{"*.example.com"}, want: true},
		{name: "wildcard excludes root", origin: "https://example.com", allowed: []string{"*.example.com"}},
		{name: "suffix attack", origin: "https://notexample.com", allowed: []string{"*.example.com"}},
		{name: "scheme missing", origin: "example.com", allowed: []string{"example.com"}},
		{name: "path rejected", origin: "https://example.com/path", allowed: []string{"example.com"}},
		{name: "credentials rejected", origin: "https://user@example.com", allowed: []string{"example.com"}},
		{name: "null rejected", origin: "null", allowed: []string{"null"}},
		{name: "trailing dot rejected", origin: "https://example.com.", allowed: []string{"example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := OriginAllowed(tt.origin, tt.allowed); got != tt.want {
				t.Errorf("OriginAllowed(%q, %#v) = %t, want %t", tt.origin, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestConfigurationReturnsEnabledRulesAndPlanFeatures(t *testing.T) {
	t.Parallel()
	delay := 3
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	store := &configurationStoreStub{configuration: project.PublicConfiguration{
		ProjectID: "project-id", AllowedDomains: []string{"example.com"},
		Plan: tenant.PlanGrowth, Status: tenant.StatusActive,
		Rules: []project.WidgetRule{{
			ID: "rule-id", Name: "All pages", Priority: 5, MatcherType: project.MatcherAllPages,
			TriggerType: project.TriggerDelay, TriggerDelaySeconds: &delay,
			CanRate: true, CanWriteText: true, CanRecordAudio: true,
			Position: project.PositionBottomRight, Theme: project.ThemeAuto,
			ReplayMode: project.ReplaySampling, ReplaySamplingRate: 25,
			ReplayMaxDurationSeconds: 1800, ReplayMaxEvents: 50000, ReplayMaskAllInputs: true,
		}},
	}}
	handler := NewHTTPHandler(HTTPOptions{Store: store, Now: func() time.Time { return now }})
	recorder := performWidgetRequest(
		handler, http.MethodGet,
		"/v1/widget/config?key=pk_live_key",
		"pk_live_key", "https://example.com",
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Access-Control-Allow-Credentials = %q", got)
	}
	for _, fragment := range []string{
		`"project_id":"project-id"`, `"audio":true`, `"video":false`,
		`"id":"rule-id"`, `"trigger_delay_seconds":3`, `"refresh_after_seconds":300`,
	} {
		if !strings.Contains(recorder.Body.String(), fragment) {
			t.Errorf("body = %s, missing %s", recorder.Body.String(), fragment)
		}
	}
	if store.publicKey != "pk_live_key" {
		t.Errorf("loaded key = %q", store.publicKey)
	}
	if store.markedProjectID != "" {
		t.Errorf("configuration fetch marked widget activity for %q", store.markedProjectID)
	}
}

func TestConfigurationPreflightIsAuthorizedAgainstProject(t *testing.T) {
	t.Parallel()
	store := &configurationStoreStub{configuration: project.PublicConfiguration{
		ProjectID: "project-id", AllowedDomains: []string{"*.example.com"},
		Plan: tenant.PlanStarter, Status: tenant.StatusActive,
	}}
	handler := NewHTTPHandler(HTTPOptions{Store: store})
	recorder := performWidgetRequest(
		handler, http.MethodOptions, "/v1/widget/config?key=pk_live_key", "", "https://app.example.com",
	)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Headers"); got != PublicKeyHeader {
		t.Errorf("Access-Control-Allow-Headers = %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Max-Age"); got != "300" {
		t.Errorf("Access-Control-Max-Age = %q", got)
	}
	if store.markedProjectID != "" {
		t.Errorf("preflight marked widget activity for %q", store.markedProjectID)
	}
}

func TestConnectionTelemetryFailureDoesNotBlockAcknowledgement(t *testing.T) {
	t.Parallel()
	store := activeStore()
	store.markErr = errors.New("database unavailable")
	handler := NewHTTPHandler(HTTPOptions{Store: store})
	recorder := performWidgetRequest(
		handler, http.MethodPost, "/v1/widget/connected?key=pk_live_key&rule_id=rule-id",
		"", "https://example.com",
	)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestConnectionRecordsActivityOnlyForEnabledConfigurationRule(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		ruleID   string
		wantMark bool
	}{
		{name: "missing rule ID"},
		{name: "unknown rule ID", ruleID: "other-rule"},
		{name: "enabled configuration rule", ruleID: "rule-id", wantMark: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &configurationStoreStub{configuration: project.PublicConfiguration{
				ProjectID: "project-id", AllowedDomains: []string{"example.com"},
				Plan: tenant.PlanStarter, Status: tenant.StatusActive,
				Rules: []project.WidgetRule{{ID: "rule-id", MatcherType: project.MatcherAllPages}},
			}}
			path := "/v1/widget/connected?key=pk_live_key"
			if tt.ruleID != "" {
				path += "&rule_id=" + tt.ruleID
			}
			handler := NewHTTPHandler(HTTPOptions{Store: store, Now: func() time.Time { return now }})
			recorder := performWidgetRequest(handler, http.MethodPost, path, "", "https://example.com")
			wantStatus := http.StatusForbidden
			if tt.wantMark {
				wantStatus = http.StatusNoContent
			}
			if recorder.Code != wantStatus {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if got := store.markedProjectID != ""; got != tt.wantMark {
				t.Errorf("widget activity marked = %t, want %t", got, tt.wantMark)
			}
			if tt.wantMark && !store.seenAt.Equal(now) {
				t.Errorf("seen at = %s, want %s", store.seenAt, now)
			}
		})
	}
}

func TestConnectionRejectsUnknownPlanWithoutRecordingActivity(t *testing.T) {
	t.Parallel()
	store := activeStore()
	store.configuration.Plan = "unknown"
	recorder := performWidgetRequest(
		handlerForStore(store), http.MethodPost,
		"/v1/widget/connected?key=pk_live_key&rule_id=rule-id", "", "https://example.com",
	)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if store.markedProjectID != "" {
		t.Errorf("unknown plan recorded widget activity for %q", store.markedProjectID)
	}
}

func TestConfigurationRejectsInvalidAccessWithoutCORS(t *testing.T) {
	t.Parallel()
	future := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	past := future.Add(-48 * time.Hour)
	tests := []struct {
		name       string
		store      *configurationStoreStub
		path       string
		key        string
		origin     string
		now        time.Time
		wantStatus int
		wantCORS   bool
	}{
		{
			name: "missing key", store: activeStore(), path: "/v1/widget/config",
			origin: "https://example.com", wantStatus: http.StatusForbidden,
		},
		{
			name: "mismatched query key", store: activeStore(), path: "/v1/widget/config?key=other",
			key: "pk_live_key", origin: "https://example.com", wantStatus: http.StatusForbidden,
		},
		{
			name: "unknown project", store: &configurationStoreStub{err: project.ErrNotFound},
			path: "/v1/widget/config", key: "pk_live_key", origin: "https://example.com", wantStatus: http.StatusForbidden,
		},
		{
			name: "invalid origin", store: activeStore(), path: "/v1/widget/config",
			key: "pk_live_key", origin: "https://evil.test", wantStatus: http.StatusForbidden,
		},
		{
			name: "suspended", store: &configurationStoreStub{configuration: project.PublicConfiguration{
				AllowedDomains: []string{"example.com"}, Plan: tenant.PlanStarter, Status: tenant.StatusSuspended,
			}}, path: "/v1/widget/config", key: "pk_live_key", origin: "https://example.com", wantStatus: http.StatusForbidden,
		},
		{
			name: "expired trial", store: &configurationStoreStub{configuration: project.PublicConfiguration{
				AllowedDomains: []string{"example.com"}, Plan: tenant.PlanStarter,
				Status: tenant.StatusTrial, TrialEndsAt: &past,
			}}, path: "/v1/widget/config", key: "pk_live_key", origin: "https://example.com",
			now: future, wantStatus: http.StatusForbidden,
		},
		{
			name: "store unavailable", store: &configurationStoreStub{err: errors.New("database unavailable")},
			path: "/v1/widget/config", key: "pk_live_key", origin: "https://example.com", wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "unknown plan", store: &configurationStoreStub{configuration: project.PublicConfiguration{
				AllowedDomains: []string{"example.com"}, Plan: "unknown", Status: tenant.StatusActive,
			}}, path: "/v1/widget/config", key: "pk_live_key", origin: "https://example.com",
			wantStatus: http.StatusServiceUnavailable, wantCORS: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			now := tt.now
			handler := NewHTTPHandler(HTTPOptions{Store: tt.store, Now: func() time.Time { return now }})
			recorder := performWidgetRequest(handler, http.MethodGet, tt.path, tt.key, tt.origin)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); (got != "") != tt.wantCORS {
				t.Errorf("Access-Control-Allow-Origin = %q", got)
			}
			if tt.store.markedProjectID != "" {
				t.Errorf("rejected request marked widget activity for %q", tt.store.markedProjectID)
			}
		})
	}
}

func TestConfigurationAllowsUnexpiredTrial(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	trialEnd := now.Add(time.Hour)
	handler := NewHTTPHandler(HTTPOptions{
		Store: &configurationStoreStub{configuration: project.PublicConfiguration{
			ProjectID: "trial-project", AllowedDomains: []string{"example.com"},
			Plan: tenant.PlanStarter, Status: tenant.StatusTrial, TrialEndsAt: &trialEnd,
		}},
		Now: func() time.Time { return now },
	})
	recorder := performWidgetRequest(handler, http.MethodGet, "/v1/widget/config", "pk_live_key", "https://example.com")
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

type configurationStoreStub struct {
	configuration   project.PublicConfiguration
	err             error
	publicKey       string
	markErr         error
	markedProjectID string
	seenAt          time.Time
}

func (s *configurationStoreStub) MarkWidgetSeen(
	_ context.Context,
	_ db.Querier,
	projectID string,
	seenAt time.Time,
) error {
	s.markedProjectID = projectID
	s.seenAt = seenAt
	return s.markErr
}

func (s *configurationStoreStub) LoadPublicConfiguration(
	_ context.Context,
	_ db.Querier,
	publicKey string,
) (project.PublicConfiguration, error) {
	s.publicKey = publicKey
	return s.configuration, s.err
}

func activeStore() *configurationStoreStub {
	return &configurationStoreStub{configuration: project.PublicConfiguration{
		ProjectID: "project-id", AllowedDomains: []string{"example.com"},
		Plan: tenant.PlanStarter, Status: tenant.StatusActive,
		Rules: []project.WidgetRule{{ID: "rule-id", MatcherType: project.MatcherAllPages}},
	}}
}

func handlerForStore(store *configurationStoreStub) http.Handler {
	return NewHTTPHandler(HTTPOptions{Store: store})
}

func performWidgetRequest(
	handler http.Handler,
	method string,
	path string,
	publicKey string,
	origin string,
) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://widget.localhost"+path, nil)
	if publicKey != "" {
		request.Header.Set(PublicKeyHeader, publicKey)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}
