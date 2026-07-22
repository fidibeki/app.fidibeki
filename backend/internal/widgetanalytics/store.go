package widgetanalytics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

type Store struct{}

var (
	errEventIDConflict         = errors.New("widget funnel event ID conflicts with another event")
	ErrAnalyticsFilterNotFound = errors.New("analytics filter not found")
)

const (
	maximumAnalyticsBreakdownRows = 100
	maximumAnalyticsOptionRows    = 200
)

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
	if err := lockAnalyticsProject(ctx, query, tenantID, projectID); err != nil {
		return IngestResult{}, err
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
	if err := lockAnalyticsProject(ctx, query, event.TenantID, event.ProjectID); err != nil {
		return false, err
	}
	return insertEvent(ctx, query, event, false)
}

func lockAnalyticsProject(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
) error {
	var lockedProjectID string
	if err := query.QueryRow(ctx, `
		SELECT id FROM projects
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, projectID).Scan(&lockedProjectID); err != nil {
		return fmt.Errorf("lock widget funnel project: %w", err)
	}
	return nil
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

func (Store) Workspace(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	filters AnalyticsQuery,
) (Workspace, error) {
	if filters.PageURL != "" && len(filters.PageKey) == 0 {
		pageKey := sha256.Sum256([]byte(filters.PageURL))
		filters.PageKey = pageKey[:]
	}
	today := time.Date(filters.Now.UTC().Year(), filters.Now.UTC().Month(), filters.Now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -(filters.RangeDays - 1))
	workspace := Workspace{
		Range: filters.Range,
		Period: Period{
			StartDate: start.Format(time.DateOnly),
			EndDate:   today.Format(time.DateOnly),
			Timezone:  "UTC",
		},
		Filters: AppliedFilters{
			ProjectID: filters.ProjectID,
			RuleID:    filters.RuleID,
			PageURL:   filters.PageURL,
		},
		Trend:    make([]TrendPoint, 0, filters.RangeDays),
		Projects: make([]Breakdown, 0),
		Rules:    make([]Breakdown, 0),
		Pages:    make([]Breakdown, 0),
		Options: AnalyticsOptions{
			Projects: make([]ProjectOption, 0),
			Rules:    make([]RuleOption, 0),
			Pages:    make([]PageOption, 0),
		},
		Metadata: newAnalyticsMetadata(),
	}
	rows, err := query.Query(ctx, `
		WITH project_option_totals AS (
			SELECT id, name FROM projects WHERE tenant_id = $1
		),
		current_rule_options AS (
			SELECT rule.id, project.id AS project_id, rule.name, false AS deleted,
			       2 AS current_rank, NULL::date AS event_date
			FROM widget_rules AS rule
			JOIN projects AS project ON project.id = rule.project_id
			WHERE project.tenant_id = $1
		),
		historical_rule_options AS (
			SELECT DISTINCT ON (daily.rule_id)
			       daily.rule_id AS id, daily.project_id, daily.rule_name AS name, true AS deleted,
			       1 AS current_rank, daily.event_date
			FROM widget_funnel_daily AS daily
			JOIN projects AS project
			  ON project.tenant_id = daily.tenant_id AND project.id = daily.project_id
			WHERE daily.tenant_id = $1
			  AND daily.event_date BETWEEN $2::date AND $3::date
			ORDER BY daily.rule_id, daily.event_date DESC, daily.rule_name
		),
		selected_historical_rule_option AS (
			SELECT daily.rule_id AS id, daily.project_id, daily.rule_name AS name, true AS deleted,
			       0 AS current_rank, daily.event_date
			FROM widget_funnel_daily AS daily
			JOIN projects AS project
			  ON project.tenant_id = daily.tenant_id AND project.id = daily.project_id
			WHERE $5::text <> ''
			  AND daily.tenant_id = $1
			  AND daily.rule_id = NULLIF($5, '')::uuid
			  AND ($4::text = '' OR daily.project_id = NULLIF($4, '')::uuid)
			  AND NOT EXISTS (SELECT 1 FROM current_rule_options WHERE id = daily.rule_id)
			ORDER BY daily.event_date DESC, daily.rule_name
			LIMIT 1
		),
		rule_option_source AS (
			SELECT * FROM current_rule_options
			UNION ALL SELECT * FROM historical_rule_options
			UNION ALL SELECT * FROM selected_historical_rule_option
		),
		rule_option_totals AS (
			SELECT DISTINCT ON (id) id, project_id, name, deleted
			FROM rule_option_source
			ORDER BY id, current_rank DESC, event_date DESC NULLS FIRST
		),
		selected_page_history AS (
			SELECT daily.page_key, daily.page_url
			FROM widget_funnel_daily AS daily
			JOIN projects AS project
			  ON project.tenant_id = daily.tenant_id AND project.id = daily.project_id
			WHERE $6::text <> ''
			  AND daily.tenant_id = $1
			  AND daily.page_key = $7
			  AND daily.page_url = $6
			  AND ($4::text = '' OR daily.project_id = NULLIF($4, '')::uuid)
			  AND ($5::text = '' OR daily.rule_id = NULLIF($5, '')::uuid)
			LIMIT 1
		),
		filter_status AS (
			SELECT
				($4::text = '' OR EXISTS (
					SELECT 1 FROM project_option_totals WHERE id = NULLIF($4, '')::uuid
				)) AS project_valid,
				($5::text = '' OR EXISTS (
					SELECT 1 FROM rule_option_totals
					WHERE id = NULLIF($5, '')::uuid
					  AND ($4::text = '' OR project_id = NULLIF($4, '')::uuid)
				)) AS rule_valid,
				($6::text = '' OR EXISTS (
					SELECT 1 FROM selected_page_history
				)) AS page_valid
		),
		base_scope AS (
			SELECT daily.*, project.name AS project_name
			FROM widget_funnel_daily AS daily
			CROSS JOIN filter_status
			JOIN projects AS project
			  ON project.tenant_id = daily.tenant_id AND project.id = daily.project_id
			WHERE filter_status.project_valid AND filter_status.rule_valid AND filter_status.page_valid
			  AND daily.tenant_id = $1
			  AND daily.event_date BETWEEN $2::date AND $3::date
			  AND ($4::text = '' OR daily.project_id = NULLIF($4, '')::uuid)
			  AND ($5::text = '' OR daily.rule_id = NULLIF($5, '')::uuid)
		),
		metric_scope AS (
			SELECT * FROM base_scope
			WHERE $6::text = '' OR (page_key = $7 AND page_url = $6)
		),
		summary AS (
			SELECT
				COALESCE(SUM(matched_count), 0)::bigint AS matched_count,
				COALESCE(SUM(impression_count), 0)::bigint AS impression_count,
				COALESCE(SUM(interaction_count), 0)::bigint AS interaction_count,
				COALESCE(SUM(submitted_count), 0)::bigint AS submitted_count,
				COALESCE(SUM(dismissed_count), 0)::bigint AS dismissed_count
			FROM metric_scope
		),
		daily_trend AS (
			SELECT day::date AS event_date,
			       COALESCE(SUM(metric_scope.impression_count), 0)::bigint AS impression_count,
			       COALESCE(SUM(metric_scope.interaction_count), 0)::bigint AS interaction_count,
			       COALESCE(SUM(metric_scope.submitted_count), 0)::bigint AS submitted_count
			FROM generate_series($2::date, $3::date, interval '1 day') AS day
			LEFT JOIN metric_scope ON metric_scope.event_date = day::date
			GROUP BY day::date
		),
		project_breakdown_totals AS (
			SELECT project_id, project_name,
			       SUM(matched_count)::bigint AS matched_count,
			       SUM(impression_count)::bigint AS impression_count,
			       SUM(interaction_count)::bigint AS interaction_count,
			       SUM(submitted_count)::bigint AS submitted_count,
			       SUM(dismissed_count)::bigint AS dismissed_count
			FROM metric_scope GROUP BY project_id, project_name
		),
		project_breakdowns AS (
			SELECT * FROM project_breakdown_totals
			ORDER BY impression_count DESC, submitted_count DESC, lower(project_name), project_id
			LIMIT $8
		),
		rule_latest_names AS (
			SELECT DISTINCT ON (scope.project_id, scope.rule_id)
			       scope.project_id, scope.rule_id,
			       COALESCE(rule.name, scope.rule_name) AS rule_name,
			       rule.id IS NULL AS deleted
			FROM metric_scope AS scope
			LEFT JOIN widget_rules AS rule ON rule.id = scope.rule_id
			ORDER BY scope.project_id, scope.rule_id, scope.event_date DESC, scope.rule_name
		),
		rule_count_totals AS (
			SELECT project_id, project_name, rule_id,
			       SUM(matched_count)::bigint AS matched_count,
			       SUM(impression_count)::bigint AS impression_count,
			       SUM(interaction_count)::bigint AS interaction_count,
			       SUM(submitted_count)::bigint AS submitted_count,
			       SUM(dismissed_count)::bigint AS dismissed_count
			FROM metric_scope GROUP BY project_id, project_name, rule_id
		),
		rule_breakdown_totals AS (
			SELECT counts.project_id, counts.project_name, counts.rule_id,
			       names.rule_name, names.deleted,
			       counts.matched_count, counts.impression_count, counts.interaction_count,
			       counts.submitted_count, counts.dismissed_count
			FROM rule_count_totals AS counts
			JOIN rule_latest_names AS names
			  ON names.project_id = counts.project_id AND names.rule_id = counts.rule_id
		),
		rule_breakdowns AS (
			SELECT * FROM rule_breakdown_totals
			ORDER BY impression_count DESC, submitted_count DESC, lower(rule_name), rule_id
			LIMIT $8
		),
		page_totals AS (
			SELECT page_key, page_url,
			       SUM(matched_count)::bigint AS matched_count,
			       SUM(impression_count)::bigint AS impression_count,
			       SUM(interaction_count)::bigint AS interaction_count,
			       SUM(submitted_count)::bigint AS submitted_count,
			       SUM(dismissed_count)::bigint AS dismissed_count
			FROM base_scope GROUP BY page_key, page_url
		),
		page_breakdown_totals AS (
			SELECT * FROM page_totals
			WHERE $6::text = '' OR (page_key = $7 AND page_url = $6)
		),
		page_breakdowns AS (
			SELECT * FROM page_breakdown_totals
			ORDER BY impression_count DESC, submitted_count DESC, page_url, page_key
			LIMIT $8
		),
		project_options AS (
			SELECT * FROM project_option_totals
			ORDER BY (id = NULLIF($4, '')::uuid) DESC, lower(name), id
			LIMIT $9
		),
		rule_options AS (
			SELECT * FROM rule_option_totals
			WHERE ($4::text = '' OR project_id = NULLIF($4, '')::uuid)
			ORDER BY (id = NULLIF($5, '')::uuid) DESC, lower(name), id
			LIMIT $9
		),
		page_option_totals AS (
			SELECT page_key, page_url, impression_count, submitted_count
			FROM page_totals
		),
		page_option_candidates AS (
			SELECT * FROM page_option_totals
			UNION ALL
			SELECT $7::bytea, $6::text, 0::bigint, 0::bigint
			WHERE $6::text <> '' AND EXISTS (SELECT 1 FROM selected_page_history) AND NOT EXISTS (
				SELECT 1 FROM page_option_totals WHERE page_key = $7 AND page_url = $6
			)
		),
		page_options AS (
			SELECT * FROM page_option_candidates
			ORDER BY (page_url = $6) DESC, impression_count DESC, submitted_count DESC, page_url
			LIMIT $9
		),
		result_rows AS (
			SELECT 'summary'::text AS kind, ''::text AS identity, ''::text AS project_id,
			       ''::text AS name, ''::text AS project_name, false AS deleted,
			       ''::text AS event_date, ''::text AS page_url,
			       matched_count, impression_count, interaction_count, submitted_count, dismissed_count
			FROM summary
			UNION ALL
			SELECT 'trend', '', '', '', '', false, to_char(event_date, 'YYYY-MM-DD'), '',
			       0, impression_count, interaction_count, submitted_count, 0
			FROM daily_trend
			UNION ALL
			SELECT 'project', project_id::text, project_id::text, project_name, '', false, '', '',
			       matched_count, impression_count, interaction_count, submitted_count, dismissed_count
			FROM project_breakdowns
			UNION ALL
			SELECT 'rule', rule_id::text, project_id::text, rule_name, project_name, deleted, '', '',
			       matched_count, impression_count, interaction_count, submitted_count, dismissed_count
			FROM rule_breakdowns
			UNION ALL
			SELECT 'page', page_url, '', '', '', false, '', page_url,
			       matched_count, impression_count, interaction_count, submitted_count, dismissed_count
			FROM page_breakdowns
			UNION ALL
			SELECT 'project_option', id::text, id::text, name, '', false, '', '', 0, 0, 0, 0, 0
			FROM project_options
			UNION ALL
			SELECT 'rule_option', id::text, project_id::text, name, '', deleted, '', '', 0, 0, 0, 0, 0
			FROM rule_options
			UNION ALL
			SELECT 'page_option', page_url, '', '', '', false, '', page_url, 0, 0, 0, 0, 0
			FROM page_options
			UNION ALL
			SELECT 'meta', 'projects', '', '', '', false, '', '', COUNT(*)::bigint, 0, 0, 0, 0
			FROM project_breakdown_totals
			UNION ALL
			SELECT 'meta', 'rules', '', '', '', false, '', '', COUNT(*)::bigint, 0, 0, 0, 0
			FROM rule_breakdown_totals
			UNION ALL
			SELECT 'meta', 'pages', '', '', '', false, '', '', COUNT(*)::bigint, 0, 0, 0, 0
			FROM page_breakdown_totals
			UNION ALL
			SELECT 'meta', 'project_options', '', '', '', false, '', '', COUNT(*)::bigint, 0, 0, 0, 0
			FROM project_option_totals
			UNION ALL
			SELECT 'meta', 'rule_options', '', '', '', false, '', '', COUNT(*)::bigint, 0, 0, 0, 0
			FROM rule_option_totals
			WHERE ($4::text = '' OR project_id = NULLIF($4, '')::uuid)
			UNION ALL
			SELECT 'meta', 'page_options', '', '', '', false, '', '', COUNT(*)::bigint, 0, 0, 0, 0
			FROM page_option_candidates
		)
		SELECT result_rows.*, filter_status.project_valid, filter_status.rule_valid, filter_status.page_valid
		FROM result_rows CROSS JOIN filter_status
	`, tenantID, start, today, filters.ProjectID, filters.RuleID, filters.PageURL,
		filters.PageKey, maximumAnalyticsBreakdownRows, maximumAnalyticsOptionRows)
	if err != nil {
		return Workspace{}, fmt.Errorf("query widget analytics workspace: %w", err)
	}
	defer rows.Close()
	projectValid, ruleValid, pageValid := true, true, true
	for rows.Next() {
		var kind, identity, projectID, name, projectName, eventDate, pageURL string
		var deleted bool
		var counts Summary
		if err := rows.Scan(
			&kind, &identity, &projectID, &name, &projectName, &deleted, &eventDate, &pageURL,
			&counts.Matched, &counts.Impressions, &counts.Interactions, &counts.Submissions,
			&counts.Dismissals, &projectValid, &ruleValid, &pageValid,
		); err != nil {
			return Workspace{}, fmt.Errorf("scan widget analytics workspace: %w", err)
		}
		switch kind {
		case "summary":
			workspace.Summary = counts
		case "trend":
			workspace.Trend = append(workspace.Trend, TrendPoint{
				Date: eventDate, Impressions: counts.Impressions,
				Interactions: counts.Interactions, Submissions: counts.Submissions,
			})
		case "project":
			workspace.Projects = append(workspace.Projects, Breakdown{
				ProjectID: identity, ProjectName: name, Summary: counts,
			})
		case "rule":
			workspace.Rules = append(workspace.Rules, Breakdown{
				ProjectID: projectID, ProjectName: projectName, RuleID: identity,
				RuleName: name, Deleted: deleted, Summary: counts,
			})
		case "page":
			workspace.Pages = append(workspace.Pages, Breakdown{PageURL: pageURL, Summary: counts})
		case "project_option":
			workspace.Options.Projects = append(workspace.Options.Projects, ProjectOption{ID: identity, Name: name})
		case "rule_option":
			workspace.Options.Rules = append(workspace.Options.Rules, RuleOption{
				ID: identity, ProjectID: projectID, Name: name, Deleted: deleted,
			})
		case "page_option":
			workspace.Options.Pages = append(workspace.Options.Pages, PageOption{URL: pageURL})
		case "meta":
			setAnalyticsMetadataTotal(&workspace.Metadata, identity, counts.Matched)
		}
	}
	if err := rows.Err(); err != nil {
		return Workspace{}, fmt.Errorf("iterate widget analytics workspace: %w", err)
	}
	if !projectValid || !ruleValid || !pageValid {
		return Workspace{}, ErrAnalyticsFilterNotFound
	}
	workspace.CalculateRates()
	sortWorkspace(&workspace)
	finalizeAnalyticsMetadata(&workspace)
	return workspace, nil
}

func newAnalyticsMetadata() AnalyticsMetadata {
	return AnalyticsMetadata{
		Projects:       ResultMetadata{Limit: maximumAnalyticsBreakdownRows},
		Rules:          ResultMetadata{Limit: maximumAnalyticsBreakdownRows},
		Pages:          ResultMetadata{Limit: maximumAnalyticsBreakdownRows},
		ProjectOptions: ResultMetadata{Limit: maximumAnalyticsOptionRows},
		RuleOptions:    ResultMetadata{Limit: maximumAnalyticsOptionRows},
		PageOptions:    ResultMetadata{Limit: maximumAnalyticsOptionRows},
	}
}

func setAnalyticsMetadataTotal(metadata *AnalyticsMetadata, dimension string, total int64) {
	switch dimension {
	case "projects":
		metadata.Projects.Total = total
	case "rules":
		metadata.Rules.Total = total
	case "pages":
		metadata.Pages.Total = total
	case "project_options":
		metadata.ProjectOptions.Total = total
	case "rule_options":
		metadata.RuleOptions.Total = total
	case "page_options":
		metadata.PageOptions.Total = total
	}
}

func finalizeAnalyticsMetadata(workspace *Workspace) {
	values := []struct {
		metadata *ResultMetadata
		returned int
	}{
		{&workspace.Metadata.Projects, len(workspace.Projects)},
		{&workspace.Metadata.Rules, len(workspace.Rules)},
		{&workspace.Metadata.Pages, len(workspace.Pages)},
		{&workspace.Metadata.ProjectOptions, len(workspace.Options.Projects)},
		{&workspace.Metadata.RuleOptions, len(workspace.Options.Rules)},
		{&workspace.Metadata.PageOptions, len(workspace.Options.Pages)},
	}
	for _, value := range values {
		value.metadata.Returned = value.returned
		value.metadata.Truncated = value.metadata.Total > int64(value.returned)
	}
}

func sortWorkspace(workspace *Workspace) {
	sortBreakdowns := func(items []Breakdown, label func(Breakdown) string) {
		sort.Slice(items, func(left, right int) bool {
			if items[left].Impressions != items[right].Impressions {
				return items[left].Impressions > items[right].Impressions
			}
			if items[left].Submissions != items[right].Submissions {
				return items[left].Submissions > items[right].Submissions
			}
			leftLabel, rightLabel := strings.ToLower(label(items[left])), strings.ToLower(label(items[right]))
			if leftLabel != rightLabel {
				return leftLabel < rightLabel
			}
			return breakdownIdentity(items[left]) < breakdownIdentity(items[right])
		})
	}
	sortBreakdowns(workspace.Projects, func(item Breakdown) string { return item.ProjectName })
	sortBreakdowns(workspace.Rules, func(item Breakdown) string { return item.RuleName })
	sortBreakdowns(workspace.Pages, func(item Breakdown) string { return item.PageURL })
	sort.Slice(workspace.Trend, func(left, right int) bool {
		return workspace.Trend[left].Date < workspace.Trend[right].Date
	})
	sort.Slice(workspace.Options.Projects, func(left, right int) bool {
		leftName := strings.ToLower(workspace.Options.Projects[left].Name)
		rightName := strings.ToLower(workspace.Options.Projects[right].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return workspace.Options.Projects[left].ID < workspace.Options.Projects[right].ID
	})
	sort.Slice(workspace.Options.Rules, func(left, right int) bool {
		leftName := strings.ToLower(workspace.Options.Rules[left].Name)
		rightName := strings.ToLower(workspace.Options.Rules[right].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return workspace.Options.Rules[left].ID < workspace.Options.Rules[right].ID
	})
	sort.Slice(workspace.Options.Pages, func(left, right int) bool {
		return workspace.Options.Pages[left].URL < workspace.Options.Pages[right].URL
	})
}

func breakdownIdentity(item Breakdown) string {
	return item.ProjectID + item.RuleID + item.PageURL
}
