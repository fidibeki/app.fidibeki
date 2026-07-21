package middleware

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
)

type UsageCounter func(context.Context, tenant.Tenant) (int, error)

type PlanLimitOptions struct {
	Feature    config.Feature
	Limit      config.Limit
	CountUsage UsageCounter
	UpgradeURL string
	Logger     *slog.Logger
}

func RequirePlanLimit(options PlanLimitOptions) func(http.Handler) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resolved, ok := TenantFromContext(r.Context())
			if !ok {
				api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
				return
			}
			definition, exists := config.PlanFor(resolved.Plan)
			if !exists {
				logger.ErrorContext(r.Context(), "tenant has unknown plan", "tenant_id", resolved.ID, "plan", resolved.Plan)
				api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
				return
			}
			if options.Feature != "" && !definition.Features.Enabled(options.Feature) {
				api.WriteError(w, http.StatusPaymentRequired, api.CodePlanFeatureUnavailable,
					"feature is not available on this plan", map[string]any{
						"feature": options.Feature, "plan": resolved.Plan, "upgrade_url": options.UpgradeURL,
					})
				return
			}
			if options.Limit != "" {
				maximum, valid := definition.Limits.Value(options.Limit)
				if !valid || options.CountUsage == nil {
					logger.ErrorContext(r.Context(), "invalid plan limit middleware configuration", "limit", options.Limit)
					api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
					return
				}
				if maximum >= 0 {
					used, err := options.CountUsage(r.Context(), resolved)
					if err != nil {
						logger.ErrorContext(r.Context(), "plan usage lookup failed", "error", err, "limit", options.Limit)
						api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
						return
					}
					if used >= maximum {
						api.WriteError(w, http.StatusPaymentRequired, api.CodePlanLimitExceeded,
							"plan limit exceeded", map[string]any{
								"limit": options.Limit, "maximum": maximum, "used": used,
								"plan": resolved.Plan, "upgrade_url": options.UpgradeURL,
							})
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
