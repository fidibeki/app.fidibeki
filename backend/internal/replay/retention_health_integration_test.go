package replay

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestCleanerRecordsRetentionHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := replayRetentionHealthTestDatabase(t, ctx)
	health := retentionhealth.Store{Database: database}
	firstRun := retentionHealthRegisteredAt(t, ctx, database).Add(time.Minute)

	candidate := RetentionCandidate{
		TenantID: "tenant-one", ID: "session-one", ObjectKeys: []string{"session-one/replay.json"},
	}
	cleaner := Cleaner{
		Store:   &retentionStoreStub{candidates: []RetentionCandidate{candidate}},
		Storage: &objectRemoverStub{}, Health: health,
		Now: func() time.Time { return firstRun },
	}
	if err := cleaner.CleanupOnce(ctx); err != nil {
		t.Fatalf("CleanupOnce(success) error = %v", err)
	}
	row := readRetentionHealthRow(t, ctx, database, "replay_sessions")
	if row.lastResult != "succeeded" || row.lastProcessed != 1 || row.totalRuns != 1 ||
		row.consecutiveFailures != 0 || row.failureCode != nil ||
		row.lastSuccessAt == nil || !row.lastSuccessAt.Equal(firstRun) {
		t.Fatalf("replay health after success = %#v", row)
	}

	failedRun := firstRun.Add(time.Minute)
	cleaner.Store = &retentionStoreStub{candidates: []RetentionCandidate{candidate}}
	cleaner.Storage = &objectRemoverStub{fail: map[string]error{
		"session-one/replay.json": errors.New("storage unavailable"),
	}}
	cleaner.Now = func() time.Time { return failedRun }
	if err := cleaner.CleanupOnce(ctx); err == nil {
		t.Fatal("CleanupOnce(storage failure) error = nil")
	}
	row = readRetentionHealthRow(t, ctx, database, "replay_sessions")
	if row.lastResult != "failed" || row.consecutiveFailures != 1 ||
		row.failureCode == nil || *row.failureCode != "cleanup_failed" ||
		row.totalRuns != 2 || row.totalFailures != 1 ||
		row.lastSuccessAt == nil || !row.lastSuccessAt.Equal(firstRun) {
		t.Fatalf("replay health after failure = %#v", row)
	}

	cleaner.Store = &retentionStoreStub{candidates: []RetentionCandidate{candidate}}
	cleaner.Storage = &objectRemoverStub{}
	cleaner.Now = func() time.Time { return failedRun.Add(time.Minute) }
	if err := cleaner.CleanupOnce(ctx); err != nil {
		t.Fatalf("CleanupOnce(rerun) error = %v", err)
	}
	row = readRetentionHealthRow(t, ctx, database, "replay_sessions")
	if row.lastResult != "succeeded" || row.consecutiveFailures != 0 ||
		row.failureCode != nil || row.totalRuns != 3 || row.totalFailures != 1 {
		t.Fatalf("replay health after rerun = %#v", row)
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

func replayRetentionHealthTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
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
		t.Fatal("resolve replay retention health test path")
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
