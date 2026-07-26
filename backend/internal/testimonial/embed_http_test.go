package testimonial

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
)

// The embed is loaded by a customer's own website, so every one of these headers
// is what makes a cross-origin fetch of a wall or a poster possible at all. A
// preflight that stops answering them fails the same way for every customer at
// once, and only on their site.
func TestEmbedPreflightAnswersTheCrossOriginQuestions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		mount func(chi.Router)
		path  string
	}{
		{
			name:  "wall payload",
			mount: func(r chi.Router) { RegisterPublicRoutes(r, PublicHTTPOptions{}) },
			path:  "/v1/public/walls/wl_example",
		},
		{
			name:  "media asset",
			mount: func(r chi.Router) { RegisterMediaRoutes(r, MediaHTTPOptions{}) },
			path:  "/v1/public/media/tm_example/" + testHash + ".mp4",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			router := chi.NewRouter()
			test.mount(router)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodOptions, test.path, nil))

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("preflight status = %d, want %d", recorder.Code, http.StatusNoContent)
			}
			want := map[string]string{
				"Access-Control-Allow-Origin":  "*",
				"Access-Control-Allow-Methods": "GET, OPTIONS",
				"Cross-Origin-Resource-Policy": "cross-origin",
				"X-Content-Type-Options":       "nosniff",
			}
			for header, value := range want {
				if got := recorder.Header().Get(header); got != value {
					t.Errorf("%s = %q, want %q", header, got, value)
				}
			}
		})
	}
}

const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// A collection link and a withdrawal link are both held by someone who is not
// logged in and cannot open a support ticket. What they must never receive is a
// stack trace, and what a shared cache must never keep is their draft.
func TestPublicFailuresSayNothingAndAreNotCached(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		serve func(http.ResponseWriter, *http.Request)
		want  int
	}{
		{
			name: "collection",
			serve: func(w http.ResponseWriter, r *http.Request) {
				(&CollectHTTPHandler{}).writeFailure(w, r, errors.New("connection refused on 10.0.0.4"))
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "withdrawal",
			serve: func(w http.ResponseWriter, r *http.Request) {
				(&RevokeHTTPHandler{}).writeFailure(w, r, errors.New("connection refused on 10.0.0.4"))
			},
			want: http.StatusServiceUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			test.serve(recorder, httptest.NewRequest(http.MethodPost, "/", nil))

			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if body := recorder.Body.String(); strings.Contains(body, "10.0.0.4") {
				t.Errorf("body %q leaks the internal failure", body)
			}
		})
	}
}

func TestNewTranscodeTaskCarriesTheTenantOnItsOwnQueue(t *testing.T) {
	t.Parallel()
	task, err := NewTranscodeTask("tenant-1", "media-1")
	if err != nil {
		t.Fatalf("NewTranscodeTask: %v", err)
	}
	if task.Type() != TaskTranscode {
		t.Fatalf("task type = %q, want %q", task.Type(), TaskTranscode)
	}
	var payload TranscodePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	// Without the tenant the handler has no way to scope its reads, and row
	// level security would refuse the work rather than do it to the wrong
	// workspace — but only after the job had already been dequeued.
	if payload.TenantID != "tenant-1" || payload.MediaID != "media-1" {
		t.Fatalf("payload = %#v, want the tenant and media it was built for", payload)
	}
}

// ffmpeg is slow and CPU-hungry. It runs on a queue of its own so a wall of
// video being processed cannot starve the work a person is waiting on.
func TestTranscodeHandlerRefusesToRetryAnUndecodablePayload(t *testing.T) {
	t.Parallel()
	handler := TranscodeHandler(TranscodeOptions{})
	err := handler(context.Background(), asynq.NewTask(TaskTranscode, []byte("{not json")))
	if err == nil {
		t.Fatal("an undecodable payload must fail")
	}
	// It will never become decodable, so retrying it twenty times only fills the
	// dead letter queue more slowly.
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("error %v must be marked SkipRetry", err)
	}
}
