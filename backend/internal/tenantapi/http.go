package tenantapi

import (
	"context"
	"net/http"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

type ContextResolver func(context.Context) (tenant.Tenant, bool)

func NewHTTPHandler(resolvers ...ContextResolver) http.Handler {
	router := chi.NewRouter()
	RegisterRoutes(router, resolvers...)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	})
	return router
}

func RegisterRoutes(router chi.Router, resolvers ...ContextResolver) {
	resolve := appmiddleware.TenantFromContext
	if len(resolvers) > 0 {
		resolve = resolvers[0]
		router.Get("/v1/config", configHandler(resolve))
		return
	}
	router.With(appmiddleware.RequireActive).Get("/v1/config", configHandler(resolve))
}

func configHandler(resolve ContextResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resolved, ok := resolve(r.Context())
		if !ok {
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}
		definition, exists := config.PlanFor(resolved.Plan)
		if !exists {
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"tenant": map[string]any{
				"id": resolved.ID, "name": resolved.Name, "slug": resolved.Slug,
				"plan": resolved.Plan, "status": resolved.Status, "trial_ends_at": resolved.TrialEndsAt,
				"deletion_requested_at":      resolved.DeletionRequestedAt,
				"reporter_access_enabled":    resolved.ReporterAccessEnabled,
				"widget_appearance_defaults": resolved.WidgetAppearanceDefaults,
			},
			"plan": definition,
		})
	}
}
