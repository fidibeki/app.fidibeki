package privacy

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
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PlatformSupportTokenVerifier verifies a platform-admin bearer token. It is
// satisfied by *auth.JWTManager.
type PlatformSupportTokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}

// PlatformSupportExportStore is the read dependency the platform-admin/support
// export path needs. It is satisfied by Store.
type PlatformSupportExportStore interface {
	GetJob(context.Context, db.Querier, string, string) (Job, error)
}

// PlatformSupportExportStorage opens a tenant's completed workspace-export
// archive for streaming. Satisfied by the same object-storage client used by
// the tenant-facing download grant (HTTPOptions.Storage), but through
// OpenObject rather than PresignGet: the platform-admin/support path proxies
// the archive itself instead of minting a signed URL a platform admin could
// forward or retain outside the audited path.
type PlatformSupportExportStorage interface {
	OpenObject(context.Context, string) (io.ReadCloser, error)
}

// PlatformSupportExportHTTPOptions configures the platform-admin/support
// access path to a tenant's completed workspace-export archive. It reuses
// the same store and object storage as the tenant-facing download grant
// (http.go) but requires platform-admin authentication
// (auth.PermissionPlatformTenantSupportAccess) plus an explicit, validated
// support reason and case reference, and records access as
// actor_type=platform_admin so support access stays separately identifiable
// from ordinary tenant-member downloads.
type PlatformSupportExportHTTPOptions struct {
	Begin   db.BeginFunc
	Store   PlatformSupportExportStore
	JWT     PlatformSupportTokenVerifier
	Storage PlatformSupportExportStorage
	Logger  *slog.Logger
	Now     func() time.Time
}

type platformSupportExportHandler struct {
	options PlatformSupportExportHTTPOptions
	logger  *slog.Logger
}

// RegisterPlatformSupportRoutes mounts the platform-admin/support privacy-export
// access path on the platform-host router (see backend/cmd/fidibeki-api).
func RegisterPlatformSupportRoutes(router chi.Router, options PlatformSupportExportHTTPOptions) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &platformSupportExportHandler{options: options, logger: logger}
	router.With(
		appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantSupportAccess),
	).Get(
		"/admin/tenants/{tenantID}/privacy-exports/{jobID}/support-access",
		handler.serveHTTP,
	)
}

func (h *platformSupportExportHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	jobID := chi.URLParam(r, "jobID")
	if _, err := uuid.Parse(tenantID); err != nil {
		writePlatformSupportExportNotFound(w)
		return
	}
	if _, err := uuid.Parse(jobID); err != nil {
		writePlatformSupportExportNotFound(w)
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
		h.writeError(w, r, fmt.Errorf("set support export tenant context: %w", err))
		return
	}
	recordAccess := func(outcome string, data map[string]any) error {
		return accessaudit.Record(r.Context(), tx, accessaudit.RecordInput{
			TenantID: tenantID, Kind: "privacy_export.support_accessed",
			Actor:          accessaudit.PlatformAdminActor(claims.Subject),
			ResourceType:   accessaudit.ResourcePrivacyExport,
			ResourceID:     jobID,
			Action:         "download",
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
	// the job lookup below, and the rejection itself is still audited as a
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
	job, err := h.options.Store.GetJob(r.Context(), tx, tenantID, jobID)
	now := h.now()
	if errors.Is(err, ErrNotFound) ||
		(err == nil && (job.Kind != JobWorkspaceExport || job.Status != JobCompleted ||
			job.ObjectKey == nil || job.ExpiresAt == nil || !job.ExpiresAt.After(now))) {
		// Audit recording and the authorization decision commit atomically: if
		// persistence fails we fail closed and never disclose whether the
		// export exists. A not-yet-completed, expired, or otherwise
		// unavailable export folds into the same not-found response as a
		// missing job so platform support access never discloses more about
		// a tenant's export lifecycle than "not currently downloadable".
		if err := recordAccess(accessaudit.OutcomeNotFound, supportData); err != nil {
			h.writeError(w, r, err)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			h.writeError(w, r, err)
			return
		}
		committed = true
		writePlatformSupportExportNotFound(w)
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if h.options.Storage == nil {
		h.writeError(w, r, errors.New("privacy export storage is unavailable"))
		return
	}
	object, err := h.options.Storage.OpenObject(r.Context(), *job.ObjectKey)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	streamOwnsObject := false
	defer func() {
		if !streamOwnsObject {
			closePrivacyExportObject(r, object, job.ID, h.logger)
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
	writePlatformSupportExportContent(w, r, object, job.ID, h.logger)
}

// writePlatformSupportExportContent streams an already-opened, authorized
// workspace-export archive. It never exposes the underlying object key or a
// signed URL, unlike the tenant-facing download grant (http.go).
func writePlatformSupportExportContent(
	w http.ResponseWriter,
	r *http.Request,
	object io.ReadCloser,
	jobID string,
	logger *slog.Logger,
) {
	defer closePrivacyExportObject(r, object, jobID, logger)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "attachment; filename=\"fidibeki-workspace-export.zip\"")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, object); err != nil {
		logger.WarnContext(r.Context(), "stream support export object", "error", err, "job_id", jobID)
	}
}

func closePrivacyExportObject(r *http.Request, object io.Closer, jobID string, logger *slog.Logger) {
	if err := object.Close(); err != nil {
		logger.WarnContext(r.Context(), "close support export object", "error", err, "job_id", jobID)
	}
}

func writePlatformSupportExportNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "privacy export not found", nil)
}

func (h *platformSupportExportHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *platformSupportExportHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "platform support export request failed", "error", err)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}
