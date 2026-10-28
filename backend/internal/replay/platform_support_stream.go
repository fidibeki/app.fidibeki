package replay

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/fidibeki/app.fidibeki/backend/internal/accessaudit"
	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PlatformSupportTokenVerifier verifies a platform-admin bearer token. It is
// satisfied by *auth.JWTManager.
type PlatformSupportTokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

// PlatformSupportStreamHTTPOptions configures the platform-admin/support
// access path to a tenant's merged replay session. It reuses the same store
// and storage backend as the tenant-facing stream (tenant_http.go) but
// requires platform-admin authentication
// (auth.PermissionPlatformTenantSupportAccess) plus an explicit, validated
// support reason and case reference, and records access as
// actor_type=platform_admin so support access stays separately identifiable
// from ordinary tenant-member/API-key reads.
type PlatformSupportStreamHTTPOptions struct {
	Begin   db.BeginFunc
	Store   Store
	JWT     PlatformSupportTokenVerifier
	Storage ReplayStorage
	Logger  *slog.Logger
}

type platformSupportStreamHandler struct {
	options PlatformSupportStreamHTTPOptions
	logger  *slog.Logger
}

// RegisterPlatformSupportRoutes mounts the platform-admin/support replay
// access path on the platform-host router (see backend/cmd/fidibeki-api).
func RegisterPlatformSupportRoutes(router chi.Router, options PlatformSupportStreamHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &platformSupportStreamHandler{options: options, logger: logger}
	router.With(
		appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantSupportAccess),
	).Get(
		"/admin/tenants/{tenantID}/sessions/{sessionID}/support-access",
		handler.serveHTTP,
	)
}

func (h *platformSupportStreamHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	sessionID := chi.URLParam(r, "sessionID")
	if _, err := uuid.Parse(tenantID); err != nil {
		writeTenantReplayNotFound(w)
		return
	}
	if _, err := uuid.Parse(sessionID); err != nil {
		writeTenantReplayNotFound(w)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	supportRequest, requestErr := accessaudit.ValidateSupportRequest(accessaudit.SupportRequest{
		Reason:        r.Header.Get(accessaudit.SupportReasonHeader),
		CaseReference: r.Header.Get(accessaudit.SupportCaseReferenceHeader),
	})

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
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, tenantID).Scan(&configured); err != nil || configured != tenantID {
		h.writeError(w, r, fmt.Errorf("set support replay stream tenant context: %w", err))
		return
	}
	recordAccess := func(outcome string, data map[string]any) error {
		return accessaudit.Record(r.Context(), tx, accessaudit.RecordInput{
			TenantID: tenantID, Kind: "replay_session.support_accessed",
			Actor:          accessaudit.PlatformAdminActor(claims.Subject),
			ResourceType:   accessaudit.ResourceReplaySession,
			ResourceID:     sessionID,
			Action:         "playback",
			PermissionPath: accessaudit.PermissionPathSupportAccess,
			RequestID:      appmiddleware.RequestIDFromContext(r.Context()),
			Outcome:        outcome,
			Data:           data,
			DedupWindow:    accessaudit.DefaultDedupWindow,
		})
	}
	// Fail closed: a platform admin without a valid, explicit support reason
	// and case reference (never accepted from the URL — only the
	// X-Fidibeki-Support-Reason/X-Fidibeki-Support-Case headers) never reaches
	// the session lookup below, and the rejection itself is still audited as a
	// denied support-access attempt.
	if requestErr != nil {
		if err := recordAccess(accessaudit.OutcomeDenied, map[string]any{"denial_reason": "support_request_invalid"}); err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeError(w, r, err)
			return
		}
		committed = true
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "support reason and case reference are required", nil)
		return
	}
	supportData := map[string]any{
		"support_reason": supportRequest.Reason, "support_case_reference": supportRequest.CaseReference,
	}
	session, err := h.options.Store.GetSession(r.Context(), tx, tenantID, sessionID)
	if errors.Is(err, ErrNotFound) ||
		(err == nil && (session.Status() != "ready" || session.MergedAt == nil || session.StorageKey == nil)) {
		// Audit recording and the authorization decision commit atomically: if
		// persistence fails we fail closed and never disclose whether the
		// session exists.
		if err := recordAccess(accessaudit.OutcomeNotFound, supportData); err != nil {
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
	if err := recordAccess(accessaudit.OutcomeSuccess, supportData); err != nil {
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

func (h *platformSupportStreamHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "platform support replay stream request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
