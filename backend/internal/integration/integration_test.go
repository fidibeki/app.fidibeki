package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
		PrivacyFields: []string{PrivacyFieldPage, PrivacyFieldDescription, PrivacyFieldPage},
	})
	if len(fields) != 0 {
		t.Fatalf("ValidateCreate(valid) fields = %#v", fields)
	}
	if values.Name != "Deploy alerts" || values.Active || len(values.Events) != 2 ||
		values.Events[0] != EventFeedbackCreated || values.CustomHeaders["Authorization"] != "Bearer secret" ||
		len(values.PrivacyFields) != 2 || values.PrivacyFields[0] != PrivacyFieldDescription {
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
		{Kind: KindSlack, Name: "Alerts", WebhookURL: "https://example.test", Events: []string{EventFeedbackCreated}, PrivacyFields: []string{"reporter_email"}},
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

func TestNotificationPrivacyProfileDefaultsToMinimumAndStripsPageSecrets(t *testing.T) {
	t.Parallel()
	rating := 1
	notification := Notification{
		Kind: KindWebhook, OutboxEventID: "event-id", Event: EventFeedbackCreated,
		ResourceType: "feedback", ResourceID: "feedback-id", PublicID: "fb_private",
		Title: "Sanitized title", Description: "DESCRIPTION-SENTINEL", Text: "TEXT-SENTINEL",
		PageURL: "https://example.test/chart?asset=BAI#private-fragment", Rating: &rating,
		Category: "billing", Actionability: "actionable", Priority: "high",
		ProjectID: "project-id", ProjectName: "Storefront", FeedbackStatus: "new",
		ResourceURL:     "https://acme.fidibeki.com/feedback/feedback-id",
		AttachmentsURL:  "https://acme.fidibeki.com/feedback/feedback-id#attachments",
		AttachmentCount: 2, AttachmentKinds: []string{"audio", "screenshot"},
		Changes: map[string]Change{"priority": {From: "low", To: "high"}},
		Actor:   &NotificationActor{Type: "member", ID: "member-id", Name: "ACTOR-SENTINEL"},
		Comment: "COMMENT-SENTINEL", PrivacyProfileVersion: PrivacyProfileVersion,
		DestinationCapabilities: destinationCapabilities(),
	}
	body, err := notificationBody(notification)
	if err != nil {
		t.Fatalf("notificationBody(minimum) error = %v", err)
	}
	for _, forbidden := range []string{
		"DESCRIPTION-SENTINEL", "TEXT-SENTINEL", "asset=BAI", "private-fragment",
		"ACTOR-SENTINEL", "COMMENT-SENTINEL", "audio", "billing", "actionable",
	} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Errorf("minimum webhook disclosed %q: %s", forbidden, body)
		}
	}
	for _, required := range []string{
		"fb_private", "Sanitized title", "Storefront", `"status":"new"`, `"priority":"high"`,
	} {
		if !bytes.Contains(body, []byte(required)) {
			t.Errorf("minimum webhook omitted %q: %s", required, body)
		}
	}

	notification.PrivacyFields = append([]string(nil), allPrivacyFields...)
	body, err = notificationBody(notification)
	if err != nil {
		t.Fatalf("notificationBody(full profile) error = %v", err)
	}
	for _, required := range []string{
		"DESCRIPTION-SENTINEL", "ACTOR-SENTINEL", "COMMENT-SENTINEL",
		`"page_url":"https://example.test/chart"`, `"attachment_metadata"`,
	} {
		if !bytes.Contains(body, []byte(required)) {
			t.Errorf("full-profile webhook omitted %q: %s", required, body)
		}
	}
	if bytes.Contains(body, []byte("asset=BAI")) || bytes.Contains(body, []byte("private-fragment")) {
		t.Errorf("full-profile webhook retained page query or hash: %s", body)
	}
}

func TestNotificationPayloadsUseProviderFormats(t *testing.T) {
	t.Parallel()
	rating := 4
	notification := Notification{
		OutboxEventID: "event-id", TenantName: "Acme", Event: EventFeedbackCreated,
		ResourceType: "feedback", ResourceID: "feedback-id", PublicID: "fb_123",
		Title: "Checkout <script>", Description: "Checkout is confusing", Rating: &rating,
		ProjectID: "project-id", ProjectName: "Storefront", Priority: "high",
		Category: "checkout", Actionability: "actionable",
		IssueID: "issue-id", IssuePublicID: "ACME-2026-0001", Channel: "#feedback",
		OccurredAt:      time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		ResourceURL:     "https://acme.fidibeki.com/feedback/feedback-id",
		FeedbackURL:     "https://acme.fidibeki.com/feedback/feedback-id",
		IssueURL:        "https://acme.fidibeki.com/issues/issue-id",
		PageURL:         "https://example.test/checkout",
		AttachmentsURL:  "https://acme.fidibeki.com/feedback/feedback-id#attachments",
		AttachmentCount: 2, AttachmentKinds: []string{"audio", "screenshot"},
		Changes:                 map[string]Change{"priority": {From: nil, To: "high"}},
		Actor:                   &NotificationActor{Type: "member", ID: "member-id", Name: "Alex"},
		Comment:                 "Needs review",
		PrivacyFields:           append([]string(nil), allPrivacyFields...),
		PrivacyProfileVersion:   PrivacyProfileVersion,
		DestinationCapabilities: destinationCapabilities(),
	}
	notification.Kind = KindSlack
	body, err := notificationBody(notification)
	if err != nil || !bytes.Contains(body, []byte(`"blocks"`)) ||
		!bytes.Contains(body, []byte(`"channel":"#feedback"`)) ||
		!bytes.Contains(body, []byte(`"type":"actions"`)) ||
		!bytes.Contains(body, []byte(`Open issue`)) ||
		bytes.Contains(body, []byte(`<script>`)) {
		t.Fatalf("Slack body = %s, error = %v", body, err)
	}
	notification.Kind = KindTeams
	body, err = notificationBody(notification)
	if err != nil || !bytes.Contains(body, []byte(`"@type":"MessageCard"`)) ||
		!bytes.Contains(body, []byte(`"potentialAction"`)) || bytes.Contains(body, []byte(`<script>`)) {
		t.Fatalf("Teams body = %s, error = %v", body, err)
	}
	notification.Kind = KindWebhook
	body, err = notificationBody(notification)
	if err != nil || !bytes.Contains(body, []byte(`"event":"feedback.created"`)) ||
		!bytes.Contains(body, []byte(`"schema_version":"1.0"`)) ||
		!bytes.Contains(body, []byte(`"event_id":"event-id"`)) ||
		!bytes.Contains(body, []byte(`"changes":{"priority":{"from":null,"to":"high"}}`)) ||
		!bytes.Contains(body, []byte(`"page_url":"https://example.test/checkout"`)) {
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
	if headline != "New issue ACME-2026-0002" || detail != "Payment failed" {
		t.Errorf("created issue copy = %q / %q", headline, detail)
	}
	facts := notificationFacts(issue)
	if len(facts) != 1 || facts[0].Name != "Status" || facts[0].Value != "in progress" {
		t.Errorf("created issue facts = %#v", facts)
	}
	issue.Event = EventIssueUpdated
	headline, _ = notificationCopy(issue)
	if headline != "Issue ACME-2026-0002 updated" {
		t.Errorf("updated issue headline = %q", headline)
	}
	issue.Event = EventIssueResolved
	headline, _ = notificationCopy(issue)
	if headline != "Issue ACME-2026-0002 resolved" {
		t.Errorf("resolved issue headline = %q", headline)
	}
}

func TestIntegrationActionsDoNotPresentOriginOnlyContextAsAnExactPage(t *testing.T) {
	t.Parallel()
	actions := integrationActions(Notification{
		ResourceType: "feedback",
		ResourceURL:  "https://acme.fidibeki.com/feedback/feedback-id",
		PageURL:      "https://example.test/",
	})
	for _, action := range actions {
		if action.Label == "View page" {
			t.Fatalf("origin-only context exposed as a page action: %#v", actions)
		}
	}
	actions = integrationActions(Notification{
		ResourceType:  "feedback",
		ResourceURL:   "https://acme.fidibeki.com/feedback/feedback-id",
		PageURL:       "https://example.test/checkout",
		PrivacyFields: []string{PrivacyFieldPage},
	})
	found := false
	for _, action := range actions {
		found = found || action.Label == "View page"
	}
	if !found {
		t.Fatalf("approved static page action missing: %#v", actions)
	}
}

func TestModerateNotificationRedactsFlaggedFeedbackAcrossProviders(t *testing.T) {
	t.Parallel()
	notification := Notification{
		ResourceType: "feedback", Title: "unsafe title", Description: "unsafe description",
		Text: "unsafe text", SuggestedTitle: "Checkout feedback needs review",
		TextModerationFlags: []string{"profanity"}, AIProcessed: true,
	}
	moderateNotification(&notification)
	if notification.Title != "Checkout feedback needs review" || notification.Description != "[MODERATED]" ||
		notification.Text != "[MODERATED]" {
		t.Fatalf("moderated notification = %#v", notification)
	}
}

func TestModerateIssueNotificationRedactsDerivedContentAndChanges(t *testing.T) {
	t.Parallel()
	notification := Notification{
		ResourceType: "issue", Title: "unsafe title", Description: "unsafe description",
		IssueSuggestedTitle: "Checkout feedback needs review",
		IssueTitleModerated: true, IssueDescriptionModerated: true,
		IssueModeratedSources: []string{"old unsafe", "unsafe title", "unsafe description"},
		Changes: map[string]Change{
			"title":       {From: "old unsafe", To: "unsafe title"},
			"description": {From: "old unsafe", To: "unsafe description"},
		},
	}
	moderateIssueNotification(&notification)
	if notification.Title != "Checkout feedback needs review" || notification.Description != "[MODERATED]" ||
		notification.Changes["title"].To != "[MODERATED]" ||
		notification.Changes["description"].From != "[MODERATED]" {
		t.Fatalf("moderated issue notification = %#v", notification)
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
	for _, test := range []struct {
		actor NotificationActor
		want  string
	}{
		{NotificationActor{Type: "member"}, "member"},
		{NotificationActor{Type: "reporter"}, "Reporter"},
		{NotificationActor{Type: "service"}, "service"},
	} {
		if got := notificationActorLabel(test.actor); got != test.want {
			t.Errorf("notificationActorLabel(%#v) = %q, want %q", test.actor, got, test.want)
		}
	}
	if got := truncateIntegrationText("short", 10); got != "short" {
		t.Errorf("truncateIntegrationText(short) = %q", got)
	}
	if got := truncateIntegrationText("long value", 7); got != "long..." {
		t.Errorf("truncateIntegrationText(long) = %q", got)
	}
	feedbackHeadline, feedbackDetail := notificationCopy(Notification{
		ResourceType: "feedback", PublicID: "fb_123", Description: "Original report",
		PrivacyFields: []string{PrivacyFieldDescription},
	})
	if feedbackHeadline != "New feedback fb_123" || feedbackDetail != "Original report" {
		t.Errorf("fallback feedback copy = %q / %q", feedbackHeadline, feedbackDetail)
	}
	emptyHeadline, emptyDetail := notificationCopy(Notification{ResourceType: "feedback", PublicID: "fb_empty"})
	if emptyHeadline != "New feedback fb_empty" || emptyDetail != "Feedback received without a text comment." {
		t.Errorf("empty feedback copy = %q / %q", emptyHeadline, emptyDetail)
	}
	_, legacyDetail := notificationCopy(Notification{
		ResourceType: "feedback", Text: "Legacy feedback text",
		PrivacyFields: []string{PrivacyFieldDescription},
	})
	if legacyDetail != "Legacy feedback text" {
		t.Errorf("legacy feedback detail = %q", legacyDetail)
	}
	_, issueDetail := notificationCopy(Notification{
		ResourceType: "issue", Event: EventIssueUpdated, Description: "Issue-only description",
		PrivacyFields: []string{PrivacyFieldDescription},
	})
	if issueDetail != "Issue-only description" {
		t.Errorf("description-only issue detail = %q", issueDetail)
	}
	_, completeIssueDetail := notificationCopy(Notification{
		ResourceType: "issue", Event: EventIssueUpdated,
		Title: "Issue title", Description: "Issue description",
		PrivacyFields: []string{PrivacyFieldDescription},
	})
	if completeIssueDetail != "Issue title\nIssue description" {
		t.Errorf("complete issue detail = %q", completeIssueDetail)
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
