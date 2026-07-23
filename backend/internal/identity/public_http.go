package identity

import (
	"errors"
	"net/http"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func RegisterPublicRoutes(router chi.Router, options HTTPOptions) {
	handler := &HTTPHandler{options: options}
	router.Get(
		"/v1/public/workspace-logo/{tenantID}/{version}/{size}.png",
		handler.getPublicLogo,
	)
}

func (handler *HTTPHandler) getPublicLogo(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "tenantID")
	version := chi.URLParam(r, "version")
	size, validSize := parseImageSize(chi.URLParam(r, "size"))
	if _, err := uuid.Parse(tenantID); err != nil || !validSize {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "logo not found", nil)
		return
	}
	current, err := handler.options.Store.CurrentLogoVersion(r.Context(), tenantID)
	if errors.Is(err, ErrAssetNotFound) || current != version {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "logo not found", nil)
		return
	}
	if err != nil {
		handler.internalError(w, r, "get public workspace logo", err)
		return
	}
	key, err := ObjectKey(tenantID, ImageKindLogo, nil, version, size)
	if err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "logo not found", nil)
		return
	}
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	handler.streamPNG(w, r, key, "public, max-age=31536000, immutable")
}
