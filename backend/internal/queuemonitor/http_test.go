package queuemonitor_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/queuemonitor"
	"github.com/hibiken/asynq"
)

func TestCurrentAsynqConnectionConstructsWithoutPanic(t *testing.T) {
	t.Parallel()
	handler, err := queuemonitor.New(asynq.RedisClientOpt{Addr: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := handler.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestQueueMonitorRendersEscapedReadOnlyOperationsData(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 21, 15, 0, 0, 0, time.UTC)
	inspector := &fakeInspector{
		queues: []string{"email"},
		info:   &asynq.QueueInfo{Queue: "email", Pending: 1, Retry: 1, Failed: 2, Latency: 3 * time.Second},
		retry:  []*asynq.TaskInfo{{ID: "task-1", Type: "email.send", Retried: 2, MaxRetry: 5, LastErr: "<script>alert(1)</script>", NextProcessAt: now.Add(time.Minute)}},
	}
	handler := queuemonitor.NewWithInspector(inspector)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/queues/?queue=email", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"Queue operations", "email.send", "task-1", "Read only", "3s",
		`href="/admin/operations/privacy-incidents"`, "Privacy incidents",
		"&lt;script&gt;alert(1)&lt;/script&gt;",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("body missing %q", expected)
		}
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("task error was not HTML escaped")
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "form-action 'none'") {
		t.Errorf("CSP = %q", recorder.Header().Get("Content-Security-Policy"))
	}
}

func TestQueueMonitorReportsInspectorFailureWithoutDetails(t *testing.T) {
	t.Parallel()
	handler := queuemonitor.NewWithInspector(&fakeInspector{err: errors.New("redis://secret@redis:6379")})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/queues/", nil))
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "secret") {
		t.Errorf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestQueueMonitorExposesReadOnlyDashboardJSON(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 21, 15, 0, 0, 0, time.UTC)
	handler := queuemonitor.NewWithInspector(&fakeInspector{
		queues: []string{"email"},
		info:   &asynq.QueueInfo{Queue: "email", Pending: 3, Active: 1, Latency: 2 * time.Second},
		retry:  []*asynq.TaskInfo{{ID: "task-1", Type: "email.send", Retried: 1, MaxRetry: 5, NextProcessAt: now.Add(time.Minute)}},
	})
	recorder := httptest.NewRecorder()
	handler.ServeBackofficeJSON(recorder, httptest.NewRequest(http.MethodGet, "/admin/backoffice/queues?queue=email", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q", contentType)
	}
	var response struct {
		Dashboard struct {
			SelectedQueue string `json:"selected_queue"`
			TotalPending  int    `json:"total_pending"`
			Tasks         []struct {
				Type string `json:"type"`
			} `json:"tasks"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Dashboard.SelectedQueue != "email" || response.Dashboard.TotalPending != 3 || len(response.Dashboard.Tasks) != 1 || response.Dashboard.Tasks[0].Type != "email.send" {
		t.Errorf("dashboard = %#v", response.Dashboard)
	}
}

type fakeInspector struct {
	queues []string
	info   *asynq.QueueInfo
	retry  []*asynq.TaskInfo
	err    error
}

func (f *fakeInspector) Queues() ([]string, error)                     { return f.queues, f.err }
func (f *fakeInspector) GetQueueInfo(string) (*asynq.QueueInfo, error) { return f.info, f.err }
func (f *fakeInspector) ListPendingTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return nil, f.err
}
func (f *fakeInspector) ListActiveTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return nil, f.err
}
func (f *fakeInspector) ListScheduledTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return nil, f.err
}
func (f *fakeInspector) ListRetryTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return f.retry, f.err
}
func (f *fakeInspector) ListArchivedTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error) {
	return nil, f.err
}
