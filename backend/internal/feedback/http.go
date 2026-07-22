package feedback

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
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
}

type AttachmentScheduler interface {
	ScheduleAttachment(context.Context, string, string) error
}

type HTTPOptions struct {
	Authorizer       widgetapi.ProjectAuthorizer
	Begin            db.BeginFunc
	Store            Store
	Limiter          ratelimit.Limiter
	Storage          ObjectStorage
	Scheduler        AttachmentScheduler
	Logger           *slog.Logger
	Now              func() time.Time
	GeneratePublicID func() (string, error)
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
	if !h.allow(w, r, configuration.ProjectID) {
		return
	}
	var request SubmissionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Honeypot) != "" {
		api.WriteJSON(w, http.StatusOK, map[string]bool{"accepted": true})
		return
	}
	values, fields := ValidateSubmission(request, configuration)
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "feedback is invalid", fields)
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
	if err := h.options.Store.LockSubmission(
		r.Context(), tx, configuration.ProjectID, values.ClientFeedbackID,
	); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	submission, exists, err := h.options.Store.FindSubmission(
		r.Context(), tx, configuration.ProjectID, values.ClientFeedbackID,
	)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	rawTokens := map[string]string{}
	if exists {
		if err := h.rotateIncompleteTokens(r.Context(), tx, &submission, rawTokens); err != nil {
			h.writeStoreError(w, r, err)
			return
		}
	} else {
		if values.ReplaySessionID != nil {
			if err := h.options.Store.LockReplaySession(
				r.Context(), tx, configuration.TenantID, configuration.ProjectID, *values.ReplaySessionID,
			); err != nil {
				if errors.Is(err, ErrInvalidReplay) {
					api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
						"feedback is invalid", []FieldError{field("session_id", "must identify an unlinked replay session")})
					return
				}
				h.writeStoreError(w, r, err)
				return
			}
		}
		definition, planExists := config.PlanFor(configuration.Plan)
		if !planExists {
			h.writeStoreError(w, r, errors.New("unsupported tenant plan"))
			return
		}
		usage, err := h.options.Store.CountMonthly(r.Context(), tx, configuration.TenantID, h.now())
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		if definition.Limits.FeedbackPerMonth >= 0 && usage >= definition.Limits.FeedbackPerMonth {
			api.WriteError(w, http.StatusPaymentRequired, api.CodePlanLimitExceeded,
				"monthly feedback limit reached", map[string]any{
					"limit": definition.Limits.FeedbackPerMonth, "usage": usage, "upgrade_url": "/v1/billing",
				})
			return
		}
		feedbackID := NewID()
		prepared, err := prepareAttachments(
			configuration, feedbackID, values.Attachments, definition.Features.AI, rawTokens,
		)
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		publicIDs, err := h.publicIDCandidates()
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		submission, err = h.options.Store.CreateSubmission(
			r.Context(), tx, configuration, values, feedbackID, publicIDs, prepared,
		)
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit feedback submission: %w", err))
		return
	}
	committed = true
	response, err := h.submissionResponse(r.Context(), submission, rawTokens)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	status := http.StatusCreated
	if exists {
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
	if !h.allow(w, r, configuration.ProjectID) {
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
	attachment, err := h.options.Store.LockAttachment(r.Context(), tx, configuration.ProjectID, attachmentID)
	if errors.Is(err, ErrNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "attachment not found", nil)
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if attachment.CompletedAt != nil || attachment.CompleteTokenHash == nil {
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "attachment is already complete", nil)
		return
	}
	wantHash := auth.HashToken(request.CompleteToken)
	if subtle.ConstantTimeCompare([]byte(wantHash), []byte(*attachment.CompleteTokenHash)) != 1 {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "attachment completion denied", nil)
		return
	}
	object, err := h.options.Storage.Stat(r.Context(), attachment.StorageKey)
	if err != nil {
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "attachment upload is not available", nil)
		return
	}
	fields := make([]FieldError, 0, 2)
	if object.Size != attachment.SizeBytes {
		fields = append(fields, field("size_bytes", "uploaded object size does not match the declaration"))
	}
	if normalizeContentType(object.ContentType) != normalizeContentType(attachment.ContentType) {
		fields = append(fields, field("content_type", "uploaded object MIME type does not match the declaration"))
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"attachment upload is invalid", fields)
		return
	}
	if err := h.options.Store.CompleteAttachment(r.Context(), tx, attachment.ID, h.now().UTC()); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit attachment completion: %w", err))
		return
	}
	committed = true
	if h.options.Scheduler != nil {
		if err := h.options.Scheduler.ScheduleAttachment(
			context.WithoutCancel(r.Context()), attachment.ID, attachment.FeedbackID,
		); err != nil {
			h.logger.ErrorContext(r.Context(), "schedule completed attachment AI", "error", err,
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

func (h *HTTPHandler) allow(w http.ResponseWriter, r *http.Request, projectID string) bool {
	windows := []struct {
		key    string
		window ratelimit.Window
	}{
		{key: "widget:ip:" + projectID + ":" + clientIP(r), window: ratelimit.Window{Limit: 10, Period: time.Minute}},
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

func (h *HTTPHandler) rotateIncompleteTokens(
	ctx context.Context,
	query db.Querier,
	submission *Submission,
	rawTokens map[string]string,
) error {
	for index := range submission.Attachments {
		attachment := &submission.Attachments[index]
		if attachment.CompletedAt != nil {
			continue
		}
		raw, tokenHash, err := auth.NewOpaqueToken()
		if err != nil {
			return err
		}
		if err := h.options.Store.RotateCompletionToken(
			ctx, query, attachment.ID, tokenHash, h.now().UTC(),
		); err != nil {
			return err
		}
		rawTokens[attachment.ID] = raw
		attachment.CompleteTokenHash = &tokenHash
	}
	return nil
}

func (h *HTTPHandler) publicIDCandidates() ([]string, error) {
	generator := h.options.GeneratePublicID
	if generator == nil {
		generator = GeneratePublicID
	}
	values := make([]string, 3)
	for index := range values {
		value, err := generator()
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

func (h *HTTPHandler) submissionResponse(
	ctx context.Context,
	submission Submission,
	rawTokens map[string]string,
) (SubmissionResponse, error) {
	expiresAt := h.now().UTC().Add(uploadTTL)
	uploads := make([]AttachmentUpload, 0, len(rawTokens))
	for _, attachment := range submission.Attachments {
		raw, exists := rawTokens[attachment.ID]
		if !exists {
			continue
		}
		url, err := h.options.Storage.PresignPut(ctx, attachment.StorageKey, uploadTTL)
		if err != nil {
			return SubmissionResponse{}, err
		}
		uploads = append(uploads, AttachmentUpload{
			AttachmentID: attachment.ID, ClientAttachmentID: attachment.ClientAttachmentID,
			PresignedURL: url, Method: http.MethodPut,
			Headers:       map[string]string{"Content-Type": attachment.ContentType},
			CompleteToken: raw, ExpiresAt: expiresAt,
		})
	}
	return SubmissionResponse{
		FeedbackID: submission.ID, FeedbackPublicID: submission.PublicID,
		IssuePublicID: submission.IssuePublicID, NotifyReporter: submission.NotifyReporter,
		Attachments: uploads,
	}, nil
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
	values []AttachmentValues,
	aiEnabled bool,
	rawTokens map[string]string,
) ([]PreparedAttachment, error) {
	prepared := make([]PreparedAttachment, 0, len(values))
	status := "skipped"
	if aiEnabled {
		status = "pending"
	}
	for _, value := range values {
		attachmentStatus := status
		if value.Kind == AttachmentReplay {
			attachmentStatus = "skipped"
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
				DurationSeconds: value.DurationSeconds,
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

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
		return forwarded
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}
	return "unknown"
}
