package emailfeedback

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type HTTPOptions struct {
	Store Store
	JWT   *auth.JWTManager
}

type httpHandler struct {
	options HTTPOptions
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	handler := &httpHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(
		protected, appmiddleware.RequirePermission(auth.PermissionProjectRead),
	)...).Get("/v1/email-feedback-campaigns", handler.list)
	router.With(append(
		protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionProjectUpdate),
	)...).Post("/v1/email-feedback-campaigns", handler.create)
	router.With(append(
		protected, appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionProjectUpdate),
	)...).Patch("/v1/email-feedback-campaigns/{campaignID}", handler.update)
}

func (handler *httpHandler) list(w http.ResponseWriter, r *http.Request) {
	resolved, query, _, ok := requestState(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID != "" {
		if _, err := uuid.Parse(projectID); err != nil {
			writeValidation(w, FieldErrors{"project_id": "must identify a project"})
			return
		}
	}
	campaigns, err := handler.options.Store.ListCampaigns(
		r.Context(), query, resolved.ID, projectID,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"campaigns": campaigns})
}

func (handler *httpHandler) create(w http.ResponseWriter, r *http.Request) {
	var input CampaignInput
	if !decodeJSON(w, r, &input) {
		return
	}
	values, fields := ValidateCampaign(input, true)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, claims, ok := requestState(w, r)
	if !ok {
		return
	}
	campaign, err := handler.options.Store.CreateCampaign(
		r.Context(), query, resolved.ID, claims.Subject, values,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"campaign": campaign})
}

func (handler *httpHandler) update(w http.ResponseWriter, r *http.Request) {
	campaignID := chi.URLParam(r, "campaignID")
	if _, err := uuid.Parse(campaignID); err != nil {
		writeNotFound(w)
		return
	}
	var input CampaignInput
	if !decodeJSON(w, r, &input) {
		return
	}
	values, fields := ValidateCampaign(input, false)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, claims, ok := requestState(w, r)
	if !ok {
		return
	}
	campaign, err := handler.options.Store.UpdateCampaign(
		r.Context(), query, resolved.ID, campaignID, claims.Subject, values,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"campaign": campaign})
}

func requestState(
	w http.ResponseWriter,
	r *http.Request,
) (tenant.Tenant, db.Querier, auth.Claims, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tenantOK || !queryOK || !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return tenant.Tenant{}, nil, auth.Claims{}, false
	}
	return resolved, query, claims, true
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

func writeValidation(w http.ResponseWriter, fields FieldErrors) {
	api.WriteError(
		w, http.StatusUnprocessableEntity, api.CodeValidation,
		"request validation failed", map[string]any{"fields": fields},
	)
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeNotFound(w)
	case errors.Is(err, ErrKeyConflict):
		api.WriteError(
			w, http.StatusConflict, api.CodeConflict,
			"campaign key is already in use", nil,
		)
	case errors.Is(err, ErrNotReady):
		api.WriteError(
			w, http.StatusUnprocessableEntity, api.CodeValidation,
			"campaign is not ready to activate", nil,
		)
	case errors.Is(err, ErrInvalidStatus), errors.Is(err, ErrArchived):
		api.WriteError(
			w, http.StatusConflict, api.CodeConflict,
			"campaign status transition is not allowed", nil,
		)
	default:
		api.WriteError(
			w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"service unavailable", nil,
		)
	}
}

func writeNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
}
