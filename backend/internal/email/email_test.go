package email

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
)

func validMessage() Message {
	return Message{
		From: "Fidibeki <auth@fidibeki.app>", To: "admin@example.com",
		Subject: "Sign in to Fidibeki", Text: "Open the sign-in link.",
	}
}

func TestCaptureSenderWritesPrivateJSON(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	id, err := (CaptureSender{Directory: directory}).Send(context.Background(), validMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	path := filepath.Join(directory, id+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600", info.Mode().Perm())
	}
	var captured capturedMessage
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(bytes, &captured); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	if captured.ID != id || captured.To != "admin@example.com" {
		t.Errorf("captured = %#v", captured)
	}
}

func TestResendSenderUsesAuthenticatedJSONRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer resend-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Idempotency-Key") != "email-attempt" {
			t.Errorf("Idempotency-Key = %q", r.Header.Get("Idempotency-Key"))
		}
		var payload struct {
			To []string `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(payload.To) != 1 || payload.To[0] != "admin@example.com" {
			t.Errorf("to = %#v", payload.To)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resend-message-id"}`))
	}))
	defer server.Close()

	message := validMessage()
	message.IdempotencyKey = "email-attempt"
	id, err := (ResendSender{APIKey: "resend-key", Endpoint: server.URL, Client: server.Client()}).Send(
		context.Background(), message,
	)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id != "resend-message-id" {
		t.Errorf("message ID = %q", id)
	}
}

func TestMessageValidationRejectsInvalidAddressesAndContent(t *testing.T) {
	t.Parallel()
	if err := (Message{From: "bad", To: "bad"}).Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
	message := validMessage()
	message.IdempotencyKey = strings.Repeat("x", 257)
	if err := message.Validate(); err == nil {
		t.Fatal("Validate(long idempotency key) error = nil")
	}
}

func TestQueueSerializesValidatedEmailTask(t *testing.T) {
	t.Parallel()

	client := &fakeTaskClient{}
	queue := NewQueue(client)
	if err := queue.Enqueue(context.Background(), validMessage()); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if client.task == nil || client.task.Type() != TaskSendEmail {
		t.Fatalf("task = %#v", client.task)
	}
	var decoded Message
	if err := json.Unmarshal(client.task.Payload(), &decoded); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if decoded.To != "admin@example.com" {
		t.Errorf("decoded message = %#v", decoded)
	}
	if err := queue.Close(); err != nil || !client.closed {
		t.Errorf("Close() = %v, closed = %t", err, client.closed)
	}
}

func TestEmailTaskHandlerSendsAndClassifiesMalformedPayload(t *testing.T) {
	t.Parallel()

	sender := &fakeSender{}
	handler := sendEmailHandler(sender)
	payload, _ := json.Marshal(validMessage())
	if err := handler(context.Background(), asynq.NewTask(TaskSendEmail, payload)); err != nil {
		t.Fatalf("handler(valid) error = %v", err)
	}
	if len(sender.messages) != 1 {
		t.Errorf("sent messages = %d", len(sender.messages))
	}
	err := handler(context.Background(), asynq.NewTask(TaskSendEmail, []byte("not-json")))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Errorf("handler(invalid) error = %v, want SkipRetry", err)
	}
}

func TestWorkerValidatesConstruction(t *testing.T) {
	t.Parallel()
	if _, err := NewWorker("redis://localhost:6379/0", 0, &fakeSender{}); err == nil {
		t.Error("NewWorker(zero concurrency) error = nil")
	}
	if _, err := NewWorker("redis://localhost:6379/0", 1, nil); err == nil {
		t.Error("NewWorker(nil sender) error = nil")
	}
	if _, err := OpenQueue("://bad"); err == nil {
		t.Error("OpenQueue(invalid URL) error = nil")
	}
}

type fakeTaskClient struct {
	task   *asynq.Task
	closed bool
}

func (c *fakeTaskClient) EnqueueContext(
	_ context.Context,
	task *asynq.Task,
	_ ...asynq.Option,
) (*asynq.TaskInfo, error) {
	c.task = task
	return &asynq.TaskInfo{}, nil
}

func (c *fakeTaskClient) Close() error {
	c.closed = true
	return nil
}

type fakeSender struct {
	messages []Message
	err      error
}

func (s *fakeSender) Send(_ context.Context, message Message) (string, error) {
	s.messages = append(s.messages, message)
	return "message-id", s.err
}
