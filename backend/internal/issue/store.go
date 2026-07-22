package issue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/triage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Store struct {
	Database db.Querier
}

type feedbackIssueState struct {
	ID             string
	ProjectID      string
	IssueID        *string
	ReporterEmail  *string
	NotifyReporter bool
	Priority       *triage.Priority
	AIPriority     *triage.Priority
	AssigneeID     *string
}

func (Store) List(ctx context.Context, query db.Querier, tenantID string) ([]Issue, error) {
	rows, err := query.Query(ctx, issueSelect+`
		WHERE issue.tenant_id = $1
		ORDER BY issue.created_at DESC, issue.id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer rows.Close()
	issues := make([]Issue, 0)
	for rows.Next() {
		found, scanErr := scanIssue(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		issues = append(issues, found)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issues: %w", err)
	}
	return issues, nil
}

func (Store) Get(ctx context.Context, query db.Querier, tenantID, issueID string) (Issue, error) {
	found, err := scanIssue(query.QueryRow(ctx, issueSelect+`
		WHERE issue.tenant_id = $1 AND issue.id = $2
	`, tenantID, issueID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return found, err
}

func (Store) ListUpdates(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
) ([]Update, error) {
	rows, err := query.Query(ctx, `
		SELECT id, issue_id, member_id, old_status, new_status, comment, created_at
		FROM issue_updates
		WHERE tenant_id = $1 AND issue_id = $2
		ORDER BY created_at, id
	`, tenantID, issueID)
	if err != nil {
		return nil, fmt.Errorf("list issue updates: %w", err)
	}
	defer rows.Close()
	updates := make([]Update, 0)
	for rows.Next() {
		var update Update
		if err := rows.Scan(
			&update.ID, &update.IssueID, &update.MemberID, &update.OldStatus,
			&update.NewStatus, &update.Comment, &update.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan issue update: %w", err)
		}
		updates = append(updates, update)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue updates: %w", err)
	}
	return updates, nil
}

func (store Store) CreateFromFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	tenantSlug string,
	input CreateFromFeedbackInput,
	now time.Time,
) (Issue, error) {
	rows, err := query.Query(ctx, `
		SELECT id, project_id, issue_id, reporter_email, notify_reporter,
		       priority, ai_priority, assignee_id
		FROM feedbacks
		WHERE tenant_id = $1 AND id = ANY($2::uuid[])
		ORDER BY id
		FOR UPDATE
	`, tenantID, input.FeedbackIDs)
	if err != nil {
		return Issue{}, fmt.Errorf("lock feedback for issue creation: %w", err)
	}
	states := make([]feedbackIssueState, 0, len(input.FeedbackIDs))
	for rows.Next() {
		var state feedbackIssueState
		if err := rows.Scan(
			&state.ID, &state.ProjectID, &state.IssueID, &state.ReporterEmail,
			&state.NotifyReporter, &state.Priority, &state.AIPriority, &state.AssigneeID,
		); err != nil {
			rows.Close()
			return Issue{}, fmt.Errorf("scan feedback for issue creation: %w", err)
		}
		states = append(states, state)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Issue{}, fmt.Errorf("iterate feedback for issue creation: %w", err)
	}
	if len(states) != len(input.FeedbackIDs) {
		return Issue{}, ErrNotFound
	}
	projectID := states[0].ProjectID
	for _, state := range states {
		if state.ProjectID != projectID {
			return Issue{}, ErrProjectMismatch
		}
		if state.IssueID != nil {
			return Issue{}, ErrFeedbackLinked
		}
	}
	priority := input.Priority
	if !input.SetPriority {
		priority = highestFeedbackPriority(states, false)
		if priority == nil {
			priority = highestFeedbackPriority(states, true)
		}
	}
	assigneeID := input.AssigneeID
	if !input.SetAssignee {
		assigneeID = consensusFeedbackAssignee(states)
	}
	if assigneeID != nil {
		var exists bool
		if err := query.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM tenant_members WHERE tenant_id = $1 AND id = $2)
		`, tenantID, *assigneeID).Scan(&exists); err != nil {
			return Issue{}, fmt.Errorf("validate issue assignee: %w", err)
		}
		if !exists {
			return Issue{}, ErrNotFound
		}
	}
	tagIDs := input.TagIDs
	if !input.SetTags {
		tagIDs, err = feedbackTagUnion(ctx, query, tenantID, input.FeedbackIDs)
		if err != nil {
			return Issue{}, err
		}
	}
	if len(tagIDs) > 0 {
		var count int
		if err := query.QueryRow(ctx, `
			SELECT count(*) FROM tags WHERE tenant_id = $1 AND id = ANY($2::uuid[])
		`, tenantID, tagIDs).Scan(&count); err != nil {
			return Issue{}, fmt.Errorf("validate issue tags: %w", err)
		}
		if count != len(tagIDs) {
			return Issue{}, ErrNotFound
		}
	}
	reporterEmail, notifyReporter := consensusFeedbackReporter(states)
	sequence, err := allocateIssueSequence(ctx, query, tenantID, now.Year())
	if err != nil {
		return Issue{}, err
	}
	createdID := uuid.NewString()
	publicID := fmt.Sprintf("%s-%d-%04d", strings.ToUpper(tenantSlug), now.Year(), sequence)
	if _, err := query.Exec(ctx, `
		INSERT INTO issues (
			id, tenant_id, project_id, public_id, title, description,
			priority, assignee_id, reporter_email, notify_reporter
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, createdID, tenantID, projectID, publicID, input.Title, input.Description,
		priority, assigneeID, reporterEmail, notifyReporter); err != nil {
		return Issue{}, fmt.Errorf("create issue from feedback: %w", err)
	}
	if len(tagIDs) > 0 {
		if _, err := query.Exec(ctx, `
			INSERT INTO issue_tags (tenant_id, issue_id, tag_id)
			SELECT $1, $2, tag_id FROM unnest($3::uuid[]) AS tag_id
		`, tenantID, createdID, tagIDs); err != nil {
			return Issue{}, fmt.Errorf("copy feedback tags to issue: %w", err)
		}
	}
	result, err := query.Exec(ctx, `
		UPDATE feedbacks SET issue_id = $3
		WHERE tenant_id = $1 AND id = ANY($2::uuid[]) AND issue_id IS NULL
	`, tenantID, input.FeedbackIDs, createdID)
	if err != nil {
		return Issue{}, fmt.Errorf("link feedback to created issue: %w", err)
	}
	if int(result.RowsAffected()) != len(input.FeedbackIDs) {
		return Issue{}, ErrFeedbackLinked
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id)
		VALUES ($1, 'issue.created', 'issue', $2)
	`, tenantID, createdID); err != nil {
		return Issue{}, fmt.Errorf("create issue outbox event: %w", err)
	}
	return store.Get(ctx, query, tenantID, createdID)
}

func highestFeedbackPriority(states []feedbackIssueState, useAI bool) *triage.Priority {
	var highest *triage.Priority
	for _, state := range states {
		value := state.Priority
		if useAI {
			value = state.AIPriority
		}
		if value != nil && (highest == nil || value.Rank() > highest.Rank()) {
			copy := *value
			highest = &copy
		}
	}
	return highest
}

func consensusFeedbackAssignee(states []feedbackIssueState) *string {
	if len(states) == 0 || states[0].AssigneeID == nil {
		return nil
	}
	candidate := *states[0].AssigneeID
	for _, state := range states[1:] {
		if state.AssigneeID == nil || *state.AssigneeID != candidate {
			return nil
		}
	}
	return &candidate
}

func consensusFeedbackReporter(states []feedbackIssueState) (*string, bool) {
	if len(states) == 0 || !states[0].NotifyReporter || states[0].ReporterEmail == nil {
		return nil, false
	}
	candidate := *states[0].ReporterEmail
	for _, state := range states[1:] {
		if !state.NotifyReporter || state.ReporterEmail == nil ||
			!strings.EqualFold(*state.ReporterEmail, candidate) {
			return nil, false
		}
	}
	return &candidate, true
}

func feedbackTagUnion(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackIDs []string,
) ([]string, error) {
	rows, err := query.Query(ctx, `
		SELECT DISTINCT tag_id
		FROM feedback_tags
		WHERE tenant_id = $1 AND feedback_id = ANY($2::uuid[])
		ORDER BY tag_id
	`, tenantID, feedbackIDs)
	if err != nil {
		return nil, fmt.Errorf("list feedback tag union: %w", err)
	}
	defer rows.Close()
	tagIDs := make([]string, 0)
	for rows.Next() {
		var tagID string
		if err := rows.Scan(&tagID); err != nil {
			return nil, fmt.Errorf("scan feedback tag union: %w", err)
		}
		tagIDs = append(tagIDs, tagID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feedback tag union: %w", err)
	}
	return tagIDs, nil
}

func allocateIssueSequence(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	year int,
) (int, error) {
	var sequence int
	if err := query.QueryRow(ctx, `
		INSERT INTO issue_counters (tenant_id, year, next_seq)
		VALUES ($1, $2, 2)
		ON CONFLICT (tenant_id, year) DO UPDATE
		SET next_seq = issue_counters.next_seq + 1
		RETURNING next_seq - 1
	`, tenantID, year).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("allocate issue sequence: %w", err)
	}
	return sequence, nil
}

func (store Store) UpdateStatus(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	memberID string,
	status Status,
	comment *string,
	now time.Time,
) (Issue, Update, error) {
	var oldStatus Status
	err := query.QueryRow(ctx, `
		SELECT status FROM issues
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, issueID).Scan(&oldStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, Update{}, ErrNotFound
	}
	if err != nil {
		return Issue{}, Update{}, fmt.Errorf("lock issue for status update: %w", err)
	}
	if oldStatus == status {
		return Issue{}, Update{}, ErrNoStatusChange
	}
	if _, err := query.Exec(ctx, `
		UPDATE issues
		SET status = $3::issue_status,
		    resolved_at = CASE
		        WHEN $3::issue_status = 'resolved' THEN $4
		        WHEN $3::issue_status IN ('open', 'in_progress') THEN NULL
		        ELSE resolved_at
		    END
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, issueID, status, now.UTC()); err != nil {
		return Issue{}, Update{}, fmt.Errorf("update issue status: %w", err)
	}
	updateID := uuid.NewString()
	if _, err := query.Exec(ctx, `
		INSERT INTO issue_updates (
			id, tenant_id, issue_id, member_id, old_status, new_status, comment, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, updateID, tenantID, issueID, memberID, oldStatus, status, comment, now.UTC()); err != nil {
		return Issue{}, Update{}, fmt.Errorf("record issue status update: %w", err)
	}
	event := "issue.updated"
	if status == StatusResolved {
		event = "issue.resolved"
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
		VALUES ($1, $2, 'issue', $3, $4)
	`, tenantID, event, issueID, updateID); err != nil {
		return Issue{}, Update{}, fmt.Errorf("create issue update outbox event: %w", err)
	}
	found, err := store.Get(ctx, query, tenantID, issueID)
	if err != nil {
		return Issue{}, Update{}, err
	}
	return found, Update{
		ID: updateID, IssueID: issueID, MemberID: &memberID,
		OldStatus: &oldStatus, NewStatus: status, Comment: comment, CreatedAt: now.UTC(),
	}, nil
}

func (store Store) Merge(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	sourceID string,
	targetID string,
	now time.Time,
) (Issue, error) {
	if sourceID == targetID {
		return Issue{}, ErrInvalidMerge
	}
	rows, err := query.Query(ctx, `
		SELECT id, project_id, merged_into_id
		FROM issues
		WHERE tenant_id = $1 AND id IN ($2, $3)
		ORDER BY id
		FOR UPDATE
	`, tenantID, sourceID, targetID)
	if err != nil {
		return Issue{}, fmt.Errorf("lock issues for merge: %w", err)
	}
	type mergeState struct {
		projectID    string
		mergedIntoID *string
	}
	states := make(map[string]mergeState, 2)
	for rows.Next() {
		var id string
		var state mergeState
		if err := rows.Scan(&id, &state.projectID, &state.mergedIntoID); err != nil {
			rows.Close()
			return Issue{}, fmt.Errorf("scan issue merge state: %w", err)
		}
		states[id] = state
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Issue{}, fmt.Errorf("iterate issue merge state: %w", err)
	}
	source, sourceOK := states[sourceID]
	target, targetOK := states[targetID]
	if !sourceOK || !targetOK {
		return Issue{}, ErrNotFound
	}
	if source.projectID != target.projectID || target.mergedIntoID != nil {
		return Issue{}, ErrInvalidMerge
	}
	if source.mergedIntoID != nil && *source.mergedIntoID == targetID {
		return store.Get(ctx, query, tenantID, sourceID)
	}
	if source.mergedIntoID != nil {
		return Issue{}, ErrInvalidMerge
	}
	if _, err := query.Exec(ctx, `
		UPDATE issues SET merged_into_id = $3, merged_at = $4
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, sourceID, targetID, now.UTC()); err != nil {
		return Issue{}, fmt.Errorf("merge issue: %w", err)
	}
	return store.Get(ctx, query, tenantID, sourceID)
}

func (store Store) Unmerge(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
) (Issue, error) {
	tag, err := query.Exec(ctx, `
		UPDATE issues SET merged_into_id = NULL, merged_at = NULL
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, issueID)
	if err != nil {
		return Issue{}, fmt.Errorf("unmerge issue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Issue{}, ErrNotFound
	}
	return store.Get(ctx, query, tenantID, issueID)
}

const issueSelect = `
	SELECT issue.id, issue.tenant_id, issue.project_id, issue.public_id, issue.title, issue.description,
	       issue.status, issue.priority, assignee.id, assignee.name, assignee.email::text,
	       COALESCE((
	           SELECT jsonb_agg(
	               jsonb_build_object('id', tag.id, 'name', tag.name::text, 'color', tag.color)
	               ORDER BY lower(tag.name::text), tag.id
	           )
	           FROM issue_tags AS issue_tag
	           JOIN tags AS tag
	             ON tag.tenant_id = issue_tag.tenant_id AND tag.id = issue_tag.tag_id
	           WHERE issue_tag.tenant_id = issue.tenant_id AND issue_tag.issue_id = issue.id
	       ), '[]'::jsonb),
	       issue.merged_into_id, issue.merged_at, issue.reporter_email,
	       issue.notify_reporter, issue.resolved_at,
	       (SELECT count(*) FROM feedbacks AS feedback WHERE feedback.issue_id = issue.id),
	       issue.created_at, issue.updated_at
	FROM issues AS issue
	LEFT JOIN tenant_members AS assignee
	  ON assignee.tenant_id = issue.tenant_id AND assignee.id = issue.assignee_id
`

type rowScanner interface {
	Scan(...any) error
}

func scanIssue(row rowScanner) (Issue, error) {
	var found Issue
	var assigneeID, assigneeName, assigneeEmail *string
	var tagPayload []byte
	if err := row.Scan(
		&found.ID, &found.TenantID, &found.ProjectID, &found.PublicID, &found.Title, &found.Description,
		&found.Status, &found.Priority, &assigneeID, &assigneeName, &assigneeEmail, &tagPayload,
		&found.MergedIntoID, &found.MergedAt, &found.ReporterEmail,
		&found.NotifyReporter, &found.ResolvedAt, &found.FeedbackCount,
		&found.CreatedAt, &found.UpdatedAt,
	); err != nil {
		return Issue{}, err
	}
	if assigneeID != nil && assigneeEmail != nil {
		found.Assignee = &triage.Assignee{ID: *assigneeID, Name: assigneeName, Email: *assigneeEmail}
	}
	tags, err := triage.DecodeTags(tagPayload)
	if err != nil {
		return Issue{}, fmt.Errorf("decode issue tags: %w", err)
	}
	found.Tags = tags
	return found, nil
}
