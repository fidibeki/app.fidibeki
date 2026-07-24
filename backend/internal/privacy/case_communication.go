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

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

const TaskCaseCommunicationNotify = "privacy:case-communication-notify"

type CaseCommunicationTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
}

type CaseCommunicationOutboxResolver struct{}

func (CaseCommunicationOutboxResolver) Resolve(
	_ context.Context,
	event outbox.Event,
) ([]outbox.Destination, error) {
	if event.Event != "privacy.case_communication.requested" ||
		event.ResourceType != "privacy_case_communication" {
		return nil, nil
	}
	return []outbox.Destination{{
		Kind:     "privacy_case_email",
		ID:       event.ResourceID,
		TaskType: TaskCaseCommunicationNotify,
		Queue:    email.QueueName,
	}}, nil
}

func (Store) CreateCaseCommunication(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	input CreateCaseCommunicationInput,
	now time.Time,
) (CaseCommunication, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return CaseCommunication{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	normalized, err := normalizeCaseCommunicationInput(input)
	if err != nil || idempotencyKey == "" || len(idempotencyKey) > 200 {
		return CaseCommunication{}, false, ErrInvalidRequest
	}
	fingerprint, err := caseCommunicationFingerprint(normalized)
	if err != nil {
		return CaseCommunication{}, false, err
	}
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return CaseCommunication{}, false, err
	}
	if err := validateCaseCommunicationState(privacyCase, normalized.CommunicationKind); err != nil {
		return CaseCommunication{}, false, err
	}
	recipientEmail, err := caseCommunicationRecipient(
		ctx, query, tenantID, privacyCase,
	)
	if err != nil {
		return CaseCommunication{}, false, err
	}

	communicationID := uuid.NewString()
	var storedFingerprint []byte
	var created bool
	communication, err := scanCaseCommunication(query.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO privacy_case_communications (
				id, tenant_id, privacy_case_id, communication_kind, recipient_email,
				subject_line, message_body, status, created_by_member_id,
				idempotency_key, request_fingerprint, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9, $10, $11, $11
			)
			ON CONFLICT (tenant_id, privacy_case_id, idempotency_key) DO NOTHING
			RETURNING *, true AS created
		), selected AS (
			SELECT * FROM inserted
			UNION ALL
			SELECT existing.*, false AS created
			FROM privacy_case_communications AS existing
			WHERE existing.tenant_id = $2
			  AND existing.privacy_case_id = $3
			  AND existing.idempotency_key = $9
			  AND NOT EXISTS (SELECT 1 FROM inserted)
		)
		SELECT `+caseCommunicationSelectColumns("selected")+`,
		       selected.request_fingerprint, selected.created
		FROM selected
	`, communicationID, tenantID, caseID, normalized.CommunicationKind,
		recipientEmail, normalized.SubjectLine, normalized.MessageBody,
		actorMemberID, idempotencyKey, fingerprint[:], now.UTC(),
	), &storedFingerprint, &created)
	if err != nil {
		return CaseCommunication{}, false, fmt.Errorf("create privacy case communication: %w", err)
	}
	if !bytes.Equal(storedFingerprint, fingerprint[:]) {
		return CaseCommunication{}, false, ErrIdempotencyConflict
	}
	if !created {
		communication.DeliveryAttempts, err = listCaseCommunicationDeliveries(
			ctx, query, tenantID, communication.ID,
		)
		return communication, false, err
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (
			tenant_id, event, resource_type, resource_id, event_key, data
		) VALUES (
			$1, 'privacy.case_communication.requested',
			'privacy_case_communication', $2, 'create',
			jsonb_build_object('privacy_case_id', $3::text)
		)
	`, tenantID, communication.ID, caseID); err != nil {
		return CaseCommunication{}, false, fmt.Errorf("queue privacy case communication: %w", err)
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "communication:"+idempotencyKey,
		"privacy_case.communication_created", &actorMemberID, nil, nil,
		map[string]any{
			"communication_id":   communication.ID,
			"communication_kind": communication.CommunicationKind,
		}, now,
	); err != nil {
		return CaseCommunication{}, false, err
	}
	if err := recordCaseCommunicationAudit(
		ctx, query, tenantID, "privacy.case_communication_created",
		actorMemberID, caseID, communication.ID, communication.CommunicationKind,
	); err != nil {
		return CaseCommunication{}, false, err
	}
	communication.DeliveryAttempts = []CaseCommunicationDelivery{}
	return communication, true, nil
}

func (Store) RetryCaseCommunication(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	communicationID string,
	actorMemberID string,
	idempotencyKey string,
	now time.Time,
) (CaseCommunication, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return CaseCommunication{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return CaseCommunication{}, false, ErrInvalidRequest
	}
	eventKey := "retry:" + idempotencyKey
	var existingEvent bool
	err := query.QueryRow(ctx, `
		SELECT true
		FROM outbox_events
		WHERE tenant_id = $1 AND resource_type = 'privacy_case_communication'
		  AND resource_id = $2 AND event_key = $3
	`, tenantID, communicationID, eventKey).Scan(&existingEvent)
	if err == nil {
		communication, loadErr := loadCaseCommunication(
			ctx, query, tenantID, caseID, communicationID, true,
		)
		return communication, false, loadErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CaseCommunication{}, false, fmt.Errorf("check privacy case communication retry: %w", err)
	}
	current, err := scanCaseCommunication(query.QueryRow(ctx, `
		SELECT `+caseCommunicationSelectColumns("communication")+`
		FROM privacy_case_communications AS communication
		WHERE communication.tenant_id = $1
		  AND communication.privacy_case_id = $2
		  AND communication.id = $3
		FOR UPDATE
	`, tenantID, caseID, communicationID))
	if err != nil {
		return CaseCommunication{}, false, err
	}
	if current.Status != CaseCommunicationFailed {
		return CaseCommunication{}, false, ErrCaseCommunicationNotRetryable
	}
	communication, err := scanCaseCommunication(query.QueryRow(ctx, `
		UPDATE privacy_case_communications
		SET status = 'pending', last_error = NULL, updated_at = $4
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND id = $3
		RETURNING `+caseCommunicationSelectColumns("privacy_case_communications")+`
	`, tenantID, caseID, communicationID, now.UTC()))
	if err != nil {
		return CaseCommunication{}, false, err
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (
			tenant_id, event, resource_type, resource_id, event_key, data
		) VALUES (
			$1, 'privacy.case_communication.requested',
			'privacy_case_communication', $2, $3,
			jsonb_build_object('privacy_case_id', $4::text)
		)
	`, tenantID, communicationID, eventKey, caseID); err != nil {
		return CaseCommunication{}, false, fmt.Errorf("queue privacy case communication retry: %w", err)
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "communication-retry:"+idempotencyKey,
		"privacy_case.communication_retried", &actorMemberID, nil, nil,
		map[string]any{"communication_id": communicationID}, now,
	); err != nil {
		return CaseCommunication{}, false, err
	}
	if err := recordCaseCommunicationAudit(
		ctx, query, tenantID, "privacy.case_communication_retried",
		actorMemberID, caseID, communicationID, communication.CommunicationKind,
	); err != nil {
		return CaseCommunication{}, false, err
	}
	communication.DeliveryAttempts, err = listCaseCommunicationDeliveries(
		ctx, query, tenantID, communicationID,
	)
	return communication, true, err
}

func normalizeCaseCommunicationInput(
	input CreateCaseCommunicationInput,
) (CreateCaseCommunicationInput, error) {
	input.SubjectLine = strings.TrimSpace(input.SubjectLine)
	input.MessageBody = strings.TrimSpace(input.MessageBody)
	if _, err := ParseCaseCommunicationKind(string(input.CommunicationKind)); err != nil {
		return CreateCaseCommunicationInput{}, err
	}
	if input.SubjectLine == "" || len(input.SubjectLine) > 200 ||
		strings.ContainsAny(input.SubjectLine, "\r\n\x00") ||
		input.MessageBody == "" || len(input.MessageBody) > 4000 ||
		strings.ContainsRune(input.MessageBody, '\x00') {
		return CreateCaseCommunicationInput{}, ErrInvalidRequest
	}
	return input, nil
}

func caseCommunicationFingerprint(
	input CreateCaseCommunicationInput,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode privacy case communication input: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func validateCaseCommunicationState(
	privacyCase PrivacyCase,
	kind CaseCommunicationKind,
) error {
	if privacyCase.VerifiedAt == nil || privacyCase.Status == CasePendingVerification {
		return ErrCaseCommunicationUnavailable
	}
	switch kind {
	case CaseCommunicationProgress:
		if caseStatusTerminal(privacyCase.Status) {
			return ErrCaseCommunicationUnavailable
		}
	case CaseCommunicationClarification:
		if privacyCase.Status != CaseAwaitingClarification {
			return ErrCaseCommunicationUnavailable
		}
	case CaseCommunicationDecision:
		if !caseStatusTerminal(privacyCase.Status) {
			return ErrCaseCommunicationUnavailable
		}
	default:
		return ErrInvalidRequest
	}
	return nil
}

func caseCommunicationRecipient(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	privacyCase PrivacyCase,
) (string, error) {
	if privacyCase.SubjectType == SubjectReporter {
		if privacyCase.SubjectEmail == nil {
			return "", ErrCaseCommunicationUnavailable
		}
		emailAddress, err := NormalizeReporterEmail(*privacyCase.SubjectEmail)
		if err != nil {
			return "", ErrCaseCommunicationUnavailable
		}
		return emailAddress, nil
	}
	if privacyCase.SubjectMemberID == nil {
		return "", ErrCaseCommunicationUnavailable
	}
	var emailAddress string
	if err := query.QueryRow(ctx, `
		SELECT email
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, *privacyCase.SubjectMemberID).Scan(&emailAddress); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCaseCommunicationUnavailable
	} else if err != nil {
		return "", fmt.Errorf("load privacy case member recipient: %w", err)
	}
	normalized, err := NormalizeReporterEmail(emailAddress)
	if err != nil {
		return "", ErrCaseCommunicationUnavailable
	}
	return normalized, nil
}

func recordCaseCommunicationAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind string,
	actorMemberID string,
	caseID string,
	communicationID string,
	communicationKind CaseCommunicationKind,
) error {
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id, data
		) VALUES (
			$1, $2, $3, NULL,
			jsonb_build_object(
				'privacy_case_id', $4::text,
				'communication_id', $5::text,
				'communication_kind', $6::text
			)
		)
	`, tenantID, kind, actorMemberID, caseID, communicationID, communicationKind); err != nil {
		return fmt.Errorf("record privacy case communication audit: %w", err)
	}
	return nil
}

func loadCaseCommunication(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	communicationID string,
	withAttempts bool,
) (CaseCommunication, error) {
	communication, err := scanCaseCommunication(query.QueryRow(ctx, `
		SELECT `+caseCommunicationSelectColumns("communication")+`
		FROM privacy_case_communications AS communication
		WHERE communication.tenant_id = $1
		  AND communication.privacy_case_id = $2
		  AND communication.id = $3
	`, tenantID, caseID, communicationID))
	if err != nil {
		return CaseCommunication{}, err
	}
	if withAttempts {
		communication.DeliveryAttempts, err = listCaseCommunicationDeliveries(
			ctx, query, tenantID, communicationID,
		)
	}
	return communication, err
}

func listCaseCommunications(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseIDs []string,
) (map[string][]CaseCommunication, error) {
	result := make(map[string][]CaseCommunication, len(caseIDs))
	if len(caseIDs) == 0 {
		return result, nil
	}
	rows, err := query.Query(ctx, `
		SELECT `+caseCommunicationSelectColumns("communication")+`
		FROM privacy_case_communications AS communication
		WHERE communication.tenant_id = $1
		  AND communication.privacy_case_id = ANY($2::uuid[])
		ORDER BY communication.created_at DESC, communication.id DESC
	`, tenantID, caseIDs)
	if err != nil {
		return nil, fmt.Errorf("list privacy case communications: %w", err)
	}
	communications := make([]CaseCommunication, 0)
	communicationIDs := make([]string, 0)
	for rows.Next() {
		communication, scanErr := scanCaseCommunication(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		communications = append(communications, communication)
		communicationIDs = append(communicationIDs, communication.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate privacy case communications: %w", err)
	}
	rows.Close()
	deliveries, err := listCaseCommunicationDeliveriesForCommunications(
		ctx, query, tenantID, communicationIDs,
	)
	if err != nil {
		return nil, err
	}
	for _, communication := range communications {
		communication.DeliveryAttempts = deliveries[communication.ID]
		if communication.DeliveryAttempts == nil {
			communication.DeliveryAttempts = []CaseCommunicationDelivery{}
		}
		result[communication.PrivacyCaseID] = append(
			result[communication.PrivacyCaseID], communication,
		)
	}
	return result, nil
}

func listCaseCommunicationDeliveriesForCommunications(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	communicationIDs []string,
) (map[string][]CaseCommunicationDelivery, error) {
	result := make(map[string][]CaseCommunicationDelivery, len(communicationIDs))
	if len(communicationIDs) == 0 {
		return result, nil
	}
	rows, err := query.Query(ctx, `
		SELECT privacy_case_communication_id, id, attempt_number, status,
		       provider_message_id, error_message, started_at, completed_at
		FROM privacy_case_communication_attempts
		WHERE tenant_id = $1
		  AND privacy_case_communication_id = ANY($2::uuid[])
		ORDER BY privacy_case_communication_id, attempt_number DESC
	`, tenantID, communicationIDs)
	if err != nil {
		return nil, fmt.Errorf("list privacy case communication attempts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var communicationID string
		var delivery CaseCommunicationDelivery
		if err := rows.Scan(
			&communicationID, &delivery.ID, &delivery.AttemptNumber, &delivery.Status,
			&delivery.ProviderMessageID, &delivery.ErrorMessage,
			&delivery.StartedAt, &delivery.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan privacy case communication attempt: %w", err)
		}
		result[communicationID] = append(result[communicationID], delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy case communication attempts: %w", err)
	}
	return result, nil
}

func listCaseCommunicationDeliveries(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	communicationID string,
) ([]CaseCommunicationDelivery, error) {
	result, err := listCaseCommunicationDeliveriesForCommunications(
		ctx, query, tenantID, []string{communicationID},
	)
	if err != nil {
		return nil, err
	}
	deliveries := result[communicationID]
	if deliveries == nil {
		deliveries = []CaseCommunicationDelivery{}
	}
	return deliveries, nil
}

func caseCommunicationSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.privacy_case_id, ` +
		alias + `.communication_kind, ` + alias + `.recipient_email, ` +
		alias + `.subject_line, ` + alias + `.message_body, ` +
		alias + `.status, ` + alias + `.attempts, ` +
		alias + `.last_error, ` + alias + `.provider_message_id, ` +
		alias + `.last_attempt_at, ` + alias + `.sent_at, ` +
		alias + `.created_by_member_id, ` + alias + `.created_at, ` + alias + `.updated_at`
}

func scanCaseCommunication(
	row rowScanner,
	extra ...any,
) (CaseCommunication, error) {
	var communication CaseCommunication
	destinations := []any{
		&communication.ID, &communication.PrivacyCaseID,
		&communication.CommunicationKind, &communication.RecipientEmail,
		&communication.SubjectLine, &communication.MessageBody,
		&communication.Status, &communication.Attempts,
		&communication.LastError, &communication.ProviderMessageID,
		&communication.LastAttemptAt, &communication.SentAt,
		&communication.CreatedByMemberID, &communication.CreatedAt,
		&communication.UpdatedAt,
	}
	destinations = append(destinations, extra...)
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return CaseCommunication{}, ErrNotFound
	} else if err != nil {
		return CaseCommunication{}, fmt.Errorf("scan privacy case communication: %w", err)
	}
	return communication, nil
}

type CaseCommunicationNotification struct {
	OutboxEventID string
	TenantID      string
	TenantName    string
	CasePublicID  string
	Communication CaseCommunication
}

type CaseCommunicationHandler struct {
	Store         Store
	Sender        email.Sender
	From          string
	PublicBaseURL string
	BaseDomain    string
	Now           func() time.Time
}

func (handler CaseCommunicationHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload CaseCommunicationTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.OutboxEventID == "" || payload.TenantID == "" || payload.ResourceID == "" {
			return fmt.Errorf("decode privacy case communication task: %w", asynq.SkipRetry)
		}
		notification, err := handler.Store.LoadCaseCommunicationNotification(ctx, payload)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("privacy case communication no longer exists: %w", asynq.SkipRetry)
		}
		if err != nil {
			return err
		}
		if notification.Communication.Status == CaseCommunicationSent {
			return nil
		}
		attempt, completed, err := handler.Store.StartCaseCommunicationDelivery(
			ctx, notification, handler.now(),
		)
		if err != nil || completed {
			return err
		}
		message, err := caseCommunicationMessage(notification, handler)
		if err != nil {
			markErr := handler.Store.MarkCaseCommunicationDeliveryFailed(
				ctx, notification, attempt.ID, handler.now(),
			)
			return errors.Join(fmt.Errorf("compose privacy case communication: %w", err), markErr)
		}
		providerID, err := handler.Sender.Send(ctx, message)
		if err != nil {
			markErr := handler.Store.MarkCaseCommunicationDeliveryFailed(
				ctx, notification, attempt.ID, handler.now(),
			)
			return errors.Join(fmt.Errorf("send privacy case communication: %w", err), markErr)
		}
		return handler.Store.MarkCaseCommunicationDeliverySent(
			ctx, notification, attempt.ID, providerID, handler.now(),
		)
	}
}

func (store Store) LoadCaseCommunicationNotification(
	ctx context.Context,
	task CaseCommunicationTask,
) (CaseCommunicationNotification, error) {
	var notification CaseCommunicationNotification
	var communication CaseCommunication
	err := store.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, privacy_case.public_id,
		       `+caseCommunicationSelectColumns("communication")+`
		FROM outbox_events AS event
		JOIN privacy_case_communications AS communication
		  ON communication.tenant_id = event.tenant_id
		 AND communication.id = event.resource_id
		JOIN privacy_cases AS privacy_case
		  ON privacy_case.tenant_id = communication.tenant_id
		 AND privacy_case.id = communication.privacy_case_id
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		WHERE event.id = $1 AND event.tenant_id = $2 AND event.resource_id = $3
		  AND event.event = 'privacy.case_communication.requested'
		  AND event.resource_type = 'privacy_case_communication'
	`, task.OutboxEventID, task.TenantID, task.ResourceID).Scan(
		&notification.OutboxEventID, &notification.TenantID,
		&notification.TenantName, &notification.CasePublicID,
		&communication.ID, &communication.PrivacyCaseID,
		&communication.CommunicationKind, &communication.RecipientEmail,
		&communication.SubjectLine, &communication.MessageBody,
		&communication.Status, &communication.Attempts,
		&communication.LastError, &communication.ProviderMessageID,
		&communication.LastAttemptAt, &communication.SentAt,
		&communication.CreatedByMemberID, &communication.CreatedAt,
		&communication.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaseCommunicationNotification{}, ErrNotFound
	}
	if err != nil {
		return CaseCommunicationNotification{}, fmt.Errorf(
			"load privacy case communication: %w", err,
		)
	}
	notification.Communication = communication
	return notification, nil
}

func (store Store) StartCaseCommunicationDelivery(
	ctx context.Context,
	notification CaseCommunicationNotification,
	now time.Time,
) (CaseCommunicationDelivery, bool, error) {
	var attempt CaseCommunicationDelivery
	var completed bool
	var inProgress bool
	err := store.Database.QueryRow(ctx, `
		WITH locked AS (
			SELECT id, status, last_attempt_at
			FROM privacy_case_communications
			WHERE tenant_id = $1 AND id = $2
			FOR UPDATE
		), closed AS (
			UPDATE privacy_case_communication_attempts
			SET status = 'failed', error_message = 'delivery interrupted',
			    completed_at = $3
			WHERE tenant_id = $1 AND privacy_case_communication_id = $2
			  AND status = 'pending'
			  AND EXISTS (
			      SELECT 1 FROM locked
			      WHERE status IN ('pending', 'failed')
			         OR (status = 'processing'
			             AND last_attempt_at <= $3::timestamptz - interval '5 minutes')
			  )
		), next_attempt AS (
			SELECT COALESCE(max(attempt_number), 0) + 1 AS number
			FROM privacy_case_communication_attempts
			WHERE tenant_id = $1 AND privacy_case_communication_id = $2
		), inserted AS (
			INSERT INTO privacy_case_communication_attempts (
				tenant_id, privacy_case_communication_id, attempt_number, started_at
			)
			SELECT $1, $2, next_attempt.number, $3
			FROM next_attempt, locked
			WHERE locked.status IN ('pending', 'failed')
			   OR (locked.status = 'processing'
			       AND locked.last_attempt_at <= $3::timestamptz - interval '5 minutes')
			RETURNING id, attempt_number, status, started_at
		), updated AS (
			UPDATE privacy_case_communications
			SET status = 'processing', attempts = inserted.attempt_number,
			    last_error = NULL, last_attempt_at = $3, updated_at = $3
			FROM inserted
			WHERE privacy_case_communications.tenant_id = $1
			  AND privacy_case_communications.id = $2
		)
		SELECT inserted.id::text, inserted.attempt_number, inserted.status,
		       inserted.started_at, false, false
		FROM inserted
		UNION ALL
		SELECT '', 0, 'sent'::privacy_case_communication_delivery_status,
		       $3, true, false
		FROM locked WHERE locked.status = 'sent'
		UNION ALL
		SELECT '', 0, 'pending'::privacy_case_communication_delivery_status,
		       $3, false, true
		FROM locked
		WHERE locked.status = 'processing'
		  AND locked.last_attempt_at > $3::timestamptz - interval '5 minutes'
	`, notification.TenantID, notification.Communication.ID, now.UTC()).Scan(
		&attempt.ID, &attempt.AttemptNumber, &attempt.Status,
		&attempt.StartedAt, &completed, &inProgress,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaseCommunicationDelivery{}, false, ErrNotFound
	}
	if err != nil {
		return CaseCommunicationDelivery{}, false, fmt.Errorf(
			"start privacy case communication delivery: %w", err,
		)
	}
	if inProgress {
		return CaseCommunicationDelivery{}, false, ErrCaseCommunicationInProgress
	}
	return attempt, completed, nil
}

func (store Store) MarkCaseCommunicationDeliverySent(
	ctx context.Context,
	notification CaseCommunicationNotification,
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
			UPDATE privacy_case_communication_attempts
			SET status = 'sent', provider_message_id = $4,
			    error_message = NULL, completed_at = $5
			WHERE tenant_id = $1 AND privacy_case_communication_id = $2
			  AND id = $3 AND status = 'pending'
			RETURNING id
		)
		UPDATE privacy_case_communications
		SET status = 'sent', provider_message_id = $4, last_error = NULL,
		    sent_at = $5, updated_at = $5
		WHERE tenant_id = $1 AND id = $2 AND EXISTS (SELECT 1 FROM completed)
	`, notification.TenantID, notification.Communication.ID, attemptID,
		providerMessageID, now.UTC())
	if err != nil {
		return fmt.Errorf("mark privacy case communication sent: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (store Store) MarkCaseCommunicationDeliveryFailed(
	ctx context.Context,
	notification CaseCommunicationNotification,
	attemptID string,
	now time.Time,
) error {
	const publicError = "email provider delivery failed"
	command, err := store.Database.Exec(ctx, `
		WITH completed AS (
			UPDATE privacy_case_communication_attempts
			SET status = 'failed', provider_message_id = NULL,
			    error_message = $4, completed_at = $5
			WHERE tenant_id = $1 AND privacy_case_communication_id = $2
			  AND id = $3 AND status = 'pending'
			RETURNING id
		)
		UPDATE privacy_case_communications
		SET status = 'failed', provider_message_id = NULL, last_error = $4,
		    sent_at = NULL, updated_at = $5
		WHERE tenant_id = $1 AND id = $2 AND EXISTS (SELECT 1 FROM completed)
	`, notification.TenantID, notification.Communication.ID, attemptID,
		publicError, now.UTC())
	if err != nil {
		return fmt.Errorf("mark privacy case communication failed: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func caseCommunicationMessage(
	notification CaseCommunicationNotification,
	handler CaseCommunicationHandler,
) (email.Message, error) {
	logoURL, err := email.BrandLogoURL(handler.PublicBaseURL, handler.BaseDomain)
	if err != nil {
		return email.Message{}, err
	}
	kindLabel := "Progress update"
	switch notification.Communication.CommunicationKind {
	case CaseCommunicationClarification:
		kindLabel = "Information request"
	case CaseCommunicationDecision:
		kindLabel = "Decision notice"
	}
	return email.Compose(email.Message{
		From: handler.From, To: notification.Communication.RecipientEmail,
		Subject: notification.Communication.SubjectLine,
		IdempotencyKey: "privacy-case-communication:" +
			notification.Communication.ID,
	}, email.Content{
		Preheader: notification.Communication.SubjectLine,
		Heading:   kindLabel,
		Workspace: notification.TenantName,
		Body: []string{
			notification.TenantName + " sent an update about data-rights request " +
				notification.CasePublicID + ".",
			notification.Communication.MessageBody,
		},
		Metadata: []email.Metadata{
			{Label: "Case reference", Value: notification.CasePublicID},
			{Label: "Update type", Value: kindLabel},
		},
		Footer:  "This is an operational message about the recorded data-rights request.",
		LogoURL: logoURL,
	})
}

func (handler CaseCommunicationHandler) now() time.Time {
	if handler.Now != nil {
		return handler.Now().UTC()
	}
	return time.Now().UTC()
}
