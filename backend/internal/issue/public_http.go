package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	svix "github.com/svix/svix-webhooks/go"
)

type WebhookVerifier interface {
	Verify([]byte, http.Header) error
}

type PublicHTTPOptions struct {
	Store             Store
	Tokens            *auth.JWTManager
	Verifier          WebhookVerifier
	Begin             db.BeginFunc
	Limiter           ratelimit.Limiter
	TrustedProxyCIDRs []netip.Prefix
}

type PublicHTTPHandler struct {
	options PublicHTTPOptions
}

func NewWebhookVerifier(secret string) (WebhookVerifier, error) {
	verifier, err := svix.NewWebhook(secret)
	if err != nil {
		return nil, fmt.Errorf("initialize Resend webhook verifier: %w", err)
	}
	return verifier, nil
}

func RegisterPublicRoutes(router chi.Router, options PublicHTTPOptions) {
	handler := &PublicHTTPHandler{options: options}
	router.Get("/v1/reporter/unsubscribe", handler.unsubscribe)
	RegisterReporterProgressRoutes(router, options)
	router.Post("/v1/webhooks/resend", handler.resendWebhook)
}

func RegisterReporterProgressRoutes(router chi.Router, options PublicHTTPOptions) {
	handler := &PublicHTTPHandler{options: options}
	router.Get("/v1/reporter/progress/{token}", handler.reporterProgress)
	router.Post("/v1/reporter/progress/{token}/response", handler.reporterResponse)
}

func (h *PublicHTTPHandler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("token"))
	if raw == "" || h.options.Tokens == nil {
		writeUnsubscribeError(w)
		return
	}
	claims, err := h.options.Tokens.Verify(raw, auth.AudienceReporterUnsubscribe)
	if err != nil || claims.Subject == "" || claims.TenantID == "" {
		writeUnsubscribeError(w)
		return
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		writeUnsubscribeError(w)
		return
	}
	feedbackToken := auth.IsFeedbackReporterUnsubscribe(claims)
	found, locale, err := h.options.Store.UnsubscribeReporter(
		r.Context(), claims.TenantID, claims.Subject, feedbackToken,
	)
	if err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	if !found {
		writeUnsubscribeError(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, unsubscribeConfirmationHTML(locale))
}

// unsubscribedCopy carries the bilingual confirmation copy for the unsubscribe landing page. It is
// selected by the consent locale recorded at opt-in time, defaulting to English when the resource
// predates locale capture or the locale is unsupported.
type unsubscribedCopy struct {
	lang, title, heading, body string
}

var unsubscribedLocales = map[string]unsubscribedCopy{
	"en": {lang: "en", title: "Unsubscribed", heading: "Unsubscribed",
		body: "You will no longer receive updates for this feedback."},
	"pt": {lang: "pt", title: "Subscrição cancelada", heading: "Subscrição cancelada",
		body: "Deixará de receber atualizações sobre este feedback."},
}

func unsubscribeConfirmationHTML(locale *string) string {
	selected := unsubscribedLocales["en"]
	if locale != nil {
		if match, ok := unsubscribedLocales[strings.ToLower((*locale)[:min(2, len(*locale))])]; ok {
			selected = match
		}
	}
	return `<!doctype html><html lang="` + selected.lang + `"><meta charset="utf-8"><title>` + selected.title +
		`</title><body><main><h1>` + selected.heading + `</h1><p>` + selected.body + `</p></main></body></html>`
}

func (h *PublicHTTPHandler) resendWebhook(w http.ResponseWriter, r *http.Request) {
	if h.options.Verifier == nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "webhook is unavailable", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return
	}
	if err := h.options.Verifier.Verify(payload, r.Header); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "webhook signature is invalid", nil)
		return
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			EmailID string   `json:"email_id"`
			To      []string `json:"to"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &event); err != nil || event.Data.EmailID == "" {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "webhook payload is invalid", nil)
		return
	}
	if event.Type != "email.bounced" && event.Type != "email.complained" {
		api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	if err := h.options.Store.ApplyResendEvent(
		r.Context(), event.Data.EmailID, event.Type, r.Header.Get("svix-id"),
	); err != nil {
		api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func writeUnsubscribeError(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "unsubscribe link is invalid or expired", nil)
}

// UnsubscribeReporter withdraws consent for the target feedback/issue (and, for an issue-scoped
// token, every feedback linked to it), leaving the underlying feedback intact. It is idempotent:
// unsubscribing an already-withdrawn resource still matches and returns found=true. The returned
// locale (the locale recorded at opt-in, if any) drives the bilingual confirmation page.
func (s Store) UnsubscribeReporter(
	ctx context.Context,
	tenantID string,
	resourceID string,
	feedbackResource bool,
) (bool, *string, error) {
	var found bool
	var locale *string
	if feedbackResource {
		err := s.Database.QueryRow(ctx, `
			UPDATE feedbacks SET notify_reporter = false
			WHERE tenant_id = $1 AND id = $2
			RETURNING true, reporter_consent_locale
		`, tenantID, resourceID).Scan(&found, &locale)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, nil
		}
		if err != nil {
			return false, nil, fmt.Errorf("unsubscribe feedback reporter: %w", err)
		}
		return found, locale, nil
	}
	err := s.Database.QueryRow(ctx, `
		WITH target AS (
			UPDATE issues SET notify_reporter = false
			WHERE tenant_id = $1 AND id = $2
			RETURNING tenant_id, id, reporter_consent_locale
		), feedback AS (
			UPDATE feedbacks AS feedback SET notify_reporter = false
			FROM target
			WHERE feedback.tenant_id = target.tenant_id AND feedback.issue_id = target.id
		)
		SELECT EXISTS (SELECT 1 FROM target), (SELECT reporter_consent_locale FROM target)
	`, tenantID, resourceID).Scan(&found, &locale)
	if err != nil {
		return false, nil, fmt.Errorf("unsubscribe reporter: %w", err)
	}
	return found, locale, nil
}

func (s Store) ApplyResendEvent(
	ctx context.Context,
	providerMessageID string,
	eventType string,
	webhookEventID string,
) error {
	if eventType != "email.bounced" && eventType != "email.complained" {
		return errors.New("unsupported Resend event type")
	}
	if _, err := s.Database.Exec(ctx, `
		WITH matched AS (
			UPDATE delivery_logs
			SET status = CASE
			        WHEN $2 = 'email.bounced' THEN 'bounced'::delivery_status
			        ELSE status
			    END,
			    webhook_event_id = NULLIF($3, '')
			WHERE provider_message_id = $1
			RETURNING tenant_id, resource_type, resource_id
		), direct_feedback_unsubscribed AS (
			UPDATE feedbacks AS feedback SET notify_reporter = false
			FROM matched
			WHERE matched.resource_type = 'feedback'
			  AND feedback.tenant_id = matched.tenant_id AND feedback.id = matched.resource_id
		), issues_unsubscribed AS (
			UPDATE issues AS issue SET notify_reporter = false
			FROM matched
			WHERE matched.resource_type = 'issue'
			  AND issue.tenant_id = matched.tenant_id AND issue.id = matched.resource_id
			RETURNING issue.tenant_id, issue.id
		)
		UPDATE feedbacks AS feedback SET notify_reporter = false
		FROM issues_unsubscribed AS issue
		WHERE feedback.tenant_id = issue.tenant_id AND feedback.issue_id = issue.id
	`, providerMessageID, eventType, webhookEventID); err != nil {
		return fmt.Errorf("apply Resend delivery event: %w", err)
	}
	return nil
}
