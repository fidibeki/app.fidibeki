package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/activity"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/moderation"
	"github.com/acidiney/app.fidibeki/backend/internal/triage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Store struct {
	Database db.Querier
}

type feedbackIssueState struct {
	ID               string
	ProjectID        string
	IssueID          *string
	ReporterEmail    *string
	NotifyReporter   bool
	ConsentGrantedAt *time.Time
	Priority         *triage.Priority
	AIPriority       *triage.Priority
	AssigneeID       *string
	Description      *string
	Text             *string
	TextCleaned      *string
	AISuggestedTitle *string
	Moderated        bool
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

func (Store) ListComments(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
) ([]Comment, error) {
	rows, err := query.Query(ctx, issueCommentSelect+`
		WHERE comment.tenant_id = $1 AND comment.issue_id = $2
		ORDER BY comment.created_at, comment.id
	`, tenantID, issueID)
	if err != nil {
		return nil, fmt.Errorf("list issue comments: %w", err)
	}
	defer rows.Close()
	comments := make([]Comment, 0)
	for rows.Next() {
		comment, scanErr := scanIssueComment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue comments: %w", err)
	}
	return comments, nil
}

func (Store) CreateComment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	authorMemberID string,
	body string,
	mentionMemberIDs []string,
	now time.Time,
) (Comment, activity.Event, error) {
	if _, err := (Store{}).Get(ctx, query, tenantID, issueID); err != nil {
		return Comment{}, activity.Event{}, err
	}
	mentionMemberIDs = uniqueIssueMutationIDs(mentionMemberIDs)
	if len(mentionMemberIDs) > 0 {
		var count int
		if err := query.QueryRow(ctx, `
			SELECT count(*) FROM tenant_members
			WHERE tenant_id = $1 AND id = ANY($2::uuid[])
			  AND permissions @> ARRAY['issue:read']::text[]
		`, tenantID, mentionMemberIDs).Scan(&count); err != nil {
			return Comment{}, activity.Event{}, fmt.Errorf("validate comment mentions: %w", err)
		}
		if count != len(mentionMemberIDs) {
			return Comment{}, activity.Event{}, ErrNotFound
		}
	}
	commentID := uuid.NewString()
	if _, err := query.Exec(ctx, `
		INSERT INTO issue_comments (id, tenant_id, issue_id, author_member_id, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, commentID, tenantID, issueID, authorMemberID, body, now.UTC()); err != nil {
		return Comment{}, activity.Event{}, fmt.Errorf("create issue comment: %w", err)
	}
	if len(mentionMemberIDs) > 0 {
		if _, err := query.Exec(ctx, `
			INSERT INTO issue_comment_mentions (tenant_id, comment_id, member_id, created_at)
			SELECT $1, $2, member_id, $4
			FROM unnest($3::uuid[]) AS member_id
		`, tenantID, commentID, mentionMemberIDs, now.UTC()); err != nil {
			return Comment{}, activity.Event{}, fmt.Errorf("create issue comment mentions: %w", err)
		}
	}
	actorID := authorMemberID
	activityID, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: issueID, ActorMemberID: &actorID,
		Kind: "issue.comment_created", Data: map[string]any{
			"comment_id": commentID, "body": body, "mention_member_ids": mentionMemberIDs,
		}, CreatedAt: now,
	})
	if err != nil {
		return Comment{}, activity.Event{}, err
	}
	if len(mentionMemberIDs) > 0 {
		if _, err := query.Exec(ctx, `
			INSERT INTO member_notifications (
				tenant_id, recipient_member_id, actor_member_id, issue_id,
				activity_id, kind, created_at
			)
			SELECT $1, member_id, $2, $3, $4, 'issue.mentioned', $6
			FROM unnest($5::uuid[]) AS member_id
			WHERE member_id <> $2
			ON CONFLICT (activity_id, recipient_member_id) DO NOTHING
		`, tenantID, authorMemberID, issueID, activityID, mentionMemberIDs, now.UTC()); err != nil {
			return Comment{}, activity.Event{}, fmt.Errorf("create mention notifications: %w", err)
		}
	}
	comment, err := getIssueComment(ctx, query, tenantID, commentID)
	if err != nil {
		return Comment{}, activity.Event{}, err
	}
	event, err := activity.GetIssue(ctx, query, tenantID, issueID, activityID)
	if err != nil {
		return Comment{}, activity.Event{}, err
	}
	return comment, event, nil
}

func (Store) ListMemberNotifications(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) ([]MemberNotification, int, error) {
	var unread int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FROM member_notifications
		WHERE tenant_id = $1 AND recipient_member_id = $2 AND read_at IS NULL
	`, tenantID, memberID).Scan(&unread); err != nil {
		return nil, 0, fmt.Errorf("count unread member notifications: %w", err)
	}
	rows, err := query.Query(ctx, `
		SELECT notification.id, notification.kind, issue.id, issue.public_id, issue.title,
		       actor.id, actor.name, actor.email::text,
		       COALESCE(activity.data->>'body', ''), notification.read_at, notification.created_at
		FROM member_notifications AS notification
		JOIN issues AS issue
		  ON issue.tenant_id = notification.tenant_id AND issue.id = notification.issue_id
		JOIN issue_activities AS activity ON activity.id = notification.activity_id
		LEFT JOIN tenant_members AS actor
		  ON actor.tenant_id = notification.tenant_id AND actor.id = notification.actor_member_id
		WHERE notification.tenant_id = $1 AND notification.recipient_member_id = $2
		ORDER BY notification.created_at DESC, notification.id DESC
		LIMIT 50
	`, tenantID, memberID)
	if err != nil {
		return nil, 0, fmt.Errorf("list member notifications: %w", err)
	}
	defer rows.Close()
	notifications := make([]MemberNotification, 0)
	for rows.Next() {
		var notification MemberNotification
		var actorID, actorName, actorEmail *string
		if err := rows.Scan(
			&notification.ID, &notification.Kind, &notification.IssueID,
			&notification.PublicID, &notification.IssueTitle,
			&actorID, &actorName, &actorEmail, &notification.Body,
			&notification.ReadAt, &notification.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan member notification: %w", err)
		}
		if actorID != nil && actorEmail != nil {
			notification.Actor = &activity.Actor{ID: *actorID, Name: actorName, Email: *actorEmail}
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate member notifications: %w", err)
	}
	return notifications, unread, nil
}

func (Store) MarkMemberNotificationRead(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	notificationID string,
	now time.Time,
) (bool, error) {
	tag, err := query.Exec(ctx, `
		UPDATE member_notifications SET read_at = COALESCE(read_at, $4)
		WHERE tenant_id = $1 AND recipient_member_id = $2 AND id = $3
	`, tenantID, memberID, notificationID, now.UTC())
	if err != nil {
		return false, fmt.Errorf("mark member notification read: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

const issueCommentSelect = `
	SELECT comment.id, comment.issue_id, comment.body, comment.created_at,
	       author.id, author.name, author.email::text,
	       COALESCE((
	           SELECT jsonb_agg(
	               jsonb_build_object('id', member.id, 'name', member.name, 'email', member.email::text)
	               ORDER BY lower(COALESCE(member.name, member.email::text)), member.id
	           )
	           FROM issue_comment_mentions AS mention
	           JOIN tenant_members AS member
	             ON member.tenant_id = mention.tenant_id AND member.id = mention.member_id
	           WHERE mention.tenant_id = comment.tenant_id AND mention.comment_id = comment.id
	       ), '[]'::jsonb)
	FROM issue_comments AS comment
	LEFT JOIN tenant_members AS author
	  ON author.tenant_id = comment.tenant_id AND author.id = comment.author_member_id
`

func getIssueComment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	commentID string,
) (Comment, error) {
	comment, err := scanIssueComment(query.QueryRow(ctx, issueCommentSelect+`
		WHERE comment.tenant_id = $1 AND comment.id = $2
	`, tenantID, commentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	return comment, err
}

func scanIssueComment(row rowScanner) (Comment, error) {
	var comment Comment
	var actorID, actorName, actorEmail *string
	var mentionsPayload []byte
	if err := row.Scan(
		&comment.ID, &comment.IssueID, &comment.Body, &comment.CreatedAt,
		&actorID, &actorName, &actorEmail, &mentionsPayload,
	); err != nil {
		return Comment{}, err
	}
	if actorID != nil && actorEmail != nil {
		comment.Author = &activity.Actor{ID: *actorID, Name: actorName, Email: *actorEmail}
	}
	if err := json.Unmarshal(mentionsPayload, &comment.Mentions); err != nil {
		return Comment{}, fmt.Errorf("decode issue comment mentions: %w", err)
	}
	return comment, nil
}

func (store Store) CreateFromFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	tenantSlug string,
	actorMemberID string,
	input CreateFromFeedbackInput,
	now time.Time,
) (Issue, error) {
	rows, err := query.Query(ctx, `
		SELECT id, project_id, issue_id, reporter_email, notify_reporter,
		       reporter_consent_granted_at,
		       priority, ai_priority, assignee_id, description, text, text_cleaned,
		       ai_suggested_title,
		       status = 'spam' OR cardinality(text_moderation_flags) > 0 OR text_process_status = 'failed'
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
			&state.NotifyReporter, &state.ConsentGrantedAt,
			&state.Priority, &state.AIPriority, &state.AssigneeID,
			&state.Description, &state.Text, &state.TextCleaned, &state.AISuggestedTitle,
			&state.Moderated,
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
	moderateIssueInput(&input, states)
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
	reporterEmail, notifyReporter, consentGrantedAt := consensusFeedbackReporter(states)
	sequence, err := allocateIssueSequence(ctx, query, tenantID, now.Year())
	if err != nil {
		return Issue{}, err
	}
	createdID := uuid.NewString()
	publicID := fmt.Sprintf("%s-%d-%04d", strings.ToUpper(tenantSlug), now.Year(), sequence)
	if _, err := query.Exec(ctx, `
		INSERT INTO issues (
			id, tenant_id, project_id, public_id, title, description,
			priority, assignee_id, reporter_email, notify_reporter,
			reporter_consent_granted_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, createdID, tenantID, projectID, publicID, input.Title, input.Description,
		priority, assigneeID, reporterEmail, notifyReporter, consentGrantedAt); err != nil {
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
	actorID := actorMemberID
	activityID, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: createdID, ActorMemberID: &actorID,
		Kind: "issue.created", Data: map[string]any{
			"feedback_ids": input.FeedbackIDs,
			"title":        input.Title,
			"description":  input.Description,
		}, CreatedAt: now,
	})
	if err != nil {
		return Issue{}, err
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
		VALUES ($1, 'issue.created', 'issue', $2, $3)
	`, tenantID, createdID, activityID); err != nil {
		return Issue{}, fmt.Errorf("create issue outbox event: %w", err)
	}
	return store.Get(ctx, query, tenantID, createdID)
}

func moderateIssueInput(input *CreateFromFeedbackInput, states []feedbackIssueState) {
	for _, state := range states {
		if !state.Moderated {
			continue
		}
		sources := make([]string, 0, 3)
		for _, value := range []*string{state.Description, state.Text, state.TextCleaned} {
			if value != nil {
				sources = append(sources, *value)
			}
		}
		redactedTitle := moderation.RedactCopies(input.Title, sources...)
		titleDerived := moderation.IsDerived(input.Title, sources...)
		if !titleDerived {
			input.Title = redactedTitle
		} else if state.AISuggestedTitle != nil && strings.TrimSpace(*state.AISuggestedTitle) != "" {
			input.Title = *state.AISuggestedTitle
		} else {
			input.Title = moderation.Placeholder
		}
		input.Description = moderation.RedactCopies(input.Description, sources...)
	}
}

func (Store) ModeratedFeedbackSources(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
) ([]string, error) {
	rows, err := query.Query(ctx, `
		SELECT DISTINCT source.value
		FROM feedbacks AS feedback
		CROSS JOIN LATERAL unnest(ARRAY[
			feedback.description, feedback.text, feedback.text_cleaned
		]) AS source(value)
		WHERE feedback.tenant_id = $1 AND feedback.issue_id = $2
		  AND (feedback.status = 'spam' OR cardinality(feedback.text_moderation_flags) > 0
		       OR feedback.text_process_status = 'failed')
		  AND NULLIF(btrim(source.value), '') IS NOT NULL
		ORDER BY source.value
	`, tenantID, issueID)
	if err != nil {
		return nil, fmt.Errorf("list moderated feedback sources: %w", err)
	}
	defer rows.Close()
	sources := make([]string, 0)
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, fmt.Errorf("scan moderated feedback source: %w", err)
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate moderated feedback sources: %w", err)
	}
	return sources, nil
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

func consensusFeedbackReporter(states []feedbackIssueState) (*string, bool, *time.Time) {
	if len(states) == 0 || !states[0].NotifyReporter || states[0].ReporterEmail == nil {
		return nil, false, nil
	}
	candidate := *states[0].ReporterEmail
	grantedAt := states[0].ConsentGrantedAt
	for _, state := range states[1:] {
		if !state.NotifyReporter || state.ReporterEmail == nil ||
			!strings.EqualFold(*state.ReporterEmail, candidate) {
			return nil, false, nil
		}
		if state.ConsentGrantedAt != nil && (grantedAt == nil || state.ConsentGrantedAt.Before(*grantedAt)) {
			grantedAt = state.ConsentGrantedAt
		}
	}
	return &candidate, true, grantedAt
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

func (store Store) UpdateIssue(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	memberID string,
	mutation Mutation,
	now time.Time,
) (Issue, *Update, error) {
	var oldStatus Status
	err := query.QueryRow(ctx, `
		SELECT status FROM issues
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, issueID).Scan(&oldStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, nil, ErrNotFound
	}
	if err != nil {
		return Issue{}, nil, fmt.Errorf("lock issue for update: %w", err)
	}
	before, err := store.Get(ctx, query, tenantID, issueID)
	if err != nil {
		return Issue{}, nil, err
	}
	if mutation.Status != nil && oldStatus == *mutation.Status {
		return Issue{}, nil, ErrNoStatusChange
	}
	if mutation.Status != nil && *mutation.Status == StatusNeedsInfo {
		allowed, err := reporterAccessAllowed(ctx, query, tenantID, issueID)
		if err != nil {
			return Issue{}, nil, err
		}
		if !allowed || before.ReporterEmail == nil || !before.NotifyReporter {
			return Issue{}, nil, ErrReporterAccessUnavailable
		}
	}
	if mutation.SetAssignee && mutation.AssigneeID != nil {
		var exists bool
		if err := query.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM tenant_members WHERE tenant_id = $1 AND id = $2)
		`, tenantID, *mutation.AssigneeID).Scan(&exists); err != nil {
			return Issue{}, nil, fmt.Errorf("validate issue assignee: %w", err)
		}
		if !exists {
			return Issue{}, nil, ErrNotFound
		}
	}
	tagIDs := uniqueIssueMutationIDs(mutation.AddTagIDs, mutation.RemoveTagIDs)
	if len(tagIDs) > 0 {
		var count int
		if err := query.QueryRow(ctx, `
			SELECT count(*) FROM tags WHERE tenant_id = $1 AND id = ANY($2::uuid[])
		`, tenantID, tagIDs).Scan(&count); err != nil {
			return Issue{}, nil, fmt.Errorf("validate issue tags: %w", err)
		}
		if count != len(tagIDs) {
			return Issue{}, nil, ErrNotFound
		}
	}
	var status, priority, assignee any
	if mutation.Status != nil {
		status = string(*mutation.Status)
	}
	if mutation.Priority != nil {
		priority = string(*mutation.Priority)
	}
	if mutation.AssigneeID != nil {
		assignee = *mutation.AssigneeID
	}
	if _, err := query.Exec(ctx, `
		UPDATE issues
		SET status = CASE WHEN $3 THEN $4::issue_status ELSE status END,
		    title = CASE WHEN $5 THEN $6 ELSE title END,
		    description = CASE WHEN $7 THEN $8 ELSE description END,
		    priority = CASE WHEN $9 THEN $10 ELSE priority END,
		    assignee_id = CASE WHEN $11 THEN $12::uuid ELSE assignee_id END,
		    resolved_at = CASE
		        WHEN $3 AND $4::issue_status = 'resolved' THEN $13
		        WHEN $3 AND $4::issue_status IN ('open', 'in_progress', 'needs_info') THEN NULL
		        ELSE resolved_at
		    END
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, issueID,
		mutation.Status != nil, status,
		mutation.Title != nil, mutation.Title,
		mutation.Description != nil, mutation.Description,
		mutation.SetPriority, priority,
		mutation.SetAssignee, assignee,
		now.UTC(),
	); err != nil {
		return Issue{}, nil, fmt.Errorf("update issue: %w", err)
	}
	if len(mutation.RemoveTagIDs) > 0 {
		if _, err := query.Exec(ctx, `
			DELETE FROM issue_tags
			WHERE tenant_id = $1 AND issue_id = $2 AND tag_id = ANY($3::uuid[])
		`, tenantID, issueID, mutation.RemoveTagIDs); err != nil {
			return Issue{}, nil, fmt.Errorf("remove issue tags: %w", err)
		}
	}
	if len(mutation.AddTagIDs) > 0 {
		if _, err := query.Exec(ctx, `
			INSERT INTO issue_tags (tenant_id, issue_id, tag_id)
			SELECT $1, $2, tag_id FROM unnest($3::uuid[]) AS tag_id
			ON CONFLICT DO NOTHING
		`, tenantID, issueID, mutation.AddTagIDs); err != nil {
			return Issue{}, nil, fmt.Errorf("add issue tags: %w", err)
		}
	}
	found, err := store.Get(ctx, query, tenantID, issueID)
	if err != nil {
		return Issue{}, nil, err
	}
	changes := issueChanges(before, found)
	if mutation.Status == nil {
		if len(changes) > 0 {
			actorID := memberID
			if _, err := activity.Record(ctx, query, activity.RecordInput{
				TenantID: tenantID, IssueID: issueID, ActorMemberID: &actorID,
				Kind: "issue.updated", Data: map[string]any{"changes": changes}, CreatedAt: now,
			}); err != nil {
				return Issue{}, nil, err
			}
		}
		return found, nil, nil
	}
	updateID := uuid.NewString()
	if _, err := query.Exec(ctx, `
		INSERT INTO issue_updates (
			id, tenant_id, issue_id, member_id, old_status, new_status, comment, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, updateID, tenantID, issueID, memberID, oldStatus, *mutation.Status, mutation.Comment, now.UTC()); err != nil {
		return Issue{}, nil, fmt.Errorf("record issue status update: %w", err)
	}
	event := "issue.updated"
	if *mutation.Status == StatusResolved {
		event = "issue.resolved"
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
		VALUES ($1, $2, 'issue', $3, $4)
	`, tenantID, event, issueID, updateID); err != nil {
		return Issue{}, nil, fmt.Errorf("create issue update outbox event: %w", err)
	}
	data := map[string]any{"changes": changes, "legacy_update_id": updateID}
	if mutation.Comment != nil {
		data["transition_comment"] = *mutation.Comment
	}
	if mutation.ReporterMessage != nil {
		data["reporter_message"] = *mutation.ReporterMessage
	}
	actorID := memberID
	activityID, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: issueID, ActorMemberID: &actorID,
		Kind: "issue.status_changed", Data: data, CreatedAt: now,
	})
	if err != nil {
		return Issue{}, nil, err
	}
	if *mutation.Status == StatusNeedsInfo {
		if _, err := query.Exec(ctx, `
			INSERT INTO reporter_clarification_requests (
				tenant_id, issue_id, requested_activity_id, prompt, created_at
			) VALUES ($1, $2, $3, $4, $5)
		`, tenantID, issueID, activityID, *mutation.ReporterMessage, now.UTC()); err != nil {
			return Issue{}, nil, fmt.Errorf("create reporter clarification request: %w", err)
		}
	} else {
		if _, err := query.Exec(ctx, `
			UPDATE reporter_clarification_requests SET closed_at = $3
			WHERE tenant_id = $1 AND issue_id = $2
			  AND responded_at IS NULL AND closed_at IS NULL
		`, tenantID, issueID, now.UTC()); err != nil {
			return Issue{}, nil, fmt.Errorf("close reporter clarification request: %w", err)
		}
	}
	update := &Update{
		ID: updateID, IssueID: issueID, MemberID: &memberID,
		OldStatus: &oldStatus, NewStatus: *mutation.Status,
		Comment: mutation.Comment, CreatedAt: now.UTC(),
	}
	return found, update, nil
}

func reporterAccessAllowed(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
) (bool, error) {
	var allowed bool
	err := query.QueryRow(ctx, `
		SELECT tenant.reporter_access_enabled
		       AND COALESCE(project.reporter_access_enabled, true)
		       AND issue.reporter_access_revoked_at IS NULL
		FROM issues AS issue
		JOIN tenants AS tenant ON tenant.id = issue.tenant_id
		JOIN projects AS project
		  ON project.tenant_id = issue.tenant_id AND project.id = issue.project_id
		WHERE issue.tenant_id = $1 AND issue.id = $2
	`, tenantID, issueID).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("resolve reporter access policy: %w", err)
	}
	return allowed, nil
}

func issueChanges(before, after Issue) map[string]any {
	changes := make(map[string]any)
	addIssueChange(changes, "status", string(before.Status), string(after.Status))
	addIssueChange(changes, "title", before.Title, after.Title)
	addIssueChange(changes, "description", before.Description, after.Description)
	addIssueChange(changes, "priority", issuePriorityValue(before.Priority), issuePriorityValue(after.Priority))
	addIssueChange(changes, "assignee_id", issueAssigneeID(before.Assignee), issueAssigneeID(after.Assignee))
	beforeTags := issueTagIDs(before.Tags)
	afterTags := issueTagIDs(after.Tags)
	if strings.Join(beforeTags, ",") != strings.Join(afterTags, ",") {
		changes["tag_ids"] = map[string]any{"from": beforeTags, "to": afterTags}
	}
	return changes
}

func addIssueChange(changes map[string]any, field string, before, after any) {
	if fmt.Sprint(before) == fmt.Sprint(after) {
		return
	}
	changes[field] = map[string]any{"from": before, "to": after}
}

func issuePriorityValue(priority *triage.Priority) any {
	if priority == nil {
		return nil
	}
	return string(*priority)
}

func issueAssigneeID(assignee *triage.Assignee) any {
	if assignee == nil {
		return nil
	}
	return assignee.ID
}

func issueTagIDs(tags []triage.Tag) []string {
	ids := make([]string, len(tags))
	for index, tag := range tags {
		ids[index] = tag.ID
	}
	return ids
}

func uniqueIssueMutationIDs(groups ...[]string) []string {
	seen := make(map[string]struct{})
	values := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			values = append(values, value)
		}
	}
	return values
}

func (store Store) Merge(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	sourceID string,
	targetID string,
	memberID string,
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
	actorID := memberID
	if _, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: sourceID, ActorMemberID: &actorID,
		Kind: "issue.merged", Data: map[string]any{"target_issue_id": targetID}, CreatedAt: now,
	}); err != nil {
		return Issue{}, err
	}
	if _, err := activity.Record(ctx, query, activity.RecordInput{
		TenantID: tenantID, IssueID: targetID, ActorMemberID: &actorID,
		Kind: "issue.merge_received", Data: map[string]any{"source_issue_id": sourceID}, CreatedAt: now,
	}); err != nil {
		return Issue{}, err
	}
	return store.Get(ctx, query, tenantID, sourceID)
}

func (store Store) Unmerge(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	memberID string,
	now time.Time,
) (Issue, error) {
	var targetID *string
	err := query.QueryRow(ctx, `
		SELECT merged_into_id FROM issues
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, issueID).Scan(&targetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	if err != nil {
		return Issue{}, fmt.Errorf("lock issue for unmerge: %w", err)
	}
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
	if targetID != nil {
		actorID := memberID
		if _, err := activity.Record(ctx, query, activity.RecordInput{
			TenantID: tenantID, IssueID: issueID, ActorMemberID: &actorID,
			Kind: "issue.unmerged", Data: map[string]any{"target_issue_id": *targetID}, CreatedAt: now,
		}); err != nil {
			return Issue{}, err
		}
		if _, err := activity.Record(ctx, query, activity.RecordInput{
			TenantID: tenantID, IssueID: *targetID, ActorMemberID: &actorID,
			Kind: "issue.merge_removed", Data: map[string]any{"source_issue_id": issueID}, CreatedAt: now,
		}); err != nil {
			return Issue{}, err
		}
	}
	return store.Get(ctx, query, tenantID, issueID)
}

const issueSelect = `
	SELECT issue.id, issue.tenant_id, issue.project_id, issue.public_id,
	       CASE WHEN COALESCE(content_moderation.title_moderated, false) THEN COALESCE((
	           SELECT feedback.ai_suggested_title
	           FROM feedbacks AS feedback
	           WHERE feedback.issue_id = issue.id
	             AND (feedback.status = 'spam' OR cardinality(feedback.text_moderation_flags) > 0
	                  OR feedback.text_process_status = 'failed')
	             AND feedback.ai_suggested_title IS NOT NULL
	           ORDER BY feedback.created_at, feedback.id LIMIT 1
	       ), '[MODERATED]') ELSE issue.title END,
	       CASE WHEN COALESCE(content_moderation.description_moderated, false)
	            THEN '[MODERATED]' ELSE issue.description END,
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
	       issue.notify_reporter, issue.reporter_consent_granted_at,
	       issue.reporter_consent_withdrawn_at,
	       issue.resolved_at, issue.reporter_access_revoked_at,
	       (SELECT count(*) FROM feedbacks AS feedback WHERE feedback.issue_id = issue.id),
	       COALESCE(content_moderation.title_moderated, false)
	           OR COALESCE(content_moderation.description_moderated, false),
	       issue.created_at, issue.updated_at
	FROM issues AS issue
	LEFT JOIN tenant_members AS assignee
	  ON assignee.tenant_id = issue.tenant_id AND assignee.id = issue.assignee_id
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
			) AS description_moderated
		FROM feedbacks AS feedback WHERE feedback.issue_id = issue.id
	) AS content_moderation ON true
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
		&found.NotifyReporter, &found.ReporterConsentGrantedAt, &found.ReporterConsentWithdrawnAt,
		&found.ResolvedAt, &found.ReporterAccessRevokedAt, &found.FeedbackCount,
		&found.ContentModerated,
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
