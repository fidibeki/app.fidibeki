package ai

import (
	"context"
	"errors"
	"net/http"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type FeedbackScheduler interface {
	ScheduleFeedback(context.Context, string) error
}

type HTTPOptions struct {
	Store     Store
	JWT       *auth.JWTManager
	Scheduler FeedbackScheduler
}

type HTTPHandler struct {
	options HTTPOptions
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireWritable,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionFeedbackUpdate),
	}
	router.With(protected...).Post("/v1/attachments/{attachmentID}/retry", handler.retryAttachment)
	router.With(protected...).Post("/v1/feedbacks/{feedbackID}/reprocess", handler.reprocessFeedback)
}

func (h *HTTPHandler) retryAttachment(w http.ResponseWriter, r *http.Request) {
	attachmentID, ok := pathUUID(w, r, "attachmentID")
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok || !requireAI(w, resolved) {
		return
	}
	feedbackID, err := h.options.Store.RetryAttachment(r.Context(), query, resolved.ID, attachmentID)
	if err != nil {
		writeError(w, err)
		return
	}
	if !appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
		return h.options.Scheduler.ScheduleFeedback(ctx, feedbackID)
	}) {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (h *HTTPHandler) reprocessFeedback(w http.ResponseWriter, r *http.Request) {
	feedbackID, ok := pathUUID(w, r, "feedbackID")
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok || !requireAI(w, resolved) {
		return
	}
	if err := h.options.Store.ReprocessFeedback(r.Context(), query, resolved.ID, feedbackID); err != nil {
		writeError(w, err)
		return
	}
	if !appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
		return h.options.Scheduler.ScheduleFeedback(ctx, feedbackID)
	}) {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func requestState(w http.ResponseWriter, r *http.Request) (tenant.Tenant, db.Querier, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return tenant.Tenant{}, nil, false
	}
	return resolved, query, true
}

func requireAI(w http.ResponseWriter, resolved tenant.Tenant) bool {
	if !config.CanUseFeature(resolved, config.FeatureAI) {
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanFeatureUnavailable,
			"AI processing is not available on this plan", map[string]string{"upgrade_url": "/v1/billing"})
		return false
	}
	return true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := chi.URLParam(r, name)
	if _, err := uuid.Parse(value); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return "", false
	}
	return value, true
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	if errors.Is(err, ErrAlreadyRunning) {
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "AI processing is already running", nil)
		return
	}
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
