package outbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestDispatcherRetriesPartialFanoutWithDeterministicIDs(t *testing.T) {
	t.Parallel()
	event := Event{
		ID: "event-id", TenantID: "tenant-id", Event: "feedback.created",
		ResourceType: "feedback", ResourceID: "feedback-id",
	}
	store := &eventStoreStub{events: []Event{event}}
	resolver := destinationResolverStub{destinations: []Destination{
		{Kind: "pipeline", ID: "feedback-id", TaskType: "feedback:pipeline"},
		{Kind: "webhook", ID: "integration-id", TaskType: "integration:deliver"},
	}}
	enqueuer := &enqueuerStub{failAt: 2}
	dispatcher := Dispatcher{
		Store: store, Resolver: resolver, Enqueuer: enqueuer,
		Now: func() time.Time { return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC) },
	}
	if err := dispatcher.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(first) error = %v", err)
	}
	if store.failed != 1 || store.dispatched != 0 {
		t.Fatalf("first store state = failed %d dispatched %d", store.failed, store.dispatched)
	}
	enqueuer.failAt = 0
	if err := dispatcher.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(second) error = %v", err)
	}
	if store.dispatched != 1 || len(enqueuer.taskIDs) != 4 {
		t.Fatalf("second store state = dispatched %d, tasks = %#v", store.dispatched, enqueuer.taskIDs)
	}
	if enqueuer.taskIDs[0] != enqueuer.taskIDs[2] || enqueuer.taskIDs[1] != enqueuer.taskIDs[3] {
		t.Errorf("task IDs are not deterministic: %#v", enqueuer.taskIDs)
	}
}

func TestAsynqEnqueuerBuildsDeterministicTaskAndHandlesDuplicates(t *testing.T) {
	t.Parallel()
	event := Event{ID: "event-id", TenantID: "tenant-id", ResourceID: "feedback-id"}
	destination := Destination{Kind: "pipeline", ID: "feedback-id", TaskType: TaskFeedbackPipe}
	client := &taskClientStub{}
	enqueuer := NewAsynqEnqueuer(client)
	if err := enqueuer.Enqueue(context.Background(), event, destination); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if client.task == nil || client.task.Type() != TaskFeedbackPipe ||
		!strings.Contains(string(client.task.Payload()), `"outbox_event_id":"event-id"`) ||
		!strings.Contains(string(client.task.Payload()), `"destination_id":"feedback-id"`) {
		t.Errorf("task = %#v, payload = %s", client.task, client.task.Payload())
	}
	options := map[asynq.OptionType]any{}
	for _, option := range client.options {
		options[option.Type()] = option.Value()
	}
	if options[asynq.TaskIDOpt] != "outbox:event-id:pipeline:feedback-id" ||
		options[asynq.QueueOpt] != QueueName || options[asynq.MaxRetryOpt] != 12 {
		t.Errorf("options = %#v", options)
	}
	client.err = asynq.ErrTaskIDConflict
	if err := enqueuer.Enqueue(context.Background(), event, destination); err != nil {
		t.Errorf("duplicate Enqueue() error = %v", err)
	}
	client.err = errors.New("Redis unavailable")
	if err := enqueuer.Enqueue(context.Background(), event, destination); err == nil {
		t.Error("failed Enqueue() error = nil")
	}
	client.err = nil
	destination.Queue = "email-pool"
	if err := enqueuer.Enqueue(context.Background(), event, destination); err != nil {
		t.Fatalf("Enqueue(custom queue) error = %v", err)
	}
	options = map[asynq.OptionType]any{}
	for _, option := range client.options {
		options[option.Type()] = option.Value()
	}
	if options[asynq.QueueOpt] != "email-pool" {
		t.Errorf("custom queue option = %#v", options[asynq.QueueOpt])
	}
	if err := enqueuer.Close(); err != nil || !client.closed {
		t.Errorf("Close() = %v, closed = %t", err, client.closed)
	}
	client.closeErr = errors.New("close failed")
	if err := enqueuer.Close(); err == nil {
		t.Error("failed Close() error = nil")
	}
}

func TestOpenAsynqEnqueuerValidatesRedisURL(t *testing.T) {
	t.Parallel()
	if _, err := OpenAsynqEnqueuer("://invalid"); err == nil {
		t.Error("OpenAsynqEnqueuer(invalid) error = nil")
	}
	queue, err := OpenAsynqEnqueuer("rediss://redis.fidibeki.test:6380/0")
	if err != nil {
		t.Fatalf("OpenAsynqEnqueuer(valid) error = %v", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestDispatcherRunStopsWithContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &eventStoreStub{}
	err := (Dispatcher{
		Store: store, Resolver: destinationResolverStub{}, Enqueuer: &enqueuerStub{},
	}).Run(ctx, time.Millisecond)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestFeedbackResolverBuildsPipelineDestination(t *testing.T) {
	t.Parallel()
	for _, eventType := range []string{"feedback.created", "feedback.rectified"} {
		event := Event{Event: eventType, ResourceType: "feedback", ResourceID: "feedback-id"}
		destinations, err := (FeedbackResolver{}).Resolve(context.Background(), event)
		if err != nil || len(destinations) != 1 || destinations[0].TaskType != TaskFeedbackPipe {
			t.Fatalf("%s destinations = %#v, error = %v", eventType, destinations, err)
		}
	}
	if got := TaskID(Event{ID: "event"}, Destination{Kind: "pipeline", ID: "feedback"}); got != "outbox:event:pipeline:feedback" {
		t.Errorf("TaskID() = %q", got)
	}
}

func TestMultiResolverCombinesDestinations(t *testing.T) {
	t.Parallel()
	first := destinationResolverStub{destinations: []Destination{{Kind: "first"}}}
	second := destinationResolverStub{destinations: []Destination{{Kind: "second"}}}
	destinations, err := (MultiResolver{first, second}).Resolve(context.Background(), Event{})
	if err != nil || len(destinations) != 2 || destinations[0].Kind != "first" || destinations[1].Kind != "second" {
		t.Fatalf("Resolve() = %#v, %v", destinations, err)
	}
}

type eventStoreStub struct {
	events     []Event
	failed     int
	dispatched int
}

func (s *eventStoreStub) Pending(context.Context, int) ([]Event, error) {
	return s.events, nil
}

func (s *eventStoreStub) MarkDispatched(context.Context, string, time.Time) error {
	s.dispatched++
	return nil
}

func (s *eventStoreStub) MarkFailed(context.Context, string, string) error {
	s.failed++
	return nil
}

type destinationResolverStub struct {
	destinations []Destination
}

func (s destinationResolverStub) Resolve(context.Context, Event) ([]Destination, error) {
	return s.destinations, nil
}

type enqueuerStub struct {
	calls   int
	failAt  int
	taskIDs []string
}

type taskClientStub struct {
	task     *asynq.Task
	options  []asynq.Option
	err      error
	closed   bool
	closeErr error
}

func (c *taskClientStub) EnqueueContext(
	_ context.Context,
	task *asynq.Task,
	options ...asynq.Option,
) (*asynq.TaskInfo, error) {
	c.task = task
	c.options = options
	return &asynq.TaskInfo{}, c.err
}

func (c *taskClientStub) Close() error {
	c.closed = true
	return c.closeErr
}

func (e *enqueuerStub) Enqueue(_ context.Context, event Event, destination Destination) error {
	e.calls++
	e.taskIDs = append(e.taskIDs, TaskID(event, destination))
	if e.calls == e.failAt {
		return errors.New("Redis unavailable")
	}
	return nil
}
