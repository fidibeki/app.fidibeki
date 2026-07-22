package triage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var tagColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type CatalogueStore interface {
	ListTags(context.Context, db.Querier, string) ([]Tag, error)
	CreateTag(context.Context, db.Querier, string, string, string) (Tag, error)
	UpdateTag(context.Context, db.Querier, string, string, *string, *string) (Tag, error)
	DeleteTag(context.Context, db.Querier, string, string) error
}

type HTTPOptions struct {
	Store CatalogueStore
	JWT   *auth.JWTManager
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
	router.With(protected...).Get("/v1/tags", handler.list)
	write := append(protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionFeedbackUpdate),
	)
	router.With(write...).Post("/v1/tags", handler.create)
	router.With(write...).Patch("/v1/tags/{tagID}", handler.update)
	router.With(write...).Delete("/v1/tags/{tagID}", handler.delete)
}

func (handler *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	tenantID, query, ok := triageRequestState(w, r)
	if !ok {
		return
	}
	tags, err := handler.options.Store.ListTags(r.Context(), query, tenantID)
	if err != nil {
		writeTriageStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (handler *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decodeTriageJSON(w, r, &request) {
		return
	}
	name, color, fields := validateTagValues(&request.Name, &request.Color)
	if len(fields) > 0 {
		writeTriageValidation(w, fields)
		return
	}
	tenantID, query, ok := triageRequestState(w, r)
	if !ok {
		return
	}
	created, err := handler.options.Store.CreateTag(r.Context(), query, tenantID, *name, *color)
	if err != nil {
		writeTriageStoreError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/tags/"+created.ID)
	api.WriteJSON(w, http.StatusCreated, map[string]any{"tag": created})
}

func (handler *HTTPHandler) update(w http.ResponseWriter, r *http.Request) {
	tagID, ok := tagPathID(w, r)
	if !ok {
		return
	}
	var request struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if !decodeTriageJSON(w, r, &request) {
		return
	}
	if request.Name == nil && request.Color == nil {
		writeTriageValidation(w, map[string]string{"request": "must include name or color"})
		return
	}
	name, color, fields := validateTagValues(request.Name, request.Color)
	if len(fields) > 0 {
		writeTriageValidation(w, fields)
		return
	}
	tenantID, query, ok := triageRequestState(w, r)
	if !ok {
		return
	}
	updated, err := handler.options.Store.UpdateTag(r.Context(), query, tenantID, tagID, name, color)
	if err != nil {
		writeTriageStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"tag": updated})
}

func (handler *HTTPHandler) delete(w http.ResponseWriter, r *http.Request) {
	tagID, ok := tagPathID(w, r)
	if !ok {
		return
	}
	tenantID, query, ok := triageRequestState(w, r)
	if !ok {
		return
	}
	if err := handler.options.Store.DeleteTag(r.Context(), query, tenantID, tagID); err != nil {
		writeTriageStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateTagValues(nameInput, colorInput *string) (*string, *string, map[string]string) {
	fields := make(map[string]string)
	var name, color *string
	if nameInput != nil {
		trimmed := strings.TrimSpace(*nameInput)
		if length := len([]rune(trimmed)); length < 1 || length > 40 {
			fields["name"] = "must be between 1 and 40 characters"
		} else {
			name = &trimmed
		}
	}
	if colorInput != nil {
		trimmed := strings.TrimSpace(*colorInput)
		if !tagColorPattern.MatchString(trimmed) {
			fields["color"] = "must use #RRGGBB format"
		} else {
			normalized := strings.ToUpper(trimmed)
			color = &normalized
		}
	}
	return name, color, fields
}

func tagPathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := chi.URLParam(r, "tagID")
	if _, err := uuid.Parse(value); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return "", false
	}
	return value, true
}

func triageRequestState(w http.ResponseWriter, r *http.Request) (string, db.Querier, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return "", nil, false
	}
	return resolved.ID, query, true
}

func decodeTriageJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
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

func writeTriageValidation(w http.ResponseWriter, fields map[string]string) {
	api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "request validation failed", map[string]any{
		"fields": fields,
	})
}

func writeTriageStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTagNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	case errors.Is(err, ErrTagConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "tag name already exists", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}
