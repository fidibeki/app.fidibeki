package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

const (
	TaskAttachmentVerify  = "attachment:verify"
	VerificationQueueName = "attachment-verification-pool"
)

type VerificationTask struct {
	TenantID     string `json:"tenant_id"`
	AttachmentID string `json:"attachment_id"`
	Generation   int    `json:"generation"`
}

type verificationTaskClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type VerificationQueue struct {
	client verificationTaskClient
}

func OpenVerificationQueue(redisURL string) (*VerificationQueue, error) {
	options, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse attachment verification Redis URL: %w", err)
	}
	return &VerificationQueue{client: asynq.NewClient(options)}, nil
}

func NewVerificationQueue(client verificationTaskClient) *VerificationQueue {
	return &VerificationQueue{client: client}
}

func (queue *VerificationQueue) Enqueue(ctx context.Context, task VerificationTask) error {
	payload, err := json.Marshal(map[string]any{
		"tenant_id": task.TenantID, "attachment_id": task.AttachmentID,
		"generation": task.Generation,
	})
	if err != nil {
		return fmt.Errorf("encode attachment verification task: %w", err)
	}
	_, err = queue.client.EnqueueContext(
		ctx,
		asynq.NewTask(TaskAttachmentVerify, payload),
		asynq.TaskID(VerificationTaskID(task)),
		asynq.Queue(VerificationQueueName),
		asynq.MaxRetry(12),
		asynq.Timeout(5*time.Minute),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue attachment verification: %w", err)
	}
	return nil
}

func (queue *VerificationQueue) Close() error {
	if err := queue.client.Close(); err != nil {
		return fmt.Errorf("close attachment verification queue: %w", err)
	}
	return nil
}

func VerificationTaskID(task VerificationTask) string {
	return fmt.Sprintf("attachment:verify:%s:%d", task.AttachmentID, task.Generation)
}
