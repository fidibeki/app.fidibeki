package integration

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type HTTPOptions struct {
	Store             Store
	JWT               *auth.JWTManager
	AllowInsecureURLs bool
	// Slack enables the one-click install path. Left zero, the endpoints report that the
	// deployment has no Slack app and the manual path keeps working unchanged.
	Slack  SlackOAuth
	Logger *slog.Logger
}

type HTTPHandler struct {
	options HTTPOptions
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIntegrationCreate))...).
		Post("/v1/integrations", handler.create)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIntegrationCreate))...).
		Post("/v1/integrations/slack/install", handler.slackInstall)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIntegrationRead))...).
		Get("/v1/integrations", handler.list)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIntegrationUpdate))...).
		Patch("/v1/integrations/{integrationID}/privacy", handler.updatePrivacy)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIntegrationDelete))...).
		Delete("/v1/integrations/{integrationID}", handler.delete)
}

func (h *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := integrationRequestState(w, r)
	if !ok || !requireIntegrationPlan(w, resolved) {
		return
	}
	var input CreateInput
	if !decodeIntegrationJSON(w, r, &input) {
		return
	}
	values, fields := ValidateCreate(input, h.options.AllowInsecureURLs)
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"request validation failed", map[string]any{"fields": fields})
		return
	}
	values.ID = uuid.NewString()
	if values.Kind == KindWebhook {
		secret, err := GenerateWebhookSecret()
		if err != nil {
			writeIntegrationError(w, err)
			return
		}
		values.WebhookSecret = secret
	}
	created, err := h.options.Store.Create(r.Context(), query, resolved.ID, values)
	if err != nil {
		writeIntegrationError(w, err)
		return
	}
	response := map[string]any{"integration": created}
	if values.WebhookSecret != "" {
		response["webhook_secret"] = values.WebhookSecret
	}
	api.WriteJSON(w, http.StatusCreated, response)
}

func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := integrationRequestState(w, r)
	if !ok {
		return
	}
	integrations, err := h.options.Store.List(r.Context(), query, resolved.ID)
	if err != nil {
		writeIntegrationError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"integrations": integrations})
}

func (h *HTTPHandler) updatePrivacy(w http.ResponseWriter, r *http.Request) {
	integrationID := chi.URLParam(r, "integrationID")
	if _, err := uuid.Parse(integrationID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	resolved, query, ok := integrationRequestState(w, r)
	if !ok || !requireIntegrationPlan(w, resolved) {
		return
	}
	var input UpdatePrivacyInput
	if !decodeIntegrationJSON(w, r, &input) {
		return
	}
	privacyFields, fields := ValidatePrivacyUpdate(input)
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"request validation failed", map[string]any{"fields": fields})
		return
	}
	updated, err := h.options.Store.UpdatePrivacy(
		r.Context(), query, resolved.ID, integrationID, privacyFields,
	)
	if err != nil {
		writeIntegrationError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"integration": updated})
}

func (h *HTTPHandler) delete(w http.ResponseWriter, r *http.Request) {
	integrationID := chi.URLParam(r, "integrationID")
	if _, err := uuid.Parse(integrationID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	resolved, query, ok := integrationRequestState(w, r)
	if !ok {
		return
	}
	// Read the token and queue the revoke before the row disappears; the Slack call itself
	// runs after this transaction commits.
	oauthInstall := h.scheduleSlackRevoke(r.Context(), query, resolved.ID, integrationID)
	if err := h.options.Store.Delete(r.Context(), query, resolved.ID, integrationID); err != nil {
		writeIntegrationError(w, err)
		return
	}
	if oauthInstall && !h.auditSlackUninstall(w, r, query, resolved.ID, integrationID) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func integrationRequestState(w http.ResponseWriter, r *http.Request) (tenant.Tenant, db.Querier, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return tenant.Tenant{}, nil, false
	}
	return resolved, query, true
}

func requireIntegrationPlan(w http.ResponseWriter, resolved tenant.Tenant) bool {
	if !config.CanUseFeature(resolved, config.FeatureSlack) {
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanFeatureUnavailable,
			"integrations are not available on this plan", map[string]string{"upgrade_url": "/v1/billing"})
		return false
	}
	return true
}

func decodeIntegrationJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest,
			"request body must contain one JSON value", nil)
		return false
	}
	return true
}

func writeIntegrationError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
