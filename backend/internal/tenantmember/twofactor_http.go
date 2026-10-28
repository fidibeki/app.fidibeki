package tenantmember

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/httpx"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/fidibeki/app.fidibeki/backend/internal/ratelimit"
	"github.com/go-chi/chi/v5"
)

// Two-factor attempt budgets. The challenge is limited per client address as
// well as per session so neither a stolen refresh cookie nor a botnet can grind
// through the six-digit space, and enrolment-side operations get a slower
// budget because a legitimate member only performs them occasionally.
var (
	twoFactorChallengeIPWindow      = ratelimit.Window{Limit: 30, Period: time.Minute}
	twoFactorChallengeSessionWindow = ratelimit.Window{Limit: 10, Period: 5 * time.Minute}
	twoFactorMemberWindow           = ratelimit.Window{Limit: 20, Period: time.Hour}
)

func registerTwoFactorRoutes(router chi.Router, handler *HTTPHandler, options HTTPOptions) {
	// The challenge runs before the session carries an access token, so it is
	// authenticated by the refresh cookie alone.
	router.Post("/auth/2fa/challenge", handler.twoFactorChallenge)

	// Enrolment routes authenticate the member but skip the policy gate: a
	// member locked out by a newly required policy must be able to reach them.
	// They expose no workspace data. Read-only workspaces are not blocked here
	// either — account security must stay reachable while a deletion is pending.
	setup := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMemberForTwoFactorSetup(options.JWT),
	}
	router.With(setup...).Get("/v1/security/two-factor", handler.twoFactorStatus)
	router.With(setup...).Post("/v1/security/two-factor/enroll", handler.startTwoFactorEnrollment)
	router.With(setup...).Post("/v1/security/two-factor/confirm", handler.confirmTwoFactorEnrollment)
	router.With(setup...).Post("/v1/security/two-factor/recovery-codes", handler.regenerateRecoveryCodes)
	router.With(setup...).Delete("/v1/security/two-factor", handler.removeTwoFactorEnrollment)

	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionTenantRead))...).
		Get("/v1/security/two-factor/policy", handler.getTwoFactorPolicy)
	router.With(append(
		protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate),
	)...).Patch("/v1/security/two-factor/policy", handler.updateTwoFactorPolicy)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionMemberRead))...).
		Get("/v1/security/two-factor/compliance", handler.twoFactorCompliance)
	router.With(append(
		protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionMemberUpdate),
	)...).Post("/v1/members/{memberID}/two-factor/reset", handler.resetMemberTwoFactor)
}

func (h *HTTPHandler) twoFactorStatus(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	status, err := h.options.TwoFactor.Status(r.Context(), query, resolved.ID, claims.Subject)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"two_factor": status})
}

func (h *HTTPHandler) startTwoFactorEnrollment(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if !h.allowTwoFactorAttempt(w, r, "enroll:"+resolved.ID+":"+claims.Subject, twoFactorMemberWindow) {
		return
	}
	member, err := h.options.Store.FindMemberByID(r.Context(), query, resolved.ID, claims.Subject)
	if err != nil {
		writeError(w, err)
		return
	}
	enrollment, err := h.options.TwoFactor.StartEnrollment(
		r.Context(), query, resolved, claims.Subject, member.Email,
	)
	if err != nil {
		if errors.Is(err, ErrTwoFactorAlreadyEnrolled) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	// The response is the one and only delivery of this secret; it is never
	// logged and never returned again.
	api.WriteJSON(w, http.StatusCreated, map[string]any{"enrollment": enrollment})
}

func (h *HTTPHandler) confirmTwoFactorEnrollment(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if !h.allowTwoFactorAttempt(w, r, "confirm:"+resolved.ID+":"+claims.Subject, twoFactorMemberWindow) {
		return
	}
	codes, err := h.options.TwoFactor.ConfirmEnrollment(
		r.Context(), query, resolved.ID, claims.Subject, request.Code,
	)
	if err != nil {
		if isTwoFactorDecisionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (h *HTTPHandler) twoFactorChallenge(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	cookie, err := r.Cookie(tenantRefreshCookie)
	if err != nil {
		writeError(w, ErrRefreshInvalid)
		return
	}
	refreshHash := auth.HashToken(cookie.Value)
	if !h.allowTwoFactorAttempt(
		w, r, "challenge:ip:"+httpx.ClientIPFromRequest(r, h.options.TrustedProxyCIDRs),
		twoFactorChallengeIPWindow,
	) {
		return
	}
	if !h.allowTwoFactorAttempt(
		w, r, "challenge:session:"+resolved.ID+":"+refreshHash, twoFactorChallengeSessionWindow,
	) {
		return
	}
	member, err := h.options.TwoFactor.Challenge(r.Context(), query, resolved.ID, refreshHash, request.Code)
	if err != nil {
		if isTwoFactorDecisionError(err) {
			// A failed attempt is still an audited security event, so the
			// transaction commits even though the response is an error.
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	accessToken, expiresAt, err := h.options.JWT.IssueTenantMember(
		member.ID, resolved.ID, member.Permissions, member.AuthorizationVersion,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeTokens(w, SessionTokens{
		AccessToken: accessToken, ExpiresAt: expiresAt, Member: member,
	}, resolved)
}

func (h *HTTPHandler) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if !h.allowTwoFactorAttempt(w, r, "recovery:"+resolved.ID+":"+claims.Subject, twoFactorMemberWindow) {
		return
	}
	codes, err := h.options.TwoFactor.RegenerateRecoveryCodes(
		r.Context(), query, resolved.ID, claims.Subject, request.Code,
	)
	if err != nil {
		if isTwoFactorDecisionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (h *HTTPHandler) removeTwoFactorEnrollment(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if !h.allowTwoFactorAttempt(w, r, "remove:"+resolved.ID+":"+claims.Subject, twoFactorMemberWindow) {
		return
	}
	if err := h.options.TwoFactor.Remove(
		r.Context(), query, resolved.ID, claims.Subject, request.Code,
	); err != nil {
		if isTwoFactorDecisionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) getTwoFactorPolicy(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	policy, err := h.options.Store.TwoFactorPolicy(r.Context(), query, resolved.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"two_factor_policy": policy})
}

func (h *HTTPHandler) updateTwoFactorPolicy(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Policy string `json:"policy"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	policy, err := h.options.Store.SetTwoFactorPolicy(
		r.Context(), query, resolved.ID, claims.Subject, request.Policy,
	)
	if err != nil {
		if isTwoFactorDecisionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		} else {
			h.options.Logger.ErrorContext(
				r.Context(), "two-factor policy update failed", "error", err, "tenant_id", resolved.ID,
			)
		}
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"two_factor_policy": policy})
}

func (h *HTTPHandler) twoFactorCompliance(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	entries, err := h.options.Store.TwoFactorCompliance(r.Context(), query, resolved.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"two_factor_compliance": entries})
}

func (h *HTTPHandler) resetMemberTwoFactor(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if err := h.options.Store.ResetMemberTwoFactor(
		r.Context(), query, resolved.ID, claims.Subject, chi.URLParam(r, "memberID"),
	); err != nil {
		if isTwoFactorDecisionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// allowTwoFactorAttempt applies one rate-limit window and writes the response
// when the caller has exhausted it.
func (h *HTTPHandler) allowTwoFactorAttempt(
	w http.ResponseWriter,
	r *http.Request,
	key string,
	window ratelimit.Window,
) bool {
	if h.options.Limiter == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return false
	}
	decision, err := h.options.Limiter.Allow(r.Context(), "two-factor:"+key, window)
	if err != nil {
		h.options.Logger.ErrorContext(r.Context(), "two-factor limiter unavailable", "error", err)
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return false
	}
	if decision.Allowed {
		return true
	}
	retrySeconds := max(1, int(math.Ceil(decision.RetryAfter.Seconds())))
	w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
	api.WriteError(w, http.StatusTooManyRequests, api.CodeRateLimitExceeded, "rate limit exceeded", nil)
	return false
}

// isTwoFactorDecisionError marks the outcomes that are deliberate answers
// rather than failures, so their audit rows survive the response.
func isTwoFactorDecisionError(err error) bool {
	return errors.Is(err, ErrTwoFactorCodeInvalid) ||
		errors.Is(err, ErrTwoFactorNotEnrolled) ||
		errors.Is(err, ErrTwoFactorAlreadyEnrolled) ||
		errors.Is(err, ErrTwoFactorChallengeNotPending) ||
		errors.Is(err, ErrTwoFactorPolicyBlocked) ||
		errors.Is(err, ErrTwoFactorManagedByIdP) ||
		errors.Is(err, ErrTwoFactorPolicyInvalid) ||
		errors.Is(err, ErrRecentAuthenticationRequired) ||
		errors.Is(err, ErrMemberActorInvalid) ||
		errors.Is(err, ErrMemberNotFound)
}
