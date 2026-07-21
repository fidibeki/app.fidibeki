package email

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

const (
	TaskSendEmail = "email:send"
	QueueName     = "email-pool"
)

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
		return nil, fmt.Errorf("parse queue Redis URL: %w", err)
	}
	return &Queue{client: asynq.NewClient(redisOptions)}, nil
}

func NewQueue(client taskClient) *Queue {
	return &Queue{client: client}
}

func (q *Queue) Enqueue(ctx context.Context, message Message) error {
	if err := message.Validate(); err != nil {
		return fmt.Errorf("validate queued email: %w", err)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode queued email: %w", err)
	}
	options := []asynq.Option{
		asynq.Queue(QueueName),
		asynq.MaxRetry(8),
		asynq.Timeout(30 * time.Second),
	}
	if message.IdempotencyKey != "" {
		digest := sha256.Sum256([]byte(message.IdempotencyKey))
		options = append(options, asynq.TaskID(fmt.Sprintf("email:%x", digest[:])))
	}
	_, err = q.client.EnqueueContext(
		ctx,
		asynq.NewTask(TaskSendEmail, payload),
		options...,
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue email: %w", err)
	}
	return nil
}

func (q *Queue) Close() error {
	return q.client.Close()
}

type Worker struct {
	server *asynq.Server
	mux    *asynq.ServeMux
}

func NewWorker(redisURL string, concurrency int, sender Sender) (*Worker, error) {
	if concurrency <= 0 {
		return nil, fmt.Errorf("email worker concurrency must be positive")
	}
	if sender == nil {
		return nil, fmt.Errorf("email worker sender is required")
	}
	redisOptions, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse worker Redis URL: %w", err)
	}
	server := asynq.NewServer(redisOptions, asynq.Config{
		Concurrency: concurrency,
		Queues:      map[string]int{QueueName: 1},
	})
	mux := asynq.NewServeMux()
	if err := RegisterHandlers(mux, sender); err != nil {
		return nil, err
	}
	return &Worker{server: server, mux: mux}, nil
}

func RegisterHandlers(mux *asynq.ServeMux, sender Sender) error {
	if mux == nil {
		return fmt.Errorf("email worker mux is required")
	}
	if sender == nil {
		return fmt.Errorf("email worker sender is required")
	}
	mux.HandleFunc(TaskSendEmail, sendEmailHandler(sender))
	return nil
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.server.Start(w.mux); err != nil {
		return fmt.Errorf("start email worker: %w", err)
	}
	<-ctx.Done()
	w.server.Shutdown()
	return nil
}

func sendEmailHandler(sender Sender) asynq.HandlerFunc {
	return func(ctx context.Context, task *asynq.Task) error {
		var message Message
		if err := json.Unmarshal(task.Payload(), &message); err != nil {
			return fmt.Errorf("decode email task: %w: %w", err, asynq.SkipRetry)
		}
		if _, err := sender.Send(ctx, message); err != nil {
			return fmt.Errorf("send queued email: %w", err)
		}
		return nil
	}
}
