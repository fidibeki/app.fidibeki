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
	"github.com/jackc/pgx/v5/pgxpool"
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
	connection, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	t.Cleanup(connection.Close)

	now := time.Now().UTC().Truncate(time.Second)
	store := Store{
		Database: connection, Now: func() time.Time { return now },
		RefreshRetryGrace: 10 * time.Second,
	}
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

	startRefresh := make(chan struct{})
	refreshErrors := make(chan error, 2)
	for range 2 {
		go func() {
			<-startRefresh
			_, err := store.RotateSession(
				ctx, auth.HashToken("refresh-one"), auth.HashToken("refresh-two"),
				"integration-test", "127.0.0.1",
			)
			refreshErrors <- err
		}()
	}
	close(startRefresh)
	for range 2 {
		if err := <-refreshErrors; err != nil {
			t.Fatalf("concurrent RotateSession() error = %v", err)
		}
	}
	var adminSessionCount int
	if err := connection.QueryRow(ctx, `
		SELECT count(*) FROM platform_admin_sessions WHERE admin_id = $1
	`, admin.ID).Scan(&adminSessionCount); err != nil || adminSessionCount != 2 {
		t.Fatalf("admin session count = %d, error = %v, want 2", adminSessionCount, err)
	}
	now = now.Add(time.Minute)
	if _, err := store.RotateSession(
		ctx, auth.HashToken("refresh-one"), auth.HashToken("refresh-two"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("late reused RotateSession() error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("refresh-two"), auth.HashToken("refresh-three"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("revoked family RotateSession() error = %v", err)
	}

	fingerprintMagicHash := auth.HashToken("fingerprint-magic")
	if err := store.CreateMagicLink(ctx, admin, fingerprintMagicHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink(fingerprint) error = %v", err)
	}
	if _, err := store.ConsumeMagicLink(
		ctx, fingerprintMagicHash, auth.HashToken("fingerprint-one"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("ConsumeMagicLink(fingerprint) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("fingerprint-one"), auth.HashToken("fingerprint-two"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("RotateSession(fingerprint) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("fingerprint-one"), auth.HashToken("fingerprint-three"), "different-agent", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("mismatched fingerprint RotateSession() error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("fingerprint-two"), auth.HashToken("fingerprint-four"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("mismatched fingerprint family error = %v", err)
	}

	raceMagicHash := auth.HashToken("race-magic")
	if err := store.CreateMagicLink(ctx, admin, raceMagicHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink(race) error = %v", err)
	}
	if _, err := store.ConsumeMagicLink(
		ctx, raceMagicHash, auth.HashToken("race-one"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("ConsumeMagicLink(race) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("race-one"), auth.HashToken("race-two"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("RotateSession(race) error = %v", err)
	}
	now = now.Add(time.Minute)
	type rotationResult struct {
		name string
		err  error
	}
	startRace := make(chan struct{})
	raceResults := make(chan rotationResult, 2)
	go func() {
		<-startRace
		_, err := store.RotateSession(
			ctx, auth.HashToken("race-two"), auth.HashToken("race-three"), "integration-test", "127.0.0.1",
		)
		raceResults <- rotationResult{name: "successor", err: err}
	}()
	go func() {
		<-startRace
		_, err := store.RotateSession(
			ctx, auth.HashToken("race-one"), auth.HashToken("race-two"), "integration-test", "127.0.0.1",
		)
		raceResults <- rotationResult{name: "reuse", err: err}
	}()
	close(startRace)
	for range 2 {
		result := <-raceResults
		if result.name == "reuse" && !errors.Is(result.err, ErrRefreshReuse) {
			t.Fatalf("overlapping reuse error = %v", result.err)
		}
		if result.name == "successor" && result.err != nil && !errors.Is(result.err, ErrRefreshReuse) {
			t.Fatalf("overlapping successor error = %v", result.err)
		}
	}
	var activeSessionCount int
	if err := connection.QueryRow(ctx, `
		SELECT count(*) FROM platform_admin_sessions WHERE admin_id = $1 AND revoked_at IS NULL
	`, admin.ID).Scan(&activeSessionCount); err != nil || activeSessionCount != 0 {
		t.Fatalf("active sessions after overlapping reuse = %d, error = %v", activeSessionCount, err)
	}

	chainMagicHash := auth.HashToken("chain-magic")
	if err := store.CreateMagicLink(ctx, admin, chainMagicHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink(chain) error = %v", err)
	}
	if _, err := store.ConsumeMagicLink(
		ctx, chainMagicHash, auth.HashToken("chain-one"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("ConsumeMagicLink(chain) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("chain-one"), auth.HashToken("chain-two"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("first RotateSession(chain) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("chain-two"), auth.HashToken("chain-three"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("second RotateSession(chain) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("chain-one"), auth.HashToken("chain-two"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("stale predecessor RotateSession() error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("chain-three"), auth.HashToken("chain-four"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("stale predecessor family error = %v", err)
	}

	logoutMagicHash := auth.HashToken("logout-magic")
	if err := store.CreateMagicLink(ctx, admin, logoutMagicHash, now.Add(time.Minute)); err != nil {
		t.Fatalf("CreateMagicLink(logout) error = %v", err)
	}
	if _, err := store.ConsumeMagicLink(
		ctx, logoutMagicHash, auth.HashToken("logout-one"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("ConsumeMagicLink(logout) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("logout-one"), auth.HashToken("logout-two"), "integration-test", "127.0.0.1",
	); err != nil {
		t.Fatalf("RotateSession(logout) error = %v", err)
	}
	if err := store.RevokeSession(ctx, auth.HashToken("logout-one")); err != nil {
		t.Fatalf("RevokeSession(predecessor) error = %v", err)
	}
	if _, err := store.RotateSession(
		ctx, auth.HashToken("logout-two"), auth.HashToken("logout-three"), "integration-test", "127.0.0.1",
	); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("logged-out successor RotateSession() error = %v", err)
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
