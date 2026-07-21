package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

const testEncryptionKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestCipherRoundTripBindsAssociatedData(t *testing.T) {
	t.Parallel()
	cipher, err := NewCipher(testEncryptionKey)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	cipher.rand = bytes.NewReader(bytes.Repeat([]byte{7}, 64))
	ciphertext, err := cipher.Encrypt([]byte("secret-value"), "tenant:integration:field")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(ciphertext, []byte("secret-value")) {
		t.Fatal("ciphertext contains plaintext")
	}
	plaintext, err := cipher.Decrypt(ciphertext, "tenant:integration:field")
	if err != nil || string(plaintext) != "secret-value" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
	if _, err := cipher.Decrypt(ciphertext, "different-integration"); err == nil {
		t.Error("Decrypt() with different AAD error = nil")
	}
	if _, err := NewCipher(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("NewCipher(short key) error = nil")
	}
	if _, err := NewCipher("not-base64"); err == nil {
		t.Error("NewCipher(invalid base64) error = nil")
	}
	secret, err := GenerateWebhookSecret()
	if err != nil || len(secret) < 40 || !strings.HasPrefix(SecretFingerprint(secret), "sha256:") {
		t.Fatalf("generated secret = %q, fingerprint = %q, error = %v", secret, SecretFingerprint(secret), err)
	}
}

func TestValidateCreateNormalizesAndRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	active := false
	values, fields := ValidateCreate(CreateInput{
		Kind: KindWebhook, Name: "  Deploy alerts  ", WebhookURL: "https://hooks.example.test/fidibeki",
		Events:        []string{EventIssueUpdated, EventFeedbackCreated, EventIssueUpdated},
		CustomHeaders: map[string]string{"Authorization": "Bearer secret"}, Active: &active,
	})
	if len(fields) != 0 {
		t.Fatalf("ValidateCreate(valid) fields = %#v", fields)
	}
	if values.Name != "Deploy alerts" || values.Active || len(values.Events) != 2 ||
		values.Events[0] != EventFeedbackCreated || values.CustomHeaders["Authorization"] != "Bearer secret" {
		t.Errorf("validated values = %#v", values)
	}

	invalidCases := []CreateInput{
		{Kind: "email", Name: "Alerts", WebhookURL: "https://example.test", Events: []string{EventFeedbackCreated}},
		{Kind: KindSlack, Name: "Alerts", WebhookURL: "http://example.test/hook", Events: []string{EventFeedbackCreated}},
		{Kind: KindWebhook, Name: "Alerts", WebhookURL: "https://user:pass@example.test", Events: []string{EventFeedbackCreated}},
		{Kind: KindTeams, Name: "Alerts", WebhookURL: "https://example.test", Channel: stringPointer("team"), Events: []string{EventFeedbackCreated}},
		{Kind: KindSlack, Name: "Alerts", WebhookURL: "https://example.test", Events: nil},
		{Kind: KindWebhook, Name: "Alerts", WebhookURL: "https://example.test", Events: []string{"unknown"}},
		{Kind: KindWebhook, Name: "Alerts", WebhookURL: "https://example.test", Events: []string{EventFeedbackCreated}, CustomHeaders: map[string]string{"X-Fidibeki-Signature": "override"}},
		{Kind: KindSlack, Name: "Alerts", WebhookURL: "https://example.test", Events: []string{EventFeedbackCreated}, CustomHeaders: map[string]string{"Authorization": "secret"}},
	}
	for index, input := range invalidCases {
		if _, fields := ValidateCreate(input); len(fields) == 0 {
			t.Errorf("invalid case %d fields = nil", index)
		}
	}
	if validWebhookURL("http://127.0.0.1:8080/hook") ||
		!validWebhookURL("http://127.0.0.1:8080/hook", true) ||
		!validWebhookURL("http://localhost:8080/hook", true) {
		t.Error("loopback development webhook URL rejected")
	}
}

func TestNotificationPayloadsUseProviderFormats(t *testing.T) {
	t.Parallel()
	rating := 4
	notification := Notification{
		TenantName: "Acme", Event: EventFeedbackCreated, ResourceType: "feedback",
		ResourceID: "feedback-id", PublicID: "fb_123", Text: "Checkout is confusing",
		Rating: &rating, IssuePublicID: "ACME-2026-0001", Channel: "#feedback",
	}
	notification.Kind = KindSlack
	body, err := notificationBody(notification)
	if err != nil || !bytes.Contains(body, []byte(`"blocks"`)) || !bytes.Contains(body, []byte(`"channel":"#feedback"`)) {
		t.Fatalf("Slack body = %s, error = %v", body, err)
	}
	notification.Kind = KindTeams
	body, err = notificationBody(notification)
	if err != nil || !bytes.Contains(body, []byte(`"@type":"MessageCard"`)) {
		t.Fatalf("Teams body = %s, error = %v", body, err)
	}
	notification.Kind = KindWebhook
	body, err = notificationBody(notification)
	if err != nil || !bytes.Contains(body, []byte(`"event":"feedback.created"`)) {
		t.Fatalf("webhook body = %s, error = %v", body, err)
	}
	notification.Kind = "email"
	if _, err := notificationBody(notification); err == nil {
		t.Error("notificationBody(unsupported) error = nil")
	}
	issue := Notification{
		Event: EventIssueCreated, ResourceType: "issue", PublicID: "ACME-2026-0002",
		Title: "Payment failed", IssueStatus: "in_progress",
	}
	headline, detail := notificationCopy(issue)
	if headline != "New issue ACME-2026-0002" || !strings.Contains(detail, "Status: in progress") {
		t.Errorf("created issue copy = %q / %q", headline, detail)
	}
	issue.Event = EventIssueUpdated
	headline, _ = notificationCopy(issue)
	if headline != "Issue ACME-2026-0002 updated" {
		t.Errorf("updated issue headline = %q", headline)
	}
}

func TestTerminalHelpersAndUnsupportedEvents(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		event, resource string
		want            bool
	}{
		{EventFeedbackCreated, "feedback", true},
		{EventFeedbackCreated, "issue", false},
		{EventIssueCreated, "issue", true},
		{EventIssueUpdated, "issue", true},
		{EventIssueResolved, "issue", true},
		{"unknown", "issue", false},
	} {
		if got := supportedEvent(test.event, test.resource); got != test.want {
			t.Errorf("supportedEvent(%q, %q) = %t", test.event, test.resource, got)
		}
	}
	response := httptest.NewRecorder()
	writeIntegrationError(response, ErrNotFound)
	if response.Code != http.StatusNotFound {
		t.Errorf("not-found response = %d", response.Code)
	}
	response = httptest.NewRecorder()
	writeIntegrationError(response, io.ErrUnexpectedEOF)
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("service response = %d", response.Code)
	}
	if err := (DeliveryHandler{}).Handler()(context.Background(), asynq.NewTask(TaskDeliver, []byte(`{}`))); err == nil || !strings.Contains(err.Error(), "skip retry") {
		t.Errorf("invalid delivery task error = %v", err)
	}
}

func TestWebhookDeliverySignsExactBodyAndHandlesProviderFailures(t *testing.T) {
	t.Parallel()
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer encrypted-value" ||
			r.Header.Get("X-Fidibeki-Event") != EventFeedbackCreated {
			t.Errorf("delivery headers = %#v", r.Header)
		}
		w.Header().Set("Request-Id", "provider-123")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	notification := Notification{
		OutboxEventID: "event-id", IntegrationID: "integration-id", Kind: KindWebhook,
		WebhookURL: server.URL, WebhookSecret: "signing-secret",
		CustomHeaders: map[string]string{"Authorization": "Bearer encrypted-value"},
		Event:         EventFeedbackCreated, ResourceType: "feedback", ResourceID: "feedback-id",
		PublicID: "fb_123", OccurredAt: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
	}
	client := &captureDoer{client: server.Client()}
	providerID, err := (DeliveryHandler{Client: client}).deliver(context.Background(), notification)
	if err != nil || providerID != "provider-123" {
		t.Fatalf("deliver() = %q, %v", providerID, err)
	}
	mac := hmac.New(sha256.New, []byte(notification.WebhookSecret))
	_, _ = mac.Write(receivedBody)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := client.request.Header.Get("X-Fidibeki-Signature"); got != wantSignature {
		t.Errorf("signature = %q, want %q", got, wantSignature)
	}

	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer failure.Close()
	notification.WebhookURL = failure.URL
	if _, err := (DeliveryHandler{Client: failure.Client()}).deliver(context.Background(), notification); err == nil ||
		!strings.Contains(err.Error(), "503") {
		t.Errorf("failed deliver error = %v", err)
	}
}

type captureDoer struct {
	client  HTTPDoer
	request *http.Request
}

func (d *captureDoer) Do(request *http.Request) (*http.Response, error) {
	d.request = request
	return d.client.Do(request)
}

func stringPointer(value string) *string { return &value }

func decodeMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return value
}
