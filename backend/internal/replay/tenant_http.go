package replay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/accessaudit"
	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type TenantTokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

type ReplayStorage interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
	RemoveObject(context.Context, string) error
}

type TenantMergeScheduler interface {
	Schedule(context.Context, string, int) error
}

type TenantHTTPOptions struct {
	Store     Store
	JWT       TenantTokenVerifier
	Storage   ReplayStorage
	Scheduler TenantMergeScheduler
	Logger    *slog.Logger
	Now       func() time.Time
}

type TenantHTTPHandler struct {
	options TenantHTTPOptions
	logger  *slog.Logger
}

func RegisterTenantRoutes(router chi.Router, options TenantHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &TenantHTTPHandler{options: options, logger: logger}
	// The plan gate guards only the standalone list: replays reached through a
	// feedback stay readable after a downgrade, matching the session detail and
	// stream routes below, but the browse-all surface is part of the paid
	// session_replay feature.
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionSessionRead),
		appmiddleware.RequirePlanLimit(appmiddleware.PlanLimitOptions{
			Feature:    config.FeatureReplay,
			UpgradeURL: "/v1/billing",
			Logger:     logger,
		}),
	).Get("/v1/sessions", handler.listSessions)
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionSessionRead),
	).Get("/v1/sessions/{sessionID}", handler.getSession)
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireWritable,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionSessionRead),
		appmiddleware.RequirePermission(auth.PermissionFeedbackUpdate),
	).Post("/v1/sessions/{sessionID}/retry", handler.retrySession)
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionWidgetRuleRead),
	).Get(
		"/v1/projects/{projectID}/widget-rules/{ruleID}/replay-consent-diagnostic",
		handler.getConsentDiagnostic,
	)
	router.With(
		appmiddleware.RequireActive,
		appmiddleware.RequireWritable,
		appmiddleware.RequireTenantMember(options.JWT),
		appmiddleware.RequirePermission(auth.PermissionSessionDelete),
	).Delete("/v1/sessions/{sessionID}", handler.deleteSession)
}

func (h *TenantHTTPHandler) retrySession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(sessionID); err != nil {
		writeTenantReplayNotFound(w)
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK || h.options.Scheduler == nil {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	session, changed, err := h.options.Store.RetrySession(
		r.Context(), query, resolved.ID, sessionID,
	)
	if errors.Is(err, ErrNotFound) {
		writeTenantReplayNotFound(w)
		return
	}
	if errors.Is(err, ErrNotRetryable) {
		api.WriteError(
			w, http.StatusConflict, api.CodeConflict, "replay session cannot be retried", nil,
		)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if changed && !appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
		return h.options.Scheduler.Schedule(ctx, session.ID, session.MergeGeneration)
	}) {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "processing"})
}

func (h *TenantHTTPHandler) getConsentDiagnostic(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	ruleID := chi.URLParam(r, "ruleID")
	if _, err := uuid.Parse(projectID); err != nil {
		writeTenantReplayNotFound(w)
		return
	}
	if _, err := uuid.Parse(ruleID); err != nil {
		writeTenantReplayNotFound(w)
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	now := time.Now().UTC()
	if h.options.Now != nil {
		now = h.options.Now().UTC()
	}
	diagnostic, err := h.options.Store.GetConsentDiagnostic(
		r.Context(), query, resolved.ID, projectID, ruleID, now,
	)
	if errors.Is(err, ErrNotFound) {
		writeTenantReplayNotFound(w)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"diagnostic": diagnostic})
}

func (h *TenantHTTPHandler) getSession(w http.ResponseWriter, r *http.Request) {
	session, ok := h.sessionFromRequest(w, r)
	if !ok {
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	failure, err := h.options.Store.GetFailureDetail(
		r.Context(), query, resolved.ID, session,
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response := tenantSessionResource(session)
	if failure != nil {
		response["failure"] = failure
	}
	api.WriteJSON(w, http.StatusOK, response)
}

func tenantSessionResource(session Session) map[string]any {
	resource := map[string]any{
		"id": session.ID, "project_id": session.ProjectID,
		"session_id": session.ClientSessionID, "page_url": session.PageURL,
		"started_at": session.StartedAt, "ended_at": session.EndedAt,
		"events_count": session.EventsCount, "batches_count": session.BatchesCount,
		"rrweb_version": session.RRWebVersion, "merged_at": session.MergedAt,
		"status": session.Status(), "feedback_id": session.FeedbackID,
		"retention_deleted_at": session.RetentionDeletedAt,
	}
	if session.Status() == "ready" {
		resource["replay_url"] = "/v1/sessions/" + session.ID + "/replay"
	}
	return resource
}

func (h *TenantHTTPHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	options, fields := parseTenantSessionListOptions(r)
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"request validation failed", map[string]any{"fields": fields})
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	sessions, err := h.options.Store.ListSessions(r.Context(), query, resolved.ID, options)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	hasMore := len(sessions) > options.Limit
	if hasMore {
		sessions = sessions[:options.Limit]
	}
	var nextCursor *string
	if hasMore && len(sessions) > 0 {
		encoded, err := encodeTenantSessionCursor(sessions[len(sessions)-1])
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		nextCursor = &encoded
	}
	resources := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		resources = append(resources, tenantSessionResource(session))
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"sessions": resources,
		"pagination": map[string]any{
			"next_cursor": nextCursor,
			"has_more":    hasMore,
			"limit":       options.Limit,
		},
	})
}

func (h *TenantHTTPHandler) deleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(sessionID); err != nil {
		writeTenantReplayNotFound(w)
		return
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	keys, err := h.options.Store.DeleteSession(r.Context(), query, resolved.ID, sessionID)
	if errors.Is(err, ErrNotFound) {
		writeTenantReplayNotFound(w)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if len(keys) > 0 && h.options.Storage != nil {
		appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
			var result error
			for _, key := range keys {
				if err := h.options.Storage.RemoveObject(ctx, key); err != nil {
					result = errors.Join(result, err)
				}
			}
			return result
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TenantHTTPHandler) sessionFromRequest(w http.ResponseWriter, r *http.Request) (Session, bool) {
	sessionID := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(sessionID); err != nil {
		writeTenantReplayNotFound(w)
		return Session{}, false
	}
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return Session{}, false
	}
	session, err := h.options.Store.GetSession(r.Context(), query, resolved.ID, sessionID)
	if errors.Is(err, ErrNotFound) {
		writeTenantReplayNotFound(w)
		return Session{}, false
	}
	if err != nil {
		h.writeError(w, r, err)
		return Session{}, false
	}
	return session, true
}

func (h *TenantHTTPHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "tenant replay request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func writeTenantReplayNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "replay session not found", nil)
}

type StreamHTTPOptions struct {
	BaseDomain string
	Resolver   tenant.Resolver
	Begin      db.BeginFunc
	Store      Store
	JWT        TenantTokenVerifier
	Storage    ReplayStorage
	Logger     *slog.Logger
	Now        func() time.Time
}

type StreamHTTPHandler struct {
	options StreamHTTPOptions
	logger  *slog.Logger
}

func NewStreamHTTPHandler(options StreamHTTPOptions) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &StreamHTTPHandler{options: options, logger: logger}
}

func (h *StreamHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		api.WriteError(w, http.StatusMethodNotAllowed, api.CodeBadRequest, "method not allowed", nil)
		return
	}
	sessionID := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(sessionID); err != nil {
		writeTenantReplayNotFound(w)
		return
	}
	resolved, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	var configured string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, resolved.ID).Scan(&configured); err != nil || configured != resolved.ID {
		h.writeError(w, r, fmt.Errorf("set replay stream tenant context: %w", err))
		return
	}
	claims, err := appmiddleware.AuthenticateTenantCredential(
		r.Context(), tx, h.options.JWT, resolved.ID, r.Header.Get("Authorization"),
		resolved.DeletionRequestedAt == nil,
	)
	if appmiddleware.WriteTenantAPIKeyRateLimitError(w, r.Context(), err) {
		return
	}
	if errors.Is(err, appmiddleware.ErrTenantCredentialInvalid) {
		api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "authentication required", nil)
		return
	}
	if errors.Is(err, appmiddleware.ErrTenantCredentialForbidden) {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "tenant access denied", nil)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	appmiddleware.WriteTenantAPIKeyRateLimitHeaders(w, r.Context())
	recordAccess := func(outcome string) error {
		return accessaudit.Record(r.Context(), tx, accessaudit.RecordInput{
			TenantID: resolved.ID, Kind: "replay_session.accessed",
			Actor:          accessaudit.ActorFromClaims(claims),
			ResourceType:   accessaudit.ResourceReplaySession,
			ResourceID:     sessionID,
			Action:         "playback",
			PermissionPath: string(auth.PermissionSessionRead),
			RequestID:      appmiddleware.RequestIDFromContext(r.Context()),
			Outcome:        outcome,
			DedupWindow:    accessaudit.DefaultDedupWindow,
		})
	}
	if !auth.HasPermission(claims.Permissions, auth.PermissionSessionRead) {
		// Audit recording and the authorization decision commit atomically:
		// if persistence fails we fail closed and never disclose whether the
		// session exists.
		if err := recordAccess(accessaudit.OutcomeDenied); err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeError(w, r, err)
			return
		}
		committed = true
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "permission denied", nil)
		return
	}
	session, err := h.options.Store.GetSession(r.Context(), tx, resolved.ID, sessionID)
	if errors.Is(err, ErrNotFound) ||
		(err == nil && (session.Status() != "ready" || session.MergedAt == nil || session.StorageKey == nil)) {
		if err := recordAccess(accessaudit.OutcomeNotFound); err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeError(w, r, err)
			return
		}
		committed = true
		writeTenantReplayNotFound(w)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.options.Storage == nil {
		h.writeError(w, r, errors.New("replay storage is unavailable"))
		return
	}
	object, err := h.options.Storage.OpenObject(r.Context(), *session.StorageKey)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	streamOwnsObject := false
	defer func() {
		if !streamOwnsObject {
			closeReplayObject(r, object, session.ID, h.logger)
		}
	}()
	if err := recordAccess(accessaudit.OutcomeSuccess); err != nil {
		h.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeError(w, r, err)
		return
	}
	committed = true
	streamOwnsObject = true
	writeReplayStreamContent(w, r, object, session.ID, h.logger)
}

// writeReplayStreamContent streams an already-opened, authorized merged replay
// object. Shared by the tenant-facing stream above and the
// platform-admin/support stream (platform_support_stream.go) so the content
// type, caching, and copy behavior stay identical regardless of which access
// path authorized the read.
func writeReplayStreamContent(
	w http.ResponseWriter,
	r *http.Request,
	object io.ReadCloser,
	sessionID string,
	logger *slog.Logger,
) {
	defer closeReplayObject(r, object, sessionID, logger)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, object); err != nil {
		logger.WarnContext(r.Context(), "stream replay object", "error", err, "session_id", sessionID)
	}
}

func closeReplayObject(r *http.Request, object io.Closer, sessionID string, logger *slog.Logger) {
	if err := object.Close(); err != nil {
		logger.WarnContext(r.Context(), "close replay object", "error", err, "session_id", sessionID)
	}
}

func (h *StreamHTTPHandler) resolveTenant(w http.ResponseWriter, r *http.Request) (tenant.Tenant, bool) {
	slug, err := tenant.SlugFromHost(r.Host, h.options.BaseDomain)
	if err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeUnknownTenant, "unknown tenant", nil)
		return tenant.Tenant{}, false
	}
	resolved, err := h.options.Resolver.Resolve(r.Context(), slug)
	if errors.Is(err, tenant.ErrUnknownTenant) {
		api.WriteError(w, http.StatusNotFound, api.CodeUnknownTenant, "unknown tenant", nil)
		return tenant.Tenant{}, false
	}
	if err != nil {
		h.writeError(w, r, err)
		return tenant.Tenant{}, false
	}
	now := time.Now()
	if h.options.Now != nil {
		now = h.options.Now()
	}
	if resolved.DeletionRequestedAt == nil && resolved.Status != tenant.StatusActive &&
		(resolved.Status != tenant.StatusTrial || resolved.TrialEndsAt == nil || !resolved.TrialEndsAt.After(now)) {
		api.WriteError(w, http.StatusForbidden, api.CodeTenantSuspended, "tenant is not active", nil)
		return tenant.Tenant{}, false
	}
	return resolved, true
}

func (h *StreamHTTPHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "replay stream request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
