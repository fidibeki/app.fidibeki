package email

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
)

const (
	TaskLifecycleEmail       = "email:lifecycle"
	EventOwnerActivated      = "tenant.owner_activated"
	EventDeletionCancelled   = "tenant.deletion_cancelled"
	EventBillingStateChanged = "billing.state_changed"
)

type LifecycleResolver struct{}

func (LifecycleResolver) Resolve(_ context.Context, event outbox.Event) ([]outbox.Destination, error) {
	switch event.Event {
	case EventOwnerActivated, EventDeletionCancelled, EventBillingStateChanged:
		if event.ResourceType != "tenant" {
			return nil, nil
		}
		return []outbox.Destination{{
			Kind: "lifecycle-email", ID: event.ID, TaskType: TaskLifecycleEmail, Queue: QueueName,
		}}, nil
	default:
		return nil, nil
	}
}

type LifecycleHandler struct {
	Database       db.Querier
	Sender         Sender
	PublicBaseURL  string
	BaseDomain     string
	AuthFrom       string
	WelcomeFrom    string
	WelcomeReplyTo string
	Now            func() time.Time
}

type lifecycleTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
}

type lifecycleEvent struct {
	ID           string
	TenantID     string
	TenantName   string
	TenantSlug   string
	ActorName    string
	BillingEmail string
	BillingOK    bool
	WelcomeSent  bool
	Event        string
	Data         lifecycleData
}

type lifecycleData struct {
	MemberID          string `json:"member_id"`
	PreviousStatus    string `json:"previous_status"`
	Status            string `json:"status"`
	PreviousPlan      string `json:"previous_plan"`
	Plan              string `json:"plan"`
	PreviousPending   string `json:"previous_pending_plan"`
	PendingPlan       string `json:"pending_plan"`
	PreviousSubID     string `json:"previous_subscription_id"`
	SubscriptionID    string `json:"subscription_id"`
	DeletionRequested string `json:"deletion_requested_at"`
}

type lifecycleRecipient struct {
	Email string
	Name  string
}

func (h LifecycleHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload lifecycleTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.OutboxEventID == "" || payload.TenantID == "" || payload.ResourceID == "" {
			return fmt.Errorf("decode lifecycle email task: %w", asynq.SkipRetry)
		}
		event, err := h.loadEvent(ctx, payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if event.Event == EventOwnerActivated && event.WelcomeSent {
			return nil
		}
		recipients, err := h.recipients(ctx, event)
		if err != nil {
			return err
		}
		for _, recipient := range recipients {
			message, err := h.message(event, recipient)
			if err != nil {
				return err
			}
			if _, err := h.Sender.Send(ctx, message); err != nil {
				return fmt.Errorf("send lifecycle email: %w", err)
			}
		}
		if event.Event == EventOwnerActivated {
			if _, err := h.Database.Exec(ctx, `
				UPDATE tenants SET welcome_email_sent_at = COALESCE(welcome_email_sent_at, $2)
				WHERE id = $1
			`, event.TenantID, h.now()); err != nil {
				return fmt.Errorf("mark founder welcome sent: %w", err)
			}
		}
		return nil
	}
}

func (h LifecycleHandler) loadEvent(ctx context.Context, task lifecycleTask) (lifecycleEvent, error) {
	var event lifecycleEvent
	var data []byte
	var billingEmail *string
	var billingBounced bool
	err := h.Database.QueryRow(ctx, `
		SELECT event.id, event.tenant_id, tenant.name, tenant.slug,
		       COALESCE(actor.name, actor.email::text, ''),
		       tenant.billing_email::text, tenant.email_bounced,
		       tenant.welcome_email_sent_at IS NOT NULL,
		       event.event::text, event.data
		FROM outbox_events AS event
		JOIN tenants AS tenant ON tenant.id = event.tenant_id
		LEFT JOIN tenant_members AS actor
		  ON actor.tenant_id = event.tenant_id
		 AND actor.id::text = event.data ->> 'actor_member_id'
		WHERE event.id = $1 AND event.tenant_id = $2
		  AND event.resource_type = 'tenant' AND event.resource_id = $3
	`, task.OutboxEventID, task.TenantID, task.ResourceID).Scan(
		&event.ID, &event.TenantID, &event.TenantName, &event.TenantSlug, &event.ActorName,
		&billingEmail, &billingBounced, &event.WelcomeSent,
		&event.Event, &data,
	)
	if err != nil {
		return lifecycleEvent{}, err
	}
	if billingEmail != nil {
		event.BillingEmail = *billingEmail
		event.BillingOK = !billingBounced
	}
	if err := json.Unmarshal(data, &event.Data); err != nil {
		return lifecycleEvent{}, fmt.Errorf("decode lifecycle email data: %w", err)
	}
	return event, nil
}

func (h LifecycleHandler) recipients(ctx context.Context, event lifecycleEvent) ([]lifecycleRecipient, error) {
	if event.Event == EventOwnerActivated {
		var recipient lifecycleRecipient
		var name *string
		err := h.Database.QueryRow(ctx, `
			SELECT email::text, name
			FROM tenant_members
			WHERE tenant_id = $1 AND id = $2
			  AND email_bounced = false AND email_complained = false
		`, event.TenantID, event.Data.MemberID).Scan(&recipient.Email, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("load founder welcome recipient: %w", err)
		}
		if name != nil {
			recipient.Name = strings.TrimSpace(*name)
		}
		return []lifecycleRecipient{recipient}, nil
	}

	rows, err := h.Database.Query(ctx, `
		SELECT email::text, COALESCE(name, '')
		FROM tenant_members
		WHERE tenant_id = $1 AND 'billing:manage' = ANY(permissions)
		  AND email_bounced = false AND email_complained = false
		ORDER BY created_at, id
	`, event.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list lifecycle email recipients: %w", err)
	}
	defer rows.Close()
	recipients := make([]lifecycleRecipient, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var recipient lifecycleRecipient
		if err := rows.Scan(&recipient.Email, &recipient.Name); err != nil {
			return nil, fmt.Errorf("scan lifecycle email recipient: %w", err)
		}
		normalized := strings.ToLower(recipient.Email)
		seen[normalized] = struct{}{}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lifecycle email recipients: %w", err)
	}
	if event.BillingOK && event.BillingEmail != "" {
		if _, exists := seen[strings.ToLower(event.BillingEmail)]; !exists {
			recipients = append(recipients, lifecycleRecipient{Email: event.BillingEmail})
		}
	}
	return recipients, nil
}

func (h LifecycleHandler) message(event lifecycleEvent, recipient lifecycleRecipient) (Message, error) {
	logoURL, err := BrandLogoURL(h.PublicBaseURL, h.BaseDomain)
	if err != nil {
		return Message{}, err
	}
	workspaceURL, err := lifecycleWorkspaceURL(
		h.PublicBaseURL, h.BaseDomain, event.TenantSlug, "/settings/billing",
	)
	if err != nil {
		return Message{}, err
	}
	recipientDigest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(recipient.Email))))
	message := Message{
		From: h.AuthFrom, To: recipient.Email,
		IdempotencyKey: fmt.Sprintf("lifecycle:%s:%x", event.ID, recipientDigest),
	}
	content := Content{
		Workspace: event.TenantName, LogoURL: logoURL,
		Footer:      "You received this mandatory account notice because you administer or manage this workspace.",
		FooterLinks: []Link{{Label: "Contact support", URL: "mailto:support@fidibeki.com"}},
		Action:      &Link{Label: "Open billing settings", URL: workspaceURL},
	}

	switch event.Event {
	case EventOwnerActivated:
		openURL, err := lifecycleWorkspaceURL(
			h.PublicBaseURL, h.BaseDomain, event.TenantSlug, "/getting-started",
		)
		if err != nil {
			return Message{}, err
		}
		greeting := "Hi,"
		if recipient.Name != "" {
			greeting = "Hi " + recipient.Name + ","
		}
		message.From = h.WelcomeFrom
		message.ReplyTo = h.WelcomeReplyTo
		message.Subject = "Welcome to Fidibeki"
		content.Preheader = "A personal welcome from Acidiney"
		content.Heading = "Welcome to Fidibeki"
		content.Body = []string{
			greeting,
			"I'm Acidiney, the person behind Fidibeki. Thanks for giving the product a try.",
			"During your trial you can capture contextual feedback, triage it into actionable work, review supporting evidence, and follow each issue through resolution.",
			"I hope the trial is useful. Reply directly to this email whenever you have a question, doubt, or need help.",
		}
		content.Action = &Link{Label: "Open your workspace", URL: openURL}
		content.Footer = "You received this one-time welcome because you created this Fidibeki workspace."
	case EventDeletionCancelled:
		message.Subject = "Fidibeki account deletion cancelled"
		content.Preheader = event.TenantName + " will remain active"
		content.Heading = "Account deletion cancelled"
		content.Body = []string{"The pending deletion was cancelled. The workspace and its data will remain available."}
		content.Action.Label = "Open workspace billing"
		if event.Data.DeletionRequested != "" {
			content.Metadata = append(content.Metadata, Metadata{
				Label: "Original request", Value: event.Data.DeletionRequested,
			})
		}
		if event.ActorName != "" {
			content.Metadata = append(content.Metadata, Metadata{
				Label: "Cancelled by", Value: event.ActorName,
			})
		}
	case EventBillingStateChanged:
		message.Subject, content = billingLifecycleContent(event, content)
	default:
		return Message{}, fmt.Errorf("unsupported lifecycle email event %q", event.Event)
	}
	return Compose(message, content)
}

func billingLifecycleContent(event lifecycleEvent, content Content) (string, Content) {
	content.Metadata = []Metadata{
		{Label: "Status", Value: lifecycleTransition(event.Data.PreviousStatus, event.Data.Status)},
		{Label: "Plan", Value: lifecycleTransition(event.Data.PreviousPlan, event.Data.Plan)},
	}
	if event.Data.PreviousPending != "" || event.Data.PendingPlan != "" {
		content.Metadata = append(content.Metadata, Metadata{
			Label: "Scheduled plan", Value: lifecycleTransition(event.Data.PreviousPending, event.Data.PendingPlan),
		})
	}
	switch {
	case event.Data.Status == "suspended":
		content.Preheader = "Payment failed for " + event.TenantName
		content.Heading = "Payment failed"
		content.Body = []string{
			"We could not complete the latest subscription payment, so workspace access is suspended.",
			"Review the payment method in billing settings to restore access.",
		}
		return "Action required: Fidibeki payment failed", content
	case event.Data.PreviousStatus == "suspended" && event.Data.Status != "suspended":
		content.Preheader = "Access restored for " + event.TenantName
		content.Heading = "Payment recovered"
		content.Body = []string{"The payment was recovered and workspace access has been restored."}
		return "Fidibeki payment recovered", content
	case event.Data.Status == "cancelled":
		content.Preheader = "Subscription cancelled for " + event.TenantName
		content.Heading = "Subscription cancelled"
		content.Body = []string{"The subscription cancellation is now reflected in Fidibeki."}
		return "Fidibeki subscription cancelled", content
	case event.Data.PreviousSubID == "" && event.Data.SubscriptionID != "":
		content.Preheader = "Subscription active for " + event.TenantName
		content.Heading = "Subscription activated"
		content.Body = []string{"Your Fidibeki subscription is active with the persisted plan shown below."}
		return "Fidibeki subscription activated", content
	default:
		content.Preheader = "Subscription updated for " + event.TenantName
		content.Heading = "Subscription updated"
		content.Body = []string{"A material subscription change is now reflected in your workspace."}
		return "Fidibeki subscription updated", content
	}
}

func lifecycleTransition(previous, current string) string {
	if previous == "" || previous == current {
		return current
	}
	return previous + " -> " + current
}

func lifecycleWorkspaceURL(publicBaseURL, baseDomain, slug, path string) (string, error) {
	base, err := url.Parse(publicBaseURL)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", errors.New("public base URL is invalid")
	}
	host := slug + "." + baseDomain
	if port := base.Port(); port != "" {
		host += ":" + port
	}
	base.Host = host
	base.Path = path
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func (h LifecycleHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}
