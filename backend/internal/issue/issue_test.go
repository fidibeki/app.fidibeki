package issue

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/activity"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/moderation"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	svix "github.com/svix/svix-webhooks/go"
)

func TestStatusAndReporterDestination(t *testing.T) {
	t.Parallel()
	for _, status := range []Status{StatusOpen, StatusInProgress, StatusNeedsInfo, StatusResolved, StatusClosed} {
		if !status.Valid() {
			t.Errorf("Status(%q).Valid() = false", status)
		}
	}
	if Status("invalid").Valid() {
		t.Error("Status(invalid).Valid() = true")
	}
	event := outbox.Event{
		ID: "event", Event: "issue.updated", ResourceType: "issue", ResourceID: "issue",
	}
	destinations, err := (OutboxResolver{}).Resolve(context.Background(), event)
	if err != nil || len(destinations) != 1 {
		t.Fatalf("Resolve() = %#v, %v", destinations, err)
	}
	if destinations[0].TaskType != TaskReporterNotify || destinations[0].Queue != email.QueueName {
		t.Errorf("destination = %#v", destinations[0])
	}
	event.Event = "feedback.created"
	event.ResourceType = "feedback"
	if destinations, err := (OutboxResolver{}).Resolve(context.Background(), event); err != nil || len(destinations) != 1 {
		t.Errorf("Resolve(feedback receipt) = %#v, %v", destinations, err)
	}
	event.Event = "feedback.updated"
	if destinations, err := (OutboxResolver{}).Resolve(context.Background(), event); err != nil || len(destinations) != 0 {
		t.Errorf("Resolve(unrelated) = %#v, %v", destinations, err)
	}
	event.ResourceType = "issue"
	event.Event = "issue.created"
	if destinations, err := (OutboxResolver{}).Resolve(context.Background(), event); err != nil || len(destinations) != 1 {
		t.Errorf("Resolve(issue created) = %#v, %v", destinations, err)
	}
}

func TestModerateIssueInputOnlyReplacesCopiedReporterContent(t *testing.T) {
	t.Parallel()
	raw := "an abusive raw report"
	cleaned := "cleaned wording"
	suggested := "Checkout feedback needs review"
	states := []feedbackIssueState{{
		Description: &raw, Text: &raw, TextCleaned: &cleaned,
		AISuggestedTitle: &suggested, Moderated: true,
	}}
	copied := CreateFromFeedbackInput{Title: "an abusive", Description: "FB-1: " + raw}
	moderateIssueInput(&copied, states)
	if copied.Title != suggested || copied.Description != "FB-1: [MODERATED]" {
		t.Errorf("copied issue input = %#v", copied)
	}
	independent := CreateFromFeedbackInput{Title: "Safe title", Description: "A safe operator summary"}
	moderateIssueInput(&independent, states)
	if independent.Title != "Safe title" || independent.Description != "A safe operator summary" {
		t.Errorf("independent issue input = %#v", independent)
	}
}

func TestModerateIssueActivitiesRedactsHistoricalChanges(t *testing.T) {
	t.Parallel()
	raw := "an abusive raw report with more detail"
	events := []activity.Event{{Data: map[string]any{
		"changes": map[string]any{
			"title":       map[string]any{"from": "an abusive raw report", "to": "Safe title"},
			"description": map[string]any{"from": "FB-1: " + raw, "to": "Safe summary"},
		},
	}}}
	moderateIssueActivities(events, Issue{}, []string{raw})
	changes := events[0].Data["changes"].(map[string]any)
	if changes["title"].(map[string]any)["from"] != moderation.Placeholder ||
		changes["description"].(map[string]any)["from"] != "FB-1: "+moderation.Placeholder ||
		changes["title"].(map[string]any)["to"] != "Safe title" {
		t.Fatalf("moderated activity changes = %#v", changes)
	}
}

func TestReporterMessageEscapesContentAndUsesStableIdentity(t *testing.T) {
	t.Parallel()
	message, err := reporterMessage(ReporterNotification{
		OutboxEventID: "event-id", TenantName: `Acme <script>`, PublicID: "ACME-2026-0001",
		TenantSlug: "acme",
		Title:      `<img src=x onerror=alert(1)>`, Status: StatusInProgress,
		ReporterEmail: "reporter@example.com",
	}, "https://api.fidibeki.com/", "fidibeki.com", "portal-token", "signed token")
	if err != nil {
		t.Fatalf("reporterMessage() error = %v", err)
	}
	if message.IdempotencyKey != "reporter:event-id" || message.To != "reporter@example.com" {
		t.Errorf("message identity = %#v", message)
	}
	if strings.Contains(message.HTML, "<script>") || strings.Contains(message.HTML, "<img src=x") {
		t.Errorf("HTML was not escaped: %s", message.HTML)
	}
	if !strings.Contains(message.Text, "in progress") ||
		!strings.Contains(message.Text, "token=signed+token") ||
		!strings.Contains(message.Text, "https://acme.fidibeki.com/reporter/portal-token") {
		t.Errorf("message text = %q", message.Text)
	}
	if !strings.Contains(message.From, "Acme <script> via Fidibeki") ||
		!strings.Contains(message.From, "updates@fidibeki.com") {
		t.Errorf("message from = %q", message.From)
	}
}

func TestPublicWebhookRejectsInvalidSignatureBeforeDatabaseAccess(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	RegisterPublicRoutes(router, PublicHTTPOptions{Verifier: verifierStub{err: errors.New("invalid")}})
	request := httptest.NewRequest(http.MethodPost, "/v1/webhooks/resend", strings.NewReader(`{"type":"email.bounced","data":{"email_id":"email"}}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestUnsubscribeConfirmationIsBilingual(t *testing.T) {
	t.Parallel()
	en := "en"
	ptPT := "pt-PT"
	fr := "fr"
	cases := []struct {
		name         string
		locale       *string
		wantLang     string
		wantFragment string
	}{
		{name: "english locale", locale: &en, wantLang: "en", wantFragment: "Unsubscribed"},
		{
			name: "portuguese regional locale falls back to the pt language", locale: &ptPT,
			wantLang: "pt", wantFragment: "Subscrição cancelada",
		},
		{name: "unsupported locale defaults to english", locale: &fr, wantLang: "en", wantFragment: "Unsubscribed"},
		{name: "missing locale defaults to english", locale: nil, wantLang: "en", wantFragment: "Unsubscribed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			html := unsubscribeConfirmationHTML(testCase.locale)
			if !strings.Contains(html, `lang="`+testCase.wantLang+`"`) {
				t.Errorf("html lang = %q, want %q in %s", testCase.wantLang, testCase.wantLang, html)
			}
			if !strings.Contains(html, testCase.wantFragment) {
				t.Errorf("html = %q, want fragment %q", html, testCase.wantFragment)
			}
		})
	}
}

func TestPublicRoutesRejectInvalidInputsAndIgnoreUntrackedEvents(t *testing.T) {
	t.Parallel()
	router := chi.NewRouter()
	RegisterPublicRoutes(router, PublicHTTPOptions{
		Tokens: newTestJWT(t, 90*24*time.Hour), Verifier: verifierStub{},
	})

	invalidUnsubscribe := httptest.NewRecorder()
	router.ServeHTTP(invalidUnsubscribe, httptest.NewRequest(
		http.MethodGet, "/v1/reporter/unsubscribe?token=invalid", nil,
	))
	if invalidUnsubscribe.Code != http.StatusBadRequest {
		t.Errorf("invalid unsubscribe status = %d", invalidUnsubscribe.Code)
	}

	invalidPayload := httptest.NewRecorder()
	router.ServeHTTP(invalidPayload, httptest.NewRequest(
		http.MethodPost, "/v1/webhooks/resend", strings.NewReader(`{"type":`),
	))
	if invalidPayload.Code != http.StatusBadRequest {
		t.Errorf("invalid webhook payload status = %d", invalidPayload.Code)
	}

	ignored := httptest.NewRecorder()
	router.ServeHTTP(ignored, httptest.NewRequest(
		http.MethodPost, "/v1/webhooks/resend",
		strings.NewReader(`{"type":"email.delivered","data":{"email_id":"message"}}`),
	))
	if ignored.Code != http.StatusOK || !strings.Contains(ignored.Body.String(), "ignored") {
		t.Errorf("ignored webhook status = %d, body = %s", ignored.Code, ignored.Body.String())
	}

	unavailable := chi.NewRouter()
	RegisterPublicRoutes(unavailable, PublicHTTPOptions{})
	response := httptest.NewRecorder()
	unavailable.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost, "/v1/webhooks/resend", strings.NewReader(`{}`),
	))
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("unavailable webhook status = %d", response.Code)
	}

	if err := (Store{}).ApplyResendEvent(context.Background(), "message", "email.delivered", "event"); err == nil {
		t.Error("ApplyResendEvent(unsupported) error = nil")
	}
}

func TestReporterHandlerRejectsMalformedTaskPermanently(t *testing.T) {
	t.Parallel()
	err := (ReporterHandler{}).Handler()(context.Background(), asynq.NewTask(TaskReporterNotify, []byte(`{`)))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("error = %v, want SkipRetry", err)
	}
}

func TestNewWebhookVerifierRejectsInvalidSecret(t *testing.T) {
	t.Parallel()
	if _, err := NewWebhookVerifier(""); err == nil {
		t.Error("NewWebhookVerifier(empty) error = nil")
	}
}

func TestOfficialWebhookVerifierAcceptsSignedRawPayload(t *testing.T) {
	t.Parallel()
	const secret = "whsec_c2VjcmV0LXNlY3JldA=="
	verifier, err := NewWebhookVerifier(secret)
	if err != nil {
		t.Fatalf("NewWebhookVerifier() error = %v", err)
	}
	signer, err := svix.NewWebhook(secret)
	if err != nil {
		t.Fatalf("svix.NewWebhook() error = %v", err)
	}
	payload := []byte(`{"type":"email.delivered","data":{"email_id":"email-id"}}`)
	now := time.Now()
	signature, err := signer.Sign("webhook-id", now, payload)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	headers := http.Header{}
	headers.Set("svix-id", "webhook-id")
	headers.Set("svix-timestamp", strconv.FormatInt(now.Unix(), 10))
	headers.Set("svix-signature", signature)
	if err := verifier.Verify(payload, headers); err != nil {
		t.Errorf("Verify() error = %v", err)
	}
}

type verifierStub struct {
	err error
}

func (v verifierStub) Verify([]byte, http.Header) error { return v.err }

type senderStub struct {
	messages []email.Message
	err      error
}

func (s *senderStub) Send(_ context.Context, message email.Message) (string, error) {
	s.messages = append(s.messages, message)
	if s.err != nil {
		return "", s.err
	}
	return "message-" + time.Now().Format("150405.000000000"), nil
}

func newTestJWT(t *testing.T, ttl time.Duration) *auth.JWTManager {
	t.Helper()
	manager, err := auth.NewJWTManager("0123456789abcdef0123456789abcdef", "fidibeki", ttl)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	return manager
}
