package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/redisconn"
	"github.com/hibiken/asynq"
)

const (
	TaskMerge = "replay:merge"
	QueueName = "replay-pool"
)

const abandonedSessionGrace = replayUploadTTL + 5*time.Minute

type taskClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type Queue struct {
	client taskClient
}

func OpenQueue(redisURL string, caCertificates ...string) (*Queue, error) {
	redisOptions, err := redisconn.AsynqOptions(redisURL, caCertificates...)
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
		asynq.Queue(QueueName),
		asynq.MaxRetry(terminalMergeAttempts),
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

func (q *Queue) EnqueueVerification(ctx context.Context, task BatchVerificationTask) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("encode replay batch verification task: %w", err)
	}
	_, err = q.client.EnqueueContext(
		ctx,
		asynq.NewTask(TaskVerifyBatch, payload),
		asynq.TaskID(fmt.Sprintf("replay:verify:%s:%d", task.BatchID, task.Generation)),
		asynq.Queue(QueueName),
		asynq.MaxRetry(12),
		asynq.Timeout(2*time.Minute),
		asynq.Retention(24*time.Hour),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue replay batch verification: %w", err)
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

type BatchVerificationQueue interface {
	EnqueueVerification(context.Context, BatchVerificationTask) error
}

type ExpiredBatchStorage interface {
	RemoveObject(context.Context, string) error
}

type Scheduler struct {
	Store             Store
	Queue             MergeQueue
	VerificationQueue BatchVerificationQueue
	Storage           ExpiredBatchStorage
	Logger            *slog.Logger
	Now               func() time.Time
}

func (s Scheduler) Schedule(ctx context.Context, sessionID string) error {
	if err := s.Queue.EnqueueMerge(ctx, sessionID); err != nil {
		return err
	}
	return s.Store.MarkMergeEnqueued(ctx, sessionID, s.now())
}

func (s Scheduler) ScheduleBatch(ctx context.Context, task BatchVerificationTask) error {
	if s.VerificationQueue == nil {
		return errors.New("replay batch verification queue is unavailable")
	}
	now := s.now()
	if err := s.Store.MarkVerificationEnqueued(ctx, task, now); err != nil {
		return err
	}
	if err := s.VerificationQueue.EnqueueVerification(ctx, task); err != nil {
		undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		undoErr := s.Store.UndoVerificationEnqueued(undoCtx, task)
		cancel()
		return errors.Join(err, undoErr)
	}
	return nil
}

func (s Scheduler) ReconcileOnce(ctx context.Context) error {
	now := s.now()
	var result error
	if s.VerificationQueue != nil {
		expired, err := s.Store.ClaimExpiredBatches(ctx, now, 100)
		if err != nil {
			result = errors.Join(result, err)
		} else if len(expired) > 0 && s.Storage == nil {
			result = errors.Join(result, errors.New("replay expiry storage is unavailable"))
		} else {
			for _, upload := range expired {
				if err := s.Storage.RemoveObject(ctx, upload.StorageKey); err != nil {
					result = errors.Join(result,
						fmt.Errorf("remove expired replay object %q: %w", upload.StorageKey, err))
					continue
				}
				if err := s.Store.CompleteBatchExpiry(ctx, upload, now); err != nil &&
					!errors.Is(err, ErrNoBatchVerificationWork) {
					result = errors.Join(result, err)
				}
			}
		}
		tasks, err := s.Store.PrepareBatchVerifications(ctx, now, 100)
		if err != nil {
			result = errors.Join(result, err)
		} else {
			for _, task := range tasks {
				if err := s.VerificationQueue.EnqueueVerification(ctx, task); err != nil {
					undoCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
					undoErr := s.Store.UndoVerificationEnqueued(undoCtx, task)
					cancel()
					result = errors.Join(result, err, undoErr)
				}
			}
		}
	}
	if err := s.Store.FinalizeReadySessions(ctx, 100); err != nil {
		result = errors.Join(result, err)
	}
	if err := s.Store.RecoverAbandoned(ctx, now.Add(-abandonedSessionGrace), now, 100); err != nil {
		result = errors.Join(result, err)
	}
	sessions, err := s.Store.PendingMerges(ctx, 100, now.Add(-5*time.Minute))
	if err != nil {
		return errors.Join(result, err)
	}
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
