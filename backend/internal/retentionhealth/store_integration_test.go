package retentionhealth

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStoreRecordsAndReadsBoundedRetentionHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := retentionHealthTestDatabase(t, ctx)
	store := Store{Database: database}
	var registeredAt time.Time
	if err := database.QueryRow(ctx, `
		SELECT registered_at
		FROM retention_maintenance_status
		WHERE operation_key = 'tenant_magic_links'
	`).Scan(&registeredAt); err != nil {
		t.Fatalf("read registered_at: %v", err)
	}
	now := registeredAt.UTC().Add(time.Minute)

	if err := store.RecordRun(ctx, Run{
		Operation: OperationTenantMagicLinks, AttemptedAt: now,
		Result: ResultSucceeded, Processed: 7,
	}); err != nil {
		t.Fatalf("RecordRun(success) error = %v", err)
	}
	report, err := Read(ctx, database, now.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("Read(after success) error = %v", err)
	}
	tenant := healthByOperation(t, report, OperationTenantMagicLinks)
	if tenant.State != StateHealthy || tenant.LastProcessed != 7 ||
		tenant.TotalProcessed != 7 || tenant.TotalRuns != 1 ||
		tenant.TotalFailures != 0 || tenant.ConsecutiveFailures != 0 ||
		tenant.FailureCode != "" {
		t.Fatalf("tenant health after success = %#v", tenant)
	}

	failedAt := now.Add(time.Hour)
	if err := store.RecordRun(ctx, Run{
		Operation: OperationTenantMagicLinks, AttemptedAt: failedAt,
		Result: ResultFailed, Processed: 2,
	}); err != nil {
		t.Fatalf("RecordRun(failure) error = %v", err)
	}
	if err := store.RecordRun(ctx, Run{
		Operation: OperationPlatformAdminMagicLinks, AttemptedAt: failedAt,
		Result: ResultSucceeded, Processed: 4000, BudgetExhausted: true,
	}); err != nil {
		t.Fatalf("RecordRun(budget exhausted) error = %v", err)
	}
	if err := store.RecordRun(ctx, Run{
		Operation: OperationAttachments, AttemptedAt: failedAt,
		Result: ResultSucceeded, Processed: 12,
	}); err != nil {
		t.Fatalf("RecordRun(attachments) error = %v", err)
	}
	report, err = Read(ctx, database, failedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("Read(after failure) error = %v", err)
	}
	tenant = healthByOperation(t, report, OperationTenantMagicLinks)
	if tenant.State != StateFailed || tenant.AlertReason != AlertLastRunFailed ||
		tenant.LastProcessed != 2 || tenant.TotalProcessed != 9 ||
		tenant.TotalRuns != 2 || tenant.TotalFailures != 1 ||
		tenant.ConsecutiveFailures != 1 || tenant.FailureCode != "cleanup_failed" {
		t.Fatalf("tenant health after failure = %#v", tenant)
	}
	platform := healthByOperation(t, report, OperationPlatformAdminMagicLinks)
	if platform.State != StateAttention || platform.AlertReason != AlertBudgetExhausted ||
		!platform.BudgetExhausted || platform.TotalProcessed != 4000 {
		t.Fatalf("platform health after budget exhaustion = %#v", platform)
	}
	attachment := healthByOperation(t, report, OperationAttachments)
	if attachment.State != StateHealthy || attachment.Category != "attachments" ||
		attachment.LastProcessed != 12 {
		t.Fatalf("attachment health after success = %#v", attachment)
	}
	identity := healthByOperation(t, report, OperationIdentityAssetDeletions)
	if identity.State != StateOverdue || identity.AlertReason != AlertCleanupOverdue {
		t.Fatalf("identity health past its minute schedule = %#v", identity)
	}
	if report.Summary != (Summary{
		Tracked: 12, Healthy: 1, Attention: 1, Failed: 1, Overdue: 1, NotObserved: 8,
	}) {
		t.Fatalf("health summary = %#v", report.Summary)
	}

	err = store.RecordRun(ctx, Run{
		Operation: OperationTenantMagicLinks, AttemptedAt: now,
		Result: ResultSucceeded,
	})
	if err == nil || !strings.Contains(err.Error(), "newer evidence exists") {
		t.Fatalf("RecordRun(stale) error = %v", err)
	}

	var failureCode *string
	if err := database.QueryRow(ctx, `
		SELECT failure_code
		FROM retention_maintenance_status
		WHERE operation_key = 'tenant_magic_links'
	`).Scan(&failureCode); err != nil || failureCode == nil || *failureCode != "cleanup_failed" {
		t.Fatalf("stored failure code = %v, error = %v", failureCode, err)
	}
}

func healthByOperation(t *testing.T, report Report, operation Operation) OperationHealth {
	t.Helper()
	for _, health := range report.Operations {
		if health.Operation == operation {
			return health
		}
	}
	t.Fatalf("operation %q not found in %#v", operation, report)
	return OperationHealth{}
}

func retentionHealthTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
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
	migrator, err := db.NewMigrator(directURL, retentionHealthMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	config, err := pgxpool.ParseConfig(directURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	database, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}

func retentionHealthMigrationsPath(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate retention health integration test")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations")
}
