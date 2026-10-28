package email

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestLifecycleHandlerDeliversPersistedTenantEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := lifecycleTestDatabase(t, ctx)

	var tenantID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status, billing_email)
		VALUES ('Acme', 'acme-lifecycle-email', 'growth', 'active', 'billing@acme.test')
		RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create lifecycle tenant: %v", err)
	}
	var ownerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions, last_login_at)
		VALUES ($1, 'owner@acme.test', 'Sam Owner', ARRAY['billing:manage'], now())
		RETURNING id
	`, tenantID).Scan(&ownerID); err != nil {
		t.Fatalf("create lifecycle owner: %v", err)
	}

	var welcomeEventID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, data)
		VALUES ($1, 'tenant.owner_activated', 'tenant', $1, jsonb_build_object('member_id', $2::text))
		RETURNING id
	`, tenantID, ownerID).Scan(&welcomeEventID); err != nil {
		t.Fatalf("create welcome event: %v", err)
	}
	sender := &fakeSender{}
	handler := LifecycleHandler{
		Database: pool, Sender: sender,
		PublicBaseURL: "https://api.staging.example.test", BaseDomain: "staging.example.test",
		AuthFrom:       "Fidibeki <auth@fidibeki.com>",
		WelcomeFrom:    "Fidibeki <welcome@example.test>",
		WelcomeReplyTo: "welcome@example.test",
		Now:            func() time.Time { return time.Date(2026, 7, 23, 5, 0, 0, 0, time.UTC) },
	}.Handler()

	if err := handler(ctx, asynq.NewTask(TaskLifecycleEmail, []byte("not-json"))); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("malformed task error = %v", err)
	}
	missingPayload := lifecyclePayload(t, "00000000-0000-0000-0000-000000000000", tenantID)
	if err := handler(ctx, asynq.NewTask(TaskLifecycleEmail, missingPayload)); err != nil {
		t.Fatalf("missing event error = %v", err)
	}
	welcomePayload := lifecyclePayload(t, welcomeEventID, tenantID)
	for attempt := 0; attempt < 2; attempt++ {
		if err := handler(ctx, asynq.NewTask(TaskLifecycleEmail, welcomePayload)); err != nil {
			t.Fatalf("welcome attempt %d: %v", attempt+1, err)
		}
	}
	if len(sender.messages) != 1 || sender.messages[0].To != "owner@acme.test" ||
		sender.messages[0].ReplyTo != "welcome@example.test" {
		t.Fatalf("welcome messages = %#v", sender.messages)
	}

	var billingEventID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key, data)
		VALUES (
			$1, 'billing.state_changed', 'tenant', $1, 'stripe-event',
			jsonb_build_object(
				'previous_status', 'active', 'status', 'suspended',
				'previous_plan', 'growth', 'plan', 'growth'
			)
		)
		RETURNING id
	`, tenantID).Scan(&billingEventID); err != nil {
		t.Fatalf("create billing event: %v", err)
	}
	if err := handler(ctx, asynq.NewTask(TaskLifecycleEmail, lifecyclePayload(t, billingEventID, tenantID))); err != nil {
		t.Fatalf("billing handler: %v", err)
	}
	if len(sender.messages) != 3 || sender.messages[1].To != "owner@acme.test" ||
		sender.messages[2].To != "billing@acme.test" ||
		sender.messages[1].Subject != "Action required: Fidibeki payment failed" {
		t.Fatalf("billing messages = %#v", sender.messages)
	}

	var cancellationEventID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key, data)
		VALUES (
			$1, 'tenant.deletion_cancelled', 'tenant', $1, 'cancellation-event',
			jsonb_build_object(
				'actor_member_id', $2::text,
				'deletion_requested_at', '2026-07-22T12:00:00Z'
			)
		)
		RETURNING id
	`, tenantID, ownerID).Scan(&cancellationEventID); err != nil {
		t.Fatalf("create cancellation event: %v", err)
	}
	if err := handler(ctx, asynq.NewTask(TaskLifecycleEmail, lifecyclePayload(t, cancellationEventID, tenantID))); err != nil {
		t.Fatalf("cancellation handler: %v", err)
	}
	if len(sender.messages) != 5 ||
		!strings.Contains(sender.messages[3].Text, "Cancelled by: Sam Owner") ||
		!strings.Contains(sender.messages[3].Text, "Original request: 2026-07-22T12:00:00Z") {
		t.Fatalf("cancellation messages = %#v", sender.messages)
	}
}

func lifecyclePayload(t *testing.T, eventID, tenantID string) []byte {
	t.Helper()
	payload, err := json.Marshal(lifecycleTask{
		OutboxEventID: eventID, TenantID: tenantID, ResourceID: tenantID,
	})
	if err != nil {
		t.Fatalf("encode lifecycle task: %v", err)
	}
	return payload
}

func lifecycleTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
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
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("PostgreSQL connection string: %v", err)
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve lifecycle test path")
	}
	migrationsPath, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, migrationsPath)
	if err != nil {
		t.Fatalf("create lifecycle migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate lifecycle database: %v", err)
	}
	pool, err := pgxpool.New(ctx, directURL)
	if err != nil {
		t.Fatalf("open lifecycle database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
