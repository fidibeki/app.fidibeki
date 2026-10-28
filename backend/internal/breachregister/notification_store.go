package breachregister

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	controllerNotificationEvent        = "breach.controller_notification.requested"
	controllerNotificationResourceType = "breach_controller_notification"
)

func (s Store) StageControllerNotification(
	ctx context.Context,
	incidentID string,
	actorPlatformAdminID string,
	idempotencyKey string,
	input StageControllerNotificationInput,
) (ControllerNotification, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	normalized, err := normalizeStageControllerNotificationInput(input)
	if err != nil || idempotencyKey == "" || len(idempotencyKey) > 200 ||
		strings.TrimSpace(actorPlatformAdminID) == "" {
		return ControllerNotification{}, false, ErrInvalidRequest
	}
	fingerprint, err := stageControllerNotificationFingerprint(normalized)
	if err != nil {
		return ControllerNotification{}, false, err
	}
	now := s.now().UTC()
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return ControllerNotification{}, false, fmt.Errorf("begin controller notification stage: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	incident, err := scanIncident(tx.QueryRow(ctx, `
		SELECT `+breachIncidentSelectColumns("incident")+`
		FROM breach_incidents AS incident
		WHERE incident.id = $1
		FOR UPDATE
	`, incidentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ControllerNotification{}, false, ErrNotFound
	}
	if err != nil {
		return ControllerNotification{}, false, fmt.Errorf("lock breach incident: %w", err)
	}
	existing, storedFingerprint, found, err := controllerNotificationByIdempotencyKey(
		ctx, tx, incidentID, idempotencyKey,
	)
	if err != nil {
		return ControllerNotification{}, false, err
	}
	if found {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return ControllerNotification{}, false, ErrIdempotencyConflict
		}
		existing.DeliveryAttempts, err = listControllerNotificationAttempts(
			ctx, tx, existing.ID,
		)
		if err != nil {
			return ControllerNotification{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ControllerNotification{}, false, fmt.Errorf(
				"commit controller notification idempotent read: %w", err,
			)
		}
		return existing, false, nil
	}
	if incident.ControllerRole != RoleProcessor {
		return ControllerNotification{}, false, ErrInvalidRequest
	}
	if err := requirePlatformAdminExists(ctx, tx, actorPlatformAdminID); err != nil {
		return ControllerNotification{}, false, err
	}

	var workspaceName string
	var recipientEmail *string
	var privacyProfileVersion int
	err = tx.QueryRow(ctx, `
		SELECT tenant.name, tenant.privacy_contact_email::text,
		       tenant.privacy_profile_version
		FROM breach_incident_tenants AS affected
		JOIN tenants AS tenant ON tenant.id = affected.tenant_id
		WHERE affected.breach_incident_id = $1 AND affected.tenant_id = $2
	`, incidentID, normalized.TenantID).Scan(
		&workspaceName, &recipientEmail, &privacyProfileVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ControllerNotification{}, false, ErrTenantNotFound
	}
	if err != nil {
		return ControllerNotification{}, false, fmt.Errorf("load affected controller contact: %w", err)
	}
	if recipientEmail == nil || strings.TrimSpace(*recipientEmail) == "" || privacyProfileVersion <= 0 {
		return ControllerNotification{}, false, ErrControllerContactMissing
	}

	var notificationCount int
	var initialSent bool
	var closureExists bool
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::integer,
		       COALESCE(bool_or(
		           notification_kind = 'initial' AND status = 'sent'
		       ), false),
		       COALESCE(bool_or(notification_kind = 'closure'), false)
		FROM breach_controller_notifications
		WHERE breach_incident_id = $1 AND tenant_id = $2
	`, incidentID, normalized.TenantID).Scan(
		&notificationCount, &initialSent, &closureExists,
	); err != nil {
		return ControllerNotification{}, false, fmt.Errorf("inspect controller notification history: %w", err)
	}
	switch normalized.NotificationKind {
	case ControllerNotificationInitial:
		if notificationCount != 0 {
			return ControllerNotification{}, false, ErrInvalidRequest
		}
	case ControllerNotificationUpdate:
		if !initialSent || closureExists {
			return ControllerNotification{}, false, ErrInvalidRequest
		}
	case ControllerNotificationClosure:
		if !initialSent || closureExists {
			return ControllerNotification{}, false, ErrInvalidRequest
		}
	}

	notification, err := scanControllerNotification(tx.QueryRow(ctx, `
		INSERT INTO breach_controller_notifications (
			id, breach_incident_id, tenant_id, notification_kind, sequence,
			template_version, incident_public_id, workspace_name, recipient_email,
			privacy_profile_version, awareness_at, affected_environments,
			affected_system_categories, affected_data_categories,
			affected_subject_categories, confidentiality_impact, integrity_impact,
			availability_impact, summary, measures_taken, next_update,
			created_by_platform_admin_id, idempotency_key, request_fingerprint,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9::citext, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $25
		)
		RETURNING `+controllerNotificationSelectColumns("")+`
	`, uuid.NewString(), incidentID, normalized.TenantID, normalized.NotificationKind,
		notificationCount+1, controllerNotificationTemplateVersion, incident.PublicID,
		workspaceName, *recipientEmail, privacyProfileVersion, incident.AwarenessAt,
		incident.AffectedEnvironments, incident.AffectedSystemCategories,
		incident.AffectedDataCategories, incident.AffectedSubjectCategories,
		incident.ConfidentialityImpact, incident.IntegrityImpact,
		incident.AvailabilityImpact, normalized.Summary, normalized.MeasuresTaken,
		normalized.NextUpdate, actorPlatformAdminID, idempotencyKey, fingerprint[:], now,
	))
	if err != nil {
		return ControllerNotification{}, false, fmt.Errorf("stage controller notification: %w", err)
	}
	notification.DeliveryAttempts = []ControllerNotificationAttempt{}
	if err := tx.Commit(ctx); err != nil {
		return ControllerNotification{}, false, fmt.Errorf("commit controller notification stage: %w", err)
	}
	return notification, true, nil
}

func (s Store) QueueControllerNotification(
	ctx context.Context,
	incidentID string,
	notificationID string,
	actorPlatformAdminID string,
	idempotencyKey string,
) (ControllerNotification, bool, error) {
	return s.queueControllerNotification(
		ctx, incidentID, notificationID, actorPlatformAdminID, idempotencyKey, false,
	)
}

func (s Store) RetryControllerNotification(
	ctx context.Context,
	incidentID string,
	notificationID string,
	actorPlatformAdminID string,
	idempotencyKey string,
) (ControllerNotification, bool, error) {
	return s.queueControllerNotification(
		ctx, incidentID, notificationID, actorPlatformAdminID, idempotencyKey, true,
	)
}

func (s Store) queueControllerNotification(
	ctx context.Context,
	incidentID string,
	notificationID string,
	actorPlatformAdminID string,
	idempotencyKey string,
	retry bool,
) (ControllerNotification, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 ||
		strings.TrimSpace(actorPlatformAdminID) == "" {
		return ControllerNotification{}, false, ErrInvalidRequest
	}
	prefix := "queue:"
	requiredStatus := ControllerNotificationDraft
	if retry {
		prefix = "retry:"
		requiredStatus = ControllerNotificationFailed
	}
	eventKey := prefix + idempotencyKey
	now := s.now().UTC()
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return ControllerNotification{}, false, fmt.Errorf("begin controller notification queue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requirePlatformAdminExists(ctx, tx, actorPlatformAdminID); err != nil {
		return ControllerNotification{}, false, err
	}
	notification, err := scanControllerNotification(tx.QueryRow(ctx, `
		SELECT `+controllerNotificationSelectColumns("notification")+`
		FROM breach_controller_notifications AS notification
		WHERE notification.breach_incident_id = $1 AND notification.id = $2
		FOR UPDATE
	`, incidentID, notificationID))
	if err != nil {
		return ControllerNotification{}, false, err
	}
	var existingOutboxID string
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM breach_controller_notification_outbox
		WHERE notification_id = $1 AND event_key = $2
	`, notificationID, eventKey).Scan(&existingOutboxID)
	if err == nil {
		notification.DeliveryAttempts, err = listControllerNotificationAttempts(
			ctx, tx, notification.ID,
		)
		if err != nil {
			return ControllerNotification{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ControllerNotification{}, false, fmt.Errorf(
				"commit controller notification queue replay: %w", err,
			)
		}
		return notification, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ControllerNotification{}, false, fmt.Errorf("read controller notification outbox: %w", err)
	}
	if notification.Status != requiredStatus {
		return ControllerNotification{}, false, ErrNotificationNotQueueable
	}
	if !retry && notification.Sequence > 1 {
		var priorNotificationsSent bool
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(bool_and(status = 'sent'), false)
			FROM breach_controller_notifications
			WHERE breach_incident_id = $1 AND tenant_id = $2
			  AND sequence < $3
		`, incidentID, notification.TenantID, notification.Sequence).Scan(
			&priorNotificationsSent,
		); err != nil {
			return ControllerNotification{}, false, fmt.Errorf(
				"inspect prior controller notifications: %w", err,
			)
		}
		if !priorNotificationsSent {
			return ControllerNotification{}, false, ErrNotificationNotQueueable
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO breach_controller_notification_outbox (
			id, tenant_id, notification_id, event_key,
			queued_by_platform_admin_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, uuid.NewString(), notification.TenantID, notification.ID, eventKey,
		actorPlatformAdminID, now,
	); err != nil {
		return ControllerNotification{}, false, fmt.Errorf("write controller notification outbox: %w", err)
	}
	notification, err = scanControllerNotification(tx.QueryRow(ctx, `
		UPDATE breach_controller_notifications
		SET status = 'pending', last_error = NULL, provider_message_id = NULL,
		    queued_at = COALESCE(queued_at, $3),
		    queued_by_platform_admin_id = COALESCE(queued_by_platform_admin_id, $4),
		    updated_at = $3
		WHERE breach_incident_id = $1 AND id = $2
		RETURNING `+controllerNotificationSelectColumns("")+`
	`, incidentID, notificationID, now, actorPlatformAdminID))
	if err != nil {
		return ControllerNotification{}, false, fmt.Errorf("queue controller notification: %w", err)
	}
	notification.DeliveryAttempts, err = listControllerNotificationAttempts(ctx, tx, notification.ID)
	if err != nil {
		return ControllerNotification{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ControllerNotification{}, false, fmt.Errorf("commit controller notification queue: %w", err)
	}
	return notification, true, nil
}

func listControllerNotifications(
	ctx context.Context,
	query db.Querier,
	incidentID string,
) ([]ControllerNotification, error) {
	rows, err := query.Query(ctx, `
		SELECT `+controllerNotificationSelectColumns("notification")+`
		FROM breach_controller_notifications AS notification
		WHERE notification.breach_incident_id = $1
		ORDER BY notification.tenant_id, notification.sequence
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list controller notifications: %w", err)
	}
	defer rows.Close()
	notifications := make([]ControllerNotification, 0)
	for rows.Next() {
		notification, err := scanControllerNotification(rows)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate controller notifications: %w", err)
	}
	notificationIndexes := make(map[string]int, len(notifications))
	for index := range notifications {
		notificationIndexes[notifications[index].ID] = index
		notifications[index].DeliveryAttempts = []ControllerNotificationAttempt{}
	}
	attemptRows, err := query.Query(ctx, `
		SELECT attempt.id, attempt.notification_id, attempt.attempt_number,
		       attempt.status, attempt.provider_message_id, attempt.error_message,
		       attempt.attempted_at
		FROM breach_controller_notification_attempts AS attempt
		WHERE attempt.breach_incident_id = $1
		ORDER BY attempt.notification_id, attempt.attempt_number
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list breach controller notification attempts: %w", err)
	}
	defer attemptRows.Close()
	for attemptRows.Next() {
		var attempt ControllerNotificationAttempt
		if err := attemptRows.Scan(
			&attempt.ID, &attempt.NotificationID, &attempt.AttemptNumber,
			&attempt.Status, &attempt.ProviderMessageID, &attempt.ErrorMessage,
			&attempt.AttemptedAt,
		); err != nil {
			return nil, fmt.Errorf("scan breach controller notification attempt: %w", err)
		}
		index, found := notificationIndexes[attempt.NotificationID]
		if !found {
			return nil, fmt.Errorf("controller notification attempt references unknown notification")
		}
		notifications[index].DeliveryAttempts = append(
			notifications[index].DeliveryAttempts, attempt,
		)
	}
	if err := attemptRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate breach controller notification attempts: %w", err)
	}
	return notifications, nil
}

func listControllerNotificationAttempts(
	ctx context.Context,
	query db.Querier,
	notificationID string,
) ([]ControllerNotificationAttempt, error) {
	rows, err := query.Query(ctx, `
		SELECT id, notification_id, attempt_number, status,
		       provider_message_id, error_message, attempted_at
		FROM breach_controller_notification_attempts
		WHERE notification_id = $1
		ORDER BY attempt_number
	`, notificationID)
	if err != nil {
		return nil, fmt.Errorf("list controller notification attempts: %w", err)
	}
	defer rows.Close()
	attempts := make([]ControllerNotificationAttempt, 0)
	for rows.Next() {
		var attempt ControllerNotificationAttempt
		if err := rows.Scan(
			&attempt.ID, &attempt.NotificationID, &attempt.AttemptNumber,
			&attempt.Status, &attempt.ProviderMessageID, &attempt.ErrorMessage,
			&attempt.AttemptedAt,
		); err != nil {
			return nil, fmt.Errorf("scan controller notification attempt: %w", err)
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate controller notification attempts: %w", err)
	}
	return attempts, nil
}

func controllerNotificationByIdempotencyKey(
	ctx context.Context,
	query db.Querier,
	incidentID string,
	idempotencyKey string,
) (ControllerNotification, []byte, bool, error) {
	var fingerprint []byte
	notification, err := scanControllerNotification(query.QueryRow(ctx, `
		SELECT `+controllerNotificationSelectColumns("notification")+`,
		       notification.request_fingerprint
		FROM breach_controller_notifications AS notification
		WHERE notification.breach_incident_id = $1
		  AND notification.idempotency_key = $2
	`, incidentID, idempotencyKey), &fingerprint)
	if errors.Is(err, ErrNotFound) {
		return ControllerNotification{}, nil, false, nil
	}
	if err != nil {
		return ControllerNotification{}, nil, false, err
	}
	return notification, fingerprint, true, nil
}

func controllerNotificationSelectColumns(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return prefix + `id, ` + prefix + `breach_incident_id, ` + prefix + `tenant_id, ` +
		prefix + `notification_kind, ` + prefix + `sequence, ` + prefix + `template_version, ` +
		prefix + `incident_public_id, ` + prefix + `workspace_name, ` +
		prefix + `recipient_email::text, ` + prefix + `privacy_profile_version, ` +
		prefix + `awareness_at, ` + prefix + `affected_environments, ` +
		prefix + `affected_system_categories, ` + prefix + `affected_data_categories, ` +
		prefix + `affected_subject_categories, ` + prefix + `confidentiality_impact, ` +
		prefix + `integrity_impact, ` + prefix + `availability_impact, ` +
		prefix + `summary, ` + prefix + `measures_taken, ` + prefix + `next_update, ` +
		prefix + `status, ` + prefix + `attempts, ` + prefix + `last_error, ` +
		prefix + `provider_message_id, ` + prefix + `last_attempt_at, ` +
		prefix + `queued_at, ` + prefix + `sent_at, ` +
		prefix + `created_by_platform_admin_id, ` + prefix + `queued_by_platform_admin_id, ` +
		prefix + `created_at, ` + prefix + `updated_at`
}

func scanControllerNotification(
	row rowScanner,
	extra ...any,
) (ControllerNotification, error) {
	var notification ControllerNotification
	destinations := []any{
		&notification.ID, &notification.BreachIncidentID, &notification.TenantID,
		&notification.NotificationKind, &notification.Sequence, &notification.TemplateVersion,
		&notification.IncidentPublicID, &notification.WorkspaceName, &notification.RecipientEmail,
		&notification.PrivacyProfileVersion, &notification.AwarenessAt,
		&notification.AffectedEnvironments, &notification.AffectedSystemCategories,
		&notification.AffectedDataCategories, &notification.AffectedSubjectCategories,
		&notification.ConfidentialityImpact, &notification.IntegrityImpact,
		&notification.AvailabilityImpact, &notification.Summary, &notification.MeasuresTaken,
		&notification.NextUpdate, &notification.Status, &notification.Attempts,
		&notification.LastError, &notification.ProviderMessageID, &notification.LastAttemptAt,
		&notification.QueuedAt, &notification.SentAt, &notification.CreatedByPlatformAdminID,
		&notification.QueuedByPlatformAdminID, &notification.CreatedAt, &notification.UpdatedAt,
	}
	destinations = append(destinations, extra...)
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return ControllerNotification{}, ErrNotFound
	} else if err != nil {
		return ControllerNotification{}, fmt.Errorf("scan controller notification: %w", err)
	}
	return notification, nil
}

type ControllerNotificationOutboxStore struct {
	Database db.Querier
}

func (s ControllerNotificationOutboxStore) Pending(
	ctx context.Context,
	limit int,
) ([]outbox.Event, error) {
	rows, err := s.Database.Query(ctx, `
		SELECT id, tenant_id, notification_id, attempts
		FROM breach_controller_notification_outbox
		WHERE dispatched_at IS NULL
		ORDER BY created_at, id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending controller notification events: %w", err)
	}
	defer rows.Close()
	events := make([]outbox.Event, 0)
	for rows.Next() {
		var event outbox.Event
		event.Event = controllerNotificationEvent
		event.ResourceType = controllerNotificationResourceType
		if err := rows.Scan(
			&event.ID, &event.TenantID, &event.ResourceID, &event.Attempts,
		); err != nil {
			return nil, fmt.Errorf("scan controller notification event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate controller notification events: %w", err)
	}
	return events, nil
}

func (s ControllerNotificationOutboxStore) MarkDispatched(
	ctx context.Context,
	eventID string,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE breach_controller_notification_outbox
		SET dispatched_at = $2, attempts = attempts + 1, last_error = NULL
		WHERE id = $1 AND dispatched_at IS NULL
	`, eventID, now.UTC()); err != nil {
		return fmt.Errorf("mark controller notification event dispatched: %w", err)
	}
	return nil
}

func (s ControllerNotificationOutboxStore) MarkFailed(
	ctx context.Context,
	eventID string,
	message string,
) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE breach_controller_notification_outbox
		SET attempts = attempts + 1, last_error = $2
		WHERE id = $1 AND dispatched_at IS NULL
	`, eventID, message); err != nil {
		return fmt.Errorf("mark controller notification event failed: %w", err)
	}
	return nil
}

type ControllerNotificationOutboxResolver struct{}

func (ControllerNotificationOutboxResolver) Resolve(
	_ context.Context,
	event outbox.Event,
) ([]outbox.Destination, error) {
	if event.Event != controllerNotificationEvent ||
		event.ResourceType != controllerNotificationResourceType {
		return nil, nil
	}
	return []outbox.Destination{{
		Kind:     "breach_controller_email",
		ID:       event.ResourceID,
		TaskType: TaskControllerNotification,
		Queue:    email.QueueName,
	}}, nil
}
