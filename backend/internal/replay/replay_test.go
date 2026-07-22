package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/storage"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMergeBatchesStreamsOrderedJSONArray(t *testing.T) {
	storage := &memoryReplayStorage{objects: map[string][]byte{
		"batch-0": []byte(`[{"type":2},{"type":3}]`),
		"batch-1": []byte(`[{"type":4}]`),
	}}
	processor := Processor{Storage: storage}
	count, err := processor.mergeBatches(context.Background(), "merged", []Batch{
		{Index: 0, StorageKey: "batch-0", SizeBytes: int64(len(storage.objects["batch-0"])), EventsCount: 2},
		{Index: 1, StorageKey: "batch-1", SizeBytes: int64(len(storage.objects["batch-1"])), EventsCount: 1},
	})
	if err != nil {
		t.Fatalf("mergeBatches() error = %v", err)
	}
	if count != 3 || string(storage.objects["merged"]) != `[{"type":2},{"type":3},{"type":4}]` {
		t.Fatalf("merge result count = %d, body = %s", count, storage.objects["merged"])
	}
	if storage.contentTypes["merged"] != "application/json" {
		t.Errorf("merged content type = %q", storage.contentTypes["merged"])
	}
}

func TestMergeBatchesRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		count  int
		needle string
	}{
		{name: "not array", body: `{}`, count: 0, needle: "expected JSON array"},
		{name: "invalid event", body: `[{]`, count: 1, needle: "decode replay event"},
		{name: "wrong count", body: `[{"type":2}]`, count: 2, needle: "declared 2 events"},
		{name: "trailing JSON", body: `[] {}`, count: 0, needle: "unexpected trailing JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storage := &memoryReplayStorage{objects: map[string][]byte{"batch": []byte(test.body)}}
			processor := Processor{Storage: storage}
			_, err := processor.mergeBatches(context.Background(), "merged", []Batch{{
				Index: 0, StorageKey: "batch", SizeBytes: int64(len(test.body)), EventsCount: test.count,
			}})
			if err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("mergeBatches() error = %v, want containing %q", err, test.needle)
			}
		})
	}
}

func TestMergeBatchesReportsMissingObject(t *testing.T) {
	processor := Processor{Storage: &memoryReplayStorage{objects: map[string][]byte{}}}
	_, err := processor.mergeBatches(context.Background(), "merged", []Batch{{
		Index: 0, StorageKey: "missing", SizeBytes: 2, EventsCount: 1,
	}})
	if err == nil || !strings.Contains(err.Error(), "open replay batch") {
		t.Fatalf("mergeBatches() error = %v", err)
	}
}

func TestMergeBatchesReportsOutputFailure(t *testing.T) {
	body := []byte(`[{}]`)
	want := errors.New("storage unavailable")
	storage := &memoryReplayStorage{objects: map[string][]byte{"batch": body}, putErr: want}
	processor := Processor{Storage: storage}
	_, err := processor.mergeBatches(context.Background(), "merged", []Batch{{
		Index: 0, StorageKey: "batch", SizeBytes: int64(len(body)), EventsCount: 1,
	}})
	if !errors.Is(err, want) {
		t.Fatalf("mergeBatches() error = %v, want %v", err, want)
	}
}

func TestRecoverUploadedBatchesPropagatesIndeterminateStorageFailure(t *testing.T) {
	want := errors.New("storage unavailable")
	processor := Processor{
		Storage: &memoryReplayStorage{objects: map[string][]byte{}, statErr: want},
		Now:     func() time.Time { return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC) },
	}
	_, err := processor.recoverUploadedBatches(context.Background(), Session{}, []Batch{{
		Index: 0, StorageKey: "batch", SizeBytes: 2, ContentType: "application/json",
	}})
	if !errors.Is(err, want) {
		t.Fatalf("recoverUploadedBatches() error = %v, want %v", err, want)
	}
}

func TestCleanerRemovesObjectsBeforeRecords(t *testing.T) {
	store := &retentionStoreStub{candidates: []RetentionCandidate{
		{ID: "one", ObjectKeys: []string{"one-final", "one-batch"}},
		{ID: "two", ObjectKeys: []string{"two-final"}},
	}}
	storage := &objectRemoverStub{fail: map[string]error{"two-final": errors.New("storage unavailable")}}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cleaner := Cleaner{Store: store, Storage: storage, Now: func() time.Time { return now }}
	err := cleaner.CleanupOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "storage unavailable") {
		t.Fatalf("CleanupOnce() error = %v", err)
	}
	if !reflect.DeepEqual(storage.removed, []string{"one-final", "one-batch", "two-final"}) {
		t.Errorf("removed objects = %#v", storage.removed)
	}
	if !reflect.DeepEqual(store.deleted, []string{"one"}) {
		t.Errorf("deleted records = %#v", store.deleted)
	}
	if !store.now.Equal(now) || store.limit != 100 {
		t.Errorf("retention query now/limit = %v/%d", store.now, store.limit)
	}
}

func TestSessionStatus(t *testing.T) {
	now := time.Now()
	key := "replay.json"
	message := "failed"
	tests := []struct {
		name    string
		session Session
		want    string
	}{
		{name: "recording", session: Session{}, want: "recording"},
		{name: "processing", session: Session{EndedAt: &now}, want: "processing"},
		{name: "failed", session: Session{EndedAt: &now, MergeError: &message, MergeAttempts: 12}, want: "failed"},
		{name: "ready", session: Session{MergedAt: &now, StorageKey: &key}, want: "ready"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.session.Status(); got != test.want {
				t.Errorf("Status() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMergeHandlerRejectsMalformedTask(t *testing.T) {
	err := (Processor{}).MergeHandler()(context.Background(), asynq.NewTask(TaskMerge, []byte(`{`)))
	if err == nil || !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("MergeHandler() error = %v, want SkipRetry", err)
	}
}

func TestReplayQueueEnqueuesDeterministically(t *testing.T) {
	client := &taskClientStub{}
	queue := NewQueue(client)
	if err := queue.EnqueueMerge(context.Background(), "session-id"); err != nil {
		t.Fatalf("EnqueueMerge() error = %v", err)
	}
	if client.task == nil || client.task.Type() != TaskMerge ||
		string(client.task.Payload()) != `{"session_id":"session-id"}` {
		t.Fatalf("enqueued task = %#v payload %s", client.task, client.task.Payload())
	}
	client.enqueueErr = asynq.ErrTaskIDConflict
	if err := queue.EnqueueMerge(context.Background(), "session-id"); err != nil {
		t.Fatalf("EnqueueMerge(conflict) error = %v", err)
	}
	client.enqueueErr = errors.New("redis unavailable")
	if err := queue.EnqueueMerge(context.Background(), "session-id"); err == nil ||
		!strings.Contains(err.Error(), "enqueue replay merge") {
		t.Fatalf("EnqueueMerge(failure) error = %v", err)
	}
	client.closeErr = errors.New("close failed")
	if err := queue.Close(); err == nil || !strings.Contains(err.Error(), "close replay queue") {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestOpenQueueValidatesURL(t *testing.T) {
	if _, err := OpenQueue("://bad"); err == nil {
		t.Fatal("OpenQueue(invalid) error = nil")
	}
	queue, err := OpenQueue("redis://localhost:6380/0")
	if err != nil {
		t.Fatalf("OpenQueue(valid) error = %v", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSchedulerReturnsQueueFailure(t *testing.T) {
	want := errors.New("queue unavailable")
	err := (Scheduler{Queue: mergeQueueFailureStub{err: want}}).Schedule(context.Background(), "session")
	if !errors.Is(err, want) {
		t.Fatalf("Schedule() error = %v, want %v", err, want)
	}
}

func TestSchedulerReturnsStoreFailure(t *testing.T) {
	want := errors.New("database unavailable")
	err := (Scheduler{
		Store: Store{Database: errorQuerier{err: want}}, Queue: mergeQueueFailureStub{},
	}).Schedule(context.Background(), "session")
	if !errors.Is(err, want) {
		t.Fatalf("Schedule() error = %v, want %v", err, want)
	}
}

func TestDefaultClocksAndTrailingJSON(t *testing.T) {
	for name, value := range map[string]time.Time{
		"HTTP":      (&HTTPHandler{}).now(),
		"processor": (Processor{}).now(),
		"scheduler": (Scheduler{}).now(),
	} {
		if value.Location() != time.UTC {
			t.Errorf("%s clock location = %v, want UTC", name, value.Location())
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{} {}`))
	if decodeReplayJSON(recorder, request, &struct{}{}) {
		t.Fatal("decodeReplayJSON() accepted trailing JSON")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("trailing JSON status = %d", recorder.Code)
	}
}

func TestCleanerRunStopsWithContext(t *testing.T) {
	store := &retentionStoreStub{}
	cleaner := Cleaner{Store: store, Storage: &objectRemoverStub{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cleaner.Run(ctx, time.Hour); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestReplayHTTPErrorMapping(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := &HTTPHandler{logger: logger}
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	tests := []struct {
		err    error
		status int
	}{
		{err: ErrNotFound, status: http.StatusNotFound},
		{err: ErrBatchComplete, status: http.StatusConflict},
		{err: ErrSessionFinalized, status: http.StatusConflict},
		{err: ErrIncompleteBatches, status: http.StatusConflict},
		{err: ErrEventLimit, status: http.StatusUnprocessableEntity},
		{err: errors.New("database unavailable"), status: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		handler.writeDomainError(recorder, request, test.err)
		if recorder.Code != test.status {
			t.Errorf("writeDomainError(%v) status = %d, want %d", test.err, recorder.Code, test.status)
		}
	}
	tenantRecorder := httptest.NewRecorder()
	(&TenantHTTPHandler{logger: logger}).writeError(tenantRecorder, request, errors.New("failed"))
	if tenantRecorder.Code != http.StatusServiceUnavailable {
		t.Errorf("tenant writeError status = %d", tenantRecorder.Code)
	}
	streamRecorder := httptest.NewRecorder()
	(&StreamHTTPHandler{logger: logger}).writeError(streamRecorder, request, errors.New("failed"))
	if streamRecorder.Code != http.StatusServiceUnavailable {
		t.Errorf("stream writeError status = %d", streamRecorder.Code)
	}
	planRecorder := httptest.NewRecorder()
	if handler.requireReplayPlan(planRecorder, project.PublicConfiguration{Plan: tenant.Plan("unknown")}) {
		t.Fatal("requireReplayPlan(unknown) = true")
	}
	if planRecorder.Code != http.StatusServiceUnavailable {
		t.Errorf("unknown plan status = %d", planRecorder.Code)
	}
}

func TestStreamTenantResolutionFailsClosed(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Hour)
	deletionRequestedAt := now.Add(-24 * time.Hour)
	active := tenant.Tenant{ID: "tenant-id", Slug: "acme", Status: tenant.StatusActive}
	tests := []struct {
		name     string
		host     string
		resolved tenant.Tenant
		resolve  error
		status   int
		ok       bool
	}{
		{name: "invalid host", host: "localhost", status: http.StatusNotFound},
		{name: "unknown tenant", host: "missing.localhost", resolve: tenant.ErrUnknownTenant, status: http.StatusNotFound},
		{name: "resolver unavailable", host: "acme.localhost", resolve: errors.New("database failed"), status: http.StatusServiceUnavailable},
		{name: "suspended", host: "acme.localhost", resolved: tenant.Tenant{ID: "tenant-id", Status: tenant.StatusSuspended}, status: http.StatusForbidden},
		{name: "suspended pending deletion", host: "acme.localhost", resolved: tenant.Tenant{ID: "tenant-id", Status: tenant.StatusSuspended, DeletionRequestedAt: &deletionRequestedAt}, status: http.StatusOK, ok: true},
		{name: "expired trial pending deletion", host: "acme.localhost", resolved: tenant.Tenant{ID: "tenant-id", Status: tenant.StatusTrial, TrialEndsAt: &expired, DeletionRequestedAt: &deletionRequestedAt}, status: http.StatusOK, ok: true},
		{name: "allowed", host: "acme.localhost", resolved: active, status: http.StatusOK, ok: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &StreamHTTPHandler{
				options: StreamHTTPOptions{
					BaseDomain: "localhost",
					Resolver:   tenantResolverStub{resolved: test.resolved, err: test.resolve},
					Now:        func() time.Time { return now },
				},
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			request := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/replay", nil)
			request.Host = test.host
			recorder := httptest.NewRecorder()
			resolved, ok := handler.resolveTenant(recorder, request)
			if ok != test.ok || (ok && resolved.ID != active.ID) {
				t.Errorf("resolveTenant() = %#v/%t, want ok %t", resolved, ok, test.ok)
			}
			if !test.ok && recorder.Code != test.status {
				t.Errorf("resolveTenant status = %d, want %d", recorder.Code, test.status)
			}
		})
	}
}

func TestPublicReplayAuthorizationReportsStoreFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	request := httptest.NewRequest(http.MethodPost, "http://widget.local/v1/replay/sessions?key=key", nil)
	request.Header.Set("Origin", "https://example.com")
	handler := &HTTPHandler{options: HTTPOptions{
		Authorizer: projectAuthorizerStub{err: errors.New("database failed")},
	}, logger: logger}
	recorder := httptest.NewRecorder()
	if _, _, ok := handler.authorize(recorder, request, "key", true); ok {
		t.Fatal("authorize() ok = true")
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("authorize status = %d", recorder.Code)
	}
	handler.options.Authorizer = projectAuthorizerStub{err: widgetapi.ErrAccessDenied}
	recorder = httptest.NewRecorder()
	if _, _, ok := handler.authorize(recorder, request, "key", true); ok {
		t.Fatal("authorize(access denied) ok = true")
	}
	if recorder.Code != http.StatusForbidden {
		t.Errorf("authorize access-denied status = %d", recorder.Code)
	}
}

type memoryReplayStorage struct {
	objects      map[string][]byte
	contentTypes map[string]string
	removed      []string
	putErr       error
	statErr      error
}

func (s *memoryReplayStorage) Stat(_ context.Context, key string) (storage.ObjectInfo, error) {
	if s.statErr != nil {
		return storage.ObjectInfo{}, s.statErr
	}
	value, exists := s.objects[key]
	if !exists {
		return storage.ObjectInfo{}, storage.ErrObjectNotFound
	}
	return storage.ObjectInfo{Size: int64(len(value)), ContentType: s.contentTypes[key]}, nil
}

func (s *memoryReplayStorage) OpenObject(_ context.Context, key string) (io.ReadCloser, error) {
	value, exists := s.objects[key]
	if !exists {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (s *memoryReplayStorage) PutObject(
	_ context.Context,
	key string,
	reader io.Reader,
	_ int64,
	contentType string,
) error {
	if s.putErr != nil {
		return s.putErr
	}
	value, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	if s.contentTypes == nil {
		s.contentTypes = map[string]string{}
	}
	s.objects[key] = value
	s.contentTypes[key] = contentType
	return nil
}

func (s *memoryReplayStorage) RemoveObject(_ context.Context, key string) error {
	s.removed = append(s.removed, key)
	delete(s.objects, key)
	return nil
}

type retentionStoreStub struct {
	candidates []RetentionCandidate
	deleted    []string
	now        time.Time
	limit      int
}

func (s *retentionStoreStub) ExpiredSessions(_ context.Context, now time.Time, limit int) ([]RetentionCandidate, error) {
	s.now = now
	s.limit = limit
	return s.candidates, nil
}

func (s *retentionStoreStub) DeleteExpiredSession(_ context.Context, sessionID string) error {
	s.deleted = append(s.deleted, sessionID)
	return nil
}

type objectRemoverStub struct {
	removed []string
	fail    map[string]error
}

func (s *objectRemoverStub) RemoveObject(_ context.Context, key string) error {
	s.removed = append(s.removed, key)
	return s.fail[key]
}

type taskClientStub struct {
	task       *asynq.Task
	enqueueErr error
	closeErr   error
}

func (s *taskClientStub) EnqueueContext(
	_ context.Context,
	task *asynq.Task,
	_ ...asynq.Option,
) (*asynq.TaskInfo, error) {
	s.task = task
	return &asynq.TaskInfo{}, s.enqueueErr
}

func (s *taskClientStub) Close() error {
	return s.closeErr
}

type tenantResolverStub struct {
	resolved tenant.Tenant
	err      error
}

func (s tenantResolverStub) Resolve(context.Context, string) (tenant.Tenant, error) {
	return s.resolved, s.err
}

type projectAuthorizerStub struct {
	configuration project.PublicConfiguration
	err           error
}

func (s projectAuthorizerStub) Authorize(context.Context, string, string) (project.PublicConfiguration, error) {
	return s.configuration, s.err
}

type mergeQueueFailureStub struct {
	err error
}

func (s mergeQueueFailureStub) EnqueueMerge(context.Context, string) error {
	return s.err
}

type errorQuerier struct {
	err error
}

func (s errorQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, s.err
}

func (errorQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (errorQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}
