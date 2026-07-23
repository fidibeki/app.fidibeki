package abuse

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TenantHTTPStore interface {
	List(context.Context, db.Querier, string, string, time.Time, time.Time) ([]DiagnosticPoint, error)
}

type TenantHTTPOptions struct {
	Store  TenantHTTPStore
	JWT    *auth.JWTManager
	Now    func() time.Time
	Logger *slog.Logger
}

type tenantHTTPHandler struct {
	options TenantHTTPOptions
	logger  *slog.Logger
}

func RegisterTenantRoutes(router chi.Router, options TenantHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &tenantHTTPHandler{options: options, logger: logger}
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionTenantRead),
	).Get("/v1/abuse/diagnostics", handler.list)
}

type DiagnosticsQuery struct {
	Range      string `json:"range"`
	RangeHours int    `json:"-"`
	ProjectID  string `json:"project_id,omitempty"`
}

type DiagnosticTotal struct {
	Category Category `json:"category"`
	Count    int64    `json:"count"`
}

type DiagnosticsWorkspace struct {
	Range   string             `json:"range"`
	Period  DiagnosticsPeriod  `json:"period"`
	Filters DiagnosticsFilters `json:"filters"`
	Total   int64              `json:"total"`
	Totals  []DiagnosticTotal  `json:"totals"`
	Trend   []DiagnosticPoint  `json:"trend"`
}

type DiagnosticsPeriod struct {
	StartAt  time.Time `json:"start_at"`
	EndAt    time.Time `json:"end_at"`
	Timezone string    `json:"timezone"`
}

type DiagnosticsFilters struct {
	ProjectID string `json:"project_id,omitempty"`
}

func (handler *tenantHTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	filters, fields := parseDiagnosticsQuery(r.URL.Query())
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"diagnostics query is invalid", fields)
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	now := time.Now().UTC()
	if handler.options.Now != nil {
		now = handler.options.Now().UTC()
	}
	end := now.Truncate(time.Hour).Add(time.Hour)
	start := end.Add(-time.Duration(filters.RangeHours) * time.Hour)
	points, err := handler.options.Store.List(
		r.Context(), query, resolved.ID, filters.ProjectID, start, end,
	)
	if err != nil {
		handler.logger.ErrorContext(r.Context(), "list abuse diagnostics",
			"error", err, "tenant_id", resolved.ID, "range", filters.Range,
			"has_project_filter", filters.ProjectID != "")
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, summarizeDiagnostics(filters, start, end, points))
}

func parseDiagnosticsQuery(values url.Values) (DiagnosticsQuery, map[string]string) {
	query := DiagnosticsQuery{Range: "7d", RangeHours: 7 * 24}
	fields := map[string]string{}
	switch raw := strings.TrimSpace(values.Get("range")); raw {
	case "", "7d":
	case "24h":
		query.Range, query.RangeHours = raw, 24
	case "30d":
		query.Range, query.RangeHours = raw, 30*24
	default:
		fields["range"] = "must be 24h, 7d, or 30d"
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
	return query, fields
}

func summarizeDiagnostics(
	query DiagnosticsQuery,
	start time.Time,
	end time.Time,
	points []DiagnosticPoint,
) DiagnosticsWorkspace {
	categories := []Category{
		CategoryRateLimit, CategorySpam, CategoryPlanQuota,
		CategoryUploadSafety, CategoryAIBudget, CategoryProvider,
	}
	counts := make(map[Category]int64, len(categories))
	var total int64
	for _, point := range points {
		counts[point.Category] += point.Count
		total += point.Count
	}
	totals := make([]DiagnosticTotal, 0, len(categories))
	for _, category := range categories {
		totals = append(totals, DiagnosticTotal{Category: category, Count: counts[category]})
	}
	return DiagnosticsWorkspace{
		Range: query.Range,
		Period: DiagnosticsPeriod{
			StartAt: start, EndAt: end, Timezone: "UTC",
		},
		Filters: DiagnosticsFilters{ProjectID: query.ProjectID},
		Total:   total, Totals: totals, Trend: points,
	}
}
