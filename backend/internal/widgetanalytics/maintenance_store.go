package widgetanalytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	analyticsMaintenanceAdvisoryLock    int64 = 0x4649444942454B49
	analyticsReconciliationChunkTimeout       = 5 * time.Second
	analyticsReconciliationCadence            = time.Hour
)

type MaintenanceStore struct {
	Database     *pgxpool.Pool
	ChunkTimeout time.Duration
	Cadence      time.Duration
}

type reconciliationProjectDay struct {
	ProjectID string
	Date      time.Time
}

func (store MaintenanceStore) ReconcileRecent(
	ctx context.Context,
	now time.Time,
	days int,
) (result ReconcileResult, resultErr error) {
	if days <= 0 {
		days = analyticsReconciliationDays
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -(days - 1))
	connection, err := store.Database.Acquire(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("acquire widget analytics maintenance connection: %w", err)
	}
	acquired := false
	defer func() {
		if !acquired {
			connection.Release()
			return
		}
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := connection.QueryRow(
			unlockCtx, `SELECT pg_advisory_unlock($1)`, analyticsMaintenanceAdvisoryLock,
		).Scan(&unlocked); err != nil || !unlocked {
			if err == nil {
				err = errors.New("maintenance advisory lock was not held")
			}
			resultErr = errors.Join(resultErr, fmt.Errorf("release widget analytics maintenance lease: %w", err))
			_ = connection.Conn().Close(unlockCtx)
		}
		connection.Release()
	}()
	if err := connection.QueryRow(
		ctx, `SELECT pg_try_advisory_lock($1)`, analyticsMaintenanceAdvisoryLock,
	).Scan(&acquired); err != nil {
		return ReconcileResult{}, fmt.Errorf("acquire widget analytics maintenance lease: %w", err)
	}
	if !acquired {
		return ReconcileResult{Skipped: true, SkipReason: "lease_unavailable"}, nil
	}
	cadence := store.Cadence
	if cadence == 0 {
		cadence = analyticsReconciliationCadence
	}
	if cadence > 0 {
		var lastReconciledAt *time.Time
		if err := connection.QueryRow(ctx, `
			SELECT last_reconciled_at
			FROM widget_funnel_maintenance
			WHERE singleton = true
		`).Scan(&lastReconciledAt); err != nil {
			return ReconcileResult{}, fmt.Errorf("read widget analytics reconciliation cadence: %w", err)
		}
		if lastReconciledAt != nil && now.UTC().Before(lastReconciledAt.UTC().Add(cadence)) {
			return ReconcileResult{Skipped: true, SkipReason: "cadence_not_due"}, nil
		}
	}

	projectDays, err := reconciliationProjectDays(ctx, connection, start, today)
	if err != nil {
		return ReconcileResult{}, err
	}
	chunkTimeout := store.ChunkTimeout
	if chunkTimeout <= 0 {
		chunkTimeout = analyticsReconciliationChunkTimeout
	}
	for _, projectDay := range projectDays {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		pageKeys, err := reconciliationPageKeys(ctx, connection, projectDay)
		if err != nil {
			return result, err
		}
		for _, pageKey := range pageKeys {
			chunkCtx, cancel := context.WithTimeout(ctx, chunkTimeout)
			chunkResult, err := reconcileProjectDayPage(chunkCtx, connection, projectDay, pageKey)
			cancel()
			if err != nil {
				return result, fmt.Errorf(
					"reconcile widget analytics after %d page chunks: %w", result.Chunks, err,
				)
			}
			result.Upserted += chunkResult.Upserted
			result.Removed += chunkResult.Removed
			result.Chunks++
		}
	}
	if _, err := connection.Exec(ctx, `
		UPDATE widget_funnel_maintenance
		SET last_reconciled_at = $1
		WHERE singleton = true
	`, now.UTC()); err != nil {
		return result, fmt.Errorf("record widget analytics reconciliation cadence: %w", err)
	}
	return result, nil
}

func reconciliationProjectDays(
	ctx context.Context,
	connection *pgxpool.Conn,
	start time.Time,
	today time.Time,
) ([]reconciliationProjectDay, error) {
	rows, err := connection.Query(ctx, `
		SELECT project.id, day::date
		FROM projects AS project
		CROSS JOIN generate_series($1::date, $2::date, interval '1 day') AS day
		ORDER BY project.id, day::date
	`, start.Format(time.DateOnly), today.Format(time.DateOnly))
	if err != nil {
		return nil, fmt.Errorf("list widget analytics reconciliation project days: %w", err)
	}
	defer rows.Close()
	projectDays := make([]reconciliationProjectDay, 0)
	for rows.Next() {
		var projectDay reconciliationProjectDay
		if err := rows.Scan(&projectDay.ProjectID, &projectDay.Date); err != nil {
			return nil, fmt.Errorf("scan widget analytics reconciliation project day: %w", err)
		}
		projectDay.Date = projectDay.Date.UTC()
		projectDays = append(projectDays, projectDay)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate widget analytics reconciliation project days: %w", err)
	}
	return projectDays, nil
}

func reconciliationPageKeys(
	ctx context.Context,
	connection *pgxpool.Conn,
	projectDay reconciliationProjectDay,
) ([][]byte, error) {
	dayStart := time.Date(
		projectDay.Date.Year(), projectDay.Date.Month(), projectDay.Date.Day(), 0, 0, 0, 0, time.UTC,
	)
	dayEnd := dayStart.AddDate(0, 0, 1)
	rows, err := connection.Query(ctx, `
		SELECT page_key
		FROM (
			SELECT page_key
			FROM widget_funnel_events
			WHERE project_id = $1
			  AND occurred_at >= $2::timestamptz AND occurred_at < $3::timestamptz
			UNION
			SELECT page_key
			FROM widget_funnel_daily
			WHERE project_id = $1 AND event_date = $4::date
		) AS candidates
		ORDER BY page_key
	`, projectDay.ProjectID, dayStart, dayEnd, dayStart.Format(time.DateOnly))
	if err != nil {
		return nil, fmt.Errorf("list widget analytics reconciliation pages: %w", err)
	}
	defer rows.Close()
	pageKeys := make([][]byte, 0)
	for rows.Next() {
		var pageKey []byte
		if err := rows.Scan(&pageKey); err != nil {
			return nil, fmt.Errorf("scan widget analytics reconciliation page: %w", err)
		}
		pageKeys = append(pageKeys, append([]byte(nil), pageKey...))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate widget analytics reconciliation pages: %w", err)
	}
	return pageKeys, nil
}

func reconcileProjectDayPage(
	ctx context.Context,
	connection *pgxpool.Conn,
	projectDay reconciliationProjectDay,
	pageKey []byte,
) (ReconcileResult, error) {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("begin widget analytics reconciliation chunk: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := transaction.Rollback(rollbackCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			_ = connection.Conn().Close(rollbackCtx)
		}
	}()
	var projectID string
	if err := transaction.QueryRow(ctx, `
		SELECT id FROM projects WHERE id = $1 FOR UPDATE
	`, projectDay.ProjectID).Scan(&projectID); errors.Is(err, pgx.ErrNoRows) {
		return ReconcileResult{}, nil
	} else if err != nil {
		return ReconcileResult{}, fmt.Errorf("lock widget analytics reconciliation project: %w", err)
	}
	dayStart := time.Date(
		projectDay.Date.Year(), projectDay.Date.Month(), projectDay.Date.Day(), 0, 0, 0, 0, time.UTC,
	)
	dayEnd := dayStart.AddDate(0, 0, 1)
	var result ReconcileResult
	if err := transaction.QueryRow(ctx, `
		WITH raw_scope AS MATERIALIZED (
			SELECT event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			       page_url, page_key, received_at
			FROM widget_funnel_events
			WHERE project_id = $1
			  AND occurred_at >= $2::timestamptz AND occurred_at < $3::timestamptz
			  AND page_key = $4
		),
		latest_labels AS (
			SELECT DISTINCT ON (tenant_id, project_id, rule_id, page_key)
			       tenant_id, project_id, rule_id, page_key, rule_name, page_url
			FROM raw_scope
			ORDER BY tenant_id, project_id, rule_id, page_key, received_at DESC, event_id DESC
		),
		raw_totals AS (
			SELECT tenant_id, project_id, rule_id, page_key,
			       COUNT(*) FILTER (WHERE event_type = 'matched')::bigint AS matched_count,
			       COUNT(*) FILTER (WHERE event_type = 'impression')::bigint AS impression_count,
			       COUNT(*) FILTER (WHERE event_type = 'interaction')::bigint AS interaction_count,
			       COUNT(*) FILTER (WHERE event_type = 'submitted')::bigint AS submitted_count,
			       COUNT(*) FILTER (WHERE event_type = 'dismissed')::bigint AS dismissed_count
			FROM raw_scope
			GROUP BY tenant_id, project_id, rule_id, page_key
		),
		reconciled AS (
			INSERT INTO widget_funnel_daily (
				tenant_id, project_id, rule_id, rule_name, page_url, page_key, event_date,
				matched_count, impression_count, interaction_count, submitted_count, dismissed_count
			)
			SELECT totals.tenant_id, totals.project_id, totals.rule_id,
			       labels.rule_name, labels.page_url, totals.page_key, $5::date,
			       totals.matched_count, totals.impression_count, totals.interaction_count,
			       totals.submitted_count, totals.dismissed_count
			FROM raw_totals AS totals
			JOIN latest_labels AS labels USING (tenant_id, project_id, rule_id, page_key)
			ON CONFLICT (project_id, rule_id, event_date, page_key) DO UPDATE SET
				rule_name = EXCLUDED.rule_name,
				page_url = EXCLUDED.page_url,
				matched_count = EXCLUDED.matched_count,
				impression_count = EXCLUDED.impression_count,
				interaction_count = EXCLUDED.interaction_count,
				submitted_count = EXCLUDED.submitted_count,
				dismissed_count = EXCLUDED.dismissed_count,
				updated_at = now()
			RETURNING 1
		),
		removed AS (
			DELETE FROM widget_funnel_daily AS daily
			WHERE daily.project_id = $1 AND daily.page_key = $4 AND daily.event_date = $5::date
			  AND NOT EXISTS (
				SELECT 1 FROM raw_totals AS totals WHERE totals.rule_id = daily.rule_id
			  )
			RETURNING 1
		)
		SELECT (SELECT count(*) FROM reconciled), (SELECT count(*) FROM removed)
	`, projectDay.ProjectID, dayStart, dayEnd, pageKey, dayStart.Format(time.DateOnly)).Scan(
		&result.Upserted, &result.Removed,
	); err != nil {
		return ReconcileResult{}, fmt.Errorf("reconcile widget analytics project day page: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return ReconcileResult{}, fmt.Errorf("commit widget analytics reconciliation chunk: %w", err)
	}
	return result, nil
}

func (store MaintenanceStore) DeleteExpiredEvents(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int64, error) {
	if limit <= 0 {
		limit = analyticsMaintenanceBatch
	}
	var deleted int64
	if err := store.Database.QueryRow(ctx, `
		WITH candidates AS (
			SELECT event_id
			FROM widget_funnel_events
			WHERE received_at < $1
			ORDER BY received_at, event_id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		),
		removed AS (
			DELETE FROM widget_funnel_events AS event
			USING candidates
			WHERE event.event_id = candidates.event_id
			RETURNING 1
		)
		SELECT count(*) FROM removed
	`, cutoff.UTC(), limit).Scan(&deleted); err != nil {
		return 0, fmt.Errorf("delete expired widget analytics events: %w", err)
	}
	return deleted, nil
}

func (store MaintenanceStore) DeleteExpiredDaily(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int64, error) {
	if limit <= 0 {
		limit = analyticsMaintenanceBatch
	}
	var deleted int64
	if err := store.Database.QueryRow(ctx, `
		WITH candidates AS (
			SELECT project_id, rule_id, event_date, page_key
			FROM widget_funnel_daily
			WHERE event_date < $1::date
			ORDER BY event_date, project_id, rule_id, page_key
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		),
		removed AS (
			DELETE FROM widget_funnel_daily AS daily
			USING candidates
			WHERE daily.project_id = candidates.project_id
			  AND daily.rule_id = candidates.rule_id
			  AND daily.event_date = candidates.event_date
			  AND daily.page_key = candidates.page_key
			RETURNING 1
		)
		SELECT count(*) FROM removed
	`, cutoff.UTC().Format(time.DateOnly), limit).Scan(&deleted); err != nil {
		return 0, fmt.Errorf("delete expired widget analytics daily aggregates: %w", err)
	}
	return deleted, nil
}

var _ MaintenanceOperations = MaintenanceStore{}
