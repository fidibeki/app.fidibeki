package platformadmin

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStorePersistsAuthenticationAndTenantLifecycle(t *testing.T) {
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
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(databaseURL, platformMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close(ctx) })

	now := time.Now().UTC().Truncate(time.Second)
	store := Store{Database: connection, Now: func() time.Time { return now }}
	permissions := auth.PlatformPermissionStrings()
	admin, err := store.BootstrapAdmin(ctx, "ADMIN@example.com", permissions)
	if err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	again, err := store.BootstrapAdmin(ctx, "admin@example.com", permissions)
	if err != nil || again.ID != admin.ID {
		t.Fatalf("idempotent BootstrapAdmin() = %#v, %v", again, err)
	}
	found, err := store.FindAdminByEmail(ctx, " Admin@Example.com ")
	if err != nil || found.ID != admin.ID || len(found.Permissions) != 9 {
		t.Fatalf("FindAdminByEmail() = %#v, %v", found, err)
	}

	magicHash := auth.HashToken("magic-token")
	if err := store.CreateMagicLink(ctx, admin, magicHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink() error = %v", err)
	}
	consumed, err := store.ConsumeMagicLink(
		ctx, magicHash, auth.HashToken("refresh-one"), "integration-test", "127.0.0.1",
	)
	if err != nil || consumed.ID != admin.ID {
		t.Fatalf("ConsumeMagicLink() = %#v, %v", consumed, err)
	}
	if _, err := store.ConsumeMagicLink(
		ctx, magicHash, auth.HashToken("other-refresh"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrMagicLinkInvalid) {
		t.Fatalf("second ConsumeMagicLink() error = %v", err)
	}

	rotated, err := store.RotateSession(
		ctx, auth.HashToken("refresh-one"), auth.HashToken("refresh-two"), "integration-test", "127.0.0.1",
	)
	if err != nil || rotated.ID != admin.ID {
		t.Fatalf("RotateSession() = %#v, %v", rotated, err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("refresh-one"), auth.HashToken("refresh-three"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("reused RotateSession() error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("refresh-two"), auth.HashToken("refresh-four"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("revoked family RotateSession() error = %v", err)
	}
	if err := store.RevokeSession(ctx, auth.HashToken("missing-refresh")); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	deletedMagicHash := auth.HashToken("deleted-magic")
	if err := store.CreateMagicLink(ctx, admin, deletedMagicHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink(deleted) error = %v", err)
	}
	if err := store.DeleteMagicLink(ctx, deletedMagicHash); err != nil {
		t.Fatalf("DeleteMagicLink() error = %v", err)
	}

	trialEnd := now.Add(DefaultTrial)
	record, err := store.CreateTenant(ctx, CreateTenantParams{
		Name: "Acme", Slug: "acme", Plan: tenant.PlanGrowth,
		Status: tenant.StatusTrial, TrialEndsAt: &trialEnd,
	})
	if err != nil {
		t.Fatalf("CreateTenant() error = %v", err)
	}
	if _, err := store.CreateTenant(ctx, CreateTenantParams{
		Name: "Duplicate", Slug: "acme", Plan: tenant.PlanStarter,
		Status: tenant.StatusActive,
	}); !errors.Is(err, ErrSlugConflict) {
		t.Fatalf("duplicate CreateTenant() error = %v", err)
	}
	fetched, err := store.GetTenant(ctx, record.ID)
	if err != nil || fetched.Slug != "acme" {
		t.Fatalf("GetTenant() = %#v, %v", fetched, err)
	}
	record.Name = "Acme Updated"
	record.Status = tenant.StatusActive
	record.TrialEndsAt = nil
	updated, err := store.UpdateTenant(ctx, record)
	if err != nil || updated.Name != "Acme Updated" || updated.Status != tenant.StatusActive {
		t.Fatalf("UpdateTenant() = %#v, %v", updated, err)
	}
	records, err := store.ListTenants(ctx)
	if err != nil || len(records) != 1 {
		t.Fatalf("ListTenants() = %#v, %v", records, err)
	}
	if err := store.DeleteTenant(ctx, record.ID); err != nil {
		t.Fatalf("DeleteTenant() error = %v", err)
	}
	if err := store.DeleteTenant(ctx, record.ID); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("DeleteTenant(missing) error = %v", err)
	}
	if _, err := store.GetTenant(ctx, record.ID); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("GetTenant(deleted) error = %v", err)
	}
}

func platformMigrationsPath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test path")
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "../../../infra/migrations"))
	if err != nil {
		t.Fatalf("resolve migration path: %v", err)
	}
	return path
}
