package mobilecapture

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
	cleanupBatchSize  = 500
	cleanupBatchLimit = 20
)

type CleanupStore interface {
	DeleteExpired(context.Context, time.Time, int) (int64, error)
}

type MaintenanceStore struct {
	Database db.Querier
	Store    Store
}

func (store MaintenanceStore) DeleteExpired(
	ctx context.Context,
	now time.Time,
	limit int,
) (int64, error) {
	return store.Store.DeleteExpired(ctx, store.Database, now, limit)
}

type Cleaner struct {
	Store  CleanupStore
	Health retentionhealth.Recorder
	Now    func() time.Time
	Logger *slog.Logger
}

func (cleaner Cleaner) CleanOnce(ctx context.Context) error {
	if cleaner.Store == nil {
		return fmt.Errorf("mobile capture cleanup store is unavailable")
	}
	if cleaner.Health == nil {
		return fmt.Errorf("mobile capture retention health recorder is unavailable")
	}
	now := time.Now().UTC()
	if cleaner.Now != nil {
		now = cleaner.Now().UTC()
	}
	var total int64
	var sweepErr error
	budgetExhausted := true
	for batch := 0; batch < cleanupBatchLimit; batch++ {
		deleted, err := cleaner.Store.DeleteExpired(ctx, now, cleanupBatchSize)
		total += deleted
		if err != nil {
			budgetExhausted = false
			sweepErr = fmt.Errorf("clean mobile capture sessions: %w", err)
			break
		}
		if deleted < cleanupBatchSize {
			budgetExhausted = false
			break
		}
	}
	switch {
	case sweepErr != nil:
	case budgetExhausted:
		cleaner.logger().WarnContext(
			ctx, "mobile capture session cleanup budget exhausted",
			"deleted", total, "backlog_possible", true,
		)
	default:
		cleaner.logger().InfoContext(
			ctx, "mobile capture session cleanup completed", "deleted", total,
		)
	}
	result := retentionhealth.ResultSucceeded
	if sweepErr != nil {
		result = retentionhealth.ResultFailed
	}
	recordErr := cleaner.Health.RecordRun(ctx, retentionhealth.Run{
		Operation:       retentionhealth.OperationMobileCaptureSessions,
		AttemptedAt:     now,
		Result:          result,
		Processed:       total,
		BudgetExhausted: budgetExhausted,
	})
	if recordErr != nil {
		recordErr = fmt.Errorf("record mobile capture retention health: %w", recordErr)
	}
	return errors.Join(sweepErr, recordErr)
}

func (cleaner Cleaner) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := cleaner.CleanOnce(ctx); err != nil && ctx.Err() == nil {
			cleaner.logger().ErrorContext(
				ctx, "mobile capture session cleanup failed", "error", err,
			)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (cleaner Cleaner) logger() *slog.Logger {
	if cleaner.Logger != nil {
		return cleaner.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
