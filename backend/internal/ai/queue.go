package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

type taskClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type TaskQueue interface {
	Enqueue(context.Context, PreparedTask) error
}

type Queue struct {
	client taskClient
}

func OpenQueue(redisURL string) (*Queue, error) {
	options, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse AI queue Redis URL: %w", err)
	}
	return &Queue{client: asynq.NewClient(options)}, nil
}

func NewQueue(client taskClient) *Queue {
	return &Queue{client: client}
}

type taskPayload struct {
	TenantID   string `json:"tenant_id"`
	ProjectID  string `json:"project_id,omitempty"`
	ResourceID string `json:"resource_id"`
	Attempt    int    `json:"attempt,omitempty"`
	Generation int64  `json:"generation,omitempty"`
}

func (q *Queue) Enqueue(ctx context.Context, prepared PreparedTask) error {
	payload, err := json.Marshal(taskPayload{
		TenantID: prepared.TenantID, ProjectID: prepared.ProjectID, ResourceID: prepared.ResourceID,
		Attempt: prepared.Attempt, Generation: prepared.Generation,
	})
	if err != nil {
		return fmt.Errorf("encode AI task: %w", err)
	}
	taskType := TaskTextProcess
	switch prepared.Kind {
	case TaskKindAttachment:
		taskType = TaskAttachmentProcess
	case TaskKindFileDelete:
		taskType = TaskGeminiFileDelete
	}
	_, err = q.client.EnqueueContext(
		ctx,
		asynq.NewTask(taskType, payload),
		asynq.TaskID(TaskID(prepared)),
		asynq.Queue(QueueName),
		asynq.MaxRetry(12),
		asynq.Timeout(10*time.Minute),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue AI task: %w", err)
	}
	return nil
}

func (q *Queue) Close() error {
	if err := q.client.Close(); err != nil {
		return fmt.Errorf("close AI queue: %w", err)
	}
	return nil
}

func TaskID(prepared PreparedTask) string {
	if prepared.Kind == TaskKindAttachment {
		return fmt.Sprintf("ai:attachment:%s:%d", prepared.ResourceID, prepared.Attempt)
	}
	if prepared.Kind == TaskKindFileDelete {
		return fmt.Sprintf("ai:file-delete:%s:%d", prepared.ResourceID, prepared.Attempt)
	}
	return fmt.Sprintf("ai:text:%s:%d", prepared.ResourceID, prepared.Generation)
}
