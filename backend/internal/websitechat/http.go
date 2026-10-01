package websitechat

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

type Options struct {
	Begin      db.BeginFunc
	Resolver   tenant.Resolver
	BaseDomain string
	JWT        middleware.TenantCredentialVerifier
	Authorizer widgetapi.ProjectAuthorizer
	Redis      redis.UniversalClient
	Limiter    ratelimit.Limiter
	Logger     *slog.Logger
}
type Handler struct{ options Options }
type scope struct {
	rules                                                 []project.WidgetRule
	tx                                                    db.Transaction
	tenantID, projectID, visitorID, actorID, name, origin string
	operator, enabled, notify, allowVisitorMessages       bool
	expires                                               time.Time
}
type failure struct {
	status  int
	code    api.ErrorCode
	message string
}

func (e failure) Error() string { return e.message }
func denied() error             { return failure{403, api.CodeForbidden, "website chat access denied"} }
func invalid() error            { return failure{400, api.CodeValidation, "invalid chat request"} }
func missing() error            { return failure{404, api.CodeNotFound, "conversation not found"} }
func unauthenticated() error {
	return failure{401, api.CodeUnauthenticated, "chat session expired or invalid"}
}
func NewHandler(o Options) *Handler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Handler{options: o}
}

func (h *Handler) RegisterTenantRoutes(r chi.Router) {
	const b = "/v1/projects/{projectID}/chat"
	r.Get(b+"/events", h.events(true))
	r.Delete(b+"/visitors/{visitorID}", h.run(true, auth.PermissionChatDelete, h.remove))
	r.Get(b+"/online", h.run(true, auth.PermissionChatRead, h.online))
	r.Get(b+"/conversations", h.run(true, auth.PermissionChatRead, h.inbox))
	r.Post(b+"/visitors/{visitorID}/read", h.run(true, auth.PermissionChatRead, h.read))
	r.Get(b+"/settings", h.run(true, auth.PermissionChatRead, h.settings))
	r.Put(b+"/settings", h.run(true, auth.PermissionProjectUpdate, h.settings))
	r.Get(b+"/visitors/{visitorID}", h.run(true, auth.PermissionChatRead, h.thread))
	r.Post(b+"/visitors/{visitorID}/messages", h.run(true, auth.PermissionChatWrite, h.send))
}
func (h *Handler) RegisterPublicRoutes(r chi.Router) {
	const b = "/v1/widget/chat"
	r.Get(b+"/events", h.events(false))
	r.Options(b+"/*", h.preflight)
	r.Put(b+"/presence", h.run(false, "", h.presence))
	r.Delete(b+"/presence/{tabID}", h.run(false, "", h.leave))
	r.Post(b+"/session/revoke", h.run(false, "", h.revoke))
	r.Post(b+"/read", h.run(false, "", h.read))
	r.Post(b+"/session", h.run(false, "", h.session))
	r.Get(b+"/conversation", h.run(false, "", h.thread))
	r.Post(b+"/messages", h.run(false, "", h.send))
}
func validID(s string) bool {
	id, e := uuid.Parse(s)
	return e == nil && id != uuid.Nil && id.String() == s
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(io.LimitReader(r.Body, 20000))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return invalid()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return invalid()
	}
	return nil
}
func (h *Handler) limit(ctx context.Context, key string, n int) error {
	d, e := h.options.Limiter.Allow(ctx, "website-chat:"+key, ratelimit.Window{Limit: n, Period: time.Minute})
	if e != nil {
		return e
	}
	if !d.Allowed {
		return failure{429, api.CodeRateLimitExceeded, "too many chat requests"}
	}
	return nil
}
func (h *Handler) authorize(r *http.Request, operator bool, permission auth.Permission) (*scope, error) {
	ctx := r.Context()
	s := &scope{operator: operator}
	var rawToken string
	if operator {
		slug, err := tenant.SlugFromHost(r.Host, h.options.BaseDomain)
		if err != nil {
			return nil, denied()
		}
		t, err := h.options.Resolver.Resolve(ctx, slug)
		if errors.Is(err, tenant.ErrUnknownTenant) {
			return nil, denied()
		}
		if err != nil {
			return nil, err
		}
		if !t.IsActiveAt(time.Now()) || t.DeletionRequestedAt != nil {
			return nil, denied()
		}
		s.tenantID = t.ID
		s.projectID = chi.URLParam(r, "projectID")
		s.visitorID = chi.URLParam(r, "visitorID")
		if !validID(s.projectID) || (s.visitorID != "" && !validID(s.visitorID)) {
			return nil, missing()
		}
	} else {
		key := r.Header.Get(widgetapi.PublicKeyHeader)
		if q := r.URL.Query().Get("key"); q != "" && q != key {
			return nil, denied()
		}
		s.origin = r.Header.Get("Origin")
		c, err := h.options.Authorizer.Authorize(ctx, key, s.origin)
		if errors.Is(err, widgetapi.ErrAccessDenied) {
			return nil, denied()
		}
		if err != nil {
			return nil, err
		}
		if c.DeletionRequestedAt != nil {
			return nil, denied()
		}
		s.rules = c.Rules
		s.tenantID = c.TenantID
		s.projectID = c.ProjectID
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/session") {
			s.visitorID = uuid.NewString()
		} else {
			rawToken = r.Header.Get(tokenHeader)
			parts := strings.Split(rawToken, ".")
			if len(parts) != 2 || !validID(parts[0]) || len(parts[1]) != 43 {
				return nil, unauthenticated()
			}
			s.visitorID = parts[0]
		}
		s.actorID = s.visitorID
		s.name = "Visitor"
	}
	tx, err := h.options.Begin(ctx)
	if err != nil {
		return nil, err
	}
	s.tx = tx
	ok := false
	defer func() {
		if !ok {
			_ = tx.Rollback(ctx)
		}
	}()
	_, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true),set_config('app.chat_project_id',$2,true),set_config('app.chat_visitor_id',$3,true),set_config('app.chat_operator',$4,true)`, s.tenantID, s.projectID, s.visitorID, strconv.FormatBool(operator))
	if err != nil {
		return nil, err
	}
	if operator {
		header := r.Header.Get("Authorization")
		if strings.Contains(header, "fk_live_") {
			return nil, denied()
		}
		claims, e := middleware.AuthenticateTenantCredential(ctx, tx, h.options.JWT, s.tenantID, header, false)
		switch {
		case errors.Is(e, middleware.ErrTenantCredentialInvalid):
			return nil, unauthenticated()
		case errors.Is(e, middleware.ErrTenantTwoFactorRequired):
			return nil, failure{403, api.CodeTwoFactorEnrollmentRequired, "two-factor enrollment is required"}
		case errors.Is(e, middleware.ErrTenantCredentialForbidden):
			return nil, denied()
		case e != nil:
			return nil, e
		}
		if claims.IsAPIKey || !auth.HasPermission(claims.Permissions, permission) {
			return nil, denied()
		}
		s.actorID = claims.Subject
		s.expires = claims.ExpiresAt.Time
		var permissions []string
		if err = tx.QueryRow(ctx, `SELECT permissions,COALESCE(NULLIF(name,''),split_part(email::text,'@',1)) FROM tenant_members WHERE tenant_id=$1 AND id=$2`, s.tenantID, s.actorID).Scan(&permissions, &s.name); err != nil {
			return nil, err
		}
		if len([]rune(s.name)) > 120 {
			s.name = string([]rune(s.name)[:120])
		}
		if !auth.HasPermission(permissions, permission) {
			return nil, denied()
		}
	}
	err = tx.QueryRow(ctx, `SELECT chat_enabled,chat_allow_visitor_messages FROM projects WHERE tenant_id=$1 AND id=$2`, s.tenantID, s.projectID).Scan(&s.enabled, &s.allowVisitorMessages)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, missing()
	}
	if err != nil {
		return nil, err
	}
	if !operator && !s.enabled {
		return nil, denied()
	}
	if !operator && rawToken != "" {
		var hash []byte
		err = tx.QueryRow(ctx, `SELECT token_hash,expires_at FROM website_chat_visitors WHERE tenant_id=$1 AND project_id=$2 AND id=$3 AND origin=$4`, s.tenantID, s.projectID, s.visitorID, s.origin).Scan(&hash, &s.expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, unauthenticated()
		}
		if err != nil {
			return nil, err
		}
		actual := sha256.Sum256([]byte(rawToken))
		if !time.Now().Before(s.expires) || subtle.ConstantTimeCompare(hash, actual[:]) != 1 {
			return nil, unauthenticated()
		}
	}
	ok = true
	return s, nil
}
func (h *Handler) run(operator bool, p auth.Permission, action func(*http.Request, *scope) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !operator && widgetapi.ValidOrigin(r.Header.Get("Origin")) {
			widgetapi.SetCORSHeaders(w, r.Header.Get("Origin"))
		}
		// Read bounded input before taking a database connection. A slow visitor
		// must not hold the shared pool while uploading a request body.
		if r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 20001))
			_ = r.Body.Close()
			if err != nil || len(body) > 20000 {
				h.fail(w, r, invalid())
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		s, err := h.authorize(r, operator, p)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		defer func() { _ = s.tx.Rollback(r.Context()) }()
		if r.Method != http.MethodGet {
			n := 120
			operation := "write"
			if strings.HasSuffix(r.URL.Path, "/messages") {
				n = 30
				operation = "send"
			}
			key := s.tenantID + ":" + s.projectID + ":" + s.actorID + ":" + operation
			if strings.HasSuffix(r.URL.Path, "/session") {
				key = s.tenantID + ":" + s.projectID + ":session"
				n = 300
			}
			if err = h.limit(r.Context(), key, n); err != nil {
				h.fail(w, r, err)
				return
			}
		}
		result, err := action(r, s)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		if err = s.tx.Commit(r.Context()); err != nil {
			h.fail(w, r, err)
			return
		}
		if s.notify {
			h.notify(r.Context(), s)
		}
		if result == nil {
			w.WriteHeader(204)
			return
		}
		status := 200
		if strings.HasSuffix(r.URL.Path, "/session") {
			status = 201
		}
		api.WriteJSON(w, status, result)
	}
}
func (h *Handler) notify(ctx context.Context, s *scope) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, err := h.options.Redis.Pipelined(ctx, func(p redis.Pipeliner) error {
		p.Publish(ctx, "website-chat:operators:"+s.tenantID+":"+s.projectID, "change")
		if s.visitorID == "" {
			p.Publish(ctx, settingsChannel(s), "change")
		} else {
			p.Publish(ctx, visitorChannel(s), "change")
		}
		return nil
	})
	if err != nil {
		h.options.Logger.Warn("website chat notification unavailable", "error", err)
	}
}
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var f failure
	if errors.As(err, &f) {
		if f.status == 429 {
			w.Header().Set("Retry-After", "10")
		}
		api.WriteError(w, f.status, f.code, f.message, nil)
		return
	}
	h.options.Logger.ErrorContext(r.Context(), "website chat request failed", "error", err)
	api.WriteError(w, 503, api.CodeServiceUnavailable, "chat temporarily unavailable", nil)
}
func (h *Handler) preflight(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	_, err := h.options.Authorizer.Authorize(r.Context(), r.URL.Query().Get("key"), origin)
	if err != nil {
		h.fail(w, r, denied())
		return
	}
	widgetapi.SetCORSHeaders(w, origin)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+widgetapi.PublicKeyHeader+", "+tokenHeader)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Max-Age", "300")
	w.WriteHeader(204)
}
func (h *Handler) settings(r *http.Request, s *scope) (any, error) {
	if r.Method == http.MethodPut {
		var input struct {
			Enabled              *bool `json:"enabled"`
			AllowVisitorMessages *bool `json:"allow_visitor_messages"`
		}
		if decode(r, &input) != nil || (input.Enabled == nil && input.AllowVisitorMessages == nil) {
			return nil, invalid()
		}
		if err := s.tx.QueryRow(r.Context(), `UPDATE projects SET chat_enabled=COALESCE($3,chat_enabled),chat_allow_visitor_messages=COALESCE($4,chat_allow_visitor_messages),updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING chat_enabled,chat_allow_visitor_messages`, s.tenantID, s.projectID, input.Enabled, input.AllowVisitorMessages).Scan(&s.enabled, &s.allowVisitorMessages); err != nil {
			return nil, err
		}
		if err := h.audit(r, s, "settings_updated", auth.PermissionProjectUpdate); err != nil {
			return nil, err
		}
		s.notify = true
	}
	return map[string]any{"enabled": s.enabled, "allow_visitor_messages": s.allowVisitorMessages}, nil
}
func (h *Handler) session(r *http.Request, s *scope) (any, error) {
	var input struct{}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	token := s.visitorID + "." + base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(24 * time.Hour)
	_, err := s.tx.Exec(r.Context(), `INSERT INTO website_chat_visitors(id,tenant_id,project_id,origin,token_hash,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, s.visitorID, s.tenantID, s.projectID, s.origin, hash[:], expires)
	return map[string]any{"token": token, "visitor_id": s.visitorID, "expires_at": expires}, err
}
func messageInput(r *http.Request) (string, string, error) {
	var input struct {
		ClientID string `json:"client_id"`
		Body     string `json:"body"`
	}
	if err := decode(r, &input); err != nil {
		return "", "", err
	}
	input.Body = strings.TrimSpace(input.Body)
	if !validID(input.ClientID) || input.Body == "" || utf8.RuneCountInString(input.Body) > 4000 || strings.ContainsRune(input.Body, 0) {
		return "", "", invalid()
	}
	return input.ClientID, input.Body, nil
}

func (h *Handler) audit(r *http.Request, s *scope, action string, permission auth.Permission) error {
	if !s.operator {
		return nil
	}
	resourceID := s.visitorID
	if resourceID == "" {
		resourceID = s.projectID
	}
	dedup := time.Duration(0)
	if action == "accessed" {
		dedup = accessaudit.DefaultDedupWindow
	}
	return accessaudit.Record(r.Context(), s.tx, accessaudit.RecordInput{TenantID: s.tenantID, Kind: "website_chat." + action, Actor: accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: s.actorID}, ResourceType: "website_chat", ResourceID: resourceID, Action: action, PermissionPath: string(permission), RequestID: middleware.RequestIDFromContext(r.Context()), Outcome: accessaudit.OutcomeSuccess, DedupWindow: dedup})
}
