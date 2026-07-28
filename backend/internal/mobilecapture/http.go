package mobilecapture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

type TenantHTTPOptions struct {
	Store         Store
	JWT           *auth.JWTManager
	Now           func() time.Time
	GenerateToken func() (raw, hash string, err error)
	Logger        *slog.Logger
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
		appmiddleware.RequireTenantAPIKey,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionMobileCaptureCreate),
	).Post("/v1/mobile/capture-sessions", handler.createSession)
}

func (handler *tenantHTTPHandler) createSession(w http.ResponseWriter, r *http.Request) {
	var request CreateSessionRequest
	if !decodeJSON(w, r, &request, false) {
		return
	}
	values, fields := ValidateCreateSession(request)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolvedTenant, query, claims, ok := tenantRequestState(w, r)
	if !ok {
		return
	}
	if err := handler.options.Store.LockDefinitionForMint(
		r.Context(), query, resolvedTenant.ID, values.ProjectID,
		values.WidgetDefinitionKey,
	); errors.Is(err, ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	} else if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	configuration, err := handler.options.Store.LoadConfiguration(
		r.Context(), query, resolvedTenant.ID, values.ProjectID,
	)
	if errors.Is(err, ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	resolved, fields := ResolveConfiguration(configuration, values)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	values.DisplayLocale = resolved.DisplayLocale
	now := handler.now().UTC()
	controllerVersion := resolved.Configuration.ControllerProfileVersion
	generate := handler.options.GenerateToken
	if generate == nil {
		generate = GenerateToken
	}
	for range 3 {
		rawToken, tokenHash, err := generate()
		if err != nil {
			handler.writeStoreError(w, r, err)
			return
		}
		if !validCaptureToken(rawToken) || tokenHash != auth.HashToken(rawToken) {
			handler.writeStoreError(w, r, errors.New("capture token generator returned an invalid token"))
			return
		}
		session, err := handler.options.Store.CreateSession(r.Context(), query, CaptureSession{
			TenantID: resolvedTenant.ID, ProjectID: values.ProjectID,
			WidgetRuleID: resolved.Definition.ID, IssuerAPIKeyID: claims.Subject,
			TokenHash: tokenHash, Platform: values.Platform,
			ApplicationID: values.ApplicationID, Screen: values.Screen,
			ContextURL: values.ContextURL, DisplayLocale: values.DisplayLocale,
			DefinitionVersion:        resolved.Definition.DefinitionVersion,
			ControllerProfileVersion: controllerVersion,
			SubmitExpiresAt:          now.Add(SubmitTTL),
			CleanupAfter:             now.Add(SubmitTTL).Add(CleanupGrace),
		})
		if errors.Is(err, ErrTokenConflict) {
			continue
		}
		if err != nil {
			handler.writeStoreError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		api.WriteJSON(w, http.StatusCreated, CreateSessionResponse{
			Token: rawToken, SubmitExpiresAt: session.SubmitExpiresAt,
			Configuration: PublicConfigurationFrom(resolved),
		})
		return
	}
	handler.writeStoreError(w, r, ErrTokenConflict)
}

func tenantRequestState(
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

func writeValidation(w http.ResponseWriter, fields FieldErrors) {
	api.WriteError(
		w, http.StatusUnprocessableEntity, api.CodeValidation,
		"request validation failed", map[string]any{"fields": fields},
	)
}

func (handler *tenantHTTPHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	handler.logger.ErrorContext(r.Context(), "mobile capture request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func (handler *tenantHTTPHandler) now() time.Time {
	if handler.options.Now != nil {
		return handler.options.Now()
	}
	return time.Now()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, recursiveStrict bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maximumRequestBody)
	if recursiveStrict {
		data, err := io.ReadAll(r.Body)
		if err != nil || rejectDuplicateJSONKeys(data) != nil {
			api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
			return false
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
	}
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
