package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

const TaskReporterNotify = "issue:reporter-notify"

type OutboxResolver struct{}

func (OutboxResolver) Resolve(_ context.Context, event outbox.Event) ([]outbox.Destination, error) {
	if event.ResourceType != "issue" || (event.Event != "issue.updated" && event.Event != "issue.resolved") {
		return nil, nil
	}
	return []outbox.Destination{{
		Kind: "reporter", ID: event.ResourceID, TaskType: TaskReporterNotify, Queue: email.QueueName,
	}}, nil
}

type ReporterTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
}

type ReporterNotification struct {
	OutboxEventID string
	TenantID      string
	TenantName    string
	IssueID       string
	PublicID      string
	Title         string
	Status        Status
	ReporterEmail string
	Notify        bool
	Event         string
}

type DeliveryAttempt struct {
	ID        string
	Completed bool
}

func (s Store) LoadReporterNotification(
	ctx context.Context,
	task ReporterTask,
) (ReporterNotification, error) {
	var notification ReporterNotification
	err := s.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, issue.id, issue.public_id,
		       issue.title, COALESCE(update.new_status, issue.status), issue.reporter_email,
		       issue.notify_reporter, event.event
		FROM outbox_events AS event
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		JOIN issues AS issue
		  ON issue.id = event.resource_id AND issue.tenant_id = event.tenant_id
		LEFT JOIN issue_updates AS update
		  ON update.id::text = event.event_key AND update.tenant_id = event.tenant_id
		WHERE event.id = $1 AND event.tenant_id = $2 AND event.resource_id = $3
		  AND event.resource_type = 'issue'
		  AND event.event IN ('issue.updated', 'issue.resolved')
	`, task.OutboxEventID, task.TenantID, task.ResourceID).Scan(
		&notification.OutboxEventID, &notification.TenantID, &notification.TenantName,
		&notification.IssueID, &notification.PublicID, &notification.Title,
		&notification.Status, &notification.ReporterEmail, &notification.Notify,
		&notification.Event,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterNotification{}, ErrNotFound
	}
	if err != nil {
		return ReporterNotification{}, fmt.Errorf("load reporter notification: %w", err)
	}
	return notification, nil
}

func (s Store) StartDeliveryAttempt(
	ctx context.Context,
	notification ReporterNotification,
	attemptNumber int,
) (DeliveryAttempt, error) {
	if _, err := s.Database.Exec(ctx, `
		UPDATE delivery_logs
		SET status = 'failed', error_message = 'worker attempt interrupted before completion'
		WHERE outbox_event_id = $1 AND delivery_key = 'reporter'
		  AND status = 'pending' AND attempt_number < $2
	`, notification.OutboxEventID, attemptNumber); err != nil {
		return DeliveryAttempt{}, fmt.Errorf("close interrupted delivery attempts: %w", err)
	}
	var attempt DeliveryAttempt
	var status string
	err := s.Database.QueryRow(ctx, `
		INSERT INTO delivery_logs (
			tenant_id, outbox_event_id, event, resource_type, resource_id, attempt_number
		) VALUES ($1, $2, $3, 'issue', $4, $5)
		ON CONFLICT (outbox_event_id, delivery_key, attempt_number) DO UPDATE
		SET status = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.status
		        ELSE 'pending'
		    END,
		    error_message = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.error_message
		        ELSE NULL
		    END
		RETURNING id, status
	`, notification.TenantID, notification.OutboxEventID, notification.Event,
		notification.IssueID, attemptNumber).Scan(&attempt.ID, &status)
	if err != nil {
		return DeliveryAttempt{}, fmt.Errorf("start delivery attempt: %w", err)
	}
	attempt.Completed = status == "sent" || status == "bounced"
	return attempt, nil
}

func (s Store) MarkDeliverySent(
	ctx context.Context,
	attemptID string,
	providerMessageID string,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE delivery_logs
		SET status = 'sent', provider_message_id = $2, error_message = NULL, sent_at = $3
		WHERE id = $1
	`, attemptID, providerMessageID, now.UTC()); err != nil {
		return fmt.Errorf("mark delivery sent: %w", err)
	}
	return nil
}

func (s Store) MarkDeliveryFailed(ctx context.Context, attemptID string, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE delivery_logs
		SET status = 'failed', provider_message_id = NULL, error_message = $2, sent_at = NULL
		WHERE id = $1
	`, attemptID, message); err != nil {
		return fmt.Errorf("mark delivery failed: %w", err)
	}
	return nil
}

type ReporterHandler struct {
	Store         Store
	Sender        email.Sender
	Tokens        *auth.JWTManager
	PublicBaseURL string
	Now           func() time.Time
}

func (h ReporterHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload ReporterTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.OutboxEventID == "" || payload.TenantID == "" || payload.ResourceID == "" {
			return fmt.Errorf("decode reporter notification task: %w", asynq.SkipRetry)
		}
		notification, err := h.Store.LoadReporterNotification(ctx, payload)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("reporter notification no longer exists: %w", asynq.SkipRetry)
		}
		if err != nil {
			return err
		}
		if !notification.Notify || strings.TrimSpace(notification.ReporterEmail) == "" {
			return nil
		}
		token, _, err := h.Tokens.IssueReporterUnsubscribe(notification.IssueID, notification.TenantID)
		if err != nil {
			return fmt.Errorf("issue reporter unsubscribe token: %w", err)
		}
		message := reporterMessage(notification, h.PublicBaseURL, token)
		retryCount, _ := asynq.GetRetryCount(ctx)
		attempt, err := h.Store.StartDeliveryAttempt(ctx, notification, retryCount+1)
		if err != nil {
			return err
		}
		if attempt.Completed {
			return nil
		}
		providerID, err := h.Sender.Send(ctx, message)
		if err != nil {
			markErr := h.Store.MarkDeliveryFailed(ctx, attempt.ID, err.Error())
			return errors.Join(fmt.Errorf("send reporter notification: %w", err), markErr)
		}
		now := time.Now().UTC()
		if h.Now != nil {
			now = h.Now().UTC()
		}
		return h.Store.MarkDeliverySent(ctx, attempt.ID, providerID, now)
	}
}

func reporterMessage(notification ReporterNotification, publicBaseURL, token string) email.Message {
	unsubscribeURL := strings.TrimRight(publicBaseURL, "/") +
		"/v1/reporter/unsubscribe?token=" + url.QueryEscape(token)
	status := strings.ReplaceAll(string(notification.Status), "_", " ")
	from := (&mail.Address{
		Name: notification.TenantName + " via Fidibeki", Address: "updates@fidibeki.app",
	}).String()
	text := fmt.Sprintf(
		"%s updated issue %s to %s.\n\n%s\n\nUnsubscribe: %s",
		notification.TenantName, notification.PublicID, status, notification.Title, unsubscribeURL,
	)
	htmlBody := fmt.Sprintf(
		"<p>%s updated issue <strong>%s</strong> to <strong>%s</strong>.</p>"+
			"<p>%s</p><p><a href=\"%s\">Unsubscribe from updates</a></p>",
		html.EscapeString(notification.TenantName), html.EscapeString(notification.PublicID),
		html.EscapeString(status), html.EscapeString(notification.Title), html.EscapeString(unsubscribeURL),
	)
	return email.Message{
		From: from, To: notification.ReporterEmail,
		Subject: "Update on " + notification.PublicID, Text: text, HTML: htmlBody,
		IdempotencyKey: "reporter:" + notification.OutboxEventID,
	}
}
