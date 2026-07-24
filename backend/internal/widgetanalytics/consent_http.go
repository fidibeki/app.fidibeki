package widgetanalytics

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
)

type ConsentAuthorizer interface {
	RecordConsent(
		ctx context.Context, query db.Querier, tenantID string, projectID string, widgetRuleID string,
		noticeVersion string, locale string, source string, customerReference *string, tokenHash *string,
		state ConsentState, now time.Time, expiresAt *time.Time,
	) (Consent, error)
	AuthorizeConsent(
		ctx context.Context, query db.Querier, tenantID string, projectID string, widgetRuleID string,
		noticeVersion string, source string, tokenHash string, now time.Time,
	) (Consent, error)
	RevokeConsent(
		ctx context.Context, query db.Querier, tenantID string, projectID string, tokenHash string,
		now time.Time,
	) (Consent, error)
}

func (h *PublicHTTPHandler) recordConsent(w http.ResponseWriter, r *http.Request) {
	configuration, _, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var request struct {
		WidgetRuleID  string `json:"widget_rule_id"`
		Purpose       string `json:"purpose"`
		NoticeVersion string `json:"notice_version"`
		Locale        string `json:"locale"`
		Source        string `json:"source"`
		ConsentID     string `json:"consent_id"`
		State         string `json:"state"`
		GrantToken    string `json:"grant_token"`
	}
	if !decodeConsentJSON(w, r, &request) {
		return
	}
	rule := analyticsRule(configuration.Rules, strings.TrimSpace(request.WidgetRuleID))
	fields := make([]map[string]string, 0, 8)
	if rule == nil || !rule.Enabled || rule.AnalyticsConsent.Mode == project.AnalyticsConsentNone {
		fields = append(fields, consentField("widget_rule_id", "must identify an analytics-enabled consent rule"))
	}
	purpose := strings.TrimSpace(request.Purpose)
	if purpose != ConsentPurpose {
		fields = append(fields, consentField("purpose", "must be widget_analytics"))
	}
	noticeVersion := strings.TrimSpace(request.NoticeVersion)
	locale := strings.ToLower(strings.TrimSpace(request.Locale))
	source := strings.TrimSpace(request.Source)
	customerReference := strings.TrimSpace(request.ConsentID)
	state := strings.TrimSpace(request.State)
	grantToken := strings.TrimSpace(request.GrantToken)
	if rule != nil {
		if noticeVersion != rule.AnalyticsConsent.NoticeVersion {
			fields = append(fields, consentField("notice_version", "must match the current analytics notice"))
		}
		if source != string(rule.AnalyticsConsent.Mode) {
			fields = append(fields, consentField("source", "must match the configured analytics consent mode"))
		}
		if !supportedConsentLocale(rule.AnalyticsConsent.SupportedLocales, locale) {
			fields = append(fields, consentField("locale", "must be supported by the current analytics notice"))
		}
	}
	if source == string(project.AnalyticsConsentCustomerManaged) {
		if len(customerReference) < 1 || len(customerReference) > 200 {
			fields = append(fields, consentField("consent_id", "must contain between 1 and 200 characters"))
		}
	} else if customerReference != "" {
		fields = append(fields, consentField("consent_id", "must be empty for Fidibeki-managed consent"))
	}
	var consentState ConsentState
	switch state {
	case "granted":
		consentState = ConsentActive
		if !validAnalyticsConsentToken(grantToken) {
			fields = append(fields, consentField("grant_token", "must be a 32-byte opaque token"))
		}
	case "denied":
		consentState = ConsentDenied
		if grantToken != "" {
			fields = append(fields, consentField("grant_token", "must be empty when consent is denied"))
		}
	default:
		fields = append(fields, consentField("state", "must be granted or denied"))
	}
	if len(fields) > 0 {
		api.WriteError(w, http.StatusUnprocessableEntity, api.CodeValidation, "widget analytics consent is invalid", fields)
		return
	}
	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("begin widget analytics consent transaction: %w", err))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	var configuredTenant string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID).
		Scan(&configuredTenant); err != nil || configuredTenant != configuration.TenantID {
		h.writeStoreError(w, r, errors.New("configure widget analytics consent tenant context"))
		return
	}
	now := h.now()
	var tokenHash *string
	expiry := now.AddDate(0, 0, rule.AnalyticsConsent.ExpiryDays)
	expiresAt := &expiry
	if consentState == ConsentActive {
		hash := auth.HashToken(grantToken)
		tokenHash = &hash
	}
	var reference *string
	if customerReference != "" {
		reference = &customerReference
	}
	consent, err := h.options.Consent.RecordConsent(
		r.Context(), tx, configuration.TenantID, configuration.ProjectID, rule.ID,
		noticeVersion, locale, source, reference, tokenHash, consentState, now, expiresAt,
	)
	if errors.Is(err, ErrConsentRequired) {
		api.WriteError(w, http.StatusConflict, api.CodeConflict, "widget analytics consent token is already scoped elsewhere", nil)
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit widget analytics consent: %w", err))
		return
	}
	committed = true
	response := map[string]any{"state": consent.State}
	response["expires_at"] = consent.ExpiresAt
	if consent.State == ConsentActive {
		response["token"] = grantToken
	}
	api.WriteJSON(w, http.StatusCreated, response)
}

func (h *PublicHTTPHandler) revokeConsent(w http.ResponseWriter, r *http.Request) {
	configuration, _, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if !decodeConsentJSON(w, r, &request) {
		return
	}
	token := strings.TrimSpace(request.Token)
	if !validAnalyticsConsentToken(token) {
		api.WriteError(
			w, http.StatusUnprocessableEntity, api.CodeValidation, "widget analytics consent revocation is invalid",
			[]map[string]string{consentField("token", "must be a 32-byte opaque token")},
		)
		return
	}
	tx, err := h.options.Begin(r.Context())
	if err != nil {
		h.writeStoreError(w, r, fmt.Errorf("begin widget analytics consent revocation transaction: %w", err))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	var configuredTenant string
	if err := tx.QueryRow(r.Context(), `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID).
		Scan(&configuredTenant); err != nil || configuredTenant != configuration.TenantID {
		h.writeStoreError(w, r, errors.New("configure widget analytics consent revocation tenant context"))
		return
	}
	if _, err := h.options.Consent.RevokeConsent(
		r.Context(), tx, configuration.TenantID, configuration.ProjectID,
		auth.HashToken(token), h.now(),
	); errors.Is(err, ErrConsentRequired) {
		api.WriteError(w, http.StatusForbidden, api.CodeForbidden, "widget analytics consent revocation denied", nil)
		return
	} else if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		h.writeStoreError(w, r, fmt.Errorf("commit widget analytics consent revocation: %w", err))
		return
	}
	committed = true
	w.WriteHeader(http.StatusNoContent)
}

func analyticsRule(rules []project.WidgetRule, ruleID string) *project.WidgetRule {
	for index := range rules {
		if rules[index].ID == ruleID {
			return &rules[index]
		}
	}
	return nil
}

func supportedConsentLocale(locales []string, locale string) bool {
	for _, supported := range locales {
		if strings.EqualFold(strings.TrimSpace(supported), locale) {
			return true
		}
	}
	return false
}

func validAnalyticsConsentToken(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func consentField(field string, message string) map[string]string {
	return map[string]string{"field": field, "message": message}
}

func decodeConsentJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	return decodeIngestRequest(w, r, destination)
}
