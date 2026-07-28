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
	if err != nil || found.ID != admin.ID || len(found.Permissions) != 13 {
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
		OwnerEmail: "owner@acme.test", ActorPlatformAdminID: admin.ID,
	})
	if err != nil {
		t.Fatalf("CreateTenant() error = %v", err)
	}
	var ownerMemberID string
	if err := connection.QueryRow(ctx, `
		SELECT owner_member_id FROM tenants WHERE id = $1
	`, record.ID).Scan(&ownerMemberID); err != nil {
		t.Fatalf("read provisioned tenant owner: %v", err)
	}
	var ownerEmail string
	if err := connection.QueryRow(ctx, `
		SELECT email FROM tenant_members WHERE tenant_id = $1 AND id = $2
	`, record.ID, ownerMemberID).Scan(&ownerEmail); err != nil || ownerEmail != "owner@acme.test" {
		t.Fatalf("provisioned owner email = %q, error = %v", ownerEmail, err)
	}
	if _, err := store.CreateTenant(ctx, CreateTenantParams{
		Name: "Duplicate", Slug: "acme", Plan: tenant.PlanStarter,
		Status: tenant.StatusActive, OwnerEmail: "other@acme.test", ActorPlatformAdminID: admin.ID,
	}); !errors.Is(err, ErrSlugConflict) {
		t.Fatalf("duplicate CreateTenant() error = %v", err)
	}
	var recoveryTargetID string
	if err := connection.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, last_login_at)
		VALUES ($1, 'recovery@acme.test', '{}', $2)
		RETURNING id
	`, record.ID, now).Scan(&recoveryTargetID); err != nil {
		t.Fatalf("create recovery target: %v", err)
	}
	recovery := RecoverOwnershipParams{
		TenantID: record.ID, ActorPlatformAdminID: admin.ID, TargetMemberID: recoveryTargetID,
		Confirmation: "acme", VerificationReference: "support-2481",
		Reason: "Verified billing email and incorporation record.",
	}
	if _, err := connection.Exec(ctx, `UPDATE platform_admins SET last_login_at = $2 WHERE id = $1`, admin.ID, now.Add(-11*time.Minute)); err != nil {
		t.Fatalf("age platform admin authentication: %v", err)
	}
	if err := store.RecoverOwnership(ctx, recovery); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("stale RecoverOwnership() error = %v", err)
	}
	if _, err := connection.Exec(ctx, `UPDATE platform_admins SET last_login_at = $2 WHERE id = $1`, admin.ID, now); err != nil {
		t.Fatalf("refresh platform admin recent auth: %v", err)
	}
	recovery.VerificationReference = ""
	if err := store.RecoverOwnership(ctx, recovery); !errors.Is(err, ErrRecoveryVerificationInvalid) {
		t.Fatalf("incomplete RecoverOwnership() error = %v", err)
	}
	recovery.VerificationReference = "support-2481"
	if _, err := connection.Exec(ctx, `UPDATE tenant_members SET last_login_at = NULL WHERE id = $1`, recoveryTargetID); err != nil {
		t.Fatalf("make recovery target unverified: %v", err)
	}
	if err := store.RecoverOwnership(ctx, recovery); !errors.Is(err, ErrOwnershipTargetInvalid) {
		t.Fatalf("unverified RecoverOwnership() error = %v", err)
	}
	if _, err := connection.Exec(ctx, `UPDATE tenant_members SET last_login_at = $2 WHERE id = $1`, recoveryTargetID, now); err != nil {
		t.Fatalf("verify recovery target: %v", err)
	}
	recovery.TargetMemberID = "00000000-0000-4000-8000-000000000099"
	if err := store.RecoverOwnership(ctx, recovery); !errors.Is(err, ErrOwnershipTargetInvalid) {
		t.Fatalf("missing-target RecoverOwnership() error = %v", err)
	}
	recovery.TargetMemberID = recoveryTargetID
	recovery.Confirmation = "wrong"
	if err := store.RecoverOwnership(ctx, recovery); !errors.Is(err, ErrOwnershipConfirmation) {
		t.Fatalf("mismatched RecoverOwnership() error = %v", err)
	}
	recovery.Confirmation = "acme"
	if err := store.RecoverOwnership(ctx, recovery); err != nil {
		t.Fatalf("RecoverOwnership() error = %v", err)
	}
	var recoveredOwnerID string
	var formerOwnerStatus string
	if err := connection.QueryRow(ctx, `SELECT owner_member_id FROM tenants WHERE id = $1`, record.ID).Scan(&recoveredOwnerID); err != nil {
		t.Fatalf("read recovered owner: %v", err)
	}
	if err := connection.QueryRow(ctx, `SELECT status FROM tenant_members WHERE tenant_id = $1 AND id = $2`, record.ID, ownerMemberID).Scan(&formerOwnerStatus); err != nil {
		t.Fatalf("read former owner status: %v", err)
	}
	if recoveredOwnerID != recoveryTargetID || formerOwnerStatus != "deactivated" {
		t.Fatalf("recovered owner = %q, former status = %q", recoveredOwnerID, formerOwnerStatus)
	}
	var recoveryAuditCount int
	if err := connection.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind IN ('ownership.recovery_denied', 'ownership.recovered')
	`, record.ID).Scan(&recoveryAuditCount); err != nil || recoveryAuditCount != 6 {
		t.Fatalf("recovery audit count = %d, error = %v", recoveryAuditCount, err)
	}
	var recoverySessionAudit bool
	if err := connection.QueryRow(ctx, `
		SELECT data ? 'target_revoked_session_count'
		   AND data ? 'former_owner_revoked_session_count'
		   AND data ? 'sessions_revoked_at'
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'ownership.recovered'
	`, record.ID).Scan(&recoverySessionAudit); err != nil || !recoverySessionAudit {
		t.Fatalf("recovery session audit = %t, error = %v", recoverySessionAudit, err)
	}
	fetched, err := store.GetTenant(ctx, record.ID)
	if err != nil || fetched.Slug != "acme" {
		t.Fatalf("GetTenant() = %#v, %v", fetched, err)
	}
	record = fetched
	record.Name = "Acme Updated"
	record.Status = tenant.StatusActive
	record.TrialEndsAt = nil
	updated, err := store.UpdateTenant(ctx, record, admin.ID)
	if err != nil || updated.Name != "Acme Updated" || updated.Status != tenant.StatusActive {
		t.Fatalf("UpdateTenant() = %#v, %v", updated, err)
	}
	var updateAuditActor string
	if err := connection.QueryRow(ctx, `
		SELECT actor_platform_admin_id::text FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'workspace.updated'
	`, record.ID).Scan(&updateAuditActor); err != nil || updateAuditActor != admin.ID {
		t.Fatalf("workspace update audit actor = %q, error = %v", updateAuditActor, err)
	}
	var updateAuditDetail bool
	if err := connection.QueryRow(ctx, `
		SELECT data->'changed_fields' ? 'name'
		   AND data->'changed_fields' ? 'status'
		   AND data->>'status_from' = 'trial'
		   AND data->>'status_to' = 'active'
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'workspace.updated'
	`, record.ID).Scan(&updateAuditDetail); err != nil || !updateAuditDetail {
		t.Fatalf("workspace update audit detail = %t, error = %v", updateAuditDetail, err)
	}
	stale := updated
	fresh, err := store.GetTenant(ctx, updated.ID)
	if err != nil {
		t.Fatalf("GetTenant(after update) error = %v", err)
	}
	fresh.Name = "Acme Fresh"
	if _, err := store.UpdateTenant(ctx, fresh, admin.ID); err != nil {
		t.Fatalf("fresh UpdateTenant() error = %v", err)
	}
	stale.Plan = tenant.PlanScale
	if _, err := store.UpdateTenant(ctx, stale, admin.ID); !errors.Is(err, ErrTenantUpdateConflict) {
		t.Fatalf("stale UpdateTenant() error = %v, want conflict", err)
	}
	confirmed, err := store.GetTenant(ctx, updated.ID)
	if err != nil || confirmed.Name != "Acme Fresh" || confirmed.Plan != tenant.PlanGrowth {
		t.Fatalf("concurrent update preserved = %#v, error = %v", confirmed, err)
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

func TestStoreBackofficeOverviewAndKeysetDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := platformMagicLinkRetentionTestDatabase(t, ctx)
	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	store := Store{Database: database, Now: func() time.Time { return now }}

	for _, row := range []struct {
		name, slug, plan, status string
		trialEndsAt, createdAt   *time.Time
	}{
		{name: "Newest Acme", slug: "newest-acme", plan: "growth", status: "active", createdAt: ptrTime(now)},
		{name: "Older Acme", slug: "older-acme", plan: "growth", status: "active", createdAt: ptrTime(now.Add(-time.Hour))},
		{name: "Trial Team", slug: "trial-team", plan: "starter", status: "trial", trialEndsAt: ptrTime(now.Add(48 * time.Hour)), createdAt: ptrTime(now.Add(-31 * 24 * time.Hour))},
		{name: "Suspended Team", slug: "suspended-team", plan: "scale", status: "suspended", createdAt: ptrTime(now.Add(-2 * 24 * time.Hour))},
	} {
		if _, err := database.Exec(ctx, `
			INSERT INTO tenants (name, slug, plan, status, trial_ends_at, created_at, updated_at)
			VALUES ($1, $2, $3::tenant_plan, $4::tenant_status, $5, $6, $6)
		`, row.name, row.slug, row.plan, row.status, row.trialEndsAt, row.createdAt); err != nil {
			t.Fatalf("seed %s: %v", row.slug, err)
		}
	}

	overview, err := store.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.WorkspaceCount != 4 || overview.ActiveWorkspaceCount != 2 ||
		overview.TrialWorkspaceCount != 1 || overview.SuspendedWorkspaceCount != 1 ||
		overview.TrialsEndingSoonCount != 1 || overview.RecentWorkspaceCount != 3 {
		t.Fatalf("overview = %#v", overview)
	}

	first, err := store.ListBackofficeTenants(ctx, BackofficeTenantListParams{
		Query: "acme", Status: tenant.StatusActive, Plan: tenant.PlanGrowth, Limit: 1,
	})
	if err != nil || len(first.Tenants) != 1 || first.Tenants[0].Slug != "newest-acme" || first.NextCursor == "" {
		t.Fatalf("first directory page = %#v, error = %v", first, err)
	}
	second, err := store.ListBackofficeTenants(ctx, BackofficeTenantListParams{
		Query: "acme", Status: tenant.StatusActive, Plan: tenant.PlanGrowth, Limit: 1, Cursor: first.NextCursor,
	})
	if err != nil || len(second.Tenants) != 1 || second.Tenants[0].Slug != "older-acme" || second.NextCursor != "" {
		t.Fatalf("second directory page = %#v, error = %v", second, err)
	}
	if _, err := store.ListBackofficeTenants(ctx, BackofficeTenantListParams{Limit: 1, Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("invalid cursor error = nil")
	}

	var newestID string
	if err := database.QueryRow(ctx, `SELECT id FROM tenants WHERE slug = 'newest-acme'`).Scan(&newestID); err != nil {
		t.Fatalf("find audit tenant: %v", err)
	}
	memberIDs := make([]string, 3)
	for index, createdAt := range []time.Time{now, now.Add(-time.Minute), now.Add(-2 * time.Minute)} {
		if err := database.QueryRow(ctx, `
			INSERT INTO tenant_members (tenant_id, email, permissions, last_login_at, created_at)
			VALUES ($1, $2, '{}', $3, $4)
			RETURNING id
		`, newestID, "member"+string(rune('a'+index))+"@acme.test", now, createdAt).Scan(&memberIDs[index]); err != nil {
			t.Fatalf("seed recovery candidate %d: %v", index, err)
		}
	}
	if _, err := database.Exec(ctx, `UPDATE tenants SET owner_member_id = $2 WHERE id = $1`, newestID, memberIDs[0]); err != nil {
		t.Fatalf("set backoffice owner: %v", err)
	}
	detail, err := store.GetBackofficeTenant(ctx, newestID)
	if err != nil || detail.Owner == nil || detail.Owner.ID != memberIDs[0] || detail.MemberCount != 3 {
		t.Fatalf("GetBackofficeTenant() = %#v, error = %v", detail, err)
	}
	firstCandidates, err := store.ListBackofficeRecoveryCandidates(ctx, newestID, BackofficeRecoveryCandidateListParams{Limit: 1})
	if err != nil || len(firstCandidates.Members) != 1 || firstCandidates.Members[0].ID != memberIDs[1] || firstCandidates.NextCursor == "" {
		t.Fatalf("first recovery candidate page = %#v, error = %v", firstCandidates, err)
	}
	secondCandidates, err := store.ListBackofficeRecoveryCandidates(ctx, newestID, BackofficeRecoveryCandidateListParams{Limit: 1, Cursor: firstCandidates.NextCursor})
	if err != nil || len(secondCandidates.Members) != 1 || secondCandidates.Members[0].ID != memberIDs[2] || secondCandidates.NextCursor != "" {
		t.Fatalf("second recovery candidate page = %#v, error = %v", secondCandidates, err)
	}
	if _, err := store.ListBackofficeRecoveryCandidates(ctx, newestID, BackofficeRecoveryCandidateListParams{Limit: 1, Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("invalid recovery candidate cursor error = nil")
	}

	for index, createdAt := range []time.Time{now, now.Add(-time.Minute)} {
		if _, err := database.Exec(ctx, `
			INSERT INTO tenant_access_events (tenant_id, kind, data, created_at)
			VALUES ($1, $2, '{"private":"not in projection"}'::jsonb, $3)
		`, newestID, "workspace.audit_"+string(rune('a'+index)), createdAt); err != nil {
			t.Fatalf("seed audit event %d: %v", index, err)
		}
	}
	firstAudit, err := store.ListBackofficeAccessEvents(ctx, newestID, BackofficeAccessEventListParams{Limit: 1})
	if err != nil || len(firstAudit.Events) != 1 || firstAudit.NextCursor == "" || firstAudit.Events[0].Kind != "workspace.audit_a" {
		t.Fatalf("first audit page = %#v, error = %v", firstAudit, err)
	}
	secondAudit, err := store.ListBackofficeAccessEvents(ctx, newestID, BackofficeAccessEventListParams{Limit: 1, Cursor: firstAudit.NextCursor})
	if err != nil || len(secondAudit.Events) != 1 || secondAudit.NextCursor != "" || secondAudit.Events[0].Kind != "workspace.audit_b" {
		t.Fatalf("second audit page = %#v, error = %v", secondAudit, err)
	}
	if _, err := store.ListBackofficeAccessEvents(ctx, newestID, BackofficeAccessEventListParams{Limit: 1, Cursor: "not-a-cursor"}); err == nil {
		t.Fatal("invalid audit cursor error = nil")
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
