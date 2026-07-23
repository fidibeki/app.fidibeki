package ai

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBudgetMaintainerBoundsRetentionCleanup(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	store := &budgetCleanupStoreStub{deleted: []int64{3000, 2}}
	maintainer := BudgetMaintainer{Store: store, Now: func() time.Time { return now }}
	if err := maintainer.MaintainOnce(context.Background()); err != nil {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	if store.calls != 2 || !store.before.Equal(now.AddDate(0, 0, -budgetRetentionDays)) ||
		store.limit != budgetCleanupBatch {
		t.Fatalf("cleanup = calls %d before %v limit %d", store.calls, store.before, store.limit)
	}
}

func TestBudgetMaintainerReturnsStoreFailureAndStopsWhenCanceled(t *testing.T) {
	t.Parallel()
	want := errors.New("database unavailable")
	maintainer := BudgetMaintainer{Store: &budgetCleanupStoreStub{err: want}}
	if err := maintainer.MaintainOnce(context.Background()); !errors.Is(err, want) {
		t.Fatalf("MaintainOnce() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (BudgetMaintainer{Store: &budgetCleanupStoreStub{}}).Run(ctx, 0); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestBudgetMaintenanceStoreDeletesEveryExpiredBudgetResource(t *testing.T) {
	t.Parallel()
	before := time.Date(2026, 4, 24, 8, 0, 0, 0, time.FixedZone("west", -3600))
	query := &budgetMaintenanceQuerier{deleted: []int64{2, 3, 4}}
	deleted, err := (BudgetMaintenanceStore{Database: query}).
		DeleteExpiredBudgetData(context.Background(), before, 100)
	if err != nil {
		t.Fatalf("DeleteExpiredBudgetData() error = %v", err)
	}
	if deleted != 9 || query.calls != 3 || !query.before.Equal(before.UTC()) ||
		query.limit != 100 {
		t.Fatalf("cleanup = deleted %d calls %d before %v limit %d",
			deleted, query.calls, query.before, query.limit)
	}

	if _, err := (BudgetMaintenanceStore{}).
		DeleteExpiredBudgetData(context.Background(), before, 100); err == nil {
		t.Fatal("DeleteExpiredBudgetData(nil database) error = nil")
	}
	for _, limit := range []int{0, 10_001} {
		if _, err := (BudgetMaintenanceStore{Database: query}).
			DeleteExpiredBudgetData(context.Background(), before, limit); err == nil {
			t.Fatalf("DeleteExpiredBudgetData(limit %d) error = nil", limit)
		}
	}

	failing := &budgetMaintenanceQuerier{deleted: []int64{5}, failAt: 2}
	deleted, err = (BudgetMaintenanceStore{Database: failing}).
		DeleteExpiredBudgetData(context.Background(), before, 100)
	if err == nil || deleted != 5 {
		t.Fatalf("DeleteExpiredBudgetData(failure) = %d, %v", deleted, err)
	}
}

type budgetCleanupStoreStub struct {
	deleted []int64
	err     error
	calls   int
	before  time.Time
	limit   int
}

type budgetMaintenanceQuerier struct {
	deleted []int64
	failAt  int
	calls   int
	before  time.Time
	limit   int
}

func (query *budgetMaintenanceQuerier) Exec(
	_ context.Context,
	_ string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	query.calls++
	query.before = arguments[0].(time.Time)
	query.limit = arguments[1].(int)
	if query.calls == query.failAt {
		return pgconn.CommandTag{}, errors.New("database unavailable")
	}
	deleted := int64(0)
	if len(query.deleted) > 0 {
		deleted = query.deleted[0]
		query.deleted = query.deleted[1:]
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", deleted)), nil
}

func (*budgetMaintenanceQuerier) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (*budgetMaintenanceQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func (store *budgetCleanupStoreStub) DeleteExpiredBudgetData(
	_ context.Context,
	before time.Time,
	limit int,
) (int64, error) {
	store.calls++
	store.before, store.limit = before, limit
	if store.err != nil {
		return 0, store.err
	}
	if len(store.deleted) == 0 {
		return 0, nil
	}
	value := store.deleted[0]
	store.deleted = store.deleted[1:]
	return value, nil
}
