package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
)

func TestResponseBufferDefersHeadersStatusAndBody(t *testing.T) {
	t.Parallel()

	buffer := httpx.NewResponseBuffer()
	buffer.Header().Set("X-Test", "buffered")
	buffer.WriteHeader(http.StatusCreated)
	buffer.WriteHeader(http.StatusNoContent)
	if _, err := buffer.Write([]byte("created")); err != nil {
		t.Fatalf("write buffer: %v", err)
	}
	if buffer.Status() != http.StatusCreated {
		t.Fatalf("status = %d, want %d", buffer.Status(), http.StatusCreated)
	}

	recorder := httptest.NewRecorder()
	buffer.FlushTo(recorder)
	if recorder.Code != http.StatusCreated || recorder.Body.String() != "created" {
		t.Errorf("flushed response = %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Test") != "buffered" {
		t.Error("buffered header was not flushed")
	}
}

func TestResponseBufferDefaultsToOK(t *testing.T) {
	t.Parallel()
	buffer := httpx.NewResponseBuffer()
	if buffer.Status() != http.StatusOK {
		t.Fatalf("status = %d, want 200", buffer.Status())
	}
}
