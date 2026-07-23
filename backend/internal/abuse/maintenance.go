package abuse

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
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
	Now    func() time.Time
	Logger *slog.Logger
}

func (maintainer Maintainer) MaintainOnce(ctx context.Context) error {
	now := time.Now().UTC()
	if maintainer.Now != nil {
		now = maintainer.Now().UTC()
	}
	var total int64
	for batch := 0; batch < diagnosticCleanupLimit; batch++ {
		deleted, err := maintainer.Store.DeleteExpired(
			ctx, now.AddDate(0, 0, -diagnosticRetentionDays), diagnosticCleanupBatch,
		)
		total += deleted
		if err != nil {
			return fmt.Errorf("clean abuse diagnostics: %w", err)
		}
		if deleted < diagnosticCleanupBatch {
			maintainer.logger().InfoContext(ctx, "abuse diagnostics maintenance completed",
				"deleted", total)
			return nil
		}
	}
	maintainer.logger().WarnContext(ctx, "abuse diagnostics cleanup budget exhausted",
		"deleted", total, "backlog_possible", true)
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
