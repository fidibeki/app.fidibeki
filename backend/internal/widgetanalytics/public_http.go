package widgetanalytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
)

const maximumBodySize int64 = 64 << 10

type EventStore interface {
	InsertEvents(rctx context.Context, query db.Querier, events []EventValues) (IngestResult, error)
}

type PublicHTTPOptions struct {
	Authorizer widgetapi.ProjectAuthorizer
	Begin      db.BeginFunc
	Store      EventStore
	Logger     *slog.Logger
	Now        func() time.Time
}

type PublicHTTPHandler struct {
	options PublicHTTPOptions
	logger  *slog.Logger
}

type ingestRequest struct {
	Events []EventInput `json:"events"`
}

func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &PublicHTTPHandler{options: options, logger: logger}
	router.Post("/v1/widget/events", handler.ingest)
	router.Options("/v1/widget/events", handler.preflight)
}

func (h *PublicHTTPHandler) preflight(w http.ResponseWriter, r *http.Request) {
	_, origin, ok := h.authorize(w, r)
	if !ok {
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+widgetapi.PublicKeyHeader)
	w.Header().Set("Access-Control-Max-Age", "300")
	w.WriteHeader(http.StatusNoContent)
}

func (h *PublicHTTPHandler) ingest(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorize(w, r)
	if !ok {
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	if configuration.DeletionRequestedAt != nil {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden,
			"tenant is pending deletion and is read-only", nil)
		return
	}
	var request ingestRequest
	if !decodeIngestRequest(w, r, &request) {
		return
	}
	values, fields := ValidateEvents(request.Events, configuration, origin, h.now())
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"widget analytics events are invalid", fields)
		return
	}
	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("begin widget analytics transaction: %w", err))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	var configuredTenant string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID).
		Scan(&configuredTenant); err != nil || configuredTenant != configuration.TenantID {
		h.writeStoreError(w, r, errors.New("configure widget analytics tenant context"))
		return
	}
	result, err := h.options.Store.InsertEvents(r.Context(), tx, values)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if result.AcceptedEventIDs == nil {
		result.AcceptedEventIDs = []string{}
	}
	if result.DuplicateEventIDs == nil {
		result.DuplicateEventIDs = []string{}
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit widget analytics transaction: %w", err))
		return
	}
	committed = true
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, http.StatusAccepted, result)
}

func (h *PublicHTTPHandler) authorize(
	w http.ResponseWriter,
	r *http.Request,
) (project.PublicConfiguration, string, bool) {
	headerKey := r.Header.Get(widgetapi.PublicKeyHeader)
	queryKey := r.URL.Query().Get("key")
	if headerKey != "" && queryKey != "" && headerKey != queryKey {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	publicKey := headerKey
	if publicKey == "" {
		publicKey = queryKey
	}
	origin := r.Header.Get("Origin")
	if publicKey == "" || !widgetapi.ValidOrigin(origin) {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	configuration, err := h.options.Authorizer.Authorize(r.Context(), publicKey, origin)
	if errors.Is(err, widgetapi.ErrAccessDenied) {
		writeAccessDenied(w)
		return project.PublicConfiguration{}, "", false
	}
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("authorize widget analytics: %w", err))
		return project.PublicConfiguration{}, "", false
	}
	return configuration, origin, true
}

func decodeIngestRequest(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maximumBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "invalid JSON request body", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "invalid JSON request body", nil)
		return false
	}
	return true
}

func (h *PublicHTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *PublicHTTPHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "widget analytics request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func writeAccessDenied(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget access denied", nil)
}
