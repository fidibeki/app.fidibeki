package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/redisconn"
	"github.com/hibiken/asynq"
)

type taskClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type TaskQueue interface {
	Enqueue(context.Context, PreparedTask) error
}

type legacyTaskInspector interface {
	GetTaskInfo(string, string) (*asynq.TaskInfo, error)
	Close() error
}
type Queue struct {
	client taskClient
	legacy legacyTaskInspector
}

func OpenQueue(redisURL string, caCertificates ...string) (*Queue, error) {
	options, err := redisconn.AsynqOptions(redisURL, caCertificates...)
	if err != nil {
		return nil, fmt.Errorf("parse AI queue Redis URL: %w", err)
	}
	return &Queue{client: asynq.NewClient(options), legacy: asynq.NewInspector(options)}, nil
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
	// Reconciliation must not clone work still owned by the legacy queue during
	// the drain. New jobs always go to v2, beyond the reach of old workers.
	if q.legacy != nil {
		_, err := q.legacy.GetTaskInfo(LegacyQueueName, TaskID(prepared))
		if err == nil {
			return nil
		}
		if !errors.Is(err, asynq.ErrTaskNotFound) && !errors.Is(err, asynq.ErrQueueNotFound) {
			return fmt.Errorf("inspect legacy AI task: %w", err)
		}
	}
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
	if q.legacy != nil {
		defer q.legacy.Close()
	}
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
