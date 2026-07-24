package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type integrationActionDiscovery struct {
	IntegrationID    *string
	DestinationKey   string
	DestinationKind  *string
	DestinationName  string
	Capabilities     map[string]any
	DataCategories   []string
	DisclosureCount  int
	FirstDisclosedAt time.Time
	LastDisclosedAt  time.Time
}

func (Store) RefreshIntegrationActions(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	now time.Time,
) ([]IntegrationAction, int, int, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return nil, 0, 0, err
	}
	privacyCase, err := lockIntegrationActionCase(ctx, query, tenantID, caseID)
	if err != nil {
		return nil, 0, 0, err
	}
	if !integrationActionCaseEligible(privacyCase) {
		return nil, 0, 0, ErrCaseExecutionUnsupported
	}
	disclosures, err := discoverIntegrationActions(ctx, query, tenantID, caseID)
	if err != nil {
		return nil, 0, 0, err
	}
	existing, err := listIntegrationActions(ctx, query, tenantID, []string{caseID})
	if err != nil {
		return nil, 0, 0, err
	}
	existingByKey := make(map[string]IntegrationAction, len(existing[caseID]))
	for _, action := range existing[caseID] {
		existingByKey[action.DestinationKey] = action
	}

	actionKind := RecipientActionKind(privacyCase.RightType)
	created := 0
	reopened := 0
	for _, disclosure := range disclosures {
		previous, exists := existingByKey[disclosure.DestinationKey]
		if !exists {
			created++
		} else if previous.Status == IntegrationActionCompleted &&
			disclosure.LastDisclosedAt.After(previous.LastDisclosedAt) {
			reopened++
		}
		capabilities, marshalErr := json.Marshal(disclosure.Capabilities)
		if marshalErr != nil {
			return nil, 0, 0, fmt.Errorf("encode integration action capabilities: %w", marshalErr)
		}
		if _, err := query.Exec(ctx, `
			INSERT INTO privacy_integration_actions (
				id, tenant_id, privacy_case_id, integration_id, destination_key,
				destination_kind, destination_name, action_kind, handling_mode, status,
				capability_snapshot, data_categories, disclosure_count,
				first_disclosed_at, last_disclosed_at, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6::integration_kind, $7, $8, 'manual',
				'manual_required', $9::jsonb, $10, $11, $12, $13, $14, $14
			)
			ON CONFLICT (tenant_id, privacy_case_id, destination_key, action_kind)
			DO UPDATE SET
				integration_id = EXCLUDED.integration_id,
				destination_kind = EXCLUDED.destination_kind,
				destination_name = EXCLUDED.destination_name,
				handling_mode = EXCLUDED.handling_mode,
				capability_snapshot = EXCLUDED.capability_snapshot,
				data_categories = EXCLUDED.data_categories,
				disclosure_count = EXCLUDED.disclosure_count,
				first_disclosed_at = EXCLUDED.first_disclosed_at,
				last_disclosed_at = EXCLUDED.last_disclosed_at,
				status = CASE
					WHEN privacy_integration_actions.status = 'completed'
					 AND EXCLUDED.last_disclosed_at > privacy_integration_actions.last_disclosed_at
					THEN 'manual_required'
					ELSE privacy_integration_actions.status
				END,
				completed_by_member_id = CASE
					WHEN privacy_integration_actions.status = 'completed'
					 AND EXCLUDED.last_disclosed_at > privacy_integration_actions.last_disclosed_at
					THEN NULL
					ELSE privacy_integration_actions.completed_by_member_id
				END,
				completed_at = CASE
					WHEN privacy_integration_actions.status = 'completed'
					 AND EXCLUDED.last_disclosed_at > privacy_integration_actions.last_disclosed_at
					THEN NULL
					ELSE privacy_integration_actions.completed_at
				END,
				completion_note = CASE
					WHEN privacy_integration_actions.status = 'completed'
					 AND EXCLUDED.last_disclosed_at > privacy_integration_actions.last_disclosed_at
					THEN NULL
					ELSE privacy_integration_actions.completion_note
				END,
				completion_idempotency_key = CASE
					WHEN privacy_integration_actions.status = 'completed'
					 AND EXCLUDED.last_disclosed_at > privacy_integration_actions.last_disclosed_at
					THEN NULL
					ELSE privacy_integration_actions.completion_idempotency_key
				END,
				completion_fingerprint = CASE
					WHEN privacy_integration_actions.status = 'completed'
					 AND EXCLUDED.last_disclosed_at > privacy_integration_actions.last_disclosed_at
					THEN NULL
					ELSE privacy_integration_actions.completion_fingerprint
				END,
				updated_at = EXCLUDED.updated_at
		`, uuid.NewString(), tenantID, caseID, disclosure.IntegrationID,
			disclosure.DestinationKey, disclosure.DestinationKind, disclosure.DestinationName,
			actionKind, string(capabilities), disclosure.DataCategories,
			disclosure.DisclosureCount, disclosure.FirstDisclosedAt.UTC(),
			disclosure.LastDisclosedAt.UTC(), now.UTC()); err != nil {
			return nil, 0, 0, fmt.Errorf("upsert privacy integration action: %w", err)
		}
	}
	if created > 0 || reopened > 0 {
		eventKey := "integration-actions-refresh:" + uuid.NewString()
		data := map[string]any{
			"created": created, "reopened": reopened, "destination_count": len(disclosures),
		}
		if err := insertCaseEvent(
			ctx, query, tenantID, caseID, eventKey,
			"privacy_case.integration_actions_refreshed", &actorMemberID,
			nil, nil, data, now,
		); err != nil {
			return nil, 0, 0, err
		}
		if err := recordRecipientActionAudit(
			ctx, query, tenantID, "privacy.integration_actions_refreshed",
			actorMemberID, caseID, data,
		); err != nil {
			return nil, 0, 0, err
		}
	}
	actionsByCase, err := listIntegrationActions(ctx, query, tenantID, []string{caseID})
	if err != nil {
		return nil, 0, 0, err
	}
	actions := actionsByCase[caseID]
	if actions == nil {
		actions = []IntegrationAction{}
	}
	return actions, created, reopened, nil
}

func (Store) CompleteIntegrationAction(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actionID string,
	actorMemberID string,
	idempotencyKey string,
	input CompleteIntegrationActionInput,
	now time.Time,
) (IntegrationAction, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return IntegrationAction{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	input.CompletionNote = strings.TrimSpace(input.CompletionNote)
	if idempotencyKey == "" || len(idempotencyKey) > 200 ||
		input.CompletionNote == "" || utf8.RuneCountInString(input.CompletionNote) > 2000 {
		return IntegrationAction{}, false, ErrInvalidRequest
	}
	if _, err := uuid.Parse(actionID); err != nil {
		return IntegrationAction{}, false, ErrInvalidRequest
	}
	privacyCase, err := lockIntegrationActionCase(ctx, query, tenantID, caseID)
	if err != nil {
		return IntegrationAction{}, false, err
	}
	if !integrationActionCaseEligible(privacyCase) {
		return IntegrationAction{}, false, ErrCaseExecutionUnsupported
	}
	if err := recipientActionEvidenceExists(
		ctx, query, privacyCase, RecipientActionKind(privacyCase.RightType),
	); err != nil {
		return IntegrationAction{}, false, err
	}
	fingerprint := sha256.Sum256([]byte(actionID + "\n" + input.CompletionNote))
	action, storedKey, storedFingerprint, err := scanIntegrationAction(query.QueryRow(ctx, `
		SELECT `+integrationActionSelectColumns("action")+`,
		       action.completion_idempotency_key, action.completion_fingerprint
		FROM privacy_integration_actions AS action
		WHERE action.tenant_id = $1 AND action.privacy_case_id = $2 AND action.id = $3
		FOR UPDATE
	`, tenantID, caseID, actionID), true)
	if err != nil {
		return IntegrationAction{}, false, err
	}
	if action.Status == IntegrationActionCompleted {
		if storedKey != nil && *storedKey == idempotencyKey &&
			!bytes.Equal(storedFingerprint, fingerprint[:]) {
			return IntegrationAction{}, false, ErrIdempotencyConflict
		}
		if action.CompletionNote != nil && *action.CompletionNote == input.CompletionNote {
			return action, false, nil
		}
		return IntegrationAction{}, false, ErrIntegrationActionNotManual
	}
	if action.HandlingMode != IntegrationActionManual ||
		action.Status != IntegrationActionManualRequired {
		return IntegrationAction{}, false, ErrIntegrationActionNotManual
	}
	completed, _, _, err := scanIntegrationAction(query.QueryRow(ctx, `
		UPDATE privacy_integration_actions
		SET status = 'completed', completed_by_member_id = $4, completed_at = $5,
		    completion_note = $6, completion_idempotency_key = $7,
		    completion_fingerprint = $8, updated_at = $5
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND id = $3
		RETURNING `+integrationActionSelectColumns("privacy_integration_actions")+`,
		          completion_idempotency_key, completion_fingerprint
	`, tenantID, caseID, actionID, actorMemberID, now.UTC(), input.CompletionNote,
		idempotencyKey, fingerprint[:]), true)
	if err != nil {
		return IntegrationAction{}, false, err
	}
	eventData := map[string]any{
		"integration_action_id": actionID,
		"destination_key":       completed.DestinationKey,
		"action_kind":           completed.ActionKind,
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "integration-action-complete:"+idempotencyKey,
		"privacy_case.integration_action_completed", &actorMemberID,
		nil, nil, eventData, now,
	); err != nil {
		return IntegrationAction{}, false, err
	}
	if err := recordRecipientActionAudit(
		ctx, query, tenantID, "privacy.integration_action_completed",
		actorMemberID, caseID, eventData,
	); err != nil {
		return IntegrationAction{}, false, err
	}
	return completed, true, nil
}

func lockIntegrationActionCase(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) (PrivacyCase, error) {
	return scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
}

func integrationActionCaseEligible(privacyCase PrivacyCase) bool {
	return privacyCase.VerifiedAt != nil &&
		privacyCase.Status != CasePendingVerification &&
		privacyCase.Status != CaseCancelled &&
		privacyCase.Status != CaseRefused &&
		(privacyCase.RightType == RightRectification || privacyCase.RightType == RightErasure)
}

func discoverIntegrationActions(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) ([]integrationActionDiscovery, error) {
	rows, err := query.Query(ctx, `
		WITH selected_case AS (
			SELECT privacy_case.*
			FROM privacy_cases AS privacy_case
			WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		), eligible AS (
			SELECT delivery.integration_id, delivery.delivery_key,
			       delivery.destination_kind::text AS destination_kind,
			       integration.name AS integration_name,
			       delivery.destination_capabilities, delivery.data_categories,
			       delivery.sent_at
			FROM delivery_logs AS delivery
			JOIN outbox_events AS event
			  ON event.tenant_id = delivery.tenant_id
			 AND event.id = delivery.outbox_event_id
			CROSS JOIN selected_case AS privacy_case
			LEFT JOIN integrations AS integration
			  ON integration.tenant_id = delivery.tenant_id
			 AND integration.id = delivery.integration_id
			WHERE delivery.tenant_id = $1
			  AND delivery.delivery_key LIKE 'integration:%'
			  AND delivery.status = 'sent'
			  AND delivery.sent_at IS NOT NULL
			  AND (
				(
					privacy_case.subject_type = 'reporter'
					AND (
						(
							delivery.resource_type = 'feedback'
							AND EXISTS (
								SELECT 1 FROM feedbacks AS feedback
								WHERE feedback.tenant_id = delivery.tenant_id
								  AND feedback.id = delivery.resource_id
								  AND feedback.created_at <= privacy_case.received_at
								  AND feedback.reporter_email IS NOT NULL
								  AND digest(lower(btrim(feedback.reporter_email)), 'sha256')
								      = privacy_case.subject_email_digest
							)
						)
						OR (
							delivery.resource_type = 'issue'
							AND EXISTS (
								SELECT 1 FROM issues AS issue
								WHERE issue.tenant_id = delivery.tenant_id
								  AND issue.id = delivery.resource_id
								  AND issue.created_at <= privacy_case.received_at
								  AND (
									(
										issue.reporter_email IS NOT NULL
										AND digest(lower(btrim(issue.reporter_email)), 'sha256')
										    = privacy_case.subject_email_digest
									)
									OR EXISTS (
										SELECT 1 FROM feedbacks AS feedback
										WHERE feedback.tenant_id = issue.tenant_id
										  AND feedback.issue_id = issue.id
										  AND feedback.created_at <= privacy_case.received_at
										  AND feedback.reporter_email IS NOT NULL
										  AND digest(lower(btrim(feedback.reporter_email)), 'sha256')
										      = privacy_case.subject_email_digest
									)
								  )
							)
						)
					)
				)
				OR (
					privacy_case.subject_type = 'member'
					AND delivery.resource_type = 'issue'
					AND 'workflow_context' = ANY(delivery.data_categories)
					AND EXISTS (
						SELECT 1
						FROM issue_activities AS activity
						WHERE activity.tenant_id = event.tenant_id
						  AND activity.issue_id = event.resource_id
						  AND activity.actor_member_id = privacy_case.subject_member_id
						  AND (
							(
								event.event_key IS NOT NULL
								AND (
									activity.id::text = event.event_key
									OR activity.data->>'legacy_update_id' = event.event_key
								)
							)
							OR (
								event.event_key IS NULL
								AND event.event = 'issue.created'
								AND activity.kind = 'issue.created'
							)
						  )
					)
				)
			  )
		)
		SELECT eligible.delivery_key,
		       (
		           array_agg(eligible.integration_id::text ORDER BY eligible.sent_at DESC)
		           FILTER (WHERE eligible.integration_id IS NOT NULL)
		       )[1],
		       (
		           array_agg(eligible.destination_kind ORDER BY eligible.sent_at DESC)
		           FILTER (WHERE eligible.destination_kind IS NOT NULL)
		       )[1],
		       COALESCE(
		           (
		               array_agg(eligible.integration_name ORDER BY eligible.sent_at DESC)
		               FILTER (WHERE eligible.integration_name IS NOT NULL)
		           )[1],
		           'Removed ' || COALESCE(
		               (
		                   array_agg(eligible.destination_kind ORDER BY eligible.sent_at DESC)
		                   FILTER (WHERE eligible.destination_kind IS NOT NULL)
		               )[1],
		               'integration'
		           ) || ' destination'
		       ),
		       (array_agg(eligible.destination_capabilities ORDER BY eligible.sent_at DESC))[1],
		       ARRAY(
		           SELECT DISTINCT category
		           FROM eligible AS selected
		           CROSS JOIN LATERAL unnest(selected.data_categories) AS category
		           WHERE selected.delivery_key = eligible.delivery_key
		           ORDER BY category
		       ),
		       count(*)::integer, min(eligible.sent_at), max(eligible.sent_at)
		FROM eligible
		GROUP BY eligible.delivery_key
		ORDER BY max(eligible.sent_at) DESC, eligible.delivery_key
	`, tenantID, caseID)
	if err != nil {
		return nil, fmt.Errorf("discover privacy integration actions: %w", err)
	}
	defer rows.Close()
	disclosures := make([]integrationActionDiscovery, 0)
	for rows.Next() {
		var disclosure integrationActionDiscovery
		var capabilities []byte
		if err := rows.Scan(
			&disclosure.DestinationKey, &disclosure.IntegrationID,
			&disclosure.DestinationKind, &disclosure.DestinationName,
			&capabilities, &disclosure.DataCategories, &disclosure.DisclosureCount,
			&disclosure.FirstDisclosedAt, &disclosure.LastDisclosedAt,
		); err != nil {
			return nil, fmt.Errorf("scan privacy integration action discovery: %w", err)
		}
		disclosure.Capabilities = make(map[string]any)
		if err := json.Unmarshal(capabilities, &disclosure.Capabilities); err != nil {
			return nil, fmt.Errorf("decode privacy integration action capabilities: %w", err)
		}
		disclosures = append(disclosures, disclosure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy integration action discoveries: %w", err)
	}
	return disclosures, nil
}

func listIntegrationActions(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseIDs []string,
) (map[string][]IntegrationAction, error) {
	actions := make(map[string][]IntegrationAction, len(caseIDs))
	if len(caseIDs) == 0 {
		return actions, nil
	}
	rows, err := query.Query(ctx, `
		SELECT `+integrationActionSelectColumns("action")+`
		FROM privacy_integration_actions AS action
		WHERE action.tenant_id = $1
		  AND action.privacy_case_id = ANY($2::uuid[])
		ORDER BY action.last_disclosed_at DESC, action.id DESC
	`, tenantID, caseIDs)
	if err != nil {
		return nil, fmt.Errorf("list privacy integration actions: %w", err)
	}
	defer rows.Close()
	allActions := make([]IntegrationAction, 0)
	for rows.Next() {
		action, _, _, scanErr := scanIntegrationAction(rows, false)
		if scanErr != nil {
			return nil, scanErr
		}
		allActions = append(allActions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy integration actions: %w", err)
	}
	actionIDs := make([]string, len(allActions))
	for index, action := range allActions {
		actionIDs[index] = action.ID
	}
	if len(actionIDs) > 0 {
		recipientRows, err := query.Query(ctx, `
			SELECT privacy_integration_action_id, id::text
			FROM privacy_recipient_actions
			WHERE tenant_id = $1
			  AND privacy_integration_action_id = ANY($2::uuid[])
			ORDER BY created_at, id
		`, tenantID, actionIDs)
		if err != nil {
			return nil, fmt.Errorf("list linked privacy recipient actions: %w", err)
		}
		defer recipientRows.Close()
		linked := make(map[string][]string)
		for recipientRows.Next() {
			var actionID, recipientID string
			if err := recipientRows.Scan(&actionID, &recipientID); err != nil {
				return nil, fmt.Errorf("scan linked privacy recipient action: %w", err)
			}
			linked[actionID] = append(linked[actionID], recipientID)
		}
		if err := recipientRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate linked privacy recipient actions: %w", err)
		}
		for index := range allActions {
			allActions[index].LinkedRecipientActionIDs = linked[allActions[index].ID]
			if allActions[index].LinkedRecipientActionIDs == nil {
				allActions[index].LinkedRecipientActionIDs = []string{}
			}
		}
	}
	for _, action := range allActions {
		actions[action.PrivacyCaseID] = append(actions[action.PrivacyCaseID], action)
	}
	return actions, nil
}

func integrationActionSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.privacy_case_id, ` + alias + `.integration_id, ` +
		alias + `.destination_key, ` + alias + `.destination_kind::text, ` +
		alias + `.destination_name, ` + alias + `.action_kind, ` + alias + `.handling_mode, ` +
		alias + `.status, ` + alias + `.capability_snapshot, ` + alias + `.data_categories, ` +
		alias + `.disclosure_count, ` + alias + `.first_disclosed_at, ` +
		alias + `.last_disclosed_at, ` + alias + `.completed_by_member_id, ` +
		alias + `.completed_at, ` + alias + `.completion_note, ` +
		alias + `.created_at, ` + alias + `.updated_at`
}

func scanIntegrationAction(
	row rowScanner,
	withCompletionEvidence bool,
) (IntegrationAction, *string, []byte, error) {
	var action IntegrationAction
	var capabilities []byte
	destinations := []any{
		&action.ID, &action.PrivacyCaseID, &action.IntegrationID,
		&action.DestinationKey, &action.DestinationKind, &action.DestinationName,
		&action.ActionKind, &action.HandlingMode, &action.Status, &capabilities,
		&action.DataCategories, &action.DisclosureCount, &action.FirstDisclosedAt,
		&action.LastDisclosedAt, &action.CompletedByMemberID, &action.CompletedAt,
		&action.CompletionNote, &action.CreatedAt, &action.UpdatedAt,
	}
	var completionKey *string
	var completionFingerprint []byte
	if withCompletionEvidence {
		destinations = append(destinations, &completionKey, &completionFingerprint)
	}
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return IntegrationAction{}, nil, nil, ErrNotFound
	} else if err != nil {
		return IntegrationAction{}, nil, nil, fmt.Errorf("scan privacy integration action: %w", err)
	}
	action.CapabilitySnapshot = make(map[string]any)
	if err := json.Unmarshal(capabilities, &action.CapabilitySnapshot); err != nil {
		return IntegrationAction{}, nil, nil, fmt.Errorf("decode privacy integration action capabilities: %w", err)
	}
	action.LinkedRecipientActionIDs = []string{}
	return action, completionKey, completionFingerprint, nil
}
