package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/httpx"
)

func TestRecoverableResponseKeepsOrdinaryResponsesPrivateUntilFinished(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := httpx.NewRecoverableResponse(recorder)
	response.Header().Set("X-Result", "ready")
	response.WriteHeader(http.StatusCreated)
	if _, err := response.Write([]byte("saved")); err != nil {
		t.Fatal(err)
	}
	if response.Committed() || recorder.Body.Len() != 0 || recorder.Header().Get("X-Result") != "" {
		t.Fatal("partial response escaped the recovery buffer")
	}
	response.Finish()
	response.Finish()
	if !response.Committed() || recorder.Code != http.StatusCreated || recorder.Body.String() != "saved" || recorder.Header().Get("X-Result") != "ready" {
		t.Fatalf("finished response = %d %q %v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
}

func TestRecoverableResponseFlushDeliversFollowingChunksImmediately(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := httpx.NewRecoverableResponse(recorder)
	response.Header().Set("Content-Type", "text/event-stream")
	if _, err := response.Write([]byte("event: change\ndata: {}\n\n")); err != nil {
		t.Fatal(err)
	}
	if err := http.NewResponseController(response).Flush(); err != nil {
		t.Fatal(err)
	}
	if !recorder.Flushed || !response.Committed() || recorder.Body.Len() == 0 {
		t.Fatal("first event was buffered instead of delivered")
	}
	if _, err := response.Write([]byte("second")); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.String() != "event: change\ndata: {}\n\nsecond" {
		t.Fatal("later streaming chunk stayed buffered")
	}
	response.Finish()
	if recorder.Body.String() != "event: change\ndata: {}\n\nsecond" {
		t.Fatal("finishing a stream repeated its first event")
	}
}

type failingStreamWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
	err      error
}

func (w *failingStreamWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
func (w *failingStreamWriter) FlushError() error         { return w.err }
func (w *failingStreamWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRecoverableResponsePreservesStreamDeadlinesAndFailures(t *testing.T) {
	failure := errors.New("disconnected client")
	writer := &failingStreamWriter{ResponseRecorder: httptest.NewRecorder(), err: failure}
	response := httpx.NewRecoverableResponse(writer)
	controller := http.NewResponseController(response)
	deadline := time.Now().Add(time.Second)
	if err := controller.SetWriteDeadline(deadline); err != nil || writer.deadline != deadline {
		t.Fatalf("write deadline did not reach connection: %v", err)
	}
	if err := controller.Flush(); !errors.Is(err, failure) {
		t.Fatalf("flush error = %v", err)
	}
	if _, err := response.Write([]byte("event")); !errors.Is(err, failure) {
		t.Fatalf("stream write error = %v", err)
	}
}
