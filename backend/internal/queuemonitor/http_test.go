package queuemonitor_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/queuemonitor"
	"github.com/go-chi/chi/v5"
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

func TestQueueMonitorRetriesOnlyAuditedRetryTasks(t *testing.T) {
	t.Parallel()
	inspector := &fakeInspector{task: &asynq.TaskInfo{ID: "task-1", State: asynq.TaskStateRetry}}
	auditor := &captureRetryAuditor{}
	handler := queuemonitor.NewWithInspector(inspector)
	handler.SetRetryAuditor(auditor)

	if err := handler.RetryTask(context.Background(), "admin-1", "email", "task-1"); err != nil {
		t.Fatalf("RetryTask() error = %v", err)
	}
	if inspector.runQueue != "email" || inspector.runID != "task-1" {
		t.Errorf("RunTask(%q, %q), want email/task-1", inspector.runQueue, inspector.runID)
	}
	if auditor.actorID != "admin-1" || auditor.queue != "email" || auditor.taskID != "task-1" {
		t.Errorf("audit = %#v", auditor)
	}

	withoutAudit := queuemonitor.NewWithInspector(&fakeInspector{task: &asynq.TaskInfo{ID: "task-2", State: asynq.TaskStateRetry}})
	if err := withoutAudit.RetryTask(context.Background(), "admin-1", "email", "task-2"); !errors.Is(err, queuemonitor.ErrRetryAuditUnavailable) {
		t.Errorf("RetryTask(without audit) error = %v, want ErrRetryAuditUnavailable", err)
	}

	inspector.task.State = asynq.TaskStateActive
	if err := handler.RetryTask(context.Background(), "admin-1", "email", "task-1"); !errors.Is(err, queuemonitor.ErrTaskNotRetryable) {
		t.Errorf("RetryTask(active) error = %v, want ErrTaskNotRetryable", err)
	}
}

func TestQueueMonitorRetryEndpointRequiresPermissionAndAuditsCommand(t *testing.T) {
	t.Parallel()
	manager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	token, _, err := manager.Issue("admin-1", auth.AudiencePlatform, []string{string(auth.PermissionPlatformQueueRun)})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	withoutPermission, _, err := manager.Issue("admin-2", auth.AudiencePlatform, nil)
	if err != nil {
		t.Fatalf("Issue() without permission error = %v", err)
	}
	inspector := &fakeInspector{task: &asynq.TaskInfo{ID: "task-1", State: asynq.TaskStateRetry}}
	auditor := &captureRetryAuditor{}
	handler := queuemonitor.NewWithInspector(inspector)
	handler.SetRetryAuditor(auditor)
	router := chi.NewRouter()
	handler.RegisterBackofficeRoutes(router, manager)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/backoffice/queues/email/tasks/task-1/retry", nil)
	request.Header.Set("Authorization", "Bearer "+withoutPermission)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || inspector.runID != "" || auditor.taskID != "" {
		t.Fatalf("unprivileged request status = %d, inspector=%#v auditor=%#v", recorder.Code, inspector, auditor)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/admin/backoffice/queues/email/tasks/task-1/retry", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if inspector.runQueue != "email" || auditor.taskID != "task-1" {
		t.Errorf("command was not audited and run: inspector=%#v auditor=%#v", inspector, auditor)
	}

	inspector.task.State = asynq.TaskStateActive
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/admin/backoffice/queues/email/tasks/task-1/retry", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Errorf("active task status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestQueueMonitorRetryEndpointFailsClosedWithoutAuditor(t *testing.T) {
	t.Parallel()
	manager, err := auth.NewJWTManager(strings.Repeat("s", 32), "fidibeki", time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	token, _, err := manager.Issue("admin-1", auth.AudiencePlatform, []string{string(auth.PermissionPlatformQueueRun)})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	inspector := &fakeInspector{task: &asynq.TaskInfo{ID: "task-1", State: asynq.TaskStateRetry}}
	handler := queuemonitor.NewWithInspector(inspector)
	router := chi.NewRouter()
	handler.RegisterBackofficeRoutes(router, manager)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/backoffice/queues/email/tasks/task-1/retry", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || inspector.runID != "" {
		t.Errorf("status = %d run task = %q, body = %s", recorder.Code, inspector.runID, recorder.Body.String())
	}
}

type fakeInspector struct {
	queues   []string
	info     *asynq.QueueInfo
	retry    []*asynq.TaskInfo
	task     *asynq.TaskInfo
	runQueue string
	runID    string
	err      error
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
func (f *fakeInspector) GetTaskInfo(string, string) (*asynq.TaskInfo, error) { return f.task, f.err }
func (f *fakeInspector) RunTask(queue, id string) error {
	f.runQueue, f.runID = queue, id
	return f.err
}

type captureRetryAuditor struct{ actorID, queue, taskID string }

func (a *captureRetryAuditor) RecordQueueTaskRetry(_ context.Context, actorID, queue, taskID string) error {
	a.actorID, a.queue, a.taskID = actorID, queue, taskID
	return nil
}
