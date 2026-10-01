package support

import (
	"context"
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
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TokenVerifier interface {
	Verify(string, string) (auth.Claims, error)
}
type HTTPOptions struct {
	AppBegin   db.BeginFunc
	AdminBegin db.BeginFunc
	Resolver   tenant.Resolver
	BaseDomain string
	JWT        TokenVerifier
	Live       Live
	Limiter    ratelimit.Limiter
	Logger     *slog.Logger
}
type Handler struct {
	options HTTPOptions
	store   Store
}
type scope struct {
	tx     db.Transaction
	actor  Actor
	claims auth.Claims
	notify bool
}
type action func(*http.Request, *scope) (any, error)
type requestError struct {
	status  int
	code    api.ErrorCode
	message string
}

func (e requestError) Error() string { return e.message }
func badRequest() error              { return requestError{400, api.CodeValidation, "invalid support request"} }
func forbidden() error               { return requestError{403, api.CodeForbidden, "support access denied"} }

func NewHandler(options HTTPOptions) *Handler {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Handler{options: options}
}

func (h *Handler) RegisterTenantRoutes(router chi.Router) {
	router.Get("/v1/support/conversation", h.run(false, false, h.memberThread))
	router.Post("/v1/support/messages", h.run(false, true, h.send))
	router.Post("/v1/support/read", h.run(false, true, h.read))
	router.Put("/v1/support/presence", h.run(false, true, h.presence))
	router.Delete("/v1/support/presence/{tabID}", h.run(false, true, h.leave))
	router.Get("/v1/support/events", h.events(false))
}
func (h *Handler) RegisterPlatformRoutes(router chi.Router) {
	const base = "/admin/backoffice/support"
	router.Get(base+"/online", h.run(true, false, h.online))
	router.Get(base+"/conversations", h.run(true, false, h.inbox))
	router.Get(base+"/conversations/{conversationID}", h.run(true, false, h.adminThread))
	router.Get(base+"/conversation", h.run(true, false, h.adminMemberThread))
	router.Post(base+"/messages", h.run(true, true, h.send))
	router.Post(base+"/conversations/{conversationID}/read", h.run(true, false, h.read))
	router.Get(base+"/events", h.events(true))
}

// Each request revalidates membership or platform permissions from the database.
// A stream commits this short transaction before subscribing to live events.
func (h *Handler) authorize(r *http.Request, admin, write bool) (*scope, error) {
	ctx := r.Context()
	var resolved tenant.Tenant
	begin := h.options.AppBegin
	if admin {
		begin = h.options.AdminBegin
	} else {
		slug, err := tenant.SlugFromHost(r.Host, h.options.BaseDomain)
		if err != nil {
			return nil, forbidden()
		}
		resolved, err = h.options.Resolver.Resolve(ctx, slug)
		if errors.Is(err, tenant.ErrUnknownTenant) {
			return nil, forbidden()
		}
		if err != nil {
			return nil, err
		}
		if !resolved.IsActiveAt(time.Now()) || resolved.DeletionRequestedAt != nil {
			return nil, forbidden()
		}
	}
	tx, err := begin(ctx)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = tx.Rollback(ctx)
		}
	}()
	s := &scope{tx: tx, actor: Actor{Admin: admin}}
	if admin {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return nil, requestError{401, api.CodeUnauthenticated, "authentication required"}
		}
		s.claims, err = h.options.JWT.Verify(parts[1], auth.AudiencePlatform)
		if err != nil {
			return nil, requestError{401, api.CodeUnauthenticated, "authentication required"}
		}
		required := auth.PermissionPlatformSupportRead
		if write {
			required = auth.PermissionPlatformSupportWrite
		}
		var permissions []string
		err = tx.QueryRow(ctx, `SELECT permissions,COALESCE(NULLIF(name,''),split_part(email::text,'@',1)) FROM platform_admins WHERE id=$1`, s.claims.Subject).Scan(&permissions, &s.actor.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, forbidden()
		}
		if err != nil {
			return nil, err
		}
		if !auth.HasPermission(permissions, required) || !auth.HasPermission(s.claims.Permissions, required) {
			return nil, forbidden()
		}
	} else {
		if _, err = tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, resolved.ID); err != nil {
			return nil, err
		}
		// This surface accepts member sessions only. Reject API keys before
		// their separate machine-credential limiter/lookup is invoked.
		credential := strings.Fields(r.Header.Get("Authorization"))
		if len(credential) == 2 && strings.EqualFold(credential[0], "Bearer") && strings.HasPrefix(credential[1], "fk_live_") {
			return nil, forbidden()
		}
		s.claims, err = middleware.AuthenticateTenantCredential(ctx, tx, h.options.JWT, resolved.ID, r.Header.Get("Authorization"), false)
		if errors.Is(err, middleware.ErrTenantCredentialInvalid) {
			return nil, requestError{401, api.CodeUnauthenticated, "authentication required"}
		}
		if errors.Is(err, middleware.ErrTenantTwoFactorRequired) {
			return nil, requestError{403, api.CodeTwoFactorEnrollmentRequired, "two-factor enrollment is required"}
		}
		if errors.Is(err, middleware.ErrTenantCredentialForbidden) {
			return nil, forbidden()
		}
		if err != nil {
			return nil, err
		}
		if s.claims.IsAPIKey {
			return nil, forbidden()
		}
		s.actor.TenantID = resolved.ID
		s.actor.MemberID = s.claims.Subject
		if _, err = tx.Exec(ctx, `SELECT set_config('app.support_member_id',$1,true)`, s.claims.Subject); err != nil {
			return nil, err
		}
		err = tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(name,''),split_part(email::text,'@',1)) FROM tenant_members WHERE tenant_id=$1 AND id=$2`, resolved.ID, s.claims.Subject).Scan(&s.actor.Name)
		if err != nil {
			return nil, err
		}
	}
	s.actor.ID = s.claims.Subject
	ok = true
	return s, nil
}

func (h *Handler) run(admin, write bool, fn action) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		s, err := h.authorize(r, admin, write)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		defer func() { _ = s.tx.Rollback(r.Context()) }()
		if r.Method != http.MethodGet {
			limit := 120
			operation := "write"
			if strings.HasSuffix(r.URL.Path, "/messages") {
				limit = 30
				operation = "send"
			}
			if err = h.limit(r.Context(), s, operation, limit); err != nil {
				h.fail(w, r, err)
				return
			}
		}
		result, err := fn(r, s)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		if err = s.tx.Commit(r.Context()); err != nil {
			h.fail(w, r, err)
			return
		}
		if s.notify {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
			if err := h.options.Live.Notify(ctx, s.actor.TenantID, s.actor.MemberID); err != nil {
				h.options.Logger.Warn("support notification unavailable", "error", err)
			}
			cancel()
		}
		if result == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	}
}

func (h *Handler) limit(ctx context.Context, s *scope, operation string, count int) error {
	decision, err := h.options.Limiter.Allow(ctx, "support:"+s.actor.kind()+":"+s.actor.ID+":"+operation, ratelimit.Window{Limit: count, Period: time.Minute})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return requestError{429, api.CodeRateLimitExceeded, "too many support requests"}
	}
	return nil
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var failure requestError
	switch {
	case errors.As(err, &failure):
		if failure.status == 429 {
			w.Header().Set("Retry-After", "10")
		}
		api.WriteError(w, failure.status, failure.code, failure.message, nil)
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, 404, api.CodeNotFound, "support conversation not found", nil)
	case errors.Is(err, ErrConflict):
		api.WriteError(w, 409, api.CodeConflict, "message already sent with different content", nil)
	default:
		h.options.Logger.ErrorContext(r.Context(), "support request failed", "error", err)
		api.WriteError(w, 503, api.CodeServiceUnavailable, "support is temporarily unavailable", nil)
	}
}

func decode(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 20000))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return badRequest()
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return badRequest()
	}
	return nil
}
func validID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil
}
func beforeSequence(r *http.Request) (int64, error) {
	value := r.URL.Query().Get("before")
	if value == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 {
		return 0, badRequest()
	}
	return n, nil
}

func (h *Handler) memberThread(r *http.Request, s *scope) (any, error) {
	before, err := beforeSequence(r)
	if err != nil {
		return nil, err
	}
	c, err := h.store.Find(r.Context(), s.tx, s.actor.TenantID, s.actor.MemberID)
	if errors.Is(err, ErrNotFound) {
		return Thread{Messages: []Message{}}, nil
	}
	if err != nil {
		return nil, err
	}
	// Members need no workspace or account-directory fields in their chat.
	c.MemberEmail = ""
	c.WorkspaceName = ""
	return h.store.Thread(r.Context(), s.tx, c, false, before)
}
func (h *Handler) adminThread(r *http.Request, s *scope) (any, error) {
	id := chi.URLParam(r, "conversationID")
	if !validID(id) {
		return nil, ErrNotFound
	}
	before, err := beforeSequence(r)
	if err != nil {
		return nil, err
	}
	c, err := h.store.Get(r.Context(), s.tx, id)
	if err != nil {
		return nil, err
	}
	if err = h.audit(r, s, c, "read", accessaudit.DefaultDedupWindow); err != nil {
		return nil, err
	}
	return h.store.Thread(r.Context(), s.tx, c, true, before)
}

func (h *Handler) adminMemberThread(r *http.Request, s *scope) (any, error) {
	tenantID, memberID := r.URL.Query().Get("tenant_id"), r.URL.Query().Get("member_id")
	if !validID(tenantID) || !validID(memberID) {
		return nil, badRequest()
	}
	c, err := h.store.Find(r.Context(), s.tx, tenantID, memberID)
	if errors.Is(err, ErrNotFound) {
		return Thread{Messages: []Message{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if err = h.audit(r, s, c, "read", accessaudit.DefaultDedupWindow); err != nil {
		return nil, err
	}
	return h.store.Thread(r.Context(), s.tx, c, true, 0)
}
func (h *Handler) inbox(r *http.Request, s *scope) (any, error) {
	before := r.URL.Query().Get("before")
	if before != "" && !validID(before) {
		return nil, badRequest()
	}
	items, err := h.store.Inbox(r.Context(), s.tx, before)
	if err != nil {
		return nil, err
	}
	next := ""
	if len(items) > PageSize {
		items = items[:PageSize]
		next = items[len(items)-1].ID
	}
	return map[string]any{"conversations": items, "next_cursor": next}, nil
}

func (h *Handler) send(r *http.Request, s *scope) (any, error) {
	var input struct {
		TenantID string `json:"tenant_id"`
		MemberID string `json:"member_id"`
		ClientID string `json:"client_id"`
		Body     string `json:"body"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	input.Body = strings.TrimSpace(input.Body)
	if !validID(input.ClientID) || input.Body == "" || utf8.RuneCountInString(input.Body) > MessageLimit || strings.ContainsRune(input.Body, 0) {
		return nil, badRequest()
	}
	if s.actor.Admin {
		if !validID(input.TenantID) || !validID(input.MemberID) {
			return nil, badRequest()
		}
		var exists bool
		err := s.tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant_members m JOIN tenants t ON t.id=m.tenant_id
            WHERE m.tenant_id=$1 AND m.id=$2 AND m.status='active' AND t.deletion_requested_at IS NULL
            AND (t.status='active' OR (t.status='trial' AND t.trial_ends_at>now())))`, input.TenantID, input.MemberID).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		s.actor.TenantID = input.TenantID
		s.actor.MemberID = input.MemberID
	} else if input.TenantID != "" || input.MemberID != "" {
		return nil, badRequest()
	}
	c, message, created, err := h.store.Send(r.Context(), s.tx, s.actor, input.ClientID, input.Body)
	if err != nil {
		return nil, err
	}
	if created {
		if err = h.audit(r, s, c, "message_sent", 0); err != nil {
			return nil, err
		}
	}
	s.notify = true
	return map[string]any{"conversation_id": c.ID, "message": message}, nil
}

func (h *Handler) read(r *http.Request, s *scope) (any, error) {
	var input struct {
		Sequence int64 `json:"sequence"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if input.Sequence < 0 {
		return nil, badRequest()
	}
	var c Conversation
	var err error
	if s.actor.Admin {
		id := chi.URLParam(r, "conversationID")
		if !validID(id) {
			return nil, ErrNotFound
		}
		c, err = h.store.Get(r.Context(), s.tx, id)
	} else {
		c, err = h.store.Find(r.Context(), s.tx, s.actor.TenantID, s.actor.MemberID)
	}
	if err != nil {
		return nil, err
	}
	if input.Sequence > c.LastSequence {
		return nil, badRequest()
	}
	previous := c.MemberReadSequence
	if s.actor.Admin {
		previous = c.AdminReadSequence
	}
	if input.Sequence <= previous {
		return nil, nil
	}
	if err = h.store.MarkRead(r.Context(), s.tx, c, s.actor.Admin, input.Sequence); err != nil {
		return nil, err
	}
	s.actor.TenantID = c.TenantID
	s.actor.MemberID = c.MemberID
	s.notify = true
	return nil, nil
}

func (h *Handler) audit(r *http.Request, s *scope, c Conversation, action string, dedup time.Duration) error {
	actor := accessaudit.ActorFromClaims(s.claims)
	permission := "support:self"
	if s.actor.Admin {
		actor = accessaudit.PlatformAdminActor(s.actor.ID)
		permission = string(auth.PermissionPlatformSupportRead)
		if action == "message_sent" {
			permission = string(auth.PermissionPlatformSupportWrite)
		}
	}
	return accessaudit.Record(r.Context(), s.tx, accessaudit.RecordInput{TenantID: c.TenantID, Kind: "support." + action, Actor: actor, ResourceType: "support_conversation", ResourceID: c.ID, Action: action, PermissionPath: permission, RequestID: middleware.RequestIDFromContext(r.Context()), Outcome: accessaudit.OutcomeSuccess, DedupWindow: dedup})
}

func (h *Handler) presence(r *http.Request, s *scope) (any, error) {
	var input struct {
		TabID   string `json:"tab_id"`
		Section string `json:"section"`
		State   string `json:"state"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if !validID(input.TabID) || (input.State != "active" && input.State != "idle") {
		return nil, badRequest()
	}
	switch input.Section {
	case "overview", "feedback", "replays", "issues", "notifications", "testimonials", "analytics", "projects", "settings", "profile", "other":
	default:
		return nil, badRequest()
	}
	return nil, h.options.Live.Touch(r.Context(), Presence{TenantID: s.actor.TenantID, MemberID: s.actor.MemberID, TabID: input.TabID, Section: input.Section, State: input.State})
}
func (h *Handler) leave(r *http.Request, s *scope) (any, error) {
	id := chi.URLParam(r, "tabID")
	if !validID(id) {
		return nil, badRequest()
	}
	return nil, h.options.Live.Leave(r.Context(), s.actor.TenantID, s.actor.MemberID, id)
}
func (h *Handler) online(r *http.Request, s *scope) (any, error) {
	users, truncated, err := h.options.Live.Online(r.Context(), s.tx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"users": users, "truncated": truncated}, nil
}
