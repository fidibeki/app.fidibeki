package widgetanalytics

import (
	"context"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type SummaryStore interface {
	Summary(context.Context, db.Querier, string, string, int, time.Time) (Summary, error)
}

type TenantHTTPOptions struct {
	Store SummaryStore
	JWT   *auth.JWTManager
	Now   func() time.Time
}

type TenantHTTPHandler struct {
	options TenantHTTPOptions
}

func RegisterTenantRoutes(router chi.Router, options TenantHTTPOptions) {
	handler := &TenantHTTPHandler{options: options}
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionProjectRead),
	).Get("/v1/analytics/widget", handler.summary)
}

func (h *TenantHTTPHandler) summary(w http.ResponseWriter, r *http.Request) {
	rangeValue, rangeDays := "30d", 30
	if raw := r.URL.Query().Get("range"); raw != "" {
		switch raw {
		case "7d":
			rangeValue, rangeDays = raw, 7
		case "30d":
			rangeValue, rangeDays = raw, 30
		case "90d":
			rangeValue, rangeDays = raw, 90
		default:
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
				"analytics query is invalid", map[string]string{"range": "must be 7d, 30d, or 90d"})
			return
		}
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID != "" {
		if _, err := uuid.Parse(projectID); err != nil {
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
				"analytics query is invalid", map[string]string{"project_id": "must be a UUID"})
			return
		}
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	summary, err := h.options.Store.Summary(r.Context(), query, resolved.ID, projectID, rangeDays, now)
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"range":   rangeValue,
		"summary": summary,
	})
}
