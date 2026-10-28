package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/abuse"
	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/httpx"
	"github.com/fidibeki/app.fidibeki/backend/internal/project"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/fidibeki/app.fidibeki/backend/internal/storage"
	"github.com/fidibeki/app.fidibeki/backend/internal/widgetanalytics"
	"github.com/fidibeki/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	uploadTTL       = 10 * time.Minute
	maximumBodySize = 1 << 20
)

type ObjectStorage interface {
	PresignPut(context.Context, string, time.Duration) (string, error)
	Stat(context.Context, string) (storage.ObjectInfo, error)
	RemoveObject(context.Context, string) error
}

type AttachmentVerificationScheduler interface {
	ScheduleVerification(context.Context, VerificationTask) error
}

type SubmissionAnalyticsStore interface {
	InsertSubmittedEvent(
		context.Context,
		db.Querier,
		project.PublicConfiguration,
		widgetanalytics.EventValues,
		time.Time,
	) (bool, error)
}

type AbuseEvaluator interface {
	Evaluate(context.Context, abuse.Policy, abuse.Request) (abuse.Decision, error)
}

type AbuseDiagnosticRecorder interface {
	Record(context.Context, abuse.DiagnosticEvent) error
}

type HTTPOptions struct {
	Authorizer        widgetapi.ProjectAuthorizer
	Begin             db.BeginFunc
	Store             Store
	Limiter           ratelimit.Limiter
	Abuse             AbuseEvaluator
	Diagnostics       AbuseDiagnosticRecorder
	TrustedProxyCIDRs []netip.Prefix
	Storage           ObjectStorage
	Scheduler         AttachmentVerificationScheduler
	Analytics         SubmissionAnalyticsStore
	Logger            *slog.Logger
	Now               func() time.Time
	GeneratePublicID  func() (string, error)
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
	router.Post("/v1/feedback", handler.submit)
	router.Options("/v1/feedback", handler.preflight)
	router.Post("/v1/attachments/{attachmentID}/complete", handler.completeAttachment)
	router.Options("/v1/attachments/{attachmentID}/complete", handler.preflight)
}

func (h *HTTPHandler) preflight(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorize(w, r, r.URL.Query().Get("key"), false)
	if !ok {
		return
	}
	_ = configuration
	widgetapi.SetCORSHeaders(w, origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+widgetapi.PublicKeyHeader)
	w.Header().Set("Access-Control-Max-Age", "300")
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) submit(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorize(w, r, r.Header.Get(widgetapi.PublicKeyHeader), true)
	if !ok {
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	var request SubmissionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Honeypot) != "" {
		h.recordDiagnostic(r.Context(), configuration, abuse.CategorySpam, abuse.ReasonSpamRejected)
		api.WriteJSON(w, http.StatusOK, map[string]bool{"accepted": true})
		return
	}
	visitorID := ""
	if request.Analytics != nil {
		visitorID = request.Analytics.VisitorID
	}
	if _, allowed := h.enforceAbusePolicy(
		w, r, configuration, origin, visitorID, abuse.SubmissionPolicy, 1, false,
	); !allowed {
		return
	}
	values, fields := ValidateSubmissionForOriginAt(
		request, configuration, origin, h.now().UTC(),
	)
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "feedback is invalid", fields)
		return
	}
	warnings := make([]SubmissionWarning, 0, 1)
	if values.ElementContextOmitted {
		warnings = append(warnings, elementContextOmittedWarning())
	}
	if len(values.Attachments) > 0 {
		grant, allowed := h.enforceAbusePolicy(
			w, r, configuration, origin, visitorID, abuse.AttachmentGrantPolicy,
			len(values.Attachments), true,
		)
		if !allowed {
			if !grant.Unavailable {
				return
			}
			warnings = append(warnings, uploadUnavailableWarning())
			values.Attachments = nil
		} else {
			mebibytes := int((attachmentTotalBytes(values.Attachments) + (1 << 20) - 1) >> 20)
			bytesDecision, bytesAllowed := h.enforceAbusePolicy(
				w, r, configuration, origin, visitorID, abuse.AttachmentBytePolicy, mebibytes,
				true,
			)
			if !bytesAllowed {
				if !bytesDecision.Unavailable {
					return
				}
				warnings = append(warnings, uploadUnavailableWarning())
				values.Attachments = nil
			}
		}
	}

	tx, ok := h.beginTenantTransaction(w, r, configuration.TenantID)
	if !ok {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	core := SubmissionCore{
		Store: h.options.Store, Analytics: h.options.Analytics,
		Now: h.options.Now, GeneratePublicID: h.options.GeneratePublicID,
	}
	result, err := core.Submit(r.Context(), tx, CoreSubmissionRequest{
		Configuration: configuration, Values: values,
		Ownership: SubmissionOwnership{Source: "web", AllowExisting: true},
		BeforeCreate: func(ctx context.Context, query db.Querier) error {
			if values.ReplaySessionID == nil {
				return nil
			}
			return h.options.Store.LockReplaySession(
				ctx, query, configuration.TenantID, configuration.ProjectID,
				*values.ReplaySessionID,
			)
		},
	})
	var planLimit *PlanLimitError
	if errors.As(err, &planLimit) {
		h.recordDiagnostic(
			r.Context(), configuration, abuse.CategoryPlanQuota,
			abuse.ReasonPlanQuotaExhausted,
		)
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanLimitExceeded,
			"monthly feedback limit reached", map[string]any{
				"limit": planLimit.Limit, "usage": planLimit.Usage, "upgrade_url": "/v1/billing",
			})
		return
	}
	if errors.Is(err, ErrInvalidReplay) {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"feedback is invalid", []FieldError{field("session_id", "must identify an unlinked replay session")})
		return
	}
	if errors.Is(err, ErrSubmissionCollision) {
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"feedback idempotency key is already in use", nil)
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit feedback submission: %w", err))
		return
	}
	committed = true
	response, err := h.submissionResponse(
		r.Context(), result.Submission, result.RawTokens, warnings,
	)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	api.WriteJSON(w, status, response)
}

func (h *HTTPHandler) completeAttachment(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorize(w, r, r.Header.Get(widgetapi.PublicKeyHeader), true)
	if !ok {
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	if _, allowed := h.enforceAbusePolicy(
		w, r, configuration, origin, "", abuse.AttachmentCompletionPolicy, 1, false,
	); !allowed {
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
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.CompleteToken == "" {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"attachment completion is invalid", []FieldError{field("complete_token", "is required")})
		return
	}
	tx, ok := h.beginTenantTransaction(w, r, configuration.TenantID)
	if !ok {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	completer := AttachmentCompletionService{
		Store: h.options.Store, Storage: h.options.Storage, Now: h.options.Now,
	}
	attachment, err := completer.Complete(
		r.Context(), tx, configuration.ProjectID, attachmentID, nil, request.CompleteToken,
	)
	var completionError *AttachmentCompletionError
	if errors.As(err, &completionError) && completionError.Failure == AttachmentCompletionExpired {
		if err := tx.Commit(r.Context()); err != nil {
			h.writeStoreError(w, r, fmt.Errorf("commit attachment expiry: %w", err))
			return
		}
		committed = true
		h.recordDiagnostic(
			r.Context(), configuration, abuse.CategoryUploadSafety, abuse.ReasonUploadExpired,
		)
		api.WriteErrorWithOptions(
			w, http.StatusGone, api.CodeConflict, "attachment upload expired", nil,
			api.ErrorOptions{Reason: abuse.ReasonUploadExpired},
		)
		return
	}
	if errors.As(err, &completionError) {
		switch completionError.Failure {
		case AttachmentCompletionNotFound:
			api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "attachment not found", nil)
		case AttachmentCompletionComplete:
			api.WriteError(w, http.StatusConflict, api.CodeConflict, "attachment is already complete", nil)
		case AttachmentCompletionDenied:
			api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "attachment completion denied", nil)
		case AttachmentCompletionMissing:
			api.WriteError(w, http.StatusConflict, api.CodeConflict, "attachment upload is not available", nil)
		case AttachmentCompletionMismatch:
			h.recordDiagnostic(
				r.Context(), configuration, abuse.CategoryUploadSafety,
				abuse.ReasonUploadCompletionMismatch,
			)
			api.WriteErrorWithOptions(
				w, http.StatusUnprocessableEntity, api.CodeValidation,
				"attachment upload is invalid", completionError.Fields,
				api.ErrorOptions{Reason: abuse.ReasonUploadInvalid},
			)
		default:
			h.writeStoreError(w, r, err)
		}
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit attachment completion: %w", err))
		return
	}
	committed = true
	if h.options.Scheduler != nil {
		if err := h.options.Scheduler.ScheduleVerification(
			context.WithoutCancel(r.Context()),
			VerificationTask{
				TenantID: configuration.TenantID, AttachmentID: attachment.ID,
				Generation: attachment.VerificationGeneration,
			},
		); err != nil {
			h.logger.ErrorContext(r.Context(), "schedule attachment safety verification", "error", err,
				"attachment_id", attachment.ID, "feedback_id", attachment.FeedbackID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) authorize(
	w http.ResponseWriter,
	r *http.Request,
	publicKey string,
	requireQueryMatch bool,
) (configuration project.PublicConfiguration, origin string, ok bool) {
	queryKey := r.URL.Query().Get("key")
	if requireQueryMatch && (queryKey == "" || queryKey != publicKey) {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget access denied", nil)
		return configuration, "", false
	}
	origin = r.Header.Get("Origin")
	configuration, err := h.options.Authorizer.Authorize(r.Context(), publicKey, origin)
	if errors.Is(err, widgetapi.ErrAccessDenied) {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget access denied", nil)
		return configuration, "", false
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return configuration, "", false
	}
	if requireQueryMatch && configuration.DeletionRequestedAt != nil {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "tenant is pending deletion and is read-only", nil)
		return configuration, "", false
	}
	return configuration, origin, true
}

func (h *HTTPHandler) allowLegacy(w http.ResponseWriter, r *http.Request, projectID string) bool {
	windows := []struct {
		key    string
		window ratelimit.Window
	}{
		{key: "widget:ip:" + projectID + ":" + httpx.ClientIPFromRequest(r, h.options.TrustedProxyCIDRs), window: ratelimit.Window{Limit: 10, Period: time.Minute}},
		{key: "widget:project:" + projectID, window: ratelimit.Window{Limit: 100, Period: time.Hour}},
	}
	limits := make([]string, 0, len(windows))
	remaining := make([]string, 0, len(windows))
	for _, item := range windows {
		limits = append(limits, fmt.Sprintf("%d;w=%d", item.window.Limit, int(item.window.Period.Seconds())))
		if h.options.Limiter == nil {
			remaining = append(remaining, strconv.Itoa(item.window.Limit))
			continue
		}
		decision, err := h.options.Limiter.Allow(r.Context(), item.key, item.window)
		if err != nil {
			h.logger.WarnContext(r.Context(), "rate limiter unavailable; allowing request", "error", err)
			remaining = append(remaining, "unknown")
			continue
		}
		remaining = append(remaining, strconv.Itoa(decision.Remaining))
		if !decision.Allowed {
			w.Header().Set("X-RateLimit-Limit", strings.Join(limits, ", "))
			w.Header().Set("X-RateLimit-Remaining", strings.Join(remaining, ", "))
			retrySeconds := int((decision.RetryAfter + time.Second - 1) / time.Second)
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded,
				"rate limit exceeded", nil)
			return false
		}
	}
	w.Header().Set("X-RateLimit-Limit", strings.Join(limits, ", "))
	w.Header().Set("X-RateLimit-Remaining", strings.Join(remaining, ", "))
	return true
}

func (h *HTTPHandler) beginTenantTransaction(
	w http.ResponseWriter,
	r *http.Request,
	tenantID string,
) (db.Transaction, bool) {
	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("begin feedback transaction: %w", err))
		return nil, false
	}
	var configured string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, tenantID).
		Scan(&configured); err != nil {
		_ = tx.Rollback(r.Context())
		h.writeStoreError(w, r, fmt.Errorf("set feedback tenant context: %w", err))
		return nil, false
	}
	if configured != tenantID {
		_ = tx.Rollback(r.Context())
		h.writeStoreError(w, r, errors.New("feedback tenant context did not match"))
		return nil, false
	}
	return tx, true
}

func (h *HTTPHandler) submissionResponse(
	ctx context.Context,
	submission Submission,
	rawTokens map[string]string,
	warnings []SubmissionWarning,
) (SubmissionResponse, error) {
	return BuildSubmissionResponse(ctx, h.options.Storage, submission, rawTokens, warnings)
}

func BuildSubmissionResponse(
	ctx context.Context,
	objectStorage ObjectStorage,
	submission Submission,
	rawTokens map[string]string,
	warnings []SubmissionWarning,
) (SubmissionResponse, error) {
	uploads := make([]AttachmentUpload, 0, len(rawTokens))
	for _, attachment := range submission.Attachments {
		raw, exists := rawTokens[attachment.ID]
		if !exists {
			continue
		}
		url, err := objectStorage.PresignPut(ctx, attachment.StorageKey, uploadTTL)
		if err != nil {
			return SubmissionResponse{}, err
		}
		uploads = append(uploads, AttachmentUpload{
			AttachmentID: attachment.ID, ClientAttachmentID: attachment.ClientAttachmentID,
			PresignedURL: url, Method: http.MethodPut,
			Headers:       map[string]string{"Content-Type": attachment.ContentType},
			CompleteToken: raw, ExpiresAt: attachment.UploadExpiresAt,
		})
	}
	return SubmissionResponse{
		FeedbackID: submission.ID, FeedbackPublicID: submission.PublicID,
		IssuePublicID: submission.IssuePublicID, NotifyReporter: submission.NotifyReporter,
		Attachments: uploads, Warnings: warnings,
	}, nil
}

func (h *HTTPHandler) enforceAbusePolicy(
	w http.ResponseWriter,
	r *http.Request,
	configuration project.PublicConfiguration,
	origin string,
	visitorID string,
	policy abuse.Policy,
	cost int,
	degradeOnUnavailable bool,
) (abuse.Decision, bool) {
	if h.options.Abuse == nil {
		if policy.Name == abuse.SubmissionPolicy.Name ||
			policy.Name == abuse.AttachmentCompletionPolicy.Name {
			allowed := h.allowLegacy(w, r, configuration.ProjectID)
			return abuse.Decision{Allowed: allowed, Reason: policy.Reason}, allowed
		}
		return abuse.Decision{Allowed: true, Reason: policy.Reason}, true
	}
	decision, err := h.options.Abuse.Evaluate(r.Context(), policy, abuse.Request{
		Identity: abuse.Identity{
			TenantID: configuration.TenantID, ProjectID: configuration.ProjectID,
			Origin:    originHost(origin),
			Network:   httpx.ClientIPFromRequest(r, h.options.TrustedProxyCIDRs),
			VisitorID: visitorID,
		},
		Cost: cost,
	})
	if err != nil || decision.Unavailable {
		unavailableReason := policy.Name + "_unavailable"
		h.logger.WarnContext(r.Context(), "abuse policy unavailable",
			"error", err, "policy", policy.Name, "reason", unavailableReason,
			"failure_mode", policy.FailureMode)
		if decision.Allowed {
			return decision, true
		}
		h.recordDiagnostic(
			r.Context(), configuration, abuse.CategoryRateLimit, unavailableReason,
		)
		if degradeOnUnavailable {
			return decision, false
		}
		h.writeAbuseError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
			decision.Reason, 5)
		return decision, false
	}
	if decision.Allowed {
		if decision.Limit > 0 {
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
		}
		return decision, true
	}
	h.recordDiagnostic(r.Context(), configuration, abuse.CategoryRateLimit, decision.Reason)
	h.writeAbuseError(
		w, http.StatusTooManyRequests, api.CodeRateLimitExceeded,
		decision.Reason, abuse.RetryAfterSeconds(decision.RetryAfter),
	)
	return decision, false
}

func (h *HTTPHandler) writeAbuseError(
	w http.ResponseWriter,
	status int,
	code api.ErrorCode,
	reason string,
	retrySeconds int,
) {
	if retrySeconds < 1 {
		retrySeconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
	api.WriteErrorWithOptions(w, status, code, "too many attempts; try again shortly", nil,
		api.ErrorOptions{Reason: reason, RetryAfterSeconds: retrySeconds})
}

func (h *HTTPHandler) recordDiagnostic(
	ctx context.Context,
	configuration project.PublicConfiguration,
	category abuse.Category,
	reason string,
) {
	if h.options.Diagnostics == nil {
		return
	}
	if err := h.options.Diagnostics.Record(ctx, abuse.DiagnosticEvent{
		TenantID: configuration.TenantID, ProjectID: configuration.ProjectID,
		Category: category, Reason: reason, OccurredAt: h.now().UTC(),
	}); err != nil {
		h.logger.WarnContext(ctx, "record abuse diagnostic", "error", err,
			"category", category, "reason", reason)
	}
}

func originHost(origin string) string {
	parsed, err := url.Parse(origin)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	return strings.ToLower(strings.TrimSpace(origin))
}

func attachmentTotalBytes(values []AttachmentValues) int64 {
	var total int64
	for _, value := range values {
		total += value.SizeBytes
	}
	return total
}

func uploadUnavailableWarning() SubmissionWarning {
	return SubmissionWarning{
		Code: "service_unavailable", Message: "Feedback saved without attachments. Try again later.",
		Reason: abuse.ReasonUploadRateLimited, RetryAfterSeconds: 5,
	}
}

func elementContextOmittedWarning() SubmissionWarning {
	return SubmissionWarning{
		Code:    "context_omitted",
		Message: "Feedback saved without the selected page element because it could not be reduced safely.",
		Reason:  "element_privacy_policy",
	}
}

func (h *HTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now()
}

func (h *HTTPHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "feedback request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func prepareAttachments(
	configuration project.PublicConfiguration,
	feedbackID string,
	widgetRuleID string,
	values []AttachmentValues,
	aiEnabled bool,
	rawTokens map[string]string,
	now time.Time,
) ([]PreparedAttachment, error) {
	prepared := make([]PreparedAttachment, 0, len(values))
	status := "skipped"
	for _, value := range values {
		attachmentStatus := status
		if aiEnabled {
			var category project.AIInputCategory
			switch value.Kind {
			case AttachmentScreenshot:
				category = project.AIInputScreenshot
			case AttachmentAudio:
				category = project.AIInputAudio
			case AttachmentVideo:
				category = project.AIInputVideo
			}
			if category != "" && ruleAIInputEnabled(configuration, widgetRuleID, category) {
				attachmentStatus = "pending"
			}
		}
		attachmentID := uuid.NewString()
		raw, tokenHash, err := auth.NewOpaqueToken()
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("tenants/%s/feedbacks/%s/attachments/%s/%s.%s",
			configuration.TenantID, feedbackID, attachmentID, value.Kind, attachmentExtension(value))
		prepared = append(prepared, PreparedAttachment{
			Attachment: Attachment{
				ID: attachmentID, ClientAttachmentID: value.ClientAttachmentID, Kind: value.Kind,
				StorageKey: key, ContentType: value.ContentType, SizeBytes: value.SizeBytes,
				DurationSeconds: value.DurationSeconds, SafetyStatus: AttachmentSafetyPending,
				UploadExpiresAt: now.Add(uploadTTL), VerificationGeneration: 1,
			},
			CompleteTokenHash: tokenHash, ProcessingStatus: attachmentStatus,
		})
		rawTokens[attachmentID] = raw
	}
	return prepared, nil
}

func attachmentExtension(value AttachmentValues) string {
	switch value.ContentType {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "audio/ogg":
		return "ogg"
	case "audio/mpeg":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "video/mp4", "audio/mp4":
		return "mp4"
	case "application/json":
		return "json"
	case "application/gzip", "application/x-gzip":
		return "json.gz"
	default:
		return "webm"
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maximumBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "invalid JSON body", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "invalid JSON body", nil)
		return false
	}
	return true
}
