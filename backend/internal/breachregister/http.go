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
	GetIncident(
		context.Context, string,
	) (
		Incident, []ChronologyEntry, []Assessment, []ActionEvent, []ActionEvent,
		[]ControllerNotification, error,
	)
	AppendChronologyEntry(
		context.Context, string, string, string, AppendChronologyEntryInput,
	) (ChronologyEntry, error)
	CreateAssessment(
		context.Context, string, string, string, CreateAssessmentInput,
	) (Assessment, error)
	CreatePostIncidentAction(
		context.Context, string, string, string, CreatePostIncidentActionInput,
	) (ActionEvent, error)
	AppendActionRevision(
		context.Context, string, string, string, string, AppendActionRevisionInput,
	) (ActionEvent, error)
	StageControllerNotification(
		context.Context, string, string, string, StageControllerNotificationInput,
	) (ControllerNotification, bool, error)
	QueueControllerNotification(
		context.Context, string, string, string, string,
	) (ControllerNotification, bool, error)
	RetryControllerNotification(
		context.Context, string, string, string, string,
	) (ControllerNotification, bool, error)
}

type TokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

type HTTPOptions struct {
	Store BreachStore
	JWT   TokenVerifier
	// Now supplies the export handlers' generated_at clock. Defaults to
	// time.Now; every other route is unaffected.
	Now func() time.Time
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
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachAppend)).
		Post("/admin/breach-incidents/{incidentID}/assessments", handler.createAssessment)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachAppend)).
		Post("/admin/breach-incidents/{incidentID}/actions", handler.createPostIncidentAction)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachAppend)).
		Post(
			"/admin/breach-incidents/{incidentID}/actions/{actionID}/revisions",
			handler.appendActionRevision,
		)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachAppend)).
		Post(
			"/admin/breach-incidents/{incidentID}/controller-notifications",
			handler.stageControllerNotification,
		)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachAppend)).
		Post(
			"/admin/breach-incidents/{incidentID}/controller-notifications/{notificationID}/queue",
			handler.queueControllerNotification,
		)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachAppend)).
		Post(
			"/admin/breach-incidents/{incidentID}/controller-notifications/{notificationID}/retry",
			handler.retryControllerNotification,
		)
	// Exports are a read of already-staged/recorded facts, not a new class of
	// action, so they reuse PermissionPlatformBreachRead like list/get above
	// (see exports_http.go).
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachRead)).
		Get(
			"/admin/breach-incidents/{incidentID}/controller-notifications/{notificationID}/export",
			handler.exportControllerNotification,
		)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachRead)).
		Head(
			"/admin/breach-incidents/{incidentID}/controller-notifications/{notificationID}/export",
			handler.exportControllerNotification,
		)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachRead)).
		Get("/admin/breach-incidents/{incidentID}/post-incident-report", handler.exportPostIncidentReport)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformBreachRead)).
		Head("/admin/breach-incidents/{incidentID}/post-incident-report", handler.exportPostIncidentReport)
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
	incident, entries, assessments, actions, actionEvents, controllerNotifications, err :=
		h.options.Store.GetIncident(r.Context(), incidentID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"incident": incident, "chronology": entries, "assessments": assessments,
		"actions": actions, "action_events": actionEvents,
		"controller_notifications": controllerNotifications,
	})
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

type createAssessmentRequest struct {
	AuthorityNotificationDecision     string   `json:"authority_notification_decision"`
	AuthorityNotificationReasoning    string   `json:"authority_notification_reasoning"`
	HighRiskToIndividuals             string   `json:"high_risk_to_individuals"`
	HighRiskReasoning                 string   `json:"high_risk_reasoning"`
	DataSubjectCommunicationDecision  string   `json:"data_subject_communication_decision"`
	DataSubjectCommunicationReasoning string   `json:"data_subject_communication_reasoning"`
	EstimatedAffectedSubjectsMin      *int64   `json:"estimated_affected_subjects_min"`
	EstimatedAffectedSubjectsMax      *int64   `json:"estimated_affected_subjects_max"`
	EstimatedAffectedRecordsMin       *int64   `json:"estimated_affected_records_min"`
	EstimatedAffectedRecordsMax       *int64   `json:"estimated_affected_records_max"`
	LikelyConsequences                *string  `json:"likely_consequences"`
	RecoveryMeasures                  *string  `json:"recovery_measures"`
	EvidenceReferences                []string `json:"evidence_references"`
}

func (h *httpHandler) createAssessment(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	var request createAssessmentRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	authorityDecision, err := ParseAuthorityNotificationDecision(request.AuthorityNotificationDecision)
	if err != nil {
		writeError(w, err)
		return
	}
	riskDecision, err := ParseRiskDecision(request.HighRiskToIndividuals)
	if err != nil {
		writeError(w, err)
		return
	}
	communicationDecision, err := ParseCommunicationDecision(request.DataSubjectCommunicationDecision)
	if err != nil {
		writeError(w, err)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	assessment, err := h.options.Store.CreateAssessment(
		r.Context(), incidentID, claims.Subject, r.Header.Get("Idempotency-Key"),
		CreateAssessmentInput{
			AuthorityNotificationDecision:     authorityDecision,
			AuthorityNotificationReasoning:    request.AuthorityNotificationReasoning,
			HighRiskToIndividuals:             riskDecision,
			HighRiskReasoning:                 request.HighRiskReasoning,
			DataSubjectCommunicationDecision:  communicationDecision,
			DataSubjectCommunicationReasoning: request.DataSubjectCommunicationReasoning,
			EstimatedAffectedSubjectsMin:      request.EstimatedAffectedSubjectsMin,
			EstimatedAffectedSubjectsMax:      request.EstimatedAffectedSubjectsMax,
			EstimatedAffectedRecordsMin:       request.EstimatedAffectedRecordsMin,
			EstimatedAffectedRecordsMax:       request.EstimatedAffectedRecordsMax,
			LikelyConsequences:                request.LikelyConsequences,
			RecoveryMeasures:                  request.RecoveryMeasures,
			EvidenceReferences:                request.EvidenceReferences,
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"assessment": assessment})
}

type createPostIncidentActionRequest struct {
	Title                string    `json:"title"`
	OwnerPlatformAdminID string    `json:"owner_platform_admin_id"`
	DueAt                time.Time `json:"due_at"`
	CorrectiveReference  *string   `json:"corrective_reference"`
}

func (h *httpHandler) createPostIncidentAction(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	var request createPostIncidentActionRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	action, err := h.options.Store.CreatePostIncidentAction(
		r.Context(), incidentID, claims.Subject, r.Header.Get("Idempotency-Key"),
		CreatePostIncidentActionInput{
			Title: request.Title, OwnerPlatformAdminID: request.OwnerPlatformAdminID,
			DueAt: request.DueAt, CorrectiveReference: request.CorrectiveReference,
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"action": action})
}

type appendActionRevisionRequest struct {
	ExpectedRevision     int        `json:"expected_revision"`
	Status               string     `json:"status"`
	OwnerPlatformAdminID *string    `json:"owner_platform_admin_id"`
	DueAt                *time.Time `json:"due_at"`
	OutcomeSummary       *string    `json:"outcome_summary"`
	CorrectiveReference  *string    `json:"corrective_reference"`
	EvidenceReferences   []string   `json:"evidence_references"`
}

func (h *httpHandler) appendActionRevision(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	actionID := chi.URLParam(r, "actionID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	if _, err := uuid.Parse(actionID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	var request appendActionRevisionRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	status, err := ParseActionStatus(request.Status)
	if err != nil {
		writeError(w, err)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	action, err := h.options.Store.AppendActionRevision(
		r.Context(), incidentID, actionID, claims.Subject, r.Header.Get("Idempotency-Key"),
		AppendActionRevisionInput{
			ExpectedRevision: request.ExpectedRevision, Status: status,
			OwnerPlatformAdminID: request.OwnerPlatformAdminID, DueAt: request.DueAt,
			OutcomeSummary: request.OutcomeSummary, CorrectiveReference: request.CorrectiveReference,
			EvidenceReferences: request.EvidenceReferences,
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"action": action})
}

type stageControllerNotificationRequest struct {
	TenantID         string  `json:"tenant_id"`
	NotificationKind string  `json:"notification_kind"`
	Summary          string  `json:"summary"`
	MeasuresTaken    string  `json:"measures_taken"`
	NextUpdate       *string `json:"next_update"`
}

func (h *httpHandler) stageControllerNotification(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	var request stageControllerNotificationRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	kind, err := ParseControllerNotificationKind(request.NotificationKind)
	if err != nil {
		writeError(w, err)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	notification, created, err := h.options.Store.StageControllerNotification(
		r.Context(), incidentID, claims.Subject, r.Header.Get("Idempotency-Key"),
		StageControllerNotificationInput{
			TenantID: request.TenantID, NotificationKind: kind,
			Summary: request.Summary, MeasuresTaken: request.MeasuresTaken,
			NextUpdate: request.NextUpdate,
		},
	)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	api.WriteJSON(w, status, map[string]any{"notification": notification})
}

func (h *httpHandler) queueControllerNotification(w http.ResponseWriter, r *http.Request) {
	h.changeControllerNotificationQueueState(w, r, false)
}

func (h *httpHandler) retryControllerNotification(w http.ResponseWriter, r *http.Request) {
	h.changeControllerNotificationQueueState(w, r, true)
}

func (h *httpHandler) changeControllerNotificationQueueState(
	w http.ResponseWriter,
	r *http.Request,
	retry bool,
) {
	incidentID := chi.URLParam(r, "incidentID")
	notificationID := chi.URLParam(r, "notificationID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	if _, err := uuid.Parse(notificationID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	var (
		notification ControllerNotification
		changed      bool
		err          error
	)
	if retry {
		notification, changed, err = h.options.Store.RetryControllerNotification(
			r.Context(), incidentID, notificationID, claims.Subject,
			r.Header.Get("Idempotency-Key"),
		)
	} else {
		notification, changed, err = h.options.Store.QueueControllerNotification(
			r.Context(), incidentID, notificationID, claims.Subject,
			r.Header.Get("Idempotency-Key"),
		)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if changed {
		status = http.StatusAccepted
	}
	api.WriteJSON(w, status, map[string]any{"notification": notification})
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
	case errors.Is(err, ErrTenantNotFound):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "affected tenant reference is invalid", nil)
	case errors.Is(err, ErrControllerContactMissing):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "affected tenant privacy contact is incomplete", nil)
	case errors.Is(err, ErrInvalidRequest):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "breach register request is invalid", nil)
	case errors.Is(err, ErrIdempotencyConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "idempotency key was reused with different input", nil)
	case errors.Is(err, ErrRevisionConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "breach action revision is stale", nil)
	case errors.Is(err, ErrNotificationNotQueueable):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "breach controller notification is not queueable", nil)
	case errors.Is(err, ErrNotificationInProgress):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "breach controller notification delivery is in progress", nil)
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
