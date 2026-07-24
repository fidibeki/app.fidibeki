package widgetanalytics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

const (
	analyticsReconciliationDays = 3
	analyticsRawRetentionDays   = 90
	analyticsDailyRetentionDays = 730
	analyticsMaintenanceBatch   = 1000
	analyticsMaintenanceBatches = 100
	analyticsMaintenanceTimeout = 2 * time.Minute
)

type ReconcileResult struct {
	Upserted   int64
	Removed    int64
	Chunks     int64
	Skipped    bool
	SkipReason string
}

type MaintenanceOperations interface {
	ReconcileRecent(context.Context, time.Time, int) (ReconcileResult, error)
	DeleteExpiredEvents(context.Context, time.Time, int) (int64, error)
	DeleteExpiredDaily(context.Context, time.Time, int) (int64, error)
}

type Maintainer struct {
	Store        MaintenanceOperations
	Health       retentionhealth.Recorder
	Logger       *slog.Logger
	Now          func() time.Time
	BatchSize    int
	MaxBatches   int
	PhaseTimeout time.Duration
}

func (maintainer Maintainer) MaintainOnce(ctx context.Context) error {
	if maintainer.Health == nil {
		return errors.New("widget analytics retention health recorder is unavailable")
	}
	now := maintainer.now()
	startedAt := time.Now()
	reconcileCtx, cancelReconcile := maintainer.phaseContext(ctx)
	reconciled, err := maintainer.Store.ReconcileRecent(reconcileCtx, now, analyticsReconciliationDays)
	cancelReconcile()
	if err != nil {
		reconcileErr := fmt.Errorf("reconcile widget analytics: %w", err)
		return errors.Join(reconcileErr,
			maintainer.recordSweep(ctx, retentionhealth.OperationWidgetFunnelEvents,
				now, 0, false, reconcileErr),
			maintainer.recordSweep(ctx, retentionhealth.OperationWidgetFunnelDaily,
				now, 0, false, reconcileErr),
		)
	}
	if reconciled.Skipped {
		maintainer.logger().InfoContext(ctx, "widget analytics maintenance skipped",
			"reason", reconciled.SkipReason)
		return nil
	}
	batchSize := maintainer.BatchSize
	if batchSize <= 0 {
		batchSize = analyticsMaintenanceBatch
	}
	maxBatches := maintainer.MaxBatches
	if maxBatches <= 0 {
		maxBatches = analyticsMaintenanceBatches
	}
	eventsCtx, cancelEvents := maintainer.phaseContext(ctx)
	eventsDeleted, eventsBudgetExhausted, eventsErr := drainAnalyticsRetention(
		eventsCtx, batchSize, maxBatches,
		func(deleteCtx context.Context, limit int) (int64, error) {
			return maintainer.Store.DeleteExpiredEvents(
				deleteCtx, now.AddDate(0, 0, -analyticsRawRetentionDays), limit,
			)
		},
	)
	cancelEvents()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	dailyCtx, cancelDaily := maintainer.phaseContext(ctx)
	dailyDeleted, dailyBudgetExhausted, dailyErr := drainAnalyticsRetention(
		dailyCtx, batchSize, maxBatches,
		func(deleteCtx context.Context, limit int) (int64, error) {
			return maintainer.Store.DeleteExpiredDaily(
				deleteCtx, today.AddDate(0, 0, -analyticsDailyRetentionDays), limit,
			)
		},
	)
	cancelDaily()
	var maintenanceErrors []error
	if eventsErr != nil {
		maintenanceErrors = append(maintenanceErrors,
			fmt.Errorf("delete expired widget analytics events: %w", eventsErr))
	}
	if dailyErr != nil {
		maintenanceErrors = append(maintenanceErrors,
			fmt.Errorf("delete expired widget analytics daily aggregates: %w", dailyErr))
	}
	maintenanceErr := errors.Join(maintenanceErrors...)
	log := maintainer.logger()
	message := "widget analytics maintenance completed"
	if maintenanceErr != nil {
		message = "widget analytics maintenance partially completed"
	}
	log.LogAttrs(ctx, slog.LevelInfo, message,
		slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		slog.Int64("aggregates_upserted", reconciled.Upserted),
		slog.Int64("aggregates_removed", reconciled.Removed),
		slog.Int64("reconciliation_chunks", reconciled.Chunks),
		slog.Int64("events_deleted", eventsDeleted),
		slog.Int64("daily_aggregates_deleted", dailyDeleted),
		slog.Bool("event_cleanup_failed", eventsErr != nil),
		slog.Bool("daily_cleanup_failed", dailyErr != nil),
	)
	if eventsBudgetExhausted || dailyBudgetExhausted {
		log.WarnContext(ctx, "widget analytics retention budget exhausted",
			"event_budget_exhausted", eventsBudgetExhausted,
			"daily_budget_exhausted", dailyBudgetExhausted,
			"backlog_possible", true,
			"maximum_batches", maxBatches,
		)
	}
	recordErr := errors.Join(
		maintainer.recordSweep(ctx, retentionhealth.OperationWidgetFunnelEvents,
			now, eventsDeleted, eventsBudgetExhausted, eventsErr),
		maintainer.recordSweep(ctx, retentionhealth.OperationWidgetFunnelDaily,
			now, dailyDeleted, dailyBudgetExhausted, dailyErr),
	)
	return errors.Join(maintenanceErr, recordErr)
}

func (maintainer Maintainer) recordSweep(
	ctx context.Context,
	operation retentionhealth.Operation,
	attemptedAt time.Time,
	processed int64,
	budgetExhausted bool,
	sweepErr error,
) error {
	result := retentionhealth.ResultSucceeded
	if sweepErr != nil {
		result = retentionhealth.ResultFailed
	}
	if err := maintainer.Health.RecordRun(ctx, retentionhealth.Run{
		Operation: operation, AttemptedAt: attemptedAt, Result: result,
		Processed: processed, BudgetExhausted: budgetExhausted,
	}); err != nil {
		return fmt.Errorf("record %s retention health: %w", operation, err)
	}
	return nil
}

func (maintainer Maintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := maintainer.MaintainOnce(ctx); err != nil && ctx.Err() == nil {
			maintainer.logger().ErrorContext(ctx, "widget analytics maintenance failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func drainAnalyticsRetention(
	ctx context.Context,
	batchSize int,
	maxBatches int,
	deleteBatch func(context.Context, int) (int64, error),
) (int64, bool, error) {
	var total int64
	for batch := 0; batch < maxBatches; batch++ {
		deleted, err := deleteBatch(ctx, batchSize)
		total += deleted
		if err != nil {
			return total, false, err
		}
		if deleted < int64(batchSize) {
			return total, false, nil
		}
	}
	return total, true, nil
}

func (maintainer Maintainer) phaseContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := maintainer.PhaseTimeout
	if timeout <= 0 {
		timeout = analyticsMaintenanceTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (maintainer Maintainer) now() time.Time {
	if maintainer.Now != nil {
		return maintainer.Now().UTC()
	}
	return time.Now().UTC()
}

func (maintainer Maintainer) logger() *slog.Logger {
	if maintainer.Logger != nil {
		return maintainer.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
