package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/activity"
	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
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
	ConsentActive  bool
	ConsentLocale  *string
	Event          string
	ReporterPrompt string
}

// reporterConsentActiveExpression evaluates to true only when notify_reporter is set AND the
// grant/withdrawal timestamps and consent purpose all agree consent is currently in force. A
// legacy row consented before structured evidence existed (reporter_consent_purpose IS NULL) never
// satisfies this expression, so it fails closed until the reporter re-confirms through a fresh
// opt-in submission. Table alias must be "issue" or "feedback".
func reporterConsentActiveExpression(alias string) string {
	return alias + `.notify_reporter AND ` + alias + `.reporter_consent_granted_at IS NOT NULL
		AND ` + alias + `.reporter_consent_withdrawn_at IS NULL
		AND ` + alias + `.reporter_consent_purpose = '` + feedback.ReporterConsentPurposeFeedbackUpdates + `'`
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
		       (`+reporterConsentActiveExpression("issue")+`),
		       issue.reporter_consent_locale, event.event, COALESCE(clarification.prompt, '')
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
		&notification.Status, &notification.ReporterEmail,
		&notification.ConsentActive, &notification.ConsentLocale,
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
		       (`+reporterConsentActiveExpression("feedback")+`),
		       feedback.reporter_consent_locale, event.event
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
		&notification.ConsentActive, &notification.ConsentLocale,
		&notification.Event,
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
	// BeforeDelivery, if set, runs after the notification is composed but before the
	// immediately-before-delivery consent recheck and provider send. It exists so tests can
	// deterministically simulate a reporter withdrawing consent during the window between the
	// initial load and the provider call; production wiring leaves it nil.
	BeforeDelivery func()
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
		if !notification.ConsentActive || strings.TrimSpace(notification.ReporterEmail) == "" {
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
		message, err := reporterMessage(
			notification, h.PublicBaseURL, h.BaseDomain, portalToken, unsubscribeToken,
		)
		if err != nil {
			markErr := h.Store.MarkDeliveryFailed(ctx, attempt.ID, err.Error())
			return errors.Join(fmt.Errorf("compose reporter notification: %w", err), markErr)
		}
		if h.BeforeDelivery != nil {
			h.BeforeDelivery()
		}
		// Re-verify consent immediately before the provider call: composing the message issued a
		// portal token and made further round-trips, leaving a window in which the reporter could
		// have withdrawn consent since the initial load above. This closes that revocation race.
		active, err := h.Store.ReporterConsentActive(ctx, notification)
		if err != nil {
			return err
		}
		if !active {
			return h.Store.MarkDeliveryFailed(ctx, attempt.ID, "reporter consent is no longer active")
		}
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

// ReporterConsentActive re-reads the current consent state for the notification's resource,
// independent of whatever was loaded earlier in the same task attempt. Callers use it immediately
// before provider delivery to close the window between the initial consent check and the send.
func (s Store) ReporterConsentActive(ctx context.Context, notification ReporterNotification) (bool, error) {
	table := "issues"
	if notification.ResourceType == "feedback" {
		table = "feedbacks"
	}
	var active bool
	err := s.Database.QueryRow(ctx, `
		SELECT `+reporterConsentActiveExpression(table)+`
		FROM `+table+` WHERE tenant_id = $1 AND id = $2
	`, notification.TenantID, notification.ResourceID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check reporter consent active: %w", err)
	}
	return active, nil
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
) (email.Message, error) {
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
	body := []string{headline, notification.Title}
	metadata := []email.Metadata{{Label: "Reference", Value: notification.PublicID}}
	if notification.ReporterPrompt != "" {
		body = append(body, "Information requested: "+notification.ReporterPrompt)
	}
	var action *email.Link
	if portalURL != "" {
		action = &email.Link{Label: "View progress", URL: portalURL}
	}
	logoURL, err := email.BrandLogoURL(publicBaseURL, baseDomain)
	if err != nil {
		return email.Message{}, err
	}
	footerLinks := []email.Link{{Label: "Unsubscribe", URL: unsubscribeURL}}
	if privacyURL := reporterPrivacyURL(publicBaseURL, baseDomain); privacyURL != "" {
		footerLinks = append(footerLinks, email.Link{Label: "Privacy", URL: privacyURL})
	}
	return email.Compose(email.Message{
		From: from, To: notification.ReporterEmail,
		Subject:        subject,
		IdempotencyKey: "reporter:" + notification.OutboxEventID,
	}, email.Content{
		Preheader: headline, Heading: subject, Workspace: notification.TenantName,
		Body: body, Metadata: metadata, Action: action,
		Footer:      "You received this because you asked for updates about this feedback.",
		FooterLinks: footerLinks,
		LogoURL:     logoURL,
	})
}

// reporterPrivacyURL points to the workspace-independent published privacy policy, giving every
// reporter email a link to the privacy information alongside its scoped unsubscribe action.
func reporterPrivacyURL(publicBaseURL, baseDomain string) string {
	if baseDomain == "" {
		return ""
	}
	scheme := "https"
	if strings.HasPrefix(publicBaseURL, "http://") {
		scheme = "http"
	}
	return scheme + "://" + baseDomain + "/privacy"
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
