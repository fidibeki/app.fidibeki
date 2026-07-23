package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestVerificationSchedulerRollsBackFailedEnqueue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 3}
	store := &verificationSchedulingStoreStub{}
	queue := &verificationQueueStub{err: errors.New("Redis unavailable")}
	scheduler := VerificationScheduler{
		Store: store, Queue: queue, Now: func() time.Time { return now },
	}
	if err := scheduler.ScheduleVerification(context.Background(), task); err == nil {
		t.Fatal("ScheduleVerification() error = nil")
	}
	if store.marked != task || store.markedAt != now || store.undone != task {
		t.Fatalf("scheduler state = marked %#v at %v undone %#v", store.marked, store.markedAt, store.undone)
	}
}

func TestVerificationSchedulerReconcilesExpiryAndStaleWork(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	tasks := []VerificationTask{
		{TenantID: "tenant", AttachmentID: "one", Generation: 1},
		{TenantID: "tenant", AttachmentID: "two", Generation: 2},
	}
	expired := []ExpiredUpload{{
		TenantID: "tenant", AttachmentID: "expired", StorageKey: "expired-object",
	}}
	store := &verificationSchedulingStoreStub{prepared: tasks, expired: expired}
	queue := &verificationQueueStub{}
	storage := &expiryStorageStub{}
	store.removed = func() bool { return len(storage.removed) > 0 }
	scheduler := VerificationScheduler{
		Store: store, Queue: queue, Storage: storage, Now: func() time.Time { return now },
	}
	if err := scheduler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	if store.expiredAt != now || store.preparedAt != now || len(queue.tasks) != 2 {
		t.Fatalf("reconcile state = expiry %v prepare %v tasks %#v",
			store.expiredAt, store.preparedAt, queue.tasks)
	}
	if len(storage.removed) != 1 || storage.removed[0] != expired[0].StorageKey ||
		len(store.completed) != 1 || store.completed[0] != expired[0] ||
		store.completedBeforeRemoval {
		t.Fatalf("expiry cleanup = removed %#v completed %#v", storage.removed, store.completed)
	}
	if VerificationTaskID(tasks[0]) == VerificationTaskID(tasks[1]) ||
		VerificationTaskID(tasks[0]) != "attachment:verify:one:1" {
		t.Fatalf("verification task IDs = %q/%q",
			VerificationTaskID(tasks[0]), VerificationTaskID(tasks[1]))
	}
}

func TestVerificationSchedulerRetriesStorageCleanupBeforeTerminalState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	upload := ExpiredUpload{
		TenantID: "tenant", AttachmentID: "expired", StorageKey: "expired-object",
	}
	store := &verificationSchedulingStoreStub{expired: []ExpiredUpload{upload}}
	storage := &expiryStorageStub{err: errors.New("storage unavailable")}
	scheduler := VerificationScheduler{
		Store: store, Queue: &verificationQueueStub{}, Storage: storage,
		Now: func() time.Time { return now },
	}
	if err := scheduler.ReconcileOnce(context.Background()); err == nil {
		t.Fatal("ReconcileOnce(storage failure) error = nil")
	}
	if len(store.completed) != 0 {
		t.Fatalf("storage failure completed terminal state = %#v", store.completed)
	}
	storage.err = nil
	if err := scheduler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce(retry) error = %v", err)
	}
	if len(store.completed) != 1 || store.completed[0] != upload {
		t.Fatalf("retried expiry cleanup = %#v", store.completed)
	}
}

func TestVerificationQueueUsesStableTaskContract(t *testing.T) {
	t.Parallel()

	task := VerificationTask{TenantID: "tenant", AttachmentID: "attachment", Generation: 4}
	client := &verificationTaskClientStub{}
	queue := NewVerificationQueue(client)
	if err := queue.Enqueue(context.Background(), task); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if client.task == nil || client.task.Type() != TaskAttachmentVerify || client.options != 4 {
		t.Fatalf("queued task = %#v, options = %d", client.task, client.options)
	}
	var decoded VerificationTask
	if err := json.Unmarshal(client.task.Payload(), &decoded); err != nil || decoded != task {
		t.Fatalf("queued payload = %#v, error = %v", decoded, err)
	}

	client.enqueueErr = asynq.ErrTaskIDConflict
	if err := queue.Enqueue(context.Background(), task); err != nil {
		t.Fatalf("duplicate Enqueue() error = %v", err)
	}
	failure := errors.New("Redis unavailable")
	client.enqueueErr = failure
	if err := queue.Enqueue(context.Background(), task); !errors.Is(err, failure) {
		t.Fatalf("failed Enqueue() error = %v", err)
	}
	client.closeErr = failure
	if err := queue.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := OpenVerificationQueue("http://not-redis"); err == nil {
		t.Fatal("OpenVerificationQueue() error = nil")
	}
}

func TestVerificationSchedulerRunReconcilesBeforeCanceledShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &verificationSchedulingStoreStub{expiryErr: errors.New("database unavailable")}
	scheduler := VerificationScheduler{Store: store, Queue: &verificationQueueStub{}}
	if err := scheduler.Run(ctx, 0); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if store.expireCalls != 1 || store.prepareCalls != 1 {
		t.Fatalf("Run() calls = expire %d, prepare %d", store.expireCalls, store.prepareCalls)
	}
	if got := (VerificationTask{
		TenantID: "tenant", AttachmentID: "attachment", Generation: 2,
	}).String(); got != "tenant/attachment/2" {
		t.Fatalf("VerificationTask.String() = %q", got)
	}
}

type verificationSchedulingStoreStub struct {
	marked                 VerificationTask
	markedAt               time.Time
	undone                 VerificationTask
	prepared               []VerificationTask
	preparedAt             time.Time
	expired                []ExpiredUpload
	completed              []ExpiredUpload
	removed                func() bool
	completedBeforeRemoval bool
	expiredAt              time.Time
	expiryErr              error
	prepareErr             error
	expireCalls            int
	prepareCalls           int
}

func (store *verificationSchedulingStoreStub) MarkEnqueued(
	_ context.Context,
	task VerificationTask,
	at time.Time,
) error {
	if store.removed != nil && !store.removed() {
		store.completedBeforeRemoval = true
	}
	store.marked, store.markedAt = task, at
	return nil
}

func (store *verificationSchedulingStoreStub) UndoEnqueued(
	_ context.Context,
	task VerificationTask,
) error {
	store.undone = task
	return nil
}

func (store *verificationSchedulingStoreStub) Prepare(
	_ context.Context,
	at time.Time,
	_ int,
) ([]VerificationTask, error) {
	store.prepareCalls++
	store.preparedAt = at
	return store.prepared, store.prepareErr
}

func (store *verificationSchedulingStoreStub) ClaimExpired(
	_ context.Context,
	at time.Time,
	_ int,
) ([]ExpiredUpload, error) {
	store.expireCalls++
	store.expiredAt = at
	return store.expired, store.expiryErr
}

func (store *verificationSchedulingStoreStub) CompleteExpiry(
	_ context.Context,
	upload ExpiredUpload,
	_ time.Time,
) error {
	store.completed = append(store.completed, upload)
	return nil
}

type expiryStorageStub struct {
	removed []string
	err     error
}

func (storage *expiryStorageStub) RemoveObject(_ context.Context, key string) error {
	storage.removed = append(storage.removed, key)
	return storage.err
}

type verificationQueueStub struct {
	tasks []VerificationTask
	err   error
}

func (queue *verificationQueueStub) Enqueue(_ context.Context, task VerificationTask) error {
	queue.tasks = append(queue.tasks, task)
	return queue.err
}

type verificationTaskClientStub struct {
	task       *asynq.Task
	options    int
	enqueueErr error
	closeErr   error
}

func (client *verificationTaskClientStub) EnqueueContext(
	_ context.Context,
	task *asynq.Task,
	options ...asynq.Option,
) (*asynq.TaskInfo, error) {
	client.task, client.options = task, len(options)
	return nil, client.enqueueErr
}

func (client *verificationTaskClientStub) Close() error {
	return client.closeErr
}
