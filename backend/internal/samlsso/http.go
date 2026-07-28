package samlsso

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
)

// maxACSBodyBytes bounds a posted SAML response. Signed assertions from real
// identity providers are tens of kilobytes at most.
const maxACSBodyBytes = 1 << 20

type HTTPOptions struct {
	Service Service
	Store   Store
	JWT     *auth.JWTManager
	Logger  *slog.Logger
}

type HTTPHandler struct{ options HTTPOptions }

func RegisterRoutes(router chi.Router, options HTTPOptions) {
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	handler := &HTTPHandler{options: options}

	// Unauthenticated: the identity provider and the login page reach these
	// before any session exists.
	router.Get(MetadataPath, handler.metadata)
	router.Get(StatusPath, handler.status)
	router.Get(StartPath, handler.start)
	router.Post(ACSPath, handler.assertionConsumer)

	// Configuration is restricted to organization administrators, the same
	// tier that already manages members, billing, and privacy settings.
	protected := []func(http.Handler) http.Handler{
		appmiddleware.RequireActive,
		appmiddleware.RequireTenantMember(options.JWT),
	}
	router.With(append(protected, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Get("/v1/sso/saml", handler.show)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Put("/v1/sso/saml", handler.configure)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Patch("/v1/sso/saml/mode", handler.setMode)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Delete("/v1/sso/saml", handler.remove)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Post("/v1/sso/saml/certificates", handler.addCertificate)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Delete("/v1/sso/saml/certificates/{certificateID}", handler.removeCertificate)
	router.With(append(protected, appmiddleware.RequireWritable, appmiddleware.RequirePermission(auth.PermissionTenantUpdate))...).
		Post("/v1/sso/saml/test", handler.startTest)
}

func (h *HTTPHandler) metadata(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	document, err := h.options.Service.Metadata(r.Context(), query, resolved)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(document)
}

// status lets the login page discover an organization's policy without
// revealing anything about who belongs to it.
func (h *HTTPHandler) status(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	policy, err := h.options.Service.Policy(r.Context(), query, resolved)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	api.WriteJSON(w, http.StatusOK, policy)
}

func (h *HTTPHandler) start(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	redirectURL, err := h.options.Service.StartLogin(
		r.Context(), query, resolved, PurposeLogin, r.URL.Query().Get("return_to"), "",
	)
	if err != nil {
		reason := "start_failed"
		if errors.Is(err, tenant.ErrInactive) {
			reason = "sso_unavailable"
		}
		h.redirectFailure(w, r, resolved, reason, err)
		return
	}
	h.redirect(w, r, redirectURL)
}

func (h *HTTPHandler) startTest(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	redirectURL, err := h.options.Service.StartLogin(
		r.Context(), query, resolved, PurposeTest, "", claims.Subject,
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if !h.audit(w, r, query, resolved.ID, auditEvent{
		Kind: "sso.test_started", Action: "test", Outcome: accessaudit.OutcomeSuccess,
		Actor: accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: claims.Subject},
	}) {
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"start_url": redirectURL})
}

// assertionConsumer receives the identity provider's POST. Every failure ends
// as a redirect back to the login page rather than a JSON error, because the
// caller here is a browser mid-navigation, not an API client. The specific
// cause is audited and logged but never shown, so a probe cannot use the
// response to learn why an assertion was rejected.
func (h *HTTPHandler) assertionConsumer(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxACSBodyBytes)
	if err := r.ParseForm(); err != nil {
		h.redirectFailure(w, r, resolved, "malformed_response", err)
		return
	}
	encodedResponse := r.PostFormValue("SAMLResponse")
	if encodedResponse == "" {
		h.redirectFailure(w, r, resolved, "missing_response", errors.New("SAMLResponse is absent"))
		return
	}
	outcome, err := h.options.Service.ConsumeAssertion(r.Context(), query, resolved, encodedResponse)
	if err != nil {
		h.recordAssertionFailure(w, r, query, resolved, err)
		return
	}
	event := auditEvent{
		Kind: "sso.login_succeeded", Action: "login", Outcome: accessaudit.OutcomeSuccess,
		Actor: accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: outcome.MemberID},
		Data:  map[string]any{"idp_entity_id": outcome.IdPEntityID},
	}
	if outcome.Purpose == PurposeTest {
		event = auditEvent{
			Kind: "sso.test_succeeded", Action: "test", Outcome: accessaudit.OutcomeSuccess,
			Actor: accessaudit.Actor{Type: accessaudit.ActorSystem},
			Data:  map[string]any{"idp_entity_id": outcome.IdPEntityID},
		}
	}
	if !h.audit(w, r, query, resolved.ID, event) {
		return
	}
	h.redirect(w, r, outcome.RedirectURL)
}

func (h *HTTPHandler) recordAssertionFailure(
	w http.ResponseWriter, r *http.Request, query db.Querier, resolved tenant.Tenant, err error,
) {
	reason := assertionFailureReason(err)
	if errors.Is(err, tenant.ErrInactive) {
		// ACS failures are redirects, which normally commit the tenant
		// transaction. Lifecycle rejection must instead discard any assertion
		// or correlation mutation performed before a late eligibility check.
		appmiddleware.RollbackTenantTransaction(r.Context())
	}
	// The failure is recorded before the redirect so a rejected assertion is
	// always auditable, including the replay and ineligible-member cases where
	// the surrounding transaction has already written correlation state.
	if auditErr := accessaudit.Record(r.Context(), query, accessaudit.RecordInput{
		TenantID: resolved.ID, Kind: "sso.login_failed",
		Actor:        accessaudit.Actor{Type: accessaudit.ActorSystem},
		ResourceType: accessaudit.ResourceSSOConfiguration, ResourceID: resolved.ID,
		Action: "login", Outcome: accessaudit.OutcomeDenied,
		Data: map[string]any{"reason": reason},
	}); auditErr != nil {
		h.options.Logger.ErrorContext(
			r.Context(), "SAML failure audit failed", "error", auditErr, "tenant_id", resolved.ID,
		)
	}
	h.redirectFailure(w, r, resolved, reason, err)
}

func (h *HTTPHandler) show(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	configuration, err := h.options.Store.Get(r.Context(), query, resolved.ID)
	if errors.Is(err, ErrNotConfigured) {
		descriptor, describeErr := h.options.Service.Endpoints.Descriptor(resolved.Slug, "")
		if describeErr != nil {
			h.writeError(w, r, describeErr)
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"configured": false, "sso": nil, "service_provider": descriptor,
		})
		return
	}
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeConfiguration(w, r, resolved, configuration)
}

type configureRequest struct {
	MetadataURL         string   `json:"idp_metadata_url"`
	MetadataXML         string   `json:"idp_metadata_xml"`
	IdPEntityID         string   `json:"idp_entity_id"`
	IdPSSOURL           string   `json:"idp_sso_url"`
	Certificates        []string `json:"certificates"`
	EmailDomains        []string `json:"email_domains"`
	AllowIDPInitiated   bool     `json:"allow_idp_initiated"`
	ReplaceCertificates bool     `json:"replace_certificates"`
}

func (h *HTTPHandler) configure(w http.ResponseWriter, r *http.Request) {
	var request configureRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	// configureRequest exists to carry the JSON tags and is otherwise identical
	// to ConfigurationInput, so a conversion says exactly that. If the two ever
	// diverge the conversion stops compiling, which is the point.
	configuration, err := h.options.Service.Configure(
		r.Context(), query, resolved, ConfigurationInput(request), claims.Subject,
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if !h.audit(w, r, query, resolved.ID, auditEvent{
		Kind: "sso.configured", Action: "configure", Outcome: accessaudit.OutcomeSuccess,
		Actor: accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: claims.Subject},
		Data: map[string]any{
			"idp_entity_id": configuration.IdPEntityID,
			"certificates":  len(configuration.Certificates),
			"tested":        configuration.Tested(),
		},
	}) {
		return
	}
	h.writeConfiguration(w, r, resolved, configuration)
}

func (h *HTTPHandler) setMode(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Mode string `json:"mode"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	configuration, err := h.options.Service.SetMode(
		r.Context(), query, resolved, auth.SSOMode(request.Mode), claims.Subject,
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if !h.audit(w, r, query, resolved.ID, auditEvent{
		Kind: "sso.mode_changed", Action: "set_mode", Outcome: accessaudit.OutcomeSuccess,
		Actor: accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: claims.Subject},
		Data:  map[string]any{"mode": string(configuration.Mode)},
	}) {
		return
	}
	h.writeConfiguration(w, r, resolved, configuration)
}

func (h *HTTPHandler) remove(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	if err := h.options.Store.Delete(r.Context(), query, resolved.ID); err != nil {
		h.writeError(w, r, err)
		return
	}
	if !h.audit(w, r, query, resolved.ID, auditEvent{
		Kind: "sso.disabled", Action: "disable", Outcome: accessaudit.OutcomeSuccess,
		Actor: accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: claims.Subject},
	}) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) addCertificate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Certificate string `json:"certificate"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	certificate, err := ParseCertificate(request.Certificate, time.Now())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if _, err := h.options.Store.Get(r.Context(), query, resolved.ID); err != nil {
		h.writeError(w, r, err)
		return
	}
	if err := h.options.Store.AddCertificate(r.Context(), query, resolved.ID, certificate); err != nil {
		h.writeError(w, r, err)
		return
	}
	if !h.audit(w, r, query, resolved.ID, auditEvent{
		Kind: "sso.certificate_added", Action: "rotate_certificate",
		Outcome: accessaudit.OutcomeSuccess,
		Actor:   accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: claims.Subject},
		Data: map[string]any{
			"fingerprint": certificate.Fingerprint,
			"not_after":   certificate.NotAfter.UTC().Format(time.RFC3339),
		},
	}) {
		return
	}
	configuration, err := h.options.Store.Get(r.Context(), query, resolved.ID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeConfiguration(w, r, resolved, configuration)
}

func (h *HTTPHandler) removeCertificate(w http.ResponseWriter, r *http.Request) {
	resolved, query, ok := requestState(w, r)
	if !ok {
		return
	}
	claims, _ := appmiddleware.TenantClaimsFromContext(r.Context())
	certificateID := chi.URLParam(r, "certificateID")
	configuration, err := h.options.Service.RemoveCertificate(
		r.Context(), query, resolved, certificateID, claims.Subject,
	)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if !h.audit(w, r, query, resolved.ID, auditEvent{
		Kind: "sso.certificate_removed", Action: "rotate_certificate",
		Outcome: accessaudit.OutcomeSuccess,
		Actor:   accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: claims.Subject},
		Data:    map[string]any{"mode": string(configuration.Mode)},
	}) {
		return
	}
	h.writeConfiguration(w, r, resolved, configuration)
}

func (h *HTTPHandler) writeConfiguration(
	w http.ResponseWriter, r *http.Request, resolved tenant.Tenant, configuration Configuration,
) {
	descriptor, err := h.options.Service.Describe(configuration, resolved)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	now := time.Now()
	warnings := make([]string, 0, 2)
	if !configuration.HasValidCertificate(now) {
		warnings = append(warnings, "no_valid_certificate")
	}
	if len(configuration.ExpiringCertificates(now, CertificateExpiryWarning)) > 0 {
		warnings = append(warnings, "certificate_expiring")
	}
	if configuration.Mode == auth.SSOModeRequired {
		// Surfaced so the settings UI can explain that MFA is the identity
		// provider's responsibility while Required mode is active.
		warnings = append(warnings, "mfa_managed_by_idp")
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"configured": true, "sso": configuration, "service_provider": descriptor,
		"can_require": configuration.CanRequire(now) == nil, "warnings": warnings,
	})
}

type auditEvent struct {
	Kind    string
	Action  string
	Outcome string
	Actor   accessaudit.Actor
	Data    map[string]any
}

// audit records an SSO lifecycle event in the same transaction as the change it
// describes, so a configuration change without a matching audit row is not a
// reachable state. A failure to record fails the request closed.
func (h *HTTPHandler) audit(
	w http.ResponseWriter, r *http.Request, query db.Querier, tenantID string, event auditEvent,
) bool {
	err := accessaudit.Record(r.Context(), query, accessaudit.RecordInput{
		TenantID: tenantID, Kind: event.Kind, Actor: event.Actor,
		ResourceType: accessaudit.ResourceSSOConfiguration, ResourceID: tenantID,
		Action: event.Action, PermissionPath: string(auth.PermissionTenantUpdate),
		Outcome: event.Outcome, Data: event.Data,
	})
	if err == nil {
		return true
	}
	h.options.Logger.ErrorContext(
		r.Context(), "SSO audit failed", "error", err, "tenant_id", tenantID, "kind", event.Kind,
	)
	api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
	return false
}

func (h *HTTPHandler) redirect(w http.ResponseWriter, r *http.Request, target string) {
	w.Header().Set("Cache-Control", "no-store")
	// 303 turns the identity provider's POST into a GET, which is what the
	// landing page expects.
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// redirectFailure sends the browser back to the login page with a stable,
// non-revealing reason code. The underlying cause is logged, never rendered.
func (h *HTTPHandler) redirectFailure(
	w http.ResponseWriter, r *http.Request, resolved tenant.Tenant, reason string, cause error,
) {
	h.options.Logger.WarnContext(
		r.Context(), "SAML sign-in rejected",
		"tenant_id", resolved.ID, "reason", reason, "error", cause,
	)
	target, err := tenant.PublicURL(
		h.options.Service.Endpoints.WebBaseURL, h.options.Service.Endpoints.BaseDomain,
		resolved.Slug, "/login",
	)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeUnauthenticated, "single sign-on failed", nil)
		return
	}
	target.RawQuery = url.Values{"sso_error": []string{reason}}.Encode()
	h.redirect(w, r, target.String())
}

// assertionFailureReason maps a rejection to a stable code the login page can
// translate. Codes are deliberately coarse: they must not let a caller
// distinguish "wrong signature" from "unknown member".
func assertionFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrAssertionReplayed):
		return "assertion_replayed"
	case errors.Is(err, ErrRequestNotFound):
		return "request_not_correlated"
	case errors.Is(err, ErrMemberNotEligible):
		return "member_not_eligible"
	case errors.Is(err, ErrNotConfigured), errors.Is(err, ErrSSODisabled),
		errors.Is(err, tenant.ErrInactive):
		return "sso_unavailable"
	case errors.Is(err, ErrAssertionInvalid):
		return "assertion_invalid"
	default:
		return "sso_failed"
	}
}

func (h *HTTPHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validation ValidationError
	switch {
	case errors.As(err, &validation):
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation,
			"SAML configuration is invalid", map[string]any{"fields": validation.Fields})
	case errors.Is(err, tenant.ErrInactive):
		api.WriteError(w, http.StatusForbidden, api.CodeTenantSuspended, "tenant is not active", nil)
	case errors.Is(err, ErrNotConfigured):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound,
			"SAML SSO is not configured for this workspace", nil)
	case errors.Is(err, ErrSSODisabled):
		api.WriteError(w, http.StatusConflict, api.CodeConflict,
			"SAML SSO is disabled for this workspace", nil)
	case errors.Is(err, ErrTestRequired):
		api.WriteError(w, http.StatusConflict, api.CodeSSONotTested,
			"a successful test login is required before enabling required mode", nil)
	case errors.Is(err, ErrCertificateRequired):
		api.WriteError(w, http.StatusConflict, api.CodeSSOConfiguration,
			"a currently valid signing certificate is required", nil)
	case errors.Is(err, ErrLastCertificate):
		api.WriteError(w, http.StatusConflict, api.CodeSSOConfiguration,
			"the last signing certificate cannot be removed while SSO is enabled", nil)
	case errors.Is(err, ErrCertificateNotFound):
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "signing certificate not found", nil)
	case errors.Is(err, ErrMemberNotEligible), errors.Is(err, ErrAssertionInvalid),
		errors.Is(err, ErrAssertionReplayed), errors.Is(err, ErrRequestNotFound):
		api.WriteError(w, http.StatusUnauthorized, api.CodeUnauthenticated,
			"single sign-on failed", nil)
	default:
		h.options.Logger.ErrorContext(r.Context(), "SAML request failed", "error", err)
		api.WriteError(w, http.StatusInternalServerError, api.CodeInternal, "internal server error", nil)
	}
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

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	return true
}
