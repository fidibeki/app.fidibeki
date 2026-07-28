package mobilecapture

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
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
	Now    func() time.Time
	Logger *slog.Logger
}

func (cleaner Cleaner) CleanOnce(ctx context.Context) error {
	if cleaner.Store == nil {
		return fmt.Errorf("mobile capture cleanup store is unavailable")
	}
	now := time.Now().UTC()
	if cleaner.Now != nil {
		now = cleaner.Now().UTC()
	}
	var total int64
	for batch := 0; batch < cleanupBatchLimit; batch++ {
		deleted, err := cleaner.Store.DeleteExpired(ctx, now, cleanupBatchSize)
		total += deleted
		if err != nil {
			return fmt.Errorf("clean mobile capture sessions: %w", err)
		}
		if deleted < cleanupBatchSize {
			cleaner.logger().InfoContext(
				ctx, "mobile capture session cleanup completed", "deleted", total,
			)
			return nil
		}
	}
	cleaner.logger().WarnContext(
		ctx, "mobile capture session cleanup budget exhausted",
		"deleted", total, "backlog_possible", true,
	)
	return nil
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
