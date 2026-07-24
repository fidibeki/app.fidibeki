package breachregister

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type BreachStore interface {
	CreateIncident(context.Context, string, string, CreateIncidentInput) (Incident, error)
	ListIncidents(context.Context, ListFilter) ([]Incident, error)
	GetIncident(context.Context, string) (Incident, []ChronologyEntry, error)
	AppendChronologyEntry(
		context.Context, string, string, string, AppendChronologyEntryInput,
	) (ChronologyEntry, error)
}

type TokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

type HTTPOptions struct {
	Store BreachStore
	JWT   TokenVerifier
}

type httpHandler struct {
	options HTTPOptions
}

// RegisterPlatformRoutes mounts the restricted breach register under the
// existing platform-admin router. Every route requires a platform-admin
// bearer token (auth.AudiencePlatform) with the specific permission below —
// a tenant member or tenant API key JWT carries auth.AudienceTenant and is
// rejected before permission checks ever run, matching every other
// platform-only surface in this codebase (see platformadmin.NewHTTPHandler).
func RegisterPlatformRoutes(router chi.Router, options HTTPOptions) {
	handler := &httpHandler{options: options}
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachCreate)).
		Post("/admin/breach-incidents", handler.createIncident)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachRead)).
		Get("/admin/breach-incidents", handler.listIncidents)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachRead)).
		Get("/admin/breach-incidents/{incidentID}", handler.getIncident)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachAppend)).
		Post("/admin/breach-incidents/{incidentID}/chronology", handler.appendChronologyEntry)
}

type createIncidentRequest struct {
	AwarenessAt               time.Time `json:"awareness_at"`
	DetectionSource           string    `json:"detection_source"`
	ControllerRole            string    `json:"controller_role"`
	AffectedEnvironments      []string  `json:"affected_environments"`
	AffectedSystemCategories  []string  `json:"affected_system_categories"`
	AffectedDataCategories    []string  `json:"affected_data_categories"`
	AffectedSubjectCategories []string  `json:"affected_subject_categories"`
	ConfidentialityImpact     bool      `json:"confidentiality_impact"`
	IntegrityImpact           bool      `json:"integrity_impact"`
	AvailabilityImpact        bool      `json:"availability_impact"`
	ContainmentSummary        string    `json:"containment_summary"`
	AffectedTenantIDs         []string  `json:"affected_tenant_ids"`
}

func (h *httpHandler) createIncident(w http.ResponseWriter, r *http.Request) {
	var request createIncidentRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	detectionSource, err := ParseDetectionSource(request.DetectionSource)
	if err != nil {
		writeError(w, err)
		return
	}
	controllerRole, err := ParseControllerRole(request.ControllerRole)
	if err != nil {
		writeError(w, err)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	incident, err := h.options.Store.CreateIncident(
		r.Context(), claims.Subject, r.Header.Get("Idempotency-Key"),
		CreateIncidentInput{
			AwarenessAt: request.AwarenessAt, DetectionSource: detectionSource,
			ControllerRole: controllerRole, AffectedEnvironments: request.AffectedEnvironments,
			AffectedSystemCategories:  request.AffectedSystemCategories,
			AffectedDataCategories:    request.AffectedDataCategories,
			AffectedSubjectCategories: request.AffectedSubjectCategories,
			ConfidentialityImpact:     request.ConfidentialityImpact,
			IntegrityImpact:           request.IntegrityImpact,
			AvailabilityImpact:        request.AvailabilityImpact,
			ContainmentSummary:        request.ContainmentSummary,
			AffectedTenantIDs:         request.AffectedTenantIDs,
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"incident": incident})
}

func (h *httpHandler) listIncidents(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "limit must be a positive integer", nil)
			return
		}
		limit = parsed
	}
	incidents, err := h.options.Store.ListIncidents(r.Context(), ListFilter{Limit: limit})
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"incidents": incidents})
}

func (h *httpHandler) getIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	incident, entries, err := h.options.Store.GetIncident(r.Context(), incidentID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"incident": incident, "chronology": entries})
}

type appendChronologyEntryRequest struct {
	EntryKind          string   `json:"entry_kind"`
	Summary            string   `json:"summary"`
	EvidenceReferences []string `json:"evidence_references"`
}

func (h *httpHandler) appendChronologyEntry(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	var request appendChronologyEntryRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	entryKind, err := ParseChronologyEntryKind(request.EntryKind)
	if err != nil {
		writeError(w, err)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	entry, err := h.options.Store.AppendChronologyEntry(
		r.Context(), incidentID, claims.Subject, r.Header.Get("Idempotency-Key"),
		AppendChronologyEntryInput{
			EntryKind: entryKind, Summary: request.Summary,
			EvidenceReferences: request.EvidenceReferences,
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"entry": entry})
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
	case errors.Is(err, ErrTenantNotFound):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "affected tenant reference is invalid", nil)
	case errors.Is(err, ErrInvalidRequest):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "breach register request is invalid", nil)
	case errors.Is(err, ErrIdempotencyConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "idempotency key was reused with different input", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any) bool {
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
