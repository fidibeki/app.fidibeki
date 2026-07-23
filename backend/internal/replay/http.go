package replay

import (
	"context"
	"crypto/subtle"
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

	"github.com/acidiney/app.fidibeki/backend/internal/abuse"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	replayUploadTTL       = 10 * time.Minute
	maximumReplayBodySize = 256 << 10
	maximumBatchSize      = 2 << 20
)

type ObjectStorage interface {
	PresignPut(context.Context, string, time.Duration) (string, error)
	Stat(context.Context, string) (storage.ObjectInfo, error)
	RemoveObject(context.Context, string) error
}

type MergeScheduler interface {
	Schedule(context.Context, string) error
}

type BatchVerificationScheduler interface {
	ScheduleBatch(context.Context, BatchVerificationTask) error
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
	Storage           ObjectStorage
	Scheduler         MergeScheduler
	Verifier          BatchVerificationScheduler
	Abuse             AbuseEvaluator
	Diagnostics       AbuseDiagnosticRecorder
	TrustedProxyCIDRs []netip.Prefix
	Logger            *slog.Logger
	Now               func() time.Time
}

type HTTPHandler struct {
	options HTTPOptions
	logger  *slog.Logger
}

func RegisterPublicRoutes(router chi.Router, options HTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{options: options, logger: logger}
	router.Post("/v1/replay/sessions", handler.createSession)
	router.Options("/v1/replay/sessions", handler.preflight)
	router.Post("/v1/replay/sessions/{sessionID}/batches", handler.declareBatch)
	router.Options("/v1/replay/sessions/{sessionID}/batches", handler.preflight)
	router.Post("/v1/replay/sessions/{sessionID}/batches/{batchIndex}/complete", handler.completeBatch)
	router.Options("/v1/replay/sessions/{sessionID}/batches/{batchIndex}/complete", handler.preflight)
	router.Post("/v1/replay/sessions/{sessionID}/finalize", handler.finalizeSession)
	router.Options("/v1/replay/sessions/{sessionID}/finalize", handler.preflight)
}

func (h *HTTPHandler) preflight(w http.ResponseWriter, r *http.Request) {
	_, origin, ok := h.authorize(w, r, r.URL.Query().Get("key"), false)
	if !ok {
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+widgetapi.PublicKeyHeader)
	w.Header().Set("Access-Control-Max-Age", "300")
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) createSession(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorizeRequest(w, r)
	if !ok {
		return
	}
	var request struct {
		SessionID    string `json:"session_id"`
		WidgetRuleID string `json:"widget_rule_id"`
		PageURL      string `json:"page_url"`
		VisitorID    string `json:"visitor_id"`
	}
	if !decodeReplayJSON(w, r, &request) {
		return
	}
	rule, canonicalPageURL, valid := h.validateReplayStart(
		w, configuration, request.SessionID, request.WidgetRuleID, request.PageURL, request.VisitorID,
	)
	if !valid {
		return
	}
	if _, allowed := h.enforceAbusePolicy(
		w, r, configuration, origin, strings.TrimSpace(request.VisitorID),
		strings.TrimSpace(request.SessionID), abuse.ReplayStartPolicy, 1,
	); !allowed {
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
	session, err := h.options.Store.CreateSession(
		r.Context(), tx, configuration.TenantID, configuration.ProjectID,
		strings.TrimSpace(request.SessionID), canonicalPageURL, h.now(),
		min(rule.ReplayMaxDurationSeconds, project.MaximumReplayDurationSeconds),
		min(rule.ReplayMaxEvents, project.MaximumReplayEvents),
	)
	if errors.Is(err, ErrPrivacyContract) {
		api.WriteError(
			w, http.StatusConflict, api.CodeConflict,
			"replay session must be restarted under the current privacy contract", nil,
		)
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit replay session creation: %w", err))
		return
	}
	committed = true
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": session.ID, "session_id": session.ClientSessionID,
		"rrweb_version":        session.RRWebVersion,
		"max_duration_seconds": session.MaxDurationSeconds,
		"max_events":           session.MaxEvents,
	})
}

func (h *HTTPHandler) declareBatch(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorizeRequest(w, r)
	if !ok || !h.requireReplayPlan(w, configuration) {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(sessionID); err != nil {
		h.writeNotFound(w)
		return
	}
	var request struct {
		BatchIndex  int   `json:"batch_index"`
		EventsCount int   `json:"events_count"`
		SizeBytes   int64 `json:"size_bytes"`
	}
	if !decodeReplayJSON(w, r, &request) {
		return
	}
	fields := make([]map[string]string, 0, 3)
	if request.BatchIndex < 0 || request.BatchIndex > 999 {
		fields = append(fields, replayField("batch_index", "must be between 0 and 999"))
	}
	if request.EventsCount < 1 || request.EventsCount > 50 {
		fields = append(fields, replayField("events_count", "must be between 1 and 50"))
	}
	if request.SizeBytes < 1 || request.SizeBytes > maximumBatchSize {
		fields = append(fields, replayField("size_bytes", fmt.Sprintf("must be between 1 and %d", maximumBatchSize)))
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "replay batch is invalid", fields)
		return
	}
	if _, allowed := h.enforceAbusePolicy(
		w, r, configuration, origin, "", sessionID, abuse.ReplayBatchPolicy, 1,
	); !allowed {
		return
	}
	mebibytes := int((request.SizeBytes + (1 << 20) - 1) / (1 << 20))
	if _, allowed := h.enforceAbusePolicy(
		w, r, configuration, origin, "", sessionID, abuse.ReplayBytePolicy, mebibytes,
	); !allowed {
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
	batch, token, err := h.options.Store.DeclareBatch(
		r.Context(), tx, configuration.TenantID, configuration.ProjectID, sessionID,
		request.BatchIndex, request.EventsCount, request.SizeBytes, h.now(),
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit replay batch declaration: %w", err))
		return
	}
	committed = true
	presigned, err := h.options.Storage.PresignPut(r.Context(), batch.StorageKey, replayUploadTTL)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, BatchUpload{
		BatchIndex: batch.Index, PresignedURL: presigned, Method: http.MethodPut,
		Headers:       map[string]string{"Content-Type": batch.ContentType},
		CompleteToken: token, ExpiresAt: batch.UploadExpiresAt,
	})
}

func (h *HTTPHandler) completeBatch(w http.ResponseWriter, r *http.Request) {
	configuration, origin, ok := h.authorizeRequest(w, r)
	if !ok || !h.requireReplayPlan(w, configuration) {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	batchIndex, indexErr := strconv.Atoi(chi.URLParam(r, "batchIndex"))
	if _, err := uuid.Parse(sessionID); err != nil || indexErr != nil || batchIndex < 0 {
		h.writeNotFound(w)
		return
	}
	var request struct {
		CompleteToken string `json:"complete_token"`
	}
	if !decodeReplayJSON(w, r, &request) {
		return
	}
	if request.CompleteToken == "" {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"replay batch completion is invalid", []map[string]string{replayField("complete_token", "is required")})
		return
	}
	if _, allowed := h.enforceAbusePolicy(
		w, r, configuration, origin, "", sessionID, abuse.ReplayBatchPolicy, 1,
	); !allowed {
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
	batch, err := h.options.Store.LockBatch(
		r.Context(), tx, configuration.TenantID, configuration.ProjectID, sessionID, batchIndex,
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if batch.SafetyStatus != BatchSafetyPending || batch.CompletedAt != nil ||
		batch.CompleteTokenHash == nil {
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "replay batch is already complete", nil)
		return
	}
	if !h.now().Before(batch.UploadExpiresAt) {
		if err := h.options.Store.ExpireBatch(r.Context(), tx, batch.ID, h.now()); err != nil {
			h.writeDomainError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeStoreError(w, r, fmt.Errorf("commit replay batch expiry: %w", err))
			return
		}
		committed = true
		h.recordDiagnostic(
			r.Context(), configuration, abuse.CategoryUploadSafety, abuse.ReasonUploadExpired,
		)
		api.WriteErrorWithOptions(
			w, http.StatusGone, api.CodeConflict, "replay batch upload expired", nil,
			api.ErrorOptions{Reason: abuse.ReasonUploadExpired},
		)
		return
	}
	want := auth.HashToken(request.CompleteToken)
	if subtle.ConstantTimeCompare([]byte(want), []byte(*batch.CompleteTokenHash)) != 1 {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "replay batch completion denied", nil)
		return
	}
	object, err := h.options.Storage.Stat(r.Context(), batch.StorageKey)
	if err != nil {
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "replay batch upload is not available", nil)
		return
	}
	fields := make([]map[string]string, 0, 2)
	if object.Size != batch.SizeBytes {
		fields = append(fields, replayField("size_bytes", "uploaded object size does not match the declaration"))
	}
	if normalizeReplayContentType(object.ContentType) != batch.ContentType {
		fields = append(fields, replayField("content_type", "uploaded object MIME type must be application/json"))
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "replay batch upload is invalid", fields)
		return
	}
	if err := h.options.Store.CompleteBatch(r.Context(), tx, sessionID, batch.ID, h.now()); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit replay batch completion: %w", err))
		return
	}
	committed = true
	if h.options.Verifier != nil {
		task := BatchVerificationTask{
			TenantID: batch.TenantID, BatchID: batch.ID, Generation: batch.Generation,
		}
		if err := h.options.Verifier.ScheduleBatch(context.WithoutCancel(r.Context()), task); err != nil {
			h.logger.ErrorContext(
				r.Context(), "schedule replay batch verification",
				"error", err, "batch_id", batch.ID,
			)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) finalizeSession(w http.ResponseWriter, r *http.Request) {
	configuration, _, ok := h.authorizeRequest(w, r)
	if !ok || !h.requireReplayPlan(w, configuration) {
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(sessionID); err != nil {
		h.writeNotFound(w)
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
	session, changed, err := h.options.Store.Finalize(
		r.Context(), tx, configuration.TenantID, configuration.ProjectID, sessionID, h.now(),
	)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit replay finalization: %w", err))
		return
	}
	committed = true
	if changed && h.options.Scheduler != nil {
		if err := h.options.Scheduler.Schedule(context.WithoutCancel(r.Context()), session.ID); err != nil {
			h.logger.ErrorContext(r.Context(), "schedule replay merge", "error", err, "session_id", session.ID)
		}
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]any{"id": session.ID, "status": session.Status()})
}

func (h *HTTPHandler) authorizeRequest(
	w http.ResponseWriter,
	r *http.Request,
) (project.PublicConfiguration, string, bool) {
	configuration, origin, ok := h.authorize(w, r, r.Header.Get(widgetapi.PublicKeyHeader), true)
	if ok {
		widgetapi.SetCORSHeaders(w, origin)
	}
	return configuration, origin, ok
}

func (h *HTTPHandler) authorize(
	w http.ResponseWriter,
	r *http.Request,
	publicKey string,
	requireQueryMatch bool,
) (project.PublicConfiguration, string, bool) {
	queryKey := r.URL.Query().Get("key")
	if requireQueryMatch && (queryKey == "" || queryKey != publicKey) {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget access denied", nil)
		return project.PublicConfiguration{}, "", false
	}
	origin := r.Header.Get("Origin")
	configuration, err := h.options.Authorizer.Authorize(r.Context(), publicKey, origin)
	if errors.Is(err, widgetapi.ErrAccessDenied) {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget access denied", nil)
		return project.PublicConfiguration{}, "", false
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return project.PublicConfiguration{}, "", false
	}
	if requireQueryMatch && configuration.DeletionRequestedAt != nil {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "tenant is pending deletion and is read-only", nil)
		return project.PublicConfiguration{}, "", false
	}
	return configuration, origin, true
}

func (h *HTTPHandler) validateReplayStart(
	w http.ResponseWriter,
	configuration project.PublicConfiguration,
	clientSessionID string,
	widgetRuleID string,
	pageURL string,
	visitorID string,
) (project.WidgetRule, string, bool) {
	if !h.requireReplayPlan(w, configuration) {
		return project.WidgetRule{}, "", false
	}
	fields := make([]map[string]string, 0, 3)
	if _, err := uuid.Parse(strings.TrimSpace(clientSessionID)); err != nil {
		fields = append(fields, replayField("session_id", "must be a UUID"))
	}
	canonicalURL, urlValid := canonicalReplayURL(strings.TrimSpace(pageURL))
	if !urlValid {
		fields = append(fields, replayField("page_url", "must be an absolute HTTP or HTTPS URL"))
	}
	if value := strings.TrimSpace(visitorID); value != "" {
		if _, err := uuid.Parse(value); err != nil {
			fields = append(fields, replayField("visitor_id", "must be a UUID"))
		}
	}
	var selected *project.WidgetRule
	for index := range configuration.Rules {
		if configuration.Rules[index].ID == strings.TrimSpace(widgetRuleID) {
			selected = &configuration.Rules[index]
			break
		}
	}
	if selected == nil || !selected.Enabled || selected.ReplayMode == project.ReplayNone ||
		(selected.ReplayMode == project.ReplaySampling && selected.ReplaySamplingRate == 0) {
		fields = append(fields, replayField("widget_rule_id", "must identify a replay-enabled widget rule"))
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "replay session is invalid", fields)
		return project.WidgetRule{}, "", false
	}
	return *selected, canonicalURL, true
}

func (h *HTTPHandler) requireReplayPlan(w http.ResponseWriter, configuration project.PublicConfiguration) bool {
	definition, exists := config.PlanFor(configuration.Plan)
	if !exists {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return false
	}
	if !definition.Features.Replay {
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanFeatureUnavailable,
			"session replay is not available on this plan", map[string]string{
				"feature": string(config.FeatureReplay), "upgrade_url": "/v1/billing",
			})
		return false
	}
	return true
}

func (h *HTTPHandler) beginTenantTransaction(
	w http.ResponseWriter,
	r *http.Request,
	tenantID string,
) (db.Transaction, bool) {
	if h.options.Begin == nil {
		h.writeStoreError(w, r, errors.New("replay transaction initializer is unavailable"))
		return nil, false
	}
	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("begin replay transaction: %w", err))
		return nil, false
	}
	var configured string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, tenantID).Scan(&configured); err != nil {
		_ = tx.Rollback(r.Context())
		h.writeStoreError(w, r, fmt.Errorf("set replay tenant context: %w", err))
		return nil, false
	}
	if configured != tenantID {
		_ = tx.Rollback(r.Context())
		h.writeStoreError(w, r, errors.New("replay tenant context did not match"))
		return nil, false
	}
	return tx, true
}

func (h *HTTPHandler) enforceAbusePolicy(
	w http.ResponseWriter,
	r *http.Request,
	configuration project.PublicConfiguration,
	origin string,
	visitorID string,
	sessionID string,
	policy abuse.Policy,
	cost int,
) (abuse.Decision, bool) {
	if h.options.Abuse == nil {
		return abuse.Decision{Allowed: true, Reason: policy.Reason}, true
	}
	decision, err := h.options.Abuse.Evaluate(r.Context(), policy, abuse.Request{
		Identity: abuse.Identity{
			TenantID: configuration.TenantID, ProjectID: configuration.ProjectID,
			Origin:    replayOriginHost(origin),
			Network:   httpx.ClientIPFromRequest(r, h.options.TrustedProxyCIDRs),
			VisitorID: visitorID, SessionID: sessionID,
		},
		Cost: cost,
	})
	if err != nil {
		h.logger.WarnContext(r.Context(), "replay abuse policy unavailable",
			"error", err, "policy", policy.Name, "fail_open", decision.Allowed)
		if decision.Allowed {
			return decision, true
		}
		h.recordDiagnostic(
			r.Context(), configuration, abuse.CategoryRateLimit, policy.Name+"_unavailable",
		)
		h.writeAbuseError(
			w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, decision.Reason, 5,
		)
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
	api.WriteErrorWithOptions(
		w, status, code, "too many replay requests; try again shortly", nil,
		api.ErrorOptions{Reason: reason, RetryAfterSeconds: retrySeconds},
	)
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
		Category: category, Reason: reason, OccurredAt: h.now(),
	}); err != nil {
		h.logger.WarnContext(ctx, "record replay abuse diagnostic",
			"error", err, "category", category, "reason", reason)
	}
}

func replayOriginHost(origin string) string {
	parsed, err := url.Parse(origin)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	return strings.ToLower(strings.TrimSpace(origin))
}

func (h *HTTPHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		h.writeNotFound(w)
	case errors.Is(err, ErrBatchComplete), errors.Is(err, ErrSessionFinalized):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, err.Error(), nil)
	case errors.Is(err, ErrIncompleteBatches):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "replay session has incomplete or no batches", nil)
	case errors.Is(err, ErrEventLimit):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"replay session event limit exceeded", []map[string]string{replayField("events_count", "exceeds the session limit")})
	case errors.Is(err, ErrBatchLimit):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"replay session batch limit exceeded", []map[string]string{replayField("batch_index", "exceeds the session limit")})
	case errors.Is(err, ErrByteLimit):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"replay session byte limit exceeded", []map[string]string{replayField("size_bytes", "exceeds the session limit")})
	default:
		h.writeStoreError(w, r, err)
	}
}

func (h *HTTPHandler) writeNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "replay session not found", nil)
}

func (h *HTTPHandler) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "replay request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func (h *HTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func decodeReplayJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maximumReplayBodySize)
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

// canonicalReplayURL reduces a page URL to origin + normalized pathname, stripping
// credentials, query parameters, and the fragment before the URL is transmitted or
// persisted (issue #53). This mirrors widget/src/targeting.ts's canonicalPage so a replay
// URL is anonymized identically whether the widget canonicalizes it or a legacy/compromised
// client sends a raw URL and the backend must enforce the contract itself.
func canonicalReplayURL(value string) (string, bool) {
	if value == "" || len(value) > 4096 {
		return "", false
	}
	canonical, err := project.CanonicalizePage(value)
	if err != nil {
		return "", false
	}
	return canonical.Target, true
}

func normalizeReplayContentType(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
}

func replayField(name string, message string) map[string]string {
	return map[string]string{"field": name, "message": message}
}
