package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/api"
)

func TestWriteErrorUsesTheStableEnvelope(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	api.WriteError(recorder, http.StatusUnprocessableEntity, api.CodeValidation, "invalid request", []string{"rating"})

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	raw := recorder.Body.String()
	var envelope api.ErrorEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error.Code != api.CodeValidation || envelope.Error.Message != "invalid request" {
		t.Errorf("error = %#v", envelope.Error)
	}
	if strings.Contains(raw, "reason") || strings.Contains(raw, "retry_after_seconds") {
		t.Errorf("optional public fields leaked into existing envelope: %s", raw)
	}
}

func TestWriteErrorWithOptionsAddsPublicDecision(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	api.WriteErrorWithOptions(
		recorder,
		http.StatusTooManyRequests,
		api.CodeRateLimitExceeded,
		"too many attempts",
		nil,
		api.ErrorOptions{Reason: "submission_rate_limited", RetryAfterSeconds: 42},
	)

	var envelope api.ErrorEnvelope
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error.Reason != "submission_rate_limited" ||
		envelope.Error.RetryAfterSeconds != 42 {
		t.Errorf("public decision = %#v", envelope.Error)
	}
}
