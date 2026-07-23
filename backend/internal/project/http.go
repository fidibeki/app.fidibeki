package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type HTTPOptions struct {
	Store             Store
	JWT               *auth.JWTManager
	GeneratePublicKey func() (string, error)
}

type HTTPHandler struct {
	options HTTPOptions
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	createLimit := appmiddleware.RequirePlanLimit(appmiddleware.PlanLimitOptions{
		Limit: config.LimitProjects, UpgradeURL: "/v1/billing",
		CountUsage: func(ctx context.Context, resolved tenant.Tenant) (int, error) {
			query, ok := appmiddleware.QuerierFromContext(ctx)
			if !ok {
				return 0, errors.New("tenant query is unavailable")
			}
			return options.Store.CountProjects(ctx, query, resolved.ID)
		},
	})

	router.With(append(protected,
		appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionProjectCreate), createLimit,
	)...).Post("/v1/projects", handler.createProject)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionProjectRead))...).
		Get("/v1/projects", handler.listProjects)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionProjectRead))...).
		Get("/v1/projects/{projectID}", handler.getProject)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionProjectUpdate))...).
		Patch("/v1/projects/{projectID}", handler.updateProject)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionProjectDelete))...).
		Delete("/v1/projects/{projectID}", handler.deleteProject)

	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionWidgetRuleCreate))...).
		Post("/v1/projects/{projectID}/widget-rules", handler.createWidgetRule)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionWidgetRuleRead))...).
		Get("/v1/projects/{projectID}/widget-rules", handler.listWidgetRules)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionWidgetRuleRead))...).
		Get("/v1/projects/{projectID}/widget-rules/{ruleID}", handler.getWidgetRule)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionWidgetRuleUpdate))...).
		Patch("/v1/projects/{projectID}/widget-rules/{ruleID}", handler.updateWidgetRule)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionWidgetRuleDelete))...).
		Delete("/v1/projects/{projectID}/widget-rules/{ruleID}", handler.deleteWidgetRule)
}

func (h *HTTPHandler) createProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name           string   `json:"name"`
		AllowedDomains []string `json:"allowed_domains"`
		DefaultLocale  string   `json:"default_locale"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	name, domains, fields := ValidateProject(request.Name, request.AllowedDomains)
	defaultLocale, localeError := ValidateDefaultLocale(request.DefaultLocale)
	if localeError != "" {
		fields["default_locale"] = localeError
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	generator := h.options.GeneratePublicKey
	if generator == nil {
		generator = GeneratePublicKey
	}
	for range 3 {
		publicKey, err := generator()
		if err != nil {
			writeStoreError(w, err)
			return
		}
		created, err := h.options.Store.CreateProject(
			r.Context(), query, resolved.ID, name, publicKey, domains, defaultLocale,
		)
		if errors.Is(err, ErrPublicKeyConflict) {
			continue
		}
		if err != nil {
			writeStoreError(w, err)
			return
		}
		api.WriteJSON(w, http.StatusCreated, map[string]any{"project": created})
		return
	}
	writeStoreError(w, ErrPublicKeyConflict)
}

func (h *HTTPHandler) listProjects(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	projects, err := h.options.Store.ListProjects(r.Context(), query, resolved.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (h *HTTPHandler) getProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	found, err := h.options.Store.GetProject(r.Context(), query, resolved.ID, projectID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"project": found})
}

func (h *HTTPHandler) updateProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	var request struct {
		Name                      string                            `json:"name"`
		AllowedDomains            []string                          `json:"allowed_domains"`
		DefaultLocale             string                            `json:"default_locale"`
		ReporterAccessEnabled     json.RawMessage                   `json:"reporter_access_enabled"`
		WidgetAppearanceOverrides *widgetconfig.AppearanceOverrides `json:"widget_appearance_overrides"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	name, domains, fields := ValidateProject(request.Name, request.AllowedDomains)
	defaultLocale, localeError := ValidateDefaultLocale(request.DefaultLocale)
	if localeError != "" {
		fields["default_locale"] = localeError
	}
	setReporterAccess := request.ReporterAccessEnabled != nil
	var reporterAccessEnabled *bool
	if setReporterAccess && !bytes.Equal(bytes.TrimSpace(request.ReporterAccessEnabled), []byte("null")) {
		var enabled bool
		if err := json.Unmarshal(request.ReporterAccessEnabled, &enabled); err != nil {
			fields["reporter_access_enabled"] = "must be true, false, or null"
		} else {
			reporterAccessEnabled = &enabled
		}
	}
	setWidgetAppearance := request.WidgetAppearanceOverrides != nil
	widgetAppearance := widgetconfig.AppearanceOverrides{}
	if setWidgetAppearance {
		widgetAppearance = widgetconfig.Normalize(*request.WidgetAppearanceOverrides)
		for field, message := range widgetconfig.Validate(widgetAppearance) {
			fields["widget_appearance_overrides."+field] = message
		}
	}
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	updated, err := h.options.Store.UpdateProject(
		r.Context(), query, resolved.ID, projectID, name, domains, defaultLocale,
		setReporterAccess, reporterAccessEnabled,
		setWidgetAppearance, widgetAppearance,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"project": updated})
}

func (h *HTTPHandler) deleteProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if err := h.options.Store.DeleteProject(r.Context(), query, resolved.ID, projectID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) createWidgetRule(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	var input WidgetRuleInput
	if !decodeJSON(w, r, &input) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	values, fields := ValidateWidgetRule(input, resolved)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	created, err := h.options.Store.CreateWidgetRule(r.Context(), query, resolved.ID, projectID, values)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"widget_rule": created})
}

func (h *HTTPHandler) listWidgetRules(w http.ResponseWriter, r *http.Request) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if _, err := h.options.Store.GetProject(r.Context(), query, resolved.ID, projectID); err != nil {
		writeStoreError(w, err)
		return
	}
	rules, err := h.options.Store.ListWidgetRules(r.Context(), query, resolved.ID, projectID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"widget_rules": rules})
}

func (h *HTTPHandler) getWidgetRule(w http.ResponseWriter, r *http.Request) {
	projectID, ruleID, ok := widgetPathUUIDs(w, r)
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	rule, err := h.options.Store.GetWidgetRule(r.Context(), query, resolved.ID, projectID, ruleID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"widget_rule": rule})
}

func (h *HTTPHandler) updateWidgetRule(w http.ResponseWriter, r *http.Request) {
	projectID, ruleID, ok := widgetPathUUIDs(w, r)
	if !ok {
		return
	}
	var input WidgetRuleInput
	if !decodeJSON(w, r, &input) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	values, fields := ValidateWidgetRule(input, resolved)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	updated, err := h.options.Store.UpdateWidgetRule(
		r.Context(), query, resolved.ID, projectID, ruleID, values,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"widget_rule": updated})
}

func (h *HTTPHandler) deleteWidgetRule(w http.ResponseWriter, r *http.Request) {
	projectID, ruleID, ok := widgetPathUUIDs(w, r)
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if err := h.options.Store.DeleteWidgetRule(r.Context(), query, resolved.ID, projectID, ruleID); err != nil {
		writeStoreError(w, err)
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

func widgetPathUUIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	projectID, ok := pathUUID(w, r, "projectID")
	if !ok {
		return "", "", false
	}
	ruleID, ok := pathUUID(w, r, "ruleID")
	if !ok {
		return "", "", false
	}
	return projectID, ruleID, true
}

func pathUUID(w http.ResponseWriter, r *http.Request, parameter string) (string, bool) {
	value := chi.URLParam(r, parameter)
	if _, err := uuid.Parse(value); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return "", false
	}
	return value, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
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
	api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "request validation failed", map[string]any{
		"fields": fields,
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
