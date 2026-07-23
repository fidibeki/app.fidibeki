package abuse

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStoreAggregatesConcurrentlyAndCleansInBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := abuseTestDatabase(t, ctx)
	tenantID, projectID := seedAbuseProject(t, ctx, database, "abuse-store")
	store := Store{}
	now := time.Date(2026, 7, 23, 9, 37, 0, 0, time.UTC)

	const writers = 20
	var wait sync.WaitGroup
	errorsByWriter := make(chan error, writers)
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(offset int) {
			defer wait.Done()
			errorsByWriter <- store.Record(ctx, database, DiagnosticEvent{
				TenantID: tenantID, ProjectID: projectID, Category: CategoryRateLimit,
				Reason: ReasonSubmissionRateLimited, OccurredAt: now.Add(time.Duration(offset) * time.Second),
			})
		}(index)
	}
	wait.Wait()
	close(errorsByWriter)
	for err := range errorsByWriter {
		if err != nil {
			t.Fatalf("record concurrent diagnostic: %v", err)
		}
	}

	points, err := store.List(ctx, database, tenantID, "", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("list diagnostics: %v", err)
	}
	if len(points) != 1 || points[0].Count != writers ||
		!points[0].BucketStart.Equal(now.Truncate(time.Hour)) {
		t.Fatalf("diagnostic points = %#v, want one count %d", points, writers)
	}

	old := now.AddDate(0, 0, -91)
	for _, reason := range []string{"old-rate", "old-spam"} {
		category := CategoryRateLimit
		if reason == "old-spam" {
			category = CategorySpam
		}
		if err := store.Record(ctx, database, DiagnosticEvent{
			TenantID: tenantID, ProjectID: projectID, Category: category,
			Reason: reason, OccurredAt: old,
		}); err != nil {
			t.Fatalf("record old diagnostic: %v", err)
		}
	}
	deleted, err := store.DeleteExpired(ctx, database, now.AddDate(0, 0, -90), 1)
	if err != nil || deleted != 1 {
		t.Fatalf("first bounded cleanup = %d, error = %v", deleted, err)
	}
	deleted, err = store.DeleteExpired(ctx, database, now.AddDate(0, 0, -90), 10)
	if err != nil || deleted != 1 {
		t.Fatalf("second bounded cleanup = %d, error = %v", deleted, err)
	}
	if err := (Recorder{}).Record(ctx, DiagnosticEvent{}); err == nil {
		t.Fatal("Recorder.Record(nil database) error = nil")
	}
	if err := (Recorder{Database: database}).Record(ctx, DiagnosticEvent{
		TenantID: tenantID, ProjectID: projectID, Category: CategoryProvider,
		Reason: "buffer_flush", OccurredAt: now,
	}); err != nil {
		t.Fatalf("Recorder.Record() error = %v", err)
	}
}

func TestStoreRejectsInvalidOrForeignDiagnostic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := abuseTestDatabase(t, ctx)
	tenantA, projectA := seedAbuseProject(t, ctx, database, "abuse-a")
	tenantB, _ := seedAbuseProject(t, ctx, database, "abuse-b")
	store := Store{}
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)

	if err := store.Record(ctx, database, DiagnosticEvent{
		TenantID: tenantB, ProjectID: projectA, Category: CategorySpam,
		Reason: ReasonSpamRejected, OccurredAt: now,
	}); err == nil {
		t.Fatal("expected cross-tenant project diagnostic to fail")
	}
	if err := store.Record(ctx, database, DiagnosticEvent{
		TenantID: tenantA, ProjectID: projectA, Category: "visitor",
		Reason: "raw-identity", OccurredAt: now,
	}); err == nil {
		t.Fatal("expected unknown diagnostic category to fail")
	}
}

func seedAbuseProject(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	slug string,
) (string, string) {
	t.Helper()
	var tenantID, projectID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ($1, $1, 'growth', 'active')
		RETURNING id
	`, slug).Scan(&tenantID); err != nil {
		t.Fatalf("seed abuse tenant: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, $2, 'pk_live_' || substr(md5($2), 1, 22), ARRAY['example.test'])
		RETURNING id
	`, tenantID, slug).Scan(&projectID); err != nil {
		t.Fatalf("seed abuse project: %v", err)
	}
	return tenantID, projectID
}

func abuseTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start abuse PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("abuse PostgreSQL URL: %v", err)
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve abuse test path")
	}
	migrations := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations")
	migrator, err := db.NewMigrator(directURL, migrations)
	if err != nil {
		t.Fatalf("create abuse migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate abuse database: %v", err)
	}
	config, err := pgxpool.ParseConfig(directURL)
	if err != nil {
		t.Fatalf("parse abuse database URL: %v", err)
	}
	config.MaxConns = 24
	database, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open abuse database: %v", err)
	}
	t.Cleanup(database.Close)
	if err := database.Ping(ctx); err != nil {
		t.Fatalf("ping abuse database: %v", err)
	}
	return database
}

func ExampleDiagnosticEvent() {
	fmt.Println(DiagnosticEvent{
		Category: CategoryRateLimit, Reason: ReasonSubmissionRateLimited,
	}.Category)
	// Output: rate_limit
}
