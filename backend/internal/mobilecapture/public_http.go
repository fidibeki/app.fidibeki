package mobilecapture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AbuseEvaluator interface {
	Evaluate(context.Context, abuse.Policy, abuse.Request) (abuse.Decision, error)
}

type AbuseDiagnosticRecorder interface {
	Record(context.Context, abuse.DiagnosticEvent) error
}

type PublicHTTPOptions struct {
	Store             Store
	Resolver          db.Querier
	Begin             db.BeginFunc
	FeedbackCore      feedback.SubmissionCore
	Storage           feedback.ObjectStorage
	Scheduler         feedback.AttachmentVerificationScheduler
	Abuse             AbuseEvaluator
	Diagnostics       AbuseDiagnosticRecorder
	TrustedProxyCIDRs []netip.Prefix
	Now               func() time.Time
	Logger            *slog.Logger
}

type publicHTTPHandler struct {
	options PublicHTTPOptions
	logger  *slog.Logger
}

func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &publicHTTPHandler{options: options, logger: logger}
	router.Post("/v1/mobile/feedback", handler.submit)
	router.Post("/v1/mobile/attachments/{attachmentID}/complete", handler.completeAttachment)
}

func (handler *publicHTTPHandler) submit(w http.ResponseWriter, r *http.Request) {
	rawToken, ok := mobileBearer(w, r)
	if !ok {
		return
	}
	var request feedback.MobileSubmissionRequest
	if !decodeJSON(w, r, &request, true) {
		return
	}
	if parsed, parseErr := uuid.Parse(strings.TrimSpace(request.ClientFeedbackID)); parseErr == nil {
		request.ClientFeedbackID = parsed.String()
	}
	requestDigest, err := canonicalRequestDigest(request)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return
	}
	resolved, err := handler.options.Store.ResolveToken(r.Context(), handler.options.Resolver, rawToken)
	if errors.Is(err, ErrAccessDenied) {
		writeMobileDenied(w)
		return
	}
	if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	tx, ok := handler.beginTenantTransaction(w, r, resolved.TenantID)
	if !ok {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	session, err := handler.options.Store.LockSession(r.Context(), tx, resolved, rawToken)
	if errors.Is(err, ErrAccessDenied) {
		writeMobileDenied(w)
		return
	}
	if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	now := handler.now().UTC()
	if !session.AuthorizedAt(now) {
		writeMobileDenied(w)
		return
	}
	bound := session.FeedbackID != nil
	if !bound && !now.Before(session.SubmitExpiresAt) {
		writeMobileDenied(w)
		return
	}
	if bound {
		if session.RetryExpiresAt == nil || !now.Before(*session.RetryExpiresAt) {
			writeMobileDenied(w)
			return
		}
		if session.ClientFeedbackID == nil || *session.ClientFeedbackID != request.ClientFeedbackID ||
			session.RequestDigest == nil || *session.RequestDigest != requestDigest {
			api.WriteError(w, http.StatusConflict, api.CodeConflict,
				"capture session is bound to another submission", nil)
			return
		}
	}

	var configuration project.PublicConfiguration
	var values feedback.SubmissionValues
	if bound {
		configuration = project.PublicConfiguration{
			TenantID: session.TenantID, ProjectID: session.ProjectID,
		}
		values = feedback.SubmissionValues{
			ClientFeedbackID: request.ClientFeedbackID,
			WidgetRuleID:     session.WidgetRuleID, Source: string(session.Platform),
		}
	} else {
		if err := handler.options.Store.LockMintedConfiguration(
			r.Context(), tx, session,
		); errors.Is(err, ErrConfigurationChanged) {
			api.WriteError(w, http.StatusConflict, api.CodeConfigurationChanged,
				"mobile capture configuration changed", nil)
			return
		} else if err != nil {
			handler.writeStoreError(w, r, err)
			return
		}
		configuration, err = handler.options.Store.LoadConfiguration(
			r.Context(), tx, session.TenantID, session.ProjectID,
		)
		if errors.Is(err, ErrNotFound) {
			writeMobileDenied(w)
			return
		}
		if err != nil {
			handler.writeStoreError(w, r, err)
			return
		}
		resolvedConfiguration, err := ResolveSessionConfiguration(configuration, session)
		if errors.Is(err, ErrConfigurationChanged) {
			api.WriteError(w, http.StatusConflict, api.CodeConfigurationChanged,
				"mobile capture configuration changed", nil)
			return
		}
		if err != nil {
			handler.writeStoreError(w, r, err)
			return
		}
		configuration = resolvedConfiguration.Configuration
		var fields []feedback.FieldError
		values, fields = feedback.ValidateMobileSubmissionAt(
			request, configuration, session.WidgetRuleID, string(session.Platform),
			session.ApplicationID, session.Screen, session.ContextURL,
			session.DisplayLocale, now,
		)
		if len(fields) > 0 {
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
				"feedback is invalid", fields)
			return
		}
	}
	if _, allowed := handler.enforceAbuse(
		w, r, session, abuse.MobileSubmissionPolicy, 1, false,
	); !allowed {
		return
	}
	warnings := make([]feedback.SubmissionWarning, 0, 1)
	if len(values.Attachments) > 0 {
		decision, allowed := handler.enforceAbuse(
			w, r, session, abuse.MobileAttachmentGrantPolicy,
			len(values.Attachments), true,
		)
		if !allowed {
			if decision.Unavailable {
				warnings = append(warnings, mobileUploadUnavailableWarning())
				values.Attachments = nil
			} else {
				return
			}
		}
	}
	if len(values.Attachments) > 0 {
		mebibytes := int((mobileAttachmentBytes(values.Attachments) + (1 << 20) - 1) >> 20)
		decision, allowed := handler.enforceAbuse(
			w, r, session, abuse.MobileAttachmentBytePolicy, mebibytes, true,
		)
		if !allowed {
			if decision.Unavailable {
				warnings = append(warnings, mobileUploadUnavailableWarning())
				values.Attachments = nil
			} else {
				return
			}
		}
	}
	ownership := feedback.SubmissionOwnership{
		Source: string(session.Platform), AllowExisting: bound,
		ExistingFeedbackID: session.FeedbackID,
	}
	feedbackCore := handler.options.FeedbackCore
	feedbackCore.Now = func() time.Time { return now }
	result, err := feedbackCore.Submit(
		r.Context(), tx, feedback.CoreSubmissionRequest{
			Configuration: configuration, Values: values, Ownership: ownership,
		},
	)
	var planLimit *feedback.PlanLimitError
	switch {
	case errors.As(err, &planLimit):
		handler.recordDiagnostic(session, abuse.CategoryPlanQuota, abuse.ReasonPlanQuotaExhausted)
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanLimitExceeded,
			"monthly feedback limit reached", map[string]any{
				"limit": planLimit.Limit, "usage": planLimit.Usage, "upgrade_url": "/v1/billing",
			})
		return
	case errors.Is(err, feedback.ErrSubmissionCollision):
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"feedback idempotency key is already in use", nil)
		return
	case err != nil:
		handler.writeStoreError(w, r, err)
		return
	}
	if !bound {
		if err := handler.options.Store.BindSession(
			r.Context(), tx, session.ID, result.Submission.ID,
			values.ClientFeedbackID, requestDigest, now,
			feedback.LatestAttachmentExpiry(result.Submission),
		); err != nil {
			if errors.Is(err, ErrSubmissionConflict) {
				api.WriteError(w, http.StatusConflict, api.CodeConflict,
					"capture session is bound to another submission", nil)
				return
			}
			handler.writeStoreError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		handler.writeStoreError(w, r, fmt.Errorf("commit mobile feedback: %w", err))
		return
	}
	committed = true
	response, err := feedback.BuildSubmissionResponse(
		r.Context(), handler.options.Storage, result.Submission, result.RawTokens, warnings,
	)
	if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, status, response)
}

func (handler *publicHTTPHandler) completeAttachment(w http.ResponseWriter, r *http.Request) {
	rawToken, ok := mobileBearer(w, r)
	if !ok {
		return
	}
	attachmentID := chi.URLParam(r, "attachmentID")
	if _, err := uuid.Parse(attachmentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "attachment not found", nil)
		return
	}
	var request struct {
		CompleteToken string `json:"complete_token"`
	}
	if !decodeJSON(w, r, &request, true) {
		return
	}
	if strings.TrimSpace(request.CompleteToken) == "" {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"attachment completion is invalid",
			[]feedback.FieldError{{Field: "complete_token", Message: "is required"}})
		return
	}
	resolved, err := handler.options.Store.ResolveToken(r.Context(), handler.options.Resolver, rawToken)
	if errors.Is(err, ErrAccessDenied) {
		writeMobileDenied(w)
		return
	}
	if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	tx, ok := handler.beginTenantTransaction(w, r, resolved.TenantID)
	if !ok {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	session, err := handler.options.Store.LockSession(
		r.Context(), tx, resolved, rawToken,
	)
	if errors.Is(err, ErrAccessDenied) {
		writeMobileDenied(w)
		return
	}
	if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	if !session.AuthorizedAt(handler.now().UTC()) || session.FeedbackID == nil {
		writeMobileDenied(w)
		return
	}
	if _, allowed := handler.enforceAbuse(
		w, r, session, abuse.MobileAttachmentCompletionPolicy, 1, false,
	); !allowed {
		return
	}
	completer := feedback.AttachmentCompletionService{
		Store:   handler.options.FeedbackCore.Store,
		Storage: handler.options.Storage, Now: handler.options.Now,
	}
	attachment, err := completer.Complete(
		r.Context(), tx, session.ProjectID, attachmentID,
		session.FeedbackID, request.CompleteToken,
	)
	var completionError *feedback.AttachmentCompletionError
	if errors.As(err, &completionError) &&
		completionError.Failure == feedback.AttachmentCompletionExpired {
		if err := tx.Commit(r.Context()); err != nil {
			handler.writeStoreError(w, r, err)
			return
		}
		committed = true
		handler.recordDiagnostic(session, abuse.CategoryUploadSafety, abuse.ReasonUploadExpired)
		api.WriteErrorWithOptions(
			w, http.StatusGone, api.CodeConflict, "attachment upload expired", nil,
			api.ErrorOptions{Reason: abuse.ReasonUploadExpired},
		)
		return
	}
	if errors.As(err, &completionError) {
		switch completionError.Failure {
		case feedback.AttachmentCompletionNotFound:
			api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "attachment not found", nil)
		case feedback.AttachmentCompletionComplete:
			api.WriteError(w, http.StatusConflict, api.CodeConflict, "attachment is already complete", nil)
		case feedback.AttachmentCompletionDenied:
			api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "attachment completion denied", nil)
		case feedback.AttachmentCompletionMissing:
			api.WriteError(w, http.StatusConflict, api.CodeConflict, "attachment upload is not available", nil)
		case feedback.AttachmentCompletionMismatch:
			handler.recordDiagnostic(
				session, abuse.CategoryUploadSafety, abuse.ReasonUploadCompletionMismatch,
			)
			api.WriteErrorWithOptions(
				w, http.StatusUnprocessableEntity, api.CodeValidation,
				"attachment upload is invalid", completionError.Fields,
				api.ErrorOptions{Reason: abuse.ReasonUploadInvalid},
			)
		default:
			handler.writeStoreError(w, r, err)
		}
		return
	}
	if err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		handler.writeStoreError(w, r, err)
		return
	}
	committed = true
	if handler.options.Scheduler != nil {
		if err := handler.options.Scheduler.ScheduleVerification(
			context.WithoutCancel(r.Context()),
			feedback.VerificationTask{
				TenantID: session.TenantID, AttachmentID: attachment.ID,
				Generation: attachment.VerificationGeneration,
			},
		); err != nil {
			handler.logger.ErrorContext(r.Context(), "schedule mobile attachment verification",
				"error", err, "attachment_id", attachment.ID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func canonicalRequestDigest(request feedback.MobileSubmissionRequest) (string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func mobileBearer(w http.ResponseWriter, r *http.Request) (string, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") ||
		!validCaptureToken(parts[1]) {
		api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated,
			"mobile capture authentication required", nil)
		return "", false
	}
	return parts[1], true
}

func (handler *publicHTTPHandler) beginTenantTransaction(
	w http.ResponseWriter,
	r *http.Request,
	tenantID string,
) (db.Transaction, bool) {
	tx, err := handler.options.Begin(r.Context())
	if err != nil {
		handler.writeStoreError(w, r, err)
		return nil, false
	}
	var configured string
	if err := tx.QueryRow(r.Context(), `
		SELECT set_config('app.tenant_id', $1, true)
	`, tenantID).Scan(&configured); err != nil || configured != tenantID {
		_ = tx.Rollback(r.Context())
		handler.writeStoreError(w, r, errors.New("mobile tenant context did not match"))
		return nil, false
	}
	return tx, true
}

func (handler *publicHTTPHandler) enforceAbuse(
	w http.ResponseWriter,
	r *http.Request,
	session CaptureSession,
	policy abuse.Policy,
	cost int,
	degradeOnUnavailable bool,
) (abuse.Decision, bool) {
	if handler.options.Abuse == nil {
		decision := abuse.Decision{Unavailable: true, Reason: policy.Reason}
		if degradeOnUnavailable {
			return decision, false
		}
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"service unavailable", nil)
		return decision, false
	}
	decision, err := handler.options.Abuse.Evaluate(r.Context(), policy, abuse.Request{
		Identity: abuse.Identity{
			TenantID: session.TenantID, ProjectID: session.ProjectID,
			Network:   httpx.ClientIPFromRequest(r, handler.options.TrustedProxyCIDRs),
			SessionID: session.ID,
		},
		Cost: cost,
	})
	if err != nil || decision.Unavailable {
		handler.logger.WarnContext(r.Context(), "mobile abuse policy unavailable",
			"error", err, "policy", policy.Name)
		if degradeOnUnavailable {
			return decision, false
		}
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			"service unavailable", nil)
		return decision, false
	}
	if decision.Allowed {
		if decision.Limit > 0 {
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
		}
		return decision, true
	}
	handler.recordDiagnostic(session, abuse.CategoryRateLimit, decision.Reason)
	retry := abuse.RetryAfterSeconds(decision.RetryAfter)
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	api.WriteErrorWithOptions(
		w, http.StatusTooManyRequests, api.CodeRateLimitExceeded,
		"too many attempts; try again shortly", nil,
		api.ErrorOptions{Reason: decision.Reason, RetryAfterSeconds: retry},
	)
	return decision, false
}

func (handler *publicHTTPHandler) recordDiagnostic(
	session CaptureSession,
	category abuse.Category,
	reason string,
) {
	if handler.options.Diagnostics == nil {
		return
	}
	_ = handler.options.Diagnostics.Record(context.Background(), abuse.DiagnosticEvent{
		TenantID: session.TenantID, ProjectID: session.ProjectID,
		Category: category, Reason: reason, OccurredAt: handler.now().UTC(),
	})
}

func (handler *publicHTTPHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	handler.logger.ErrorContext(r.Context(), "mobile capture request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
		"service unavailable", nil)
}

func (handler *publicHTTPHandler) now() time.Time {
	if handler.options.Now != nil {
		return handler.options.Now()
	}
	return time.Now()
}

func writeMobileDenied(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "mobile capture denied", nil)
}

func mobileAttachmentBytes(values []feedback.AttachmentValues) int64 {
	var total int64
	for _, value := range values {
		total += value.SizeBytes
	}
	return total
}

func mobileUploadUnavailableWarning() feedback.SubmissionWarning {
	return feedback.SubmissionWarning{
		Code:    "service_unavailable",
		Message: "Feedback saved without attachments. Try again later.",
		Reason:  abuse.ReasonUploadRateLimited, RetryAfterSeconds: 5,
	}
}
