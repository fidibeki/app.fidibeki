package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

const (
	TaskExport = "privacy:export"
	QueueName  = "privacy-pool"
)

type taskClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type TaskQueue interface {
	Enqueue(context.Context, PreparedJob) error
}

type Queue struct {
	client taskClient
}

func OpenQueue(redisURL string) (*Queue, error) {
	options, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse privacy queue Redis URL: %w", err)
	}
	return &Queue{client: asynq.NewClient(options)}, nil
}

func NewQueue(client taskClient) *Queue {
	return &Queue{client: client}
}

func (queue *Queue) Enqueue(ctx context.Context, job PreparedJob) error {
	payload, err := json.Marshal(map[string]any{
		"job_id": job.ID, "tenant_id": job.TenantID, "generation": job.Generation,
	})
	if err != nil {
		return fmt.Errorf("encode privacy job: %w", err)
	}
	_, err = queue.client.EnqueueContext(
		ctx,
		asynq.NewTask(TaskExport, payload),
		asynq.TaskID(fmt.Sprintf("privacy:export:%s:%d", job.ID, job.Generation)),
		asynq.Queue(QueueName),
		asynq.MaxRetry(3),
		asynq.Timeout(30*time.Minute),
		asynq.Retention(24*time.Hour),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue privacy job: %w", err)
	}
	return nil
}

func (queue *Queue) Close() error {
	if err := queue.client.Close(); err != nil {
		return fmt.Errorf("close privacy queue: %w", err)
	}
	return nil
}
