package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

type DeliveryTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
	IntegrationID string `json:"destination_id"`
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type DeliveryHandler struct {
	Store  Store
	Client HTTPDoer
	Now    func() time.Time
}

func (s Store) LoadNotification(ctx context.Context, task DeliveryTask) (Notification, error) {
	var notification Notification
	var plan tenant.Plan
	var status tenant.Status
	var headersCiphertext []byte
	var secretCiphertext []byte
	var channel *string
	err := s.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, tenant.plan, tenant.status,
		       integration.id, integration.kind, integration.webhook_url, integration.channel,
		       integration.custom_headers_ciphertext, integration.webhook_secret_ciphertext,
		       event.event, event.resource_type, event.resource_id, event.created_at,
		       COALESCE(feedback.public_id, issue.public_id),
		       COALESCE(issue.title, ''), COALESCE(feedback.text, ''),
		       COALESCE(feedback.page_url, ''), feedback.rating,
		       COALESCE(feedback_issue.public_id, issue.public_id, ''),
		       COALESCE(issue.status::text, feedback_issue.status::text, '')
		FROM outbox_events AS event
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		JOIN integrations AS integration
		  ON integration.id = $4 AND integration.tenant_id = event.tenant_id
		LEFT JOIN feedbacks AS feedback
		  ON event.resource_type = 'feedback' AND feedback.id = event.resource_id
		 AND feedback.tenant_id = event.tenant_id
		LEFT JOIN issues AS feedback_issue
		  ON feedback_issue.id = feedback.issue_id AND feedback_issue.tenant_id = event.tenant_id
		LEFT JOIN issues AS issue
		  ON event.resource_type = 'issue' AND issue.id = event.resource_id
		 AND issue.tenant_id = event.tenant_id
		WHERE event.id = $1 AND event.tenant_id = $2 AND event.resource_id = $3
		  AND integration.is_active = true
		  AND event.event = ANY(integration.events)
	`, task.OutboxEventID, task.TenantID, task.ResourceID, task.IntegrationID).Scan(
		&notification.OutboxEventID, &notification.TenantID, &notification.TenantName,
		&plan, &status, &notification.IntegrationID, &notification.Kind,
		&notification.WebhookURL, &channel, &headersCiphertext, &secretCiphertext,
		&notification.Event, &notification.ResourceType, &notification.ResourceID,
		&notification.OccurredAt, &notification.PublicID, &notification.Title,
		&notification.Text, &notification.PageURL, &notification.Rating,
		&notification.IssuePublicID, &notification.IssueStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrNotFound
	}
	if err != nil {
		return Notification{}, fmt.Errorf("load integration notification: %w", err)
	}
	if status != tenant.StatusActive && status != tenant.StatusTrial ||
		!config.CanUseFeature(tenant.Tenant{Plan: plan}, config.FeatureSlack) {
		return Notification{}, ErrNotFound
	}
	if channel != nil {
		notification.Channel = *channel
	}
	if len(headersCiphertext) > 0 {
		plaintext, err := s.Cipher.Decrypt(
			headersCiphertext, encryptionAAD(notification.TenantID, notification.IntegrationID, "headers"),
		)
		if err != nil {
			return Notification{}, err
		}
		if err := json.Unmarshal(plaintext, &notification.CustomHeaders); err != nil {
			return Notification{}, fmt.Errorf("decode integration delivery headers: %w", err)
		}
	}
	if len(secretCiphertext) > 0 {
		plaintext, err := s.Cipher.Decrypt(
			secretCiphertext, encryptionAAD(notification.TenantID, notification.IntegrationID, "webhook-secret"),
		)
		if err != nil {
			return Notification{}, err
		}
		notification.WebhookSecret = string(plaintext)
	}
	if !supportedEvent(notification.Event, notification.ResourceType) || notification.PublicID == "" {
		return Notification{}, ErrNotFound
	}
	return notification, nil
}

func (s Store) StartDeliveryAttempt(
	ctx context.Context,
	notification Notification,
	attemptNumber int,
) (DeliveryAttempt, error) {
	deliveryKey := "integration:" + notification.IntegrationID
	if _, err := s.Database.Exec(ctx, `
		UPDATE delivery_logs
		SET status = 'failed', error_message = 'worker attempt interrupted before completion'
		WHERE outbox_event_id = $1 AND delivery_key = $2
		  AND status = 'pending' AND attempt_number < $3
	`, notification.OutboxEventID, deliveryKey, attemptNumber); err != nil {
		return DeliveryAttempt{}, fmt.Errorf("close interrupted integration delivery attempts: %w", err)
	}
	var attempt DeliveryAttempt
	var status string
	err := s.Database.QueryRow(ctx, `
		INSERT INTO delivery_logs (
			tenant_id, outbox_event_id, integration_id, delivery_key, event,
			resource_type, resource_id, attempt_number
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
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
	`, notification.TenantID, notification.OutboxEventID, notification.IntegrationID,
		deliveryKey, notification.Event, notification.ResourceType,
		notification.ResourceID, attemptNumber).Scan(&attempt.ID, &status)
	if err != nil {
		return DeliveryAttempt{}, fmt.Errorf("start integration delivery attempt: %w", err)
	}
	attempt.Completed = status == "sent" || status == "bounced"
	return attempt, nil
}

func (s Store) MarkDeliverySent(ctx context.Context, attemptID, providerMessageID string, now time.Time) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE delivery_logs
		SET status = 'sent', provider_message_id = $2, error_message = NULL, sent_at = $3
		WHERE id = $1
	`, attemptID, providerMessageID, now.UTC()); err != nil {
		return fmt.Errorf("mark integration delivery sent: %w", err)
	}
	return nil
}

func (s Store) MarkDeliveryFailed(ctx context.Context, attemptID, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE delivery_logs
		SET status = 'failed', provider_message_id = NULL, error_message = $2, sent_at = NULL
		WHERE id = $1
	`, attemptID, message); err != nil {
		return fmt.Errorf("mark integration delivery failed: %w", err)
	}
	return nil
}

func (h DeliveryHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload DeliveryTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.OutboxEventID == "" || payload.TenantID == "" ||
			payload.ResourceID == "" || payload.IntegrationID == "" {
			return fmt.Errorf("decode integration delivery task: %w", asynq.SkipRetry)
		}
		notification, err := h.Store.LoadNotification(ctx, payload)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		retryCount, _ := asynq.GetRetryCount(ctx)
		attempt, err := h.Store.StartDeliveryAttempt(ctx, notification, retryCount+1)
		if err != nil {
			return err
		}
		if attempt.Completed {
			return nil
		}
		providerID, err := h.deliver(ctx, notification)
		if err != nil {
			markErr := h.Store.MarkDeliveryFailed(ctx, attempt.ID, err.Error())
			return errors.Join(err, markErr)
		}
		now := time.Now().UTC()
		if h.Now != nil {
			now = h.Now().UTC()
		}
		return h.Store.MarkDeliverySent(ctx, attempt.ID, providerID, now)
	}
}

func (h DeliveryHandler) deliver(ctx context.Context, notification Notification) (string, error) {
	body, err := notificationBody(notification)
	if err != nil {
		return "", fmt.Errorf("encode integration notification: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, notification.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create integration request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Fidibeki-Integrations/1.0")
	request.Header.Set("X-Fidibeki-Delivery", notification.OutboxEventID+":"+notification.IntegrationID)
	request.Header.Set("X-Fidibeki-Event", notification.Event)
	for name, value := range notification.CustomHeaders {
		request.Header.Set(name, value)
	}
	if notification.Kind == KindWebhook {
		mac := hmac.New(sha256.New, []byte(notification.WebhookSecret))
		_, _ = mac.Write(body)
		request.Header.Set("X-Fidibeki-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	client := h.Client
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("send integration notification: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", fmt.Errorf("integration endpoint returned %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	providerID := response.Header.Get("X-Slack-Req-Id")
	if providerID == "" {
		providerID = response.Header.Get("Request-Id")
	}
	if providerID == "" {
		providerID = fmt.Sprintf("http-%d", response.StatusCode)
	}
	return providerID, nil
}

func notificationBody(notification Notification) ([]byte, error) {
	switch notification.Kind {
	case KindSlack:
		return json.Marshal(slackMessage(notification))
	case KindTeams:
		return json.Marshal(teamsMessage(notification))
	case KindWebhook:
		return json.Marshal(webhookMessage(notification))
	default:
		return nil, fmt.Errorf("unsupported integration kind %q", notification.Kind)
	}
}

func slackMessage(notification Notification) map[string]any {
	headline, detail := notificationCopy(notification)
	message := map[string]any{
		"text": headline,
		"blocks": []map[string]any{
			{"type": "header", "text": map[string]string{"type": "plain_text", "text": headline}},
			{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": detail}},
			{"type": "context", "elements": []map[string]string{{
				"type": "mrkdwn", "text": notification.TenantName + " | " + notification.Event,
			}}},
		},
	}
	if notification.Channel != "" {
		message["channel"] = notification.Channel
	}
	return message
}

func teamsMessage(notification Notification) map[string]any {
	headline, detail := notificationCopy(notification)
	return map[string]any{
		"@type": "MessageCard", "@context": "https://schema.org/extensions",
		"summary": headline, "themeColor": "167D64",
		"sections": []map[string]any{{
			"activityTitle": headline, "text": detail,
			"facts": []map[string]string{
				{"name": "Event", "value": notification.Event},
				{"name": "Tenant", "value": notification.TenantName},
			},
		}},
	}
}

func webhookMessage(notification Notification) map[string]any {
	data := map[string]any{
		"id": notification.ResourceID, "public_id": notification.PublicID,
		"title": notification.Title, "text": notification.Text,
		"page_url": notification.PageURL, "rating": notification.Rating,
		"issue_public_id": notification.IssuePublicID, "issue_status": notification.IssueStatus,
	}
	return map[string]any{
		"id": notification.OutboxEventID, "event": notification.Event,
		"occurred_at":   notification.OccurredAt.UTC(),
		"resource_type": notification.ResourceType, "data": data,
	}
}

func notificationCopy(notification Notification) (string, string) {
	if notification.ResourceType == "feedback" {
		headline := "New feedback " + notification.PublicID
		detail := strings.TrimSpace(notification.Text)
		if detail == "" {
			detail = "Feedback received without a text comment."
		}
		if notification.Rating != nil {
			detail = fmt.Sprintf("Rating: %d/5\n%s", *notification.Rating, detail)
		}
		if notification.IssuePublicID != "" {
			detail += "\nIssue: " + notification.IssuePublicID
		}
		return headline, detail
	}
	headline := "Issue " + notification.PublicID + " updated"
	if notification.Event == EventIssueCreated {
		headline = "New issue " + notification.PublicID
	}
	detail := notification.Title
	if notification.IssueStatus != "" {
		detail += "\nStatus: " + strings.ReplaceAll(notification.IssueStatus, "_", " ")
	}
	return headline, detail
}
