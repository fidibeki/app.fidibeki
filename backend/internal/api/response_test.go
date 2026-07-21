package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
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
	var envelope api.ErrorEnvelope
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error.Code != api.CodeValidation || envelope.Error.Message != "invalid request" {
		t.Errorf("error = %#v", envelope.Error)
	}
}
