package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
		RedisURL: "rediss://redis.fidibeki.test:6380/0", EmailConcurrency: 2, AIConcurrency: 3,
		DomainConcurrency: 4, MediaConcurrency: 2, PrivacyConcurrency: 1,
		Logger: &testAsynqLogger{},
		Sender: testSender{}, PipelineHandler: handler, TextHandler: handler, AttachmentHandler: handler,
		FileDeleteHandler: handler, AIErrorHandler: asynq.ErrorHandlerFunc(func(context.Context, *asynq.Task, error) {}),
		ReporterHandler: handler, RecipientHandler: handler,
		CaseCommunicationHandler: handler, ControllerNotificationHandler: handler,
		LifecycleHandler:   handler,
		ReplayMergeHandler: handler, ReplayVerificationHandler: handler,
		IntegrationHandler:            handler,
		IssueTrackerHandler:           handler,
		AttachmentVerificationHandler: handler, PrivacyExportHandler: handler,
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
	invalid = valid
	invalid.Logger = nil
	if _, err := New(invalid); err == nil {
		t.Error("New(nil logger) error = nil")
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
	logger := &testAsynqLogger{}
	worker, err := New(Options{
		RedisURL:         fmt.Sprintf("redis://%s:%s/0", host, port.Port()),
		EmailConcurrency: 1, AIConcurrency: 1, Sender: testSender{},
		DomainConcurrency: 1, MediaConcurrency: 1, PrivacyConcurrency: 1,
		Logger:          logger,
		PipelineHandler: handler, TextHandler: handler, AttachmentHandler: handler,
		FileDeleteHandler: handler, AIErrorHandler: asynq.ErrorHandlerFunc(func(context.Context, *asynq.Task, error) {}),
		ReporterHandler: handler, RecipientHandler: handler,
		CaseCommunicationHandler: handler, ControllerNotificationHandler: handler,
		LifecycleHandler:   handler,
		ReplayMergeHandler: handler, ReplayVerificationHandler: handler,
		IntegrationHandler:            handler,
		IssueTrackerHandler:           handler,
		AttachmentVerificationHandler: handler, PrivacyExportHandler: handler,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runCtx, stop := context.WithCancel(ctx)
	stop()
	if err := worker.Run(runCtx); err != nil {
		t.Errorf("Run(cancelled) error = %v", err)
	}
	if !logger.contains("info:Starting processing") {
		t.Errorf("worker logs = %v, want structured logger to receive Asynq startup", logger.entries)
	}
}

type testSender struct{}

func (testSender) Send(context.Context, email.Message) (string, error) {
	return "message", nil
}

type testAsynqLogger struct {
	mu      sync.Mutex
	entries []string
}

func (l *testAsynqLogger) Debug(args ...interface{}) { l.add("debug", args...) }
func (l *testAsynqLogger) Info(args ...interface{})  { l.add("info", args...) }
func (l *testAsynqLogger) Warn(args ...interface{})  { l.add("warn", args...) }
func (l *testAsynqLogger) Error(args ...interface{}) { l.add("error", args...) }
func (l *testAsynqLogger) Fatal(args ...interface{}) { l.add("fatal", args...) }

func (l *testAsynqLogger) add(level string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, level+":"+fmt.Sprint(args...))
}

func (l *testAsynqLogger) contains(value string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, entry := range l.entries {
		if strings.Contains(entry, value) {
			return true
		}
	}
	return false
}
