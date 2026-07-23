package tenantmember

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestWorkspaceOwnershipTransferSerializesAndRespectsTenantIsolation(t *testing.T) {
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
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	var tenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	memberIDs := make([]string, 3)
	for index, email := range []string{"owner@acme.test", "first@acme.test", "second@acme.test"} {
		permissions := []string{"project:read"}
		if index == 0 {
			permissions = auth.TenantPermissionStrings()
		}
		if err := root.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions, last_login_at)
			VALUES ($1, $2, $3, $4) RETURNING id
		`, tenantID, email, permissions, now).Scan(&memberIDs[index]); err != nil {
			t.Fatalf("create member %s: %v", email, err)
		}
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET owner_member_id = $2 WHERE id = $1`, tenantID, memberIDs[0]); err != nil {
		t.Fatalf("assign owner: %v", err)
	}
	var otherTenantID, otherOwnerID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Beta', 'beta', 'starter', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, last_login_at)
		VALUES ($1, 'owner@beta.test', $2, $3) RETURNING id
	`, otherTenantID, auth.TenantPermissionStrings(), now).Scan(&otherOwnerID); err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET owner_member_id = $2 WHERE id = $1`, otherTenantID, otherOwnerID); err != nil {
		t.Fatalf("assign other owner: %v", err)
	}

	pools, err := db.OpenPools(ctx, tenantRoleURL(t, directURL, "fidibeki_app", "app-password"), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open pools: %v", err)
	}
	t.Cleanup(pools.Close)
	store := Store{Now: func() time.Time { return now }}
	type result struct {
		member Member
		err    error
	}
	transfer := func(actorID, targetID string, start <-chan struct{}, results chan<- result) {
		<-start
		tx, err := pools.App.Begin(ctx)
		if err != nil {
			results <- result{err: err}
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
			results <- result{err: err}
			return
		}
		member, err := store.TransferOwnership(ctx, tx, tenantID, actorID, targetID, "acme")
		if err == nil || errors.Is(err, ErrOwnerProtection) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				results <- result{err: commitErr}
				return
			}
		}
		results <- result{member: member, err: err}
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	go transfer(memberIDs[0], memberIDs[1], start, results)
	go transfer(memberIDs[0], memberIDs[2], start, results)
	close(start)
	var winner Member
	var successCount, deniedCount int
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successCount++
			winner = result.member
		case errors.Is(result.err, ErrOwnerProtection):
			deniedCount++
		default:
			t.Fatalf("concurrent transfer error = %v", result.err)
		}
	}
	if successCount != 1 || deniedCount != 1 {
		t.Fatalf("concurrent transfer results = %d success, %d denied", successCount, deniedCount)
	}
	var storedOwnerID string
	if err := root.QueryRow(ctx, `SELECT owner_member_id FROM tenants WHERE id = $1`, tenantID).Scan(&storedOwnerID); err != nil {
		t.Fatalf("read serialized owner: %v", err)
	}
	if storedOwnerID != winner.ID {
		t.Fatalf("stored owner = %q, winner = %q", storedOwnerID, winner.ID)
	}
	var transferEvents, deniedEvents int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE kind = 'ownership.transferred'),
		       count(*) FILTER (WHERE kind = 'ownership.transfer_denied')
		FROM tenant_access_events WHERE tenant_id = $1
	`, tenantID).Scan(&transferEvents, &deniedEvents); err != nil || transferEvents != 1 || deniedEvents != 1 {
		t.Fatalf("ownership events = %d transferred/%d denied, error = %v", transferEvents, deniedEvents, err)
	}

	if _, err := root.Exec(ctx, `UPDATE tenant_members SET last_login_at = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, winner.ID, now.Add(-11*time.Minute)); err != nil {
		t.Fatalf("age winner login: %v", err)
	}
	tx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin recent-auth probe: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		t.Fatalf("set recent-auth tenant: %v", err)
	}
	if _, err := store.TransferOwnership(ctx, tx, tenantID, winner.ID, memberIDs[0], "acme"); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("stale owner transfer error = %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit recent-auth denial: %v", err)
	}

	isolationTx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin isolation probe: %v", err)
	}
	defer func() { _ = isolationTx.Rollback(ctx) }()
	if _, err := isolationTx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		t.Fatalf("set isolation tenant: %v", err)
	}
	if _, err := store.TransferOwnership(ctx, isolationTx, otherTenantID, otherOwnerID, memberIDs[0], "beta"); err == nil {
		t.Fatal("cross-tenant ownership transfer unexpectedly succeeded")
	}
	var unchangedOtherOwner string
	if err := root.QueryRow(ctx, `SELECT owner_member_id FROM tenants WHERE id = $1`, otherTenantID).Scan(&unchangedOtherOwner); err != nil || unchangedOtherOwner != otherOwnerID {
		t.Fatalf("other owner = %q, error = %v", unchangedOtherOwner, err)
	}
}
