package privacy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetentionMaintainerRecordsHealthPerPhase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := privacyTestDatabase(t, ctx)
	health := retentionhealth.Store{Database: database}
	operations := []string{
		"attachments", "privacy_export_archives", "reporter_privacy_requests",
		"email_feedback_invitations",
	}
	firstRun := retentionHealthRegisteredAt(t, ctx, database).Add(time.Minute)

	maintainer := RetentionMaintainer{
		Store: &retentionOperationsStub{
			attachmentBatches: [][]RetentionObjectCandidate{{
				{TenantID: "one", ID: "attachment-one", ObjectKey: "attachment-one"},
				{TenantID: "one", ID: "attachment-two", ObjectKey: "attachment-two"},
			}},
			archiveBatches: [][]RetentionObjectCandidate{{
				{TenantID: "one", ID: "archive-one", ObjectKey: "archive-one"},
			}},
			requestResults:    []int64{3},
			invitationResults: []int64{4},
		},
		Storage: &retentionStorageStub{}, Health: health,
		Now: func() time.Time { return firstRun }, BatchSize: 10,
	}
	if err := maintainer.MaintainOnce(ctx); err != nil {
		t.Fatalf("MaintainOnce(success) error = %v", err)
	}
	for operation, processed := range map[string]int64{
		"attachments": 2, "privacy_export_archives": 1, "reporter_privacy_requests": 3,
		"email_feedback_invitations": 4,
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
	maintainer.Store = &retentionOperationsStub{
		attachmentErr: failure, archiveErr: failure, requestErr: failure,
		invitationErr: failure,
	}
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

	maintainer.Store = &retentionOperationsStub{}
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
