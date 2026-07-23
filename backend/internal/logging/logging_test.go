package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/logging"
)

func TestLoggerRedactsPIIAndSecrets(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.New(&output, slog.LevelDebug)
	logger.Info("feedback received",
		"tenant_id", "tenant-1",
		"email", "person@example.test",
		"text", "private feedback",
		"page_url", "https://example.test/account/person@example.test?token=secret",
		"referrer", "https://external.test/search?q=person@example.test",
		"user_agent", "PrivateBrowser account-48291",
		"element_selector", "#person-account-48291",
		"dom_snapshot_html", `<div data-email="person@example.test">private</div>`,
		"dom_snapshot_styles", `{"background":"url(https://private.test/avatar.png)"}`,
		"storage_key", "tenants/tenant-1/private.wav",
		"token", "secret-token",
	)

	var entry map[string]any
	if err := json.NewDecoder(&output).Decode(&entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if entry["tenant_id"] != "tenant-1" {
		t.Errorf("tenant_id = %v, want tenant-1", entry["tenant_id"])
	}
	for _, name := range []string{
		"email", "text", "page_url", "referrer", "user_agent", "element_selector",
		"dom_snapshot_html", "dom_snapshot_styles", "storage_key", "token",
	} {
		if entry[name] != "[REDACTED]" {
			t.Errorf("%s = %v, want [REDACTED]", name, entry[name])
		}
	}
}

func TestAsynqLoggerWritesStructuredLevels(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := logging.NewAsynq(logging.New(&output, slog.LevelDebug))
	logger.Debug("worker ", 1)
	logger.Info("started")
	logger.Warn("delayed")
	logger.Error("failed")

	want := []struct {
		level   string
		message string
	}{
		{level: "DEBUG", message: "worker 1"},
		{level: "INFO", message: "started"},
		{level: "WARN", message: "delayed"},
		{level: "ERROR", message: "failed"},
	}
	decoder := json.NewDecoder(&output)
	for index, expected := range want {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			t.Fatalf("decode log entry %d: %v", index, err)
		}
		if entry["level"] != expected.level {
			t.Errorf("entry %d level = %v, want %s", index, entry["level"], expected.level)
		}
		if entry["msg"] != expected.message {
			t.Errorf("entry %d msg = %v, want %s", index, entry["msg"], expected.message)
		}
		if entry["component"] != "asynq" {
			t.Errorf("entry %d component = %v, want asynq", index, entry["component"])
		}
	}
}
