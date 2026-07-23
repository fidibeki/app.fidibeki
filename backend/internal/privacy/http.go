package privacy

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
}

type DownloadStorage interface {
	PresignGet(context.Context, string, string, time.Duration) (string, error)
}

type HTTPOptions struct {
	Store   HTTPStore
	JWT     *auth.JWTManager
	Storage DownloadStorage
	Logger  *slog.Logger
	Now     func() time.Time
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
	read := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
		handler.requireHuman,
		appmiddleware.RequirePermission(auth.PermissionTenantUpdate),
	}
	router.With(read...).Get("/v1/privacy/exports", handler.listExports)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/exports", handler.createExport)
	router.With(append(read, appmiddleware.RequireWritable)...).
		Post("/v1/privacy/exports/{jobID}/retry", handler.retryExport)
	router.With(read...).Post("/v1/privacy/exports/{jobID}/download", handler.downloadExport)
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
) (resolvedTenant struct{ ID, Slug string }, query db.Querier, claims auth.Claims, ok bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tenantOK || !queryOK || !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return resolvedTenant, nil, auth.Claims{}, false
	}
	resolvedTenant.ID = resolved.ID
	resolvedTenant.Slug = resolved.Slug
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

func (handler *HTTPHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "privacy export not found", nil)
	case errors.Is(err, ErrJobUnavailable):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "privacy export is unavailable", nil)
	case errors.Is(err, ErrHumanRequired):
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "active human member required", nil)
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
