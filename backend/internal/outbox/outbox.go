package outbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/redisconn"
	"github.com/hibiken/asynq"
)

const (
	QueueName        = "domain-pool"
	TaskFeedbackPipe = "feedback:pipeline"
)

type Event struct {
	ID           string
	TenantID     string
	Event        string
	ResourceType string
	ResourceID   string
	Attempts     int
}

type Destination struct {
	Kind     string
	ID       string
	TaskType string
	Queue    string
}

type EventStore interface {
	Pending(context.Context, int) ([]Event, error)
	MarkDispatched(context.Context, string, time.Time) error
	MarkFailed(context.Context, string, string) error
}

type DestinationResolver interface {
	Resolve(context.Context, Event) ([]Destination, error)
}

type Enqueuer interface {
	Enqueue(context.Context, Event, Destination) error
}

type Store struct {
	Database db.Querier
}

func (s Store) Pending(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.Database.Query(ctx, `
		SELECT id, tenant_id, event, resource_type, resource_id, attempts
		FROM outbox_events
		WHERE dispatched_at IS NULL
		ORDER BY created_at, id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(
			&event.ID, &event.TenantID, &event.Event, &event.ResourceType,
			&event.ResourceID, &event.Attempts,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox events: %w", err)
	}
	return events, nil
}

func (s Store) MarkDispatched(ctx context.Context, eventID string, now time.Time) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE outbox_events
		SET dispatched_at = $2, attempts = attempts + 1, last_error = NULL
		WHERE id = $1 AND dispatched_at IS NULL
	`, eventID, now); err != nil {
		return fmt.Errorf("mark outbox event dispatched: %w", err)
	}
	return nil
}

func (s Store) MarkFailed(ctx context.Context, eventID string, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE outbox_events
		SET attempts = attempts + 1, last_error = $2
		WHERE id = $1 AND dispatched_at IS NULL
	`, eventID, message); err != nil {
		return fmt.Errorf("mark outbox event failed: %w", err)
	}
	return nil
}

type FeedbackResolver struct{}

func (FeedbackResolver) Resolve(_ context.Context, event Event) ([]Destination, error) {
	if (event.Event != "feedback.created" && event.Event != "feedback.rectified") ||
		event.ResourceType != "feedback" {
		return nil, nil
	}
	return []Destination{{
		Kind: "pipeline", ID: event.ResourceID, TaskType: TaskFeedbackPipe,
	}}, nil
}

type MultiResolver []DestinationResolver

func (resolvers MultiResolver) Resolve(ctx context.Context, event Event) ([]Destination, error) {
	destinations := make([]Destination, 0)
	for _, resolver := range resolvers {
		resolved, err := resolver.Resolve(ctx, event)
		if err != nil {
			return nil, err
		}
		destinations = append(destinations, resolved...)
	}
	return destinations, nil
}

type taskClient interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

type AsynqEnqueuer struct {
	client taskClient
}

func OpenAsynqEnqueuer(redisURL string, caCertificates ...string) (*AsynqEnqueuer, error) {
	options, err := redisconn.AsynqOptions(redisURL, caCertificates...)
	if err != nil {
		return nil, fmt.Errorf("parse outbox Redis URL: %w", err)
	}
	return &AsynqEnqueuer{client: asynq.NewClient(options)}, nil
}

func NewAsynqEnqueuer(client taskClient) *AsynqEnqueuer {
	return &AsynqEnqueuer{client: client}
}

func (e *AsynqEnqueuer) Enqueue(ctx context.Context, event Event, destination Destination) error {
	queue := destination.Queue
	if queue == "" {
		queue = QueueName
	}
	payload := []byte(fmt.Sprintf(
		`{"outbox_event_id":%q,"tenant_id":%q,"resource_id":%q,"destination_id":%q}`,
		event.ID, event.TenantID, event.ResourceID, destination.ID,
	))
	_, err := e.client.EnqueueContext(
		ctx,
		asynq.NewTask(destination.TaskType, payload),
		asynq.TaskID(TaskID(event, destination)),
		asynq.Queue(queue),
		asynq.MaxRetry(12),
		asynq.Timeout(2*time.Minute),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue outbox destination: %w", err)
	}
	return nil
}

func (e *AsynqEnqueuer) Close() error {
	if err := e.client.Close(); err != nil {
		return fmt.Errorf("close outbox queue: %w", err)
	}
	return nil
}

func TaskID(event Event, destination Destination) string {
	return fmt.Sprintf("outbox:%s:%s:%s", event.ID, destination.Kind, destination.ID)
}

type Dispatcher struct {
	Store    EventStore
	Resolver DestinationResolver
	Enqueuer Enqueuer
	Logger   *slog.Logger
	Now      func() time.Time
	Batch    int
}

func (d Dispatcher) RunOnce(ctx context.Context) error {
	batch := d.Batch
	if batch <= 0 {
		batch = 100
	}
	events, err := d.Store.Pending(ctx, batch)
	if err != nil {
		return err
	}
	for _, event := range events {
		destinations, err := d.Resolver.Resolve(ctx, event)
		if err == nil {
			for _, destination := range destinations {
				if enqueueErr := d.Enqueuer.Enqueue(ctx, event, destination); enqueueErr != nil {
					err = enqueueErr
					break
				}
			}
		}
		if err != nil {
			if markErr := d.Store.MarkFailed(ctx, event.ID, err.Error()); markErr != nil {
				return errors.Join(err, markErr)
			}
			continue
		}
		now := time.Now().UTC()
		if d.Now != nil {
			now = d.Now().UTC()
		}
		if err := d.Store.MarkDispatched(ctx, event.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (d Dispatcher) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := d.RunOnce(ctx); err != nil {
			logger.ErrorContext(ctx, "outbox dispatch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
