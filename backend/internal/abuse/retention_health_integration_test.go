package abuse

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMaintainerRecordsRetentionHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := abuseTestDatabase(t, ctx)
	health := retentionhealth.Store{Database: database}
	firstRun := retentionHealthRegisteredAt(t, ctx, database).Add(time.Minute)

	maintainer := Maintainer{
		Store: &cleanupStoreStub{deleted: []int64{7}}, Health: health,
		Now: func() time.Time { return firstRun },
	}
	if err := maintainer.MaintainOnce(ctx); err != nil {
		t.Fatalf("MaintainOnce(success) error = %v", err)
	}
	row := readRetentionHealthRow(t, ctx, database, "abuse_diagnostics")
	if row.lastResult != "succeeded" || row.lastProcessed != 7 || row.totalRuns != 1 ||
		row.consecutiveFailures != 0 || row.failureCode != nil ||
		row.lastSuccessAt == nil || !row.lastSuccessAt.Equal(firstRun) {
		t.Fatalf("abuse diagnostics health after success = %#v", row)
	}

	failedRun := firstRun.Add(time.Minute)
	failure := errors.New("database unavailable")
	maintainer.Store = &cleanupStoreStub{err: failure}
	maintainer.Now = func() time.Time { return failedRun }
	if err := maintainer.MaintainOnce(ctx); !errors.Is(err, failure) {
		t.Fatalf("MaintainOnce(failure) error = %v", err)
	}
	row = readRetentionHealthRow(t, ctx, database, "abuse_diagnostics")
	if row.lastResult != "failed" || row.consecutiveFailures != 1 ||
		row.failureCode == nil || *row.failureCode != "cleanup_failed" ||
		row.totalRuns != 2 || row.totalFailures != 1 ||
		row.lastSuccessAt == nil || !row.lastSuccessAt.Equal(firstRun) {
		t.Fatalf("abuse diagnostics health after failure = %#v", row)
	}

	maintainer.Store = &cleanupStoreStub{}
	maintainer.Now = func() time.Time { return failedRun.Add(time.Minute) }
	if err := maintainer.MaintainOnce(ctx); err != nil {
		t.Fatalf("MaintainOnce(rerun) error = %v", err)
	}
	row = readRetentionHealthRow(t, ctx, database, "abuse_diagnostics")
	if row.lastResult != "succeeded" || row.consecutiveFailures != 0 ||
		row.failureCode != nil || row.totalRuns != 3 || row.totalFailures != 1 {
		t.Fatalf("abuse diagnostics health after rerun = %#v", row)
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
