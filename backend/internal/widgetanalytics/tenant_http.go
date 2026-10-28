package widgetanalytics

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/project"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AnalyticsStore interface {
	Workspace(context.Context, db.Querier, string, AnalyticsQuery) (Workspace, error)
	GetConsentDiagnostic(context.Context, db.Querier, string, string, string, time.Time) (ConsentDiagnostic, error)
}

type TenantHTTPOptions struct {
	Store        AnalyticsStore
	JWT          *auth.JWTManager
	Now          func() time.Time
	Logger       *slog.Logger
	QueryTimeout time.Duration
}

type TenantHTTPHandler struct {
	options      TenantHTTPOptions
	logger       *slog.Logger
	queryTimeout time.Duration
	concurrency  *analyticsTenantConcurrency
}

const (
	analyticsRequestTimeout           = 10 * time.Second
	maximumConcurrentAnalyticsQueries = 2
	analyticsConcurrencyRetryAfter    = "1"
)

type analyticsTenantConcurrency struct {
	mu     sync.Mutex
	active map[string]int
}

func newAnalyticsTenantConcurrency() *analyticsTenantConcurrency {
	return &analyticsTenantConcurrency{active: make(map[string]int)}
}

func (limiter *analyticsTenantConcurrency) acquire(tenantID string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.active[tenantID] >= maximumConcurrentAnalyticsQueries {
		return false
	}
	limiter.active[tenantID]++
	return true
}

func (limiter *analyticsTenantConcurrency) release(tenantID string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.active[tenantID]--
	if limiter.active[tenantID] <= 0 {
		delete(limiter.active, tenantID)
	}
}

func RegisterTenantRoutes(router chi.Router, options TenantHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	queryTimeout := options.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = analyticsRequestTimeout
	}
	handler := &TenantHTTPHandler{
		options: options, logger: logger, queryTimeout: queryTimeout,
		concurrency: newAnalyticsTenantConcurrency(),
	}
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionProjectRead),
	).Get("/v1/analytics/widget", handler.summary)
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionWidgetRuleRead),
	).Get(
		"/v1/projects/{projectID}/widget-rules/{ruleID}/analytics-consent-diagnostic",
		handler.getConsentDiagnostic,
	)
}

func (h *TenantHTTPHandler) getConsentDiagnostic(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	ruleID := chi.URLParam(r, "ruleID")
	if _, err := uuid.Parse(projectID); err != nil {
		writeAnalyticsConsentDiagnosticNotFound(w)
		return
	}
	if _, err := uuid.Parse(ruleID); err != nil {
		writeAnalyticsConsentDiagnosticNotFound(w)
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	if !h.concurrency.acquire(resolved.ID) {
		w.Header().Set("Retry-After", analyticsConcurrencyRetryAfter)
		api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded,
			"too many concurrent analytics queries", nil)
		return
	}
	defer h.concurrency.release(resolved.ID)
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	queryContext, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()
	diagnostic, err := h.options.Store.GetConsentDiagnostic(
		queryContext, query, resolved.ID, projectID, ruleID, now,
	)
	if errors.Is(err, ErrNotFound) {
		writeAnalyticsConsentDiagnosticNotFound(w)
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "widget analytics consent diagnostic query failed",
			"error", err, "tenant_id", resolved.ID)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"diagnostic": diagnostic})
}

func writeAnalyticsConsentDiagnosticNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "widget rule not found", nil)
}

func (h *TenantHTTPHandler) summary(w http.ResponseWriter, r *http.Request) {
	analyticsQuery, fields := parseAnalyticsQuery(r.URL.Query())
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"analytics query is invalid", fields)
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	if !h.concurrency.acquire(resolved.ID) {
		w.Header().Set("Retry-After", analyticsConcurrencyRetryAfter)
		api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded,
			"too many concurrent analytics queries", nil)
		return
	}
	defer h.concurrency.release(resolved.ID)
	analyticsQuery.Now = time.Now().UTC()
	if h.options.Now != nil {
		analyticsQuery.Now = h.options.Now().UTC()
	}
	startedAt := time.Now()
	queryContext, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()
	workspace, err := h.options.Store.Workspace(queryContext, query, resolved.ID, analyticsQuery)
	if errors.Is(err, ErrAnalyticsFilterNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "analytics filter not found", nil)
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "widget analytics query failed",
			"error", err, "tenant_id", resolved.ID, "range", analyticsQuery.Range,
			"has_project_filter", analyticsQuery.ProjectID != "",
			"has_rule_filter", analyticsQuery.RuleID != "",
			"has_page_filter", analyticsQuery.PageURL != "",
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	h.logger.InfoContext(r.Context(), "widget analytics query completed",
		"tenant_id", resolved.ID, "range", analyticsQuery.Range,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"projects_returned", len(workspace.Projects), "projects_total", workspace.Metadata.Projects.Total,
		"rules_returned", len(workspace.Rules), "rules_total", workspace.Metadata.Rules.Total,
		"pages_returned", len(workspace.Pages), "pages_total", workspace.Metadata.Pages.Total,
		"results_truncated", workspace.Metadata.Projects.Truncated ||
			workspace.Metadata.Rules.Truncated || workspace.Metadata.Pages.Truncated,
	)
	api.WriteJSON(w, http.StatusOK, workspace)
}

func parseAnalyticsQuery(values url.Values) (AnalyticsQuery, FieldErrors) {
	query := AnalyticsQuery{Range: "30d", RangeDays: 30}
	fields := FieldErrors{}
	if raw := strings.TrimSpace(values.Get("range")); raw != "" {
		switch raw {
		case "7d":
			query.Range, query.RangeDays = raw, 7
		case "30d":
			query.Range, query.RangeDays = raw, 30
		case "90d":
			query.Range, query.RangeDays = raw, 90
		default:
			fields["range"] = "must be 7d, 30d, or 90d"
		}
	}
	query.ProjectID = strings.TrimSpace(values.Get("project_id"))
	if query.ProjectID != "" {
		parsed, err := uuid.Parse(query.ProjectID)
		if err != nil {
			fields["project_id"] = "must be a UUID"
		} else {
			query.ProjectID = parsed.String()
		}
	}
	query.RuleID = strings.TrimSpace(values.Get("rule_id"))
	if query.RuleID != "" {
		parsed, err := uuid.Parse(query.RuleID)
		if err != nil {
			fields["rule_id"] = "must be a UUID"
		} else {
			query.RuleID = parsed.String()
		}
	}
	if rawPage := strings.TrimSpace(values.Get("page")); rawPage != "" {
		page, err := project.CanonicalizePage(rawPage)
		if err != nil || len(page.Target) > 4096 {
			fields["page"] = "must be an absolute canonical HTTP(S) page URL"
		} else {
			query.PageURL = page.Target
			pageKey := sha256.Sum256([]byte(page.Target))
			query.PageKey = pageKey[:]
		}
	}
	return query, fields
}
