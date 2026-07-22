package widgetanalytics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type Store struct{}

var errEventIDConflict = errors.New("widget funnel event ID conflicts with another event")

func (Store) InsertEvents(
	ctx context.Context,
	query db.Querier,
	events []EventValues,
) (IngestResult, error) {
	result := IngestResult{
		AcceptedEventIDs:  make([]string, 0, len(events)),
		DuplicateEventIDs: make([]string, 0),
	}
	if len(events) == 0 {
		return result, nil
	}
	tenantID, projectID := events[0].TenantID, events[0].ProjectID
	for _, event := range events[1:] {
		if event.TenantID != tenantID || event.ProjectID != projectID {
			return IngestResult{}, errors.New("widget funnel batch must belong to one tenant and project")
		}
	}
	var lockedProjectID string
	if err := query.QueryRow(ctx, `
		SELECT id FROM projects
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, projectID).Scan(&lockedProjectID); err != nil {
		return IngestResult{}, fmt.Errorf("lock widget funnel project: %w", err)
	}
	for _, event := range events {
		inserted, err := insertEvent(ctx, query, event, event.EventType != EventMatched)
		if err != nil {
			return IngestResult{}, err
		}
		if !inserted {
			result.DuplicateEventIDs = append(result.DuplicateEventIDs, event.EventID)
			continue
		}
		result.AcceptedEventIDs = append(result.AcceptedEventIDs, event.EventID)
	}
	return result, nil
}

func (Store) InsertSubmittedEvent(
	ctx context.Context,
	query db.Querier,
	event EventValues,
) (bool, error) {
	if event.EventType != EventSubmitted || event.FeedbackID == nil {
		return false, errors.New("submitted widget funnel event requires feedback context")
	}
	return insertEvent(ctx, query, event, false)
}

func insertEvent(
	ctx context.Context,
	query db.Querier,
	event EventValues,
	requireMatched bool,
) (bool, error) {
	if _, err := query.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, event.EventID,
	); err != nil {
		return false, fmt.Errorf("lock widget funnel event ID: %w", err)
	}
	var eventIDExists bool
	if err := query.QueryRow(ctx,
		`SELECT widget_funnel_event_id_exists($1::uuid)`, event.EventID,
	).Scan(&eventIDExists); err != nil {
		return false, fmt.Errorf("check global widget funnel event ID: %w", err)
	}
	if eventIDExists {
		return classifyExistingEventID(ctx, query, event)
	}

	if requireMatched {
		var matched bool
		if err := query.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM widget_funnel_events
				WHERE tenant_id = $1 AND project_id = $2 AND rule_id = $3
				  AND page_view_id = $4 AND visitor_id = $5
				  AND page_key = $6 AND event_type = 'matched'
			)
		`, event.TenantID, event.ProjectID, event.RuleID, event.PageViewID,
			event.VisitorID, event.PageKey,
		).Scan(&matched); err != nil {
			return false, fmt.Errorf("check widget funnel matched event: %w", err)
		}
		if !matched {
			return false, errors.New("widget funnel event requires a matching matched event")
		}
	}
	var insertedID string
	err := query.QueryRow(ctx, `
		INSERT INTO widget_funnel_events (
			event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			visitor_id, page_view_id, page_url, page_key, feedback_id,
			occurred_at, received_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT DO NOTHING
		RETURNING event_id
	`, event.EventID, event.TenantID, event.ProjectID, event.RuleID, event.RuleName,
		event.EventType, event.VisitorID, event.PageViewID, event.PageURL, event.PageKey,
		event.FeedbackID, event.OccurredAt, event.ReceivedAt,
	).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return classifyLifecycleDuplicate(ctx, query, event)
	}
	if err != nil {
		return false, fmt.Errorf("insert widget funnel event: %w", err)
	}
	if _, err := query.Exec(ctx, `
			INSERT INTO widget_funnel_daily (
				tenant_id, project_id, rule_id, rule_name, page_url, page_key, event_date,
				matched_count, impression_count, interaction_count, submitted_count, dismissed_count
			) VALUES (
				$1, $2, $3, $4, $5, $6, ($7 AT TIME ZONE 'UTC')::date,
				CASE WHEN $8 = 'matched' THEN 1 ELSE 0 END,
				CASE WHEN $8 = 'impression' THEN 1 ELSE 0 END,
				CASE WHEN $8 = 'interaction' THEN 1 ELSE 0 END,
				CASE WHEN $8 = 'submitted' THEN 1 ELSE 0 END,
				CASE WHEN $8 = 'dismissed' THEN 1 ELSE 0 END
			)
			ON CONFLICT (project_id, rule_id, event_date, page_key) DO UPDATE SET
				rule_name = EXCLUDED.rule_name,
				page_url = EXCLUDED.page_url,
				matched_count = widget_funnel_daily.matched_count + EXCLUDED.matched_count,
				impression_count = widget_funnel_daily.impression_count + EXCLUDED.impression_count,
				interaction_count = widget_funnel_daily.interaction_count + EXCLUDED.interaction_count,
				submitted_count = widget_funnel_daily.submitted_count + EXCLUDED.submitted_count,
				dismissed_count = widget_funnel_daily.dismissed_count + EXCLUDED.dismissed_count,
				updated_at = now()
	`, event.TenantID, event.ProjectID, event.RuleID, event.RuleName, event.PageURL,
		event.PageKey, event.OccurredAt, event.EventType,
	); err != nil {
		return false, fmt.Errorf("increment widget funnel daily aggregate: %w", err)
	}
	return true, nil
}

func classifyExistingEventID(ctx context.Context, query db.Querier, event EventValues) (bool, error) {
	var existing EventValues
	err := query.QueryRow(ctx, `
		SELECT event_id, tenant_id, project_id, rule_id, event_type, visitor_id,
		       page_view_id, page_url, page_key, feedback_id, occurred_at
		FROM widget_funnel_events
		WHERE event_id = $1
	`, event.EventID).Scan(
		&existing.EventID, &existing.TenantID, &existing.ProjectID, &existing.RuleID,
		&existing.EventType, &existing.VisitorID, &existing.PageViewID, &existing.PageURL,
		&existing.PageKey, &existing.FeedbackID, &existing.OccurredAt,
	)
	if err == nil {
		if sameImmutableEvent(existing, event) {
			return false, nil
		}
		return false, errEventIDConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("classify widget funnel event ID conflict: %w", err)
	}
	return false, errEventIDConflict
}

func classifyLifecycleDuplicate(ctx context.Context, query db.Querier, event EventValues) (bool, error) {
	var lifecycleExists bool
	if err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM widget_funnel_events
			WHERE project_id = $1 AND rule_id = $2
			  AND page_view_id = $3 AND event_type = $4
		)
	`, event.ProjectID, event.RuleID, event.PageViewID, event.EventType).Scan(&lifecycleExists); err != nil {
		return false, fmt.Errorf("classify widget funnel lifecycle conflict: %w", err)
	}
	if lifecycleExists {
		return false, nil
	}
	return false, errEventIDConflict
}

func sameImmutableEvent(left, right EventValues) bool {
	return left.EventID == right.EventID &&
		left.TenantID == right.TenantID &&
		left.ProjectID == right.ProjectID &&
		left.RuleID == right.RuleID &&
		left.EventType == right.EventType &&
		left.VisitorID == right.VisitorID &&
		left.PageViewID == right.PageViewID &&
		left.PageURL == right.PageURL &&
		bytes.Equal(left.PageKey, right.PageKey) &&
		sameOptionalString(left.FeedbackID, right.FeedbackID) &&
		left.OccurredAt.Equal(right.OccurredAt)
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (Store) Summary(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	rangeDays int,
	now time.Time,
) (Summary, error) {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -(rangeDays - 1))
	var summary Summary
	err := query.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(matched_count), 0),
			COALESCE(SUM(impression_count), 0),
			COALESCE(SUM(interaction_count), 0),
			COALESCE(SUM(submitted_count), 0),
			COALESCE(SUM(dismissed_count), 0)
		FROM widget_funnel_daily
		WHERE tenant_id = $1
		  AND event_date BETWEEN $2::date AND $3::date
		  AND ($4 = '' OR project_id = NULLIF($4, '')::uuid)
	`, tenantID, start, today, projectID).Scan(
		&summary.Matched, &summary.Impressions, &summary.Interactions,
		&summary.Submissions, &summary.Dismissals,
	)
	if err != nil {
		return Summary{}, fmt.Errorf("summarize widget funnel analytics: %w", err)
	}
	summary.CalculateRates()
	return summary, nil
}
