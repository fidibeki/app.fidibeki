package widgetanalytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMaintainerRecordsRetentionHealthPerSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := analyticsRetentionHealthTestDatabase(t, ctx)
	health := retentionhealth.Store{Database: database}
	operations := []string{"widget_funnel_events", "widget_funnel_daily"}
	firstRun := retentionHealthRegisteredAt(t, ctx, database).Add(time.Minute)

	maintainer := Maintainer{
		Store:  &maintenanceStoreStub{eventsResults: []int64{5}, dailyResults: []int64{2}},
		Health: health, Now: func() time.Time { return firstRun },
	}
	if err := maintainer.MaintainOnce(ctx); err != nil {
		t.Fatalf("MaintainOnce(success) error = %v", err)
	}
	for operation, processed := range map[string]int64{
		"widget_funnel_events": 5, "widget_funnel_daily": 2,
	} {
		row := readRetentionHealthRow(t, ctx, database, operation)
		if row.lastResult != "succeeded" || row.lastProcessed != processed ||
			row.totalRuns != 1 || row.consecutiveFailures != 0 || row.failureCode != nil ||
			row.lastSuccessAt == nil || !row.lastSuccessAt.Equal(firstRun) {
			t.Fatalf("%s health after success = %#v", operation, row)
		}
	}

	failedRun := firstRun.Add(time.Minute)
	failure := errors.New("database unavailable")
	maintainer.Store = &maintenanceStoreStub{eventsErr: failure, dailyErr: failure}
	maintainer.Now = func() time.Time { return failedRun }
	if err := maintainer.MaintainOnce(ctx); !errors.Is(err, failure) {
		t.Fatalf("MaintainOnce(failure) error = %v", err)
	}
	for _, operation := range operations {
		row := readRetentionHealthRow(t, ctx, database, operation)
		if row.lastResult != "failed" || row.consecutiveFailures != 1 ||
			row.failureCode == nil || *row.failureCode != "cleanup_failed" ||
			row.totalRuns != 2 || row.totalFailures != 1 ||
			row.lastSuccessAt == nil || !row.lastSuccessAt.Equal(firstRun) {
			t.Fatalf("%s health after failure = %#v", operation, row)
		}
	}

	maintainer.Store = &maintenanceStoreStub{}
	maintainer.Now = func() time.Time { return failedRun.Add(time.Minute) }
	if err := maintainer.MaintainOnce(ctx); err != nil {
		t.Fatalf("MaintainOnce(rerun) error = %v", err)
	}
	for _, operation := range operations {
		row := readRetentionHealthRow(t, ctx, database, operation)
		if row.lastResult != "succeeded" || row.consecutiveFailures != 0 ||
			row.failureCode != nil || row.totalRuns != 3 || row.totalFailures != 1 {
			t.Fatalf("%s health after rerun = %#v", operation, row)
		}
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

func analyticsRetentionHealthTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
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
	migrator, err := db.NewMigrator(directURL, analyticsMigrationsPath(t))
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
