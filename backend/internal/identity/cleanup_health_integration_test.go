package identity

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestCleanupMaintainerRecordsRetentionHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := identityRetentionHealthTestDatabase(t, ctx)
	health := retentionhealth.Store{Database: database}
	current := retentionHealthRegisteredAt(t, ctx, database).Add(time.Minute)
	clock := func() time.Time { return current }
	objects := &identityFailureStorage{}
	maintainer := CleanupMaintainer{
		Store:   Store{Database: database, Now: clock},
		Storage: objects, Health: health, Now: clock,
	}

	if err := maintainer.Maintain(ctx); err != nil {
		t.Fatalf("Maintain(empty queue) error = %v", err)
	}
	row := readRetentionHealthRow(t, ctx, database, "identity_asset_deletions")
	if row.lastResult != "succeeded" || row.lastProcessed != 0 || row.totalRuns != 1 ||
		row.consecutiveFailures != 0 || row.failureCode != nil {
		t.Fatalf("identity cleanup health after empty sweep = %#v", row)
	}

	var tenantID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Identity Health', 'identity-health', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO identity_asset_deletions (tenant_id, asset_kind, version, next_attempt_at)
		VALUES ($1, 'workspace_logo', $2, $3)
	`, tenantID, uuid.NewString(), current); err != nil {
		t.Fatalf("stage identity asset deletion: %v", err)
	}

	current = current.Add(time.Minute)
	firstSuccess := row.lastSuccessAt
	objects.removeErr = errors.New("storage cleanup unavailable")
	if err := maintainer.Maintain(ctx); err == nil {
		t.Fatal("Maintain(storage failure) error = nil")
	}
	row = readRetentionHealthRow(t, ctx, database, "identity_asset_deletions")
	if row.lastResult != "failed" || row.consecutiveFailures != 1 ||
		row.failureCode == nil || *row.failureCode != "cleanup_failed" ||
		row.totalRuns != 2 || row.totalFailures != 1 ||
		row.lastSuccessAt == nil || !row.lastSuccessAt.Equal(*firstSuccess) {
		t.Fatalf("identity cleanup health after failure = %#v", row)
	}

	// The failed candidate backs off; the next sweep succeeds without it.
	current = current.Add(time.Minute)
	objects.removeErr = nil
	if err := maintainer.Maintain(ctx); err != nil {
		t.Fatalf("Maintain(rerun) error = %v", err)
	}
	row = readRetentionHealthRow(t, ctx, database, "identity_asset_deletions")
	if row.lastResult != "succeeded" || row.consecutiveFailures != 0 ||
		row.failureCode != nil || row.totalRuns != 3 || row.totalFailures != 1 {
		t.Fatalf("identity cleanup health after rerun = %#v", row)
	}

	current = current.Add(10 * time.Minute)
	if err := maintainer.Maintain(ctx); err != nil {
		t.Fatalf("Maintain(retry due) error = %v", err)
	}
	row = readRetentionHealthRow(t, ctx, database, "identity_asset_deletions")
	if row.lastResult != "succeeded" || row.lastProcessed != 1 || row.totalRuns != 4 {
		t.Fatalf("identity cleanup health after retry = %#v", row)
	}
	var pending int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM identity_asset_deletions WHERE tenant_id = $1
	`, tenantID).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("pending deletions = %d, error = %v, want 0", pending, err)
	}
}

type retentionHealthRow struct {
	lastResult          string
	lastProcessed       int64
	consecutiveFailures int
	totalRuns           int64
	totalFailures       int64
	failureCode         *string
	lastSuccessAt       *time.Time
}

func readRetentionHealthRow(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	operation string,
) retentionHealthRow {
	t.Helper()
	var row retentionHealthRow
	var lastResult *string
	if err := database.QueryRow(ctx, `
		SELECT last_result, last_processed, consecutive_failures,
		       total_runs, total_failures, failure_code, last_success_at
		FROM retention_maintenance_status
		WHERE operation_key = $1
	`, operation).Scan(
		&lastResult, &row.lastProcessed, &row.consecutiveFailures,
		&row.totalRuns, &row.totalFailures, &row.failureCode, &row.lastSuccessAt,
	); err != nil {
		t.Fatalf("read %s retention health: %v", operation, err)
	}
	if lastResult != nil {
		row.lastResult = *lastResult
	}
	if row.lastSuccessAt != nil {
		utc := row.lastSuccessAt.UTC()
		row.lastSuccessAt = &utc
	}
	return row
}

func retentionHealthRegisteredAt(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
) time.Time {
	t.Helper()
	var registeredAt time.Time
	if err := database.QueryRow(ctx, `
		SELECT max(registered_at) FROM retention_maintenance_status
	`).Scan(&registeredAt); err != nil {
		t.Fatalf("read retention health registration time: %v", err)
	}
	return registeredAt.UTC()
}

func identityRetentionHealthTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
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
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve identity retention health test path")
	}
	migrations := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations")
	migrator, err := db.NewMigrator(directURL, migrations)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	database, err := pgxpool.New(ctx, directURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}
