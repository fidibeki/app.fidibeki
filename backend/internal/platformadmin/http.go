package platformadmin

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/billing"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	platformRefreshCookie          = "fidibeki_platform_refresh"
	PlatformQueueAccessCookie      = "fidibeki_platform_queue_access"
	PlatformOperationsAccessCookie = "fidibeki_platform_operations_access"
)

type TenantStore interface {
	CreateTenant(context.Context, CreateTenantParams) (TenantRecord, error)
	GetTenant(context.Context, string) (TenantRecord, error)
	ListTenants(context.Context) ([]TenantRecord, error)
	UpdateTenant(context.Context, TenantRecord, string) (TenantRecord, error)
	DeleteTenant(context.Context, string) error
	RecoverOwnership(context.Context, RecoverOwnershipParams) error
}

type BackofficeStore interface {
	Overview(context.Context) (BackofficeOverview, error)
	ListBackofficeTenants(context.Context, BackofficeTenantListParams) (BackofficeTenantPage, error)
	GetBackofficeTenant(context.Context, string) (BackofficeTenantDetail, error)
	ListBackofficeRecoveryCandidates(context.Context, string, BackofficeRecoveryCandidateListParams) (BackofficeRecoveryCandidatePage, error)
	ListBackofficeAccessEvents(context.Context, string, BackofficeAccessEventListParams) (BackofficeAccessEventPage, error)
}

type HTTPOptions struct {
	Auth          Service
	Tenants       TenantStore
	Backoffice    BackofficeStore
	JWT           *auth.JWTManager
	SecureCookies bool
	RefreshTTL    time.Duration
	Now           func() time.Time
	Deleter       TenantDeleter
	LoginLimiter  ratelimit.Limiter
	Logger        *slog.Logger
}

type TenantDeleter interface {
	ForceDelete(context.Context, string, string) error
}

type HTTPHandler struct {
	options HTTPOptions
}

func NewHTTPHandler(options HTTPOptions) http.Handler {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{options: options}
	router := chi.NewRouter()
	router.Post("/admin/auth/login", handler.login)
	router.Get("/admin/auth/verify", handler.verifyPage)
	router.Post("/admin/auth/verify", handler.verify)
	router.Post("/admin/auth/refresh", handler.refresh)
	router.Post("/admin/auth/logout", handler.logout)
	router.Post("/admin/backoffice/auth/login", handler.backofficeLogin)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantRead)).
		Get("/admin/backoffice/overview", handler.backofficeOverview)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantRead)).
		Get("/admin/backoffice/tenants", handler.backofficeTenants)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantRead)).
		Get("/admin/backoffice/tenants/{tenantID}", handler.backofficeTenant)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantUpdate)).
		Get("/admin/backoffice/tenants/{tenantID}/recovery-candidates", handler.backofficeRecoveryCandidates)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantRead)).
		Get("/admin/backoffice/tenants/{tenantID}/access-events", handler.backofficeAccessEvents)

	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantCreate)).
		Post("/admin/tenants", handler.createTenant)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantRead)).
		Get("/admin/tenants", handler.listTenants)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantRead)).
		Get("/admin/tenants/{tenantID}", handler.getTenant)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantUpdate)).
		Patch("/admin/tenants/{tenantID}", handler.updateTenant)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantUpdate)).
		Post("/admin/tenants/{tenantID}/ownership/recover", handler.recoverOwnership)
	router.With(appmiddleware.RequirePlatformAdmin(options.JWT, auth.PermissionPlatformTenantDelete)).
		Delete("/admin/tenants/{tenantID}", handler.deleteTenant)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "resource not found", nil)
	})
	return router
}

func (h *HTTPHandler) login(w http.ResponseWriter, r *http.Request) {
	h.requestLogin(w, r, h.options.Auth.RequestMagicLink, h.options.LoginLimiter)
}

func (h *HTTPHandler) backofficeLogin(w http.ResponseWriter, r *http.Request) {
	h.requestLogin(w, r, h.options.Auth.RequestBackofficeMagicLink, h.options.LoginLimiter)
}

func (h *HTTPHandler) backofficeOverview(w http.ResponseWriter, r *http.Request) {
	if h.options.Backoffice == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	overview, err := h.options.Backoffice.Overview(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]BackofficeOverview{"overview": overview})
}

func (h *HTTPHandler) backofficeTenants(w http.ResponseWriter, r *http.Request) {
	if h.options.Backoffice == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	params, err := parseBackofficeTenantListParams(r)
	if err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, err.Error(), nil)
		return
	}
	page, err := h.options.Backoffice.ListBackofficeTenants(r.Context(), params)
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, page)
}

func (h *HTTPHandler) backofficeTenant(w http.ResponseWriter, r *http.Request) {
	if h.options.Backoffice == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	tenantID, ok := backofficeTenantID(w, r)
	if !ok {
		return
	}
	detail, err := h.options.Backoffice.GetBackofficeTenant(r.Context(), tenantID)
	if errors.Is(err, ErrTenantNotFound) {
		writeTenantError(w, err)
		return
	}
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, detail)
}

func (h *HTTPHandler) backofficeRecoveryCandidates(w http.ResponseWriter, r *http.Request) {
	if h.options.Backoffice == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	tenantID, ok := backofficeTenantID(w, r)
	if !ok {
		return
	}
	params, err := parseBackofficeRecoveryCandidateListParams(r)
	if err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, err.Error(), nil)
		return
	}
	page, err := h.options.Backoffice.ListBackofficeRecoveryCandidates(r.Context(), tenantID, params)
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, page)
}

func parseBackofficeRecoveryCandidateListParams(r *http.Request) (BackofficeRecoveryCandidateListParams, error) {
	params := BackofficeRecoveryCandidateListParams{Limit: 25, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor"))}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return BackofficeRecoveryCandidateListParams{}, errors.New("limit must be between 1 and 100")
		}
		params.Limit = limit
	}
	if params.Cursor != "" {
		if _, err := decodeBackofficeTenantCursor(params.Cursor); err != nil {
			return BackofficeRecoveryCandidateListParams{}, err
		}
	}
	return params, nil
}

func (h *HTTPHandler) backofficeAccessEvents(w http.ResponseWriter, r *http.Request) {
	if h.options.Backoffice == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	tenantID, ok := backofficeTenantID(w, r)
	if !ok {
		return
	}
	params, err := parseBackofficeAccessEventListParams(r)
	if err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, err.Error(), nil)
		return
	}
	page, err := h.options.Backoffice.ListBackofficeAccessEvents(r.Context(), tenantID, params)
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, page)
}

func parseBackofficeAccessEventListParams(r *http.Request) (BackofficeAccessEventListParams, error) {
	params := BackofficeAccessEventListParams{Limit: 25, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor"))}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return BackofficeAccessEventListParams{}, errors.New("limit must be between 1 and 100")
		}
		params.Limit = limit
	}
	if params.Cursor != "" {
		if _, err := decodeBackofficeAccessEventCursor(params.Cursor); err != nil {
			return BackofficeAccessEventListParams{}, err
		}
	}
	return params, nil
}

func backofficeTenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "tenantID")
	if _, err := uuid.Parse(id); err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "tenant ID is invalid", nil)
		return "", false
	}
	return id, true
}

func parseBackofficeTenantListParams(r *http.Request) (BackofficeTenantListParams, error) {
	query := r.URL.Query()
	params := BackofficeTenantListParams{
		Query:  strings.TrimSpace(query.Get("q")),
		Cursor: strings.TrimSpace(query.Get("cursor")),
		Limit:  25,
	}
	if len(params.Query) > 120 {
		return BackofficeTenantListParams{}, errors.New("q must not exceed 120 characters")
	}
	if rawLimit := query.Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > 100 {
			return BackofficeTenantListParams{}, errors.New("limit must be between 1 and 100")
		}
		params.Limit = limit
	}
	if rawStatus := strings.TrimSpace(query.Get("status")); rawStatus != "" {
		params.Status = tenant.Status(rawStatus)
		switch params.Status {
		case tenant.StatusActive, tenant.StatusTrial, tenant.StatusSuspended, tenant.StatusCancelled:
		default:
			return BackofficeTenantListParams{}, errors.New("status is invalid")
		}
	}
	if rawPlan := strings.TrimSpace(query.Get("plan")); rawPlan != "" {
		params.Plan = tenant.Plan(rawPlan)
		if _, exists := config.PlanFor(params.Plan); !exists {
			return BackofficeTenantListParams{}, errors.New("plan is invalid")
		}
	}
	if params.Cursor != "" {
		if _, err := decodeBackofficeTenantCursor(params.Cursor); err != nil {
			return BackofficeTenantListParams{}, err
		}
	}
	return params, nil
}

func (h *HTTPHandler) requestLogin(
	w http.ResponseWriter,
	r *http.Request,
	requestMagicLink func(context.Context, string) error,
	limiter ratelimit.Limiter,
) {
	var request struct {
		Email string `json:"email"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	if limiter != nil {
		key := "platform-login:" + clientIP(r.RemoteAddr) + ":" + auth.HashToken(strings.ToLower(strings.TrimSpace(request.Email)))
		decision, err := limiter.Allow(r.Context(), key, ratelimit.Window{Limit: 5, Period: 15 * time.Minute})
		if err != nil {
			api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
			return
		}
		if !decision.Allowed {
			api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded, "rate limit exceeded", nil)
			return
		}
	}
	if err := requestMagicLink(r.Context(), request.Email); err != nil {
		if strings.Contains(err.Error(), "email is invalid") {
			api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "email is invalid", nil)
			return
		}
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *HTTPHandler) verify(w http.ResponseWriter, r *http.Request) {
	token, formRequest, ok := decodeVerificationToken(w, r)
	if !ok {
		return
	}
	tokens, err := h.options.Auth.VerifyMagicLink(
		r.Context(), token, r.UserAgent(), clientIP(r.RemoteAddr),
	)
	if err != nil {
		if formRequest {
			writeVerificationPage(w, http.StatusUnauthorized, verificationPageData{
				Title: "Sign-in link unavailable",
				Body:  "This platform-admin link is invalid, expired, or has already been used.",
			})
			return
		}
		writeAuthError(w, err)
		return
	}
	h.setRefreshCookie(w, tokens.RefreshToken)
	h.setQueueAccessCookie(w, tokens.AccessToken, tokens.ExpiresAt)
	h.setOperationsAccessCookie(w, tokens.AccessToken, tokens.ExpiresAt)
	if formRequest {
		http.Redirect(w, r, "/admin/queues/", http.StatusSeeOther)
		return
	}
	writeSession(w, tokens)
}

func (h *HTTPHandler) verifyPage(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		writeVerificationPage(w, http.StatusBadRequest, verificationPageData{
			Title: "Sign-in link incomplete",
			Body:  "This platform-admin link does not include a verification token.",
		})
		return
	}
	writeVerificationPage(w, http.StatusOK, verificationPageData{
		Title:       "Open queue operations",
		Body:        "Confirm this sign-in to inspect Fidibeki background jobs and delivery queues.",
		Token:       token,
		ShowConfirm: true,
	})
}

func (h *HTTPHandler) refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(platformRefreshCookie)
	if err != nil {
		writeAuthError(w, ErrRefreshInvalid)
		return
	}
	tokens, err := h.options.Auth.Refresh(
		r.Context(), cookie.Value, r.UserAgent(), clientIP(r.RemoteAddr),
	)
	if err != nil {
		if errors.Is(err, ErrRefreshReuse) {
			h.options.Logger.WarnContext(r.Context(), "platform refresh token reuse detected; sessions revoked")
		}
		h.clearRefreshCookie(w)
		h.clearQueueAccessCookie(w)
		h.clearOperationsAccessCookie(w)
		writeAuthError(w, err)
		return
	}
	h.setRefreshCookie(w, tokens.RefreshToken)
	h.setQueueAccessCookie(w, tokens.AccessToken, tokens.ExpiresAt)
	h.setOperationsAccessCookie(w, tokens.AccessToken, tokens.ExpiresAt)
	writeSession(w, tokens)
}

func (h *HTTPHandler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(platformRefreshCookie); err == nil {
		if err := h.options.Auth.Logout(r.Context(), cookie.Value); err != nil {
			api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
			return
		}
	}
	h.clearRefreshCookie(w)
	h.clearQueueAccessCookie(w)
	h.clearOperationsAccessCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type createTenantRequest struct {
	Name         string        `json:"name"`
	Slug         string        `json:"slug"`
	Plan         tenant.Plan   `json:"plan"`
	Status       tenant.Status `json:"status"`
	BillingEmail *string       `json:"billing_email"`
	TrialEndsAt  *time.Time    `json:"trial_ends_at"`
	OwnerEmail   string        `json:"owner_email"`
}

func (h *HTTPHandler) createTenant(w http.ResponseWriter, r *http.Request) {
	var request createTenantRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	input, err := h.validateCreateTenant(request)
	if err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, err.Error(), nil)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	input.ActorPlatformAdminID = claims.Subject
	record, err := h.options.Tenants.CreateTenant(r.Context(), input)
	if err != nil {
		writeTenantError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, map[string]any{"tenant": record})
}

func (h *HTTPHandler) recoverOwnership(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TargetMemberID        string `json:"target_member_id"`
		Confirmation          string `json:"confirmation"`
		VerificationReference string `json:"verification_reference"`
		Reason                string `json:"reason"`
	}
	if !decodeRequest(w, r, &request) {
		return
	}
	tenantID := chi.URLParam(r, "tenantID")
	if _, err := uuid.Parse(tenantID); err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "tenant ID is invalid", nil)
		return
	}
	if _, err := uuid.Parse(request.TargetMemberID); err != nil || strings.TrimSpace(request.Confirmation) == "" ||
		len(strings.TrimSpace(request.VerificationReference)) < 3 || len(strings.TrimSpace(request.Reason)) < 10 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "ownership recovery evidence is invalid", nil)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	if err := h.options.Tenants.RecoverOwnership(r.Context(), RecoverOwnershipParams{
		TenantID: tenantID, ActorPlatformAdminID: claims.Subject,
		TargetMemberID: request.TargetMemberID, Confirmation: strings.TrimSpace(request.Confirmation),
		VerificationReference: strings.TrimSpace(request.VerificationReference), Reason: strings.TrimSpace(request.Reason),
	}); err != nil {
		writeTenantError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"recovered": true})
}

func (h *HTTPHandler) listTenants(w http.ResponseWriter, r *http.Request) {
	records, err := h.options.Tenants.ListTenants(r.Context())
	if err != nil {
		writeTenantError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"tenants": records})
}

func (h *HTTPHandler) getTenant(w http.ResponseWriter, r *http.Request) {
	record, err := h.options.Tenants.GetTenant(r.Context(), chi.URLParam(r, "tenantID"))
	if err != nil {
		writeTenantError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"tenant": record})
}

type updateTenantRequest struct {
	Name         *string        `json:"name"`
	Slug         *string        `json:"slug"`
	Plan         *tenant.Plan   `json:"plan"`
	Status       *tenant.Status `json:"status"`
	BillingEmail *string        `json:"billing_email"`
	TrialEndsAt  *time.Time     `json:"trial_ends_at"`
}

func (h *HTTPHandler) updateTenant(w http.ResponseWriter, r *http.Request) {
	current, err := h.options.Tenants.GetTenant(r.Context(), chi.URLParam(r, "tenantID"))
	if err != nil {
		writeTenantError(w, err)
		return
	}
	var request updateTenantRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	if request.Name != nil {
		current.Name = strings.TrimSpace(*request.Name)
	}
	if request.Slug != nil {
		current.Slug = strings.ToLower(strings.TrimSpace(*request.Slug))
	}
	if request.Plan != nil {
		current.Plan = *request.Plan
	}
	if request.Status != nil {
		if *request.Status == tenant.StatusSuspended {
			claims, _ := appmiddleware.PlatformClaimsFromContext(r.Context())
			if !auth.HasPermission(claims.Permissions, auth.PermissionPlatformTenantSuspend) {
				api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "permission denied", nil)
				return
			}
		}
		current.Status = *request.Status
	}
	if request.BillingEmail != nil {
		emailAddress := strings.ToLower(strings.TrimSpace(*request.BillingEmail))
		current.BillingEmail = &emailAddress
	}
	if request.TrialEndsAt != nil {
		current.TrialEndsAt = request.TrialEndsAt
	}
	if err := h.validateTenantRecord(&current); err != nil {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, err.Error(), nil)
		return
	}
	claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	record, err := h.options.Tenants.UpdateTenant(r.Context(), current, claims.Subject)
	if err != nil {
		writeTenantError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"tenant": record})
}

func (h *HTTPHandler) deleteTenant(w http.ResponseWriter, r *http.Request) {
	if h.options.Deleter != nil {
		claims, ok := appmiddleware.PlatformClaimsFromContext(r.Context())
		if !ok {
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}
		if err := h.options.Deleter.ForceDelete(r.Context(), chi.URLParam(r, "tenantID"), claims.Subject); err != nil {
			if errors.Is(err, billing.ErrNotFound) {
				writeTenantError(w, ErrTenantNotFound)
				return
			}
			writeTenantError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.options.Tenants.DeleteTenant(r.Context(), chi.URLParam(r, "tenantID")); err != nil {
		writeTenantError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) validateCreateTenant(request createTenantRequest) (CreateTenantParams, error) {
	status := request.Status
	if status == "" {
		status = tenant.StatusTrial
	}
	plan := request.Plan
	if plan == "" {
		plan = tenant.PlanStarter
	}
	record := TenantRecord{
		Name: strings.TrimSpace(request.Name), Slug: strings.ToLower(strings.TrimSpace(request.Slug)),
		Plan: plan, Status: status, BillingEmail: request.BillingEmail, TrialEndsAt: request.TrialEndsAt,
	}
	if err := h.validateTenantRecord(&record); err != nil {
		return CreateTenantParams{}, err
	}
	ownerEmail, err := normalizedEmail(request.OwnerEmail)
	if err != nil {
		return CreateTenantParams{}, errors.New("owner_email is invalid")
	}
	return CreateTenantParams{
		Name: record.Name, Slug: record.Slug, Plan: record.Plan, Status: record.Status,
		BillingEmail: record.BillingEmail, TrialEndsAt: record.TrialEndsAt, OwnerEmail: ownerEmail,
	}, nil
}

func (h *HTTPHandler) validateTenantRecord(record *TenantRecord) error {
	if len(record.Name) < 1 || len(record.Name) > 120 {
		return errors.New("name must contain 1 to 120 characters")
	}
	if err := tenant.ValidateSlug(record.Slug); err != nil {
		return errors.New("slug must contain 3 to 24 lowercase letters, numbers, or hyphens and cannot be reserved")
	}
	if _, exists := config.PlanFor(record.Plan); !exists {
		return errors.New("plan is invalid")
	}
	switch record.Status {
	case tenant.StatusActive, tenant.StatusSuspended, tenant.StatusCancelled:
		record.TrialEndsAt = nil
	case tenant.StatusTrial:
		if record.TrialEndsAt == nil {
			trialEnd := h.now().Add(DefaultTrial)
			record.TrialEndsAt = &trialEnd
		}
		if !record.TrialEndsAt.After(h.now()) {
			return errors.New("trial_ends_at must be in the future")
		}
	default:
		return errors.New("status is invalid")
	}
	if record.BillingEmail != nil {
		emailAddress, err := normalizedEmail(*record.BillingEmail)
		if err != nil {
			return errors.New("billing_email is invalid")
		}
		record.BillingEmail = &emailAddress
	}
	return nil
}

func (h *HTTPHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now()
}

func (h *HTTPHandler) setRefreshCookie(w http.ResponseWriter, value string) {
	ttl := h.options.RefreshTTL
	if ttl <= 0 {
		ttl = RefreshTTL
	}
	http.SetCookie(w, &http.Cookie{
		Name: platformRefreshCookie, Value: value, Path: "/admin/auth",
		MaxAge: int(ttl.Seconds()), HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (h *HTTPHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: platformRefreshCookie, Path: "/admin/auth", MaxAge: -1,
		HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (h *HTTPHandler) setQueueAccessCookie(w http.ResponseWriter, value string, expiresAt time.Time) {
	maxAge := int(expiresAt.Sub(h.now()).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name: PlatformQueueAccessCookie, Value: value, Path: "/admin/queues",
		Expires: expiresAt, MaxAge: maxAge, HttpOnly: true,
		Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (h *HTTPHandler) clearQueueAccessCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: PlatformQueueAccessCookie, Path: "/admin/queues", MaxAge: -1,
		HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (h *HTTPHandler) setOperationsAccessCookie(w http.ResponseWriter, value string, expiresAt time.Time) {
	maxAge := int(expiresAt.Sub(h.now()).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name: PlatformOperationsAccessCookie, Value: value, Path: "/admin/operations",
		Expires: expiresAt, MaxAge: maxAge, HttpOnly: true,
		Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func (h *HTTPHandler) clearOperationsAccessCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: PlatformOperationsAccessCookie, Path: "/admin/operations", MaxAge: -1,
		HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteStrictMode,
	})
}

func writeSession(w http.ResponseWriter, tokens SessionTokens) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"access_token": tokens.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   int(time.Until(tokens.ExpiresAt).Seconds()),
		"admin":        tokens.Admin,
	})
}

func writeAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrMagicLinkInvalid) || errors.Is(err, ErrRefreshInvalid) ||
		errors.Is(err, ErrRefreshExpired) || errors.Is(err, ErrRefreshReuse) {
		api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "authentication failed", nil)
		return
	}
	api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
}

func writeTenantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTenantNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "tenant not found", nil)
	case errors.Is(err, ErrSlugConflict):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "tenant slug already exists", nil)
	case errors.Is(err, ErrOwnershipConfirmation):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeOwnershipConfirmation, "workspace confirmation does not match", nil)
	case errors.Is(err, ErrRecentAuthenticationRequired):
		api.WriteError(w, http.StatusForbidden, api.CodeRecentAuthRequired, "recent authentication is required", nil)
	case errors.Is(err, ErrOwnershipTargetInvalid):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeOwnershipTarget, "new workspace owner is invalid", nil)
	case errors.Is(err, ErrRecoveryVerificationInvalid):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "ownership recovery evidence is invalid", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body must contain one JSON value", nil)
		return false
	}
	return true
}

func decodeVerificationToken(w http.ResponseWriter, r *http.Request) (string, bool, bool) {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		if err := r.ParseForm(); err != nil {
			writeVerificationPage(w, http.StatusBadRequest, verificationPageData{
				Title: "Sign-in could not continue", Body: "The verification request was invalid.",
			})
			return "", true, false
		}
		token := strings.TrimSpace(r.PostForm.Get("token"))
		if token == "" {
			writeVerificationPage(w, http.StatusBadRequest, verificationPageData{
				Title: "Sign-in link incomplete", Body: "The verification token is missing.",
			})
			return "", true, false
		}
		return token, true, true
	}
	var request struct {
		Token string `json:"token"`
	}
	if !decodeRequest(w, r, &request) {
		return "", false, false
	}
	return strings.TrimSpace(request.Token), false, true
}

type verificationPageData struct {
	Title       string
	Body        string
	Token       string
	ShowConfirm bool
}

var verificationPage = template.Must(template.New("platform-verification").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} · Fidibeki</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, sans-serif; color: #172321; background: #f3f6f5; }
    * { box-sizing: border-box; }
    body { display: grid; min-height: 100vh; margin: 0; place-items: center; padding: 24px; }
    main { width: min(100%, 440px); background: #fff; border: 1px solid #d9e1df; border-radius: 6px; box-shadow: 0 14px 40px rgba(20, 40, 36, .08); }
    header { display: flex; align-items: center; gap: 10px; padding: 15px 18px; border-bottom: 1px solid #e6ebe9; font-size: 14px; font-weight: 750; }
    header span { display: grid; width: 28px; height: 28px; place-items: center; background: #0d7c6c; color: #fff; border-radius: 5px; }
    section { padding: 26px 24px 24px; }
    p:first-child { margin: 0 0 7px; color: #0d7c6c; font-size: 11px; font-weight: 750; text-transform: uppercase; }
    h1 { margin: 0; font-size: 22px; letter-spacing: 0; }
    section > p:last-of-type { margin: 11px 0 22px; color: #5e6d69; font-size: 14px; line-height: 1.55; }
    button { width: 100%; min-height: 42px; border: 0; border-radius: 5px; background: #0d7c6c; color: #fff; font: inherit; font-size: 13px; font-weight: 750; cursor: pointer; }
    button:hover { background: #09695c; }
  </style>
</head>
<body>
  <main>
    <header><span>F</span> fidibeki operations</header>
    <section>
      <p>Platform admin</p>
      <h1>{{.Title}}</h1>
      <p>{{.Body}}</p>
      {{if .ShowConfirm}}<form method="post" action="/admin/auth/verify"><input type="hidden" name="token" value="{{.Token}}"><button type="submit">Continue to queue monitor</button></form>{{end}}
    </section>
  </main>
</body>
</html>`))

func writeVerificationPage(w http.ResponseWriter, status int, data verificationPageData) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = verificationPage.Execute(w, data)
}

func clientIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return ""
}
