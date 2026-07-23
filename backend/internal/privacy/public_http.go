package privacy

import (
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/go-chi/chi/v5"
)

type PublicHTTPOptions struct {
	Store             Store
	Begin             db.BeginFunc
	Storage           DownloadStorage
	Limiter           ratelimit.Limiter
	TrustedProxyCIDRs []netip.Prefix
	Now               func() time.Time
}

type PublicHTTPHandler struct {
	options PublicHTTPOptions
}

func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	handler := &PublicHTTPHandler{options: options}
	router.Post("/v1/privacy/requests/{token}/verify", handler.verifyReporterRequest)
	router.Get("/v1/privacy/requests/{token}", handler.reporterRequestStatus)
	router.Post("/v1/privacy/requests/{token}/download", handler.reporterRequestDownload)
}

func (handler *PublicHTTPHandler) reporterRequestDownload(w http.ResponseWriter, r *http.Request) {
	tokenHash, ok := handler.reporterToken(w, r)
	if !ok {
		return
	}
	if handler.options.Storage == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	now := handler.now()
	download, err := handler.options.Store.LoadReporterDownload(r.Context(), tokenHash, now)
	if errors.Is(err, ErrNotFound) {
		writeReporterPrivacyUnavailable(w)
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	filename := "fidibeki-reporter-data-" + now.Format("2006-01-02") + ".zip"
	url, err := handler.options.Storage.PresignGet(
		r.Context(), download.ObjectKey, filename, downloadLinkTTL,
	)
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	expiresAt := now.Add(downloadLinkTTL)
	if err := handler.options.Store.RecordReporterDownload(r.Context(), download, expiresAt); err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	api.WriteJSON(w, http.StatusOK, map[string]any{"url": url, "expires_at": expiresAt})
}

func (handler *PublicHTTPHandler) verifyReporterRequest(w http.ResponseWriter, r *http.Request) {
	tokenHash, ok := handler.reporterToken(w, r)
	if !ok {
		return
	}
	if handler.options.Begin == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	tx, err := handler.options.Begin(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	view, err := handler.options.Store.VerifyReporterRequest(
		r.Context(), tx, tokenHash, handler.now(),
	)
	if errors.Is(err, ErrNotFound) {
		writeReporterPrivacyUnavailable(w)
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	committed = true
	writeReporterPrivacyView(w, view)
}

func (handler *PublicHTTPHandler) reporterRequestStatus(w http.ResponseWriter, r *http.Request) {
	tokenHash, ok := handler.reporterToken(w, r)
	if !ok {
		return
	}
	view, err := handler.options.Store.GetReporterRequestView(r.Context(), tokenHash, handler.now())
	if errors.Is(err, ErrNotFound) {
		writeReporterPrivacyUnavailable(w)
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	writeReporterPrivacyView(w, view)
}

func (handler *PublicHTTPHandler) reporterToken(
	w http.ResponseWriter,
	r *http.Request,
) ([32]byte, bool) {
	tokenHash, ok := parseReporterRequestToken(chi.URLParam(r, "token"))
	if !ok {
		writeReporterPrivacyUnavailable(w)
		return [32]byte{}, false
	}
	if !handler.allowReporterRequest(w, r, hex.EncodeToString(tokenHash[:8])) {
		return [32]byte{}, false
	}
	return tokenHash, true
}

func (handler *PublicHTTPHandler) allowReporterRequest(
	w http.ResponseWriter,
	r *http.Request,
	tokenKey string,
) bool {
	if handler.options.Limiter == nil {
		return true
	}
	windows := []struct {
		key    string
		window ratelimit.Window
	}{
		{key: "privacy-request:ip:" + httpx.ClientIPFromRequest(r, handler.options.TrustedProxyCIDRs), window: ratelimit.Window{Limit: 20, Period: time.Minute}},
		{key: "privacy-request:token:" + tokenKey, window: ratelimit.Window{Limit: 15, Period: time.Minute}},
	}
	for _, item := range windows {
		decision, err := handler.options.Limiter.Allow(r.Context(), item.key, item.window)
		if err != nil {
			api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
			return false
		}
		if !decision.Allowed {
			retrySeconds := int((decision.RetryAfter + time.Second - 1) / time.Second)
			if retrySeconds < 1 {
				retrySeconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded, "rate limit exceeded", nil)
			return false
		}
	}
	return true
}

func (handler *PublicHTTPHandler) now() time.Time {
	if handler.options.Now != nil {
		return handler.options.Now().UTC()
	}
	return time.Now().UTC()
}

func writeReporterPrivacyView(w http.ResponseWriter, view ReporterRequestView) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	api.WriteJSON(w, http.StatusOK, map[string]any{"request": view})
}

func writeReporterPrivacyUnavailable(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "data request is invalid or unavailable", nil)
}
