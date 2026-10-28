package abuse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
)

const (
	diagnosticRetentionDays = 90
	diagnosticCleanupBatch  = 1000
	diagnosticCleanupLimit  = 100
)

type CleanupStore interface {
	DeleteExpired(context.Context, time.Time, int) (int64, error)
}

type MaintenanceStore struct {
	Recorder Recorder
}

func (store MaintenanceStore) DeleteExpired(
	ctx context.Context,
	before time.Time,
	limit int,
) (int64, error) {
	return store.Recorder.Store.DeleteExpired(ctx, store.Recorder.Database, before, limit)
}

type Maintainer struct {
	Store  CleanupStore
	Health retentionhealth.Recorder
	Now    func() time.Time
	Logger *slog.Logger
}

func (maintainer Maintainer) MaintainOnce(ctx context.Context) error {
	if maintainer.Health == nil {
		return errors.New("abuse diagnostics retention health recorder is unavailable")
	}
	now := time.Now().UTC()
	if maintainer.Now != nil {
		now = maintainer.Now().UTC()
	}
	var total int64
	var sweepErr error
	budgetExhausted := true
	for batch := 0; batch < diagnosticCleanupLimit; batch++ {
		deleted, err := maintainer.Store.DeleteExpired(
			ctx, now.AddDate(0, 0, -diagnosticRetentionDays), diagnosticCleanupBatch,
		)
		total += deleted
		if err != nil {
			budgetExhausted = false
			sweepErr = fmt.Errorf("clean abuse diagnostics: %w", err)
			break
		}
		if deleted < diagnosticCleanupBatch {
			budgetExhausted = false
			break
		}
	}
	switch {
	case sweepErr != nil:
	case budgetExhausted:
		maintainer.logger().WarnContext(ctx, "abuse diagnostics cleanup budget exhausted",
			"deleted", total, "backlog_possible", true)
	default:
		maintainer.logger().InfoContext(ctx, "abuse diagnostics maintenance completed",
			"deleted", total)
	}
	result := retentionhealth.ResultSucceeded
	if sweepErr != nil {
		result = retentionhealth.ResultFailed
	}
	recordErr := maintainer.Health.RecordRun(ctx, retentionhealth.Run{
		Operation:   retentionhealth.OperationAbuseDiagnostics,
		AttemptedAt: now, Result: result, Processed: total,
		BudgetExhausted: budgetExhausted,
	})
	if recordErr != nil {
		recordErr = fmt.Errorf("record abuse diagnostics retention health: %w", recordErr)
	}
	return errors.Join(sweepErr, recordErr)
}

func (maintainer Maintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := maintainer.MaintainOnce(ctx); err != nil && ctx.Err() == nil {
			maintainer.logger().ErrorContext(ctx, "abuse diagnostics maintenance failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (maintainer Maintainer) logger() *slog.Logger {
	if maintainer.Logger != nil {
		return maintainer.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
