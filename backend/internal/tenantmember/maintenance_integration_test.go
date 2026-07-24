package tenantmember

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestExpiredMagicLinksBoundaryIdempotencyIsolationAndAudit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := magicLinkRetentionTestDatabase(t, ctx)
	store := MaintenanceStore{Database: database}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	tenantA, memberA := seedMagicLinkRetentionTenant(t, ctx, database, "retention-a")
	tenantB, memberB := seedMagicLinkRetentionTenant(t, ctx, database, "retention-b")

	unconsumedExpiredA := insertMagicLink(t, ctx, database, tenantA, nil,
		"invite-a@retention.test", now.Add(-time.Hour), nil)
	consumedExpiredA := insertMagicLink(t, ctx, database, tenantA, &memberA,
		"owner-a@retention.test", now.Add(-time.Hour), ptrTime(now.Add(-30*time.Minute)))
	boundaryExpiredA := insertMagicLink(t, ctx, database, tenantA, nil,
		"boundary-a@retention.test", now, nil)
	notYetExpiredA := insertMagicLink(t, ctx, database, tenantA, nil,
		"future-a@retention.test", now.Add(time.Second), nil)
	expiredB := insertMagicLink(t, ctx, database, tenantB, &memberB,
		"owner-b@retention.test", now.Add(-time.Hour), ptrTime(now.Add(-time.Minute)))
	notYetExpiredB := insertMagicLink(t, ctx, database, tenantB, nil,
		"future-b@retention.test", now.Add(time.Hour), nil)

	deleted, err := store.ExpiredMagicLinks(ctx, now, 100)
	if err != nil {
		t.Fatalf("ExpiredMagicLinks() error = %v", err)
	}
	if deleted != 4 {
		t.Fatalf("deleted = %d, want 4 (unconsumed, consumed, and exact-boundary expired links)", deleted)
	}
	assertMagicLinkGone(t, ctx, database, unconsumedExpiredA)
	assertMagicLinkGone(t, ctx, database, consumedExpiredA)
	assertMagicLinkGone(t, ctx, database, boundaryExpiredA)
	assertMagicLinkGone(t, ctx, database, expiredB)
	assertMagicLinkPresent(t, ctx, database, notYetExpiredA)
	assertMagicLinkPresent(t, ctx, database, notYetExpiredB)

	assertMagicLinkExpiredAudit(t, ctx, database, tenantA, unconsumedExpiredA, "", false)
	assertMagicLinkExpiredAudit(t, ctx, database, tenantA, consumedExpiredA, memberA, true)
	assertMagicLinkExpiredAudit(t, ctx, database, tenantA, boundaryExpiredA, "", false)
	assertMagicLinkExpiredAudit(t, ctx, database, tenantB, expiredB, memberB, true)

	var crossTenantLeak int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND (data->>'magic_link_id')::uuid = $2
	`, tenantA, expiredB).Scan(&crossTenantLeak); err != nil {
		t.Fatalf("check cross-tenant audit leak: %v", err)
	}
	if crossTenantLeak != 0 {
		t.Fatalf("tenant A audit log contains tenant B's magic link event")
	}

	repeat, err := store.ExpiredMagicLinks(ctx, now, 100)
	if err != nil {
		t.Fatalf("ExpiredMagicLinks() repeat error = %v", err)
	}
	if repeat != 0 {
		t.Fatalf("repeat deleted = %d, want 0 (idempotent rerun)", repeat)
	}
	var auditCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events WHERE kind = 'auth.magic_link_expired'
	`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount != 4 {
		t.Fatalf("audit events after rerun = %d, want 4 (no duplicate evidence)", auditCount)
	}
}

func TestExpiredMagicLinksRespectsBatchLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := magicLinkRetentionTestDatabase(t, ctx)
	store := MaintenanceStore{Database: database}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	tenantID, _ := seedMagicLinkRetentionTenant(t, ctx, database, "retention-batch")
	for index := 0; index < 5; index++ {
		insertMagicLink(t, ctx, database, tenantID, nil,
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
		SELECT count(*) FROM tenant_magic_links WHERE tenant_id = $1
	`, tenantID).Scan(&remaining); err != nil {
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
	database := magicLinkRetentionTestDatabase(t, ctx)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	tenantID, _ := seedMagicLinkRetentionTenant(t, ctx, database, "retention-concurrent")
	const total = 20
	for index := 0; index < total; index++ {
		insertMagicLink(t, ctx, database, tenantID, nil,
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
		SELECT count(*) FROM tenant_magic_links WHERE tenant_id = $1
	`, tenantID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining links: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining links = %d, want 0", remaining)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

func seedMagicLinkRetentionTenant(
	t *testing.T, ctx context.Context, database *pgxpool.Pool, slug string,
) (tenantID string, memberID string) {
	t.Helper()
	if err := database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ($1, $1, 'growth', 'active') RETURNING id
	`, slug).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, $2, '{}') RETURNING id
	`, tenantID, slug+"-owner@retention.test").Scan(&memberID); err != nil {
		t.Fatalf("create member for %s: %v", slug, err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE tenants SET owner_member_id = $2 WHERE id = $1
	`, tenantID, memberID); err != nil {
		t.Fatalf("assign owner for %s: %v", slug, err)
	}
	return tenantID, memberID
}

func insertMagicLink(
	t *testing.T, ctx context.Context, database *pgxpool.Pool,
	tenantID string, memberID *string, email string, expiresAt time.Time, consumedAt *time.Time,
) string {
	t.Helper()
	var id string
	createdAt := expiresAt.Add(-24 * time.Hour)
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_magic_links (
			tenant_id, member_id, email, token_hash, expires_at, consumed_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, tenantID, memberID, email, fixedTokenHash(email), expiresAt, consumedAt, createdAt).Scan(&id); err != nil {
		t.Fatalf("insert magic link %s: %v", email, err)
	}
	return id
}

func fixedTokenHash(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(digest[:])
}

func assertMagicLinkGone(t *testing.T, ctx context.Context, database *pgxpool.Pool, id string) {
	t.Helper()
	var exists bool
	if err := database.QueryRow(ctx, `
		SELECT exists(SELECT 1 FROM tenant_magic_links WHERE id = $1)
	`, id).Scan(&exists); err != nil {
		t.Fatalf("check magic link %s: %v", id, err)
	}
	if exists {
		t.Fatalf("magic link %s still present, want deleted", id)
	}
}

func assertMagicLinkPresent(t *testing.T, ctx context.Context, database *pgxpool.Pool, id string) {
	t.Helper()
	var exists bool
	if err := database.QueryRow(ctx, `
		SELECT exists(SELECT 1 FROM tenant_magic_links WHERE id = $1)
	`, id).Scan(&exists); err != nil {
		t.Fatalf("check magic link %s: %v", id, err)
	}
	if !exists {
		t.Fatalf("magic link %s missing, want preserved (not yet expired)", id)
	}
}

func assertMagicLinkExpiredAudit(
	t *testing.T, ctx context.Context, database *pgxpool.Pool,
	tenantID, linkID, expectSubjectMember string, expectConsumed bool,
) {
	t.Helper()
	var subjectMemberID *string
	var wasConsumed bool
	var dataKeys []string
	err := database.QueryRow(ctx, `
		SELECT subject_member_id, (data->>'was_consumed')::boolean,
		       array(SELECT jsonb_object_keys(data))
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'auth.magic_link_expired'
		  AND data->>'magic_link_id' = $2
	`, tenantID, linkID).Scan(&subjectMemberID, &wasConsumed, &dataKeys)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("no audit event for magic link %s", linkID)
		}
		t.Fatalf("load audit event for magic link %s: %v", linkID, err)
	}
	gotSubject := ""
	if subjectMemberID != nil {
		gotSubject = *subjectMemberID
	}
	if gotSubject != expectSubjectMember {
		t.Fatalf("audit subject_member_id = %q, want %q", gotSubject, expectSubjectMember)
	}
	if wasConsumed != expectConsumed {
		t.Fatalf("audit was_consumed = %t, want %t", wasConsumed, expectConsumed)
	}
	if len(dataKeys) != 2 {
		t.Fatalf("audit data keys = %v, want exactly magic_link_id and was_consumed", dataKeys)
	}
}

func magicLinkRetentionTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
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
	migrator, err := db.NewMigrator(directURL, tenantMigrationsPath(t))
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
