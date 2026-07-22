package widgetanalytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type Store struct{}

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
			result.DuplicateEventIDs = append(result.DuplicateEventIDs, event.EventID)
			continue
		}
		if err != nil {
			return IngestResult{}, fmt.Errorf("insert widget funnel event: %w", err)
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
			return IngestResult{}, fmt.Errorf("increment widget funnel daily aggregate: %w", err)
		}
		result.AcceptedEventIDs = append(result.AcceptedEventIDs, event.EventID)
	}
	return result, nil
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
