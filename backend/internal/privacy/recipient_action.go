package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/outbox"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

const TaskRecipientNotify = "privacy:recipient-notify"

type RecipientTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
}

type RecipientOutboxResolver struct{}

func (RecipientOutboxResolver) Resolve(
	_ context.Context,
	event outbox.Event,
) ([]outbox.Destination, error) {
	if event.Event != "privacy.recipient_action.requested" ||
		event.ResourceType != "privacy_recipient_action" {
		return nil, nil
	}
	return []outbox.Destination{{
		Kind:     "privacy_recipient_email",
		ID:       event.ResourceID,
		TaskType: TaskRecipientNotify,
		Queue:    email.QueueName,
	}}, nil
}

func (Store) CreateRecipientAction(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	input CreateRecipientActionInput,
	now time.Time,
) (RecipientAction, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return RecipientAction{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	normalized, err := normalizeRecipientActionInput(input)
	if err != nil || idempotencyKey == "" || len(idempotencyKey) > 200 {
		return RecipientAction{}, false, ErrInvalidRequest
	}
	fingerprint, err := recipientActionFingerprint(normalized)
	if err != nil {
		return RecipientAction{}, false, err
	}
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return RecipientAction{}, false, err
	}
	if err := recipientActionEvidenceExists(ctx, query, privacyCase, normalized.ActionKind); err != nil {
		return RecipientAction{}, false, err
	}
	if normalized.IntegrationActionID != "" {
		var linkedKind RecipientActionKind
		if err := query.QueryRow(ctx, `
			SELECT action_kind
			FROM privacy_integration_actions
			WHERE tenant_id = $1 AND privacy_case_id = $2 AND id = $3
		`, tenantID, caseID, normalized.IntegrationActionID).Scan(&linkedKind); errors.Is(err, pgx.ErrNoRows) {
			return RecipientAction{}, false, ErrNotFound
		} else if err != nil {
			return RecipientAction{}, false, fmt.Errorf("load linked integration action: %w", err)
		} else if linkedKind != normalized.ActionKind {
			return RecipientAction{}, false, ErrInvalidRequest
		}
	}

	actionID := uuid.NewString()
	var storedFingerprint []byte
	var created bool
	action, err := scanRecipientAction(query.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO privacy_recipient_actions (
				id, tenant_id, privacy_case_id, action_kind, recipient_name,
				recipient_email, instruction_summary, status, created_by_member_id,
				privacy_integration_action_id, idempotency_key, request_fingerprint,
				created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, 'pending', $8,
				NULLIF($9, '')::uuid, $10, $11, $12, $12
			)
			ON CONFLICT (tenant_id, privacy_case_id, idempotency_key) DO NOTHING
			RETURNING *, true AS created
		), selected AS (
			SELECT * FROM inserted
			UNION ALL
			SELECT existing.*, false AS created
			FROM privacy_recipient_actions AS existing
			WHERE existing.tenant_id = $2
			  AND existing.privacy_case_id = $3
			  AND existing.idempotency_key = $10
			  AND NOT EXISTS (SELECT 1 FROM inserted)
		)
		SELECT `+recipientActionSelectColumns("selected")+`,
		       selected.request_fingerprint, selected.created
		FROM selected
	`, actionID, tenantID, caseID, normalized.ActionKind, normalized.RecipientName,
		normalized.RecipientEmail, normalized.InstructionSummary, actorMemberID,
		normalized.IntegrationActionID, idempotencyKey, fingerprint[:], now.UTC()),
		&storedFingerprint, &created)
	if err != nil {
		return RecipientAction{}, false, fmt.Errorf("create privacy recipient action: %w", err)
	}
	if !bytes.Equal(storedFingerprint, fingerprint[:]) {
		return RecipientAction{}, false, ErrIdempotencyConflict
	}
	if !created {
		action.DeliveryAttempts, err = listRecipientDeliveries(
			ctx, query, tenantID, action.ID,
		)
		return action, false, err
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (
			tenant_id, event, resource_type, resource_id, event_key, data
		) VALUES (
			$1, 'privacy.recipient_action.requested',
			'privacy_recipient_action', $2, 'create',
			jsonb_build_object('privacy_case_id', $3::text)
		)
	`, tenantID, action.ID, caseID); err != nil {
		return RecipientAction{}, false, fmt.Errorf("queue privacy recipient action: %w", err)
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "recipient-action:"+idempotencyKey,
		"privacy_case.recipient_action_created", &actorMemberID, nil, nil,
		map[string]any{
			"recipient_action_id":   action.ID,
			"integration_action_id": action.IntegrationActionID,
			"action_kind":           action.ActionKind,
		}, now,
	); err != nil {
		return RecipientAction{}, false, err
	}
	if err := recordRecipientActionAudit(
		ctx, query, tenantID, "privacy.recipient_action_created",
		actorMemberID, caseID, map[string]any{
			"recipient_action_id":   action.ID,
			"integration_action_id": action.IntegrationActionID,
			"action_kind":           action.ActionKind,
		},
	); err != nil {
		return RecipientAction{}, false, err
	}
	action.DeliveryAttempts = []RecipientDelivery{}
	return action, true, nil
}

func recordRecipientActionAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind string,
	actorMemberID string,
	caseID string,
	data map[string]any,
) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode recipient action audit: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id, data
		) VALUES (
			$1, $2, $3, NULL,
			$4::jsonb || jsonb_build_object('privacy_case_id', $5::text)
		)
	`, tenantID, kind, actorMemberID, string(encoded), caseID); err != nil {
		return fmt.Errorf("record recipient action audit: %w", err)
	}
	return nil
}

func (Store) RetryRecipientAction(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actionID string,
	actorMemberID string,
	idempotencyKey string,
	now time.Time,
) (RecipientAction, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return RecipientAction{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return RecipientAction{}, false, ErrInvalidRequest
	}
	eventKey := "retry:" + idempotencyKey
	var existingEvent bool
	err := query.QueryRow(ctx, `
		SELECT true
		FROM outbox_events
		WHERE tenant_id = $1 AND resource_type = 'privacy_recipient_action'
		  AND resource_id = $2 AND event_key = $3
	`, tenantID, actionID, eventKey).Scan(&existingEvent)
	if err == nil {
		action, loadErr := loadRecipientAction(ctx, query, tenantID, caseID, actionID, true)
		return action, false, loadErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return RecipientAction{}, false, fmt.Errorf("check recipient action retry: %w", err)
	}
	current, err := scanRecipientAction(query.QueryRow(ctx, `
		SELECT `+recipientActionSelectColumns("action")+`
		FROM privacy_recipient_actions AS action
		WHERE action.tenant_id = $1 AND action.privacy_case_id = $2 AND action.id = $3
		FOR UPDATE
	`, tenantID, caseID, actionID))
	if err != nil {
		return RecipientAction{}, false, err
	}
	if current.Status != RecipientActionFailed {
		return RecipientAction{}, false, ErrRecipientActionNotRetryable
	}
	action, err := scanRecipientAction(query.QueryRow(ctx, `
		UPDATE privacy_recipient_actions
		SET status = 'pending', last_error = NULL, updated_at = $4
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND id = $3
		RETURNING `+recipientActionSelectColumns("privacy_recipient_actions")+`
	`, tenantID, caseID, actionID, now.UTC()))
	if err != nil {
		return RecipientAction{}, false, err
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (
			tenant_id, event, resource_type, resource_id, event_key, data
		) VALUES (
			$1, 'privacy.recipient_action.requested',
			'privacy_recipient_action', $2, $3,
			jsonb_build_object('privacy_case_id', $4::text)
		)
	`, tenantID, actionID, eventKey, caseID); err != nil {
		return RecipientAction{}, false, fmt.Errorf("queue recipient action retry: %w", err)
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "recipient-action-retry:"+idempotencyKey,
		"privacy_case.recipient_action_retried", &actorMemberID, nil, nil,
		map[string]any{"recipient_action_id": actionID}, now,
	); err != nil {
		return RecipientAction{}, false, err
	}
	action.DeliveryAttempts, err = listRecipientDeliveries(ctx, query, tenantID, actionID)
	return action, true, err
}

func normalizeRecipientActionInput(
	input CreateRecipientActionInput,
) (CreateRecipientActionInput, error) {
	input.RecipientName = strings.TrimSpace(input.RecipientName)
	input.RecipientEmail = strings.TrimSpace(input.RecipientEmail)
	input.InstructionSummary = strings.TrimSpace(input.InstructionSummary)
	input.IntegrationActionID = strings.TrimSpace(input.IntegrationActionID)
	if input.ActionKind != RecipientActionRectification &&
		input.ActionKind != RecipientActionErasure ||
		input.RecipientName == "" || len(input.RecipientName) > 200 ||
		input.InstructionSummary == "" || len(input.InstructionSummary) > 2000 ||
		len(input.RecipientEmail) > 320 {
		return CreateRecipientActionInput{}, ErrInvalidRequest
	}
	if input.IntegrationActionID != "" {
		if _, err := uuid.Parse(input.IntegrationActionID); err != nil {
			return CreateRecipientActionInput{}, ErrInvalidRequest
		}
	}
	address, err := mail.ParseAddress(input.RecipientEmail)
	if err != nil || !strings.EqualFold(address.Address, input.RecipientEmail) {
		return CreateRecipientActionInput{}, ErrInvalidRequest
	}
	input.RecipientEmail = strings.ToLower(address.Address)
	return input, nil
}

func recipientActionFingerprint(input CreateRecipientActionInput) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode recipient action input: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func recipientActionEvidenceExists(
	ctx context.Context,
	query db.Querier,
	privacyCase PrivacyCase,
	kind RecipientActionKind,
) error {
	if privacyCase.VerifiedAt == nil || string(privacyCase.RightType) != string(kind) {
		return ErrRecipientActionEvidence
	}
	if kind == RecipientActionRectification {
		var count int
		if err := query.QueryRow(ctx, `
			SELECT
				(SELECT count(*) FROM privacy_case_rectifications
				 WHERE tenant_id = $1 AND privacy_case_id = $2)
			  + (SELECT count(*) FROM privacy_case_feedback_rectifications
				 WHERE tenant_id = $1 AND privacy_case_id = $2)
		`, privacyCase.TenantID, privacyCase.ID).Scan(&count); err != nil {
			return fmt.Errorf("check rectification evidence: %w", err)
		}
		if count == 0 {
			return ErrRecipientActionEvidence
		}
		return nil
	}
	if privacyCase.Status != CaseCompleted && privacyCase.Status != CasePartiallyFulfilled {
		return ErrRecipientActionEvidence
	}
	execution, err := loadCaseExecution(ctx, query, privacyCase.TenantID, privacyCase.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrRecipientActionEvidence
		}
		return err
	}
	if execution.RequestStatus == nil || *execution.RequestStatus != ReporterCompleted ||
		execution.JobStatus == nil || *execution.JobStatus != JobCompleted ||
		execution.ErasureReview == nil || execution.ErasureReview.Pending != 0 {
		return ErrRecipientActionEvidence
	}
	return nil
}

func loadRecipientAction(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actionID string,
	withAttempts bool,
) (RecipientAction, error) {
	action, err := scanRecipientAction(query.QueryRow(ctx, `
		SELECT `+recipientActionSelectColumns("action")+`
		FROM privacy_recipient_actions AS action
		WHERE action.tenant_id = $1 AND action.privacy_case_id = $2 AND action.id = $3
	`, tenantID, caseID, actionID))
	if err != nil {
		return RecipientAction{}, err
	}
	if withAttempts {
		action.DeliveryAttempts, err = listRecipientDeliveries(
			ctx, query, tenantID, actionID,
		)
	}
	return action, err
}

func listRecipientActions(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseIDs []string,
) (map[string][]RecipientAction, error) {
	actions := make(map[string][]RecipientAction, len(caseIDs))
	rows, err := query.Query(ctx, `
		SELECT `+recipientActionSelectColumns("action")+`
		FROM privacy_recipient_actions AS action
		WHERE action.tenant_id = $1 AND action.privacy_case_id = ANY($2::uuid[])
		ORDER BY action.created_at DESC, action.id DESC
	`, tenantID, caseIDs)
	if err != nil {
		return nil, fmt.Errorf("list privacy recipient actions: %w", err)
	}
	allActions := make([]RecipientAction, 0)
	actionIDs := make([]string, 0)
	for rows.Next() {
		action, scanErr := scanRecipientAction(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		allActions = append(allActions, action)
		actionIDs = append(actionIDs, action.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate privacy recipient actions: %w", err)
	}
	rows.Close()
	deliveries, err := listRecipientDeliveriesForActions(
		ctx, query, tenantID, actionIDs,
	)
	if err != nil {
		return nil, err
	}
	for _, action := range allActions {
		action.DeliveryAttempts = deliveries[action.ID]
		if action.DeliveryAttempts == nil {
			action.DeliveryAttempts = []RecipientDelivery{}
		}
		actions[action.PrivacyCaseID] = append(actions[action.PrivacyCaseID], action)
	}
	return actions, nil
}

func listRecipientDeliveriesForActions(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actionIDs []string,
) (map[string][]RecipientDelivery, error) {
	result := make(map[string][]RecipientDelivery, len(actionIDs))
	if len(actionIDs) == 0 {
		return result, nil
	}
	rows, err := query.Query(ctx, `
		SELECT privacy_recipient_action_id, id, attempt_number, status,
		       provider_message_id, error_message, started_at, completed_at
		FROM privacy_recipient_action_attempts
		WHERE tenant_id = $1 AND privacy_recipient_action_id = ANY($2::uuid[])
		ORDER BY privacy_recipient_action_id, attempt_number DESC
	`, tenantID, actionIDs)
	if err != nil {
		return nil, fmt.Errorf("list recipient delivery attempts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var actionID string
		var attempt RecipientDelivery
		if err := rows.Scan(
			&actionID, &attempt.ID, &attempt.AttemptNumber, &attempt.Status,
			&attempt.ProviderMessageID, &attempt.ErrorMessage,
			&attempt.StartedAt, &attempt.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recipient delivery attempt: %w", err)
		}
		result[actionID] = append(result[actionID], attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipient delivery attempts: %w", err)
	}
	return result, nil
}

func listRecipientDeliveries(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actionID string,
) ([]RecipientDelivery, error) {
	rows, err := query.Query(ctx, `
		SELECT id, attempt_number, status, provider_message_id, error_message,
		       started_at, completed_at
		FROM privacy_recipient_action_attempts
		WHERE tenant_id = $1 AND privacy_recipient_action_id = $2
		ORDER BY attempt_number DESC
	`, tenantID, actionID)
	if err != nil {
		return nil, fmt.Errorf("list recipient delivery attempts: %w", err)
	}
	defer rows.Close()
	attempts := make([]RecipientDelivery, 0)
	for rows.Next() {
		var attempt RecipientDelivery
		if err := rows.Scan(
			&attempt.ID, &attempt.AttemptNumber, &attempt.Status,
			&attempt.ProviderMessageID, &attempt.ErrorMessage,
			&attempt.StartedAt, &attempt.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recipient delivery attempt: %w", err)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recipient delivery attempts: %w", err)
	}
	return attempts, nil
}

func recipientActionSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.privacy_case_id, ` + alias + `.action_kind, ` +
		alias + `.recipient_name, ` + alias + `.recipient_email, ` +
		alias + `.instruction_summary, ` + alias + `.status, ` + alias + `.attempts, ` +
		alias + `.last_error, ` + alias + `.provider_message_id, ` +
		alias + `.last_attempt_at, ` + alias + `.sent_at, ` +
		alias + `.created_by_member_id, ` + alias + `.privacy_integration_action_id, ` +
		alias + `.created_at, ` + alias + `.updated_at`
}

func scanRecipientAction(row rowScanner, extra ...any) (RecipientAction, error) {
	var action RecipientAction
	destinations := []any{
		&action.ID, &action.PrivacyCaseID, &action.ActionKind,
		&action.RecipientName, &action.RecipientEmail, &action.InstructionSummary,
		&action.Status, &action.Attempts, &action.LastError,
		&action.ProviderMessageID, &action.LastAttemptAt, &action.SentAt,
		&action.CreatedByMemberID, &action.IntegrationActionID,
		&action.CreatedAt, &action.UpdatedAt,
	}
	destinations = append(destinations, extra...)
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return RecipientAction{}, ErrNotFound
	} else if err != nil {
		return RecipientAction{}, fmt.Errorf("scan privacy recipient action: %w", err)
	}
	return action, nil
}

type RecipientNotification struct {
	OutboxEventID string
	TenantID      string
	TenantName    string
	CasePublicID  string
	Action        RecipientAction
}

type RecipientHandler struct {
	Store         Store
	Sender        email.Sender
	From          string
	PublicBaseURL string
	Now           func() time.Time
}

func (handler RecipientHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload RecipientTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.OutboxEventID == "" || payload.TenantID == "" || payload.ResourceID == "" {
			return fmt.Errorf("decode recipient notification task: %w", asynq.SkipRetry)
		}
		notification, err := handler.Store.LoadRecipientNotification(ctx, payload)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("recipient notification no longer exists: %w", asynq.SkipRetry)
		}
		if err != nil {
			return err
		}
		if notification.Action.Status == RecipientActionSent {
			return nil
		}
		attempt, completed, err := handler.Store.StartRecipientDelivery(
			ctx, notification, handler.now(),
		)
		if err != nil || completed {
			return err
		}
		message, err := recipientActionMessage(notification, handler)
		if err != nil {
			markErr := handler.Store.MarkRecipientDeliveryFailed(
				ctx, notification, attempt.ID, handler.now(),
			)
			return errors.Join(fmt.Errorf("compose recipient notification: %w", err), markErr)
		}
		providerID, err := handler.Sender.Send(ctx, message)
		if err != nil {
			markErr := handler.Store.MarkRecipientDeliveryFailed(
				ctx, notification, attempt.ID, handler.now(),
			)
			return errors.Join(fmt.Errorf("send recipient notification: %w", err), markErr)
		}
		return handler.Store.MarkRecipientDeliverySent(
			ctx, notification, attempt.ID, providerID, handler.now(),
		)
	}
}

func (store Store) LoadRecipientNotification(
	ctx context.Context,
	task RecipientTask,
) (RecipientNotification, error) {
	var notification RecipientNotification
	var action RecipientAction
	err := store.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, privacy_case.public_id,
		       `+recipientActionSelectColumns("action")+`
		FROM outbox_events AS event
		JOIN privacy_recipient_actions AS action
		  ON action.tenant_id = event.tenant_id AND action.id = event.resource_id
		JOIN privacy_cases AS privacy_case
		  ON privacy_case.tenant_id = action.tenant_id
		 AND privacy_case.id = action.privacy_case_id
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		WHERE event.id = $1 AND event.tenant_id = $2 AND event.resource_id = $3
		  AND event.event = 'privacy.recipient_action.requested'
		  AND event.resource_type = 'privacy_recipient_action'
	`, task.OutboxEventID, task.TenantID, task.ResourceID).Scan(
		&notification.OutboxEventID, &notification.TenantID,
		&notification.TenantName, &notification.CasePublicID,
		&action.ID, &action.PrivacyCaseID, &action.ActionKind,
		&action.RecipientName, &action.RecipientEmail, &action.InstructionSummary,
		&action.Status, &action.Attempts, &action.LastError,
		&action.ProviderMessageID, &action.LastAttemptAt, &action.SentAt,
		&action.CreatedByMemberID, &action.IntegrationActionID,
		&action.CreatedAt, &action.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecipientNotification{}, ErrNotFound
	}
	if err != nil {
		return RecipientNotification{}, fmt.Errorf("load recipient notification: %w", err)
	}
	notification.Action = action
	return notification, nil
}

func (store Store) StartRecipientDelivery(
	ctx context.Context,
	notification RecipientNotification,
	now time.Time,
) (RecipientDelivery, bool, error) {
	var attempt RecipientDelivery
	var completed bool
	var inProgress bool
	err := store.Database.QueryRow(ctx, `
		WITH locked AS (
			SELECT id, status, last_attempt_at
			FROM privacy_recipient_actions
			WHERE tenant_id = $1 AND id = $2
			FOR UPDATE
		), closed AS (
			UPDATE privacy_recipient_action_attempts
			SET status = 'failed', error_message = 'delivery interrupted',
			    completed_at = $3
			WHERE tenant_id = $1 AND privacy_recipient_action_id = $2
			  AND status = 'pending'
			  AND EXISTS (
			      SELECT 1 FROM locked
			      WHERE status IN ('pending', 'failed')
			         OR (status = 'processing'
			             AND last_attempt_at <= $3::timestamptz - interval '5 minutes')
			  )
		), next_attempt AS (
			SELECT COALESCE(max(attempt_number), 0) + 1 AS number
			FROM privacy_recipient_action_attempts
			WHERE tenant_id = $1 AND privacy_recipient_action_id = $2
		), inserted AS (
			INSERT INTO privacy_recipient_action_attempts (
				tenant_id, privacy_recipient_action_id, attempt_number, started_at
			)
			SELECT $1, $2, next_attempt.number, $3
			FROM next_attempt, locked
			WHERE locked.status IN ('pending', 'failed')
			   OR (locked.status = 'processing'
			       AND locked.last_attempt_at <= $3::timestamptz - interval '5 minutes')
			RETURNING id, attempt_number, status, started_at
		), updated AS (
			UPDATE privacy_recipient_actions
			SET status = 'processing', attempts = inserted.attempt_number,
			    last_error = NULL, last_attempt_at = $3, updated_at = $3
			FROM inserted
			WHERE privacy_recipient_actions.tenant_id = $1
			  AND privacy_recipient_actions.id = $2
		)
		SELECT inserted.id::text, inserted.attempt_number, inserted.status,
		       inserted.started_at, false, false
		FROM inserted
		UNION ALL
		SELECT '', 0, 'sent'::privacy_recipient_delivery_status,
		       $3, true, false
		FROM locked WHERE locked.status = 'sent'
		UNION ALL
		SELECT '', 0, 'pending'::privacy_recipient_delivery_status,
		       $3, false, true
		FROM locked
		WHERE locked.status = 'processing'
		  AND locked.last_attempt_at > $3::timestamptz - interval '5 minutes'
	`, notification.TenantID, notification.Action.ID, now.UTC()).Scan(
		&attempt.ID, &attempt.AttemptNumber, &attempt.Status,
		&attempt.StartedAt, &completed, &inProgress,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RecipientDelivery{}, false, ErrNotFound
	}
	if err != nil {
		return RecipientDelivery{}, false, fmt.Errorf("start recipient delivery: %w", err)
	}
	if inProgress {
		return RecipientDelivery{}, false, ErrRecipientDeliveryInProgress
	}
	return attempt, completed, nil
}

func (store Store) MarkRecipientDeliverySent(
	ctx context.Context,
	notification RecipientNotification,
	attemptID string,
	providerMessageID string,
	now time.Time,
) error {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if providerMessageID == "" || len(providerMessageID) > 500 {
		return ErrInvalidRequest
	}
	command, err := store.Database.Exec(ctx, `
		WITH completed AS (
			UPDATE privacy_recipient_action_attempts
			SET status = 'sent', provider_message_id = $4,
			    error_message = NULL, completed_at = $5
			WHERE tenant_id = $1 AND privacy_recipient_action_id = $2
			  AND id = $3 AND status = 'pending'
			RETURNING id
		)
		UPDATE privacy_recipient_actions
		SET status = 'sent', provider_message_id = $4, last_error = NULL,
		    sent_at = $5, updated_at = $5
		WHERE tenant_id = $1 AND id = $2 AND EXISTS (SELECT 1 FROM completed)
	`, notification.TenantID, notification.Action.ID, attemptID,
		providerMessageID, now.UTC())
	if err != nil {
		return fmt.Errorf("mark recipient delivery sent: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (store Store) MarkRecipientDeliveryFailed(
	ctx context.Context,
	notification RecipientNotification,
	attemptID string,
	now time.Time,
) error {
	const publicError = "email provider delivery failed"
	command, err := store.Database.Exec(ctx, `
		WITH completed AS (
			UPDATE privacy_recipient_action_attempts
			SET status = 'failed', provider_message_id = NULL,
			    error_message = $4, completed_at = $5
			WHERE tenant_id = $1 AND privacy_recipient_action_id = $2
			  AND id = $3 AND status = 'pending'
			RETURNING id
		)
		UPDATE privacy_recipient_actions
		SET status = 'failed', provider_message_id = NULL, last_error = $4,
		    sent_at = NULL, updated_at = $5
		WHERE tenant_id = $1 AND id = $2 AND EXISTS (SELECT 1 FROM completed)
	`, notification.TenantID, notification.Action.ID, attemptID,
		publicError, now.UTC())
	if err != nil {
		return fmt.Errorf("mark recipient delivery failed: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func recipientActionMessage(
	notification RecipientNotification,
	handler RecipientHandler,
) (email.Message, error) {
	logoURL, err := email.BrandLogoURL(handler.PublicBaseURL)
	if err != nil {
		return email.Message{}, err
	}
	actionLabel := "rectification"
	if notification.Action.ActionKind == RecipientActionErasure {
		actionLabel = "erasure"
	}
	subject := "Data-rights action " + notification.CasePublicID
	return email.Compose(email.Message{
		From: handler.From, To: notification.Action.RecipientEmail,
		Subject:        subject,
		IdempotencyKey: "privacy-recipient:" + notification.Action.ID,
	}, email.Content{
		Preheader: subject,
		Heading:   "Data-rights action",
		Workspace: notification.TenantName,
		Body: []string{
			"An authorized workspace operator recorded a " + actionLabel +
				" action for case " + notification.CasePublicID + ".",
			notification.Action.InstructionSummary,
		},
		Metadata: []email.Metadata{
			{Label: "Case reference", Value: notification.CasePublicID},
			{Label: "Action", Value: actionLabel},
		},
		Footer:  "This operational notice contains only the approved instruction and case reference.",
		LogoURL: logoURL,
	})
}

func (handler RecipientHandler) now() time.Time {
	if handler.Now != nil {
		return handler.Now().UTC()
	}
	return time.Now().UTC()
}
