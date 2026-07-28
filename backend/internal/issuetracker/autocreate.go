package issuetracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/activity"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/acidiney/app.fidibeki/backend/internal/privacy"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/hibiken/asynq"
)

// TaskAutoCreate creates the external copy of a Fidibeki issue without a member confirming it,
// for workspaces that switched automatic creation on.
const TaskAutoCreate = "issue-tracker:auto-create"

// autoCreateDestinationKind labels the outbox destination so the deterministic task ID stays
// distinct from the notification destinations resolved for the same event.
const autoCreateDestinationKind = "issue_tracker"

// OutboxResolver fans an issue.created event out to every provider a workspace has switched
// automatic creation on for. It resolves nothing for any other event, so an issue update never
// produces a second external issue.
type OutboxResolver struct {
	Database db.Querier
	Now      func() time.Time
}

func (r OutboxResolver) Resolve(
	ctx context.Context,
	event outbox.Event,
) ([]outbox.Destination, error) {
	if event.ResourceType != "issue" || event.Event != "issue.created" {
		return nil, nil
	}
	if r.Database == nil {
		return nil, errors.New("issue tracker outbox resolver database is required")
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	rows, err := r.Database.Query(ctx, `
		SELECT connection.provider
		FROM issue_tracker_connections AS connection
		JOIN tenants AS tenant ON tenant.id = connection.tenant_id
		WHERE connection.tenant_id = $1
		  AND `+tenant.OutboundDeliveryEligibleSQL(2)+`
		  AND connection.auto_create
		  AND connection.is_enabled
		  AND connection.destination_id <> ''
		  AND (connection.provider <> 'jira' OR connection.issue_type_id <> '')
		ORDER BY connection.provider
	`, event.TenantID, now)
	if err != nil {
		return nil, fmt.Errorf("resolve automatic issue tracker destinations: %w", err)
	}
	defer rows.Close()
	destinations := make([]outbox.Destination, 0, len(Providers()))
	for rows.Next() {
		var provider string
		if err := rows.Scan(&provider); err != nil {
			return nil, fmt.Errorf("scan automatic issue tracker destination: %w", err)
		}
		destinations = append(destinations, outbox.Destination{
			Kind: autoCreateDestinationKind, ID: provider,
			TaskType: TaskAutoCreate, Queue: outbox.QueueName,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate automatic issue tracker destinations: %w", err)
	}
	return destinations, nil
}

// AutoCreateTask is the payload the outbox enqueuer writes for each destination.
type AutoCreateTask struct {
	OutboxEventID string `json:"outbox_event_id"`
	TenantID      string `json:"tenant_id"`
	ResourceID    string `json:"resource_id"`
	Provider      string `json:"destination_id"`
}

// AutoCreateHandler performs the provider call outside any request, so a slow or failing
// provider can never delay or roll back the Fidibeki issue that triggered it.
type AutoCreateHandler struct {
	Store   Store
	Clients ClientFactory
	Now     func() time.Time
}

func (h AutoCreateHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h AutoCreateHandler) Handler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload AutoCreateTask
		if err := json.Unmarshal(task.Payload(), &payload); err != nil ||
			payload.TenantID == "" || payload.ResourceID == "" || payload.Provider == "" {
			return fmt.Errorf("decode automatic issue tracker task: %w", asynq.SkipRetry)
		}
		provider := Provider(payload.Provider)
		if !provider.Valid() {
			return fmt.Errorf("unsupported issue tracker provider: %w", asynq.SkipRetry)
		}
		return h.create(ctx, payload.TenantID, payload.ResourceID, provider)
	}
}

func (h AutoCreateHandler) create(
	ctx context.Context,
	tenantID string,
	issueID string,
	provider Provider,
) error {
	query := h.Store.Database
	if query == nil {
		return errors.New("automatic issue tracker handler requires a database")
	}
	eligible, err := h.Store.OutboundDeliveryEligible(ctx, query, tenantID, h.now())
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !eligible {
		return accessaudit.Record(ctx, query, accessaudit.RecordInput{
			TenantID: tenantID, Kind: AuditKindAutoCreateSuppressed,
			Actor:        accessaudit.Actor{Type: accessaudit.ActorSystem},
			ResourceType: AuditResourceType, ResourceID: issueID,
			Action: AuditActionAutomate, Outcome: accessaudit.OutcomeDenied,
			Data: map[string]any{
				"provider": string(provider),
				"reason":   autoCreateSuppressionReasonTenantLifecycle,
			},
			Now: h.now(),
		})
	}
	connection, err := h.Store.GetConnection(ctx, query, tenantID, provider)
	if errors.Is(err, ErrNotConnected) {
		// The workspace disconnected the provider between the event and this task.
		return nil
	}
	if err != nil {
		return err
	}
	if !connection.AutoCreate || !connection.Enabled || !connection.Configured() {
		return nil
	}
	issue, err := h.Store.LoadIssue(ctx, query, tenantID, issueID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// The same restriction that suppresses integration delivery also suppresses copying an
	// issue into an external tracker.
	if err := privacy.EnsureResourceProcessingAllowed(
		ctx, query, tenantID, "issue", issueID, privacy.PurposeIntegration,
	); errors.Is(err, privacy.ErrProcessingRestricted) {
		return nil
	} else if err != nil {
		return err
	}
	link, err := h.beginAttempt(ctx, query, tenantID, issueID, provider, connection, issue)
	if err != nil || link.ID == "" {
		return err
	}
	credentials, err := h.Store.Credentials(ctx, query, tenantID, provider)
	if err != nil {
		return err
	}
	client, err := h.Clients(provider)
	if err != nil {
		return err
	}
	result, providerErr := client.CreateIssue(ctx, credentials, CreateRequest{
		Title: link.RequestTitle, Description: link.RequestDescription,
		Priority: link.RequestPriority, DestinationID: connection.DestinationID,
		IssueTypeID: connection.IssueTypeID, SubDestination: connection.SubDestinationID,
		IdempotencyKey: link.ID,
	})
	if providerErr != nil {
		return h.recordFailure(ctx, query, tenantID, issueID, link, provider, providerErr)
	}
	completed, err := h.Store.CompleteLink(ctx, query, tenantID, link.ID, result)
	if err != nil {
		return err
	}
	_, err = activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: issueID,
		Kind: ActivityExternalIssueCreated, Data: map[string]any{
			"provider": string(provider), "external_key": completed.ExternalKey,
			"external_url": completed.ExternalURL, "destination": completed.DestinationName,
			"automatic": true,
		}, CreatedAt: h.now(),
	})
	return err
}

// beginAttempt claims the single live attempt slot for this issue and provider. A retried task,
// a duplicate event, or a member who created the issue manually in the meantime all resolve to
// an empty link, which means "nothing left to do".
func (h AutoCreateHandler) beginAttempt(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	provider Provider,
	connection Connection,
	issue IssueSnapshot,
) (ExternalLink, error) {
	live, err := h.Store.FindLiveLink(ctx, query, tenantID, issueID, provider)
	if err == nil {
		if live.Status == LinkStatusCreated {
			return ExternalLink{}, nil
		}
		return live, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return ExternalLink{}, err
	}
	values := PendingLinkValues{
		IssueID: issueID, Provider: provider,
		IdempotencyKey: AutoCreateIdempotencyKey(issueID, provider),
		Title:          issue.Title, Description: issue.Description, Priority: issue.Priority,
		DestinationID: connection.DestinationID, DestinationName: connection.DestinationName,
		IssueTypeID: connection.IssueTypeID,
	}
	prior, err := h.Store.FindLinkByIdempotencyKey(ctx, query, tenantID, values.IdempotencyKey)
	if err == nil {
		return h.Store.RetryLink(ctx, query, tenantID, issueID, prior.ID, values)
	}
	if !errors.Is(err, ErrNotFound) {
		return ExternalLink{}, err
	}
	link, err := h.Store.BeginLink(ctx, query, tenantID, values)
	if errors.Is(err, ErrLinkExists) || errors.Is(err, ErrLinkInFlight) {
		return ExternalLink{}, nil
	}
	return link, err
}

// recordFailure stores an actionable failure and only asks Asynq to retry when the provider
// said the problem is transient. A rejected credential is left for a member to fix and retry
// from the issue instead of being retried for hours.
func (h AutoCreateHandler) recordFailure(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	link ExternalLink,
	provider Provider,
	providerErr error,
) error {
	message := providerErr.Error()
	code := ErrorCodeRejected
	retryable := false
	var failure *ProviderError
	if errors.As(providerErr, &failure) {
		message = failure.Message
		code = failure.Code
		retryable = failure.Retryable
	}
	if _, err := h.Store.FailLink(ctx, query, tenantID, link.ID, message); err != nil {
		return err
	}
	if err := h.Store.RecordConnectionError(ctx, query, tenantID, provider, message); err != nil {
		return err
	}
	if _, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: issueID,
		Kind: ActivityExternalIssueFailed, Data: map[string]any{
			"provider": string(provider), "error_code": code,
			"attempt": link.AttemptCount, "automatic": true,
		}, CreatedAt: h.now(),
	}); err != nil {
		return err
	}
	if retryable {
		return providerErr
	}
	return nil
}

// AutoCreateIdempotencyKey is stable per issue and provider, so a redelivered event or a task
// retry converges on the one attempt instead of opening new ones.
func AutoCreateIdempotencyKey(issueID string, provider Provider) string {
	return "auto:" + issueID + ":" + string(provider)
}
