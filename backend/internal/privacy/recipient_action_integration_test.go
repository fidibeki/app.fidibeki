package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/outbox"
	"github.com/hibiken/asynq"
)

func TestRecipientActionRequiresEvidenceAndTracksRetryDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 23, 0, 0, 0, time.UTC)
	tenantID, actorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "recipient-action", "Sensitive feedback must not enter the notice",
	)
	otherTenantID, otherActorMemberID := seedPrivacyWorkspace(
		t, ctx, database, "recipient-action-other", "Other tenant content",
	)
	store := Store{Database: database}
	privacyCase, _, _, err := store.CreateCase(
		ctx, database, tenantID, actorMemberID, "recipient-action-case",
		CreateCaseInput{
			SubjectType: SubjectMember, RightType: RightRectification,
			ControllerRole: RoleController, SubjectMemberID: actorMemberID,
			OwnerMemberID: actorMemberID,
		}, now,
	)
	if err != nil {
		t.Fatalf("create rectification case: %v", err)
	}
	input := CreateRecipientActionInput{
		ActionKind:         RecipientActionRectification,
		RecipientName:      "Downstream processor",
		RecipientEmail:     "PROCESSOR@example.test",
		InstructionSummary: "Correct the approved profile fields in the downstream record.",
	}
	if _, _, err := store.CreateRecipientAction(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"recipient-before-evidence", input, now.Add(time.Minute),
	); !errors.Is(err, ErrRecipientActionEvidence) {
		t.Fatalf("recipient action before evidence error = %v", err)
	}
	if _, _, err := store.ApplyCaseRectification(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"recipient-proof",
		RecordCaseRectificationInput{
			Changes: map[string]*string{"name": pointerTo("Corrected member")},
			Reason:  "Approved profile correction.",
		}, now.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("apply rectification proof: %v", err)
	}
	action, created, err := store.CreateRecipientAction(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"recipient-create", input, now.Add(3*time.Minute),
	)
	if err != nil || !created || action.Status != RecipientActionPending ||
		action.RecipientEmail != "processor@example.test" {
		t.Fatalf("create recipient action = %#v created=%t error=%v", action, created, err)
	}
	replayed, created, err := store.CreateRecipientAction(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"recipient-create", input, now.Add(4*time.Minute),
	)
	if err != nil || created || replayed.ID != action.ID {
		t.Fatalf("replay recipient action = %#v created=%t error=%v", replayed, created, err)
	}
	conflictInput := input
	conflictInput.InstructionSummary = "A different instruction."
	if _, _, err := store.CreateRecipientAction(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"recipient-create", conflictInput, now.Add(5*time.Minute),
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("recipient action idempotency conflict = %v", err)
	}
	loadedCase, err := store.GetCase(ctx, database, tenantID, privacyCase.ID)
	if err != nil || len(loadedCase.RecipientActions) != 1 ||
		loadedCase.RecipientActions[0].ID != action.ID {
		t.Fatalf("loaded recipient actions = %#v error=%v", loadedCase.RecipientActions, err)
	}

	var firstEventID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM outbox_events
		WHERE tenant_id = $1 AND resource_type = 'privacy_recipient_action'
		  AND resource_id = $2 AND event_key = 'create'
	`, tenantID, action.ID).Scan(&firstEventID); err != nil {
		t.Fatalf("read recipient outbox event: %v", err)
	}
	destinations, err := (RecipientOutboxResolver{}).Resolve(ctx, outbox.Event{
		ID: firstEventID, TenantID: tenantID,
		Event:        "privacy.recipient_action.requested",
		ResourceType: "privacy_recipient_action", ResourceID: action.ID,
	})
	if err != nil || len(destinations) != 1 ||
		destinations[0].TaskType != TaskRecipientNotify ||
		destinations[0].Queue != email.QueueName {
		t.Fatalf("recipient destination = %#v error=%v", destinations, err)
	}

	failing := &recipientSender{err: errors.New("provider secret token must not persist")}
	handler := RecipientHandler{
		Store: store, Sender: failing, From: "Fidibeki <privacy@fidibeki.com>",
		PublicBaseURL: "https://api.fidibeki.com",
		Now:           func() time.Time { return now.Add(6 * time.Minute) },
	}
	if err := handler.Handler()(ctx, recipientTask(t, firstEventID, tenantID, action.ID)); err == nil {
		t.Fatal("failed recipient delivery error = nil")
	}
	failed, err := loadRecipientAction(
		ctx, database, tenantID, privacyCase.ID, action.ID, true,
	)
	if err != nil || failed.Status != RecipientActionFailed ||
		failed.Attempts != 1 || failed.LastError == nil ||
		*failed.LastError != "email provider delivery failed" ||
		len(failed.DeliveryAttempts) != 1 ||
		failed.DeliveryAttempts[0].Status != RecipientDeliveryFailed ||
		strings.Contains(*failed.LastError, "secret") {
		t.Fatalf("failed recipient action = %#v error=%v", failed, err)
	}

	retried, queued, err := store.RetryRecipientAction(
		ctx, database, tenantID, privacyCase.ID, action.ID, actorMemberID,
		"retry-delivery", now.Add(7*time.Minute),
	)
	if err != nil || !queued || retried.Status != RecipientActionPending {
		t.Fatalf("retry recipient action = %#v queued=%t error=%v", retried, queued, err)
	}
	retryReplay, queued, err := store.RetryRecipientAction(
		ctx, database, tenantID, privacyCase.ID, action.ID, actorMemberID,
		"retry-delivery", now.Add(8*time.Minute),
	)
	if err != nil || queued || retryReplay.ID != action.ID {
		t.Fatalf("replay recipient retry = %#v queued=%t error=%v", retryReplay, queued, err)
	}
	if _, _, err := store.RetryRecipientAction(
		ctx, database, otherTenantID, privacyCase.ID, action.ID, otherActorMemberID,
		"cross-tenant-retry", now.Add(8*time.Minute),
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant recipient retry error = %v", err)
	}

	var retryEventID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM outbox_events
		WHERE tenant_id = $1 AND resource_type = 'privacy_recipient_action'
		  AND resource_id = $2 AND event_key = 'retry:retry-delivery'
	`, tenantID, action.ID).Scan(&retryEventID); err != nil {
		t.Fatalf("read recipient retry event: %v", err)
	}
	sender := &recipientSender{}
	handler.Sender = sender
	handler.Now = func() time.Time { return now.Add(9 * time.Minute) }
	retryTask := recipientTask(t, retryEventID, tenantID, action.ID)
	if err := handler.Handler()(ctx, retryTask); err != nil {
		t.Fatalf("send recipient retry: %v", err)
	}
	if err := handler.Handler()(ctx, retryTask); err != nil {
		t.Fatalf("repeat sent recipient task: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("recipient messages = %d, want one", len(sender.messages))
	}
	message := sender.messages[0]
	if message.IdempotencyKey != "privacy-recipient:"+action.ID {
		t.Fatalf("recipient idempotency key = %q", message.IdempotencyKey)
	}
	for _, required := range []string{
		privacyCase.PublicID, "rectification",
		input.InstructionSummary,
	} {
		if !strings.Contains(message.Text, required) {
			t.Errorf("recipient message does not contain %q: %s", required, message.Text)
		}
	}
	for _, forbidden := range []string{
		"Sensitive feedback must not enter the notice",
		"owner@recipient-action.test",
		"provider secret token",
	} {
		if strings.Contains(message.Text, forbidden) {
			t.Errorf("recipient message leaked %q: %s", forbidden, message.Text)
		}
	}
	delivered, err := loadRecipientAction(
		ctx, database, tenantID, privacyCase.ID, action.ID, true,
	)
	if err != nil || delivered.Status != RecipientActionSent ||
		delivered.Attempts != 2 || delivered.SentAt == nil ||
		len(delivered.DeliveryAttempts) != 2 ||
		delivered.DeliveryAttempts[0].Status != RecipientDeliverySent ||
		delivered.DeliveryAttempts[1].Status != RecipientDeliveryFailed {
		t.Fatalf("delivered recipient action = %#v error=%v", delivered, err)
	}
	if _, _, err := store.RetryRecipientAction(
		ctx, database, tenantID, privacyCase.ID, action.ID, actorMemberID,
		"retry-sent-delivery", now.Add(10*time.Minute),
	); !errors.Is(err, ErrRecipientActionNotRetryable) {
		t.Fatalf("retry sent recipient action error = %v", err)
	}

	secondInput := input
	secondInput.RecipientEmail = "second@example.test"
	secondAction, _, err := store.CreateRecipientAction(
		ctx, database, tenantID, privacyCase.ID, actorMemberID,
		"recipient-concurrency", secondInput, now.Add(11*time.Minute),
	)
	if err != nil {
		t.Fatalf("create concurrency recipient action: %v", err)
	}
	var secondEventID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM outbox_events
		WHERE tenant_id = $1 AND resource_type = 'privacy_recipient_action'
		  AND resource_id = $2 AND event_key = 'create'
	`, tenantID, secondAction.ID).Scan(&secondEventID); err != nil {
		t.Fatalf("read concurrency recipient event: %v", err)
	}
	secondNotification, err := store.LoadRecipientNotification(ctx, RecipientTask{
		OutboxEventID: secondEventID, TenantID: tenantID, ResourceID: secondAction.ID,
	})
	if err != nil {
		t.Fatalf("load concurrency recipient notification: %v", err)
	}
	activeAttempt, completed, err := store.StartRecipientDelivery(
		ctx, secondNotification, now.Add(12*time.Minute),
	)
	if err != nil || completed {
		t.Fatalf("start concurrency recipient delivery = %#v completed=%t error=%v",
			activeAttempt, completed, err)
	}
	if _, _, err := store.StartRecipientDelivery(
		ctx, secondNotification, now.Add(13*time.Minute),
	); !errors.Is(err, ErrRecipientDeliveryInProgress) {
		t.Fatalf("concurrent recipient delivery error = %v", err)
	}
	if err := store.MarkRecipientDeliveryFailed(
		ctx, secondNotification, activeAttempt.ID, now.Add(13*time.Minute),
	); err != nil {
		t.Fatalf("close concurrency recipient delivery: %v", err)
	}
}

type recipientSender struct {
	messages []email.Message
	err      error
}

func (sender *recipientSender) Send(
	_ context.Context,
	message email.Message,
) (string, error) {
	if sender.err != nil {
		return "", sender.err
	}
	sender.messages = append(sender.messages, message)
	return "recipient-provider-message", nil
}

func recipientTask(
	t *testing.T,
	eventID string,
	tenantID string,
	actionID string,
) *asynq.Task {
	t.Helper()
	payload, err := json.Marshal(RecipientTask{
		OutboxEventID: eventID, TenantID: tenantID, ResourceID: actionID,
	})
	if err != nil {
		t.Fatalf("encode recipient task: %v", err)
	}
	return asynq.NewTask(TaskRecipientNotify, payload)
}
