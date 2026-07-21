package feedback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

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
	if !auth.HasPermission(claims.Permissions, auth.PermissionFeedbackRead) {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "permission denied", nil)
		return
	}
	object, err := h.options.Store.GetTenantAttachmentObject(
		r.Context(), tx, resolved.ID, feedbackID, attachmentID,
	)
	if errors.Is(err, ErrNotFound) {
		writeAttachmentStreamNotFound(w)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeError(w, r, err)
		return
	}
	committed = true
	if h.options.Storage == nil {
		h.writeError(w, r, errors.New("attachment storage is unavailable"))
		return
	}
	reader, err := h.options.Storage.OpenObject(r.Context(), object.StorageKey)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", object.ContentType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "inline; filename=\"feedback-"+feedbackID+"-"+string(object.Kind)+"\"")
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
		h.logger.WarnContext(r.Context(), "stream feedback attachment", "error", err, "attachment_id", object.ID)
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
	if resolved.Status != tenant.StatusActive &&
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
