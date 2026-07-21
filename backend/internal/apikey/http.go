package apikey

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type HTTPOptions struct {
	Store         Store
	JWT           *auth.JWTManager
	GenerateToken func() (raw, hash, prefix string, err error)
}

type HTTPHandler struct{ options HTTPOptions }

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionAPIKeyCreate))...).
		Post("/v1/api-keys", handler.create)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionAPIKeyRead))...).
		Get("/v1/api-keys", handler.list)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionAPIKeyDelete))...).
		Delete("/v1/api-keys/{apiKeyID}", handler.delete)
}

func (h *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if !decodeJSON(w, r, &input) {
		return
	}
	values, fields := ValidateCreate(input, time.Now())
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"request validation failed", map[string]any{"fields": fields})
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if !permissionSubset(values.Permissions, claims.Permissions) {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"permissions exceed the current member permissions", nil)
		return
	}
	generator := h.options.GenerateToken
	if generator == nil {
		generator = GenerateToken
	}
	for range 3 {
		raw, hash, prefix, err := generator()
		if err != nil {
			writeError(w, err)
			return
		}
		values.TokenHash = hash
		values.TokenPrefix = prefix
		created, err := h.options.Store.Create(r.Context(), query, resolved.ID, values)
		if errors.Is(err, ErrTokenConflict) {
			continue
		}
		if err != nil {
			writeError(w, err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, map[string]any{"api_key": created, "token": raw})
		return
	}
	writeError(w, ErrTokenConflict)
}

func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	keys, err := h.options.Store.List(r.Context(), query, resolved.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"api_keys": keys})
}

func (h *HTTPHandler) delete(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "apiKeyID")
	if _, err := uuid.Parse(keyID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if err := h.options.Store.Delete(r.Context(), query, resolved.ID, keyID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func permissionSubset(requested, granted []string) bool {
	allowed := make(map[string]struct{}, len(granted))
	for _, permission := range granted {
		allowed[permission] = struct{}{}
	}
	for _, permission := range requested {
		if _, exists := allowed[permission]; !exists {
			return false
		}
	}
	return true
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

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
