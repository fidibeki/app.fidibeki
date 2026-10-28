package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
)

func TestRecoveryReplacesPartialResponsesWithAnInternalError(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := appmiddleware.RequestID(appmiddleware.Recovery(logger)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("partial response"))
			panic("sensitive panic value")
		},
	)))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.Code != "internal_error" {
		t.Errorf("error.code = %q, want internal_error", response.Error.Code)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("partial response")) {
		t.Error("response contains bytes written before panic")
	}
	if bytes.Contains(logs.Bytes(), []byte("sensitive panic value")) {
		t.Error("logs contain the panic value")
	}
}

func TestRecoveryReportsPanicAndReturnsSentryEventID(t *testing.T) {
	t.Parallel()

	reporter := panicReporterFunc(func(_ context.Context, recovered any) string {
		if recovered != "boom" {
			t.Errorf("recovered value = %#v, want boom", recovered)
		}
		return "54f7658f2f0b4fdca5d6f0cb96f4f0f2"
	})
	handler := appmiddleware.RequestID(appmiddleware.Recovery(
		slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), reporter,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))

	var response struct {
		Error struct {
			SentryEventID string `json:"sentry_event_id"`
		} `json:"error"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error.SentryEventID != "54f7658f2f0b4fdca5d6f0cb96f4f0f2" {
		t.Errorf("sentry_event_id = %q", response.Error.SentryEventID)
	}
}

type panicReporterFunc func(context.Context, any) string

func TestRecoveryFlushesExplicitStreamsAndAbortsPanicsAfterCommit(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	router := chi.NewRouter()
	router.Use(appmiddleware.RequestLogger(logger))
	router.Use(appmiddleware.Recovery(logger))
	router.Get("/stream", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: first\n\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("data: second\n\n"))
		panic("after streaming began")
	})
	recorder := httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Errorf("panic = %v", recovered)
		}
		if !recorder.Flushed || recorder.Body.String() != "data: first\n\ndata: second\n\n" {
			t.Errorf("stream = %q, flushed = %v", recorder.Body.String(), recorder.Flushed)
		}
	}()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/stream", nil))
}

func (f panicReporterFunc) CapturePanic(ctx context.Context, recovered any) string {
	return f(ctx, recovered)
}
