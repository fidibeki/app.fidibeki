package widgetanalytics

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMaintenanceStoreReconcilesAndRetainsBoundedBatches(t *testing.T) {
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
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, analyticsMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	poolConfig, err := pgxpool.ParseConfig(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL pool config: %v", err)
	}
	poolConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `SET TIME ZONE 'Pacific/Honolulu'`)
		return err
	}
	database, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	t.Cleanup(database.Close)

	var tenantID, projectID, ruleID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Maintenance', 'maintenance', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create maintenance tenant: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Maintenance Web', 'pk_live_MAINTENANCEANALYTICS1X', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create maintenance project: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO widget_rules (project_id, name, matcher_type, trigger_type, trigger_delay_seconds)
		VALUES ($1, 'Current rule', 'all_pages', 'delay', 0) RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create maintenance rule: %v", err)
	}
	now := time.Date(2026, 7, 22, 14, 30, 0, 0, time.UTC)
	if _, err := database.Exec(ctx, `
		INSERT INTO widget_funnel_events (
			event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			visitor_id, page_view_id, page_url, page_key, occurred_at, received_at
		) VALUES
			(gen_random_uuid(), $1, $2, $3, 'Original rule', 'matched', gen_random_uuid(), gen_random_uuid(),
			 'https://example.com/current', digest('https://example.com/current', 'sha256'), $4::timestamptz - interval '1 hour', $4::timestamptz - interval '10 minutes'),
			(gen_random_uuid(), $1, $2, $3, 'Renamed rule', 'impression', gen_random_uuid(), gen_random_uuid(),
			 'https://example.com/current', digest('https://example.com/current', 'sha256'), $4::timestamptz - interval '50 minutes', $4::timestamptz - interval '5 minutes'),
			(gen_random_uuid(), $1, $2, $3, 'Renamed rule', 'interaction', gen_random_uuid(), gen_random_uuid(),
			 'https://example.com/current', digest('https://example.com/current', 'sha256'), $4::timestamptz - interval '40 minutes', $4::timestamptz - interval '4 minutes'),
			(gen_random_uuid(), $1, $2, $3, 'Original rule', 'matched', gen_random_uuid(), gen_random_uuid(),
			 'https://example.com/yesterday', digest('https://example.com/yesterday', 'sha256'), $4::timestamptz - interval '1 day', $4::timestamptz - interval '1 day'),
			(gen_random_uuid(), $1, $2, $3, 'Original rule', 'matched', gen_random_uuid(), gen_random_uuid(),
			 'https://example.com/expired-a', digest('https://example.com/expired-a', 'sha256'), $4::timestamptz - interval '91 days', $4::timestamptz - interval '91 days'),
			(gen_random_uuid(), $1, $2, $3, 'Original rule', 'matched', gen_random_uuid(), gen_random_uuid(),
			 'https://example.com/expired-b', digest('https://example.com/expired-b', 'sha256'), $4::timestamptz - interval '92 days', $4::timestamptz - interval '92 days'),
			(gen_random_uuid(), $1, $2, $3, 'Original rule', 'matched', gen_random_uuid(), gen_random_uuid(),
			 'https://example.com/raw-boundary', digest('https://example.com/raw-boundary', 'sha256'), $4::timestamptz - interval '90 days', $4::timestamptz - interval '90 days')
	`, tenantID, projectID, ruleID, now); err != nil {
		t.Fatalf("seed maintenance events: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO widget_funnel_daily (
			tenant_id, project_id, rule_id, rule_name, page_url, page_key, event_date,
			matched_count, impression_count, interaction_count, submitted_count, dismissed_count
		) VALUES
			($1, $2, $3, 'Corrupt', 'https://example.com/current', digest('https://example.com/current', 'sha256'), $4::date, 99, 99, 99, 99, 99),
			($1, $2, $3, 'Phantom', 'https://example.com/phantom', digest('https://example.com/phantom', 'sha256'), $4::date, 12, 0, 0, 0, 0),
			($1, $2, $3, 'Preserved', 'https://example.com/older-valid', digest('https://example.com/older-valid', 'sha256'), $4::date - 4, 7, 0, 0, 0, 0),
			($1, $2, $3, 'Expired', 'https://example.com/daily-expired-a', digest('https://example.com/daily-expired-a', 'sha256'), $4::date - 731, 1, 0, 0, 0, 0),
			($1, $2, $3, 'Expired', 'https://example.com/daily-expired-b', digest('https://example.com/daily-expired-b', 'sha256'), $4::date - 800, 1, 0, 0, 0, 0),
			($1, $2, $3, 'Boundary', 'https://example.com/daily-boundary', digest('https://example.com/daily-boundary', 'sha256'), $4::date - 730, 1, 0, 0, 0, 0)
	`, tenantID, projectID, ruleID, now); err != nil {
		t.Fatalf("seed maintenance aggregates: %v", err)
	}

	store := MaintenanceStore{Database: database, Cadence: -1}
	reconciled, err := store.ReconcileRecent(ctx, now, 3)
	if err != nil {
		t.Fatalf("ReconcileRecent() error = %v", err)
	}
	if reconciled.Upserted != 2 || reconciled.Removed != 1 {
		t.Fatalf("ReconcileRecent() = %#v, want 2 upserted and 1 removed", reconciled)
	}
	var ruleName string
	var matched, impressions, interactions int64
	if err := database.QueryRow(ctx, `
		SELECT rule_name, matched_count, impression_count, interaction_count
		FROM widget_funnel_daily
		WHERE project_id = $1 AND page_url = 'https://example.com/current'
	`, projectID).Scan(&ruleName, &matched, &impressions, &interactions); err != nil {
		t.Fatalf("read reconciled current aggregate: %v", err)
	}
	if ruleName != "Renamed rule" || matched != 1 || impressions != 1 || interactions != 1 {
		t.Fatalf("reconciled current aggregate = %q %d/%d/%d", ruleName, matched, impressions, interactions)
	}
	var count int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_daily
		WHERE project_id = $1 AND page_url = 'https://example.com/yesterday' AND matched_count = 1
	`, projectID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recreated yesterday aggregate count = %d, error = %v", count, err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_daily
		WHERE project_id = $1 AND page_url = 'https://example.com/phantom'
	`, projectID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("phantom aggregate count = %d, error = %v", count, err)
	}
	if err := database.QueryRow(ctx, `
		SELECT matched_count FROM widget_funnel_daily
		WHERE project_id = $1 AND page_url = 'https://example.com/older-valid'
	`, projectID).Scan(&matched); err != nil || matched != 7 {
		t.Fatalf("older aggregate matched = %d, error = %v", matched, err)
	}

	ingestion, err := database.Begin(ctx)
	if err != nil {
		t.Fatalf("begin concurrent ingestion: %v", err)
	}
	concurrentPage := "https://example.com/current"
	concurrentPageKey := sha256.Sum256([]byte(concurrentPage))
	concurrentEvent := EventValues{
		EventID: uuid.NewString(), TenantID: tenantID, ProjectID: projectID, RuleID: ruleID,
		RuleName: "Current rule", EventType: EventMatched,
		VisitorID: uuid.NewString(), PageViewID: uuid.NewString(), PageURL: concurrentPage,
		PageKey: concurrentPageKey[:], OccurredAt: now, ReceivedAt: now,
	}
	if _, err := (Store{}).InsertEvents(ctx, ingestion, []EventValues{concurrentEvent}); err != nil {
		_ = ingestion.Rollback(ctx)
		t.Fatalf("insert concurrent maintenance event: %v", err)
	}
	reconcileDone := make(chan error, 1)
	go func() {
		_, err := store.ReconcileRecent(ctx, now, 3)
		reconcileDone <- err
	}()
	select {
	case err := <-reconcileDone:
		_ = ingestion.Rollback(ctx)
		t.Fatalf("reconciliation completed before ingestion released its project lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := ingestion.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent ingestion: %v", err)
	}
	select {
	case err := <-reconcileDone:
		if err != nil {
			t.Fatalf("reconcile after concurrent ingestion: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconciliation did not resume after concurrent ingestion")
	}
	if err := database.QueryRow(ctx, `
		SELECT matched_count FROM widget_funnel_daily
		WHERE project_id = $1 AND page_url = $2
	`, projectID, concurrentPage).Scan(&matched); err != nil || matched != 2 {
		t.Fatalf("concurrent aggregate matched = %d, error = %v", matched, err)
	}
	cadenceResult, err := (MaintenanceStore{Database: database}).ReconcileRecent(ctx, now, 3)
	if err != nil || !cadenceResult.Skipped || cadenceResult.SkipReason != "cadence_not_due" {
		t.Fatalf("cadenced ReconcileRecent() = %#v, %v, want cadence skip", cadenceResult, err)
	}
	leaseConnection, err := database.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire competing maintenance connection: %v", err)
	}
	if _, err := leaseConnection.Exec(ctx, `SELECT pg_advisory_lock($1)`, analyticsMaintenanceAdvisoryLock); err != nil {
		leaseConnection.Release()
		t.Fatalf("acquire competing maintenance lease: %v", err)
	}
	skipped, err := store.ReconcileRecent(ctx, now, 3)
	if _, unlockErr := leaseConnection.Exec(ctx, `SELECT pg_advisory_unlock($1)`, analyticsMaintenanceAdvisoryLock); unlockErr != nil {
		t.Errorf("release competing maintenance lease: %v", unlockErr)
	}
	leaseConnection.Release()
	if err != nil || !skipped.Skipped {
		t.Fatalf("competing ReconcileRecent() = %#v, %v, want skipped", skipped, err)
	}

	rawCutoff := now.AddDate(0, 0, -analyticsRawRetentionDays)
	for expectedRemaining := 1; expectedRemaining >= 0; expectedRemaining-- {
		deleted, err := store.DeleteExpiredEvents(ctx, rawCutoff, 1)
		if err != nil || deleted != 1 {
			t.Fatalf("DeleteExpiredEvents() = %d, %v, want 1", deleted, err)
		}
		if err := database.QueryRow(ctx, `
			SELECT count(*) FROM widget_funnel_events WHERE received_at < $1
		`, rawCutoff).Scan(&count); err != nil || count != expectedRemaining {
			t.Fatalf("expired raw remaining = %d, error = %v, want %d", count, err, expectedRemaining)
		}
	}
	if deleted, err := store.DeleteExpiredEvents(ctx, rawCutoff, 1); err != nil || deleted != 0 {
		t.Fatalf("DeleteExpiredEvents(empty) = %d, %v", deleted, err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_events WHERE received_at = $1
	`, rawCutoff).Scan(&count); err != nil || count != 1 {
		t.Fatalf("raw cutoff boundary count = %d, error = %v", count, err)
	}

	dailyCutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -analyticsDailyRetentionDays)
	for expectedRemaining := 1; expectedRemaining >= 0; expectedRemaining-- {
		deleted, err := store.DeleteExpiredDaily(ctx, dailyCutoff, 1)
		if err != nil || deleted != 1 {
			t.Fatalf("DeleteExpiredDaily() = %d, %v, want 1", deleted, err)
		}
		if err := database.QueryRow(ctx, `
			SELECT count(*) FROM widget_funnel_daily WHERE event_date < $1::date
		`, dailyCutoff).Scan(&count); err != nil || count != expectedRemaining {
			t.Fatalf("expired daily remaining = %d, error = %v, want %d", count, err, expectedRemaining)
		}
	}
	if deleted, err := store.DeleteExpiredDaily(ctx, dailyCutoff, 1); err != nil || deleted != 0 {
		t.Fatalf("DeleteExpiredDaily(empty) = %d, %v", deleted, err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_daily WHERE event_date = $1::date
	`, dailyCutoff).Scan(&count); err != nil || count != 1 {
		t.Fatalf("daily cutoff boundary count = %d, error = %v", count, err)
	}
}
