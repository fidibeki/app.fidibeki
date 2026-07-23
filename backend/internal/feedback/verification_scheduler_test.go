package feedback

import (
	"context"
	"errors"
	"testing"
	"time"
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
	store := &verificationSchedulingStoreStub{prepared: tasks, expired: 4}
	queue := &verificationQueueStub{}
	scheduler := VerificationScheduler{
		Store: store, Queue: queue, Now: func() time.Time { return now },
	}
	if err := scheduler.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce() error = %v", err)
	}
	if store.expiredAt != now || store.preparedAt != now || len(queue.tasks) != 2 {
		t.Fatalf("reconcile state = expiry %v prepare %v tasks %#v",
			store.expiredAt, store.preparedAt, queue.tasks)
	}
	if VerificationTaskID(tasks[0]) == VerificationTaskID(tasks[1]) ||
		VerificationTaskID(tasks[0]) != "attachment:verify:one:1" {
		t.Fatalf("verification task IDs = %q/%q",
			VerificationTaskID(tasks[0]), VerificationTaskID(tasks[1]))
	}
}

type verificationSchedulingStoreStub struct {
	marked     VerificationTask
	markedAt   time.Time
	undone     VerificationTask
	prepared   []VerificationTask
	preparedAt time.Time
	expired    int64
	expiredAt  time.Time
}

func (store *verificationSchedulingStoreStub) MarkEnqueued(
	_ context.Context,
	task VerificationTask,
	at time.Time,
) error {
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
	store.preparedAt = at
	return store.prepared, nil
}

func (store *verificationSchedulingStoreStub) ExpirePending(
	_ context.Context,
	at time.Time,
	_ int,
) (int64, error) {
	store.expiredAt = at
	return store.expired, nil
}

type verificationQueueStub struct {
	tasks []VerificationTask
	err   error
}

func (queue *verificationQueueStub) Enqueue(_ context.Context, task VerificationTask) error {
	queue.tasks = append(queue.tasks, task)
	return queue.err
}
