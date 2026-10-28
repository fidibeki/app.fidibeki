package billing

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

type HTTPOptions struct {
	Service Service
	JWT     *auth.JWTManager
}

type HTTPHandler struct{ options HTTPOptions }

func RegisterPlatformRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	router.Get("/v1/plans", handler.plans)
	router.Post("/v1/signup", handler.signup)
	router.Post("/v1/billing/webhooks/stripe", handler.webhook)
}

func (h *HTTPHandler) plans(w http.ResponseWriter, _ *http.Request) {
	api.WriteJSON(w, http.StatusOK, map[string]any{"plans": config.SelfServicePlans()})
}

func RegisterTenantRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	// Billing deliberately omits RequireActive: a suspended or expired
	// workspace must still be able to inspect billing and recover service.
	// RequireWritable keeps deletion-grace workspaces read-only except for the
	// explicit cancel-deletion action below.
	read := []func(http.Handler) http.Handler{
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequireAnyPermission(auth.PermissionBillingRead, auth.PermissionBillingManage),
	}
	manage := []func(http.Handler) http.Handler{
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionBillingManage),
	}
	writableManage := append(manage, appmiddleware.RequireWritable)
	router.With(read...).Get("/v1/billing", handler.getBilling)
	router.With(writableManage...).Post("/v1/billing/checkout", handler.checkout)
	router.With(writableManage...).Post("/v1/billing/portal", handler.portal)
	router.With(writableManage...).Post("/v1/billing/delete-account", handler.deleteAccount)
	router.With(manage...).Post("/v1/billing/cancel-deletion", handler.cancelDeletion)
}

func (h *HTTPHandler) signup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name      string      `json:"name"`
		Slug      string      `json:"slug"`
		Email     string      `json:"email"`
		Plan      tenant.Plan `json:"plan"`
		Turnstile string      `json:"turnstile_token"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	created, err := h.options.Service.Signup(r.Context(), SignupInput{
		Name: request.Name, Slug: request.Slug, Email: request.Email, Plan: request.Plan,
	}, request.Turnstile, remoteIP(r.RemoteAddr), appmiddleware.RequestIDFromContext(r.Context()))
	if err != nil {
		writeBillingError(w, err)
		return
	}
	location, err := h.options.Service.tenantURL(created.Slug, "/login")
	if err == nil {
		w.Header().Set("Location", location)
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"tenant": map[string]any{
			"id": created.ID, "name": created.Name, "slug": created.Slug,
			"plan": created.Plan, "status": created.Status, "trial_ends_at": created.TrialEndsAt,
		},
	})
}

func (h *HTTPHandler) webhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return
	}
	_, err = h.options.Service.HandleWebhook(r.Context(), payload, r.Header.Get("Stripe-Signature"))
	if err != nil {
		if errors.Is(err, ErrInvalidWebhook) {
			api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "webhook signature or payload is invalid", nil)
			return
		}
		writeBillingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) getBilling(w http.ResponseWriter, r *http.Request) {
	resolved, ok := appmiddleware.TenantFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	current, err := h.options.Service.Store.Get(r.Context(), resolved.ID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	var interval *BillingInterval
	if current.BillingInterval != "" {
		interval = &current.BillingInterval
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"billing": map[string]any{
			"plan": current.Plan, "pending_plan": current.PendingPlan, "status": current.Status,
			"billing_interval":      interval,
			"trial_ends_at":         current.TrialEndsAt,
			"has_customer":          current.StripeCustomerID != "",
			"has_subscription":      current.StripeSubscriptionID != "",
			"deletion_requested_at": current.DeletionRequestedAt,
		},
	})
}

func (h *HTTPHandler) checkout(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Plan            tenant.Plan     `json:"plan"`
		BillingInterval BillingInterval `json:"billing_interval"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.BillingInterval == "" {
		request.BillingInterval = IntervalMonthly
	}
	resolved, ok := appmiddleware.TenantFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	checkoutURL, err := h.options.Service.Checkout(r.Context(), resolved.ID, request.Plan, request.BillingInterval)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"url": checkoutURL})
}

func (h *HTTPHandler) portal(w http.ResponseWriter, r *http.Request) {
	resolved, ok := appmiddleware.TenantFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	portalURL, err := h.options.Service.Portal(r.Context(), resolved.ID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"url": portalURL})
}

func (h *HTTPHandler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Confirmation string `json:"confirmation"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tenantOK || !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	executesAt, err := h.options.Service.RequestDeletion(
		r.Context(), resolved.ID, claims.Subject, request.Confirmation,
	)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]any{"deletion_scheduled_at": executesAt})
}

func (h *HTTPHandler) cancelDeletion(w http.ResponseWriter, r *http.Request) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tenantOK || !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	if err := h.options.Service.CancelDeletion(r.Context(), resolved.ID, claims.Subject); err != nil {
		writeBillingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body must contain one JSON value", nil)
		return false
	}
	return true
}

func remoteIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return ""
	}
	return host
}

func writeBillingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTurnstile):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "Turnstile verification failed", nil)
	case errors.Is(err, ErrSlugConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "tenant slug already exists", nil)
	case errors.Is(err, ErrUnsupportedPlan):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "plan is invalid", nil)
	case errors.Is(err, ErrUnsupportedInterval):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "billing interval is invalid", nil)
	case errors.Is(err, ErrAlreadySubscribed), errors.Is(err, ErrDeletionPending):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, err.Error(), nil)
	case errors.Is(err, ErrConfirmation):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "confirmation must match the tenant slug", nil)
	case errors.Is(err, ErrDeletionNotPending), errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	case errors.Is(err, ErrProviderUnavailable):
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "billing is unavailable", nil)
	case strings.Contains(err.Error(), "invalid"):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, err.Error(), nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}
