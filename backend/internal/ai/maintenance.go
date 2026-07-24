package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

const (
	budgetRetentionDays = 90
	budgetCleanupBatch  = 1000
	budgetCleanupLimit  = 100
)

type BudgetCleanupStore interface {
	DeleteExpiredBudgetData(context.Context, time.Time, int) (int64, error)
}

type BudgetMaintenanceStore struct {
	Database db.Querier
}

func (store BudgetMaintenanceStore) DeleteExpiredBudgetData(
	ctx context.Context,
	before time.Time,
	limit int,
) (int64, error) {
	if store.Database == nil {
		return 0, errors.New("AI budget maintenance database is not configured")
	}
	if limit < 1 || limit > 10_000 {
		return 0, errors.New("AI budget cleanup limit must be between 1 and 10000")
	}
	var total int64
	for _, statement := range []string{
		`WITH candidates AS (
			SELECT tenant_id, work_kind, resource_id, generation
			FROM ai_work_reservations
			WHERE usage_date < $1::date
			ORDER BY usage_date, tenant_id, work_kind, resource_id, generation
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM ai_work_reservations AS reservation
		USING candidates
		WHERE reservation.tenant_id = candidates.tenant_id
		  AND reservation.work_kind = candidates.work_kind
		  AND reservation.resource_id = candidates.resource_id
		  AND reservation.generation = candidates.generation`,
		`WITH candidates AS (
			SELECT project_id, usage_date
			FROM ai_project_daily_usage
			WHERE usage_date < $1::date
			ORDER BY usage_date, project_id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM ai_project_daily_usage AS usage
		USING candidates
		WHERE usage.project_id = candidates.project_id
		  AND usage.usage_date = candidates.usage_date`,
		`WITH candidates AS (
			SELECT tenant_id, usage_date
			FROM ai_daily_usage
			WHERE usage_date < $1::date
			ORDER BY usage_date, tenant_id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM ai_daily_usage AS usage
		USING candidates
		WHERE usage.tenant_id = candidates.tenant_id
		  AND usage.usage_date = candidates.usage_date`,
	} {
		tag, err := store.Database.Exec(ctx, statement, before.UTC(), limit)
		if err != nil {
			return total, fmt.Errorf("delete expired AI budget data: %w", err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

type BudgetMaintainer struct {
	Store  BudgetCleanupStore
	Health retentionhealth.Recorder
	Now    func() time.Time
	Logger *slog.Logger
}

func (maintainer BudgetMaintainer) MaintainOnce(ctx context.Context) error {
	if maintainer.Health == nil {
		return errors.New("AI budget retention health recorder is unavailable")
	}
	now := time.Now().UTC()
	if maintainer.Now != nil {
		now = maintainer.Now().UTC()
	}
	var total int64
	var sweepErr error
	budgetExhausted := true
	for batch := 0; batch < budgetCleanupLimit; batch++ {
		deleted, err := maintainer.Store.DeleteExpiredBudgetData(
			ctx, now.AddDate(0, 0, -budgetRetentionDays), budgetCleanupBatch,
		)
		total += deleted
		if err != nil {
			budgetExhausted = false
			sweepErr = fmt.Errorf("clean AI budget data: %w", err)
			break
		}
		if deleted < budgetCleanupBatch {
			budgetExhausted = false
			break
		}
	}
	switch {
	case sweepErr != nil:
	case budgetExhausted:
		maintainer.logger().WarnContext(ctx, "AI budget cleanup budget exhausted",
			"deleted", total, "backlog_possible", true)
	default:
		maintainer.logger().InfoContext(ctx, "AI budget maintenance completed", "deleted", total)
	}
	result := retentionhealth.ResultSucceeded
	if sweepErr != nil {
		result = retentionhealth.ResultFailed
	}
	recordErr := maintainer.Health.RecordRun(ctx, retentionhealth.Run{
		Operation:   retentionhealth.OperationAIBudgetLedgers,
		AttemptedAt: now, Result: result, Processed: total,
		BudgetExhausted: budgetExhausted,
	})
	if recordErr != nil {
		recordErr = fmt.Errorf("record AI budget retention health: %w", recordErr)
	}
	return errors.Join(sweepErr, recordErr)
}

func (maintainer BudgetMaintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := maintainer.MaintainOnce(ctx); err != nil && ctx.Err() == nil {
			maintainer.logger().ErrorContext(ctx, "AI budget maintenance failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (maintainer BudgetMaintainer) logger() *slog.Logger {
	if maintainer.Logger != nil {
		return maintainer.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
