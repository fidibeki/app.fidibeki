package privacy

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
)

func TestIntegrationActionChecklistTracksManualDisclosureLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	tenantID, memberID := seedPrivacyWorkspace(
		t, ctx, database, "integration-actions", "Reporter-owned feedback",
	)
	store := Store{Database: database}
	now := time.Now().UTC().Add(time.Minute)
	privacyCase := createVerifiedReporterCase(
		t, ctx, database, store, tenantID, memberID,
		"integration-action-case-rectification", RightRectification, now,
	)

	var feedbackID, primaryIntegrationID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM feedbacks
		WHERE tenant_id = $1 AND reporter_email = 'reporter@example.test'
	`, tenantID).Scan(&feedbackID); err != nil {
		t.Fatalf("read reporter feedback: %v", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT id FROM integrations WHERE tenant_id = $1 ORDER BY created_at, id LIMIT 1
	`, tenantID).Scan(&primaryIntegrationID); err != nil {
		t.Fatalf("read primary integration: %v", err)
	}
	seedIntegrationDisclosure(
		t, ctx, database, tenantID, primaryIntegrationID, feedbackID,
		"feedback", "feedback.created", []string{"description", "resource_identity"},
		"sent", now.Add(5*time.Minute),
	)
	seedIntegrationDisclosure(
		t, ctx, database, tenantID, primaryIntegrationID, feedbackID,
		"feedback", "feedback.created", []string{"source_page", "resource_identity"},
		"sent", now.Add(10*time.Minute),
	)
	seedIntegrationDisclosure(
		t, ctx, database, tenantID, primaryIntegrationID, feedbackID,
		"feedback", "feedback.created", []string{"attachment_metadata"},
		"failed", now.Add(15*time.Minute),
	)

	var removedIntegrationID string
	if err := database.QueryRow(ctx, `
		INSERT INTO integrations (
			tenant_id, kind, name, webhook_url, events, is_active
		) VALUES (
			$1, 'teams', 'Removed destination', 'https://teams.example.test/hook',
			ARRAY['feedback.created']::event_type[], true
		) RETURNING id
	`, tenantID).Scan(&removedIntegrationID); err != nil {
		t.Fatalf("create removable integration: %v", err)
	}
	seedIntegrationDisclosure(
		t, ctx, database, tenantID, removedIntegrationID, feedbackID,
		"feedback", "feedback.created", []string{"rating"}, "sent", now.Add(12*time.Minute),
	)
	if _, err := database.Exec(ctx, `
		DELETE FROM integrations WHERE tenant_id = $1 AND id = $2
	`, tenantID, removedIntegrationID); err != nil {
		t.Fatalf("remove disclosed integration: %v", err)
	}

	actions, created, reopened, err := store.RefreshIntegrationActions(
		ctx, database, tenantID, privacyCase.ID, memberID, now.Add(20*time.Minute),
	)
	if err != nil || created != 2 || reopened != 0 || len(actions) != 2 {
		t.Fatalf("RefreshIntegrationActions() = %#v created=%d reopened=%d error=%v",
			actions, created, reopened, err)
	}
	primary := integrationActionByKey(t, actions, "integration:"+primaryIntegrationID)
	if primary.Status != IntegrationActionManualRequired ||
		primary.HandlingMode != IntegrationActionManual ||
		primary.DisclosureCount != 2 ||
		!slices.Contains(primary.DataCategories, "description") ||
		!slices.Contains(primary.DataCategories, "source_page") ||
		slices.Contains(primary.DataCategories, "attachment_metadata") {
		t.Fatalf("primary integration action = %#v", primary)
	}
	removed := integrationActionByKey(t, actions, "integration:"+removedIntegrationID)
	if removed.IntegrationID != nil || removed.DestinationKind == nil ||
		*removed.DestinationKind != "teams" ||
		removed.DestinationName != "Removed teams destination" {
		t.Fatalf("removed integration action = %#v", removed)
	}
	actions, created, reopened, err = store.RefreshIntegrationActions(
		ctx, database, tenantID, privacyCase.ID, memberID, now.Add(21*time.Minute),
	)
	if err != nil || created != 0 || reopened != 0 || len(actions) != 2 {
		t.Fatalf("idempotent refresh = %#v created=%d reopened=%d error=%v",
			actions, created, reopened, err)
	}

	_, _, err = store.CompleteIntegrationAction(
		ctx, database, tenantID, privacyCase.ID, primary.ID, memberID,
		"complete-1", CompleteIntegrationActionInput{CompletionNote: "Provider ticket EXT-100 closed."},
		now.Add(22*time.Minute),
	)
	if !errors.Is(err, ErrRecipientActionEvidence) {
		t.Fatalf("completion without correction evidence error = %v", err)
	}
	_, _, err = store.ApplyFeedbackRectification(
		ctx, database, tenantID, privacyCase.ID, memberID, "feedback-fix-1",
		RecordFeedbackRectificationInput{
			FeedbackID: feedbackID, Description: "Corrected reporter description.",
			Reason: "Verified reporter correction.",
		},
		now.Add(23*time.Minute),
	)
	if err != nil {
		t.Fatalf("apply feedback rectification evidence: %v", err)
	}
	completed, changed, err := store.CompleteIntegrationAction(
		ctx, database, tenantID, privacyCase.ID, primary.ID, memberID,
		"complete-1", CompleteIntegrationActionInput{CompletionNote: "Provider ticket EXT-100 closed."},
		now.Add(24*time.Minute),
	)
	if err != nil || !changed || completed.Status != IntegrationActionCompleted ||
		completed.CompletedByMemberID == nil || completed.CompletionNote == nil {
		t.Fatalf("CompleteIntegrationAction() = %#v changed=%t error=%v", completed, changed, err)
	}
	replayed, changed, err := store.CompleteIntegrationAction(
		ctx, database, tenantID, privacyCase.ID, primary.ID, memberID,
		"complete-1", CompleteIntegrationActionInput{CompletionNote: "Provider ticket EXT-100 closed."},
		now.Add(25*time.Minute),
	)
	if err != nil || changed || replayed.Status != IntegrationActionCompleted {
		t.Fatalf("idempotent completion = %#v changed=%t error=%v", replayed, changed, err)
	}
	if _, _, err := store.CompleteIntegrationAction(
		ctx, database, tenantID, privacyCase.ID, primary.ID, memberID,
		"complete-1", CompleteIntegrationActionInput{CompletionNote: "Different evidence."},
		now.Add(25*time.Minute),
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting completion error = %v", err)
	}

	recipient, recipientCreated, err := store.CreateRecipientAction(
		ctx, database, tenantID, privacyCase.ID, memberID, "recipient-link-1",
		CreateRecipientActionInput{
			ActionKind: RecipientActionRectification, RecipientName: "Slack owner",
			RecipientEmail:      "slack-owner@example.test",
			InstructionSummary:  "Apply the verified correction to retained destination copies.",
			IntegrationActionID: primary.ID,
		}, now.Add(26*time.Minute),
	)
	if err != nil || !recipientCreated || recipient.IntegrationActionID == nil ||
		*recipient.IntegrationActionID != primary.ID {
		t.Fatalf("linked CreateRecipientAction() = %#v created=%t error=%v",
			recipient, recipientCreated, err)
	}

	seedIntegrationDisclosure(
		t, ctx, database, tenantID, primaryIntegrationID, feedbackID,
		"feedback", "feedback.created", []string{"description", "resource_identity"},
		"sent", now.Add(30*time.Minute),
	)
	actions, created, reopened, err = store.RefreshIntegrationActions(
		ctx, database, tenantID, privacyCase.ID, memberID, now.Add(31*time.Minute),
	)
	if err != nil || created != 0 || reopened != 1 {
		t.Fatalf("refresh after new disclosure = %#v created=%d reopened=%d error=%v",
			actions, created, reopened, err)
	}
	primary = integrationActionByKey(t, actions, "integration:"+primaryIntegrationID)
	if primary.Status != IntegrationActionManualRequired ||
		primary.CompletedAt != nil || primary.CompletionNote != nil ||
		!slices.Contains(primary.LinkedRecipientActionIDs, recipient.ID) {
		t.Fatalf("reopened integration action = %#v", primary)
	}

	apiActionID := uuid.NewString()
	if _, err := database.Exec(ctx, `
		INSERT INTO privacy_integration_actions (
			id, tenant_id, privacy_case_id, integration_id, destination_key,
			destination_kind, destination_name, action_kind, handling_mode, status,
			capability_snapshot, data_categories, disclosure_count,
			first_disclosed_at, last_disclosed_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, 'webhook', 'Future API destination',
			'rectification', 'api', 'api_pending',
			'{"message_update":true}'::jsonb, ARRAY['description'], 1,
			$6, $6, $6, $6
		)
	`, apiActionID, tenantID, privacyCase.ID, primaryIntegrationID,
		"integration-api:"+primaryIntegrationID, now.Add(32*time.Minute)); err != nil {
		t.Fatalf("create future API integration action: %v", err)
	}
	if _, _, err := store.CompleteIntegrationAction(
		ctx, database, tenantID, privacyCase.ID, apiActionID, memberID,
		"manual-api-completion", CompleteIntegrationActionInput{
			CompletionNote: "Manual completion must not bypass the destination adapter.",
		}, now.Add(33*time.Minute),
	); !errors.Is(err, ErrIntegrationActionNotManual) {
		t.Fatalf("manual completion of API action error = %v", err)
	}

	otherTenantID, otherMemberID := seedPrivacyWorkspace(
		t, ctx, database, "int-actions-other", "Other tenant feedback",
	)
	if _, _, _, err := store.RefreshIntegrationActions(
		ctx, database, otherTenantID, privacyCase.ID, otherMemberID, now,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant refresh error = %v", err)
	}
}

func TestIntegrationActionChecklistScopesMemberWorkflowDisclosure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	tenantID, memberID := seedPrivacyWorkspace(
		t, ctx, database, "int-member-actions", "Member workflow feedback",
	)
	store := Store{Database: database}
	now := time.Now().UTC().Add(time.Minute)
	privacyCase, _, _, err := store.CreateCase(
		ctx, database, tenantID, memberID, "member-integration-action-case",
		CreateCaseInput{
			SubjectType: SubjectMember, RightType: RightRectification,
			ControllerRole: RoleController, SubjectMemberID: memberID,
			OwnerMemberID: memberID, Scope: map[string]any{"source": "member_profile"},
		}, now,
	)
	if err != nil {
		t.Fatalf("create member rectification case: %v", err)
	}
	var issueID, integrationID, activityID string
	if err := database.QueryRow(ctx, `
		SELECT id FROM issues WHERE tenant_id = $1 ORDER BY created_at, id LIMIT 1
	`, tenantID).Scan(&issueID); err != nil {
		t.Fatalf("read member issue: %v", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT id FROM integrations WHERE tenant_id = $1 ORDER BY created_at, id LIMIT 1
	`, tenantID).Scan(&integrationID); err != nil {
		t.Fatalf("read member integration: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO issue_activities (tenant_id, issue_id, actor_member_id, kind, data)
		VALUES ($1, $2, $3, 'issue.status_changed', '{"changes":{"status":{"from":"open","to":"in_progress"}}}')
		RETURNING id
	`, tenantID, issueID, memberID).Scan(&activityID); err != nil {
		t.Fatalf("create member issue activity: %v", err)
	}
	seedIntegrationDisclosureWithEventKey(
		t, ctx, database, tenantID, integrationID, issueID, "issue", "issue.updated",
		activityID, []string{"workflow_context", "resource_identity"}, "sent", now.Add(time.Minute),
	)
	seedIntegrationDisclosureWithEventKey(
		t, ctx, database, tenantID, integrationID, issueID, "issue", "issue.updated",
		"unrelated-event", []string{"resource_identity"}, "sent", now.Add(2*time.Minute),
	)
	actions, created, reopened, err := store.RefreshIntegrationActions(
		ctx, database, tenantID, privacyCase.ID, memberID, now.Add(3*time.Minute),
	)
	if err != nil || created != 1 || reopened != 0 || len(actions) != 1 ||
		actions[0].DisclosureCount != 1 ||
		!slices.Contains(actions[0].DataCategories, "workflow_context") {
		t.Fatalf("member disclosure actions = %#v created=%d reopened=%d error=%v",
			actions, created, reopened, err)
	}
}

func seedIntegrationDisclosure(
	t *testing.T,
	ctx context.Context,
	database db.Querier,
	tenantID string,
	integrationID string,
	resourceID string,
	resourceType string,
	event string,
	categories []string,
	status string,
	occurredAt time.Time,
) {
	t.Helper()
	seedIntegrationDisclosureWithEventKey(
		t, ctx, database, tenantID, integrationID, resourceID, resourceType,
		event, "", categories, status, occurredAt,
	)
}

func seedIntegrationDisclosureWithEventKey(
	t *testing.T,
	ctx context.Context,
	database db.Querier,
	tenantID string,
	integrationID string,
	resourceID string,
	resourceType string,
	event string,
	eventKey string,
	categories []string,
	status string,
	occurredAt time.Time,
) {
	t.Helper()
	var outboxID, destinationKind string
	if eventKey == "" {
		eventKey = uuid.NewString()
	}
	if err := database.QueryRow(ctx, `
		SELECT kind::text FROM integrations WHERE tenant_id = $1 AND id = $2
	`, tenantID, integrationID).Scan(&destinationKind); err != nil {
		t.Fatalf("read integration kind: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO outbox_events (
			tenant_id, event, resource_type, resource_id, event_key, created_at
		) VALUES ($1, $2::event_type, $3, $4, $5, $6)
		RETURNING id
	`, tenantID, event, resourceType, resourceID, eventKey, occurredAt.UTC()).Scan(&outboxID); err != nil {
		t.Fatalf("create disclosure outbox event: %v", err)
	}
	deliveryKey := "integration:" + integrationID
	if status == "sent" {
		if _, err := database.Exec(ctx, `
			INSERT INTO delivery_logs (
				tenant_id, outbox_event_id, integration_id, delivery_key, event,
				resource_type, resource_id, attempt_number, status,
				provider_message_id, sent_at, destination_kind,
				privacy_profile_version, data_categories, destination_capabilities,
				created_at
			) VALUES (
				$1, $2, $3, $4, $5::event_type, $6, $7, 1, 'sent',
				'provider-message', $8, $9::integration_kind, 1, $10,
				'{"message_update":false,"message_delete":false,"message_recall":false,
				  "fidibeki_links_require_authentication":true,
				  "fidibeki_links_access_controlled":true}'::jsonb,
				$8
			)
		`, tenantID, outboxID, integrationID, deliveryKey, event, resourceType,
			resourceID, occurredAt.UTC(), destinationKind, categories); err != nil {
			t.Fatalf("create sent integration disclosure: %v", err)
		}
		return
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO delivery_logs (
			tenant_id, outbox_event_id, integration_id, delivery_key, event,
			resource_type, resource_id, attempt_number, status, error_message,
			destination_kind, privacy_profile_version, data_categories,
			destination_capabilities, created_at
		) VALUES (
			$1, $2, $3, $4, $5::event_type, $6, $7, 1, 'failed', 'provider failed',
			$8::integration_kind, 1, $9,
			'{"message_update":false,"message_delete":false,"message_recall":false,
			  "fidibeki_links_require_authentication":true,
			  "fidibeki_links_access_controlled":true}'::jsonb,
			$10
		)
	`, tenantID, outboxID, integrationID, deliveryKey, event, resourceType,
		resourceID, destinationKind, categories, occurredAt.UTC()); err != nil {
		t.Fatalf("create failed integration disclosure: %v", err)
	}
}

func integrationActionByKey(
	t *testing.T,
	actions []IntegrationAction,
	destinationKey string,
) IntegrationAction {
	t.Helper()
	for _, action := range actions {
		if action.DestinationKey == destinationKey {
			return action
		}
	}
	t.Fatalf("integration action %q not found in %#v", destinationKey, actions)
	return IntegrationAction{}
}

func TestIntegrationActionHelpersRejectUnsafeCases(t *testing.T) {
	t.Parallel()
	if integrationActionCaseEligible(PrivacyCase{
		VerifiedAt: func() *time.Time { value := time.Now(); return &value }(),
		RightType:  RightAccess,
		Status:     CaseInProgress,
	}) {
		t.Fatal("access case was eligible for an integration action checklist")
	}
	if IntegrationActionManualRequired != "manual_required" {
		t.Fatal("manual integration action status changed unexpectedly")
	}
}
