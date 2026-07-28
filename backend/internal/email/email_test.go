package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func validMessage() Message {
	return Message{
		From: "Fidibeki <auth@fidibeki.com>", To: "admin@example.com",
		Subject: "Sign in to Fidibeki", Text: "Open the sign-in link.",
	}
}

func TestCaptureSenderWritesPrivateJSON(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	capturedAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	id, err := (CaptureSender{Directory: directory, Now: func() time.Time { return capturedAt }}).Send(
		context.Background(), validMessage(),
	)
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
	if captured.ID != id || captured.To != "admin@example.com" || !captured.CapturedAt.Equal(capturedAt) {
		t.Errorf("captured = %#v", captured)
	}
}

func TestCaptureSenderRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := (CaptureSender{}).Send(context.Background(), validMessage()); err == nil ||
		!strings.Contains(err.Error(), "directory is required") {
		t.Fatalf("Send() missing directory error = %v", err)
	}
	if _, err := (CaptureSender{Directory: t.TempDir()}).Send(context.Background(), Message{}); err == nil ||
		!strings.Contains(err.Error(), "validate captured email") {
		t.Fatalf("Send() invalid message error = %v", err)
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}
	if _, err := (CaptureSender{Directory: filepath.Join(blocked, "capture")}).Send(
		context.Background(), validMessage(),
	); err == nil || !strings.Contains(err.Error(), "create capture email directory") {
		t.Fatalf("Send() blocked directory error = %v", err)
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
	id, err := (ResendSender{APIKey: "resend-key", Endpoint: server.URL}).Send(
		context.Background(), message,
	)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if id != "resend-message-id" {
		t.Errorf("message ID = %q", id)
	}
}

func TestResendSenderReportsProviderAndResponseFailures(t *testing.T) {
	t.Parallel()

	message := validMessage()
	tests := []struct {
		name      string
		sender    ResendSender
		message   Message
		wantError string
	}{
		{name: "invalid message", sender: ResendSender{APIKey: "key"}, message: Message{}, wantError: "validate Resend email"},
		{name: "missing API key", sender: ResendSender{}, message: message, wantError: "API key is required"},
		{name: "invalid endpoint", sender: ResendSender{APIKey: "key", Endpoint: "://bad"}, message: message, wantError: "create Resend request"},
		{name: "transport failure", sender: resendSenderWithResponse(nil, errors.New("network down")), message: message, wantError: "send Resend email"},
		{name: "response read failure", sender: resendSenderWithResponse(&http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Body: failingReadCloser{}, Header: make(http.Header),
		}, nil), message: message, wantError: "read Resend response"},
		{name: "provider rejection", sender: resendSenderWithResponse(resendResponse(
			http.StatusServiceUnavailable, "temporarily unavailable",
		), nil), message: message, wantError: "503 Service Unavailable"},
		{name: "malformed response", sender: resendSenderWithResponse(resendResponse(
			http.StatusOK, "not-json",
		), nil), message: message, wantError: "decode Resend response"},
		{name: "missing message ID", sender: resendSenderWithResponse(resendResponse(
			http.StatusOK, `{}`,
		), nil), message: message, wantError: "did not include a message ID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := test.sender.Send(context.Background(), test.message)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Send() error = %v, want %q", err, test.wantError)
			}
		})
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
	idempotent := validMessage()
	idempotent.IdempotencyKey = "email-attempt"
	if err := queue.Enqueue(context.Background(), idempotent); err != nil {
		t.Fatalf("Enqueue(idempotent) error = %v", err)
	}
	if err := queue.Close(); err != nil || !client.closed {
		t.Errorf("Close() = %v, closed = %t", err, client.closed)
	}
	client.enqueueErr = asynq.ErrTaskIDConflict
	if err := queue.Enqueue(context.Background(), validMessage()); err != nil {
		t.Errorf("Enqueue(conflict) error = %v", err)
	}
	client.enqueueErr = errors.New("redis unavailable")
	if err := queue.Enqueue(context.Background(), validMessage()); err == nil || !strings.Contains(err.Error(), "enqueue email") {
		t.Errorf("Enqueue(provider error) = %v", err)
	}
	if err := queue.Enqueue(context.Background(), Message{}); err == nil || !strings.Contains(err.Error(), "validate queued email") {
		t.Errorf("Enqueue(invalid message) = %v", err)
	}
	client.closeErr = errors.New("close failed")
	if err := queue.Close(); !errors.Is(err, client.closeErr) {
		t.Errorf("Close(error) = %v", err)
	}
	if err := RegisterHandlers(nil, &fakeSender{}); err == nil {
		t.Error("RegisterHandlers(nil mux) error = nil")
	}
	if err := RegisterHandlers(asynq.NewServeMux(), nil); err == nil {
		t.Error("RegisterHandlers(nil sender) error = nil")
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
	sender.err = errors.New("provider unavailable")
	if err := handler(context.Background(), asynq.NewTask(TaskSendEmail, payload)); err == nil ||
		!strings.Contains(err.Error(), "send queued email") {
		t.Errorf("handler(provider error) = %v", err)
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
	if _, err := NewWorker("://bad", 1, &fakeSender{}); err == nil {
		t.Error("NewWorker(invalid URL) error = nil")
	}
	if worker, err := NewWorker("rediss://redis.fidibeki.test:6380/0", 1, &fakeSender{}); err != nil || worker == nil {
		t.Errorf("NewWorker(valid) = %#v, %v", worker, err)
	}
	if err := RegisterHandlers(asynq.NewServeMux(), &fakeSender{}); err != nil {
		t.Errorf("RegisterHandlers(valid) error = %v", err)
	}
}

type fakeTaskClient struct {
	task       *asynq.Task
	closed     bool
	enqueueErr error
	closeErr   error
}

func (c *fakeTaskClient) EnqueueContext(
	_ context.Context,
	task *asynq.Task,
	_ ...asynq.Option,
) (*asynq.TaskInfo, error) {
	c.task = task
	return &asynq.TaskInfo{}, c.enqueueErr
}

func (c *fakeTaskClient) Close() error {
	c.closed = true
	return c.closeErr
}

type fakeSender struct {
	messages []Message
	err      error
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReadCloser) Close() error             { return nil }

func resendSenderWithResponse(response *http.Response, err error) ResendSender {
	return ResendSender{APIKey: "key", Client: &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response, err
		}),
		Timeout: time.Second,
	}}
}

func resendResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
	}
}

func (s *fakeSender) Send(_ context.Context, message Message) (string, error) {
	s.messages = append(s.messages, message)
	return "message-id", s.err
}
