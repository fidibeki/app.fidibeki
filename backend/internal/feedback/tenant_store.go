package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/triage"
	"github.com/jackc/pgx/v5"
)

const feedbackSentimentExpression = `COALESCE(
	feedback.text_sentiment_score::double precision,
	(SELECT avg(attachment.sentiment_score)::double precision
	 FROM attachments AS attachment
	 WHERE attachment.feedback_id = feedback.id)
)`

const feedbackAIStatusExpression = `CASE
	WHEN feedback.text_process_status = 'failed' OR EXISTS (
		SELECT 1 FROM attachments AS attachment
		WHERE attachment.feedback_id = feedback.id AND attachment.processing_status = 'failed'
	) THEN 'failed'
	WHEN feedback.text_process_status = 'processing' OR EXISTS (
		SELECT 1 FROM attachments AS attachment
		WHERE attachment.feedback_id = feedback.id AND attachment.processing_status = 'processing'
	) THEN 'processing'
	WHEN feedback.text_process_status = 'pending' OR EXISTS (
		SELECT 1 FROM attachments AS attachment
		WHERE attachment.feedback_id = feedback.id AND attachment.processing_status = 'pending'
	) THEN 'pending'
	WHEN feedback.text_process_status = 'completed' OR EXISTS (
		SELECT 1 FROM attachments AS attachment
		WHERE attachment.feedback_id = feedback.id AND attachment.processing_status = 'completed'
	) THEN 'completed'
	ELSE 'skipped'
END`

const tenantFeedbackColumns = `
	feedback.id, feedback.public_id, feedback.project_id, project.name,
	feedback.widget_rule_id, feedback.session_id, feedback.issue_id,
	issue.public_id, issue.status::text, feedback.rating, feedback.responses, feedback.comment,
	feedback.widget_definition_version, feedback.title, feedback.description,
	feedback.text, feedback.text_cleaned, feedback.ai_suggested_title,
	feedback.ai_actionability, feedback.ai_category, feedback.ai_priority,
	feedback.priority, assignee.id, assignee.name, assignee.email::text,
	COALESCE((
		SELECT jsonb_agg(
			jsonb_build_object('id', tag.id, 'name', tag.name::text, 'color', tag.color)
			ORDER BY lower(tag.name::text), tag.id
		)
		FROM feedback_tags AS feedback_tag
		JOIN tags AS tag
		  ON tag.tenant_id = feedback_tag.tenant_id AND tag.id = feedback_tag.tag_id
		WHERE feedback_tag.tenant_id = feedback.tenant_id
		  AND feedback_tag.feedback_id = feedback.id
	), '[]'::jsonb),
	feedback.reporter_email, feedback.notify_reporter,
	feedback.page_url, feedback.referrer, feedback.user_agent, feedback.os,
	feedback.browser, feedback.device, feedback.viewport_width,
	feedback.viewport_height, feedback.language, feedback.status::text,
	` + feedbackAIStatusExpression + `,
	` + feedbackSentimentExpression + `,
	ARRAY(
		SELECT DISTINCT flag
		FROM (
			SELECT unnest(feedback.text_moderation_flags) AS flag
			UNION ALL
			SELECT unnest(attachment.moderation_flags) AS flag
			FROM attachments AS attachment
			WHERE attachment.feedback_id = feedback.id
		) AS flags
		WHERE flag <> ''
		ORDER BY flag
	),
	ARRAY(
		SELECT DISTINCT attachment.kind::text
		FROM attachments AS attachment
		WHERE attachment.feedback_id = feedback.id
		ORDER BY attachment.kind::text
	),
	feedback.created_at, feedback.updated_at
`

const tenantFeedbackFrom = `
	FROM feedbacks AS feedback
	JOIN projects AS project ON project.id = feedback.project_id
	LEFT JOIN issues AS issue ON issue.id = feedback.issue_id
	LEFT JOIN tenant_members AS assignee
	  ON assignee.tenant_id = feedback.tenant_id AND assignee.id = feedback.assignee_id
`

func (Store) ListTenantFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	options ListOptions,
) ([]Item, error) {
	args := []any{tenantID}
	where := []string{"feedback.tenant_id = $1"}
	add := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if options.ProjectID != "" {
		where = append(where, "feedback.project_id = "+add(options.ProjectID))
	}
	if len(options.IDs) > 0 {
		where = append(where, "feedback.id = ANY("+add(options.IDs)+"::uuid[])")
	}
	if options.Query != "" {
		term := add("%" + options.Query + "%")
		where = append(where, `(feedback.public_id ILIKE `+term+`
			OR feedback.title ILIKE `+term+`
			OR COALESCE(feedback.description, feedback.text_cleaned, feedback.text, '') ILIKE `+term+`
			OR feedback.page_url ILIKE `+term+`
			OR COALESCE(feedback.reporter_email, '') ILIKE `+term+`
			OR project.name ILIKE `+term+`
			OR COALESCE(issue.public_id, '') ILIKE `+term+`)`)
	}
	if options.Status != "" {
		where = append(where, "feedback.status = "+add(options.Status)+"::feedback_status")
	}
	if options.Rating != 0 {
		where = append(where, "feedback.rating = "+add(options.Rating))
	}
	if options.Attachment != "" {
		switch options.Attachment {
		case "any":
			where = append(where, "(feedback.session_id IS NOT NULL OR EXISTS (SELECT 1 FROM attachments AS attachment WHERE attachment.feedback_id = feedback.id))")
		case "none":
			where = append(where, "(feedback.session_id IS NULL AND NOT EXISTS (SELECT 1 FROM attachments AS attachment WHERE attachment.feedback_id = feedback.id))")
		case "replay":
			where = append(where, "(feedback.session_id IS NOT NULL OR EXISTS (SELECT 1 FROM attachments AS attachment WHERE attachment.feedback_id = feedback.id AND attachment.kind = 'replay'))")
		default:
			where = append(where, "EXISTS (SELECT 1 FROM attachments AS attachment WHERE attachment.feedback_id = feedback.id AND attachment.kind::text = "+add(options.Attachment)+")")
		}
	}
	if options.Processing != "" {
		where = append(where, "("+feedbackAIStatusExpression+") = "+add(options.Processing))
	}
	moderationFilter := `(cardinality(feedback.text_moderation_flags) > 0 OR EXISTS (
		SELECT 1 FROM attachments AS attachment
		WHERE attachment.feedback_id = feedback.id AND cardinality(attachment.moderation_flags) > 0
	))`
	switch options.Moderation {
	case "flagged":
		where = append(where, moderationFilter)
	case "clean":
		where = append(where, "NOT "+moderationFilter)
	}
	switch options.Issue {
	case "linked":
		where = append(where, "feedback.issue_id IS NOT NULL")
	case "unlinked":
		where = append(where, "feedback.issue_id IS NULL")
	}
	if options.DateFrom != nil {
		where = append(where, "feedback.created_at >= "+add(options.DateFrom.UTC()))
	}
	if options.DateTo != nil {
		where = append(where, "feedback.created_at < "+add(options.DateTo.UTC()))
	}
	if options.Sentiment != "" {
		switch options.Sentiment {
		case "positive":
			where = append(where, feedbackSentimentExpression+" > 0.2")
		case "negative":
			where = append(where, feedbackSentimentExpression+" < -0.2")
		case "neutral":
			where = append(where, feedbackSentimentExpression+" BETWEEN -0.2 AND 0.2")
		}
	}

	order := "feedback.created_at DESC, feedback.id DESC"
	if options.Cursor != nil {
		created := add(options.Cursor.CreatedAt.UTC())
		id := add(options.Cursor.ID)
		switch options.Sort {
		case SortOldest:
			where = append(where, "(feedback.created_at, feedback.id) > ("+created+", "+id+"::uuid)")
		case SortRatingLow:
			rating := add(options.Cursor.RatingKey)
			where = append(where, "(COALESCE(feedback.rating, 6) > "+rating+" OR (COALESCE(feedback.rating, 6) = "+rating+" AND (feedback.created_at, feedback.id) < ("+created+", "+id+"::uuid)))")
		case SortRatingHigh:
			rating := add(options.Cursor.RatingKey)
			where = append(where, "(COALESCE(feedback.rating, 0) < "+rating+" OR (COALESCE(feedback.rating, 0) = "+rating+" AND (feedback.created_at, feedback.id) < ("+created+", "+id+"::uuid)))")
		default:
			where = append(where, "(feedback.created_at, feedback.id) < ("+created+", "+id+"::uuid)")
		}
	}
	switch options.Sort {
	case SortOldest:
		order = "feedback.created_at ASC, feedback.id ASC"
	case SortRatingLow:
		order = "COALESCE(feedback.rating, 6) ASC, feedback.created_at DESC, feedback.id DESC"
	case SortRatingHigh:
		order = "COALESCE(feedback.rating, 0) DESC, feedback.created_at DESC, feedback.id DESC"
	}

	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1)
	statement := "SELECT " + tenantFeedbackColumns + tenantFeedbackFrom +
		" WHERE " + strings.Join(where, " AND ") + " ORDER BY " + order +
		fmt.Sprintf(" LIMIT $%d", len(args))
	rows, err := query.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list tenant feedback: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanTenantFeedback(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant feedback: %w", err)
	}
	return items, nil
}

func (Store) GetTenantFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackID string,
) (Detail, error) {
	row := query.QueryRow(ctx, "SELECT "+tenantFeedbackColumns+`,
		feedback.dom_snapshot_html, feedback.dom_snapshot_styles, feedback.ai_processed_at
		`+tenantFeedbackFrom+` WHERE feedback.tenant_id = $1 AND feedback.id = $2`, tenantID, feedbackID)
	detail, err := scanTenantFeedbackDetail(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	attachments, err := listTenantAttachments(ctx, query, tenantID, feedbackID)
	if err != nil {
		return Detail{}, err
	}
	detail.Attachments = attachments
	if detail.SessionID != nil {
		session, err := getTenantSession(ctx, query, tenantID, *detail.SessionID)
		if err != nil {
			return Detail{}, err
		}
		detail.Session = &session
	}
	return detail, nil
}

func (Store) UpdateTenantFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackID string,
	mutation TenantMutation,
) (Detail, error) {
	if _, err := (Store{}).BulkUpdateTenantFeedback(
		ctx, query, tenantID, []string{feedbackID}, mutation,
	); err != nil {
		return Detail{}, err
	}
	return (Store{}).GetTenantFeedback(ctx, query, tenantID, feedbackID)
}

func (Store) BulkUpdateTenantFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackIDs []string,
	mutation TenantMutation,
) ([]Item, error) {
	if _, err := lockTenantFeedbackStates(ctx, query, tenantID, feedbackIDs); err != nil {
		return nil, err
	}
	if mutation.SetAssignee && mutation.AssigneeID != nil {
		var exists bool
		err := query.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM tenant_members WHERE tenant_id = $1 AND id = $2
			)
		`, tenantID, *mutation.AssigneeID).Scan(&exists)
		if err != nil {
			return nil, fmt.Errorf("validate feedback assignee: %w", err)
		}
		if !exists {
			return nil, ErrNotFound
		}
	}
	tagIDs := uniqueTenantMutationIDs(mutation.AddTagIDs, mutation.RemoveTagIDs)
	if len(tagIDs) > 0 {
		var count int
		if err := query.QueryRow(ctx, `
			SELECT count(*) FROM tags WHERE tenant_id = $1 AND id = ANY($2::uuid[])
		`, tenantID, tagIDs).Scan(&count); err != nil {
			return nil, fmt.Errorf("validate feedback tags: %w", err)
		}
		if count != len(tagIDs) {
			return nil, ErrNotFound
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
		UPDATE feedbacks
		SET status = CASE WHEN $3 THEN $4::feedback_status ELSE status END,
		    title = CASE WHEN $5 THEN $6 ELSE title END,
		    priority = CASE WHEN $7 THEN $8 ELSE priority END,
		    assignee_id = CASE WHEN $9 THEN $10::uuid ELSE assignee_id END
		WHERE tenant_id = $1 AND id = ANY($2::uuid[])
	`, tenantID, feedbackIDs,
		mutation.Status != nil, status,
		mutation.Title != nil, mutation.Title,
		mutation.SetPriority, priority,
		mutation.SetAssignee, assignee,
	); err != nil {
		return nil, fmt.Errorf("bulk update feedback: %w", err)
	}
	if len(mutation.RemoveTagIDs) > 0 {
		if _, err := query.Exec(ctx, `
			DELETE FROM feedback_tags
			WHERE tenant_id = $1
			  AND feedback_id = ANY($2::uuid[])
			  AND tag_id = ANY($3::uuid[])
		`, tenantID, feedbackIDs, mutation.RemoveTagIDs); err != nil {
			return nil, fmt.Errorf("remove feedback tags: %w", err)
		}
	}
	if len(mutation.AddTagIDs) > 0 {
		if _, err := query.Exec(ctx, `
			INSERT INTO feedback_tags (tenant_id, feedback_id, tag_id)
			SELECT $1, feedback_id, tag_id
			FROM unnest($2::uuid[]) AS feedback_id
			CROSS JOIN unnest($3::uuid[]) AS tag_id
			ON CONFLICT DO NOTHING
		`, tenantID, feedbackIDs, mutation.AddTagIDs); err != nil {
			return nil, fmt.Errorf("add feedback tags: %w", err)
		}
	}
	return listTenantFeedbackByIDs(ctx, query, tenantID, feedbackIDs)
}

func (Store) LinkTenantFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackIDs []string,
	issueID string,
) ([]Item, error) {
	states, err := lockTenantFeedbackStates(ctx, query, tenantID, feedbackIDs)
	if err != nil {
		return nil, err
	}
	var issueProjectID string
	var mergedIntoID *string
	err = query.QueryRow(ctx, `
		SELECT project_id, merged_into_id
		FROM issues
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, issueID).Scan(&issueProjectID, &mergedIntoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock issue for feedback link: %w", err)
	}
	if mergedIntoID != nil {
		return nil, ErrMergedIssue
	}
	for _, state := range states {
		if state.ProjectID != issueProjectID {
			return nil, ErrProjectMismatch
		}
		if state.IssueID != nil && *state.IssueID != issueID {
			return nil, ErrLinkConflict
		}
	}
	if _, err := query.Exec(ctx, `
		UPDATE feedbacks
		SET issue_id = $3
		WHERE tenant_id = $1 AND id = ANY($2::uuid[]) AND issue_id IS NULL
	`, tenantID, feedbackIDs, issueID); err != nil {
		return nil, fmt.Errorf("link feedback to issue: %w", err)
	}
	return listTenantFeedbackByIDs(ctx, query, tenantID, feedbackIDs)
}

func (Store) UnlinkTenantFeedback(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackIDs []string,
) ([]Item, error) {
	if _, err := lockTenantFeedbackStates(ctx, query, tenantID, feedbackIDs); err != nil {
		return nil, err
	}
	if _, err := query.Exec(ctx, `
		UPDATE feedbacks SET issue_id = NULL
		WHERE tenant_id = $1 AND id = ANY($2::uuid[])
	`, tenantID, feedbackIDs); err != nil {
		return nil, fmt.Errorf("unlink feedback from issue: %w", err)
	}
	return listTenantFeedbackByIDs(ctx, query, tenantID, feedbackIDs)
}

type tenantFeedbackLinkState struct {
	ID        string
	ProjectID string
	IssueID   *string
}

func lockTenantFeedbackStates(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackIDs []string,
) ([]tenantFeedbackLinkState, error) {
	rows, err := query.Query(ctx, `
		SELECT id, project_id, issue_id
		FROM feedbacks
		WHERE tenant_id = $1 AND id = ANY($2::uuid[])
		ORDER BY id
		FOR UPDATE
	`, tenantID, feedbackIDs)
	if err != nil {
		return nil, fmt.Errorf("lock feedback: %w", err)
	}
	states := make([]tenantFeedbackLinkState, 0, len(feedbackIDs))
	for rows.Next() {
		var state tenantFeedbackLinkState
		if err := rows.Scan(&state.ID, &state.ProjectID, &state.IssueID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan locked feedback: %w", err)
		}
		states = append(states, state)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked feedback: %w", err)
	}
	if len(states) != len(feedbackIDs) {
		return nil, ErrNotFound
	}
	return states, nil
}

func listTenantFeedbackByIDs(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackIDs []string,
) ([]Item, error) {
	return (Store{}).ListTenantFeedback(ctx, query, tenantID, ListOptions{
		IDs: feedbackIDs, Sort: SortNewest, Limit: len(feedbackIDs),
	})
}

func uniqueTenantMutationIDs(groups ...[]string) []string {
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

func (Store) TenantDashboard(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	rangeDays int,
	now time.Time,
) (Dashboard, error) {
	now = now.UTC()
	currentStart := now.AddDate(0, 0, -rangeDays)
	previousStart := currentStart.AddDate(0, 0, -rangeDays)
	args := []any{tenantID, previousStart, currentStart, now}
	projectClause := ""
	if projectID != "" {
		args = append(args, projectID)
		projectClause = fmt.Sprintf(" AND feedback.project_id = $%d", len(args))
	}
	moderationOrFailure := `(cardinality(feedback.text_moderation_flags) > 0
		OR feedback.text_process_status = 'failed'
		OR EXISTS (SELECT 1 FROM attachments AS attachment
			WHERE attachment.feedback_id = feedback.id
			AND (attachment.processing_status = 'failed' OR cardinality(attachment.moderation_flags) > 0)))`
	var currentCount, previousCount, attentionCount int
	var satisfaction, previousSatisfaction *float64
	err := query.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE feedback.created_at >= $3 AND feedback.created_at < $4),
			count(*) FILTER (WHERE feedback.created_at >= $2 AND feedback.created_at < $3),
			(avg(feedback.rating) FILTER (WHERE feedback.created_at >= $3 AND feedback.created_at < $4))::double precision,
			(avg(feedback.rating) FILTER (WHERE feedback.created_at >= $2 AND feedback.created_at < $3))::double precision,
			count(*) FILTER (WHERE feedback.created_at >= $3 AND feedback.created_at < $4
				AND feedback.status = 'open' AND `+moderationOrFailure+`)
		FROM feedbacks AS feedback
		WHERE feedback.tenant_id = $1 AND feedback.created_at >= $2 AND feedback.created_at < $4`+projectClause,
		args...,
	).Scan(&currentCount, &previousCount, &satisfaction, &previousSatisfaction, &attentionCount)
	if err != nil {
		return Dashboard{}, fmt.Errorf("load feedback dashboard metrics: %w", err)
	}

	issueArgs := []any{tenantID}
	issueProjectClause := ""
	if projectID != "" {
		issueArgs = append(issueArgs, projectID)
		issueProjectClause = " AND project_id = $2"
	}
	var openIssues int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FROM issues
		WHERE tenant_id = $1 AND status IN ('open', 'in_progress')
		  AND merged_into_id IS NULL`+issueProjectClause, issueArgs...).Scan(&openIssues); err != nil {
		return Dashboard{}, fmt.Errorf("load feedback dashboard issues: %w", err)
	}

	trendArgs := []any{tenantID, currentStart, now}
	trendProjectClause := ""
	if projectID != "" {
		trendArgs = append(trendArgs, projectID)
		trendProjectClause = fmt.Sprintf(" AND feedback.project_id = $%d", len(trendArgs))
	}
	rows, err := query.Query(ctx, `
		SELECT day::date, count(feedback.id), avg(feedback.rating)::double precision
		FROM generate_series($2::date, $3::date, interval '1 day') AS day
		LEFT JOIN feedbacks AS feedback
		  ON feedback.tenant_id = $1
		 AND feedback.created_at >= day
		 AND feedback.created_at < day + interval '1 day'`+trendProjectClause+`
		GROUP BY day ORDER BY day`, trendArgs...)
	if err != nil {
		return Dashboard{}, fmt.Errorf("load feedback dashboard trend: %w", err)
	}
	defer rows.Close()
	trend := make([]TrendPoint, 0, rangeDays+1)
	for rows.Next() {
		var point TrendPoint
		if err := rows.Scan(&point.Date, &point.FeedbackCount, &point.Satisfaction); err != nil {
			return Dashboard{}, fmt.Errorf("scan feedback dashboard trend: %w", err)
		}
		trend = append(trend, point)
	}
	if err := rows.Err(); err != nil {
		return Dashboard{}, fmt.Errorf("iterate feedback dashboard trend: %w", err)
	}

	dashboard := Dashboard{
		RangeDays: rangeDays, FeedbackCount: currentCount, OpenIssueCount: openIssues,
		Satisfaction: satisfaction, AttentionCount: attentionCount, Trend: trend,
	}
	if previousCount > 0 {
		delta := (float64(currentCount-previousCount) / float64(previousCount)) * 100
		dashboard.FeedbackDeltaPercent = &delta
	} else if currentCount > 0 {
		delta := 100.0
		dashboard.FeedbackDeltaPercent = &delta
	}
	if satisfaction != nil && previousSatisfaction != nil {
		delta := *satisfaction - *previousSatisfaction
		dashboard.SatisfactionDelta = &delta
	}
	return dashboard, nil
}

type tenantRowScanner interface {
	Scan(...any) error
}

func scanTenantFeedback(row tenantRowScanner) (Item, error) {
	var item Item
	var attachmentKinds []string
	var assigneeID, assigneeName, assigneeEmail *string
	var tagPayload []byte
	var responsesPayload []byte
	if err := row.Scan(tenantFeedbackScanTargets(
		&item, &attachmentKinds, &assigneeID, &assigneeName, &assigneeEmail, &tagPayload, &responsesPayload,
	)...); err != nil {
		return Item{}, err
	}
	if err := finalizeTenantFeedbackTriage(
		&item, assigneeID, assigneeName, assigneeEmail, tagPayload,
	); err != nil {
		return Item{}, err
	}
	if err := json.Unmarshal(responsesPayload, &item.Responses); err != nil {
		return Item{}, fmt.Errorf("decode feedback responses: %w", err)
	}
	finalizeTenantFeedback(&item, attachmentKinds)
	return item, nil
}

func scanTenantFeedbackDetail(row tenantRowScanner) (Detail, error) {
	var detail Detail
	var attachmentKinds []string
	var assigneeID, assigneeName, assigneeEmail *string
	var tagPayload []byte
	var responsesPayload []byte
	targets := tenantFeedbackScanTargets(
		&detail.Item, &attachmentKinds, &assigneeID, &assigneeName, &assigneeEmail, &tagPayload, &responsesPayload,
	)
	targets = append(targets, &detail.DOMSnapshotHTML, &detail.DOMSnapshotStyles, &detail.AIProcessedAt)
	if err := row.Scan(targets...); err != nil {
		return Detail{}, err
	}
	if err := finalizeTenantFeedbackTriage(
		&detail.Item, assigneeID, assigneeName, assigneeEmail, tagPayload,
	); err != nil {
		return Detail{}, err
	}
	if err := json.Unmarshal(responsesPayload, &detail.Responses); err != nil {
		return Detail{}, fmt.Errorf("decode feedback responses: %w", err)
	}
	finalizeTenantFeedback(&detail.Item, attachmentKinds)
	return detail, nil
}

func tenantFeedbackScanTargets(
	item *Item,
	attachmentKinds *[]string,
	assigneeID **string,
	assigneeName **string,
	assigneeEmail **string,
	tagPayload *[]byte,
	responsesPayload *[]byte,
) []any {
	return []any{
		&item.ID, &item.PublicID, &item.ProjectID, &item.ProjectName,
		&item.WidgetRuleID, &item.SessionID, &item.IssueID, &item.IssuePublicID,
		&item.IssueStatus, &item.Rating, responsesPayload, &item.Comment,
		&item.DefinitionVersion, &item.Title, &item.Description,
		&item.Text, &item.TextCleaned, &item.AISuggestedTitle,
		&item.AIActionability, &item.AICategory, &item.AIPriority,
		&item.Priority, assigneeID, assigneeName, assigneeEmail, tagPayload,
		&item.ReporterEmail, &item.NotifyReporter, &item.PageURL, &item.Referrer,
		&item.UserAgent, &item.OS, &item.Browser, &item.Device, &item.ViewportWidth,
		&item.ViewportHeight, &item.Language, &item.Status, &item.AIStatus,
		&item.SentimentScore, &item.ModerationFlags, attachmentKinds,
		&item.CreatedAt, &item.UpdatedAt,
	}
}

func finalizeTenantFeedbackTriage(
	item *Item,
	assigneeID *string,
	assigneeName *string,
	assigneeEmail *string,
	tagPayload []byte,
) error {
	if assigneeID != nil && assigneeEmail != nil {
		item.Assignee = &triage.Assignee{ID: *assigneeID, Name: assigneeName, Email: *assigneeEmail}
	}
	tags, err := triage.DecodeTags(tagPayload)
	if err != nil {
		return fmt.Errorf("decode feedback tags: %w", err)
	}
	item.Tags = tags
	return nil
}

func finalizeTenantFeedback(item *Item, attachmentKinds []string) {
	item.AttachmentKinds = make([]AttachmentKind, len(attachmentKinds))
	for index, kind := range attachmentKinds {
		item.AttachmentKinds[index] = AttachmentKind(kind)
	}
	item.HasReplay = item.SessionID != nil
}

func listTenantAttachments(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackID string,
) ([]AttachmentDetail, error) {
	rows, err := query.Query(ctx, `
		SELECT id, kind::text, content_type, size_bytes, duration_seconds,
		       processing_status::text, error_message, transcription,
		       sentiment_score::double precision, moderation_flags,
		       processing_attempts, completed_at, processed_at, created_at
		FROM attachments
		WHERE tenant_id = $1 AND feedback_id = $2
		ORDER BY created_at, id
	`, tenantID, feedbackID)
	if err != nil {
		return nil, fmt.Errorf("list tenant feedback attachments: %w", err)
	}
	defer rows.Close()
	attachments := make([]AttachmentDetail, 0)
	for rows.Next() {
		var attachment AttachmentDetail
		if err := rows.Scan(
			&attachment.ID, &attachment.Kind, &attachment.ContentType, &attachment.SizeBytes,
			&attachment.DurationSeconds, &attachment.ProcessingStatus, &attachment.ErrorMessage,
			&attachment.Transcription, &attachment.SentimentScore, &attachment.ModerationFlags,
			&attachment.ProcessingAttempts, &attachment.CompletedAt, &attachment.ProcessedAt,
			&attachment.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tenant feedback attachment: %w", err)
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant feedback attachments: %w", err)
	}
	return attachments, nil
}

func (Store) GetTenantAttachmentObject(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	feedbackID string,
	attachmentID string,
) (AttachmentObject, error) {
	var object AttachmentObject
	err := query.QueryRow(ctx, `
		SELECT id, kind::text, storage_key, content_type, size_bytes, completed_at, created_at
		FROM attachments
		WHERE tenant_id = $1 AND feedback_id = $2 AND id = $3 AND completed_at IS NOT NULL
	`, tenantID, feedbackID, attachmentID).Scan(
		&object.ID, &object.Kind, &object.StorageKey, &object.ContentType,
		&object.SizeBytes, &object.CompletedAt, &object.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AttachmentObject{}, ErrNotFound
	}
	if err != nil {
		return AttachmentObject{}, fmt.Errorf("get tenant attachment object: %w", err)
	}
	return object, nil
}

func getTenantSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	sessionID string,
) (SessionDetail, error) {
	var session SessionDetail
	var mergedAt *time.Time
	err := query.QueryRow(ctx, `
		SELECT id, page_url, started_at, ended_at, events_count, batches_count,
		       rrweb_version, merged_at, merge_error,
		       CASE
		           WHEN merged_at IS NOT NULL THEN 'ready'
		           WHEN merge_error IS NOT NULL AND merge_attempts >= 12 THEN 'failed'
		           WHEN ended_at IS NOT NULL THEN 'processing'
		           ELSE 'recording'
		       END
		FROM sessions WHERE tenant_id = $1 AND id = $2
	`, tenantID, sessionID).Scan(
		&session.ID, &session.PageURL, &session.StartedAt, &session.EndedAt,
		&session.EventsCount, &session.BatchesCount, &session.RRWebVersion,
		&mergedAt, &session.MergeError, &session.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionDetail{}, ErrNotFound
	}
	if err != nil {
		return SessionDetail{}, fmt.Errorf("get tenant feedback session: %w", err)
	}
	session.ReplayReady = session.Status == "ready"
	return session, nil
}
