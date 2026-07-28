package privacy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestQueueBuildsDeterministicExportTask(t *testing.T) {
	t.Parallel()
	client := &privacyTaskClientStub{}
	queue := NewQueue(client)
	job := PreparedJob{ID: "job-id", TenantID: "tenant-id", Kind: JobWorkspaceExport, Generation: 4}

	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if client.task == nil || client.task.Type() != TaskExport ||
		!strings.Contains(string(client.task.Payload()), `"job_id":"job-id"`) ||
		!strings.Contains(string(client.task.Payload()), `"tenant_id":"tenant-id"`) ||
		!strings.Contains(string(client.task.Payload()), `"generation":4`) {
		t.Fatalf("task = %#v payload = %s", client.task, client.task.Payload())
	}
	options := map[asynq.OptionType]any{}
	for _, option := range client.options {
		options[option.Type()] = option.Value()
	}
	if options[asynq.TaskIDOpt] != "privacy:export:job-id:4" ||
		options[asynq.QueueOpt] != QueueName ||
		options[asynq.MaxRetryOpt] != 3 ||
		options[asynq.TimeoutOpt] != 30*time.Minute ||
		options[asynq.RetentionOpt] != 24*time.Hour {
		t.Errorf("options = %#v", options)
	}

	client.err = asynq.ErrTaskIDConflict
	if err := queue.Enqueue(context.Background(), job); err != nil {
		t.Errorf("duplicate Enqueue() error = %v", err)
	}
	client.err = errors.New("redis unavailable")
	if err := queue.Enqueue(context.Background(), job); err == nil || !strings.Contains(err.Error(), "enqueue privacy job") {
		t.Errorf("failed Enqueue() error = %v", err)
	}
	client.err = nil
	if err := queue.Close(); err != nil || !client.closed {
		t.Errorf("Close() = %v, closed = %t", err, client.closed)
	}
	client.closeErr = errors.New("close failed")
	if err := queue.Close(); err == nil || !strings.Contains(err.Error(), "close privacy queue") {
		t.Errorf("failed Close() error = %v", err)
	}
}

func TestOpenQueueValidatesRedisURL(t *testing.T) {
	t.Parallel()
	if _, err := OpenQueue("://invalid"); err == nil {
		t.Error("OpenQueue(invalid) error = nil")
	}
	queue, err := OpenQueue("rediss://redis.fidibeki.test:6380/0")
	if err != nil {
		t.Fatalf("OpenQueue(valid) error = %v", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

type privacyTaskClientStub struct {
	task     *asynq.Task
	options  []asynq.Option
	err      error
	closed   bool
	closeErr error
}

func (client *privacyTaskClientStub) EnqueueContext(
	_ context.Context,
	task *asynq.Task,
	options ...asynq.Option,
) (*asynq.TaskInfo, error) {
	client.task = task
	client.options = options
	return &asynq.TaskInfo{}, client.err
}

func (client *privacyTaskClientStub) Close() error {
	client.closed = true
	return client.closeErr
}
