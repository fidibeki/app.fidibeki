package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

type Scheduler struct {
	Store      Store
	Queue      TaskQueue
	DailyLimit int
	Logger     *slog.Logger
	Now        func() time.Time
}

func (s Scheduler) ScheduleFeedback(ctx context.Context, feedbackID string) error {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	prepared, exhausted, err := s.Store.PrepareFeedback(ctx, feedbackID, s.DailyLimit, now)
	if err != nil {
		return err
	}
	var enqueueErrors []error
	for _, task := range prepared {
		if err := s.Queue.Enqueue(ctx, task); err != nil {
			undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			undoErr := s.Store.UndoPrepared(undoCtx, task)
			cancel()
			if undoErr != nil {
				err = errors.Join(err, undoErr)
			}
			enqueueErrors = append(enqueueErrors, err)
		}
	}
	if exhausted {
		s.logger().WarnContext(ctx, "tenant AI daily budget exhausted", "feedback_id", feedbackID)
	}
	return errors.Join(enqueueErrors...)
}

func (s Scheduler) ScheduleAttachment(ctx context.Context, _ string, feedbackID string) error {
	return s.ScheduleFeedback(ctx, feedbackID)
}

func (s Scheduler) ReconcileOnce(ctx context.Context) error {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	var scheduleErrors []error
	inFlight, err := s.Store.InFlightTasks(ctx, 500)
	if err != nil {
		scheduleErrors = append(scheduleErrors, err)
	} else {
		for _, task := range inFlight {
			if enqueueErr := s.Queue.Enqueue(ctx, task); enqueueErr != nil {
				scheduleErrors = append(scheduleErrors, enqueueErr)
			}
		}
	}
	ids, err := s.Store.PendingFeedbackIDs(ctx, 100, s.DailyLimit, now)
	if err != nil {
		scheduleErrors = append(scheduleErrors, err)
	}
	for _, id := range ids {
		if err := s.ScheduleFeedback(ctx, id); err != nil {
			scheduleErrors = append(scheduleErrors, fmt.Errorf("schedule feedback %s: %w", id, err))
		}
	}
	cleanupTasks, err := s.Store.PrepareGeminiFileDeletions(ctx, 100, now)
	if err != nil {
		scheduleErrors = append(scheduleErrors, err)
	} else {
		for _, task := range cleanupTasks {
			if enqueueErr := s.Queue.Enqueue(ctx, task); enqueueErr != nil {
				undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				undoErr := s.Store.UndoPrepared(undoCtx, task)
				cancel()
				if undoErr != nil {
					enqueueErr = errors.Join(enqueueErr, undoErr)
				}
				scheduleErrors = append(scheduleErrors, enqueueErr)
			}
		}
	}
	return errors.Join(scheduleErrors...)
}

func (s Scheduler) RunReconciler(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.ReconcileOnce(ctx); err != nil {
			s.logger().ErrorContext(ctx, "AI reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s Scheduler) PipelineHandler() asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload struct {
			ResourceID string `json:"resource_id"`
		}
		if err := json.Unmarshal(task.Payload(), &payload); err != nil || payload.ResourceID == "" {
			return fmt.Errorf("decode feedback pipeline task: %w: %w", err, asynq.SkipRetry)
		}
		if err := s.ScheduleFeedback(ctx, payload.ResourceID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("schedule missing feedback: %w: %w", err, asynq.SkipRetry)
			}
			return fmt.Errorf("schedule feedback AI: %w", err)
		}
		return nil
	}
}

func (s Scheduler) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
