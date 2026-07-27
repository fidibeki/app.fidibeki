package seatlimit

import (
	"context"
	"errors"
	"testing"

	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestEnsureAvailableUsesThePlanCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		plan      string
		members   int
		wantError error
		wantRows  int
	}{
		{name: "growth has room", plan: "growth", members: 2, wantRows: 2},
		{name: "growth is full", plan: "growth", members: 3, wantError: ErrAtCapacity, wantRows: 2},
		{name: "deactivated members still count", plan: "starter", members: 1, wantError: ErrAtCapacity, wantRows: 2},
		{name: "enterprise is unlimited", plan: "enterprise", members: 500, wantRows: 1},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query := &capacityQuerier{values: []any{test.plan, test.members}}
			err := EnsureAvailable(context.Background(), query, "tenant-1")
			if !errors.Is(err, test.wantError) {
				t.Fatalf("EnsureAvailable() error = %v, want %v", err, test.wantError)
			}
			if query.rows != test.wantRows {
				t.Fatalf("QueryRow calls = %d, want %d", query.rows, test.wantRows)
			}
		})
	}
}

func TestEnsureAvailableRejectsUnknownPlan(t *testing.T) {
	t.Parallel()
	err := EnsureAvailable(
		context.Background(),
		&capacityQuerier{values: []any{"unknown"}},
		"tenant-1",
	)
	if err == nil || errors.Is(err, ErrAtCapacity) {
		t.Fatalf("EnsureAvailable() error = %v, want an unknown-plan failure", err)
	}
}

type capacityQuerier struct {
	values []any
	rows   int
}

func (*capacityQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (*capacityQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (q *capacityQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	value := q.values[q.rows]
	q.rows++
	return capacityRow{value: value}
}

type capacityRow struct{ value any }

func (r capacityRow) Scan(destinations ...any) error {
	switch destination := destinations[0].(type) {
	case *int:
		*destination = r.value.(int)
	case *tenant.Plan:
		*destination = tenant.Plan(r.value.(string))
	case *string:
		*destination = r.value.(string)
	default:
		return errors.New("unexpected scan destination")
	}
	return nil
}
