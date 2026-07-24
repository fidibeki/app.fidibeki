package feedback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AttachmentTokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

type AttachmentStreamStorage interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
}

type AttachmentObjectStore interface {
	GetTenantAttachmentObject(context.Context, db.Querier, string, string, string) (AttachmentObject, error)
}

type AttachmentStreamHTTPOptions struct {
	BaseDomain string
	Resolver   tenant.Resolver
	Begin      db.BeginFunc
	Store      AttachmentObjectStore
	JWT        AttachmentTokenVerifier
	Storage    AttachmentStreamStorage
	Logger     *slog.Logger
	Now        func() time.Time
}

type AttachmentStreamHTTPHandler struct {
	options AttachmentStreamHTTPOptions
	logger  *slog.Logger
}

func NewAttachmentStreamHTTPHandler(options AttachmentStreamHTTPOptions) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &AttachmentStreamHTTPHandler{options: options, logger: logger}
}

func (h *AttachmentStreamHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		api.WriteError(w, http.StatusMethodNotAllowed, api.CodeBadRequest, "method not allowed", nil)
		return
	}
	feedbackID := chi.URLParam(r, "feedbackID")
	attachmentID := chi.URLParam(r, "attachmentID")
	if _, err := uuid.Parse(feedbackID); err != nil {
		writeAttachmentStreamNotFound(w)
		return
	}
	if _, err := uuid.Parse(attachmentID); err != nil {
		writeAttachmentStreamNotFound(w)
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
		h.writeError(w, r, fmt.Errorf("set attachment stream tenant context: %w", err))
		return
	}
	claims, err := appmiddleware.AuthenticateTenantCredential(
		r.Context(), tx, h.options.JWT, resolved.ID, r.Header.Get("Authorization"),
		resolved.DeletionRequestedAt == nil,
	)
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
	mode := "preview"
	if r.URL.Query().Get("mode") == "download" {
		mode = "download"
	}
	recordAccess := func(outcome string, data map[string]any) error {
		if r.Method == http.MethodHead {
			// HEAD requests are protocol probes, not evidence views; the
			// access-audit contract explicitly excludes them.
			return nil
		}
		return accessaudit.Record(r.Context(), tx, accessaudit.RecordInput{
			TenantID: resolved.ID, Kind: "feedback_attachment.accessed",
			Actor:          accessaudit.ActorFromClaims(claims),
			ResourceType:   accessaudit.ResourceFeedbackAttachment,
			ResourceID:     attachmentID,
			Action:         mode,
			PermissionPath: string(auth.PermissionFeedbackRead),
			RequestID:      appmiddleware.RequestIDFromContext(r.Context()),
			Outcome:        outcome,
			Data:           data,
			DedupWindow:    accessaudit.DefaultDedupWindow,
		})
	}
	if !auth.HasPermission(claims.Permissions, auth.PermissionFeedbackRead) {
		// Audit recording and the authorization decision commit atomically:
		// if persistence fails we fail closed and never disclose whether the
		// attachment exists.
		if err := recordAccess(accessaudit.OutcomeDenied, nil); err != nil {
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
	object, err := h.options.Store.GetTenantAttachmentObject(
		r.Context(), tx, resolved.ID, feedbackID, attachmentID,
	)
	if errors.Is(err, ErrNotFound) {
		if err := recordAccess(accessaudit.OutcomeNotFound, nil); err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeError(w, r, err)
			return
		}
		committed = true
		writeAttachmentStreamNotFound(w)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.options.Storage == nil {
		h.writeError(w, r, errors.New("attachment storage is unavailable"))
		return
	}
	reader, err := h.options.Storage.OpenObject(r.Context(), object.StorageKey)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	streamOwnsObject := false
	defer func() {
		if !streamOwnsObject {
			closeAttachmentObject(r, reader, object.ID, h.logger)
		}
	}()
	if err := recordAccess(accessaudit.OutcomeSuccess, map[string]any{"content_type": object.ContentType}); err != nil {
		h.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeError(w, r, err)
		return
	}
	committed = true
	streamOwnsObject = true
	writeAttachmentStreamContent(w, r, reader, object, feedbackID, mode, h.logger)
}

// writeAttachmentStreamContent streams an already-opened, authorized
// attachment object. Shared by the tenant-facing stream above and the
// platform-admin/support stream (platform_support_attachment.go) so the
// disposition, caching, and range-request handling stay identical regardless
// of which access path authorized the read.
func writeAttachmentStreamContent(
	w http.ResponseWriter,
	r *http.Request,
	reader io.ReadCloser,
	object AttachmentObject,
	feedbackID string,
	mode string,
	logger *slog.Logger,
) {
	defer closeAttachmentObject(r, reader, object.ID, logger)

	disposition := "inline"
	if mode == "download" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", object.ContentType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", disposition+"; filename=\"feedback-"+feedbackID+"-"+string(object.Kind)+"\"")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if seeker, seekable := reader.(io.ReadSeeker); seekable {
		http.ServeContent(w, r, "feedback-"+feedbackID+"-"+string(object.Kind), object.CreatedAt, seeker)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", object.SizeBytes))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		logger.WarnContext(r.Context(), "stream feedback attachment", "error", err, "attachment_id", object.ID)
	}
}

func closeAttachmentObject(r *http.Request, reader io.Closer, attachmentID string, logger *slog.Logger) {
	if err := reader.Close(); err != nil {
		logger.WarnContext(r.Context(), "close feedback attachment stream", "error", err, "attachment_id", attachmentID)
	}
}

func (h *AttachmentStreamHTTPHandler) resolveTenant(w http.ResponseWriter, r *http.Request) (tenant.Tenant, bool) {
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

func (h *AttachmentStreamHTTPHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "attachment stream request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func writeAttachmentStreamNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "attachment not found", nil)
}
