package membernotify

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// UnsubscribePath is the public one-click opt-out endpoint carried in every
// optional notification footer.
const UnsubscribePath = "/v1/member-notifications/unsubscribe"

type HTTPOptions struct {
	Store  Store
	JWT    *auth.JWTManager
	Logger *slog.Logger
	Now    func() time.Time
}

type HTTPHandler struct {
	options HTTPOptions
}

// RegisterRoutes mounts the self-scoped preference endpoints. There is no
// permission gate beyond being an authenticated member: these settings belong to
// the member, and the routes never accept another member's ID.
func RegisterRoutes(router chi.Router, options HTTPOptions) {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{options: options}
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(protected...).Get("/v1/notification-preferences", handler.get)
	router.With(append(protected, appmiddleware.RequireWritable)...).
		Patch("/v1/notification-preferences", handler.update)
}

func (h *HTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	resolved, query, claims, ok := requestState(w, r)
	if !ok {
		return
	}
	preferences, err := h.options.Store.Get(r.Context(), query, resolved.ID, claims.Subject)
	if err != nil {
		writeError(w, err)
		return
	}
	deliveries, err := h.options.Store.RecentDeliveries(
		r.Context(), query, resolved.ID, claims.Subject, 20,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writePreferences(w, preferences, deliveries)
}

func (h *HTTPHandler) update(w http.ResponseWriter, r *http.Request) {
	var patch Patch
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(
			w, http.StatusBadRequest, api.CodeBadRequest,
			"request body must contain one JSON value", nil,
		)
		return
	}
	resolved, query, claims, ok := requestState(w, r)
	if !ok {
		return
	}
	current, err := h.options.Store.Get(r.Context(), query, resolved.ID, claims.Subject)
	if err != nil {
		writeError(w, err)
		return
	}
	next, fields := Apply(current, patch)
	timezone, setTimezone, timezoneError := TimezonePatch(patch)
	if timezoneError != "" {
		if fields == nil {
			fields = make(map[string]string)
		}
		fields["timezone"] = timezoneError
	}
	if len(fields) > 0 {
		api.WriteError(
			w, http.StatusUnprocessableEntity, api.CodeValidation, "request validation failed",
			map[string]any{"fields": fields},
		)
		return
	}
	saved, err := h.options.Store.Save(
		r.Context(), query, resolved.ID, claims.Subject, next, timezone, setTimezone, h.now(),
	)
	if err != nil {
		h.options.Logger.ErrorContext(
			r.Context(), "member notification preference update failed",
			"error", err, "tenant_id", resolved.ID, "member_id", claims.Subject,
		)
		writeError(w, err)
		return
	}
	deliveries, err := h.options.Store.RecentDeliveries(
		r.Context(), query, resolved.ID, claims.Subject, 20,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writePreferences(w, saved, deliveries)
}

// PublicHTTPOptions wires the one-click opt-out endpoint, which runs on the API
// host without a session and therefore uses the cross-tenant pool directly.
type PublicHTTPOptions struct {
	Store    Store
	Database db.Querier
	Tokens   *auth.JWTManager
	Logger   *slog.Logger
	Now      func() time.Time
}

type PublicHTTPHandler struct {
	options PublicHTTPOptions
}

// RegisterPublicRoutes mounts the signed opt-out endpoint. GET only renders the
// confirmation form so a mail scanner that follows links cannot change anyone's
// settings; POST applies the change.
func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &PublicHTTPHandler{options: options}
	router.Get(UnsubscribePath, handler.confirm)
	router.Post(UnsubscribePath, handler.unsubscribe)
}

func (h *PublicHTTPHandler) confirm(w http.ResponseWriter, r *http.Request) {
	writeUnsubscribeHTML(w, unsubscribeFormHTML(strings.TrimSpace(r.URL.Query().Get("token"))))
}

// unsubscribe applies a signed opt-out. Every outcome — valid token, forged
// token, expired token, deleted member, database error — returns the same page.
// The endpoint therefore answers no question about whether a member, workspace,
// or address exists, which is what keeps the link from being an enumeration
// oracle. Genuine failures are logged server-side instead.
func (h *PublicHTTPHandler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	defer writeUnsubscribeHTML(w, unsubscribeConfirmedHTML())
	raw := strings.TrimSpace(r.FormValue("token"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if raw == "" || h.options.Tokens == nil || h.options.Database == nil {
		return
	}
	claims, err := h.options.Tokens.Verify(raw, auth.AudienceMemberUnsubscribe)
	if err != nil || claims.Subject == "" || claims.TenantID == "" {
		return
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return
	}
	if _, err := uuid.Parse(claims.TenantID); err != nil {
		return
	}
	if err := h.options.Store.RecordUnsubscribe(
		r.Context(), h.options.Database, claims.TenantID, claims.Subject, h.now(),
	); err != nil && !errors.Is(err, ErrMemberNotFound) {
		h.options.Logger.ErrorContext(
			r.Context(), "member notification unsubscribe failed", "error", err,
		)
	}
}

func (h *PublicHTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *HTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now().UTC()
	}
	return time.Now().UTC()
}

func writePreferences(w http.ResponseWriter, preferences Preferences, deliveries []Delivery) {
	options := make(map[string][]string, len(Categories))
	for _, category := range Categories {
		options[category] = SupportedModes(category)
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"notification_preferences": preferences,
		"categories":               Categories,
		"supported_modes":          options,
		"deliveries":               deliveries,
	})
}

func requestState(
	w http.ResponseWriter,
	r *http.Request,
) (tenant.Tenant, db.Querier, auth.Claims, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !tenantOK || !queryOK || !claimsOK || claims.Subject == "" {
		api.WriteError(
			w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil,
		)
		return tenant.Tenant{}, nil, auth.Claims{}, false
	}
	return resolved, query, claims, true
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrMemberNotFound) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
		return
	}
	api.WriteError(
		w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil,
	)
}

func writeUnsubscribeHTML(w http.ResponseWriter, document string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, document)
}

func unsubscribeFormHTML(token string) string {
	return unsubscribeDocument(
		"Turn off optional notifications",
		`<p>Optional product notifications for this workspace will stop. Account, security, `+
			`and billing messages are not affected.</p>`+
			`<form method="post" action="`+UnsubscribePath+`">`+
			`<input type="hidden" name="token" value="`+escapeAttribute(token)+`">`+
			`<button type="submit">Turn off optional notifications</button></form>`,
	)
}

func unsubscribeConfirmedHTML() string {
	return unsubscribeDocument(
		"Optional notifications are off",
		`<p>If this link was still valid, optional product notifications for that workspace `+
			`are now off. Account, security, and billing messages are not affected, and you `+
			`can turn notifications back on in your workspace settings.</p>`,
	)
}

func unsubscribeDocument(heading, body string) string {
	return `<!doctype html><html lang="en"><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + heading + `</title>` +
		`<body style="font-family:Arial,Helvetica,sans-serif;margin:0;padding:32px 16px;` +
		`background:#f4f5f4;color:#161a17;">` +
		`<main style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #dfe3df;` +
		`padding:28px;"><h1 style="font-size:22px;margin:0 0 16px;">` + heading + `</h1>` +
		body + `</main></body></html>`
}

// escapeAttribute keeps a caller-supplied token inside the hidden form field
// instead of letting it break out into markup. The token is echoed, never
// interpreted, and an invalid one is indistinguishable from a valid one here.
func escapeAttribute(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;",
	)
	return replacer.Replace(value)
}
