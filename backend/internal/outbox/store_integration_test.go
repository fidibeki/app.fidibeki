package outbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStoreTracksOutboxDispatchAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close(ctx) })
	if _, err := connection.Exec(ctx, `
		CREATE TABLE outbox_events (
			id uuid PRIMARY KEY, tenant_id uuid NOT NULL, event text NOT NULL,
			resource_type text NOT NULL, resource_id uuid NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(), dispatched_at timestamptz,
			attempts integer NOT NULL DEFAULT 0, last_error text
		);
		INSERT INTO outbox_events (id, tenant_id, event, resource_type, resource_id)
		VALUES (
			'00000000-0000-4000-8000-000000000001',
			'00000000-0000-4000-8000-000000000002',
			'feedback.created', 'feedback',
			'00000000-0000-4000-8000-000000000003'
		)
	`); err != nil {
		t.Fatalf("seed outbox table: %v", err)
	}
	store := Store{Database: connection}
	events, err := store.Pending(ctx, 10)
	if err != nil || len(events) != 1 || events[0].Attempts != 0 {
		t.Fatalf("Pending() = %#v, error = %v", events, err)
	}
	longError := strings.Repeat("x", 2100)
	if err := store.MarkFailed(ctx, events[0].ID, longError); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	var attempts int
	var lastError string
	if err := connection.QueryRow(ctx,
		`SELECT attempts, last_error FROM outbox_events WHERE id = $1`, events[0].ID,
	).Scan(&attempts, &lastError); err != nil {
		t.Fatalf("read failed event: %v", err)
	}
	if attempts != 1 || len(lastError) != 2000 {
		t.Errorf("failed event attempts = %d, error length = %d", attempts, len(lastError))
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	if err := store.MarkDispatched(ctx, events[0].ID, now); err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}
	events, err = store.Pending(ctx, 10)
	if err != nil || len(events) != 0 {
		t.Errorf("Pending(after dispatch) = %#v, error = %v", events, err)
	}
}
