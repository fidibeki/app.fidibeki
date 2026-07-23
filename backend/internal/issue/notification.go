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

	"github.com/acidiney/app.fidibeki/backend/internal/activity"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

const TaskReporterNotify = "issue:reporter-notify"

type OutboxResolver struct{}

func (OutboxResolver) Resolve(_ context.Context, event outbox.Event) ([]outbox.Destination, error) {
	issueEvent := event.ResourceType == "issue" &&
		(event.Event == "issue.created" || event.Event == "issue.updated" || event.Event == "issue.resolved")
	feedbackReceipt := event.ResourceType == "feedback" && event.Event == "feedback.created"
	if !issueEvent && !feedbackReceipt {
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
	OutboxEventID  string
	TenantID       string
	TenantName     string
	TenantSlug     string
	ResourceType   string
	ResourceID     string
	IssueID        string
	PublicID       string
	Title          string
	Status         Status
	ReporterEmail  string
	Notify         bool
	Event          string
	ReporterPrompt string
}

type DeliveryAttempt struct {
	ID        string
	Completed bool
}

func (s Store) LoadReporterNotification(
	ctx context.Context,
	task ReporterTask,
) (ReporterNotification, error) {
	var resourceType string
	err := s.Database.QueryRow(ctx, `
		SELECT resource_type FROM outbox_events
		WHERE id = $1 AND tenant_id = $2 AND resource_id = $3
	`, task.OutboxEventID, task.TenantID, task.ResourceID).Scan(&resourceType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterNotification{}, ErrNotFound
	}
	if err != nil {
		return ReporterNotification{}, fmt.Errorf("load reporter event: %w", err)
	}
	if resourceType == "feedback" {
		return s.loadFeedbackReceipt(ctx, task)
	}
	if resourceType != "issue" {
		return ReporterNotification{}, ErrNotFound
	}
	return s.loadIssueNotification(ctx, task)
}

func (s Store) loadIssueNotification(
	ctx context.Context,
	task ReporterTask,
) (ReporterNotification, error) {
	var notification ReporterNotification
	err := s.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, tenant.slug, event.resource_type,
		       issue.id, issue.public_id,
		       issue.title, COALESCE(update.new_status, issue.status), issue.reporter_email,
		       issue.notify_reporter, event.event, COALESCE(clarification.prompt, '')
		FROM outbox_events AS event
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		JOIN issues AS issue
		  ON issue.id = event.resource_id AND issue.tenant_id = event.tenant_id
		LEFT JOIN issue_updates AS update
		  ON update.id::text = event.event_key AND update.tenant_id = event.tenant_id
		LEFT JOIN reporter_clarification_requests AS clarification
		  ON clarification.requested_activity_id::text = (
		      SELECT activity.id::text FROM issue_activities AS activity
		      WHERE activity.tenant_id = event.tenant_id
		        AND activity.issue_id = issue.id
		        AND activity.data->>'legacy_update_id' = event.event_key
		      LIMIT 1
		  )
		WHERE event.id = $1 AND event.tenant_id = $2 AND event.resource_id = $3
		  AND event.resource_type = 'issue'
		  AND event.event IN ('issue.created', 'issue.updated', 'issue.resolved')
	`, task.OutboxEventID, task.TenantID, task.ResourceID).Scan(
		&notification.OutboxEventID, &notification.TenantID, &notification.TenantName,
		&notification.TenantSlug, &notification.ResourceType,
		&notification.IssueID, &notification.PublicID, &notification.Title,
		&notification.Status, &notification.ReporterEmail, &notification.Notify,
		&notification.Event, &notification.ReporterPrompt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterNotification{}, ErrNotFound
	}
	if err != nil {
		return ReporterNotification{}, fmt.Errorf("load reporter notification: %w", err)
	}
	notification.ResourceID = task.ResourceID
	return notification, nil
}

func (s Store) loadFeedbackReceipt(
	ctx context.Context,
	task ReporterTask,
) (ReporterNotification, error) {
	var notification ReporterNotification
	err := s.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, tenant.slug, event.resource_type,
		       feedback.public_id, feedback.title, feedback.reporter_email,
		       feedback.notify_reporter, event.event
		FROM outbox_events AS event
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		JOIN feedbacks AS feedback
		  ON feedback.id = event.resource_id AND feedback.tenant_id = event.tenant_id
		WHERE event.id = $1 AND event.tenant_id = $2 AND event.resource_id = $3
		  AND event.resource_type = 'feedback' AND event.event = 'feedback.created'
	`, task.OutboxEventID, task.TenantID, task.ResourceID).Scan(
		&notification.OutboxEventID, &notification.TenantID, &notification.TenantName,
		&notification.TenantSlug, &notification.ResourceType,
		&notification.PublicID, &notification.Title, &notification.ReporterEmail,
		&notification.Notify, &notification.Event,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterNotification{}, ErrNotFound
	}
	if err != nil {
		return ReporterNotification{}, fmt.Errorf("load reporter feedback receipt: %w", err)
	}
	notification.ResourceID = task.ResourceID
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
		) VALUES ($1, $2, $3, $4, $5, $6)
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
		notification.ResourceType, notification.ResourceID, attemptNumber).
		Scan(&attempt.ID, &status)
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
	BaseDomain    string
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
		retryCount, _ := asynq.GetRetryCount(ctx)
		attempt, err := h.Store.StartDeliveryAttempt(ctx, notification, retryCount+1)
		if err != nil {
			return err
		}
		if attempt.Completed {
			return h.Store.RecordReporterNotification(ctx, notification, h.now())
		}
		var unsubscribeToken string
		if notification.ResourceType == "feedback" {
			unsubscribeToken, _, err = h.Tokens.IssueFeedbackReporterUnsubscribe(
				notification.ResourceID, notification.TenantID,
			)
		} else {
			unsubscribeToken, _, err = h.Tokens.IssueReporterUnsubscribe(
				notification.IssueID, notification.TenantID,
			)
		}
		if err != nil {
			return fmt.Errorf("reporter unsubscribe token: %w", err)
		}
		portalToken := ""
		if notification.IssueID != "" {
			portalToken, _, err = h.Store.RotateReporterAccess(
				ctx, notification.TenantID, notification.IssueID, h.now(),
			)
			if err != nil {
				return err
			}
		}
		message := reporterMessage(
			notification, h.PublicBaseURL, h.BaseDomain, portalToken, unsubscribeToken,
		)
		providerID, err := h.Sender.Send(ctx, message)
		if err != nil {
			markErr := h.Store.MarkDeliveryFailed(ctx, attempt.ID, err.Error())
			return errors.Join(fmt.Errorf("send reporter notification: %w", err), markErr)
		}
		now := h.now()
		if err := h.Store.MarkDeliverySent(ctx, attempt.ID, providerID, now); err != nil {
			return err
		}
		return h.Store.RecordReporterNotification(ctx, notification, now)
	}
}

func (h ReporterHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Store) RecordReporterNotification(
	ctx context.Context,
	notification ReporterNotification,
	now time.Time,
) error {
	if notification.IssueID == "" {
		return nil
	}
	_, err := activity.Record(ctx, s.Database, activity.RecordInput{
		ID: notification.OutboxEventID, TenantID: notification.TenantID,
		IssueID: notification.IssueID, Kind: "reporter.notified",
		Visibility: activity.VisibilityReporter,
		Data: map[string]any{
			"channel": "email", "event": notification.Event, "status": notification.Status,
		}, CreatedAt: now,
	})
	return err
}

func reporterMessage(
	notification ReporterNotification,
	publicBaseURL string,
	baseDomain string,
	portalToken string,
	unsubscribeToken string,
) email.Message {
	unsubscribeURL := strings.TrimRight(publicBaseURL, "/") +
		"/v1/reporter/unsubscribe?token=" + url.QueryEscape(unsubscribeToken)
	portalURL := reporterPortalURL(publicBaseURL, baseDomain, notification.TenantSlug, portalToken)
	status := strings.ReplaceAll(string(notification.Status), "_", " ")
	from := (&mail.Address{
		Name: notification.TenantName + " via Fidibeki", Address: "updates@fidibeki.com",
	}).String()
	headline := notification.TenantName + " updated issue " + notification.PublicID + " to " + status + "."
	subject := "Update on " + notification.PublicID
	if notification.ResourceType == "feedback" {
		headline = notification.TenantName + " received your feedback " + notification.PublicID + "."
		subject = "Feedback received: " + notification.PublicID
	}
	if notification.Event == "issue.created" {
		headline = notification.TenantName + " created issue " + notification.PublicID + " from your feedback."
	}
	if notification.Status == StatusResolved || notification.Status == StatusClosed {
		headline = notification.TenantName + " completed issue " + notification.PublicID + "."
	}
	textParts := []string{headline, notification.Title}
	htmlParts := []string{
		"<p>" + html.EscapeString(headline) + "</p>",
		"<p>" + html.EscapeString(notification.Title) + "</p>",
	}
	if notification.ReporterPrompt != "" {
		textParts = append(textParts, "Information requested: "+notification.ReporterPrompt)
		htmlParts = append(htmlParts, "<p><strong>Information requested</strong><br>"+
			html.EscapeString(notification.ReporterPrompt)+"</p>")
	}
	if portalURL != "" {
		textParts = append(textParts, "View progress: "+portalURL)
		htmlParts = append(htmlParts, "<p><a href=\""+html.EscapeString(portalURL)+"\">View progress</a></p>")
	}
	textParts = append(textParts, "Unsubscribe: "+unsubscribeURL)
	htmlParts = append(htmlParts, "<p><a href=\""+html.EscapeString(unsubscribeURL)+"\">Unsubscribe from updates</a></p>")
	return email.Message{
		From: from, To: notification.ReporterEmail,
		Subject: subject,
		Text:    strings.Join(textParts, "\n\n"), HTML: strings.Join(htmlParts, ""),
		IdempotencyKey: "reporter:" + notification.OutboxEventID,
	}
}

func reporterPortalURL(publicBaseURL, baseDomain, tenantSlug, token string) string {
	if token == "" || baseDomain == "" || tenantSlug == "" {
		return ""
	}
	scheme := "https"
	if strings.HasPrefix(publicBaseURL, "http://") {
		scheme = "http"
	}
	return scheme + "://" + tenantSlug + "." + baseDomain + "/reporter/" + url.PathEscape(token)
}
