package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/outbox"
	"github.com/hibiken/asynq"
)

const TaskMerge = "replay:merge"

const abandonedSessionGrace = replayUploadTTL + 5*time.Minute

type taskClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type Queue struct {
	client taskClient
}

func OpenQueue(redisURL string) (*Queue, error) {
	redisOptions, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse replay queue Redis URL: %w", err)
	}
	return &Queue{client: asynq.NewClient(redisOptions)}, nil
}

func NewQueue(client taskClient) *Queue {
	return &Queue{client: client}
}

func (q *Queue) EnqueueMerge(ctx context.Context, sessionID string) error {
	payload, err := json.Marshal(map[string]string{"session_id": sessionID})
	if err != nil {
		return fmt.Errorf("encode replay merge task: %w", err)
	}
	_, err = q.client.EnqueueContext(
		ctx,
		asynq.NewTask(TaskMerge, payload),
		asynq.TaskID("replay:merge:"+sessionID),
		asynq.Queue(outbox.QueueName),
		asynq.MaxRetry(12),
		asynq.Timeout(10*time.Minute),
		asynq.Retention(24*time.Hour),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue replay merge: %w", err)
	}
	return nil
}

func (q *Queue) Close() error {
	if err := q.client.Close(); err != nil {
		return fmt.Errorf("close replay queue: %w", err)
	}
	return nil
}

type MergeQueue interface {
	EnqueueMerge(context.Context, string) error
}

type Scheduler struct {
	Store  Store
	Queue  MergeQueue
	Logger *slog.Logger
	Now    func() time.Time
}

func (s Scheduler) Schedule(ctx context.Context, sessionID string) error {
	if err := s.Queue.EnqueueMerge(ctx, sessionID); err != nil {
		return err
	}
	return s.Store.MarkMergeEnqueued(ctx, sessionID, s.now())
}

func (s Scheduler) ReconcileOnce(ctx context.Context) error {
	now := s.now()
	if err := s.Store.RecoverAbandoned(ctx, now.Add(-abandonedSessionGrace), now, 100); err != nil {
		return err
	}
	sessions, err := s.Store.PendingMerges(ctx, 100, now.Add(-5*time.Minute))
	if err != nil {
		return err
	}
	var result error
	for _, session := range sessions {
		if err := s.Schedule(ctx, session.ID); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (s Scheduler) RunReconciler(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Minute
	}
	logger := s.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.ReconcileOnce(ctx); err != nil {
			logger.ErrorContext(ctx, "replay merge reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
