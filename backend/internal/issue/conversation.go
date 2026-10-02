package issue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/activity"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
)

// CreateFromConversation creates an operator-authored issue in the caller's
// transaction. It does not copy chat contents or infer reporter email consent.
// The caller must validate access, input, and the source message before calling.
func (store Store) CreateFromConversation(ctx context.Context, query db.Querier, tenantID, projectID, tenantSlug, actorID, title, description string, now time.Time) (Issue, error) {
	sequence, err := allocateIssueSequence(ctx, query, tenantID, now.Year())
	if err != nil {
		return Issue{}, err
	}
	id := uuid.NewString()
	publicID := fmt.Sprintf("%s-%d-%04d", strings.ToUpper(tenantSlug), now.Year(), sequence)
	if _, err = query.Exec(ctx, `INSERT INTO issues (id, tenant_id, project_id, public_id, title, description)
        VALUES ($1,$2,$3,$4,$5,$6)`, id, tenantID, projectID, publicID, title, description); err != nil {
		return Issue{}, fmt.Errorf("create issue from conversation: %w", err)
	}
	eventID, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: id, ActorMemberID: &actorID,
		Kind: "issue.created", Data: map[string]any{"source": "conversation", "title": title, "description": description}, CreatedAt: now,
	})
	if err != nil {
		return Issue{}, err
	}
	if _, err = query.Exec(ctx, `INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
        VALUES ($1,'issue.created','issue',$2,$3)`, tenantID, id, eventID); err != nil {
		return Issue{}, fmt.Errorf("create conversation issue outbox event: %w", err)
	}
	return store.Get(ctx, query, tenantID, id)
}
