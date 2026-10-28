package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/config"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/outbox"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

const TaskDeliver = "integration:deliver"

type OutboxResolver struct {
	Database db.Querier
	Now      func() time.Time
}

func (r OutboxResolver) Resolve(ctx context.Context, event outbox.Event) ([]outbox.Destination, error) {
	if !supportedEvent(event.Event, event.ResourceType) {
		return nil, nil
	}
	rows, err := r.Database.Query(ctx, `
		SELECT integration.id, tenant.plan
		FROM integrations AS integration
		JOIN tenants AS tenant ON tenant.id = integration.tenant_id
		WHERE integration.tenant_id = $1
		  AND integration.is_active = true
		  AND `+tenant.OutboundDeliveryEligibleSQL(3)+`
		  AND $2::event_type = ANY(integration.events)
		ORDER BY integration.id
	`, event.TenantID, event.Event, integrationNow(r.Now))
	if err != nil {
		return nil, fmt.Errorf("resolve integration destinations: %w", err)
	}
	defer rows.Close()
	destinations := make([]outbox.Destination, 0)
	for rows.Next() {
		var integrationID string
		var plan tenant.Plan
		if err := rows.Scan(&integrationID, &plan); err != nil {
			return nil, fmt.Errorf("scan integration destination: %w", err)
		}
		if config.CanUseFeature(tenant.Tenant{Plan: plan}, config.FeatureSlack) {
			destinations = append(destinations, outbox.Destination{
				Kind: "integration", ID: integrationID, TaskType: TaskDeliver,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate integration destinations: %w", err)
	}
	return destinations, nil
}

func integrationNow(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}

func supportedEvent(event, resourceType string) bool {
	switch event {
	case EventFeedbackCreated:
		return resourceType == "feedback"
	case EventIssueCreated, EventIssueUpdated, EventIssueResolved:
		return resourceType == "issue"
	default:
		return false
	}
}
