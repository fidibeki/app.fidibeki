package accessaudit

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
)

// EventStore is the read-side dependency the HTTP handler needs. It is
// satisfied by Store.
type EventStore interface {
	ListEvents(context.Context, db.Querier, string, ListFilter) ([]Event, error)
	DetectAnomalies(context.Context, db.Querier, string, AnomalySignalParams) ([]ActorSignal, error)
	DetectRevokedCredentialAccess(context.Context, db.Querier, string, RevokedCredentialParams) ([]ActorSignal, error)
	DetectUnusualAPIKeyAccess(context.Context, db.Querier, string, UnusualAPIKeyParams) ([]ActorSignal, error)
	DetectSupportAccessVolume(context.Context, db.Querier, string, SupportAccessVolumeParams) ([]ActorSignal, error)
}

type TokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

type HTTPOptions struct {
	Store  EventStore
	JWT    TokenVerifier
	Now    func() time.Time
	Logger *slog.Logger
}

type httpHandler struct {
	options HTTPOptions
	logger  *slog.Logger
}

// RegisterTenantRoutes mounts GET /v1/access-events for tenant administrators
// (gated on auth.PermissionAuditRead) — the bounded, tenant-visible view of
// the sensitive-data access ledger required by the access-governance audit
// contract.
func RegisterTenantRoutes(router chi.Router, options HTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &httpHandler{options: options, logger: logger}
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionAuditRead),
	).Get("/v1/access-events", handler.list)
}

func (h *httpHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now()
}

func (h *httpHandler) list(w http.ResponseWriter, r *http.Request) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	isCSV := r.URL.Query().Get("format") == "csv"
	filter, err := parseListFilter(r, isCSV)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, err.Error(), nil)
		return
	}
	events, err := h.options.Store.ListEvents(r.Context(), query, resolved.ID, filter)
	if errors.Is(err, ErrInvalidCursor) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "invalid cursor", nil)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	hasMore := len(events) > filter.Limit
	if hasMore {
		events = events[:filter.Limit]
	}
	if isCSV {
		writeEventsCSV(w, events)
		return
	}
	var nextCursor *string
	if hasMore && len(events) > 0 {
		encoded := EncodeCursor(events[len(events)-1])
		nextCursor = &encoded
	}
	signals, err := h.options.Store.DetectAnomalies(r.Context(), query, resolved.ID, AnomalySignalParams{Now: h.now()})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	revokedCredentialSignals, err := h.options.Store.DetectRevokedCredentialAccess(
		r.Context(), query, resolved.ID, RevokedCredentialParams{Now: h.now()})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	unusualAPIKeySignals, err := h.options.Store.DetectUnusualAPIKeyAccess(
		r.Context(), query, resolved.ID, UnusualAPIKeyParams{Now: h.now()})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	supportAccessVolumeSignals, err := h.options.Store.DetectSupportAccessVolume(
		r.Context(), query, resolved.ID, SupportAccessVolumeParams{Now: h.now()})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	signals = append(signals, revokedCredentialSignals...)
	signals = append(signals, unusualAPIKeySignals...)
	signals = append(signals, supportAccessVolumeSignals...)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"events": events, "signals": signals,
		"pagination": map[string]any{
			"next_cursor": nextCursor, "has_more": hasMore, "limit": filter.Limit,
		},
	})
}

func parseListFilter(r *http.Request, isCSV bool) (ListFilter, error) {
	q := r.URL.Query()
	filter := ListFilter{
		ResourceType: q.Get("resource_type"),
		ActorType:    q.Get("actor_type"),
		Outcome:      q.Get("outcome"),
		Cursor:       q.Get("cursor"),
		Limit:        defaultListLimit,
	}
	if isCSV {
		filter.Limit = maxCSVRows
		filter.MaxLimit = maxCSVRows
	}
	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return ListFilter{}, fmt.Errorf("limit must be a positive integer")
		}
		filter.Limit = limit
	}
	maxLimit := filter.MaxLimit
	if maxLimit <= 0 {
		maxLimit = maxListLimit
	}
	if filter.Limit > maxLimit {
		filter.Limit = maxLimit
	}
	if raw := q.Get("from"); raw != "" {
		from, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return ListFilter{}, fmt.Errorf("from must be an RFC3339 timestamp")
		}
		filter.From = &from
	}
	if raw := q.Get("to"); raw != "" {
		to, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return ListFilter{}, fmt.Errorf("to must be an RFC3339 timestamp")
		}
		filter.To = &to
	}
	return filter, nil
}

// maxCSVRows bounds a single export so the endpoint can't be used to pull an
// unbounded dump of the ledger in one request.
const maxCSVRows = 5000

func writeEventsCSV(w http.ResponseWriter, events []Event) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "attachment; filename=\"access-events.csv\"")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"id", "created_at", "kind", "outcome", "actor_type", "actor_id",
		"resource_type", "resource_id", "action", "permission_path", "request_id",
	})
	if len(events) > maxCSVRows {
		events = events[:maxCSVRows]
	}
	for _, event := range events {
		_ = writer.Write([]string{
			event.ID, event.CreatedAt.UTC().Format(time.RFC3339), event.Kind, event.Outcome,
			event.ActorType, deref(event.ActorID), deref(event.ResourceType), deref(event.ResourceID),
			deref(event.Action), deref(event.PermissionPath), deref(event.RequestID),
		})
	}
	writer.Flush()
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (h *httpHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "access event request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
