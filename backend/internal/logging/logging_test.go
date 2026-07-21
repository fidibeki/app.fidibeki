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
	for _, name := range []string{"email", "text", "storage_key", "token"} {
		if entry[name] != "[REDACTED]" {
			t.Errorf("%s = %v, want [REDACTED]", name, entry[name])
		}
	}
}
