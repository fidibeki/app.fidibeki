package issue

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/httpx"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/go-chi/chi/v5"
)

func (h *PublicHTTPHandler) reporterProgress(w http.ResponseWriter, r *http.Request) {
	raw, hash, ok := h.reporterToken(w, r)
	if !ok {
		return
	}
	_ = raw
	progress, err := h.options.Store.LoadReporterProgress(r.Context(), hash, time.Now().UTC())
	if errors.Is(err, ErrNotFound) {
		writeReporterAccessError(w)
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	api.WriteJSON(w, http.StatusOK, map[string]any{"progress": progress})
}

func (h *PublicHTTPHandler) reporterResponse(w http.ResponseWriter, r *http.Request) {
	_, hash, ok := h.reporterToken(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var request struct {
		Response string `json:"response"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body must contain one JSON value", nil)
		return
	}
	response := strings.TrimSpace(request.Response)
	if length := len([]rune(response)); length < 1 || length > 5000 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"request validation failed", map[string]any{
				"fields": map[string]string{"response": "must be between 1 and 5000 characters"},
			})
		return
	}
	if h.options.Begin == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	tx, err := h.options.Begin(r.Context())
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
	if err := h.options.Store.SubmitReporterResponse(
		r.Context(), tx, hash, response, time.Now().UTC(),
	); errors.Is(err, ErrNotFound) {
		writeReporterAccessError(w)
		return
	} else if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	committed = true
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (h *PublicHTTPHandler) reporterToken(
	w http.ResponseWriter,
	r *http.Request,
) (string, [32]byte, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, "token"))
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		writeReporterAccessError(w)
		return "", [32]byte{}, false
	}
	hash := reporterTokenHash(raw)
	if !h.allowReporterAccess(w, r, hex.EncodeToString(hash[:8])) {
		return "", [32]byte{}, false
	}
	return raw, hash, true
}

func (h *PublicHTTPHandler) allowReporterAccess(
	w http.ResponseWriter,
	r *http.Request,
	tokenKey string,
) bool {
	if h.options.Limiter == nil {
		return true
	}
	windows := []struct {
		key    string
		window ratelimit.Window
	}{
		{key: "reporter:ip:" + httpx.ClientIPFromRequest(r, h.options.TrustedProxyCIDRs), window: ratelimit.Window{Limit: 30, Period: time.Minute}},
		{key: "reporter:token:" + tokenKey, window: ratelimit.Window{Limit: 20, Period: time.Minute}},
	}
	for _, item := range windows {
		decision, err := h.options.Limiter.Allow(r.Context(), item.key, item.window)
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

func writeReporterAccessError(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "progress link is invalid or unavailable", nil)
}
