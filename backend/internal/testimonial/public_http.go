package testimonial

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// snapshotMaxAge and snapshotStaleWhileRevalidate bound how long a revocation
// can remain visible to a viewer who already loaded the page.
//
// The honest arithmetic: an edge may serve a stale payload for max-age plus
// stale-while-revalidate, so the worst case is 360 seconds, not 60. That number
// goes into the consent notice the author signs, because promising instant
// erasure from other people's caches would be a discoverable lie.
const (
	snapshotMaxAge               = 60
	snapshotStaleWhileRevalidate = 300
)

// PublicStore is the read surface the public endpoint needs.
type PublicStore interface {
	LoadCurrentSnapshot(context.Context, db.Querier, string, string) (Snapshot, error)
	RevokedCardIDs(context.Context, db.Querier, string, string) (map[string]struct{}, error)
}

// PublicHTTPOptions wires the public endpoint.
type PublicHTTPOptions struct {
	Store      PublicStore
	Authorizer PublicAuthorizer
	// Begin opens a transaction on the app pool, where row-level security
	// applies. The authorizer runs on the admin pool before this.
	Begin  func(context.Context) (pgx.Tx, error)
	Logger *slog.Logger
	Now    func() time.Time
}

// PublicHTTPHandler serves published walls to anonymous callers.
type PublicHTTPHandler struct {
	options PublicHTTPOptions
}

// RegisterPublicRoutes mounts the endpoints a customer's website calls.
func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	handler := &PublicHTTPHandler{options: options}
	router.Get("/v1/public/walls/{wallPublicID}", handler.wall)
	router.Options("/v1/public/walls/{wallPublicID}", handler.preflight)
}

func (h *PublicHTTPHandler) preflight(w http.ResponseWriter, _ *http.Request) {
	writeEmbedCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

// wall serves the published snapshot for a wall.
//
// The shape of this handler is the whole security design in miniature: resolve
// the workspace on the admin pool, assert it may publish, then open a
// transaction and set app.tenant_id so every subsequent read still runs under
// row-level security. "Unauthenticated" never means "unprotected".
func (h *PublicHTTPHandler) wall(w http.ResponseWriter, r *http.Request) {
	writeEmbedCORS(w)
	publicID := chi.URLParam(r, "wallPublicID")

	resolved, err := h.options.Authorizer.Authorize(r.Context(), publicID)
	if err != nil {
		h.writeUnavailable(w, r, err)
		return
	}
	if !OriginAllowed(r.Header.Get("Origin"), resolved.AllowedOrigins) {
		h.writeUnavailable(w, r, ErrPublicAccessDenied)
		return
	}

	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeUnavailable(w, r, fmt.Errorf("begin public wall transaction: %w", err))
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var configured string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`,
		resolved.TenantID).Scan(&configured); err != nil || configured != resolved.TenantID {
		h.writeUnavailable(w, r, errors.New("configure public wall tenant context"))
		return
	}

	snapshot, err := h.options.Store.LoadCurrentSnapshot(r.Context(), tx, resolved.TenantID, resolved.WallID)
	if err != nil {
		h.writeUnavailable(w, r, err)
		return
	}

	payload := snapshot.Payload
	etag := snapshot.ETag
	stale := snapshot.IsStale
	if stale {
		// A revocation landed and the rebuild has not run yet. Filter the stored
		// payload through a short indexed lookup rather than re-projecting the
		// whole wall live: correct, bounded, and roughly the cost of the normal
		// path even when a popular wall is being hammered.
		revoked, err := h.options.Store.RevokedCardIDs(r.Context(), tx, resolved.TenantID, resolved.WallID)
		if err != nil {
			h.writeUnavailable(w, r, err)
			return
		}
		payload = filterRevoked(payload, revoked)
		etag = ""
	}

	if etag != "" {
		w.Header().Set("ETag", `"`+etag+`"`)
		if match := r.Header.Get("If-None-Match"); match == `"`+etag+`"` {
			w.Header().Set("Cache-Control", fmt.Sprintf(
				"public, max-age=%d, stale-while-revalidate=%d",
				snapshotMaxAge, snapshotStaleWhileRevalidate))
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Cache-Control", fmt.Sprintf(
			"public, max-age=%d, stale-while-revalidate=%d",
			snapshotMaxAge, snapshotStaleWhileRevalidate))
	} else {
		// Never let a filtered, mid-revocation payload be cached anywhere.
		w.Header().Set("Cache-Control", "no-store")
	}

	api.WriteJSON(w, http.StatusOK, payload)
}

// filterRevoked removes cards whose consent no longer holds.
func filterRevoked(payload SnapshotPayload, revoked map[string]struct{}) SnapshotPayload {
	if len(revoked) == 0 {
		return payload
	}
	kept := make([]SnapshotCard, 0, len(payload.Cards))
	for _, card := range payload.Cards {
		if _, gone := revoked[card.ID]; gone {
			continue
		}
		kept = append(kept, card)
	}
	payload.Cards = kept
	return payload
}

// writeEmbedCORS opens the endpoint to any site, which is what an embed needs.
// The data is public by construction; the allowlist above is the only origin
// signal, and it is advisory.
func writeEmbedCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "If-None-Match")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Vary", "Origin")
}

// writeUnavailable answers every failure identically.
func (h *PublicHTTPHandler) writeUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil && !errors.Is(err, ErrPublicAccessDenied) && !errors.Is(err, ErrNotFound) &&
		h.options.Logger != nil {
		h.options.Logger.ErrorContext(r.Context(), "public testimonial wall failed",
			slog.String("error", err.Error()))
	}
	w.Header().Set("Cache-Control", "no-store")
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "testimonial wall is not available", nil)
}
