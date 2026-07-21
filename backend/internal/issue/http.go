package issue

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
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
	Store Store
	JWT   *auth.JWTManager
	Now   func() time.Time
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
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIssueRead))...).
		Get("/v1/issues", handler.list)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionIssueRead))...).
		Get("/v1/issues/{issueID}", handler.get)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueUpdate))...).
		Patch("/v1/issues/{issueID}", handler.updateStatus)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueMerge))...).
		Post("/v1/issues/{issueID}/merge", handler.merge)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionIssueMerge))...).
		Delete("/v1/issues/{issueID}/merge", handler.unmerge)
}

func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	issues, err := h.options.Store.List(r.Context(), query, resolved.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

func (h *HTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	found, err := h.options.Store.Get(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	updates, err := h.options.Store.ListUpdates(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": found, "updates": updates})
}

func (h *HTTPHandler) updateStatus(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	var request struct {
		Status  Status  `json:"status"`
		Comment *string `json:"comment"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if !request.Status.Valid() {
		writeValidation(w, map[string]string{"status": "must be open, in_progress, resolved, or closed"})
		return
	}
	if request.Comment != nil {
		trimmed := strings.TrimSpace(*request.Comment)
		if len(trimmed) > 2000 {
			writeValidation(w, map[string]string{"comment": "must be at most 2000 characters"})
			return
		}
		if trimmed == "" {
			request.Comment = nil
		} else {
			request.Comment = &trimmed
		}
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, ok := appmiddleware.TenantClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	found, update, err := h.options.Store.UpdateStatus(
		r.Context(), query, resolved.ID, issueID, claims.Subject,
		request.Status, request.Comment, now,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": found, "update": update})
}

func (h *HTTPHandler) merge(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	var request struct {
		TargetIssueID string `json:"target_issue_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if _, err := uuid.Parse(request.TargetIssueID); err != nil {
		writeValidation(w, map[string]string{"target_issue_id": "must be a UUID"})
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	found, err := h.options.Store.Merge(
		r.Context(), query, resolved.ID, issueID, request.TargetIssueID, now,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": found})
}

func (h *HTTPHandler) unmerge(w http.ResponseWriter, r *http.Request) {
	issueID, ok := issuePathID(w, r)
	if !ok {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	found, err := h.options.Store.Unmerge(r.Context(), query, resolved.ID, issueID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"issue": found})
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

func issuePathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := chi.URLParam(r, "issueID")
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

func writeValidation(w http.ResponseWriter, fields map[string]string) {
	api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "request validation failed", map[string]any{
		"fields": fields,
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	case errors.Is(err, ErrNoStatusChange):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "issue already has that status", nil)
	case errors.Is(err, ErrInvalidMerge):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "issues cannot be merged", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}
