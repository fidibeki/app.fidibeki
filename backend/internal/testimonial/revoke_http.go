package testimonial

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
)

// RevokeStore is the surface the withdrawal page needs.
type RevokeStore interface {
	ResolveRevocationTenant(context.Context, db.Querier, string) (string, string, error)
	GetTestimonial(context.Context, db.Querier, string, string) (Testimonial, error)
	Revoke(context.Context, db.Querier, string, string, string, time.Time) error
}

// RevokeHTTPOptions wires the author-facing withdrawal endpoints.
type RevokeHTTPOptions struct {
	Store RevokeStore
	// Resolver runs on the admin pool: the token has to be traced to a workspace
	// before a tenant context can exist.
	Resolver db.Querier
	Begin    db.BeginFunc
	Logger   *slog.Logger
	Now      func() time.Time
}

// RevokeHTTPHandler serves the withdrawal link every author is given at the
// moment they give permission.
type RevokeHTTPHandler struct {
	options RevokeHTTPOptions
}

// RegisterRevokeRoutes mounts the withdrawal endpoints.
//
// The consent notice promises this exists. Handing someone a link that answers
// nothing would make that notice a lie, and the way out is the one promise a
// consent record has to keep.
func RegisterRevokeRoutes(router chi.Router, options RevokeHTTPOptions) {
	handler := &RevokeHTTPHandler{options: options}
	router.Get("/v1/public/testimonial-revocations/{token}", handler.load)
	router.Post("/v1/public/testimonial-revocations/{token}", handler.revoke)
}

func (h *RevokeHTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now().UTC()
}

// RevokeView is what the author sees before deciding.
//
// It carries their own words and nothing else. Which workspace holds the
// testimonial, who requested it, when it was published — none of that is owed
// to whoever opened the link, and the link is the only credential here.
type RevokeView struct {
	Quote   string `json:"quote"`
	Revoked bool   `json:"revoked"`
}

func (h *RevokeHTTPHandler) load(w http.ResponseWriter, r *http.Request) {
	_, item, ok := h.resolve(w, r)
	if !ok {
		return
	}
	noStore(w)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"testimonial": RevokeView{
			Quote:   item.Quote,
			Revoked: item.ConsentState == ConsentRevoked,
		},
	})
}

// revoke withdraws consent.
//
// It is deliberately idempotent: someone who clicks twice, or returns to an old
// email months later, should be reassured that it is already withdrawn rather
// than told their withdrawal failed.
func (h *RevokeHTTPHandler) revoke(w http.ResponseWriter, r *http.Request) {
	var submission struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &submission) {
		return
	}
	tenantID, item, ok := h.resolve(w, r)
	if !ok {
		return
	}
	if item.ConsentState == ConsentRevoked {
		noStore(w)
		api.WriteJSON(w, http.StatusOK, map[string]any{"revoked": true})
		return
	}
	// Truncated rather than refused. Someone explaining why they want their words
	// taken down should not be stopped by a length limit.
	reason := strings.TrimSpace(submission.Reason)
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		return h.options.Store.Revoke(r.Context(), query, tenantID, item.ID, reason, h.now())
	}); err != nil {
		h.writeFailure(w, r, err)
		return
	}
	noStore(w)
	api.WriteJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

// resolve turns the token into the testimonial behind it, answering every
// failure identically for the same reason the collection endpoints do.
func (h *RevokeHTTPHandler) resolve(w http.ResponseWriter, r *http.Request) (string, Testimonial, bool) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		h.writeUnusable(w)
		return "", Testimonial{}, false
	}
	tenantID, id, err := h.options.Store.ResolveRevocationTenant(
		r.Context(), h.options.Resolver, HashToken(token))
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			h.writeUnusable(w)
		} else {
			h.writeFailure(w, r, err)
		}
		return "", Testimonial{}, false
	}

	var item Testimonial
	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		loaded, loadErr := h.options.Store.GetTestimonial(r.Context(), query, tenantID, id)
		item = loaded
		return loadErr
	}); err != nil {
		if errors.Is(err, ErrNotFound) {
			h.writeUnusable(w)
		} else {
			h.writeFailure(w, r, err)
		}
		return "", Testimonial{}, false
	}
	return tenantID, item, true
}

// withTenant runs work inside a transaction that pins app.tenant_id, so an
// unauthenticated request still reads and writes under row-level security.
func (h *RevokeHTTPHandler) withTenant(
	ctx context.Context,
	tenantID string,
	work func(db.Querier) error,
) error {
	tx, err := h.options.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var configured string
	if err := tx.QueryRow(ctx, `SELECT set_config('app.tenant_id', $1, true)`,
		tenantID).Scan(&configured); err != nil || configured != tenantID {
		return errors.New("configure revocation tenant context")
	}
	if err := work(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (h *RevokeHTTPHandler) writeUnusable(w http.ResponseWriter) {
	noStore(w)
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "this link is no longer usable", nil)
}

func (h *RevokeHTTPHandler) writeFailure(w http.ResponseWriter, r *http.Request, err error) {
	if h.options.Logger != nil {
		h.options.Logger.ErrorContext(r.Context(), "testimonial revocation failed", "error", err)
	}
	noStore(w)
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
		"service unavailable", nil)
}
