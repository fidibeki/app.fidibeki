package breachregister

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

const TaskControllerNotification = "breach:controller-notification"

type ControllerNotificationTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
}

type ControllerNotificationDelivery struct {
	OutboxEventID string
	Notification  ControllerNotification
}

type ControllerNotificationHandler struct {
	Store         Store
	Sender        email.Sender
	From          string
	PublicBaseURL string
	Now           func() time.Time
}

func (handler ControllerNotificationHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload ControllerNotificationTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.OutboxEventID == "" || payload.TenantID == "" || payload.ResourceID == "" {
			return fmt.Errorf("decode breach controller notification task: %w", asynq.SkipRetry)
		}
		delivery, err := handler.Store.LoadControllerNotificationDelivery(ctx, payload)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("breach controller notification no longer exists: %w", asynq.SkipRetry)
		}
		if err != nil {
			return err
		}
		if delivery.Notification.Status == ControllerNotificationSent {
			return nil
		}
		attemptNumber, completed, err := handler.Store.StartControllerNotificationDelivery(
			ctx, delivery.Notification.ID, handler.now(),
		)
		if err != nil || completed {
			return err
		}
		message, err := controllerNotificationMessage(delivery.Notification, handler)
		if err != nil {
			markErr := handler.Store.MarkControllerNotificationDeliveryFailed(
				ctx, delivery.Notification.ID, attemptNumber, handler.now(),
			)
			return errors.Join(fmt.Errorf("compose breach controller notification: %w", err), markErr)
		}
		providerID, err := handler.Sender.Send(ctx, message)
		if err != nil {
			markErr := handler.Store.MarkControllerNotificationDeliveryFailed(
				ctx, delivery.Notification.ID, attemptNumber, handler.now(),
			)
			return errors.Join(errors.New("send breach controller notification failed"), markErr)
		}
		return handler.Store.MarkControllerNotificationDeliverySent(
			ctx, delivery.Notification.ID, attemptNumber, providerID, handler.now(),
		)
	}
}

func (s Store) LoadControllerNotificationDelivery(
	ctx context.Context,
	task ControllerNotificationTask,
) (ControllerNotificationDelivery, error) {
	var delivery ControllerNotificationDelivery
	notification, err := scanControllerNotification(s.Database.QueryRow(ctx, `
		SELECT `+controllerNotificationSelectColumns("notification")+`,
		       event.id
		FROM breach_controller_notification_outbox AS event
		JOIN breach_controller_notifications AS notification
		  ON notification.id = event.notification_id
		 AND notification.tenant_id = event.tenant_id
		WHERE event.id = $1 AND event.tenant_id = $2
		  AND event.notification_id = $3
	`, task.OutboxEventID, task.TenantID, task.ResourceID), &delivery.OutboxEventID)
	if err != nil {
		return ControllerNotificationDelivery{}, err
	}
	delivery.Notification = notification
	return delivery, nil
}

func (s Store) StartControllerNotificationDelivery(
	ctx context.Context,
	notificationID string,
	now time.Time,
) (int, bool, error) {
	var attemptNumber int
	var completed bool
	var inProgress bool
	err := s.Database.QueryRow(ctx, `
		WITH locked AS (
			SELECT id, status, attempts, last_attempt_at
			FROM breach_controller_notifications
			WHERE id = $1
			FOR UPDATE
		), started AS (
			UPDATE breach_controller_notifications
			SET status = 'processing', attempts = locked.attempts + 1,
			    last_error = NULL, provider_message_id = NULL,
			    last_attempt_at = $2, updated_at = $2
			FROM locked
			WHERE breach_controller_notifications.id = locked.id
			  AND (
			      locked.status IN ('pending', 'failed')
			      OR (
			          locked.status = 'processing'
			          AND locked.last_attempt_at <= $2::timestamptz - interval '5 minutes'
			      )
			  )
			RETURNING breach_controller_notifications.attempts
		)
		SELECT started.attempts, false, false FROM started
		UNION ALL
		SELECT locked.attempts, true, false
		FROM locked WHERE locked.status = 'sent'
		UNION ALL
		SELECT locked.attempts, false, true
		FROM locked
		WHERE locked.status = 'processing'
		  AND locked.last_attempt_at > $2::timestamptz - interval '5 minutes'
	`, notificationID, now.UTC()).Scan(&attemptNumber, &completed, &inProgress)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ErrNotFound
	}
	if err != nil {
		return 0, false, fmt.Errorf("start breach controller notification delivery: %w", err)
	}
	if inProgress {
		return 0, false, ErrNotificationInProgress
	}
	return attemptNumber, completed, nil
}

func (s Store) MarkControllerNotificationDeliverySent(
	ctx context.Context,
	notificationID string,
	attemptNumber int,
	providerMessageID string,
	now time.Time,
) error {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if providerMessageID == "" || len(providerMessageID) > 500 || attemptNumber <= 0 {
		return ErrInvalidRequest
	}
	command, err := s.Database.Exec(ctx, `
		WITH notification AS (
			SELECT id, breach_incident_id, tenant_id
			FROM breach_controller_notifications
			WHERE id = $1 AND status = 'processing' AND attempts = $2
			FOR UPDATE
		), inserted AS (
			INSERT INTO breach_controller_notification_attempts (
				id, breach_incident_id, tenant_id, notification_id,
				attempt_number, status, provider_message_id, attempted_at
			)
			SELECT gen_random_uuid(), notification.breach_incident_id,
			       notification.tenant_id, notification.id, $2, 'sent', $3, $4
			FROM notification
			RETURNING notification_id
		)
		UPDATE breach_controller_notifications
		SET status = 'sent', provider_message_id = $3, last_error = NULL,
		    sent_at = $4, updated_at = $4
		WHERE id = $1 AND EXISTS (SELECT 1 FROM inserted)
	`, notificationID, attemptNumber, providerMessageID, now.UTC())
	if err != nil {
		return fmt.Errorf("mark breach controller notification sent: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s Store) MarkControllerNotificationDeliveryFailed(
	ctx context.Context,
	notificationID string,
	attemptNumber int,
	now time.Time,
) error {
	if attemptNumber <= 0 {
		return ErrInvalidRequest
	}
	const publicError = "email provider delivery failed"
	command, err := s.Database.Exec(ctx, `
		WITH notification AS (
			SELECT id, breach_incident_id, tenant_id
			FROM breach_controller_notifications
			WHERE id = $1 AND status = 'processing' AND attempts = $2
			FOR UPDATE
		), inserted AS (
			INSERT INTO breach_controller_notification_attempts (
				id, breach_incident_id, tenant_id, notification_id,
				attempt_number, status, error_message, attempted_at
			)
			SELECT gen_random_uuid(), notification.breach_incident_id,
			       notification.tenant_id, notification.id, $2, 'failed', $3, $4
			FROM notification
			RETURNING notification_id
		)
		UPDATE breach_controller_notifications
		SET status = 'failed', provider_message_id = NULL, last_error = $3,
		    sent_at = NULL, updated_at = $4
		WHERE id = $1 AND EXISTS (SELECT 1 FROM inserted)
	`, notificationID, attemptNumber, publicError, now.UTC())
	if err != nil {
		return fmt.Errorf("mark breach controller notification failed: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func controllerNotificationMessage(
	notification ControllerNotification,
	handler ControllerNotificationHandler,
) (email.Message, error) {
	logoURL, err := email.BrandLogoURL(handler.PublicBaseURL)
	if err != nil {
		return email.Message{}, err
	}
	kindLabel := "Initial notification"
	switch notification.NotificationKind {
	case ControllerNotificationUpdate:
		kindLabel = "Incident update"
	case ControllerNotificationClosure:
		kindLabel = "Closure update"
	}
	body := []string{
		"Fidibeki is notifying " + notification.WorkspaceName +
			" about a personal-data breach recorded while Fidibeki is acting as processor.",
		notification.Summary,
		"Measures taken: " + notification.MeasuresTaken,
	}
	if notification.NextUpdate != nil {
		body = append(body, "Next update: "+*notification.NextUpdate)
	}
	impacts := make([]string, 0, 3)
	if notification.ConfidentialityImpact {
		impacts = append(impacts, "confidentiality")
	}
	if notification.IntegrityImpact {
		impacts = append(impacts, "integrity")
	}
	if notification.AvailabilityImpact {
		impacts = append(impacts, "availability")
	}
	return email.Compose(email.Message{
		From: handler.From, To: notification.RecipientEmail,
		Subject:        "Personal-data breach " + kindLabel + " - " + notification.IncidentPublicID,
		IdempotencyKey: "breach-controller-notification:" + notification.ID,
	}, email.Content{
		Preheader: "Breach notification " + notification.IncidentPublicID,
		Heading:   kindLabel,
		Workspace: notification.WorkspaceName,
		Body:      body,
		Metadata: []email.Metadata{
			{Label: "Incident reference", Value: notification.IncidentPublicID},
			{Label: "Awareness time", Value: notification.AwarenessAt.UTC().Format(time.RFC3339)},
			{Label: "Affected environments", Value: strings.Join(notification.AffectedEnvironments, ", ")},
			{Label: "Affected systems", Value: strings.Join(notification.AffectedSystemCategories, ", ")},
			{Label: "Data categories", Value: strings.Join(notification.AffectedDataCategories, ", ")},
			{Label: "Subject categories", Value: strings.Join(notification.AffectedSubjectCategories, ", ")},
			{Label: "Impact", Value: strings.Join(impacts, ", ")},
		},
		Footer: "This operational processor notification does not determine whether an authority " +
			"or affected-person notification is required.",
		LogoURL: logoURL,
	})
}

func (handler ControllerNotificationHandler) now() time.Time {
	if handler.Now != nil {
		return handler.Now().UTC()
	}
	return time.Now().UTC()
}
