package feedback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

type VerificationTaskQueue interface {
	Enqueue(context.Context, VerificationTask) error
}

type VerificationSchedulingStore interface {
	MarkEnqueued(context.Context, VerificationTask, time.Time) error
	UndoEnqueued(context.Context, VerificationTask) error
	Prepare(context.Context, time.Time, int) ([]VerificationTask, error)
	ExpirePending(context.Context, time.Time, int) (int64, error)
}

type VerificationScheduler struct {
	Store  VerificationSchedulingStore
	Queue  VerificationTaskQueue
	Now    func() time.Time
	Logger *slog.Logger
}

func (scheduler VerificationScheduler) ScheduleVerification(
	ctx context.Context,
	task VerificationTask,
) error {
	now := scheduler.now()
	if err := scheduler.Store.MarkEnqueued(ctx, task, now); err != nil {
		return err
	}
	if err := scheduler.Queue.Enqueue(ctx, task); err != nil {
		undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		undoErr := scheduler.Store.UndoEnqueued(undoCtx, task)
		cancel()
		return errors.Join(err, undoErr)
	}
	return nil
}

func (scheduler VerificationScheduler) ReconcileOnce(ctx context.Context) error {
	now := scheduler.now()
	_, expiryErr := scheduler.Store.ExpirePending(ctx, now, 100)
	tasks, prepareErr := scheduler.Store.Prepare(ctx, now, 100)
	var scheduleErrors []error
	if expiryErr != nil {
		scheduleErrors = append(scheduleErrors, expiryErr)
	}
	if prepareErr != nil {
		scheduleErrors = append(scheduleErrors, prepareErr)
	} else {
		for _, task := range tasks {
			if err := scheduler.Queue.Enqueue(ctx, task); err != nil {
				undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				undoErr := scheduler.Store.UndoEnqueued(undoCtx, task)
				cancel()
				scheduleErrors = append(scheduleErrors, errors.Join(err, undoErr))
			}
		}
	}
	return errors.Join(scheduleErrors...)
}

func (scheduler VerificationScheduler) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := scheduler.ReconcileOnce(ctx); err != nil {
			scheduler.logger().ErrorContext(ctx, "attachment verification reconciliation failed",
				"error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (scheduler VerificationScheduler) now() time.Time {
	if scheduler.Now != nil {
		return scheduler.Now().UTC()
	}
	return time.Now().UTC()
}

func (scheduler VerificationScheduler) logger() *slog.Logger {
	if scheduler.Logger != nil {
		return scheduler.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func (task VerificationTask) String() string {
	return fmt.Sprintf("%s/%s/%d", task.TenantID, task.AttachmentID, task.Generation)
}
