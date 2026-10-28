package retentionhealth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
)

type Store struct {
	Database db.Querier
}

func (store Store) RecordRun(ctx context.Context, run Run) error {
	if store.Database == nil {
		return errors.New("retention health database is unavailable")
	}
	if err := ValidateRun(run); err != nil {
		return err
	}
	failureCode := any(nil)
	if run.Result == ResultFailed {
		failureCode = "cleanup_failed"
	}
	tag, err := store.Database.Exec(ctx, `
		UPDATE retention_maintenance_status
		SET last_attempt_at = $2,
		    last_success_at = CASE
		        WHEN $3::text = 'succeeded' THEN $2
		        ELSE last_success_at
		    END,
		    last_result = $3,
		    last_processed = $4,
		    last_budget_exhausted = $5,
		    consecutive_failures = CASE
		        WHEN $3::text = 'succeeded' THEN 0
		        ELSE consecutive_failures + 1
		    END,
		    total_runs = total_runs + 1,
		    total_failures = total_failures + CASE
		        WHEN $3::text = 'failed' THEN 1
		        ELSE 0
		    END,
		    total_processed = total_processed + $4,
		    failure_code = $6,
		    updated_at = $2
		WHERE operation_key = $1
		  AND (last_attempt_at IS NULL OR last_attempt_at <= $2)
	`, run.Operation, run.AttemptedAt.UTC(), run.Result, run.Processed,
		run.BudgetExhausted, failureCode)
	if err != nil {
		return fmt.Errorf("record retention operation %q: %w", run.Operation, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("retention operation %q is unknown or newer evidence exists", run.Operation)
	}
	return nil
}

func Read(ctx context.Context, query db.Querier, now time.Time) (Report, error) {
	if query == nil {
		return Report{}, errors.New("retention health database is unavailable")
	}
	rows, err := query.Query(ctx, `
		SELECT operation_key, category, registered_at,
		       expected_interval_seconds, alert_grace_seconds,
		       last_attempt_at, last_success_at, last_result,
		       last_processed, last_budget_exhausted, consecutive_failures,
		       total_runs, total_failures, total_processed,
		       COALESCE(failure_code, '')
		FROM retention_maintenance_status
		ORDER BY operation_key
	`)
	if err != nil {
		return Report{}, fmt.Errorf("list retention operation health: %w", err)
	}
	defer rows.Close()

	records := make([]operationRecord, 0, len(operationCategories))
	for rows.Next() {
		var record operationRecord
		if err := rows.Scan(
			&record.Operation, &record.Category, &record.RegisteredAt,
			&record.ExpectedIntervalSec, &record.AlertGraceSec,
			&record.LastAttemptAt, &record.LastSuccessAt, &record.LastResult,
			&record.LastProcessed, &record.BudgetExhausted, &record.ConsecutiveFailures,
			&record.TotalRuns, &record.TotalFailures, &record.TotalProcessed,
			&record.FailureCode,
		); err != nil {
			return Report{}, fmt.Errorf("scan retention operation health: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return Report{}, fmt.Errorf("iterate retention operation health: %w", err)
	}
	if len(records) != len(operationCategories) {
		return Report{}, fmt.Errorf(
			"retention operation health registry has %d rows, want %d",
			len(records), len(operationCategories),
		)
	}
	report, err := buildReport(records, now)
	if err != nil {
		return Report{}, err
	}
	return report, nil
}
