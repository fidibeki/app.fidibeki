package tenantmember

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetconfig"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const tenantRefreshCookie = "fidibeki_tenant_refresh"

type HTTPOptions struct {
	Service       Service
	Store         Store
	JWT           *auth.JWTManager
	Email         email.Enqueuer
	SecureCookies bool
	RefreshTTL    time.Duration
	Logger        *slog.Logger
}

type HTTPHandler struct {
	options HTTPOptions
}

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{options: options}
	router.Post("/auth/login", handler.login)
	router.Post("/auth/verify", handler.verify)
	router.Post("/auth/member-domains/verify", handler.verifyMemberDomain)
	router.Post("/auth/refresh", handler.refresh)
	router.Post("/auth/logout", handler.logout)

	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionMemberInvite))...).
		Post("/auth/invite", handler.invite)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionMemberRead))...).
		Get("/v1/members", handler.listMembers)
	router.With(append(protected, appmiddleware.RequireWritable)...).
		Patch("/v1/profile", handler.updateProfile)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionTenantRead))...).
		Get("/v1/organization-profile", handler.getOrganizationProfile)
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionMemberRead))...).
		Get("/v1/member-domains", handler.listMemberDomains)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionMemberInvite))...).
		Post("/v1/member-domains", handler.requestMemberDomainVerification)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionMemberUpdate))...).
		Patch("/v1/members/{memberID}", handler.updateMember)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionMemberUpdate))...).
		Patch("/v1/members/{memberID}/status", handler.updateMemberStatus)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionMemberRemove))...).
		Delete("/v1/members/{memberID}", handler.removeMember)
	router.With(append(
		protected,
		appmiddleware.RequireWritable,
		appmiddleware.RequirePermission(auth.PermissionMemberUpdate),
		appmiddleware.RequirePermission(auth.PermissionTenantUpdate),
	)...).Post("/v1/ownership/transfer", handler.transferOwnership)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Patch("/v1/reporter-access", handler.updateReporterAccess)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Patch("/v1/widget-appearance", handler.updateWidgetAppearance)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Patch("/v1/organization-profile", handler.updateOrganizationProfile)
}

func (h *HTTPHandler) getOrganizationProfile(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	profile, err := h.options.Store.GetOrganizationProfile(r.Context(), query, resolved.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"organization_profile": profile})
}

func (h *HTTPHandler) updateOrganizationProfile(w http.ResponseWriter, r *http.Request) {
	var request tenant.OrganizationProfile
	if !decodeJSON(w, r, &request) {
		return
	}
	profile, fields := ValidateOrganizationProfile(request)
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "validation failed", fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	profile, err := h.options.Store.UpdateOrganizationProfile(
		r.Context(), query, resolved.ID, claims.Subject, profile,
	)
	if err != nil {
		h.options.Logger.ErrorContext(
			r.Context(), "organization profile update failed",
			"error", err, "tenant_id", resolved.ID, "actor_member_id", claims.Subject,
		)
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"organization_profile": profile})
}

func (h *HTTPHandler) updateWidgetAppearance(w http.ResponseWriter, r *http.Request) {
	var request widgetconfig.AppearanceOverrides
	if !decodeJSON(w, r, &request) {
		return
	}
	request = widgetconfig.Normalize(request)
	if fields := widgetconfig.Validate(request); len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "validation failed", fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if err := h.options.Store.UpdateWidgetAppearance(r.Context(), query, resolved.ID, request); err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"widget_appearance_defaults": request})
}

func (h *HTTPHandler) updateReporterAccess(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if err := h.options.Store.UpdateReporterAccess(r.Context(), query, resolved.ID, request.Enabled); err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]bool{"reporter_access_enabled": request.Enabled})
}

func (h *HTTPHandler) listMemberDomains(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	domains, err := h.options.Store.ListMemberDomains(r.Context(), query, resolved.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"member_domains": domains})
}

func (h *HTTPHandler) requestMemberDomainVerification(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	memberDomain, message, err := h.options.Service.RequestMemberDomainVerification(
		r.Context(), query, resolved, request.Email,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	if !appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
		return h.options.Email.Enqueue(ctx, *message)
	}) {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]any{"member_domain": memberDomain})
}

func (h *HTTPHandler) verifyMemberDomain(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	memberDomain, err := h.options.Service.VerifyMemberDomain(r.Context(), query, resolved.ID, request.Token)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"member_domain": memberDomain})
}

func (h *HTTPHandler) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	// Resolved before issuing the link so the break-glass use is recorded in the
	// same transaction that grants it: an owner sign-in that bypasses Required
	// SSO is never possible without a matching audit row.
	member, breakGlass, err := h.options.Service.BreakGlassLogin(r.Context(), query, resolved, request.Email)
	if err != nil {
		writeError(w, err)
		return
	}
	message, err := h.options.Service.RequestMagicLink(r.Context(), query, resolved, request.Email)
	if err != nil {
		writeError(w, err)
		return
	}
	if breakGlass {
		if err := accessaudit.Record(r.Context(), query, accessaudit.RecordInput{
			TenantID: resolved.ID, Kind: "sso.break_glass_login_requested",
			Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: member.ID},
			ResourceType: accessaudit.ResourceSSOConfiguration, ResourceID: resolved.ID,
			Action: "break_glass", Outcome: accessaudit.OutcomeSuccess,
			Data: map[string]any{"reason": "workspace_owner_recovery"},
		}); err != nil {
			h.options.Logger.ErrorContext(
				r.Context(), "SSO break-glass audit failed", "error", err, "tenant_id", resolved.ID,
			)
			api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
			return
		}
	}
	if message != nil && !appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
		return h.options.Email.Enqueue(ctx, *message)
	}) {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *HTTPHandler) invite(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Email       string   `json:"email"`
		Permissions []string `json:"permissions"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if !auth.ValidTenantPermissions(request.Permissions) || !permissionSubset(request.Permissions, claims.Permissions) {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "permissions are invalid", nil)
		return
	}
	definition, _ := config.PlanFor(resolved.Plan)
	count, err := h.options.Store.CountMembers(r.Context(), query, resolved.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if definition.Limits.Users >= 0 && count >= definition.Limits.Users {
		api.WriteError(w, http.StatusPaymentRequired, api.CodePlanLimitExceeded, "plan limit exceeded", map[string]any{
			"limit": config.LimitUsers, "maximum": definition.Limits.Users, "used": count, "upgrade_url": "/v1/billing",
		})
		return
	}
	message, err := h.options.Service.Invite(r.Context(), query, resolved, request.Email, request.Permissions)
	if err != nil {
		writeError(w, err)
		return
	}
	if !appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
		return h.options.Email.Enqueue(ctx, *message)
	}) {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *HTTPHandler) verify(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	tokens, err := h.options.Service.Verify(
		r.Context(), query, resolved.ID, request.Token, r.UserAgent(), remoteIP(r.RemoteAddr),
	)
	if err != nil {
		writeError(w, err)
		return
	}
	h.setCookie(w, tokens.RefreshToken)
	writeTokens(w, tokens, resolved)
}

func (h *HTTPHandler) refresh(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	cookie, err := r.Cookie(tenantRefreshCookie)
	if err != nil {
		writeError(w, ErrRefreshInvalid)
		return
	}
	tokens, err := h.options.Service.Refresh(
		r.Context(), query, resolved.ID, cookie.Value, r.UserAgent(), remoteIP(r.RemoteAddr),
	)
	if err != nil {
		if errors.Is(err, ErrRefreshExpired) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		if errors.Is(err, ErrRefreshReuse) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
			appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
				h.options.Logger.WarnContext(
					ctx, "tenant refresh token reuse detected; sessions revoked", "tenant_id", resolved.ID,
				)
				return nil
			})
		}
		h.clearCookie(w)
		writeError(w, err)
		return
	}
	h.setCookie(w, tokens.RefreshToken)
	writeTokens(w, tokens, resolved)
}

func (h *HTTPHandler) logout(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	if cookie, err := r.Cookie(tenantRefreshCookie); err == nil {
		if err := h.options.Service.Logout(r.Context(), query, resolved.ID, cookie.Value); err != nil {
			writeError(w, err)
			return
		}
	}
	h.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) listMembers(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	members, err := h.options.Store.ListMembers(r.Context(), query, resolved.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (h *HTTPHandler) updateProfile(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name     optionalStringPatch `json:"name"`
		Locale   optionalStringPatch `json:"locale"`
		Timezone optionalStringPatch `json:"timezone"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	patch := MemberProfilePatch{
		SetName: request.Name.Set, SetLocale: request.Locale.Set, SetTimezone: request.Timezone.Set,
	}
	fields := map[string]string{}
	if patch.SetName {
		patch.Name, fields["name"] = NormalizeMemberName(request.Name.Value)
	}
	if patch.SetLocale {
		patch.Locale, fields["locale"] = NormalizeMemberLocale(request.Locale.Value)
	}
	if patch.SetTimezone {
		patch.Timezone, fields["timezone"] = NormalizeMemberTimezone(request.Timezone.Value)
	}
	for field, message := range fields {
		if message == "" {
			delete(fields, field)
		}
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "validation failed", fields)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	member, err := h.options.Store.UpdateOwnProfile(
		r.Context(), query, resolved.ID, claims.Subject, patch,
	)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"member": member})
}

type optionalStringPatch struct {
	Set   bool
	Value *string
}

func (field *optionalStringPatch) UnmarshalJSON(data []byte) error {
	field.Set = true
	return json.Unmarshal(data, &field.Value)
}

func (h *HTTPHandler) updateMember(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Permissions []string `json:"permissions"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if !auth.ValidTenantPermissions(request.Permissions) || !permissionSubset(request.Permissions, claims.Permissions) {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "permissions are invalid", nil)
		return
	}
	member, err := h.options.Store.UpdatePermissions(
		r.Context(), query, resolved.ID, claims.Subject, chi.URLParam(r, "memberID"), request.Permissions,
	)
	if err != nil {
		if isMemberProtectionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		} else {
			h.options.Logger.ErrorContext(
				r.Context(), "tenant member permission update failed", "error", err, "tenant_id", resolved.ID,
			)
		}
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"member": member})
}

func (h *HTTPHandler) updateMemberStatus(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Status MemberStatus `json:"status"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Status != MemberStatusActive && request.Status != MemberStatusDeactivated {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "member status is invalid", nil)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	member, err := h.options.Store.UpdateMemberStatus(
		r.Context(), query, resolved.ID, claims.Subject, chi.URLParam(r, "memberID"), request.Status,
	)
	if err != nil {
		if isMemberProtectionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"member": member})
}

func (h *HTTPHandler) removeMember(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	member, err := h.options.Store.RemoveMember(
		r.Context(), query, resolved.ID, claims.Subject, chi.URLParam(r, "memberID"),
	)
	if err != nil {
		if isMemberProtectionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		}
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"member": member})
}

func (h *HTTPHandler) transferOwnership(w http.ResponseWriter, r *http.Request) {
	var request struct {
		NewOwnerMemberID string `json:"new_owner_member_id"`
		Confirmation     string `json:"confirmation"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if _, err := uuid.Parse(request.NewOwnerMemberID); err != nil || request.Confirmation == "" {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "ownership transfer is invalid", nil)
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, claimsOK := appmiddleware.TenantClaimsFromContext(r.Context())
	if !claimsOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return
	}
	owner, err := h.options.Store.TransferOwnership(
		r.Context(), query, resolved.ID, claims.Subject, request.NewOwnerMemberID, request.Confirmation,
	)
	if err != nil {
		if isOwnershipDecisionError(err) {
			appmiddleware.CommitTenantTransactionOnError(r.Context())
		} else {
			h.options.Logger.ErrorContext(
				r.Context(), "workspace ownership transfer failed", "error", err, "tenant_id", resolved.ID,
			)
		}
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"owner": owner})
}

func requestState(w http.ResponseWriter, r *http.Request) (tenant.Tenant, db.Querier, bool) {
	resolved, tenantOK := appmiddleware.TenantFromContext(r.Context())
	query, queryOK := appmiddleware.QuerierFromContext(r.Context())
	if !tenantOK || !queryOK {
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
		return tenant.Tenant{}, nil, false
	}
	return resolved, query, true
}

func permissionSubset(requested, granted []string) bool {
	allowed := make(map[string]struct{}, len(granted))
	for _, permission := range granted {
		allowed[permission] = struct{}{}
	}
	for _, permission := range requested {
		if _, exists := allowed[permission]; !exists {
			return false
		}
	}
	return true
}

func (h *HTTPHandler) setCookie(w http.ResponseWriter, value string) {
	ttl := h.options.RefreshTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	http.SetCookie(w, &http.Cookie{
		Name: tenantRefreshCookie, Value: value, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (h *HTTPHandler) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: tenantRefreshCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.options.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func writeTokens(w http.ResponseWriter, tokens SessionTokens, resolved tenant.Tenant) {
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"access_token": tokens.AccessToken, "token_type": "Bearer",
		"expires_in": int(time.Until(tokens.ExpiresAt).Seconds()), "member": tokens.Member,
		// SAML-authenticated sessions delegate multi-factor assurance to the
		// identity provider, so the client must be able to tell them apart
		// from local ones without inspecting the credential.
		"authentication": tokens.Identity,
		"tenant": map[string]any{
			"id": resolved.ID, "name": resolved.Name, "slug": resolved.Slug,
			"plan": resolved.Plan, "status": resolved.Status, "trial_ends_at": resolved.TrialEndsAt,
			"deletion_requested_at":   resolved.DeletionRequestedAt,
			"short_name":              resolved.ShortName,
			"website_url":             resolved.WebsiteURL,
			"support_email":           resolved.SupportEmail,
			"default_locale":          resolved.DefaultLocale,
			"default_timezone":        resolved.DefaultTimezone,
			"privacy_policy_url":      resolved.PrivacyPolicyURL,
			"privacy_contact_email":   resolved.PrivacyContactEmail,
			"privacy_rights_email":    resolved.PrivacyRightsEmail,
			"privacy_profile_version": resolved.PrivacyProfileVersion,
			"terms_url":               resolved.TermsURL,
			"logo_version":            resolved.LogoVersion,
		},
	})
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrMagicLinkInvalid), errors.Is(err, ErrRefreshInvalid),
		errors.Is(err, ErrRefreshExpired), errors.Is(err, ErrRefreshReuse):
		api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "authentication failed", nil)
	case errors.Is(err, ErrLocalLoginDisabled):
		api.WriteError(w, http.StatusForbidden, api.CodeSSORequired,
			"this workspace requires single sign-on", nil)
	case errors.Is(err, ErrMemberExists):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "member already exists", nil)
	case errors.Is(err, ErrMemberDomainAlreadyVerified):
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "member domain is already verified", nil)
	case errors.Is(err, ErrMemberDomainNotVerified):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "email domain is not verified for this workspace", nil)
	case errors.Is(err, ErrDomainVerificationInvalid):
		api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated, "domain verification failed", nil)
	case errors.Is(err, ErrMemberNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "member not found", nil)
	case errors.Is(err, ErrOwnerProtection):
		api.WriteError(w, http.StatusConflict, api.CodeOwnerProtected, "workspace owner is protected", nil)
	case errors.Is(err, ErrOwnershipConfirmation):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeOwnershipConfirmation, "workspace confirmation does not match", nil)
	case errors.Is(err, ErrRecentAuthenticationRequired):
		api.WriteError(w, http.StatusForbidden, api.CodeRecentAuthRequired, "recent authentication is required", nil)
	case errors.Is(err, ErrOwnershipTargetInvalid):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeOwnershipTarget, "new workspace owner is invalid", nil)
	case errors.Is(err, ErrLastAdministrator):
		api.WriteError(w, http.StatusConflict, api.CodeLastAdministrator, "last recoverable administrator is protected", nil)
	case errors.Is(err, ErrMemberAlreadyRemoved):
		api.WriteError(w, http.StatusConflict, api.CodeMemberAlreadyRemoved, "member is already removed", nil)
	case errors.Is(err, ErrMemberStatusInvalid):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "member status is invalid", nil)
	case errors.Is(err, ErrMemberActorInvalid):
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "active member authentication is required", nil)
	case strings.Contains(err.Error(), "email is invalid"):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "email is invalid", nil)
	default:
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
	}
}

func isMemberProtectionError(err error) bool {
	return errors.Is(err, ErrOwnerProtection) ||
		errors.Is(err, ErrLastAdministrator) ||
		errors.Is(err, ErrMemberAlreadyRemoved) ||
		errors.Is(err, ErrMemberActorInvalid)
}

func isOwnershipDecisionError(err error) bool {
	return errors.Is(err, ErrOwnerProtection) ||
		errors.Is(err, ErrOwnershipConfirmation) ||
		errors.Is(err, ErrRecentAuthenticationRequired) ||
		errors.Is(err, ErrOwnershipTargetInvalid)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
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

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	return host
}
