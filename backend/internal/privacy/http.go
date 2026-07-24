package privacy

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
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const downloadLinkTTL = 10 * time.Minute

type HTTPStore interface {
	CreateWorkspaceExport(context.Context, db.Querier, string, string, time.Time) (Job, bool, error)
	ListJobs(context.Context, db.Querier, string, int) ([]Job, error)
	GetJob(context.Context, db.Querier, string, string) (Job, error)
	RetryJob(context.Context, db.Querier, string, string, string, time.Time) (Job, error)
	RecordDownload(context.Context, db.Querier, string, string, string, time.Time) error
	CreateReporterRequest(context.Context, db.Querier, string, string, string, ReporterAction, time.Time) (ReporterRequest, string, bool, error)
	ListReporterRequests(context.Context, db.Querier, string, int) ([]ReporterRequest, error)
	ListReviewItems(context.Context, db.Querier, string, string) ([]ReviewItem, error)
	DecideReviewItem(context.Context, db.Querier, string, string, string, string, ReviewDecision, time.Time) (ReviewItem, error)
	FinalizeReporterAnonymization(context.Context, db.Querier, string, string, time.Time) (bool, int, error)
	CreateCase(context.Context, db.Querier, string, string, string, CreateCaseInput, time.Time) (PrivacyCase, string, bool, error)
	ListCases(context.Context, db.Querier, string, int) ([]PrivacyCase, error)
	GetCase(context.Context, db.Querier, string, string) (PrivacyCase, error)
	ListCaseEvents(context.Context, db.Querier, string, string) ([]CaseEvent, error)
	TransitionCase(context.Context, db.Querier, string, string, string, string, TransitionCaseInput, time.Time) (PrivacyCase, error)
	CaseDashboard(context.Context, db.Querier, string, time.Time) (CaseDashboard, error)
	SetCaseRestriction(context.Context, db.Querier, string, string, string, string, bool, string, time.Time) (Restriction, error)
	PlaceLegalHold(context.Context, db.Querier, string, string, string, string, string, string, time.Time) (LegalHold, error)
	ReleaseLegalHold(context.Context, db.Querier, string, string, string, string, string, string, time.Time) (LegalHold, error)
	RecordPortabilityAssessment(context.Context, db.Querier, string, string, string, string, RecordPortabilityAssessmentInput, time.Time) (PortabilityAssessment, bool, error)
	StartCaseExecution(context.Context, db.Querier, string, string, string, string, time.Time) (CaseExecution, bool, error)
}

type DownloadStorage interface {
	PresignGet(context.Context, string, string, time.Duration) (string, error)
}

type HTTPOptions struct {
	Store         HTTPStore
	JWT           *auth.JWTManager
	Storage       DownloadStorage
	Email         email.Enqueuer
	PublicBaseURL string
	BaseDomain    string
	From          string
	Logger        *slog.Logger
	Now           func() time.Time
}

type HTTPHandler struct {
	options HTTPOptions
	logger  *slog.Logger
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{options: options, logger: logger}
	retentionRead := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionTenantRead),
	}
	read := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		handler.requireHuman,
		appmiddleware.RequirePermission(auth.PermissionTenantUpdate),
	}
	router.With(retentionRead...).Get("/v1/privacy/retention", handler.retention)
	router.With(read...).Get("/v1/privacy/exports", handler.listExports)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/exports", handler.createExport)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/exports/{jobID}/retry", handler.retryExport)
	router.With(read...).Post("/v1/privacy/exports/{jobID}/download", handler.downloadExport)
	router.With(read...).Get("/v1/privacy/requests", handler.listReporterRequests)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/requests", handler.createReporterRequest)
	router.With(read...).Get("/v1/privacy/requests/{requestID}/review-items", handler.listReviewItems)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/requests/{requestID}/review-items/{itemID}/decide", handler.decideReviewItem)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/requests/{requestID}/finalize", handler.finalizeReporterRequest)
	router.With(read...).Get("/v1/privacy/cases", handler.listCases)
	router.With(read...).Get("/v1/privacy/cases/dashboard", handler.caseDashboard)
	router.With(read...).Get("/v1/privacy/cases/{caseID}", handler.getCase)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/cases", handler.createCase)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/cases/{caseID}/transition", handler.transitionCase)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/cases/{caseID}/restriction", handler.setCaseRestriction)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/cases/{caseID}/legal-holds", handler.placeCaseLegalHold)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/cases/{caseID}/legal-holds/{holdID}/release", handler.releaseCaseLegalHold)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/cases/{caseID}/portability-assessments", handler.recordCasePortabilityAssessment)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/cases/{caseID}/execute", handler.startCaseExecution)
	router.With(read...).
		Post("/v1/privacy/cases/{caseID}/execution/download", handler.downloadCaseExecution)
}

func (handler *HTTPHandler) retention(w http.ResponseWriter, r *http.Request) {
	resolved, ok := appmiddleware.TenantFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	effective, err := EffectiveRetentionForPlan(resolved.Plan)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"retention": effective})
}

func (handler *HTTPHandler) requireHuman(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolved, query, claims, ok := privacyRequestState(w, r)
		if !ok {
			return
		}
		if err := requireActiveHuman(r.Context(), query, resolved.ID, claims.Subject); err != nil {
			handler.writeError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (handler *HTTPHandler) createExport(w http.ResponseWriter, r *http.Request) {
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	job, created, err := handler.options.Store.CreateWorkspaceExport(
		r.Context(), query, resolved.ID, claims.Subject, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	api.WriteJSON(w, status, map[string]any{"export": job, "created": created})
}

func (handler *HTTPHandler) listExports(w http.ResponseWriter, r *http.Request) {
	resolved, query, _, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	jobs, err := handler.options.Store.ListJobs(r.Context(), query, resolved.ID, 50)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"exports": jobs})
}

func (handler *HTTPHandler) createReporterRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var input struct {
		Email  string `json:"email"`
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body must contain one JSON value", nil)
		return
	}
	action, err := ParseReporterAction(input.Action)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	request, token, shouldEmail, err := handler.options.Store.CreateReporterRequest(
		r.Context(), query, resolved.ID, claims.Subject, input.Email, action, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if shouldEmail {
		message, err := reporterVerificationMessage(
			request, resolved.Name, resolved.Slug, token, handler.options,
		)
		if err != nil || handler.options.Email == nil ||
			!appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
				return handler.options.Email.Enqueue(ctx, message)
			}) {
			if err == nil {
				err = errors.New("privacy verification email is unavailable")
			}
			handler.writeError(w, r, err)
			return
		}
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]any{
		"request": request, "verification_email_queued": shouldEmail,
	})
}

func (handler *HTTPHandler) listReporterRequests(w http.ResponseWriter, r *http.Request) {
	resolved, query, _, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	requests, err := handler.options.Store.ListReporterRequests(r.Context(), query, resolved.ID, 50)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"requests": requests})
}

func (handler *HTTPHandler) listReviewItems(w http.ResponseWriter, r *http.Request) {
	requestID, ok := privacyRequestID(w, r)
	if !ok {
		return
	}
	resolved, query, _, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	items, err := handler.options.Store.ListReviewItems(r.Context(), query, resolved.ID, requestID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"review_items": items})
}

func (handler *HTTPHandler) decideReviewItem(w http.ResponseWriter, r *http.Request) {
	requestID, ok := privacyRequestID(w, r)
	if !ok {
		return
	}
	itemID, ok := privacyReviewItemID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var input struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body must contain one JSON value", nil)
		return
	}
	status, err := ParseReviewDecisionStatus(input.Status)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	now := handler.now()
	item, err := handler.options.Store.DecideReviewItem(
		r.Context(), query, resolved.ID, requestID, itemID, claims.Subject,
		ReviewDecision{Status: status, Reason: input.Reason}, now,
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	// Best-effort: attempt completion immediately so the last decision in a queue finishes the
	// request without a separate call. A failure here does not affect the decision that was just
	// recorded; the explicit finalize endpoint (or the next decision) can retry it.
	completed, pending, _ := handler.options.Store.FinalizeReporterAnonymization(
		r.Context(), query, resolved.ID, requestID, now,
	)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"review_item": item, "completed": completed, "pending": pending,
	})
}

func (handler *HTTPHandler) finalizeReporterRequest(w http.ResponseWriter, r *http.Request) {
	requestID, ok := privacyRequestID(w, r)
	if !ok {
		return
	}
	resolved, query, _, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	completed, pending, err := handler.options.Store.FinalizeReporterAnonymization(
		r.Context(), query, resolved.ID, requestID, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"completed": completed, "pending": pending})
}

func (handler *HTTPHandler) retryExport(w http.ResponseWriter, r *http.Request) {
	jobID, ok := privacyJobID(w, r)
	if !ok {
		return
	}
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	job, err := handler.options.Store.RetryJob(
		r.Context(), query, resolved.ID, claims.Subject, jobID, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]any{"export": job})
}

func (handler *HTTPHandler) downloadExport(w http.ResponseWriter, r *http.Request) {
	jobID, ok := privacyJobID(w, r)
	if !ok {
		return
	}
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	job, err := handler.options.Store.GetJob(r.Context(), query, resolved.ID, jobID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	now := handler.now()
	if job.Kind != JobWorkspaceExport || job.Status != JobCompleted || job.ObjectKey == nil ||
		job.ExpiresAt == nil || !job.ExpiresAt.After(now) {
		handler.writeError(w, r, ErrJobUnavailable)
		return
	}
	if handler.options.Storage == nil {
		handler.writeError(w, r, errors.New("privacy export storage is unavailable"))
		return
	}
	linkExpiresAt := now.Add(downloadLinkTTL)
	filename := fmt.Sprintf("fidibeki-%s-workspace-export-%s.zip", resolved.Slug, now.Format("2006-01-02"))
	url, err := handler.options.Storage.PresignGet(
		r.Context(), *job.ObjectKey, filename, downloadLinkTTL,
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if err := handler.options.Store.RecordDownload(
		r.Context(), query, resolved.ID, claims.Subject, job.ID, linkExpiresAt,
	); err != nil {
		handler.writeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"url": url, "expires_at": linkExpiresAt,
	})
}

func privacyRequestState(
	w http.ResponseWriter,
	r *http.Request,
) (resolvedTenant struct{ ID, Slug, Name string }, query db.Querier, claims auth.Claims, ok bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tenantOK || !queryOK || !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return resolvedTenant, nil, auth.Claims{}, false
	}
	resolvedTenant.ID = resolved.ID
	resolvedTenant.Slug = resolved.Slug
	resolvedTenant.Name = resolved.Name
	return resolvedTenant, query, claims, true
}

func privacyJobID(w http.ResponseWriter, r *http.Request) (string, bool) {
	jobID := chi.URLParam(r, "jobID")
	if _, err := uuid.Parse(jobID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "privacy export not found", nil)
		return "", false
	}
	return jobID, true
}

func privacyRequestID(w http.ResponseWriter, r *http.Request) (string, bool) {
	requestID := chi.URLParam(r, "requestID")
	if _, err := uuid.Parse(requestID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "privacy request not found", nil)
		return "", false
	}
	return requestID, true
}

func privacyReviewItemID(w http.ResponseWriter, r *http.Request) (string, bool) {
	itemID := chi.URLParam(r, "itemID")
	if _, err := uuid.Parse(itemID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "privacy review item not found", nil)
		return "", false
	}
	return itemID, true
}

func (handler *HTTPHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "privacy export not found", nil)
	case errors.Is(err, ErrJobUnavailable):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "privacy export is unavailable", nil)
	case errors.Is(err, ErrHumanRequired):
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "active human member required", nil)
	case errors.Is(err, ErrInvalidRequest):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "request validation failed", nil)
	case errors.Is(err, ErrReviewReasonMissing):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "a decision reason is required", nil)
	case errors.Is(err, ErrReviewItemDecided):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "privacy review item is already decided", nil)
	case errors.Is(err, ErrIdempotencyConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "idempotency key conflicts with an existing command", nil)
	case errors.Is(err, ErrCaseTransition):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "privacy case transition is invalid", nil)
	case errors.Is(err, ErrCaseReasonRequired):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "a reason is required for this decision", nil)
	case errors.Is(err, ErrLegalHoldActive):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "an active legal hold blocks this privacy operation", nil)
	case errors.Is(err, ErrCaseExecutionUnsupported):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "this privacy case cannot use automated execution", nil)
	default:
		handler.logger.ErrorContext(r.Context(), "privacy request failed", "error", err)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}

func (handler *HTTPHandler) now() time.Time {
	if handler.options.Now != nil {
		return handler.options.Now().UTC()
	}
	return time.Now().UTC()
}
