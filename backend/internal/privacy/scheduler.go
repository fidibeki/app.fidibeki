package privacy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"
)

type Scheduler struct {
	Store  Store
	Queue  TaskQueue
	Logger *slog.Logger
	Now    func() time.Time
}

func (scheduler Scheduler) ReconcileOnce(ctx context.Context) error {
	prepared, err := scheduler.Store.PrepareJobs(ctx, scheduler.now(), 25)
	if err != nil {
		return err
	}
	var result error
	for _, job := range prepared {
		if err := scheduler.Queue.Enqueue(ctx, job); err != nil {
			undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			undoErr := scheduler.Store.UndoPrepared(undoCtx, job)
			cancel()
			result = errors.Join(result, err, undoErr)
		}
	}
	return result
}

func (scheduler Scheduler) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := scheduler.ReconcileOnce(ctx); err != nil && ctx.Err() == nil {
			scheduler.logger().ErrorContext(ctx, "privacy reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (scheduler Scheduler) now() time.Time {
	if scheduler.Now != nil {
		return scheduler.Now().UTC()
	}
	return time.Now().UTC()
}

func (scheduler Scheduler) logger() *slog.Logger {
	if scheduler.Logger != nil {
		return scheduler.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
