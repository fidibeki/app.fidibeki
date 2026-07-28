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
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/config"
	"github.com/acidiney/app.fidibeki/backend/internal/moderation"
	"github.com/acidiney/app.fidibeki/backend/internal/outboundhttp"
	"github.com/acidiney/app.fidibeki/backend/internal/privacy"
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
	Store    Store
	Client   HTTPDoer
	Outbound outboundhttp.Policy
	// AllowInsecureURLs is restricted to local/test environments so their
	// loopback webhook stubs remain reachable.
	AllowInsecureURLs bool
	PublicBaseURL     string
	BaseDomain        string
	Now               func() time.Time
}

func (s Store) LoadNotification(ctx context.Context, task DeliveryTask) (Notification, error) {
	var notification Notification
	var plan tenant.Plan
	var tenantDeliveryEligible bool
	var headersCiphertext []byte
	var secretCiphertext []byte
	var channel *string
	var changesJSON []byte
	var actorID, actorName *string
	var activityKind string
	err := s.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, tenant.slug, tenant.plan,
		       `+tenant.OutboundDeliveryEligibleSQL(3)+`,
		       integration.id, integration.kind, integration.webhook_url, integration.channel,
		       integration.custom_headers_ciphertext, integration.webhook_secret_ciphertext,
		       integration.privacy_fields, integration.privacy_profile_version,
		       event.event, event.resource_type, event.resource_id, event.created_at,
		       COALESCE(feedback.public_id, issue.public_id),
		       COALESCE(feedback.title, issue.title, ''),
		       COALESCE(feedback.description, issue.description, ''),
		       COALESCE(feedback.text, ''),
		       COALESCE(feedback.ai_suggested_title, ''),
		       COALESCE(feedback.status::text, ''),
		       COALESCE(feedback.text_process_status::text, ''),
		       COALESCE(feedback.text_moderation_flags, '{}'::text[]),
		       feedback.ai_processed_at IS NOT NULL,
		       COALESCE(feedback.source::text, primary_feedback.source::text, 'web'),
		       COALESCE(feedback.mobile_application_id, primary_feedback.mobile_application_id, ''),
		       COALESCE(feedback.mobile_screen, primary_feedback.mobile_screen, ''),
		       COALESCE(feedback.mobile_app_version, primary_feedback.mobile_app_version, ''),
		       COALESCE(feedback.mobile_os_version, primary_feedback.mobile_os_version, ''),
		       COALESCE(feedback.page_url, primary_feedback.page_url, ''),
		       COALESCE(feedback.rating, primary_feedback.rating),
		       COALESCE(feedback_issue.id::text, issue.id::text, ''),
		       COALESCE(feedback_issue.public_id, issue.public_id, ''),
		       COALESCE(issue.status::text, feedback_issue.status::text, ''),
		       COALESCE(issue_update.old_status::text, ''),
		       COALESCE(issue_update.new_status::text, ''),
		       project.id::text, project.name,
		       COALESCE(feedback.id::text, primary_feedback.id::text, ''),
		       COALESCE(feedback.ai_category, primary_feedback.ai_category, ''),
		       COALESCE(feedback.ai_actionability, primary_feedback.ai_actionability, ''),
		       COALESCE(issue.priority::text, feedback.priority::text, feedback.ai_priority, ''),
		       COALESCE(issue_activity.data->'changes', '{}'::jsonb),
		       COALESCE(
		           issue_activity.data->>'transition_comment',
		           issue_activity.data->>'response',
		           issue_update.comment,
		           ''
		       ),
		       COALESCE(issue_activity.kind, ''),
		       actor.id::text, actor.name,
		       evidence.attachment_count, evidence.attachment_kinds,
		       evidence.attachment_feedback_id,
		       COALESCE(issue_moderation.title_moderated, false),
		       COALESCE(issue_moderation.description_moderated, false),
		       COALESCE(issue_moderation.suggested_title, ''),
		       ARRAY(
		           SELECT DISTINCT source.value
		           FROM feedbacks AS moderated_feedback
		           CROSS JOIN LATERAL unnest(ARRAY[
		               moderated_feedback.description,
		               moderated_feedback.text,
		               moderated_feedback.text_cleaned
		           ]) AS source(value)
		           WHERE moderated_feedback.issue_id = issue.id
		             AND (moderated_feedback.status = 'spam'
		                  OR cardinality(moderated_feedback.text_moderation_flags) > 0
		                  OR moderated_feedback.text_process_status = 'failed')
		             AND NULLIF(btrim(source.value), '') IS NOT NULL
		           ORDER BY source.value
		       )
		FROM outbox_events AS event
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		JOIN integrations AS integration
		  ON integration.id = $5 AND integration.tenant_id = event.tenant_id
		LEFT JOIN feedbacks AS feedback
		  ON event.resource_type = 'feedback' AND feedback.id = event.resource_id
		 AND feedback.tenant_id = event.tenant_id
		LEFT JOIN issues AS feedback_issue
		  ON feedback_issue.id = feedback.issue_id AND feedback_issue.tenant_id = event.tenant_id
		LEFT JOIN issues AS issue
		  ON event.resource_type = 'issue' AND issue.id = event.resource_id
		 AND issue.tenant_id = event.tenant_id
		JOIN projects AS project
		  ON project.tenant_id = event.tenant_id
		 AND project.id = COALESCE(feedback.project_id, issue.project_id)
		LEFT JOIN issue_updates AS issue_update
		  ON issue_update.tenant_id = event.tenant_id
		 AND issue_update.id::text = event.event_key
		LEFT JOIN LATERAL (
			SELECT linked.id, linked.page_url, linked.rating, linked.source,
			       linked.mobile_application_id, linked.mobile_screen,
			       linked.mobile_app_version, linked.mobile_os_version,
			       linked.ai_category, linked.ai_actionability
			FROM feedbacks AS linked
			WHERE linked.tenant_id = event.tenant_id AND linked.issue_id = issue.id
			ORDER BY linked.created_at, linked.id
			LIMIT 1
		) AS primary_feedback ON event.resource_type = 'issue'
		LEFT JOIN LATERAL (
			SELECT
				bool_or(
					(feedback.status = 'spam' OR cardinality(feedback.text_moderation_flags) > 0
					 OR feedback.text_process_status = 'failed')
					AND (
						(NULLIF(feedback.description, '') IS NOT NULL AND strpos(issue.title, feedback.description) > 0)
						OR (NULLIF(feedback.description, '') IS NOT NULL AND strpos(feedback.description, issue.title) > 0)
						OR (NULLIF(feedback.text, '') IS NOT NULL AND strpos(issue.title, feedback.text) > 0)
						OR (NULLIF(feedback.text, '') IS NOT NULL AND strpos(feedback.text, issue.title) > 0)
						OR (NULLIF(feedback.text_cleaned, '') IS NOT NULL AND strpos(issue.title, feedback.text_cleaned) > 0)
						OR (NULLIF(feedback.text_cleaned, '') IS NOT NULL AND strpos(feedback.text_cleaned, issue.title) > 0)
					)
				) AS title_moderated,
				bool_or(
					(feedback.status = 'spam' OR cardinality(feedback.text_moderation_flags) > 0
					 OR feedback.text_process_status = 'failed')
					AND (
						(NULLIF(feedback.description, '') IS NOT NULL AND strpos(issue.description, feedback.description) > 0)
						OR (NULLIF(feedback.text, '') IS NOT NULL AND strpos(issue.description, feedback.text) > 0)
						OR (NULLIF(feedback.text_cleaned, '') IS NOT NULL AND strpos(issue.description, feedback.text_cleaned) > 0)
					)
				) AS description_moderated,
				(ARRAY_AGG(feedback.ai_suggested_title ORDER BY feedback.created_at, feedback.id)
				 FILTER (WHERE feedback.ai_suggested_title IS NOT NULL))[1] AS suggested_title
			FROM feedbacks AS feedback WHERE feedback.issue_id = issue.id
		) AS issue_moderation ON event.resource_type = 'issue'
		LEFT JOIN LATERAL (
			SELECT activity.kind, activity.actor_member_id, activity.data
			FROM issue_activities AS activity
			WHERE activity.tenant_id = event.tenant_id AND activity.issue_id = issue.id
			  AND (
			      (event.event_key IS NOT NULL AND (
			          activity.id::text = event.event_key
			          OR activity.data->>'legacy_update_id' = event.event_key
			      ))
			      OR (event.event_key IS NULL AND event.event = 'issue.created'
			          AND activity.kind = 'issue.created')
			  )
			ORDER BY activity.created_at DESC, activity.id DESC
			LIMIT 1
		) AS issue_activity ON true
		LEFT JOIN tenant_members AS actor
		  ON actor.tenant_id = event.tenant_id AND actor.id = issue_activity.actor_member_id
		LEFT JOIN LATERAL (
			SELECT count(*)::integer AS attachment_count,
			       COALESCE(array_agg(DISTINCT attachment.kind::text), '{}'::text[]) AS attachment_kinds,
			       COALESCE(min(attachment.feedback_id::text), '') AS attachment_feedback_id
			FROM attachments AS attachment
			JOIN feedbacks AS attached_feedback
			  ON attached_feedback.tenant_id = attachment.tenant_id
			 AND attached_feedback.id = attachment.feedback_id
				WHERE attachment.tenant_id = event.tenant_id AND attachment.completed_at IS NOT NULL
				  AND attachment.safety_status = 'safe'
				  AND attachment.retention_deleted_at IS NULL
			  AND (
			      (event.resource_type = 'feedback' AND attachment.feedback_id = feedback.id)
			      OR (event.resource_type = 'issue' AND attached_feedback.issue_id = issue.id)
			  )
		) AS evidence ON true
		WHERE event.id = $1 AND event.tenant_id = $2 AND event.resource_id = $4
		  AND integration.is_active = true
		  AND event.event = ANY(integration.events)
	`, task.OutboxEventID, task.TenantID, integrationNow(s.Now),
		task.ResourceID, task.IntegrationID).Scan(
		&notification.OutboxEventID, &notification.TenantID, &notification.TenantName,
		&notification.TenantSlug, &plan, &tenantDeliveryEligible,
		&notification.IntegrationID, &notification.Kind,
		&notification.WebhookURL, &channel, &headersCiphertext, &secretCiphertext,
		&notification.PrivacyFields, &notification.PrivacyProfileVersion,
		&notification.Event, &notification.ResourceType, &notification.ResourceID,
		&notification.OccurredAt, &notification.PublicID, &notification.Title,
		&notification.Description, &notification.Text, &notification.SuggestedTitle,
		&notification.FeedbackStatus, &notification.TextProcessStatus,
		&notification.TextModerationFlags, &notification.AIProcessed,
		&notification.Source, &notification.MobileApplicationID,
		&notification.MobileScreen, &notification.MobileAppVersion,
		&notification.MobileOSVersion,
		&notification.PageURL, &notification.Rating,
		&notification.IssueID, &notification.IssuePublicID, &notification.IssueStatus,
		&notification.OldStatus, &notification.NewStatus,
		&notification.ProjectID, &notification.ProjectName, &notification.FeedbackID,
		&notification.Category, &notification.Actionability, &notification.Priority,
		&changesJSON, &notification.Comment, &activityKind,
		&actorID, &actorName,
		&notification.AttachmentCount, &notification.AttachmentKinds,
		&notification.AttachmentFeedbackID,
		&notification.IssueTitleModerated, &notification.IssueDescriptionModerated,
		&notification.IssueSuggestedTitle, &notification.IssueModeratedSources,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrNotFound
	}
	if err != nil {
		return Notification{}, fmt.Errorf("load integration notification: %w", err)
	}
	notification.DestinationCapabilities = destinationCapabilities()
	if !tenantDeliveryEligible {
		return notification, ErrTenantDeliverySuppressed
	}
	if !config.CanUseFeature(tenant.Tenant{Plan: plan}, config.FeatureSlack) {
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
	notification.Changes = make(map[string]Change)
	if len(changesJSON) > 0 {
		if err := json.Unmarshal(changesJSON, &notification.Changes); err != nil {
			return Notification{}, fmt.Errorf("decode integration changes: %w", err)
		}
	}
	if actorID != nil {
		notification.Actor = &NotificationActor{Type: "member", ID: *actorID}
		if actorName != nil {
			notification.Actor.Name = *actorName
		}
	} else if activityKind == "reporter.responded" {
		notification.Actor = &NotificationActor{Type: "reporter", Name: "Reporter"}
	}
	if !supportedEvent(notification.Event, notification.ResourceType) || notification.PublicID == "" {
		return Notification{}, ErrNotFound
	}
	switch notification.ResourceType {
	case "feedback":
		if !notification.AIProcessed && notification.FeedbackStatus != "spam" {
			return Notification{}, ErrNotReady
		}
		moderateNotification(&notification)
	case "issue":
		moderateIssueNotification(&notification)
	}
	return notification, nil
}

func moderateIssueNotification(notification *Notification) {
	if notification.IssueTitleModerated {
		if strings.TrimSpace(notification.IssueSuggestedTitle) != "" {
			notification.Title = notification.IssueSuggestedTitle
		} else {
			notification.Title = moderation.Placeholder
		}
		if change, exists := notification.Changes["title"]; exists {
			change.From, change.To = moderation.Placeholder, moderation.Placeholder
			notification.Changes["title"] = change
		}
	}
	if notification.IssueDescriptionModerated {
		notification.Description = moderation.Placeholder
		if change, exists := notification.Changes["description"]; exists {
			change.From, change.To = moderation.Placeholder, moderation.Placeholder
			notification.Changes["description"] = change
		}
	}
	for _, field := range []string{"title", "description"} {
		change, exists := notification.Changes[field]
		if !exists {
			continue
		}
		change.From = redactIssueChangeValue(change.From, notification.IssueModeratedSources)
		change.To = redactIssueChangeValue(change.To, notification.IssueModeratedSources)
		notification.Changes[field] = change
	}
}

func redactIssueChangeValue(value any, sources []string) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	return moderation.RedactDerived(text, sources...)
}

func moderateNotification(notification *Notification) {
	if notification.ResourceType != "feedback" ||
		(notification.FeedbackStatus != "spam" && notification.TextProcessStatus != "failed" &&
			len(notification.TextModerationFlags) == 0) {
		return
	}
	if strings.TrimSpace(notification.SuggestedTitle) != "" {
		notification.Title = notification.SuggestedTitle
	} else {
		notification.Title = moderation.Placeholder
	}
	notification.Description = moderation.Placeholder
	notification.Text = moderation.Placeholder
}

func (s Store) StartDeliveryAttempt(
	ctx context.Context,
	notification Notification,
	attemptNumber int,
) (DeliveryAttempt, error) {
	capabilities, err := json.Marshal(notification.DestinationCapabilities)
	if err != nil {
		return DeliveryAttempt{}, fmt.Errorf("encode integration destination capabilities: %w", err)
	}
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
	err = s.Database.QueryRow(ctx, `
		INSERT INTO delivery_logs (
			tenant_id, outbox_event_id, integration_id, delivery_key, event,
			resource_type, resource_id, attempt_number, destination_kind,
			privacy_profile_version, data_categories, destination_capabilities
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (outbox_event_id, delivery_key, attempt_number) DO UPDATE
		SET status = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.status
		        ELSE 'pending'
		    END,
		    error_message = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.error_message
		        ELSE NULL
		    END,
		    destination_kind = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.destination_kind
		        ELSE EXCLUDED.destination_kind
		    END,
		    privacy_profile_version = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.privacy_profile_version
		        ELSE EXCLUDED.privacy_profile_version
		    END,
		    data_categories = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.data_categories
		        ELSE EXCLUDED.data_categories
		    END,
		    destination_capabilities = CASE
		        WHEN delivery_logs.status IN ('sent', 'bounced') THEN delivery_logs.destination_capabilities
		        ELSE EXCLUDED.destination_capabilities
		    END
		RETURNING id, status
	`, notification.TenantID, notification.OutboxEventID, notification.IntegrationID,
		deliveryKey, notification.Event, notification.ResourceType,
		notification.ResourceID, attemptNumber, notification.Kind,
		notification.PrivacyProfileVersion, notificationDataCategories(notification),
		string(capabilities)).Scan(&attempt.ID, &status)
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
		retryCount, _ := asynq.GetRetryCount(ctx)
		notification, err := h.Store.LoadNotification(ctx, payload)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if errors.Is(err, ErrTenantDeliverySuppressed) {
			attempt, startErr := h.Store.StartDeliveryAttempt(ctx, notification, retryCount+1)
			if startErr != nil {
				return startErr
			}
			if attempt.Completed {
				return nil
			}
			return h.Store.MarkDeliveryFailed(ctx, attempt.ID, ErrTenantDeliverySuppressed.Error())
		}
		if err != nil {
			return err
		}
		notification = withNotificationURLs(notification, h.PublicBaseURL, h.BaseDomain)
		attempt, err := h.Store.StartDeliveryAttempt(ctx, notification, retryCount+1)
		if err != nil {
			return err
		}
		if attempt.Completed {
			return nil
		}
		if err := privacy.EnsureResourceProcessingAllowed(
			ctx, h.Store.Database, notification.TenantID, notification.ResourceType,
			notification.ResourceID, privacy.PurposeIntegration,
		); err != nil {
			if errors.Is(err, privacy.ErrProcessingRestricted) {
				return h.Store.MarkDeliveryFailed(
					ctx, attempt.ID, "integration delivery suppressed by privacy restriction",
				)
			}
			return err
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
		if h.AllowInsecureURLs {
			client = &http.Client{
				Timeout:       10 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
		} else {
			client = h.Outbound.NewClient(10 * time.Second)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("send integration notification: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
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

func withNotificationURLs(
	notification Notification,
	publicBaseURL string,
	baseDomain string,
) Notification {
	domain := strings.TrimSuffix(strings.TrimSpace(baseDomain), ".")
	if domain == "" || notification.TenantSlug == "" {
		return notification
	}
	scheme := "https"
	if parsed, err := url.Parse(publicBaseURL); err == nil && parsed.Scheme != "" {
		scheme = parsed.Scheme
	}
	root := scheme + "://" + notification.TenantSlug + "." + domain
	if notification.IssueID != "" {
		notification.IssueURL = root + "/issues/" + url.PathEscape(notification.IssueID)
	}
	if notification.FeedbackID != "" {
		notification.FeedbackURL = root + "/feedback/" + url.PathEscape(notification.FeedbackID)
	}
	if notification.AttachmentCount > 0 && notification.AttachmentFeedbackID != "" {
		notification.AttachmentsURL = root + "/feedback/" +
			url.PathEscape(notification.AttachmentFeedbackID) + "#attachments"
	}
	if notification.ResourceType == "issue" {
		notification.ResourceURL = notification.IssueURL
	} else {
		notification.ResourceURL = notification.FeedbackURL
	}
	return notification
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
	blocks := []map[string]any{
		{"type": "header", "text": map[string]string{
			"type": "plain_text", "text": truncateIntegrationText(headline, 150),
		}},
		{"type": "section", "text": map[string]string{
			"type": "mrkdwn", "text": slackEscape(truncateIntegrationText(detail, 2800)),
		}},
	}
	facts := notificationFacts(notification)
	if len(facts) > 0 {
		fields := make([]map[string]string, 0, len(facts))
		for _, fact := range facts {
			fields = append(fields, map[string]string{
				"type": "mrkdwn", "text": "*" + slackEscape(fact.Name) + "*\n" +
					slackEscape(truncateIntegrationText(fact.Value, 1500)),
			})
		}
		blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
	}
	blocks = append(blocks, map[string]any{
		"type": "context", "elements": []map[string]string{{
			"type": "mrkdwn", "text": slackEscape(notification.Event) + " | " +
				notification.OccurredAt.UTC().Format(time.RFC3339),
		}},
	})
	actions := integrationActions(notification)
	if len(actions) > 0 {
		elements := make([]map[string]any, 0, len(actions))
		for _, action := range actions {
			elements = append(elements, map[string]any{
				"type": "button",
				"text": map[string]string{"type": "plain_text", "text": action.Label},
				"url":  action.URL,
			})
		}
		blocks = append(blocks, map[string]any{"type": "actions", "elements": elements})
	}
	message := map[string]any{
		"text":   headline,
		"blocks": blocks,
	}
	if notification.Channel != "" {
		message["channel"] = notification.Channel
	}
	return message
}

func teamsMessage(notification Notification) map[string]any {
	headline, detail := notificationCopy(notification)
	facts := notificationFacts(notification)
	teamFacts := make([]map[string]string, 0, len(facts)+1)
	teamFacts = append(teamFacts, map[string]string{"name": "Event", "value": notification.Event})
	for _, fact := range facts {
		teamFacts = append(teamFacts, map[string]string{
			"name":  htmlEscape(fact.Name),
			"value": htmlEscape(truncateIntegrationText(fact.Value, 1500)),
		})
	}
	message := map[string]any{
		"@type": "MessageCard", "@context": "https://schema.org/extensions",
		"summary": headline, "themeColor": "167D64",
		"sections": []map[string]any{{
			"activityTitle": htmlEscape(truncateIntegrationText(headline, 150)),
			"text":          htmlEscape(truncateIntegrationText(detail, 2800)),
			"facts":         teamFacts,
		}},
	}
	actions := integrationActions(notification)
	if len(actions) > 0 {
		potentialActions := make([]map[string]any, 0, len(actions))
		for _, action := range actions {
			potentialActions = append(potentialActions, map[string]any{
				"@type": "OpenUri", "name": action.Label,
				"targets": []map[string]string{{"os": "default", "uri": action.URL}},
			})
		}
		message["potentialAction"] = potentialActions
	}
	return message
}

func webhookMessage(notification Notification) map[string]any {
	data := map[string]any{
		"id": notification.ResourceID, "public_id": notification.PublicID,
		"url": notification.ResourceURL, "title": notification.Title,
		"issue_public_id": notification.IssuePublicID, "issue_status": notification.IssueStatus,
		"status": notificationStatus(notification), "priority": notification.Priority,
		"project": map[string]string{
			"id": notification.ProjectID, "name": notification.ProjectName,
		},
	}
	if notificationHasFeedbackContext(notification) {
		data["source"] = notificationSource(notification)
	}
	if notificationAllows(notification, PrivacyFieldDescription) {
		data["description"] = notification.Description
		data["text"] = notification.Text
	}
	if notificationAllows(notification, PrivacyFieldPage) {
		data["page_url"] = integrationSafePageURL(notification.PageURL)
		if notificationHasFeedbackContext(notification) &&
			notificationSource(notification) != "web" {
			data["mobile_context"] = map[string]string{
				"application_id": notification.MobileApplicationID,
				"screen":         notification.MobileScreen,
				"app_version":    notification.MobileAppVersion,
				"os_version":     notification.MobileOSVersion,
			}
		}
	}
	if notificationAllows(notification, PrivacyFieldRating) {
		data["rating"] = notification.Rating
	}
	if notificationAllows(notification, PrivacyFieldClassification) {
		data["classification"] = map[string]string{
			"category": notification.Category, "actionability": notification.Actionability,
		}
	}
	if notificationAllows(notification, PrivacyFieldAttachments) {
		data["attachments"] = map[string]any{
			"count": notification.AttachmentCount,
			"kinds": notification.AttachmentKinds,
			"url":   notification.AttachmentsURL,
		}
	}
	changes := map[string]Change{}
	var actor *NotificationActor
	comment := ""
	if notificationAllows(notification, PrivacyFieldWorkflow) {
		changes = notification.Changes
		actor = notification.Actor
		comment = notification.Comment
	}
	return map[string]any{
		"schema_version": "1.0",
		"id":             notification.OutboxEventID, "event_id": notification.OutboxEventID,
		"event":         notification.Event,
		"occurred_at":   notification.OccurredAt.UTC(),
		"resource_type": notification.ResourceType,
		"resource": map[string]string{
			"id": notification.ResourceID, "public_id": notification.PublicID,
			"type": notification.ResourceType, "url": notification.ResourceURL,
		},
		"changes": changes,
		"actor":   actor,
		"comment": comment,
		"data":    data,
		"privacy": map[string]any{
			"profile_version":          notification.PrivacyProfileVersion,
			"data_categories":          notificationDataCategories(notification),
			"destination_capabilities": notification.DestinationCapabilities,
		},
	}
}

func notificationCopy(notification Notification) (string, string) {
	if notification.ResourceType == "feedback" {
		headline := "New feedback " + notification.PublicID
		detail := notification.Title
		if notificationAllows(notification, PrivacyFieldDescription) {
			description := strings.TrimSpace(notification.Description)
			if description == "" {
				description = strings.TrimSpace(notification.Text)
			}
			if description != "" && description != detail {
				if detail == "" {
					detail = description
				} else {
					detail += "\n" + description
				}
			}
		}
		if detail == "" {
			detail = "Feedback received without a text comment."
		}
		return headline, detail
	}
	headline := "Issue " + notification.PublicID + " updated"
	switch notification.Event {
	case EventIssueCreated:
		headline = "New issue " + notification.PublicID
	case EventIssueResolved:
		headline = "Issue " + notification.PublicID + " resolved"
	}
	detail := notification.Title
	if notificationAllows(notification, PrivacyFieldDescription) &&
		notification.Description != "" && notification.Description != detail {
		if detail == "" {
			detail = notification.Description
		} else {
			detail += "\n" + notification.Description
		}
	}
	return headline, detail
}

type notificationFact struct {
	Name  string
	Value string
}

func notificationFacts(notification Notification) []notificationFact {
	facts := make([]notificationFact, 0, 10)
	add := func(name, value string) {
		if strings.TrimSpace(value) != "" && len(facts) < 10 {
			facts = append(facts, notificationFact{Name: name, Value: value})
		}
	}
	add("Project", notification.ProjectName)
	if notificationHasFeedbackContext(notification) {
		source := notificationSourceLabel(notification)
		if notificationAllows(notification, PrivacyFieldPage) &&
			notificationSource(notification) != "web" {
			context := make([]string, 0, 4)
			for _, value := range []string{
				notification.MobileApplicationID, notification.MobileScreen,
				notification.MobileAppVersion, notification.MobileOSVersion,
			} {
				if strings.TrimSpace(value) != "" {
					context = append(context, value)
				}
			}
			if len(context) > 0 {
				source += " | " + strings.Join(context, " | ")
			}
		}
		add("Source", source)
	}
	if notificationAllows(notification, PrivacyFieldPage) {
		add("Page", integrationPageLabel(notification.PageURL))
	}
	if notificationAllows(notification, PrivacyFieldRating) && notification.Rating != nil {
		add("Rating", fmt.Sprintf("%d/5", *notification.Rating))
	}
	if notificationAllows(notification, PrivacyFieldClassification) {
		add("Category", strings.ReplaceAll(notification.Category, "_", " "))
		add("Actionability", strings.ReplaceAll(notification.Actionability, "_", " "))
	}
	add("Priority", strings.ReplaceAll(notification.Priority, "_", " "))
	status := strings.ReplaceAll(notificationStatus(notification), "_", " ")
	if notification.OldStatus != "" && notification.NewStatus != "" {
		status = strings.ReplaceAll(notification.OldStatus, "_", " ") + " -> " +
			strings.ReplaceAll(notification.NewStatus, "_", " ")
	}
	add("Status", status)
	if notificationAllows(notification, PrivacyFieldWorkflow) {
		if notification.Actor != nil {
			add("Actor", notificationActorLabel(*notification.Actor))
		}
		if notification.Comment != "" {
			add("Comment", notification.Comment)
		}
	}
	if notificationAllows(notification, PrivacyFieldAttachments) && notification.AttachmentCount > 0 {
		value := fmt.Sprintf("%d", notification.AttachmentCount)
		if len(notification.AttachmentKinds) > 0 {
			value += " (" + strings.Join(notification.AttachmentKinds, ", ") + ")"
		}
		add("Attachments", value)
	}
	return facts
}

func notificationHasFeedbackContext(notification Notification) bool {
	return notification.ResourceType == "feedback" || notification.FeedbackID != ""
}

func notificationSource(notification Notification) string {
	switch notification.Source {
	case "ios", "android":
		return notification.Source
	default:
		return "web"
	}
}

func notificationSourceLabel(notification Notification) string {
	switch notificationSource(notification) {
	case "ios":
		return "iOS"
	case "android":
		return "Android"
	default:
		return "Web"
	}
}

type integrationAction struct {
	Label string
	URL   string
}

func integrationActions(notification Notification) []integrationAction {
	candidates := make([]integrationAction, 0, 5)
	if notification.ResourceType == "issue" {
		candidates = append(candidates, integrationAction{Label: "Open issue", URL: notification.ResourceURL})
		if notification.FeedbackURL != "" {
			candidates = append(candidates, integrationAction{Label: "Open feedback", URL: notification.FeedbackURL})
		}
	} else {
		candidates = append(candidates, integrationAction{Label: "Open feedback", URL: notification.ResourceURL})
		if notification.IssueURL != "" {
			candidates = append(candidates, integrationAction{Label: "Open issue", URL: notification.IssueURL})
		}
	}
	if notificationAllows(notification, PrivacyFieldPage) &&
		integrationSpecificPageURL(notification.PageURL) {
		candidates = append(candidates, integrationAction{
			Label: "View page", URL: integrationSafePageURL(notification.PageURL),
		})
	}
	if notificationAllows(notification, PrivacyFieldAttachments) {
		candidates = append(candidates,
			integrationAction{Label: "View attachments", URL: notification.AttachmentsURL},
		)
	}
	actions := make([]integrationAction, 0, len(candidates))
	for _, action := range candidates {
		if integrationActionURL(action.URL) {
			actions = append(actions, action)
		}
	}
	return actions
}

func integrationSpecificPageURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") &&
		parsed.Host != "" && parsed.Path != "" && parsed.Path != "/"
}

func integrationActionURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != ""
}

func integrationSafePageURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.User = nil
	return parsed.String()
}

func integrationPageLabel(value string) string {
	parsed, err := url.Parse(integrationSafePageURL(value))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return truncateIntegrationText(parsed.Host+parsed.EscapedPath(), 100)
}

func notificationAllows(notification Notification, field string) bool {
	for _, allowed := range notification.PrivacyFields {
		if allowed == field {
			return true
		}
	}
	return false
}

func notificationStatus(notification Notification) string {
	if notification.ResourceType == "feedback" {
		return notification.FeedbackStatus
	}
	return notification.IssueStatus
}

func notificationDataCategories(notification Notification) []string {
	categories := []string{"event_metadata", "resource_identity"}
	add := func(category string, present bool) {
		if present {
			categories = append(categories, category)
		}
	}
	add("sanitized_title", strings.TrimSpace(notification.Title) != "")
	add("project", notification.ProjectID != "" || strings.TrimSpace(notification.ProjectName) != "")
	add("workflow_status", strings.TrimSpace(notificationStatus(notification)) != "")
	add("priority", strings.TrimSpace(notification.Priority) != "")
	add("fidibeki_link", notification.ResourceURL != "")
	add("description", notificationAllows(notification, PrivacyFieldDescription) &&
		(strings.TrimSpace(notification.Description) != "" || strings.TrimSpace(notification.Text) != ""))
	add("source_page", notificationAllows(notification, PrivacyFieldPage) &&
		integrationSafePageURL(notification.PageURL) != "")
	add("rating", notificationAllows(notification, PrivacyFieldRating) && notification.Rating != nil)
	add("classification", notificationAllows(notification, PrivacyFieldClassification) &&
		(notification.Category != "" || notification.Actionability != ""))
	add("workflow_context", notificationAllows(notification, PrivacyFieldWorkflow) &&
		(len(notification.Changes) > 0 || notification.Actor != nil || notification.Comment != ""))
	add("attachment_metadata", notificationAllows(notification, PrivacyFieldAttachments) &&
		notification.AttachmentCount > 0)
	return categories
}

func notificationActorLabel(actor NotificationActor) string {
	if actor.Name != "" {
		return actor.Name
	}
	if actor.Type == "reporter" {
		return "Reporter"
	}
	return actor.Type
}

func slackEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	return strings.ReplaceAll(value, ">", "&gt;")
}

func htmlEscape(value string) string {
	return slackEscape(value)
}

func truncateIntegrationText(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum-3]) + "..."
}
