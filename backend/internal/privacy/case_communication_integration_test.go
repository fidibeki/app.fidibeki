package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/hibiken/asynq"
)

func TestCaseCommunicationTracksStateDeliveryAndRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "case-communication",
		"Feedback content must not enter case communication audit data.",
	)
	otherTenantID, otherActorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "case-communication-other", "Other tenant content.",
	)
	store := Store{Database: database}
	privacyCase, _, _, err := store.CreateCase(
		ctx, database, tenantID, actorMemberID, "case-communication-case",
		CreateCaseInput{
			SubjectType: SubjectMember, RightType: RightAccess,
			ControllerRole: RoleController, SubjectMemberID: actorMemberID,
			OwnerMemberID: actorMemberID, Scope: map[string]any{"channel": "backoffice"},
		}, now,
	)
	if err != nil {
		t.Fatalf("create privacy case: %v", err)
	}

	if _, _, err := store.CreateCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, actorMemberID, "decision-too-early",
		CreateCaseCommunicationInput{
			CommunicationKind: CaseCommunicationDecision,
			SubjectLine:       "Decision",
			MessageBody:       "The decision is not ready.",
		}, now.Add(time.Minute),
	); !errors.Is(err, ErrCaseCommunicationUnavailable) {
		t.Fatalf("early decision communication error = %v", err)
	}
	if _, _, err := store.CreateCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, actorMemberID, "clarification-too-early",
		CreateCaseCommunicationInput{
			CommunicationKind: CaseCommunicationClarification,
			SubjectLine:       "Information needed",
			MessageBody:       "Please provide more information.",
		}, now.Add(time.Minute),
	); !errors.Is(err, ErrCaseCommunicationUnavailable) {
		t.Fatalf("early clarification communication error = %v", err)
	}

	input := CreateCaseCommunicationInput{
		CommunicationKind: CaseCommunicationProgress,
		SubjectLine:       "Your request is being reviewed",
		MessageBody:       "We have started reviewing the requested records.",
	}
	communication, created, err := store.CreateCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"communication-create", input, now.Add(2*time.Minute),
	)
	if err != nil || !created || communication.Status != CaseCommunicationPending ||
		communication.RecipientEmail != "owner@case-communication.test" {
		t.Fatalf("create case communication = %#v created=%t error=%v",
			communication, created, err)
	}
	replayed, created, err := store.CreateCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"communication-create", input, now.Add(3*time.Minute),
	)
	if err != nil || created || replayed.ID != communication.ID {
		t.Fatalf("replay case communication = %#v created=%t error=%v",
			replayed, created, err)
	}
	conflicting := input
	conflicting.MessageBody = "A different update."
	if _, _, err := store.CreateCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"communication-create", conflicting, now.Add(4*time.Minute),
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("case communication idempotency conflict = %v", err)
	}
	if _, _, err := store.CreateCaseCommunication(
		ctx, database, otherTenantID, privacyCase.ID, otherActorMemberID,
		"cross-tenant-create", input, now.Add(4*time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant case communication error = %v", err)
	}
	loadedCase, err := store.GetCase(ctx, database, tenantID, privacyCase.ID)
	if err != nil || len(loadedCase.Communications) != 1 ||
		loadedCase.Communications[0].ID != communication.ID {
		t.Fatalf("loaded case communications = %#v error=%v",
			loadedCase.Communications, err)
	}

	var eventID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM outbox_events
		WHERE tenant_id = $1 AND resource_type = 'privacy_case_communication'
		  AND resource_id = $2 AND event_key = 'create'
	`, tenantID, communication.ID).Scan(&eventID); err != nil {
		t.Fatalf("read case communication outbox event: %v", err)
	}
	destinations, err := (CaseCommunicationOutboxResolver{}).Resolve(ctx, outbox.Event{
		ID: eventID, TenantID: tenantID,
		Event:        "privacy.case_communication.requested",
		ResourceType: "privacy_case_communication", ResourceID: communication.ID,
	})
	if err != nil || len(destinations) != 1 ||
		destinations[0].TaskType != TaskCaseCommunicationNotify ||
		destinations[0].Queue != email.QueueName {
		t.Fatalf("case communication destination = %#v error=%v", destinations, err)
	}

	failing := &recipientSender{err: errors.New("provider secret must not persist")}
	handler := CaseCommunicationHandler{
		Store: store, Sender: failing, From: "Fidibeki <privacy@fidibeki.com>",
		PublicBaseURL: "https://api.fidibeki.com",
		Now:           func() time.Time { return now.Add(5 * time.Minute) },
	}
	if err := handler.Handler()(ctx, caseCommunicationTask(
		t, eventID, tenantID, communication.ID,
	)); err == nil {
		t.Fatal("failed case communication delivery error = nil")
	}
	failed, err := loadCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, communication.ID, true,
	)
	if err != nil || failed.Status != CaseCommunicationFailed ||
		failed.Attempts != 1 || failed.LastError == nil ||
		*failed.LastError != "email provider delivery failed" ||
		len(failed.DeliveryAttempts) != 1 ||
		failed.DeliveryAttempts[0].Status != CaseCommunicationDeliveryFailed ||
		strings.Contains(*failed.LastError, "secret") {
		t.Fatalf("failed case communication = %#v error=%v", failed, err)
	}
	dashboard, err := store.CaseDashboard(ctx, database, tenantID, now.Add(6*time.Minute))
	if err != nil || dashboard.FailedExternal != 1 {
		t.Fatalf("failed external dashboard = %#v error=%v", dashboard, err)
	}

	retried, queued, err := store.RetryCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, communication.ID, actorMemberID,
		"communication-retry", now.Add(6*time.Minute),
	)
	if err != nil || !queued || retried.Status != CaseCommunicationPending {
		t.Fatalf("retry case communication = %#v queued=%t error=%v", retried, queued, err)
	}
	retryReplay, queued, err := store.RetryCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, communication.ID, actorMemberID,
		"communication-retry", now.Add(7*time.Minute),
	)
	if err != nil || queued || retryReplay.ID != communication.ID {
		t.Fatalf("replay communication retry = %#v queued=%t error=%v",
			retryReplay, queued, err)
	}
	if _, _, err := store.RetryCaseCommunication(
		ctx, database, otherTenantID, privacyCase.ID, communication.ID,
		otherActorMemberID, "cross-tenant-retry", now.Add(7*time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant case communication retry error = %v", err)
	}

	var retryEventID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM outbox_events
		WHERE tenant_id = $1 AND resource_type = 'privacy_case_communication'
		  AND resource_id = $2 AND event_key = 'retry:communication-retry'
	`, tenantID, communication.ID).Scan(&retryEventID); err != nil {
		t.Fatalf("read case communication retry event: %v", err)
	}
	sender := &recipientSender{}
	handler.Sender = sender
	handler.Now = func() time.Time { return now.Add(8 * time.Minute) }
	retryTask := caseCommunicationTask(t, retryEventID, tenantID, communication.ID)
	if err := handler.Handler()(ctx, retryTask); err != nil {
		t.Fatalf("send case communication retry: %v", err)
	}
	if err := handler.Handler()(ctx, retryTask); err != nil {
		t.Fatalf("repeat sent case communication task: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("case communication messages = %d, want one", len(sender.messages))
	}
	message := sender.messages[0]
	if message.IdempotencyKey != "privacy-case-communication:"+communication.ID {
		t.Fatalf("case communication idempotency key = %q", message.IdempotencyKey)
	}
	if message.Subject != input.SubjectLine {
		t.Fatalf("case communication subject = %q, want %q", message.Subject, input.SubjectLine)
	}
	for _, required := range []string{privacyCase.PublicID, input.MessageBody} {
		if !strings.Contains(message.Text, required) {
			t.Errorf("case communication does not contain %q: %s", required, message.Text)
		}
	}
	for _, forbidden := range []string{
		"Feedback content must not enter case communication audit data.",
		"provider secret must not persist",
	} {
		if strings.Contains(message.Text, forbidden) {
			t.Errorf("case communication leaked %q: %s", forbidden, message.Text)
		}
	}
	delivered, err := loadCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, communication.ID, true,
	)
	if err != nil || delivered.Status != CaseCommunicationSent ||
		delivered.Attempts != 2 || delivered.SentAt == nil ||
		len(delivered.DeliveryAttempts) != 2 ||
		delivered.DeliveryAttempts[0].Status != CaseCommunicationDeliverySent ||
		delivered.DeliveryAttempts[1].Status != CaseCommunicationDeliveryFailed {
		t.Fatalf("delivered case communication = %#v error=%v", delivered, err)
	}
	if _, _, err := store.RetryCaseCommunication(
		ctx, database, tenantID, privacyCase.ID, communication.ID, actorMemberID,
		"retry-sent-communication", now.Add(9*time.Minute),
	); !errors.Is(err, ErrCaseCommunicationNotRetryable) {
		t.Fatalf("retry sent case communication error = %v", err)
	}

	var auditData string
	if err := database.QueryRow(ctx, `
		SELECT data::text
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'privacy.case_communication_created'
		  AND data->>'communication_id' = $2
	`, tenantID, communication.ID).Scan(&auditData); err != nil {
		t.Fatalf("read case communication audit: %v", err)
	}
	for _, forbidden := range []string{
		communication.RecipientEmail, input.SubjectLine, input.MessageBody,
		"Feedback content must not enter case communication audit data.",
	} {
		if strings.Contains(auditData, forbidden) {
			t.Errorf("case communication audit leaked %q: %s", forbidden, auditData)
		}
	}
}

func caseCommunicationTask(
	t *testing.T,
	eventID string,
	tenantID string,
	communicationID string,
) *asynq.Task {
	t.Helper()
	payload, err := json.Marshal(CaseCommunicationTask{
		OutboxEventID: eventID, TenantID: tenantID, ResourceID: communicationID,
	})
	if err != nil {
		t.Fatalf("encode case communication task: %v", err)
	}
	return asynq.NewTask(TaskCaseCommunicationNotify, payload)
}
