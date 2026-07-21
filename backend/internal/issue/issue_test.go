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

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	svix "github.com/svix/svix-webhooks/go"
)

func TestStatusAndReporterDestination(t *testing.T) {
	t.Parallel()
	for _, status := range []Status{StatusOpen, StatusInProgress, StatusResolved, StatusClosed} {
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
	if destinations, err := (OutboxResolver{}).Resolve(context.Background(), event); err != nil || len(destinations) != 0 {
		t.Errorf("Resolve(unrelated) = %#v, %v", destinations, err)
	}
}

func TestReporterMessageEscapesContentAndUsesStableIdentity(t *testing.T) {
	t.Parallel()
	message := reporterMessage(ReporterNotification{
		OutboxEventID: "event-id", TenantName: `Acme <script>`, PublicID: "ACME-2026-0001",
		Title: `<img src=x onerror=alert(1)>`, Status: StatusInProgress,
		ReporterEmail: "reporter@example.com",
	}, "https://api.fidibeki.com/", "signed token")
	if message.IdempotencyKey != "reporter:event-id" || message.To != "reporter@example.com" {
		t.Errorf("message identity = %#v", message)
	}
	if strings.Contains(message.HTML, "<script>") || strings.Contains(message.HTML, "<img src=x") {
		t.Errorf("HTML was not escaped: %s", message.HTML)
	}
	if !strings.Contains(message.Text, "in progress") ||
		!strings.Contains(message.Text, "token=signed+token") {
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
