package testimonial

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
)

// mediaRedirectTTL bounds how long a resolved asset URL stays usable.
//
// Short, because the redirect target is a bare storage URL with no further
// checks on it: whoever holds it holds the file until it expires. The path that
// produced it is re-checked on every request, which is the point of not putting
// a presigned URL in the snapshot.
const mediaRedirectTTL = 5 * time.Minute

// mediaCacheSeconds is how long a browser may keep the redirect.
//
// It is deliberately shorter than the wall's own cache window so a withdrawal
// takes the images down on roughly the same timetable as the words. Content
// addressing means the URL changes whenever the bytes do, so nothing is gained
// by caching the redirect itself for longer.
const mediaCacheSeconds = 60

// MediaStore is the read surface the public media endpoint needs.
type MediaStore interface {
	ResolveMediaTenant(context.Context, db.Querier, string) (string, error)
	LoadPublishableMedia(context.Context, db.Querier, string, string, string, string) (PublicMedia, error)
}

// MediaSigner issues the short-lived URL the redirect points at.
type MediaSigner interface {
	PresignInline(context.Context, string, string, time.Duration) (string, error)
}

// PublicMedia is a resolved asset, cleared for public serving.
type PublicMedia struct {
	StorageKey  string
	ContentType string
}

// MediaHTTPOptions wires the public media endpoint.
type MediaHTTPOptions struct {
	Store MediaStore
	// Resolver runs on the admin pool: the public id has to be traced to a
	// workspace before a tenant context can exist.
	Resolver db.Querier
	Begin    db.BeginFunc
	Signer   MediaSigner
	Logger   *slog.Logger
}

// MediaHTTPHandler serves the assets a published wall refers to.
type MediaHTTPHandler struct {
	options MediaHTTPOptions
}

// RegisterMediaRoutes mounts the endpoint the snapshot's media paths point at.
//
// Without it every avatar and video in a published wall resolves to nothing,
// which is what the payload was already promising the customer's website.
func RegisterMediaRoutes(router chi.Router, options MediaHTTPOptions) {
	handler := &MediaHTTPHandler{options: options}
	router.Get("/v1/public/media/{mediaPublicID}/{asset}", handler.serve)
	router.Options("/v1/public/media/{mediaPublicID}/{asset}", handler.preflight)
}

func (h *MediaHTTPHandler) preflight(w http.ResponseWriter, _ *http.Request) {
	writeEmbedCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

// serve resolves a content-addressed path to a short-lived storage URL.
//
// The check runs per request rather than once at publication: consent can be
// withdrawn between the snapshot being built and this image being fetched, and
// an asset that outlived its consent is exactly what must not still load.
func (h *MediaHTTPHandler) serve(w http.ResponseWriter, r *http.Request) {
	writeEmbedCORS(w)
	publicID := chi.URLParam(r, "mediaPublicID")
	contentHash, extension, ok := splitAsset(chi.URLParam(r, "asset"))
	if !ok {
		h.writeGone(w)
		return
	}

	tenantID, err := h.options.Store.ResolveMediaTenant(r.Context(), h.options.Resolver, publicID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.writeGone(w)
		} else {
			h.writeUnavailable(w, r, err)
		}
		return
	}

	var media PublicMedia
	if err := h.withTenant(r.Context(), tenantID, func(query db.Querier) error {
		loaded, loadErr := h.options.Store.LoadPublishableMedia(
			r.Context(), query, tenantID, publicID, contentHash, extension)
		media = loaded
		return loadErr
	}); err != nil {
		if errors.Is(err, ErrNotFound) {
			h.writeGone(w)
		} else {
			h.writeUnavailable(w, r, err)
		}
		return
	}

	url, err := h.options.Signer.PresignInline(
		r.Context(), media.StorageKey, media.ContentType, mediaRedirectTTL)
	if err != nil {
		h.writeUnavailable(w, r, err)
		return
	}
	w.Header().Set("Cache-Control",
		fmt.Sprintf("public, max-age=%d", mediaCacheSeconds))
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, url, http.StatusFound)
}

// splitAsset separates the content hash from the extension in "<hash>.<ext>".
//
// The hash is part of the path rather than a query parameter so the whole URL
// is content-addressed: a changed file is a changed URL, and no cache anywhere
// has to be told about it.
func splitAsset(asset string) (string, string, bool) {
	dot := strings.LastIndex(asset, ".")
	if dot <= 0 || dot == len(asset)-1 {
		return "", "", false
	}
	hash, extension := asset[:dot], strings.ToLower(asset[dot+1:])
	if len(hash) != 64 {
		return "", "", false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", "", false
		}
	}
	return hash, extension, true
}

func (h *MediaHTTPHandler) withTenant(
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
		return errors.New("configure public media tenant context")
	}
	return work(tx)
}

// writeGone answers every refusal identically, whether the asset never existed,
// was withdrawn, or is still being scanned. Which of those it is would tell an
// anonymous caller about someone else's consent decisions.
func (h *MediaHTTPHandler) writeGone(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "not found", nil)
}

func (h *MediaHTTPHandler) writeUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	if h.options.Logger != nil {
		h.options.Logger.ErrorContext(r.Context(), "public testimonial media failed", "error", err)
	}
	w.Header().Set("Cache-Control", "no-store")
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable,
		"service unavailable", nil)
}
