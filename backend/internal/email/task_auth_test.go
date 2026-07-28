package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
)

func TestEmailTaskHandlerRequiresValidSignedEnvelope(t *testing.T) {
	t.Parallel()
	currentKey := encodedTaskKey("current")
	previousKey := encodedTaskKey("previous")
	authenticator, err := NewTaskAuthenticator(currentKey, previousKey)
	if err != nil {
		t.Fatalf("NewTaskAuthenticator() error = %v", err)
	}
	sender := &fakeSender{}
	handler := sendEmailHandler(sender, authenticator)

	validPayload, err := authenticator.Encode(validMessage())
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if err := handler(
		context.Background(), asynq.NewTask(TaskSendEmail, validPayload),
	); err != nil {
		t.Fatalf("handler(valid) error = %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(sender.messages))
	}

	rawPayload, err := json.Marshal(validMessage())
	if err != nil {
		t.Fatalf("marshal raw payload: %v", err)
	}
	assertRejectedEmailTask(t, handler, rawPayload)
	tamperedPayload := bytes.Replace(
		validPayload, []byte("admin@example.com"), []byte("other@example.com"), 1,
	)
	assertRejectedEmailTask(t, handler, tamperedPayload)
	if len(sender.messages) != 1 {
		t.Fatalf("sent messages after forged tasks = %d, want 1", len(sender.messages))
	}

	previousAuthenticator, err := NewTaskAuthenticator(previousKey, "")
	if err != nil {
		t.Fatalf("previous NewTaskAuthenticator() error = %v", err)
	}
	previousPayload, err := previousAuthenticator.Encode(validMessage())
	if err != nil {
		t.Fatalf("previous Encode() error = %v", err)
	}
	if err := handler(
		context.Background(), asynq.NewTask(TaskSendEmail, previousPayload),
	); err != nil {
		t.Fatalf("handler(previous key) error = %v", err)
	}
	if len(sender.messages) != 2 {
		t.Fatalf("sent messages after previous key = %d, want 2", len(sender.messages))
	}
}

func TestTaskAuthenticatorValidatesSigningKeys(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		current  string
		previous string
		want     string
	}{
		{name: "missing current", want: "current signing key is required"},
		{name: "invalid current", current: "not-base64", want: "current signing key"},
		{
			name: "short current", current: base64.StdEncoding.EncodeToString([]byte("short")),
			want: "current signing key",
		},
		{
			name: "invalid previous", current: encodedTaskKey("current"), previous: "not-base64",
			want: "previous signing key",
		},
		{
			name: "same keys", current: encodedTaskKey("current"), previous: encodedTaskKey("current"),
			want: "previous signing key must differ",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewTaskAuthenticator(test.current, test.previous); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewTaskAuthenticator() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestZeroTaskAuthenticatorCannotEncodeOrDecode(t *testing.T) {
	t.Parallel()
	configured, err := NewTaskAuthenticator(encodedTaskKey("current"), "")
	if err != nil {
		t.Fatalf("NewTaskAuthenticator() error = %v", err)
	}
	payload, err := configured.Encode(validMessage())
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var zero TaskAuthenticator
	if _, err := zero.Encode(validMessage()); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("zero Encode() error = %v", err)
	}
	if _, err := zero.Decode(payload); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("zero Decode() error = %v", err)
	}
}

func assertRejectedEmailTask(t *testing.T, handler asynq.HandlerFunc, payload []byte) {
	t.Helper()
	err := handler(context.Background(), asynq.NewTask(TaskSendEmail, payload))
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("handler(forged) error = %v, want SkipRetry", err)
	}
}

func encodedTaskKey(seed string) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte(seed), 32/len(seed)+1)[:32])
}
