package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type HTTPOptions struct {
	Store            Store
	JWT              *auth.JWTManager
	Tester           Tester
	Limiter          ratelimit.Limiter
	Allowed          func(string, string) bool
	ManagedAvailable bool
	ManagedModel     string
	AWSCallerARN     string
}
type handler struct{ HTTPOptions }

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	h := handler{options}
	base := []func(http.Handler) http.Handler{appmiddleware.RequireActive, appmiddleware.RequireTenantMember(options.JWT)}
	read := append(append([]func(http.Handler) http.Handler{}, base...), appmiddleware.RequirePermission(auth.PermissionAIConnectionRead))
	manage := append(append([]func(http.Handler) http.Handler{}, base...), appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionAIConnectionManage))
	router.With(read...).Get("/v1/ai/providers", h.providers)
	router.With(read...).Get("/v1/ai/connections", h.list)
	router.With(manage...).Post("/v1/ai/connections", h.create)
	router.With(manage...).Patch("/v1/ai/connections/{connectionID}", h.update)
	router.With(manage...).Delete("/v1/ai/connections/{connectionID}", h.remove)
	router.With(read...).Get("/v1/ai/connections/{connectionID}/models", h.models)
	router.With(manage...).Post("/v1/ai/connections/{connectionID}/validate", h.validate)
}
func (h handler) allowed(tenant, provider string) bool {
	return h.Allowed != nil && h.Allowed(tenant, provider)
}
func state(w http.ResponseWriter, r *http.Request) (string, string, db.Querier, bool) {
	t, tok := appmiddleware.TenantFromContext(r.Context())
	q, qok := appmiddleware.QuerierFromContext(r.Context())
	claims, cok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tok || !qok || !cok {
		writeError(w, errors.New("missing request state"))
		return "", "", nil, false
	}
	if !config.CanUseFeature(t, config.FeatureAI) {
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanFeatureUnavailable, "AI is not available on this plan", nil)
		return "", "", nil, false
	}
	return t.ID, claims.Subject, q, true
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "invalid request body", nil)
		return false
	}
	return true
}
func writeError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, api.CodeInternal, "AI connection operation failed"
	switch {
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, api.CodeNotFound, ErrNotFound.Error()
	case errors.Is(err, ErrConflict):
		status, code, message = http.StatusConflict, api.CodeConflict, ErrConflict.Error()
	case errors.Is(err, ErrInvalid) || errors.Is(err, ErrUnvalidated) || errors.Is(err, ErrDisabled):
		status, code, message = http.StatusUnprocessableEntity, api.CodeValidation, err.Error()
	}
	var failure *Failure
	if errors.As(err, &failure) {
		status, code, message = http.StatusUnprocessableEntity, api.CodeValidation, failure.Error()
	}
	api.WriteError(w, status, code, message, nil)
}
func (h handler) providers(w http.ResponseWriter, r *http.Request) {
	tenant, _, _, ok := state(w, r)
	if !ok {
		return
	}
	providers := Providers()
	for i := range providers {
		providers[i].Enabled = h.allowed(tenant, providers[i].ID)
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"providers": providers, "managed": map[string]any{"available": h.ManagedAvailable, "model": h.ManagedModel}, "aws_caller_arn": h.AWSCallerARN})
}
func (h handler) list(w http.ResponseWriter, r *http.Request) {
	tenant, _, q, ok := state(w, r)
	if !ok {
		return
	}
	connections, err := h.Store.List(r.Context(), q, tenant)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"connections": connections})
}
func (h handler) create(w http.ResponseWriter, r *http.Request) {
	tenant, actor, q, ok := state(w, r)
	if !ok {
		return
	}
	var input Input
	if !decode(w, r, &input) {
		return
	}
	if !h.allowed(tenant, input.Provider) {
		writeError(w, ErrDisabled)
		return
	}
	connection, err := h.Store.Create(r.Context(), q, tenant, actor, input)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"connection": connection})
}
func (h handler) update(w http.ResponseWriter, r *http.Request) {
	tenant, actor, q, ok := state(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "connectionID")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, ErrNotFound)
		return
	}
	var input Input
	if !decode(w, r, &input) {
		return
	}
	if input.Enabled && !h.allowed(tenant, input.Provider) {
		writeError(w, ErrDisabled)
		return
	}
	connection, err := h.Store.Update(r.Context(), q, tenant, actor, id, input)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"connection": connection})
}
func (h handler) remove(w http.ResponseWriter, r *http.Request) {
	tenant, actor, q, ok := state(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "connectionID")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, ErrNotFound)
		return
	}
	status, err := h.Store.Delete(r.Context(), q, tenant, actor, id)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"status": status})
}
func (h handler) remoteState(w http.ResponseWriter, r *http.Request) (string, db.Querier, Connection, Secret, bool) {
	tenant, actor, q, ok := state(w, r)
	if !ok {
		return "", nil, Connection{}, Secret{}, false
	}
	id := chi.URLParam(r, "connectionID")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, ErrNotFound)
		return "", nil, Connection{}, Secret{}, false
	}
	c, err := h.Store.Get(r.Context(), q, tenant, id)
	if err != nil {
		writeError(w, err)
		return "", nil, c, Secret{}, false
	}
	if !h.allowed(tenant, c.Provider) || h.Tester == nil {
		writeError(w, ErrDisabled)
		return "", nil, c, Secret{}, false
	}
	if h.Limiter != nil {
		d, err := h.Limiter.Allow(r.Context(), "ai-setup:"+tenant, ratelimit.Window{Limit: 10, Period: time.Minute})
		if err != nil || !d.Allowed {
			api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded, "connection setup rate limit reached", nil)
			return "", nil, c, Secret{}, false
		}
	}
	secret, err := h.Store.Credential(r.Context(), q, c)
	if err != nil {
		writeError(w, err)
		return "", nil, c, secret, false
	}
	return actor, q, c, secret, true
}
func (h handler) models(w http.ResponseWriter, r *http.Request) {
	_, _, c, secret, ok := h.remoteState(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	models, err := h.Tester.Models(ctx, c, secret)
	if err != nil {
		api.WriteJSON(w, http.StatusOK, map[string]any{"models": c.Models, "discovery_available": false, "manual_model_supported": true})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"models": models, "discovery_available": true, "manual_model_supported": true})
}
func (h handler) validate(w http.ResponseWriter, r *http.Request) {
	actor, q, c, secret, ok := h.remoteState(w, r)
	if !ok {
		return
	}
	var input struct {
		Model  string   `json:"model"`
		Inputs []string `json:"input_categories"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !ValidModelID(input.Model, false) || len(input.Inputs) == 0 || len(input.Inputs) > 4 {
		writeError(w, ErrInvalid)
		return
	}
	p, _ := Lookup(c.Provider)
	for _, inputCategory := range input.Inputs {
		if !Supports(p.Inputs, inputCategory) {
			writeError(w, ErrInvalid)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	model, failure := h.Tester.Validate(ctx, c, secret, input.Model, input.Inputs)
	if failure != nil {
		model.ID = input.Model
	}
	if err := h.Store.RecordValidation(r.Context(), q, c, actor, model, failure); err != nil {
		writeError(w, err)
		return
	}
	// A successful HTTP transaction commits the safe validation status even when
	// the provider rejected the synthetic request.
	if failure != nil {
		code := "validation_failed"
		var f *Failure
		if errors.As(failure, &f) {
			code = f.Code
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{"valid": false, "failure_code": code})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"valid": true, "model": model})
}
