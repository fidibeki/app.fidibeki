package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/email"
	"github.com/hibiken/asynq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestWorkerValidatesConstruction(t *testing.T) {
	t.Parallel()
	handler := func(context.Context, *asynq.Task) error { return nil }
	valid := Options{
		RedisURL: "redis://localhost:6379/0", EmailConcurrency: 2, AIConcurrency: 3,
		Sender: testSender{}, PipelineHandler: handler, TextHandler: handler, AttachmentHandler: handler,
		FileDeleteHandler: handler, AIErrorHandler: asynq.ErrorHandlerFunc(func(context.Context, *asynq.Task, error) {}),
		ReporterHandler: handler, ReplayMergeHandler: handler, IntegrationHandler: handler,
	}
	if worker, err := New(valid); err != nil || worker == nil {
		t.Fatalf("New(valid) = %#v, %v", worker, err)
	}
	invalid := valid
	invalid.AIConcurrency = 0
	if _, err := New(invalid); err == nil {
		t.Error("New(zero AI concurrency) error = nil")
	}
	invalid = valid
	invalid.PipelineHandler = nil
	if _, err := New(invalid); err == nil {
		t.Error("New(nil handler) error = nil")
	}
	invalid = valid
	invalid.Sender = nil
	if _, err := New(invalid); err == nil {
		t.Error("New(nil sender) error = nil")
	}
	invalid = valid
	invalid.RedisURL = "://bad"
	if _, err := New(invalid); err == nil {
		t.Error("New(invalid Redis URL) error = nil")
	}
}

func TestWorkerStartsEveryQueueAndStops(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Redis integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	container, err := testcontainers.Run(ctx, "redis:7.4-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Ready to accept connections").WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("start Redis container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("resolve Redis host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("resolve Redis port: %v", err)
	}
	handler := func(context.Context, *asynq.Task) error { return nil }
	worker, err := New(Options{
		RedisURL:         fmt.Sprintf("redis://%s:%s/0", host, port.Port()),
		EmailConcurrency: 1, AIConcurrency: 1, Sender: testSender{},
		PipelineHandler: handler, TextHandler: handler, AttachmentHandler: handler,
		FileDeleteHandler: handler, AIErrorHandler: asynq.ErrorHandlerFunc(func(context.Context, *asynq.Task, error) {}),
		ReporterHandler: handler, ReplayMergeHandler: handler, IntegrationHandler: handler,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runCtx, stop := context.WithCancel(ctx)
	stop()
	if err := worker.Run(runCtx); err != nil {
		t.Errorf("Run(cancelled) error = %v", err)
	}
}

type testSender struct{}

func (testSender) Send(context.Context, email.Message) (string, error) {
	return "message", nil
}
