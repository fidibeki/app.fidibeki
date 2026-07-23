package abuse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

type Category string

const (
	CategoryRateLimit    Category = "rate_limit"
	CategorySpam         Category = "spam"
	CategoryPlanQuota    Category = "plan_quota"
	CategoryUploadSafety Category = "upload_safety"
	CategoryAIBudget     Category = "ai_budget"
	CategoryProvider     Category = "provider"
)

type DiagnosticEvent struct {
	TenantID        string
	ProjectID       string
	Category        Category
	Reason          string
	Count           int64
	FirstOccurredAt time.Time
	OccurredAt      time.Time
}

func (event DiagnosticEvent) validate() error {
	if strings.TrimSpace(event.TenantID) == "" || strings.TrimSpace(event.ProjectID) == "" {
		return errors.New("abuse diagnostic tenant and project are required")
	}
	switch event.Category {
	case CategoryRateLimit, CategorySpam, CategoryPlanQuota, CategoryUploadSafety,
		CategoryAIBudget, CategoryProvider:
	default:
		return errors.New("abuse diagnostic category is invalid")
	}
	reason := strings.TrimSpace(event.Reason)
	if reason == "" || len(reason) > 64 {
		return errors.New("abuse diagnostic reason must be between 1 and 64 characters")
	}
	if event.Count < 0 {
		return errors.New("abuse diagnostic count must not be negative")
	}
	if event.OccurredAt.IsZero() {
		return errors.New("abuse diagnostic occurrence time is required")
	}
	if !event.FirstOccurredAt.IsZero() && event.FirstOccurredAt.After(event.OccurredAt) {
		return errors.New("abuse diagnostic first occurrence must not follow the last occurrence")
	}
	return nil
}

type Store struct{}

type Recorder struct {
	Database db.Querier
	Store    Store
}

func (recorder Recorder) Record(ctx context.Context, event DiagnosticEvent) error {
	if recorder.Database == nil {
		return errors.New("abuse diagnostic database is not configured")
	}
	return recorder.Store.Record(ctx, recorder.Database, event)
}

func (Store) Record(
	ctx context.Context,
	query db.Querier,
	event DiagnosticEvent,
) error {
	if err := event.validate(); err != nil {
		return err
	}
	count := event.Count
	if count == 0 {
		count = 1
	}
	occurredAt := event.OccurredAt.UTC()
	firstOccurredAt := occurredAt
	if !event.FirstOccurredAt.IsZero() {
		firstOccurredAt = event.FirstOccurredAt.UTC()
	}
	bucketStart := occurredAt.Truncate(time.Hour)
	if _, err := query.Exec(ctx, `
		INSERT INTO abuse_diagnostics_hourly (
			tenant_id, project_id, bucket_start, category, reason, event_count,
			first_occurred_at, last_occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (project_id, bucket_start, category, reason) DO UPDATE SET
			event_count = abuse_diagnostics_hourly.event_count + EXCLUDED.event_count,
			first_occurred_at = LEAST(
				abuse_diagnostics_hourly.first_occurred_at,
				EXCLUDED.first_occurred_at
			),
			last_occurred_at = GREATEST(
				abuse_diagnostics_hourly.last_occurred_at,
				EXCLUDED.last_occurred_at
			)
	`, event.TenantID, event.ProjectID, bucketStart, event.Category, strings.TrimSpace(event.Reason),
		count, firstOccurredAt, occurredAt,
	); err != nil {
		return fmt.Errorf("record abuse diagnostic: %w", err)
	}
	return nil
}

type DiagnosticPoint struct {
	ProjectID   string    `json:"project_id"`
	BucketStart time.Time `json:"bucket_start"`
	Category    Category  `json:"category"`
	Reason      string    `json:"reason"`
	Count       int64     `json:"count"`
}

func (Store) List(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	start time.Time,
	end time.Time,
) ([]DiagnosticPoint, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("abuse diagnostics tenant is required")
	}
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return nil, errors.New("abuse diagnostics range is invalid")
	}
	rows, err := query.Query(ctx, `
		SELECT project_id, bucket_start, category::text, reason, event_count
		FROM abuse_diagnostics_hourly
		WHERE tenant_id = $1
		  AND ($2 = '' OR project_id = NULLIF($2, '')::uuid)
		  AND bucket_start >= $3
		  AND bucket_start < $4
		ORDER BY bucket_start ASC, project_id ASC, category ASC, reason ASC
		LIMIT 10000
	`, tenantID, projectID, start.UTC(), end.UTC())
	if err != nil {
		return nil, fmt.Errorf("list abuse diagnostics: %w", err)
	}
	defer rows.Close()
	points := make([]DiagnosticPoint, 0)
	for rows.Next() {
		var point DiagnosticPoint
		if err := rows.Scan(
			&point.ProjectID, &point.BucketStart, &point.Category, &point.Reason, &point.Count,
		); err != nil {
			return nil, fmt.Errorf("scan abuse diagnostic: %w", err)
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate abuse diagnostics: %w", err)
	}
	return points, nil
}

func (Store) DeleteExpired(
	ctx context.Context,
	query db.Querier,
	before time.Time,
	limit int,
) (int64, error) {
	if limit < 1 || limit > 10000 {
		return 0, errors.New("abuse diagnostics cleanup limit must be between 1 and 10000")
	}
	tag, err := query.Exec(ctx, `
		WITH candidates AS (
			SELECT project_id, bucket_start, category, reason
			FROM abuse_diagnostics_hourly
			WHERE bucket_start < $1
			ORDER BY bucket_start, project_id, category, reason
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM abuse_diagnostics_hourly AS diagnostic
		USING candidates
		WHERE diagnostic.project_id = candidates.project_id
		  AND diagnostic.bucket_start = candidates.bucket_start
		  AND diagnostic.category = candidates.category
		  AND diagnostic.reason = candidates.reason
	`, before.UTC(), limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired abuse diagnostics: %w", err)
	}
	return tag.RowsAffected(), nil
}
