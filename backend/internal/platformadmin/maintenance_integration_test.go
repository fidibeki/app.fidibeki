package platformadmin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPlatformSecurityEventsPrivilegeBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := platformMagicLinkRetentionTestDatabase(t, ctx)

	// fidibeki_app must never reach the global security-event ledger: it has
	// no tenant scope to enforce RLS against, so the grant itself is the only
	// enforcement boundary.
	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var allowed bool
		if err := database.QueryRow(ctx,
			`SELECT has_table_privilege('fidibeki_app', 'platform_security_events', $1)`, privilege,
		).Scan(&allowed); err != nil {
			t.Fatalf("check fidibeki_app privilege on platform_security_events: %v", err)
		}
		if allowed {
			t.Errorf("fidibeki_app has %s on platform_security_events, want no grant", privilege)
		}
	}
	for _, privilege := range []string{"SELECT", "INSERT"} {
		var allowed bool
		if err := database.QueryRow(ctx,
			`SELECT has_table_privilege('fidibeki_admin', 'platform_security_events', $1)`, privilege,
		).Scan(&allowed); err != nil {
			t.Fatalf("check fidibeki_admin privilege on platform_security_events: %v", err)
		}
		if !allowed {
			t.Errorf("fidibeki_admin lacks %s on platform_security_events", privilege)
		}
	}
	for _, privilege := range []string{"UPDATE", "DELETE"} {
		var allowed bool
		if err := database.QueryRow(ctx,
			`SELECT has_table_privilege('fidibeki_admin', 'platform_security_events', $1)`, privilege,
		).Scan(&allowed); err != nil {
			t.Fatalf("check fidibeki_admin privilege on platform_security_events: %v", err)
		}
		if allowed {
			t.Errorf("fidibeki_admin has %s on platform_security_events, want append-only (no update/delete)", privilege)
		}
	}
	var publicPrivilegeCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.table_privileges
		WHERE table_schema = 'public'
		  AND table_name = 'platform_security_events'
		  AND grantee = 'PUBLIC'
	`).Scan(&publicPrivilegeCount); err != nil || publicPrivilegeCount != 0 {
		t.Fatalf("PUBLIC platform_security_events privilege count = %d, error = %v, want 0",
			publicPrivilegeCount, err)
	}
	var expiryIndex *string
	if err := database.QueryRow(ctx, `
		SELECT to_regclass('public.idx_platform_admin_magic_links_expires_at')::text
	`).Scan(&expiryIndex); err != nil || expiryIndex == nil {
		t.Fatalf("platform admin magic link expiry index = %v, error = %v", expiryIndex, err)
	}
}

func TestExpiredMagicLinksBoundaryIdempotencyMinimizationAndAudit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := platformMagicLinkRetentionTestDatabase(t, ctx)
	store := MaintenanceStore{Database: database}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	admin := seedPlatformAdmin(t, ctx, database, "retention-admin@example.test")

	unconsumedExpired := insertPlatformMagicLink(t, ctx, database, admin,
		"invite@retention.test", now.Add(-time.Hour), nil)
	consumedExpired := insertPlatformMagicLink(t, ctx, database, admin,
		"owner@retention.test", now.Add(-time.Hour), ptrTime(now.Add(-30*time.Minute)))
	boundaryExpired := insertPlatformMagicLink(t, ctx, database, admin,
		"boundary@retention.test", now, nil)
	notYetExpired := insertPlatformMagicLink(t, ctx, database, admin,
		"future@retention.test", now.Add(time.Second), nil)

	deleted, err := store.ExpiredMagicLinks(ctx, now, 100)
	if err != nil {
		t.Fatalf("ExpiredMagicLinks() error = %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3 (unconsumed, consumed, and exact-boundary expired links)", deleted)
	}
	assertPlatformMagicLinkGone(t, ctx, database, unconsumedExpired)
	assertPlatformMagicLinkGone(t, ctx, database, consumedExpired)
	assertPlatformMagicLinkGone(t, ctx, database, boundaryExpired)
	assertPlatformMagicLinkPresent(t, ctx, database, notYetExpired)

	assertPlatformMagicLinkExpiredEvent(t, ctx, database, unconsumedExpired, false)
	assertPlatformMagicLinkExpiredEvent(t, ctx, database, consumedExpired, true)
	assertPlatformMagicLinkExpiredEvent(t, ctx, database, boundaryExpired, false)

	repeat, err := store.ExpiredMagicLinks(ctx, now, 100)
	if err != nil {
		t.Fatalf("ExpiredMagicLinks() repeat error = %v", err)
	}
	if repeat != 0 {
		t.Fatalf("repeat deleted = %d, want 0 (idempotent rerun)", repeat)
	}
	var eventCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM platform_security_events
		WHERE kind = 'auth.platform_magic_link_expired'
	`).Scan(&eventCount); err != nil {
		t.Fatalf("count platform security events: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("events after rerun = %d, want 3 (no duplicate evidence)", eventCount)
	}
}

func TestExpiredMagicLinksRespectsBatchLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := platformMagicLinkRetentionTestDatabase(t, ctx)
	store := MaintenanceStore{Database: database}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	admin := seedPlatformAdmin(t, ctx, database, "retention-batch@example.test")
	for index := 0; index < 5; index++ {
		insertPlatformMagicLink(t, ctx, database, admin,
			fmt.Sprintf("batch-%d@retention.test", index), now.Add(-time.Hour), nil)
	}

	first, err := store.ExpiredMagicLinks(ctx, now, 2)
	if err != nil {
		t.Fatalf("ExpiredMagicLinks() error = %v", err)
	}
	if first != 2 {
		t.Fatalf("first batch deleted = %d, want 2", first)
	}
	var remaining int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM platform_admin_magic_links WHERE admin_id = $1
	`, admin).Scan(&remaining); err != nil {
		t.Fatalf("count remaining links: %v", err)
	}
	if remaining != 3 {
		t.Fatalf("remaining links = %d, want 3 (limit was not exceeded)", remaining)
	}
}

func TestExpiredMagicLinksConcurrentSweepsDoNotDoubleDeleteOrLoseRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := platformMagicLinkRetentionTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	admin := seedPlatformAdmin(t, ctx, database, "retention-concurrent@example.test")
	const total = 20
	for index := 0; index < total; index++ {
		insertPlatformMagicLink(t, ctx, database, admin,
			fmt.Sprintf("concurrent-%d@retention.test", index), now.Add(-time.Hour), nil)
	}

	const workers = 4
	var wg sync.WaitGroup
	var sum int64
	var mu sync.Mutex
	var runErr error
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := MaintenanceStore{Database: database}
			for {
				deleted, err := store.ExpiredMagicLinks(ctx, now, 3)
				if err != nil {
					mu.Lock()
					runErr = errors.Join(runErr, err)
					mu.Unlock()
					return
				}
				mu.Lock()
				sum += deleted
				mu.Unlock()
				if deleted == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()
	if runErr != nil {
		t.Fatalf("concurrent ExpiredMagicLinks() error = %v", runErr)
	}
	if sum != total {
		t.Fatalf("total deleted across workers = %d, want %d (no double delete, no lost row)", sum, total)
	}
	var remaining int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM platform_admin_magic_links WHERE admin_id = $1
	`, admin).Scan(&remaining); err != nil {
		t.Fatalf("count remaining links: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining links = %d, want 0", remaining)
	}
}

func TestExpiredMagicLinksRollsBackDeletionWhenEvidenceInsertionFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := platformMagicLinkRetentionTestDatabase(t, ctx)
	store := MaintenanceStore{Database: database}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	admin := seedPlatformAdmin(t, ctx, database, "retention-rollback@example.test")
	blockedLink := insertPlatformMagicLink(t, ctx, database, admin,
		"blocked@retention.test", now.Add(-time.Hour), nil)

	// Pre-seed a conflicting event for the exact identity the sweep would
	// produce for this link, forcing the same-statement INSERT to violate
	// the (kind, resource_type, resource_id) uniqueness constraint.
	if _, err := database.Exec(ctx, `
		INSERT INTO platform_security_events (kind, resource_type, resource_id, data)
		VALUES ('auth.platform_magic_link_expired', 'platform_admin_magic_link', $1, '{"was_consumed":false}')
	`, blockedLink); err != nil {
		t.Fatalf("seed conflicting platform security event: %v", err)
	}

	if _, err := store.ExpiredMagicLinks(ctx, now, 100); err == nil {
		t.Fatal("ExpiredMagicLinks() error = nil, want evidence-insertion failure")
	}
	assertPlatformMagicLinkPresent(t, ctx, database, blockedLink)
	var eventCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM platform_security_events
		WHERE kind = 'auth.platform_magic_link_expired'
		  AND resource_id = $1
	`, blockedLink).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("events for blocked link = %d, error = %v, want 1 (no duplicate written)", eventCount, err)
	}

	// The pre-seeded conflicting event never clears on its own, so the retry
	// hits the same unique-constraint violation. The point isn't that it
	// eventually succeeds — it's that the link is never partially deleted or
	// silently dropped: it stays present and safe across repeated attempts.
	if _, err := store.ExpiredMagicLinks(ctx, now.Add(time.Hour), 100); err == nil {
		t.Fatal("retried ExpiredMagicLinks() error = nil, want the same evidence conflict to persist")
	}
	assertPlatformMagicLinkPresent(t, ctx, database, blockedLink)
}

func ptrTime(value time.Time) *time.Time { return &value }

func seedPlatformAdmin(
	t *testing.T, ctx context.Context, database *pgxpool.Pool, email string,
) string {
	t.Helper()
	var id string
	if err := database.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ($1, '{}') RETURNING id
	`, email).Scan(&id); err != nil {
		t.Fatalf("create platform admin %s: %v", email, err)
	}
	return id
}

func insertPlatformMagicLink(
	t *testing.T, ctx context.Context, database *pgxpool.Pool,
	adminID, email string, expiresAt time.Time, consumedAt *time.Time,
) string {
	t.Helper()
	var id string
	createdAt := expiresAt.Add(-24 * time.Hour)
	if err := database.QueryRow(ctx, `
		INSERT INTO platform_admin_magic_links (
			admin_id, email, token_hash, expires_at, consumed_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, adminID, email, auth.HashToken(email), expiresAt, consumedAt, createdAt).Scan(&id); err != nil {
		t.Fatalf("insert platform admin magic link %s: %v", email, err)
	}
	return id
}

func assertPlatformMagicLinkGone(t *testing.T, ctx context.Context, database *pgxpool.Pool, id string) {
	t.Helper()
	var exists bool
	if err := database.QueryRow(ctx, `
		SELECT exists(SELECT 1 FROM platform_admin_magic_links WHERE id = $1)
	`, id).Scan(&exists); err != nil {
		t.Fatalf("check platform admin magic link %s: %v", id, err)
	}
	if exists {
		t.Fatalf("platform admin magic link %s still present, want deleted", id)
	}
}

func assertPlatformMagicLinkPresent(t *testing.T, ctx context.Context, database *pgxpool.Pool, id string) {
	t.Helper()
	var exists bool
	if err := database.QueryRow(ctx, `
		SELECT exists(SELECT 1 FROM platform_admin_magic_links WHERE id = $1)
	`, id).Scan(&exists); err != nil {
		t.Fatalf("check platform admin magic link %s: %v", id, err)
	}
	if !exists {
		t.Fatalf("platform admin magic link %s missing, want preserved", id)
	}
}

func assertPlatformMagicLinkExpiredEvent(
	t *testing.T, ctx context.Context, database *pgxpool.Pool, linkID string, expectConsumed bool,
) {
	t.Helper()
	var resourceType string
	var wasConsumed bool
	var dataKeys []string
	err := database.QueryRow(ctx, `
		SELECT resource_type, (data->>'was_consumed')::boolean,
		       array(SELECT jsonb_object_keys(data))
		FROM platform_security_events
		WHERE kind = 'auth.platform_magic_link_expired' AND resource_id = $1
	`, linkID).Scan(&resourceType, &wasConsumed, &dataKeys)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("no platform security event for magic link %s", linkID)
		}
		t.Fatalf("load platform security event for magic link %s: %v", linkID, err)
	}
	if resourceType != "platform_admin_magic_link" {
		t.Fatalf("event resource_type = %q, want platform_admin_magic_link", resourceType)
	}
	if wasConsumed != expectConsumed {
		t.Fatalf("event was_consumed = %t, want %t", wasConsumed, expectConsumed)
	}
	if len(dataKeys) != 1 || dataKeys[0] != "was_consumed" {
		t.Fatalf("event data keys = %v, want exactly was_consumed (no email, token hash, or admin id)", dataKeys)
	}
}

func platformMagicLinkRetentionTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
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
	migrator, err := db.NewMigrator(directURL, platformMigrationsPath(t))
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
	config.MaxConns = 8
	database, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}
