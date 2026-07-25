package db_test

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMigrationsEnforceTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	const appPassword = "app password with 'quotes'"
	const adminPassword = `admin:/password?#@`
	const latestMigrationVersion = 111

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
	migrator, err := db.NewMigrator(directURL, migrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() {
		if err := migrator.Close(); err != nil {
			t.Errorf("close migrator: %v", err)
		}
	})
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	appURL := roleURL(t, directURL, "fidibeki_app", appPassword)
	adminURL := roleURL(t, directURL, "fidibeki_admin", adminPassword)
	if err := db.ConfigureRuntimeRoles(ctx, directURL, appURL, adminURL); err != nil {
		t.Fatalf("configure runtime roles: %v", err)
	}

	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as migration user: %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(ctx); err != nil {
			t.Errorf("close migration connection: %v", err)
		}
	})
	for role, wantBypassRLS := range map[string]bool{
		"fidibeki_app": false, "fidibeki_admin": true,
	} {
		var canLogin, isSuperuser, bypassRLS bool
		if err := root.QueryRow(ctx, `
			SELECT rolcanlogin, rolsuper, rolbypassrls
			FROM pg_roles
			WHERE rolname = $1
		`, role).Scan(&canLogin, &isSuperuser, &bypassRLS); err != nil {
			t.Fatalf("read %s role flags: %v", role, err)
		}
		if !canLogin || isSuperuser || bypassRLS != wantBypassRLS {
			t.Fatalf(
				"unexpected %s role flags: login=%t superuser=%t bypass_rls=%t",
				role, canLogin, isSuperuser, bypassRLS,
			)
		}
	}
	if err := migrateToVersion(migrator, 59); err != nil {
		t.Fatalf("roll back workspace ownership migration: %v", err)
	}
	var legacyTenantID, legacyOwnerID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Legacy', 'legacy', 'starter', 'active') RETURNING id
	`).Scan(&legacyTenantID); err != nil {
		t.Fatalf("create legacy tenant before ownership migration: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, created_at)
		VALUES ($1, 'founder@legacy.test', ARRAY['project:read'], now() - interval '2 minutes')
		RETURNING id
	`, legacyTenantID).Scan(&legacyOwnerID); err != nil {
		t.Fatalf("create legacy founder before ownership migration: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, created_at)
		VALUES ($1, 'later@legacy.test', ARRAY['project:read'], now() - interval '1 minute')
	`, legacyTenantID); err != nil {
		t.Fatalf("create later legacy member before ownership migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply workspace ownership migration: %v", err)
	}
	var migratedOwnerID, migratedOwnerStatus string
	var migratedOwnerVersion, migratedOwnerPermissionCount int
	if err := root.QueryRow(ctx, `
		SELECT tenant.owner_member_id, member.status, member.authorization_version,
		       cardinality(member.permissions)
		FROM tenants AS tenant
		JOIN tenant_members AS member
		  ON member.tenant_id = tenant.id AND member.id = tenant.owner_member_id
		WHERE tenant.id = $1
	`, legacyTenantID).Scan(
		&migratedOwnerID, &migratedOwnerStatus, &migratedOwnerVersion, &migratedOwnerPermissionCount,
	); err != nil {
		t.Fatalf("read migrated legacy owner: %v", err)
	}
	if migratedOwnerID != legacyOwnerID || migratedOwnerStatus != "active" ||
		migratedOwnerVersion != 0 || migratedOwnerPermissionCount != 32 {
		t.Fatalf(
			"migrated owner = %q status=%q version=%d permissions=%d, want %q active/0/32",
			migratedOwnerID, migratedOwnerStatus, migratedOwnerVersion,
			migratedOwnerPermissionCount, legacyOwnerID,
		)
	}
	if _, err := root.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, legacyTenantID); err != nil {
		t.Fatalf("remove legacy ownership migration fixture: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply privacy jobs migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply reporter privacy migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply unverified privacy expiry migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply abuse diagnostics migration: %v", err)
	}
	var safetyTenantID, safetyProjectID, safetyFeedbackID, safeAttachmentID, expiredAttachmentID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Safety migration', 'safety-migration', 'growth', 'active')
		RETURNING id
	`).Scan(&safetyTenantID); err != nil {
		t.Fatalf("create attachment safety migration tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Safety', 'pk_live_SAFETYABCDEFGHIJKLMNOQ', '{example.test}')
		RETURNING id
	`, safetyTenantID).Scan(&safetyProjectID); err != nil {
		t.Fatalf("create attachment safety migration project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, public_id, client_feedback_id, title, page_url,
			text_process_status
		) VALUES (
			$1, $2, 'fb_Safety01', gen_random_uuid(), 'Safety migration',
			'https://example.test', 'skipped'
		) RETURNING id
	`, safetyTenantID, safetyProjectID).Scan(&safetyFeedbackID); err != nil {
		t.Fatalf("create attachment safety migration feedback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at
		) VALUES (
			$1, $2, 'screenshot', gen_random_uuid(), 'migration/safe.png',
			'image/png', 8, 'skipped', now() - interval '1 hour'
		) RETURNING id
	`, safetyTenantID, safetyFeedbackID).Scan(&safeAttachmentID); err != nil {
		t.Fatalf("create completed legacy attachment: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, complete_token_hash,
			upload_refreshed_at
		) VALUES (
			$1, $2, 'audio', gen_random_uuid(), 'migration/expired.webm',
			'audio/webm', 8, 'pending', 'legacy-token', now() - interval '1 hour'
		) RETURNING id
	`, safetyTenantID, safetyFeedbackID).Scan(&expiredAttachmentID); err != nil {
		t.Fatalf("create incomplete legacy attachment: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply attachment safety migration: %v", err)
	}
	var safeStatus, safeDetected string
	var safeCheckedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT safety_status, detected_content_type, safety_checked_at
		FROM attachments WHERE id = $1
	`, safeAttachmentID).Scan(&safeStatus, &safeDetected, &safeCheckedAt); err != nil {
		t.Fatalf("read migrated completed attachment: %v", err)
	}
	if safeStatus != "safe" || safeDetected != "image/png" || safeCheckedAt == nil {
		t.Fatalf("migrated completed attachment = %q/%q/%v", safeStatus, safeDetected, safeCheckedAt)
	}
	var expiredStatus, expiredReason string
	var expiredToken *string
	if err := root.QueryRow(ctx, `
		SELECT safety_status, safety_error_reason, complete_token_hash
		FROM attachments WHERE id = $1
	`, expiredAttachmentID).Scan(&expiredStatus, &expiredReason, &expiredToken); err != nil {
		t.Fatalf("read migrated expired attachment: %v", err)
	}
	if expiredStatus != "expired" || expiredReason != "upload_expired" || expiredToken != nil {
		t.Fatalf("migrated expired attachment = %q/%q/%v", expiredStatus, expiredReason, expiredToken)
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back attachment safety migration: %v", err)
	}
	var rollbackToken *string
	if err := root.QueryRow(ctx, `
		SELECT complete_token_hash FROM attachments WHERE id = $1
	`, expiredAttachmentID).Scan(&rollbackToken); err != nil {
		t.Fatalf("read rolled-back attachment: %v", err)
	}
	if rollbackToken == nil || *rollbackToken == "" {
		t.Fatal("rolled-back expired attachment completion token was not restored")
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply attachment safety migration after rollback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT safety_status, complete_token_hash
		FROM attachments WHERE id = $1
	`, expiredAttachmentID).Scan(&expiredStatus, &expiredToken); err != nil {
		t.Fatalf("read re-migrated expired attachment: %v", err)
	}
	if expiredStatus != "expired" || expiredToken != nil {
		t.Fatalf("re-migrated expired attachment = %q/%v", expiredStatus, expiredToken)
	}
	var replaySafetyTenantID, replaySafetyProjectID, replaySafetySessionID string
	var safeReplayBatchID, expiredReplayBatchID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Replay safety migration', 'replay-safety-migration', 'growth', 'active')
		RETURNING id
	`).Scan(&replaySafetyTenantID); err != nil {
		t.Fatalf("create replay safety migration tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Replay safety', 'pk_live_REPLAYSAFETYABCDEFGHIJ', '{example.test}')
		RETURNING id
	`, replaySafetyTenantID).Scan(&replaySafetyProjectID); err != nil {
		t.Fatalf("create replay safety migration project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, rrweb_version,
			max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), now() - interval '1 hour', '2.1.1', 1800, 50000)
		RETURNING id
	`, replaySafetyTenantID, replaySafetyProjectID).Scan(&replaySafetySessionID); err != nil {
		t.Fatalf("create replay safety migration session: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, completed_at, upload_refreshed_at
		) VALUES (
			$1, $2, $3, 0, 'migration/replay-safe.json',
			1, 32, now() - interval '30 minutes', now() - interval '31 minutes'
		) RETURNING id
	`, replaySafetyTenantID, replaySafetyProjectID, replaySafetySessionID).Scan(&safeReplayBatchID); err != nil {
		t.Fatalf("create completed legacy replay batch: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO session_batches (
			tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, complete_token_hash, upload_refreshed_at
		) VALUES (
			$1, $2, $3, 1, 'migration/replay-expired.json',
			1, 32, 'legacy-replay-token', now() - interval '1 hour'
		) RETURNING id
	`, replaySafetyTenantID, replaySafetyProjectID, replaySafetySessionID).Scan(&expiredReplayBatchID); err != nil {
		t.Fatalf("create incomplete legacy replay batch: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply replay safety migration: %v", err)
	}
	var safeReplayStatus string
	var safeReplayCheckedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT safety_status, safety_checked_at
		FROM session_batches WHERE id = $1
	`, safeReplayBatchID).Scan(&safeReplayStatus, &safeReplayCheckedAt); err != nil {
		t.Fatalf("read migrated completed replay batch: %v", err)
	}
	if safeReplayStatus != "safe" || safeReplayCheckedAt == nil {
		t.Fatalf("migrated completed replay batch = %q/%v", safeReplayStatus, safeReplayCheckedAt)
	}
	var expiredReplayStatus, expiredReplayReason string
	var expiredReplayToken *string
	if err := root.QueryRow(ctx, `
		SELECT safety_status, safety_error_reason, complete_token_hash
		FROM session_batches WHERE id = $1
	`, expiredReplayBatchID).Scan(
		&expiredReplayStatus, &expiredReplayReason, &expiredReplayToken,
	); err != nil {
		t.Fatalf("read migrated expired replay batch: %v", err)
	}
	if expiredReplayStatus != "expired" || expiredReplayReason != "upload_expired" ||
		expiredReplayToken != nil {
		t.Fatalf(
			"migrated expired replay batch = %q/%q/%v",
			expiredReplayStatus, expiredReplayReason, expiredReplayToken,
		)
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back replay safety migration: %v", err)
	}
	var rollbackReplayToken *string
	if err := root.QueryRow(ctx, `
		SELECT complete_token_hash FROM session_batches WHERE id = $1
	`, expiredReplayBatchID).Scan(&rollbackReplayToken); err != nil {
		t.Fatalf("read rolled-back replay batch: %v", err)
	}
	if rollbackReplayToken == nil || *rollbackReplayToken == "" {
		t.Fatal("rolled-back expired replay batch completion token was not restored")
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply replay safety migration after rollback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT safety_status, complete_token_hash
		FROM session_batches WHERE id = $1
	`, expiredReplayBatchID).Scan(&expiredReplayStatus, &expiredReplayToken); err != nil {
		t.Fatalf("read re-migrated expired replay batch: %v", err)
	}
	if expiredReplayStatus != "expired" || expiredReplayToken != nil {
		t.Fatalf("re-migrated expired replay batch = %q/%v", expiredReplayStatus, expiredReplayToken)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply idempotent AI budget migration: %v", err)
	}
	var aiBudgetTable *string
	if err := root.QueryRow(ctx, `
		SELECT to_regclass('public.ai_work_reservations')::text
	`).Scan(&aiBudgetTable); err != nil || aiBudgetTable == nil {
		t.Fatalf("read AI budget migration table = %v, error = %v", aiBudgetTable, err)
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back idempotent AI budget migration: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT to_regclass('public.ai_work_reservations')::text
	`).Scan(&aiBudgetTable); err != nil {
		t.Fatalf("read rolled-back AI budget table: %v", err)
	}
	if aiBudgetTable != nil {
		t.Fatalf("rolled-back AI budget table = %v, want nil", aiBudgetTable)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply idempotent AI budget migration: %v", err)
	}
	if version, dirty, err := migrator.Version(); err != nil || version != 67 || dirty {
		t.Fatalf("pre-forward migration state = version %d dirty %t, error %v", version, dirty, err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply cleanup lifecycle migration from original v67 schema: %v", err)
	}
	for resource, query := range map[string]string{
		"attachment":   `SELECT safety_status FROM attachments WHERE id = $1`,
		"replay batch": `SELECT safety_status FROM session_batches WHERE id = $1`,
	} {
		resourceID := expiredAttachmentID
		if resource == "replay batch" {
			resourceID = expiredReplayBatchID
		}
		var status string
		if err := root.QueryRow(ctx, query, resourceID).Scan(&status); err != nil {
			t.Fatalf("read migrated legacy %s: %v", resource, err)
		}
		if status != "cleaning" {
			t.Fatalf("migrated legacy %s status = %q, want cleaning", resource, status)
		}
	}
	var finalizeColumn, finalizeIndex *string
	if err := root.QueryRow(ctx, `
		SELECT to_regclass('public.idx_sessions_finalize_pending')::text
	`).Scan(&finalizeIndex); err != nil || finalizeIndex == nil {
		t.Fatalf("read replay finalization index = %v, error = %v", finalizeIndex, err)
	}
	if err := root.QueryRow(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'sessions'
		  AND column_name = 'finalize_requested_at'
	`).Scan(&finalizeColumn); err != nil || finalizeColumn == nil {
		t.Fatalf("read replay finalization column = %v, error = %v", finalizeColumn, err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE attachments
		SET safety_status = 'expired',
		    safety_checked_at = now(),
		    safety_error_reason = 'upload_expired'
		WHERE id = $1
	`, expiredAttachmentID); err != nil {
		t.Fatalf("complete migrated attachment cleanup: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE session_batches
		SET safety_status = 'expired',
		    safety_checked_at = now(),
		    safety_error_reason = 'upload_expired'
		WHERE id = $1
	`, expiredReplayBatchID); err != nil {
		t.Fatalf("complete migrated replay batch cleanup: %v", err)
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back cleanup lifecycle migration after cleanup: %v", err)
	}
	var cleaningEnumValues int
	if err := root.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_enum AS value
		JOIN pg_type AS kind ON kind.oid = value.enumtypid
		WHERE kind.typname IN ('attachment_safety_status', 'replay_batch_safety_status')
		  AND value.enumlabel = 'cleaning'
	`).Scan(&cleaningEnumValues); err != nil || cleaningEnumValues != 0 {
		t.Fatalf("cleaning enum values after rollback = %d, error = %v", cleaningEnumValues, err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply cleanup lifecycle migration: %v", err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, safetyTenantID); err != nil {
		t.Fatalf("remove attachment safety migration fixture: %v", err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, replaySafetyTenantID); err != nil {
		t.Fatalf("remove replay safety migration fixture: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply AI project cleanup index migration: %v", err)
	}
	var aiCleanupIndex *string
	if err := root.QueryRow(ctx, `
		SELECT to_regclass('public.idx_ai_project_daily_usage_cleanup')::text
	`).Scan(&aiCleanupIndex); err != nil || aiCleanupIndex == nil {
		t.Fatalf("read AI project cleanup index = %v, error = %v", aiCleanupIndex, err)
	}
	var profileMigrationTenantID, profileMigrationMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Profile migration', 'profile-migration', 'starter', 'active')
		RETURNING id
	`).Scan(&profileMigrationTenantID); err != nil {
		t.Fatalf("create organization profile migration tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name)
		VALUES ($1, 'owner@profile-migration.test', 'Profile Owner')
		RETURNING id
	`, profileMigrationTenantID).Scan(&profileMigrationMemberID); err != nil {
		t.Fatalf("create organization profile migration member: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply organization identity profile migration: %v", err)
	}
	var shortName, organizationDefaultLocale, defaultTimezone *string
	var logoVersion, avatarVersion *string
	if err := root.QueryRow(ctx, `
		SELECT short_name, default_locale, default_timezone, logo_version
		FROM tenants WHERE id = $1
	`, profileMigrationTenantID).Scan(
		&shortName, &organizationDefaultLocale, &defaultTimezone, &logoVersion,
	); err != nil {
		t.Fatalf("read migrated organization profile: %v", err)
	}
	if shortName != nil || organizationDefaultLocale != nil || defaultTimezone != nil || logoVersion != nil {
		t.Fatalf("migrated organization defaults = %v/%v/%v/%v, want nil", shortName,
			organizationDefaultLocale, defaultTimezone, logoVersion)
	}
	if err := root.QueryRow(ctx, `
		SELECT avatar_version FROM tenant_members WHERE id = $1
	`, profileMigrationMemberID).Scan(&avatarVersion); err != nil || avatarVersion != nil {
		t.Fatalf("migrated member avatar = %v, error = %v, want nil", avatarVersion, err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants
		SET short_name = '', default_locale = 'not_a_locale', default_timezone = 'bad timezone'
		WHERE id = $1
	`, profileMigrationTenantID); err == nil {
		t.Fatal("invalid organization profile constraints succeeded")
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO identity_asset_deletions (tenant_id, asset_kind, member_id, version)
		VALUES ($1, 'workspace_logo', $2, gen_random_uuid())
	`, profileMigrationTenantID, profileMigrationMemberID); err == nil {
		t.Fatal("workspace logo cleanup with member subject succeeded")
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO identity_asset_deletions (tenant_id, asset_kind, member_id, version)
		VALUES ($1, 'member_avatar', $2, gen_random_uuid())
	`, profileMigrationTenantID, profileMigrationMemberID); err != nil {
		t.Fatalf("create member avatar cleanup: %v", err)
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back organization identity profile migration: %v", err)
	}
	var legacyProfileTenantName, legacyProfileMemberName string
	if err := root.QueryRow(ctx, `
		SELECT tenant.name, member.name
		FROM tenants AS tenant
		JOIN tenant_members AS member ON member.tenant_id = tenant.id
		WHERE tenant.id = $1 AND member.id = $2
	`, profileMigrationTenantID, profileMigrationMemberID).Scan(
		&legacyProfileTenantName, &legacyProfileMemberName,
	); err != nil {
		t.Fatalf("read profile records after compatible rollback: %v", err)
	}
	if legacyProfileTenantName != "Profile migration" || legacyProfileMemberName != "Profile Owner" {
		t.Fatalf("profile records after rollback = %q/%q", legacyProfileTenantName, legacyProfileMemberName)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply organization identity profile migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply transactional identity cleanup migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply member regional preference migration: %v", err)
	}
	var memberLocale, memberTimezone *string
	if err := root.QueryRow(ctx, `
		SELECT locale, timezone
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2
	`, profileMigrationTenantID, profileMigrationMemberID).Scan(
		&memberLocale, &memberTimezone,
	); err != nil {
		t.Fatalf("read migrated member regional preferences: %v", err)
	}
	if memberLocale != nil || memberTimezone != nil {
		t.Fatalf("migrated member regional preferences = %v/%v, want nil", memberLocale, memberTimezone)
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenant_members
		SET locale = 'pt-AO', timezone = 'Africa/Luanda'
		WHERE tenant_id = $1 AND id = $2
	`, profileMigrationTenantID, profileMigrationMemberID); err != nil {
		t.Fatalf("write valid member regional preferences: %v", err)
	}
	for name, statement := range map[string]string{
		"unsupported locale":  `UPDATE tenant_members SET locale = 'fr-FR' WHERE id = $1`,
		"unnormalized locale": `UPDATE tenant_members SET locale = 'PT-ao' WHERE id = $1`,
		"unsafe timezone":     `UPDATE tenant_members SET timezone = 'bad timezone' WHERE id = $1`,
	} {
		if _, err := root.Exec(ctx, statement, profileMigrationMemberID); err == nil {
			t.Fatalf("%s regional preference succeeded", name)
		}
	}
	for _, column := range []string{"locale", "timezone"} {
		var canSelect, canUpdate bool
		if err := root.QueryRow(ctx, `
			SELECT has_column_privilege('fidibeki_app', 'tenant_members', $1, 'SELECT'),
			       has_column_privilege('fidibeki_app', 'tenant_members', $1, 'UPDATE')
		`, column).Scan(&canSelect, &canUpdate); err != nil {
			t.Fatalf("read app privilege for tenant_members.%s: %v", column, err)
		}
		if !canSelect || !canUpdate {
			t.Fatalf("app privileges for tenant_members.%s = select:%t update:%t", column, canSelect, canUpdate)
		}
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back member regional preference migration: %v", err)
	}
	var regionalColumnCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'tenant_members'
		  AND column_name IN ('locale', 'timezone')
	`).Scan(&regionalColumnCount); err != nil || regionalColumnCount != 0 {
		t.Fatalf("member regional columns after rollback = %d, error = %v", regionalColumnCount, err)
	}
	var preservedProfileMemberName string
	if err := root.QueryRow(ctx, `
		SELECT name FROM tenant_members WHERE tenant_id = $1 AND id = $2
	`, profileMigrationTenantID, profileMigrationMemberID).Scan(&preservedProfileMemberName); err != nil {
		t.Fatalf("read member after regional preference rollback: %v", err)
	}
	if preservedProfileMemberName != "Profile Owner" {
		t.Fatalf("member name after regional preference rollback = %q", preservedProfileMemberName)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply member regional preference migration: %v", err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, profileMigrationTenantID); err != nil {
		t.Fatalf("remove organization profile migration fixture: %v", err)
	}

	var tenantA, tenantB string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active')
		RETURNING id
	`).Scan(&tenantA); err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Beta', 'beta', 'starter', 'active')
		RETURNING id
	`).Scan(&tenantB); err != nil {
		t.Fatalf("create tenant B: %v", err)
	}
	for tenantID, email := range map[string]string{
		tenantA: "owner@acme.test",
		tenantB: "owner@beta.test",
	} {
		if _, err := root.Exec(ctx,
			`INSERT INTO tenant_members (tenant_id, email) VALUES ($1, $2)`,
			tenantID, email,
		); err != nil {
			t.Fatalf("create member %s: %v", email, err)
		}
	}
	for tenantID, domain := range map[string]string{
		tenantA: "acme.test",
		tenantB: "beta.test",
	} {
		if _, err := root.Exec(ctx, `
			INSERT INTO tenant_member_domains (
				tenant_id, domain, verification_email, verification_token_hash,
				verification_expires_at, verified_at
			) VALUES ($1::uuid, $2::text, 'security@' || $2::text, encode(sha256(($1::text || $2::text)::bytea), 'hex'), now() + interval '10 minutes', now())
		`, tenantID, domain); err != nil {
			t.Fatalf("create tenant member domain %s: %v", domain, err)
		}
	}
	var memberA, memberB string
	if err := root.QueryRow(ctx,
		`SELECT id FROM tenant_members WHERE tenant_id = $1 AND email = 'owner@acme.test'`, tenantA,
	).Scan(&memberA); err != nil {
		t.Fatalf("read tenant A member: %v", err)
	}
	if err := root.QueryRow(ctx,
		`SELECT id FROM tenant_members WHERE tenant_id = $1 AND email = 'owner@beta.test'`, tenantB,
	).Scan(&memberB); err != nil {
		t.Fatalf("read tenant B member: %v", err)
	}
	for tenantID, memberID := range map[string]string{tenantA: memberA, tenantB: memberB} {
		if _, err := root.Exec(ctx, `
			INSERT INTO privacy_jobs (tenant_id, kind, status, actor_member_id)
			VALUES ($1, 'workspace_export', 'pending', $2)
		`, tenantID, memberID); err != nil {
			t.Fatalf("create privacy job for tenant %s: %v", tenantID, err)
		}
	}
	requestTokens := map[string]string{tenantA: "a", tenantB: "b"}
	for tenantID, memberID := range map[string]string{tenantA: memberA, tenantB: memberB} {
		if _, err := root.Exec(ctx, `
			INSERT INTO reporter_privacy_requests (
				tenant_id, action, status, reporter_email, email_digest, token_hash,
				actor_member_id, verification_expires_at
			) VALUES (
				$1, 'export', 'pending_verification', 'reporter@example.test',
				decode(repeat('ab', 32), 'hex'), decode(repeat($3::text, 64), 'hex'),
				$2, now() + interval '48 hours'
			)
		`, tenantID, memberID, requestTokens[tenantID]); err != nil {
			t.Fatalf("create reporter privacy request for tenant %s: %v", tenantID, err)
		}
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO privacy_jobs (tenant_id, kind, status)
		VALUES ($1, 'reporter_export', 'pending')
	`, tenantA); err == nil {
		t.Fatal("reporter export job without request succeeded")
	}
	var assigneeA, assigneeB string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name)
		VALUES ($1, 'assignee@acme.test', 'Acme Assignee') RETURNING id
	`, tenantA).Scan(&assigneeA); err != nil {
		t.Fatalf("create tenant A assignee: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name)
		VALUES ($1, 'assignee@beta.test', 'Beta Assignee') RETURNING id
	`, tenantB).Scan(&assigneeB); err != nil {
		t.Fatalf("create tenant B assignee: %v", err)
	}
	var projectA, projectB, widgetRuleA, widgetRuleB, issueA, issueB, feedbackA, feedbackB, attachmentA, attachmentB string
	var sessionA, sessionB string
	var integrationA, integrationB string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'A web', 'pk_live_AAAAAAAAAAAAAAAAAAAAAA', '{a.example.com}') RETURNING id
	`, tenantA).Scan(&projectA); err != nil {
		t.Fatalf("create tenant A project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'B web', 'pk_live_BBBBBBBBBBBBBBBBBBBBBB', '{b.example.com}') RETURNING id
	`, tenantB).Scan(&projectB); err != nil {
		t.Fatalf("create tenant B project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds
		) VALUES ($1, 'Replay limits', 'all_pages', 'delay', 0)
		RETURNING id
	`, projectA).Scan(&widgetRuleA); err != nil {
		t.Fatalf("create tenant A widget rule: %v", err)
	}
	var recurrenceMode string
	var recurrenceDays *int
	if err := root.QueryRow(ctx, `
		SELECT recurrence_mode, recurrence_days FROM widget_rules WHERE id = $1
	`, widgetRuleA).Scan(&recurrenceMode, &recurrenceDays); err != nil {
		t.Fatalf("read widget recurrence defaults: %v", err)
	}
	if recurrenceMode != "cooldown" || recurrenceDays == nil || *recurrenceDays != 30 {
		t.Fatalf("widget recurrence defaults = %q/%v, want cooldown/30", recurrenceMode, recurrenceDays)
	}
	var definitionKey, definitionStatus, presentation, launcherVariant, objective, defaultLocale string
	var definitionVersion, responseBlockCount, localizationCount int
	if err := root.QueryRow(ctx, `
		SELECT rule.definition_key, rule.definition_version, rule.definition_status,
		       rule.presentation, rule.objective, jsonb_array_length(rule.response_blocks),
		       (SELECT count(*)::integer FROM jsonb_object_keys(rule.localizations)),
		       project.default_locale
		FROM widget_rules AS rule
		JOIN projects AS project ON project.id = rule.project_id
		WHERE rule.id = $1
	`, widgetRuleA).Scan(
		&definitionKey, &definitionVersion, &definitionStatus, &presentation, &objective,
		&responseBlockCount, &localizationCount, &defaultLocale,
	); err != nil {
		t.Fatalf("read widget definition defaults: %v", err)
	}
	if definitionKey == "" || definitionVersion != 1 || definitionStatus != "active" ||
		presentation != "launcher" || objective != "continuous_feedback" ||
		responseBlockCount != 2 || localizationCount != 1 || defaultLocale != "en" {
		t.Fatalf(
			"widget definition defaults = %q/%d/%q/%q/%q blocks=%d locales=%d default=%q",
			definitionKey, definitionVersion, definitionStatus, presentation, objective,
			responseBlockCount, localizationCount, defaultLocale,
		)
	}
	for name, statement := range map[string]string{
		"cooldown below minimum": `UPDATE widget_rules SET recurrence_days = 0 WHERE id = $1`,
		"cooldown above maximum": `UPDATE widget_rules SET recurrence_days = 3651 WHERE id = $1`,
		"session with days":      `UPDATE widget_rules SET recurrence_mode = 'session', recurrence_days = 30 WHERE id = $1`,
		"never with days":        `UPDATE widget_rules SET recurrence_mode = 'never', recurrence_days = 30 WHERE id = $1`,
	} {
		if _, err := root.Exec(ctx, statement, widgetRuleA); err == nil {
			t.Fatalf("invalid widget recurrence %s succeeded", name)
		}
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET recurrence_mode = 'session', recurrence_days = NULL WHERE id = $1
	`, widgetRuleA); err != nil {
		t.Fatalf("set session widget recurrence: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET recurrence_mode = 'cooldown', recurrence_days = 3650 WHERE id = $1
	`, widgetRuleA); err != nil {
		t.Fatalf("set maximum widget recurrence cooldown: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET replay_max_duration_seconds = 1801 WHERE id = $1
	`, widgetRuleA); err == nil {
		t.Fatal("widget replay duration above 1800 seconds succeeded")
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET replay_max_events = 50001 WHERE id = $1
	`, widgetRuleA); err == nil {
		t.Fatal("widget replay event count above 50000 succeeded")
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds
		) VALUES ($1, 'Beta targeting', 'all_pages', 'delay', 0)
		RETURNING id
	`, projectB).Scan(&widgetRuleB); err != nil {
		t.Fatalf("create tenant B widget rule: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO widget_rule_target_conditions (
			widget_rule_id, kind, matcher_type, matcher_value, position
		) VALUES
			($1, 'include', 'all_pages', NULL, 0),
			($2, 'include', 'all_pages', NULL, 0)
	`, widgetRuleA, widgetRuleB); err != nil {
		t.Fatalf("seed widget target conditions: %v", err)
	}
	for name, statement := range map[string]string{
		"all pages with value": `
			INSERT INTO widget_rule_target_conditions (
				widget_rule_id, kind, matcher_type, matcher_value, position
			) VALUES ($1, 'exclude', 'all_pages', 'unexpected', 0)`,
		"matcher without value": `
			INSERT INTO widget_rule_target_conditions (
				widget_rule_id, kind, matcher_type, matcher_value, position
			) VALUES ($1, 'exclude', 'url_exact', NULL, 0)`,
		"negative position": `
			INSERT INTO widget_rule_target_conditions (
				widget_rule_id, kind, matcher_type, matcher_value, position
			) VALUES ($1, 'exclude', 'url_exact', 'https://a.example.com', -1)`,
		"duplicate position": `
			INSERT INTO widget_rule_target_conditions (
				widget_rule_id, kind, matcher_type, matcher_value, position
			) VALUES ($1, 'include', 'all_pages', NULL, 0)`,
	} {
		if _, err := root.Exec(ctx, statement, widgetRuleA); err == nil {
			t.Fatalf("invalid widget target condition %s succeeded", name)
		}
	}
	var disposableRule string
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds
		) VALUES ($1, 'Disposable targeting', 'all_pages', 'delay', 0)
		RETURNING id
	`, projectA).Scan(&disposableRule); err != nil {
		t.Fatalf("create disposable widget rule: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO widget_rule_target_conditions (
			widget_rule_id, kind, matcher_type, matcher_value, position
		) VALUES ($1, 'include', 'all_pages', NULL, 0)
	`, disposableRule); err != nil {
		t.Fatalf("create disposable widget target condition: %v", err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM widget_rules WHERE id = $1`, disposableRule); err != nil {
		t.Fatalf("delete disposable widget rule: %v", err)
	}
	var disposableConditions int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM widget_rule_target_conditions WHERE widget_rule_id = $1
	`, disposableRule).Scan(&disposableConditions); err != nil || disposableConditions != 0 {
		t.Fatalf("conditions after widget rule deletion = %d, error = %v, want 0", disposableConditions, err)
	}
	for _, event := range []struct {
		eventID, tenantID, projectID, ruleID, ruleName, pageURL string
	}{
		{"00000000-0000-4000-8000-000000000101", tenantA, projectA, widgetRuleA, "Replay limits", "https://a.example.com/chart"},
		{"00000000-0000-4000-8000-000000000102", tenantB, projectB, widgetRuleB, "Beta targeting", "https://b.example.com/chart"},
	} {
		if _, err := root.Exec(ctx, `
			INSERT INTO widget_funnel_events (
				event_id, tenant_id, project_id, rule_id, rule_name, event_type,
				visitor_id, page_view_id, page_url, page_key, occurred_at
			) VALUES (
				$1, $2, $3, $4, $5, 'matched', gen_random_uuid(), gen_random_uuid(),
				$6, decode(repeat('ab', 32), 'hex'), now()
			)
		`, event.eventID, event.tenantID, event.projectID, event.ruleID, event.ruleName, event.pageURL); err != nil {
			t.Fatalf("create %s widget funnel event: %v", event.ruleName, err)
		}
		if _, err := root.Exec(ctx, `
			INSERT INTO widget_funnel_daily (
				tenant_id, project_id, rule_id, rule_name, page_url, page_key,
				event_date, matched_count
			) VALUES ($1, $2, $3, $4, $5, decode(repeat('ab', 32), 'hex'), CURRENT_DATE, 1)
		`, event.tenantID, event.projectID, event.ruleID, event.ruleName, event.pageURL); err != nil {
			t.Fatalf("create %s widget funnel aggregate: %v", event.ruleName, err)
		}
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO widget_funnel_events (
			event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			visitor_id, page_view_id, page_url, page_key, occurred_at
		)
		SELECT gen_random_uuid(), tenant_id, project_id, rule_id, rule_name, event_type,
		       visitor_id, page_view_id, page_url, page_key, occurred_at
		FROM widget_funnel_events WHERE event_id = '00000000-0000-4000-8000-000000000101'
	`); err == nil {
		t.Fatal("duplicate widget funnel lifecycle event succeeded")
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO widget_funnel_events (
			event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			visitor_id, page_view_id, page_url, page_key, occurred_at
		) VALUES (
			gen_random_uuid(), $1, $2, $3, 'Mixed tenant', 'matched',
			gen_random_uuid(), gen_random_uuid(), 'https://b.example.com',
			decode(repeat('ab', 32), 'hex'), now()
		)
	`, tenantA, projectB, widgetRuleB); err == nil {
		t.Fatal("mixed-tenant widget funnel event succeeded")
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title)
		VALUES ($1, $2, 'ACME-2026-0001', 'Tenant A issue') RETURNING id
	`, tenantA, projectA).Scan(&issueA); err != nil {
		t.Fatalf("create tenant A issue: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title)
		VALUES ($1, $2, 'BETA-2026-0001', 'Tenant B issue') RETURNING id
	`, tenantB, projectB).Scan(&issueB); err != nil {
		t.Fatalf("create tenant B issue: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, issue_id, public_id, client_feedback_id, page_url, text_process_status
		) VALUES ($1, $2, $3, 'fb_RlsA0001', gen_random_uuid(), 'https://a.example.com', 'skipped')
		RETURNING id
	`, tenantA, projectA, issueA).Scan(&feedbackA); err != nil {
		t.Fatalf("create tenant A feedback: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, issue_id, public_id, client_feedback_id, page_url, text_process_status
		) VALUES ($1, $2, $3, 'fb_RlsB0001', gen_random_uuid(), 'https://b.example.com', 'skipped')
		RETURNING id
	`, tenantB, projectB, issueB).Scan(&feedbackB); err != nil {
		t.Fatalf("create tenant B feedback: %v", err)
	}
	if _, err := root.Exec(ctx,
		`UPDATE feedbacks SET priority = 'urgent', assignee_id = $2 WHERE id = $1`,
		feedbackA, assigneeA,
	); err != nil {
		t.Fatalf("assign tenant A feedback triage metadata: %v", err)
	}
	if _, err := root.Exec(ctx,
		`UPDATE issues SET priority = 'high', assignee_id = $2 WHERE id = $1`,
		issueA, assigneeA,
	); err != nil {
		t.Fatalf("assign tenant A issue triage metadata: %v", err)
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET priority = 'invalid' WHERE id = $1`, feedbackA); err == nil {
		t.Fatal("invalid feedback priority succeeded")
	}
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET assignee_id = $2 WHERE id = $1`, feedbackA, assigneeB); err == nil {
		t.Fatal("cross-tenant feedback assignment succeeded")
	}
	var tagA, tagB string
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color) VALUES ($1, 'Billing', '#167D68') RETURNING id
	`, tenantA).Scan(&tagA); err != nil {
		t.Fatalf("create tenant A tag: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color) VALUES ($1, 'Billing', '#3B82F6') RETURNING id
	`, tenantB).Scan(&tagB); err != nil {
		t.Fatalf("create tenant B tag: %v", err)
	}
	if _, err := root.Exec(ctx,
		`INSERT INTO feedback_tags (tenant_id, feedback_id, tag_id) VALUES ($1, $2, $3)`,
		tenantA, feedbackA, tagA,
	); err != nil {
		t.Fatalf("tag tenant A feedback: %v", err)
	}
	if _, err := root.Exec(ctx,
		`INSERT INTO issue_tags (tenant_id, issue_id, tag_id) VALUES ($1, $2, $3)`,
		tenantA, issueA, tagA,
	); err != nil {
		t.Fatalf("tag tenant A issue: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO feedback_tags (tenant_id, feedback_id, tag_id) VALUES ($1, $2, $3)
	`, tenantA, feedbackA, tagB); err == nil {
		t.Fatal("cross-tenant feedback tag succeeded")
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, feedback_id, session_id, page_url, started_at,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, $3, gen_random_uuid(), 'https://a.example.com', now(), '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantA, projectA, feedbackA).Scan(&sessionA); err != nil {
		t.Fatalf("create tenant A replay session: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, feedback_id, session_id, page_url, started_at,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, $3, gen_random_uuid(), 'https://b.example.com', now(), '2.1.1', 1800, 50000)
		RETURNING id
	`, tenantB, projectB, feedbackB).Scan(&sessionB); err != nil {
		t.Fatalf("create tenant B replay session: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks SET session_id = CASE id WHEN $1::uuid THEN $2::uuid ELSE $3::uuid END
		WHERE id IN ($1, $4)
	`, feedbackA, sessionA, sessionB, feedbackB); err != nil {
		t.Fatalf("link feedback replay sessions: %v", err)
	}
	for tenantID, values := range map[string][2]string{
		tenantA: {projectA, sessionA}, tenantB: {projectB, sessionB},
	} {
		if _, err := root.Exec(ctx, `
			INSERT INTO session_batches (
				tenant_id, project_id, session_id, batch_index, storage_key,
				events_count, size_bytes, complete_token_hash
			) VALUES ($1, $2, $3::uuid, 0, 'rls/' || ($3::uuid)::text || '.json', 1, 2, 'token')
		`, tenantID, values[0], values[1]); err != nil {
			t.Fatalf("create replay batch for tenant %s: %v", tenantID, err)
		}
	}
	for tenantID, destination := range map[string]*string{tenantA: &integrationA, tenantB: &integrationB} {
		if err := root.QueryRow(ctx, `
			INSERT INTO integrations (tenant_id, kind, name, webhook_url, events)
			VALUES ($1, 'slack', 'Alerts', 'https://hooks.example.test/fidibeki', '{feedback.created}')
			RETURNING id
		`, tenantID).Scan(destination); err != nil {
			t.Fatalf("create integration for tenant %s: %v", tenantID, err)
		}
	}
	for tenantID, issueID := range map[string]string{tenantA: issueA, tenantB: issueB} {
		var updateID, outboxID string
		if err := root.QueryRow(ctx, `
			INSERT INTO issue_updates (tenant_id, issue_id, member_id, old_status, new_status)
			SELECT $1, $2, id, 'open', 'in_progress'
			FROM tenant_members WHERE tenant_id = $1 AND id = $3
			RETURNING id
		`, tenantID, issueID, map[string]string{tenantA: memberA, tenantB: memberB}[tenantID]).Scan(&updateID); err != nil {
			t.Fatalf("create issue update for tenant %s: %v", tenantID, err)
		}
		if err := root.QueryRow(ctx, `
			INSERT INTO outbox_events (tenant_id, event, resource_type, resource_id, event_key)
			VALUES ($1, 'issue.updated', 'issue', $2, $3) RETURNING id
		`, tenantID, issueID, updateID).Scan(&outboxID); err != nil {
			t.Fatalf("create issue outbox event for tenant %s: %v", tenantID, err)
		}
		if _, err := root.Exec(ctx, `
			INSERT INTO delivery_logs (
				tenant_id, outbox_event_id, event, resource_type, resource_id, attempt_number
			) VALUES ($1, $2, 'issue.updated', 'issue', $3, 1)
		`, tenantID, outboxID, issueID); err != nil {
			t.Fatalf("create delivery log for tenant %s: %v", tenantID, err)
		}
		integrationID := integrationA
		if tenantID == tenantB {
			integrationID = integrationB
		}
		if _, err := root.Exec(ctx, `
			INSERT INTO delivery_logs (
				tenant_id, outbox_event_id, integration_id, delivery_key, event,
				resource_type, resource_id, attempt_number
			) VALUES ($1, $2, $3::uuid, 'integration:' || $3::uuid::text, 'issue.updated', 'issue', $4, 1)
		`, tenantID, outboxID, integrationID, issueID); err != nil {
			t.Fatalf("create integration delivery log for tenant %s: %v", tenantID, err)
		}
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at,
			safety_status, safety_checked_at, detected_content_type
		) VALUES ($1, $2, 'screenshot', gen_random_uuid(), 'rls/a.png',
		          'image/png', 1, 'skipped', now(), 'safe', now(), 'image/png') RETURNING id
	`, tenantA, feedbackA).Scan(&attachmentA); err != nil {
		t.Fatalf("create tenant A attachment: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at,
			safety_status, safety_checked_at, detected_content_type
		) VALUES ($1, $2, 'screenshot', gen_random_uuid(), 'rls/b.png',
		          'image/png', 1, 'skipped', now(), 'safe', now(), 'image/png') RETURNING id
	`, tenantB, feedbackB).Scan(&attachmentB); err != nil {
		t.Fatalf("create tenant B attachment: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO ai_daily_usage (tenant_id, usage_date, requests)
		VALUES ($1, CURRENT_DATE, 1), ($2, CURRENT_DATE, 1)
	`, tenantA, tenantB); err != nil {
		t.Fatalf("seed AI daily usage: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO ai_project_daily_usage (
			tenant_id, project_id, usage_date, requests
		) VALUES
			($1, $2, CURRENT_DATE, 1),
			($3, $4, CURRENT_DATE, 1)
	`, tenantA, projectA, tenantB, projectB); err != nil {
		t.Fatalf("seed project AI daily usage: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO ai_work_reservations (
			tenant_id, project_id, work_kind, resource_id, generation, usage_date
		) VALUES
			($1, $2, 'text', $3, 1, CURRENT_DATE),
			($4, $5, 'text', $6, 1, CURRENT_DATE)
	`, tenantA, projectA, feedbackA, tenantB, projectB, feedbackB); err != nil {
		t.Fatalf("seed AI work reservations: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO ai_gemini_files (tenant_id, attachment_id, file_name, delete_after)
		VALUES ($1, $2, 'files/rls-a', now()), ($3, $4, 'files/rls-b', now())
	`, tenantA, attachmentA, tenantB, attachmentB); err != nil {
		t.Fatalf("seed Gemini file ledger: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO abuse_diagnostics_hourly (
			tenant_id, project_id, bucket_start, category, reason, event_count,
			first_occurred_at, last_occurred_at
		) VALUES
			($1, $2, date_trunc('hour', now()), 'rate_limit', 'submission_rate_limited', 2, now(), now()),
			($3, $4, date_trunc('hour', now()), 'spam', 'spam_rejected', 1, now(), now())
	`, tenantA, projectA, tenantB, projectB); err != nil {
		t.Fatalf("seed abuse diagnostics: %v", err)
	}

	pools, err := db.OpenPools(
		ctx,
		roleURL(t, directURL, "fidibeki_app", appPassword),
		roleURL(t, directURL, "fidibeki_admin", adminPassword),
		3,
		2,
	)
	if err != nil {
		t.Fatalf("open application pools: %v", err)
	}
	appPool := pools.App
	adminPool := pools.Admin
	if err := (db.PoolsCheck{Pools: pools}).Check(ctx); err != nil {
		t.Fatalf("check both database pools: %v", err)
	}
	if err := (db.PoolCheck{Pool: appPool}).Check(ctx); err != nil {
		t.Fatalf("check app database pool: %v", err)
	}
	begin := db.BeginFromPool(appPool)
	probeTx, err := begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction through pool adapter: %v", err)
	}
	if err := probeTx.Rollback(ctx); err != nil {
		t.Fatalf("roll back pool adapter transaction: %v", err)
	}
	resolver := tenant.PostgresResolver{Admin: adminPool}
	resolved, err := resolver.Resolve(ctx, "acme")
	if err != nil {
		t.Fatalf("resolve tenant: %v", err)
	}
	if resolved.ID != tenantA || resolved.Plan != tenant.PlanGrowth || resolved.Status != tenant.StatusActive {
		t.Fatalf("resolved tenant = %#v, want Acme growth/active", resolved)
	}
	if _, err := resolver.Resolve(ctx, "missing"); !errors.Is(err, tenant.ErrUnknownTenant) {
		t.Fatalf("resolve missing tenant error = %v, want ErrUnknownTenant", err)
	}

	var count int
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM tenant_members`).Scan(&count); err != nil {
		t.Fatalf("query without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("app role without tenant context read %d rows, want 0", count)
	}
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM tenant_member_domains`).Scan(&count); err != nil {
		t.Fatalf("query member domains without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("app role without tenant context read %d member domains, want 0", count)
	}
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM tags`).Scan(&count); err != nil {
		t.Fatalf("query tags without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("app role without tenant context read %d tags, want 0", count)
	}
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM widget_rule_target_conditions`).Scan(&count); err != nil {
		t.Fatalf("query widget target conditions without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("app role without tenant context read %d widget target conditions, want 0", count)
	}
	for _, table := range []string{"widget_funnel_events", "widget_funnel_daily"} {
		if err := appPool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("query %s without tenant context: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("app role without tenant context read %d %s rows, want 0", count, table)
		}
	}
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM privacy_jobs`).Scan(&count); err != nil {
		t.Fatalf("query privacy jobs without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("app role without tenant context read %d privacy jobs, want 0", count)
	}
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM reporter_privacy_requests`).Scan(&count); err != nil {
		t.Fatalf("query reporter privacy requests without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("app role without tenant context read %d reporter privacy requests, want 0", count)
	}
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM abuse_diagnostics_hourly`).Scan(&count); err != nil {
		t.Fatalf("query abuse diagnostics without tenant context: %v", err)
	}
	if count != 0 {
		t.Fatalf("app role without tenant context read %d abuse diagnostics, want 0", count)
	}
	for _, table := range []string{"ai_project_daily_usage", "ai_work_reservations"} {
		if err := appPool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("query %s without tenant context: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("app role without tenant context read %d %s rows, want 0", count, table)
		}
	}

	tx, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tenant A transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	setTenantContext(t, ctx, tx, tenantA)
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenant_members`).Scan(&count); err != nil {
		t.Fatalf("query tenant A members: %v", err)
	}
	if count != 2 {
		t.Fatalf("tenant A member count = %d, want 2", count)
	}
	var appMemberLocale, appMemberTimezone string
	if err := tx.QueryRow(ctx, `
		UPDATE tenant_members
		SET locale = 'pt-PT', timezone = 'Europe/Lisbon'
		WHERE tenant_id = $1 AND id = $2
		RETURNING locale, timezone
	`, tenantA, memberA).Scan(&appMemberLocale, &appMemberTimezone); err != nil {
		t.Fatalf("update tenant A member regional preferences: %v", err)
	}
	if appMemberLocale != "pt-PT" || appMemberTimezone != "Europe/Lisbon" {
		t.Fatalf("tenant A member regional preferences = %q/%q", appMemberLocale, appMemberTimezone)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenant_member_domains`).Scan(&count); err != nil {
		t.Fatalf("query tenant A member domains: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant A member domain count = %d, want 1", count)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_daily_usage`).Scan(&count); err != nil {
		t.Fatalf("query tenant A AI usage: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant A AI usage count = %d, want 1", count)
	}
	for _, table := range []string{"ai_project_daily_usage", "ai_work_reservations"} {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil ||
			count != 1 {
			t.Fatalf("tenant A %s count = %d, error = %v, want 1", table, count, err)
		}
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_gemini_files`).Scan(&count); err != nil {
		t.Fatalf("query tenant A Gemini files: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant A Gemini file count = %d, want 1", count)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM abuse_diagnostics_hourly`).Scan(&count); err != nil ||
		count != 1 {
		t.Fatalf("tenant A abuse diagnostic count = %d, error = %v, want 1", count, err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM issue_updates`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A issue update count = %d, error = %v, want 1", count, err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_logs`).Scan(&count); err != nil {
		t.Fatalf("tenant A delivery log count = %d, error = %v, want 2", count, err)
	}
	if count != 2 {
		t.Fatalf("tenant A delivery log count = %d, want 2", count)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM integrations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A integration count = %d, error = %v, want 1", count, err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A replay session count = %d, error = %v, want 1", count, err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM session_batches`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A replay batch count = %d, error = %v, want 1", count, err)
	}
	for table, want := range map[string]int{"tags": 1, "feedback_tags": 1, "issue_tags": 1} {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != want {
			t.Fatalf("tenant A %s count = %d, error = %v, want %d", table, count, err, want)
		}
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM widget_rule_target_conditions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A widget target condition count = %d, error = %v, want 1", count, err)
	}
	for _, table := range []string{"widget_funnel_events", "widget_funnel_daily"} {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("tenant A %s count = %d, error = %v, want 1", table, count, err)
		}
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM privacy_jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A privacy job count = %d, error = %v, want 1", count, err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM reporter_privacy_requests`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A reporter privacy request count = %d, error = %v, want 1", count, err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM tenant_members WHERE tenant_id = $1`, tenantB,
	).Scan(&count); err != nil {
		t.Fatalf("query tenant B through tenant A transaction: %v", err)
	}
	if count != 0 {
		t.Fatalf("tenant A transaction read %d tenant B rows, want 0", count)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO identity_asset_deletions (tenant_id, asset_kind, version)
		VALUES ($1, 'workspace_logo', gen_random_uuid())
	`, tenantA); err != nil {
		t.Fatalf("insert tenant A identity cleanup: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE identity_asset_deletions
		SET next_attempt_at = now()
		WHERE tenant_id = $1
	`, tenantA); err != nil {
		t.Fatalf("update tenant A identity cleanup: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM identity_asset_deletions WHERE tenant_id = $1
	`, tenantA).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tenant A identity cleanup count = %d, error = %v, want 1", count, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tenant A read transaction: %v", err)
	}

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant write transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx,
		`INSERT INTO tenant_members (tenant_id, email) VALUES ($1, 'intruder@beta.test')`, tenantB,
	); err == nil {
		t.Fatal("cross-tenant insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant member preference transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	tag, err := tx.Exec(ctx, `
		UPDATE tenant_members
		SET locale = 'en', timezone = 'UTC'
		WHERE tenant_id = $1 AND id = $2
	`, tenantB, memberB)
	if err != nil {
		t.Fatalf("cross-tenant member preference update returned error: %v", err)
	}
	if tag.RowsAffected() != 0 {
		t.Fatalf("cross-tenant member preference update affected %d rows", tag.RowsAffected())
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant identity cleanup transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO identity_asset_deletions (tenant_id, asset_kind, version)
		VALUES ($1, 'workspace_logo', gen_random_uuid())
	`, tenantB); err == nil {
		t.Fatal("cross-tenant identity cleanup insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant reporter privacy transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO reporter_privacy_requests (
			tenant_id, action, status, reporter_email, email_digest, token_hash,
			actor_member_id, verification_expires_at
		) VALUES (
			$1, 'anonymize', 'pending_verification', 'other@example.test',
			decode(repeat('cd', 32), 'hex'), decode(repeat('c', 64), 'hex'),
			$2, now() + interval '48 hours'
		)
	`, tenantB, memberB); err == nil {
		t.Fatal("cross-tenant reporter privacy request insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant privacy job transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO privacy_jobs (tenant_id, kind, status, actor_member_id)
		VALUES ($1, 'reporter_export', 'pending', $2)
	`, tenantB, memberB); err == nil {
		t.Fatal("cross-tenant privacy job insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant widget target condition transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO widget_rule_target_conditions (
			widget_rule_id, kind, matcher_type, matcher_value, position
		) VALUES ($1, 'exclude', 'url_exact', 'https://b.example.com/private', 0)
	`, widgetRuleB); err == nil {
		t.Fatal("cross-tenant widget target condition insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant widget funnel transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO widget_funnel_events (
			event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			visitor_id, page_view_id, page_url, page_key, occurred_at
		) VALUES (
			gen_random_uuid(), $1, $2, $3, 'Cross tenant', 'matched',
			gen_random_uuid(), gen_random_uuid(), 'https://b.example.com/chart',
			decode(repeat('ab', 32), 'hex'), now()
		)
	`, tenantB, projectB, widgetRuleB); err == nil {
		t.Fatal("cross-tenant widget funnel event insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mixed-tenant integration delivery write: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO delivery_logs (
			tenant_id, outbox_event_id, integration_id, delivery_key, event,
			resource_type, resource_id, attempt_number
		)
		SELECT $1, id, $2::uuid, 'integration:' || $2::uuid::text, event, resource_type, resource_id, 2
		FROM outbox_events WHERE tenant_id = $1 LIMIT 1
	`, tenantA, integrationB); err == nil {
		t.Fatal("mixed-tenant integration delivery log succeeded, want composite FK rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant AI usage write: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO ai_daily_usage (tenant_id, usage_date, requests)
		VALUES ($1, CURRENT_DATE + 1, 1)
	`, tenantB); err == nil {
		t.Fatal("cross-tenant AI usage insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant project AI budget write: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO ai_project_daily_usage (
			tenant_id, project_id, usage_date, requests
		) VALUES ($1, $2, CURRENT_DATE + 1, 1)
	`, tenantB, projectB); err == nil {
		t.Fatal("cross-tenant project AI usage insert succeeded, want RLS rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mixed-tenant AI reservation write: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO ai_work_reservations (
			tenant_id, project_id, work_kind, resource_id, generation, usage_date
		) VALUES ($1, $2, 'text', gen_random_uuid(), 2, CURRENT_DATE)
	`, tenantA, projectB); err == nil {
		t.Fatal("mixed-tenant AI reservation insert succeeded, want composite FK rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mixed-tenant Gemini file write: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO ai_gemini_files (tenant_id, attachment_id, file_name, delete_after)
		VALUES ($1, $2, 'files/mixed-tenant', now())
	`, tenantA, attachmentB); err == nil {
		t.Fatal("mixed-tenant Gemini file insert succeeded, want composite FK rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mixed-tenant feedback tag write: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO feedback_tags (tenant_id, feedback_id, tag_id) VALUES ($1, $2, $3)
	`, tenantA, feedbackA, tagB); err == nil {
		t.Fatal("mixed-tenant feedback tag succeeded, want composite FK rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mixed-tenant replay write: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, started_at, rrweb_version,
			max_duration_seconds, max_events
		) VALUES ($1, $2, gen_random_uuid(), now(), '2.1.1', 1800, 50000)
	`, tenantA, projectB); err == nil {
		t.Fatal("mixed-tenant replay session insert succeeded, want composite FK rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tenant update transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET name = 'Acme Updated' WHERE id = $1`, tenantA); err != nil {
		t.Fatalf("update permitted tenant column: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit permitted tenant update: %v", err)
	}

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin forbidden tenant update transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET plan = 'enterprise' WHERE id = $1`, tenantA); err == nil {
		t.Fatal("app role updated billing plan, want column privilege rejection")
	}
	_ = tx.Rollback(ctx)

	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin forbidden deletion update transaction: %v", err)
	}
	setTenantContext(t, ctx, tx, tenantA)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET deletion_requested_at = now() WHERE id = $1`, tenantA); err == nil {
		t.Fatal("app role updated deletion lifecycle, want column privilege rejection")
	}
	_ = tx.Rollback(ctx)

	if _, err := appPool.Exec(ctx, `SELECT count(*) FROM stripe_events`); err == nil {
		t.Fatal("app role read Stripe events, want privilege rejection")
	}
	if _, err := appPool.Exec(ctx, `SELECT last_reconciled_at FROM widget_funnel_maintenance`); err == nil {
		t.Fatal("app role read widget funnel maintenance state, want privilege rejection")
	}
	if _, err := adminPool.Exec(ctx, `
		UPDATE widget_funnel_maintenance SET last_reconciled_at = now() WHERE singleton = true
	`); err != nil {
		t.Fatalf("update widget funnel maintenance state as admin: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `INSERT INTO stripe_events (event_id) VALUES ('evt_migration_test')`); err != nil {
		t.Fatalf("insert Stripe event as admin: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `
		INSERT INTO tenant_deletions (
			tenant_id, tenant_slug, tenant_name, requested_at, executed_at,
			forced_by_platform_admin_id, feedback_count, attachment_count
		) VALUES (gen_random_uuid(), 'deleted-test', 'Deleted Test', now(), now(), gen_random_uuid(), 0, 0)
	`); err != nil {
		t.Fatalf("insert tenant deletion audit as admin: %v", err)
	}

	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM tenant_members`).Scan(&count); err != nil {
		t.Fatalf("query members as admin role: %v", err)
	}
	if count != 4 {
		t.Fatalf("admin member count = %d, want 4", count)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM widget_rule_target_conditions`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin widget target condition count = %d, error = %v, want 2", count, err)
	}
	for _, table := range []string{"widget_funnel_events", "widget_funnel_daily"} {
		if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 2 {
			t.Fatalf("admin %s count = %d, error = %v, want 2", table, count, err)
		}
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM ai_daily_usage`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin AI usage count = %d, error = %v, want 2", count, err)
	}
	for _, table := range []string{"ai_project_daily_usage", "ai_work_reservations"} {
		if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil ||
			count != 2 {
			t.Fatalf("admin %s count = %d, error = %v, want 2", table, count, err)
		}
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM ai_gemini_files`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin Gemini file count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM privacy_jobs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin privacy job count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM reporter_privacy_requests`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin reporter privacy request count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM abuse_diagnostics_hourly`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin abuse diagnostic count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM identity_asset_deletions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("admin identity cleanup count = %d, error = %v, want 1", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM issue_updates`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin issue update count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM delivery_logs`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("admin delivery log count = %d, error = %v, want 4", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM integrations`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin integration count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin replay session count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM session_batches`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin replay batch count = %d, error = %v, want 2", count, err)
	}
	for table, want := range map[string]int{"tags": 2, "feedback_tags": 1, "issue_tags": 1} {
		if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != want {
			t.Fatalf("admin %s count = %d, error = %v, want %d", table, count, err, want)
		}
	}

	if _, err := root.Exec(ctx, `DELETE FROM tenant_members WHERE id = $1`, assigneeA); err != nil {
		t.Fatalf("delete assigned tenant member: %v", err)
	}
	var feedbackAssignee, issueAssignee *string
	if err := root.QueryRow(ctx, `
		SELECT feedback.assignee_id, issue.assignee_id
		FROM feedbacks AS feedback
		JOIN issues AS issue ON issue.id = $2
		WHERE feedback.id = $1
	`, feedbackA, issueA).Scan(&feedbackAssignee, &issueAssignee); err != nil {
		t.Fatalf("read resources after assigned member deletion: %v", err)
	}
	if feedbackAssignee != nil || issueAssignee != nil {
		t.Fatalf("assignees after member deletion = feedback %v issue %v, want nil", feedbackAssignee, issueAssignee)
	}
	if _, err := root.Exec(ctx, `DELETE FROM tags WHERE id = $1`, tagA); err != nil {
		t.Fatalf("delete assigned tag: %v", err)
	}
	for table := range map[string]struct{}{"feedback_tags": {}, "issue_tags": {}} {
		if err := root.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, tenantA).Scan(&count); err != nil || count != 0 {
			t.Fatalf("tenant A %s after tag deletion = %d, error = %v, want 0", table, count, err)
		}
	}

	if err := migrateToVersion(migrator, 53); err != nil {
		t.Fatalf("roll back widget definition migration: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET enabled = false, greeting = 'Legacy localized prompt' WHERE id = $1
	`, widgetRuleA); err != nil {
		t.Fatalf("prepare legacy widget definition: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks SET rating = 4, text = 'Legacy structured response' WHERE id = $1
	`, feedbackA); err != nil {
		t.Fatalf("prepare legacy feedback response: %v", err)
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("reapply widget definition migration: %v", err)
	}
	var localizedPrompt, appearancePosition, appearanceTheme string
	if err := root.QueryRow(ctx, `
		SELECT definition_key, definition_version, definition_status, presentation,
		       launcher_variant,
		       localizations->'en'->>'prompt',
		       widget_appearance_overrides->>'position',
		       widget_appearance_overrides->>'theme'
		FROM widget_rules WHERE id = $1
	`, widgetRuleA).Scan(
		&definitionKey, &definitionVersion, &definitionStatus, &presentation, &launcherVariant,
		&localizedPrompt, &appearancePosition, &appearanceTheme,
	); err != nil {
		t.Fatalf("read backfilled widget definition: %v", err)
	}
	if definitionKey == "" || definitionVersion != 1 || definitionStatus != "paused" ||
		presentation != "prompt" || launcherVariant != "floating_button" ||
		localizedPrompt != "Legacy localized prompt" ||
		appearancePosition == "" || appearanceTheme == "" {
		t.Fatalf(
			"backfilled widget definition = %q/%d/%q/%q/%q/%q appearance %q/%q",
			definitionKey, definitionVersion, definitionStatus, presentation, launcherVariant,
			localizedPrompt, appearancePosition, appearanceTheme,
		)
	}
	var responseRating *int
	var responseText *string
	if err := root.QueryRow(ctx, `
		SELECT (responses->>'rating')::integer, responses->>'feedback'
		FROM feedbacks WHERE id = $1
	`, feedbackA).Scan(&responseRating, &responseText); err != nil {
		t.Fatalf("read backfilled structured feedback responses: %v", err)
	}
	if responseRating == nil || *responseRating != 4 || responseText == nil || *responseText != "Legacy structured response" {
		t.Fatalf("structured feedback response backfill = rating %v text %v", responseRating, responseText)
	}

	var newWidgetRule string
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds
		) VALUES ($1, 'Edge tab default', 'all_pages', 'delay', 0)
		RETURNING id
	`, projectA).Scan(&newWidgetRule); err != nil {
		t.Fatalf("create widget rule after launcher variant migration: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT launcher_variant FROM widget_rules WHERE id = $1
	`, newWidgetRule).Scan(&launcherVariant); err != nil {
		t.Fatalf("read new widget rule launcher variant: %v", err)
	}
	if launcherVariant != "edge_tab" {
		t.Fatalf("new widget rule launcher variant = %q, want edge_tab", launcherVariant)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET launcher_variant = 'sidebar_drawer' WHERE id = $1
	`, newWidgetRule); err == nil {
		t.Fatal("invalid widget launcher variant succeeded")
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET launcher_variant = NULL WHERE id = $1
	`, newWidgetRule); err == nil {
		t.Fatal("null widget launcher variant succeeded")
	}
	if _, err := root.Exec(ctx, `DELETE FROM widget_rules WHERE id = $1`, newWidgetRule); err != nil {
		t.Fatalf("delete disposable launcher variant rule: %v", err)
	}

	if err := migrateToVersion(migrator, 48); err != nil {
		t.Fatalf("roll back widget funnel analytics migration: %v", err)
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("reapply widget funnel analytics migration: %v", err)
	}
	if err := migrateToVersion(migrator, 47); err != nil {
		t.Fatalf("roll back widget target conditions migration: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules
		SET matcher_type = 'url_exact', matcher_value = 'HTTPS://A.EXAMPLE.COM:443/chart///?asset=BAI#price'
		WHERE id = $1
	`, widgetRuleA); err != nil {
		t.Fatalf("prepare legacy widget matcher before targeting replay: %v", err)
	}
	var preservedIssueID string
	if err := root.QueryRow(ctx, `SELECT issue_id FROM feedbacks WHERE id = $1`, feedbackA).Scan(&preservedIssueID); err != nil {
		t.Fatalf("read feedback link after targeting rollback: %v", err)
	}
	if preservedIssueID != issueA {
		t.Fatalf("feedback issue after targeting rollback = %q, want %q", preservedIssueID, issueA)
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("reapply widget target conditions migration: %v", err)
	}
	var targetKind, targetMatcherType, targetMatcherValue string
	var targetPosition int
	if err := root.QueryRow(ctx, `
		SELECT kind, matcher_type, matcher_value, position
		FROM widget_rule_target_conditions
		WHERE widget_rule_id = $1
	`, widgetRuleA).Scan(&targetKind, &targetMatcherType, &targetMatcherValue, &targetPosition); err != nil {
		t.Fatalf("read backfilled widget target condition: %v", err)
	}
	if targetKind != "include" || targetMatcherType != "url_exact" ||
		targetMatcherValue != "https://a.example.com/chart" || targetPosition != 0 {
		t.Fatalf(
			"backfilled widget target = %q/%q/%q/%d, want include/url_exact/https://a.example.com/chart/0",
			targetKind, targetMatcherType, targetMatcherValue, targetPosition,
		)
	}
	if err := migrateToVersion(migrator, 46); err != nil {
		t.Fatalf("roll back targeting and widget recurrence migrations: %v", err)
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("reapply widget recurrence and targeting migrations: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT feedback.issue_id, rule.recurrence_mode, rule.recurrence_days
		FROM feedbacks AS feedback
		JOIN widget_rules AS rule ON rule.id = $2
		WHERE feedback.id = $1
	`, feedbackA, widgetRuleA).Scan(&preservedIssueID, &recurrenceMode, &recurrenceDays); err != nil {
		t.Fatalf("read state after recurrence replay: %v", err)
	}
	if preservedIssueID != issueA || recurrenceMode != "cooldown" ||
		recurrenceDays == nil || *recurrenceDays != 30 {
		t.Fatalf("state after recurrence replay = issue %q recurrence %q/%v", preservedIssueID, recurrenceMode, recurrenceDays)
	}

	if err := migrateToVersion(migrator, 72); err != nil {
		t.Fatalf("roll back canvas replay policy migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply canvas replay policy migration: %v", err)
	}
	var replayRecordCanvas bool
	var replayCanvasFPS int
	if err := root.QueryRow(ctx, `
		SELECT replay_record_canvas, replay_canvas_fps
		FROM widget_rules
		WHERE id = $1
	`, widgetRuleA).Scan(&replayRecordCanvas, &replayCanvasFPS); err != nil {
		t.Fatalf("read backfilled canvas replay policy: %v", err)
	}
	if replayRecordCanvas || replayCanvasFPS != 2 {
		t.Fatalf("backfilled canvas replay policy = %t/%d, want false/2", replayRecordCanvas, replayCanvasFPS)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET replay_canvas_fps = 6 WHERE id = $1
	`, widgetRuleA); err == nil {
		t.Fatal("canvas replay FPS constraint accepted value above 5")
	}

	if _, err := root.Exec(ctx, `
			UPDATE widget_rules
			SET replay_mask_all_inputs = false, replay_record_canvas = true
			WHERE id = $1
		`, widgetRuleA); err != nil {
		t.Fatalf("write legacy unsafe replay rule before migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply replay input masking migration: %v", err)
	}
	var replayMaskAllInputs bool
	if err := root.QueryRow(ctx, `
			SELECT replay_mask_all_inputs, replay_record_canvas
			FROM widget_rules WHERE id = $1
		`, widgetRuleA).Scan(&replayMaskAllInputs, &replayRecordCanvas); err != nil {
		t.Fatalf("read backfilled replay input masking: %v", err)
	}
	if !replayMaskAllInputs || replayRecordCanvas {
		t.Fatalf(
			"backfilled replay privacy policy = mask %t canvas %t, want true/false",
			replayMaskAllInputs, replayRecordCanvas,
		)
	}
	if _, err := root.Exec(ctx, `
			UPDATE widget_rules SET replay_mask_all_inputs = false WHERE id = $1
		`, widgetRuleA); err == nil {
		t.Fatal("replay input masking constraint accepted false")
	}
	if _, err := root.Exec(ctx, `
			UPDATE widget_rules SET replay_record_canvas = true WHERE id = $1
		`, widgetRuleA); err == nil {
		t.Fatal("anonymous replay canvas constraint accepted true")
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back replay input masking migration: %v", err)
	}
	if _, err := root.Exec(ctx, `
			UPDATE widget_rules
			SET replay_mask_all_inputs = false, replay_record_canvas = true
			WHERE id = $1
		`, widgetRuleA); err != nil {
		t.Fatalf("write unsafe replay rule after rollback: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply replay input masking migration: %v", err)
	}
	if err := root.QueryRow(ctx, `
			SELECT replay_mask_all_inputs, replay_record_canvas
			FROM widget_rules WHERE id = $1
		`, widgetRuleA).Scan(&replayMaskAllInputs, &replayRecordCanvas); err != nil {
		t.Fatalf("read re-migrated replay input masking: %v", err)
	}
	if !replayMaskAllInputs || replayRecordCanvas {
		t.Fatalf(
			"re-migrated replay privacy policy = mask %t canvas %t, want true/false",
			replayMaskAllInputs, replayRecordCanvas,
		)
	}

	if _, err := root.Exec(ctx, `
		UPDATE sessions SET page_url = $2 WHERE id = $1
	`, sessionA, "https://sentinel-user:sentinel-pass@a.example.com/chart/?email=jane.doe@example.com&token=secret#fragment-secret"); err != nil {
		t.Fatalf("write legacy uncanonicalized replay page URL: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply replay page URL canonicalization migration: %v", err)
	}
	var canonicalizedPageURL string
	var legacyPrivacyVersion int
	if err := root.QueryRow(ctx, `
			SELECT page_url, replay_privacy_version FROM sessions WHERE id = $1
		`, sessionA).Scan(&canonicalizedPageURL, &legacyPrivacyVersion); err != nil {
		t.Fatalf("read canonicalized legacy replay page URL: %v", err)
	}
	if canonicalizedPageURL != "https://a.example.com/chart" || legacyPrivacyVersion != 0 {
		t.Fatalf(
			"legacy replay privacy state = URL %q version %d, want https://a.example.com/chart and version 0",
			canonicalizedPageURL, legacyPrivacyVersion,
		)
	}
	var newPrivacyVersion int
	if err := root.QueryRow(ctx, `
			INSERT INTO sessions (
				tenant_id, project_id, session_id, started_at, rrweb_version,
				max_duration_seconds, max_events
			) VALUES ($1, $2, gen_random_uuid(), now(), '2.1.1', 1800, 50000)
			RETURNING replay_privacy_version
		`, tenantA, projectA).Scan(&newPrivacyVersion); err != nil {
		t.Fatalf("insert replay after privacy contract migration: %v", err)
	}
	if newPrivacyVersion != 1 {
		t.Fatalf("new replay privacy version = %d, want 1", newPrivacyVersion)
	}
	var cleanupIndex *string
	if err := root.QueryRow(ctx, `
			SELECT to_regclass('public.idx_sessions_replay_privacy_cleanup')::text
		`).Scan(&cleanupIndex); err != nil || cleanupIndex == nil {
		t.Fatalf("replay privacy cleanup index = %v, error = %v", cleanupIndex, err)
	}

	if err := migrateToVersion(migrator, 44); err != nil {
		t.Fatalf("roll back targeting, recurrence, triage, and feedback/issue decoupling migrations: %v", err)
	}
	legacyDescription := "Legacy feedback keeps its complete description after migration. More detail follows."
	if _, err := root.Exec(ctx, `UPDATE feedbacks SET text = $2 WHERE id = $1`, feedbackA, legacyDescription); err != nil {
		t.Fatalf("prepare legacy feedback before migration replay: %v", err)
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("reapply feedback/issue decoupling, triage, recurrence, and targeting migrations: %v", err)
	}
	var migratedTitle, migratedDescription, migratedIssueDescription, migratedIssueID string
	if err := root.QueryRow(ctx, `
		SELECT feedback.title, feedback.description, feedback.issue_id, issue.description
		FROM feedbacks AS feedback
		JOIN issues AS issue ON issue.id = feedback.issue_id
		WHERE feedback.id = $1
	`, feedbackA).Scan(
		&migratedTitle, &migratedDescription, &migratedIssueID, &migratedIssueDescription,
	); err != nil {
		t.Fatalf("read feedback/issue backfill after migration replay: %v", err)
	}
	if migratedTitle != "Legacy feedback keeps its complete description after migration." ||
		migratedDescription != legacyDescription || migratedIssueID != issueA ||
		migratedIssueDescription != legacyDescription {
		t.Fatalf("migration backfill = title %q description %q issue %q issue description %q",
			migratedTitle, migratedDescription, migratedIssueID, migratedIssueDescription)
	}

	if err := migrateToVersion(migrator, 42); err != nil {
		t.Fatalf("roll back targeting, recurrence, triage, product fields, session rotation marker, and replay cap migrations: %v", err)
	}
	for cycle := 1; cycle <= 2; cycle++ {
		if _, err := root.Exec(ctx, `
			UPDATE widget_rules
			SET replay_max_duration_seconds = 7200, replay_max_events = 200000
			WHERE id = $1
		`, widgetRuleA); err != nil {
			t.Fatalf("write legacy replay caps before migration cycle %d: %v", cycle, err)
		}
		if err := migrator.Steps(1); err != nil {
			t.Fatalf("apply replay cap migration cycle %d: %v", cycle, err)
		}

		var replayMaxDuration, replayMaxEvents int
		if err := root.QueryRow(ctx, `
			SELECT replay_max_duration_seconds, replay_max_events
			FROM widget_rules
			WHERE id = $1
		`, widgetRuleA).Scan(&replayMaxDuration, &replayMaxEvents); err != nil {
			t.Fatalf("read normalized replay caps after migration cycle %d: %v", cycle, err)
		}
		if replayMaxDuration != 1800 || replayMaxEvents != 50000 {
			t.Fatalf(
				"replay caps after migration cycle %d = %d seconds/%d events, want 1800/50000",
				cycle, replayMaxDuration, replayMaxEvents,
			)
		}
		if cycle == 1 {
			if err := migrator.Steps(-1); err != nil {
				t.Fatalf("roll back replay cap migration after cycle 1: %v", err)
			}
		}
	}

	if err := migrateToVersion(migrator, 75); err != nil {
		t.Fatalf("roll back reporter consent evidence migration: %v", err)
	}
	// Seed a pre-migration ("legacy") consented row exactly as migration 000062 already produces:
	// notify_reporter=true with a grant timestamp, but no structured evidence.
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks SET reporter_email = 'legacy-reporter@example.com', notify_reporter = true
		WHERE id = $1
	`, feedbackA); err != nil {
		t.Fatalf("seed legacy consented feedback before evidence migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply reporter consent evidence migration: %v", err)
	}
	var legacyPurpose *string
	var legacyGrantedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT reporter_consent_purpose, reporter_consent_granted_at FROM feedbacks WHERE id = $1
	`, feedbackA).Scan(&legacyPurpose, &legacyGrantedAt); err != nil {
		t.Fatalf("read legacy feedback after evidence migration: %v", err)
	}
	if legacyPurpose != nil || legacyGrantedAt == nil {
		t.Fatalf("legacy consent row after migration = purpose %v granted_at %v, want no evidence with the grant timestamp preserved",
			legacyPurpose, legacyGrantedAt)
	}
	// A fresh, fully-evidenced grant is accepted...
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks SET reporter_consent_purpose = 'feedback_updates',
		    reporter_consent_notice_version = '2026-07', reporter_consent_locale = 'en',
		    reporter_consent_source = 'widget_submission'
		WHERE id = $1
	`, feedbackA); err != nil {
		t.Fatalf("apply full consent evidence: %v", err)
	}
	// ...but a partial evidence write is rejected by the all-or-nothing evidence constraint,
	// and an unrelated later update to the still evidence-free legacy issue row must not be
	// blocked by that same constraint (it never touches the evidence columns).
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks SET reporter_consent_notice_version = NULL WHERE id = $1
	`, feedbackA); err == nil {
		t.Fatal("partial consent evidence write was accepted")
	}
	for name, query := range map[string]string{
		"unknown purpose": `
			UPDATE feedbacks SET reporter_consent_purpose = 'marketing' WHERE id = $1`,
		"unknown notice version": `
			UPDATE feedbacks SET reporter_consent_notice_version = '1999-01' WHERE id = $1`,
		"unsupported locale": `
			UPDATE feedbacks SET reporter_consent_locale = 'fr' WHERE id = $1`,
		"unknown source": `
			UPDATE feedbacks SET reporter_consent_source = 'import' WHERE id = $1`,
	} {
		if _, err := root.Exec(ctx, query, feedbackA); err == nil {
			t.Fatalf("%s consent evidence was accepted", name)
		}
	}
	if _, err := root.Exec(ctx, `UPDATE issues SET priority = 'low' WHERE id = $1`, issueA); err != nil {
		t.Fatalf("unrelated update to legacy-consent issue row after evidence migration: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE issues SET reporter_email = 'reporter@example.com', notify_reporter = true,
		    reporter_consent_purpose = 'feedback_updates',
		    reporter_consent_notice_version = '2026-07', reporter_consent_locale = 'pt-AO',
		    reporter_consent_source = 'widget_submission'
		WHERE id = $1
	`, issueA); err != nil {
		t.Fatalf("apply full issue consent evidence: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE issues SET reporter_consent_source = 'import' WHERE id = $1
	`, issueA); err == nil {
		t.Fatal("unknown issue consent source was accepted")
	}
	var evidenceIndexCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes WHERE schemaname = 'public'
		  AND indexname IN ('idx_feedbacks_reporter_consent_active', 'idx_issues_reporter_consent_active')
	`).Scan(&evidenceIndexCount); err != nil || evidenceIndexCount != 2 {
		t.Fatalf("reporter consent evidence index count = %d, error = %v, want 2", evidenceIndexCount, err)
	}

	if err := migrateToVersion(migrator, 76); err != nil {
		t.Fatalf("roll back automatic context remediation migration: %v", err)
	}
	const unsafeContextURL = "https://sentinel-user:sentinel-pa@ss@A.EXAMPLE.COM:443/private/account-48291?email=jane@example.com&token=SECRET#PRIVATE"
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks
		SET page_url = $2,
		    referrer = 'https://referrer.example/private?reporter=jane@example.com',
		    user_agent = 'Sentinel Browser jane@example.com',
		    dom_snapshot_html = '<div data-email="jane@example.com">SECRET</div>',
		    dom_snapshot_styles = '{"background-image":"url(https://avatar.example/jane.png)"}'
		WHERE id = $1
	`, feedbackA, unsafeContextURL); err != nil {
		t.Fatalf("seed legacy feedback automatic context: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE sessions SET page_url = $2 WHERE id = $1
	`, sessionA, unsafeContextURL); err != nil {
		t.Fatalf("seed legacy replay location: %v", err)
	}
	var priorReplayContractID string
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, page_url, started_at, ended_at,
			events_count, batches_count, storage_key, rrweb_version, merged_at,
			merge_requested_at, max_duration_seconds, max_events,
			replay_privacy_version
		) VALUES (
			$1, $2, gen_random_uuid(), $3, now() - interval '1 minute', now(),
			1, 1, 'tenants/sentinel-user/sessions/SECRET/replay.json', '2.1.1',
			now(), now(), 1800, 50000, 1
		)
		RETURNING id
	`, tenantA, projectA, unsafeContextURL).Scan(&priorReplayContractID); err != nil {
		t.Fatalf("seed prior replay context privacy contract: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO widget_funnel_events (
			event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			visitor_id, page_view_id, page_url, page_key, occurred_at
		) VALUES (
			'00000000-0000-4000-8000-000000000501', $1, $2, $3,
			'Legacy context', 'matched', gen_random_uuid(), gen_random_uuid(),
			$4, digest($4, 'sha256'), now()
		)
	`, tenantA, projectA, widgetRuleA, unsafeContextURL); err != nil {
		t.Fatalf("seed legacy funnel event location: %v", err)
	}
	if _, err := root.Exec(ctx, `
		DELETE FROM widget_funnel_daily WHERE tenant_id = $1 AND rule_id = $2
	`, tenantA, widgetRuleA); err != nil {
		t.Fatalf("clear funnel aggregates before remediation fixture: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO widget_funnel_daily (
			tenant_id, project_id, rule_id, rule_name, page_url, page_key,
			event_date, matched_count, impression_count
		) VALUES
			($1, $3, $2, 'Replay limits', $4, digest($4, 'sha256'), CURRENT_DATE, 1, 2),
			($1, $3, $2, 'Replay limits', $5, digest($5, 'sha256'), CURRENT_DATE, 3, 4)
	`, tenantA, widgetRuleA, projectA,
		"https://a.example.com/private/jane@example.com?token=SECRET",
		"https://a.example.com/accounts/account-48291#PRIVATE",
	); err != nil {
		t.Fatalf("seed colliding legacy funnel aggregates: %v", err)
	}
	var legacyExportJobID string
	if err := root.QueryRow(ctx, `
		INSERT INTO privacy_jobs (
			tenant_id, kind, status, actor_member_id, started_at, heartbeat_at,
			completed_at, expires_at, object_key, object_size_bytes
		) VALUES (
			$1, 'workspace_export', 'completed', $2, now() - interval '1 minute', now(),
			now(), now() + interval '7 days',
			'tenants/sentinel-user/exports/SECRET.zip', 128
		)
		RETURNING id
	`, tenantA, memberA).Scan(&legacyExportJobID); err != nil {
		t.Fatalf("seed legacy export archive: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply automatic context remediation migration: %v", err)
	}

	var remediatedPage string
	var remediatedReferrer, remediatedUserAgent, remediatedHTML, remediatedStyles *string
	if err := root.QueryRow(ctx, `
		SELECT page_url, referrer, user_agent, dom_snapshot_html, dom_snapshot_styles
		FROM feedbacks WHERE id = $1
	`, feedbackA).Scan(
		&remediatedPage, &remediatedReferrer, &remediatedUserAgent,
		&remediatedHTML, &remediatedStyles,
	); err != nil {
		t.Fatalf("read remediated feedback context: %v", err)
	}
	if remediatedPage != "https://a.example.com/" ||
		remediatedReferrer != nil || remediatedUserAgent != nil ||
		remediatedHTML != nil || remediatedStyles != nil {
		t.Fatalf(
			"remediated feedback context = page %q referrer %v user agent %v html %v styles %v",
			remediatedPage, remediatedReferrer, remediatedUserAgent,
			remediatedHTML, remediatedStyles,
		)
	}
	if err := root.QueryRow(ctx, `
		SELECT page_url FROM sessions WHERE id = $1
	`, sessionA).Scan(&remediatedPage); err != nil || remediatedPage != "https://a.example.com/" {
		t.Fatalf("remediated replay location = %q, error = %v", remediatedPage, err)
	}
	var priorReplayPrivacyVersion int
	if err := root.QueryRow(ctx, `
		SELECT page_url, replay_privacy_version
		FROM sessions
		WHERE id = $1
	`, priorReplayContractID).Scan(
		&remediatedPage,
		&priorReplayPrivacyVersion,
	); err != nil || remediatedPage != "https://a.example.com/" ||
		priorReplayPrivacyVersion != 0 {
		t.Fatalf(
			"prior replay context privacy = page %q version %d, error = %v",
			remediatedPage,
			priorReplayPrivacyVersion,
			err,
		)
	}
	var currentReplayPrivacyVersion int
	if err := root.QueryRow(ctx, `
		INSERT INTO sessions (
			tenant_id, project_id, session_id, page_url, started_at, rrweb_version,
			max_duration_seconds, max_events
		) VALUES (
			$1, $2, gen_random_uuid(), 'https://a.example.com/', now(),
			'2.1.1', 1800, 50000
		)
		RETURNING replay_privacy_version
	`, tenantA, projectA).Scan(
		&currentReplayPrivacyVersion,
	); err != nil || currentReplayPrivacyVersion != 2 {
		t.Fatalf(
			"current replay context privacy version = %d, error = %v, want 2",
			currentReplayPrivacyVersion,
			err,
		)
	}
	var funnelKeyMatches bool
	if err := root.QueryRow(ctx, `
		SELECT page_url, page_key = digest(page_url, 'sha256')
		FROM widget_funnel_events
		WHERE event_id = '00000000-0000-4000-8000-000000000501'
	`).Scan(&remediatedPage, &funnelKeyMatches); err != nil ||
		remediatedPage != "https://a.example.com/" || !funnelKeyMatches {
		t.Fatalf(
			"remediated funnel event = page %q key matches %t, error = %v",
			remediatedPage, funnelKeyMatches, err,
		)
	}
	var aggregateCount, matchedCount, impressionCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*), sum(matched_count), sum(impression_count)
		FROM widget_funnel_daily
		WHERE tenant_id = $1 AND rule_id = $2 AND event_date = CURRENT_DATE
	`, tenantA, widgetRuleA).Scan(
		&aggregateCount, &matchedCount, &impressionCount,
	); err != nil || aggregateCount != 1 || matchedCount != 4 || impressionCount != 6 {
		t.Fatalf(
			"remediated funnel aggregates = rows %d matched %d impressions %d, error = %v",
			aggregateCount, matchedCount, impressionCount, err,
		)
	}
	var legacyContextVersion int
	if err := root.QueryRow(ctx, `
		SELECT context_privacy_version FROM privacy_jobs WHERE id = $1
	`, legacyExportJobID).Scan(&legacyContextVersion); err != nil || legacyContextVersion != 0 {
		t.Fatalf(
			"legacy export context privacy version = %d, error = %v, want 0",
			legacyContextVersion, err,
		)
	}
	var contextCleanupIndex *string
	if err := root.QueryRow(ctx, `
		SELECT to_regclass('public.idx_privacy_jobs_context_privacy_cleanup')::text
	`).Scan(&contextCleanupIndex); err != nil || contextCleanupIndex == nil {
		t.Fatalf("context privacy cleanup index = %v, error = %v", contextCleanupIndex, err)
	}

	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply reporter privacy review queue migration: %v", err)
	}
	var legacyReviewRequestID string
	if err := root.QueryRow(ctx, `
		INSERT INTO reporter_privacy_requests (
			tenant_id, action, status, reporter_email, email_digest, token_hash,
			verification_expires_at, verified_at, access_expires_at, completed_at,
			record_count, manual_review_count
		) VALUES (
			$1, 'anonymize', 'completed', NULL, digest('legacy-review-email', 'sha256'),
			digest('legacy-review-token', 'sha256'), now() + interval '1 day',
			now() - interval '2 hours', now() + interval '30 days',
			now() - interval '1 hour', 1, 1
		)
		RETURNING id
	`, tenantA).Scan(&legacyReviewRequestID); err != nil {
		t.Fatalf("seed legacy completed reporter privacy request: %v", err)
	}
	var legacyReviewJobID string
	if err := root.QueryRow(ctx, `
		INSERT INTO privacy_jobs (
			tenant_id, kind, status, actor_member_id, reporter_request_id,
			started_at, heartbeat_at, completed_at, expires_at, object_size_bytes
		) VALUES (
			$1, 'reporter_anonymize', 'completed', $2, $3,
			now() - interval '2 hours', now() - interval '1 hour',
			now() - interval '1 hour', now() + interval '7 days', 0
		)
		RETURNING id
	`, tenantA, memberA, legacyReviewRequestID).Scan(&legacyReviewJobID); err != nil {
		t.Fatalf("seed legacy completed reporter privacy job: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply reporter privacy review lifecycle migration: %v", err)
	}
	var legacyRequestStatus, legacyJobStatus string
	var legacyRequestCompletedAt, legacyJobCompletedAt *time.Time
	if err := root.QueryRow(ctx, `
		SELECT status::text, completed_at
		FROM reporter_privacy_requests
		WHERE id = $1
	`, legacyReviewRequestID).Scan(&legacyRequestStatus, &legacyRequestCompletedAt); err != nil {
		t.Fatalf("read reconciled reporter privacy request: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT status::text, completed_at
		FROM privacy_jobs
		WHERE id = $1
	`, legacyReviewJobID).Scan(&legacyJobStatus, &legacyJobCompletedAt); err != nil {
		t.Fatalf("read reconciled reporter privacy job: %v", err)
	}
	if legacyRequestStatus != "awaiting_manual_review" || legacyRequestCompletedAt != nil ||
		legacyJobStatus != "awaiting_manual_review" || legacyJobCompletedAt != nil {
		t.Fatalf(
			"legacy privacy reconciliation = request %q/%v job %q/%v",
			legacyRequestStatus, legacyRequestCompletedAt, legacyJobStatus, legacyJobCompletedAt,
		)
	}
	var legacyReviewItems int
	if err := root.QueryRow(ctx, `
		SELECT count(*)
		FROM reporter_privacy_review_items
		WHERE tenant_id = $1 AND reporter_request_id = $2
		  AND privacy_job_id = $3 AND resource_type = 'legacy_unlinked'
		  AND status = 'pending'
	`, tenantA, legacyReviewRequestID, legacyReviewJobID).Scan(&legacyReviewItems); err != nil ||
		legacyReviewItems != 1 {
		t.Fatalf("legacy privacy review items = %d, error = %v", legacyReviewItems, err)
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("apply replay consent orchestration migration: %v", err)
	}
	var legacyReplayConsentMode string
	if err := root.QueryRow(ctx, `
		SELECT replay_consent->>'mode' FROM widget_rules WHERE id = $1
	`, widgetRuleA).Scan(&legacyReplayConsentMode); err != nil || legacyReplayConsentMode != "none" {
		t.Fatalf(
			"legacy replay consent mode = %q, error = %v, want none",
			legacyReplayConsentMode, err,
		)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET replay_consent = '{"mode":"implicit"}'::jsonb WHERE id = $1
	`, widgetRuleA); err == nil {
		t.Fatal("replay consent configuration accepted an implicit mode")
	}
	var replayConsentRLS, replayConsentForceRLS bool
	if err := root.QueryRow(ctx, `
		SELECT relrowsecurity, relforcerowsecurity
		FROM pg_class
		WHERE oid = 'replay_consents'::regclass
	`).Scan(&replayConsentRLS, &replayConsentForceRLS); err != nil ||
		!replayConsentRLS || !replayConsentForceRLS {
		t.Fatalf(
			"replay consent RLS = enabled %t forced %t, error = %v",
			replayConsentRLS, replayConsentForceRLS, err,
		)
	}
	var replayConsentID string
	if err := root.QueryRow(ctx, `
		INSERT INTO replay_consents (
			tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, token_hash, state, accepted_at, expires_at
		) VALUES (
			$1, $2, $3, 'session_replay', '2026-07', 'en', 'customer_cmp',
			encode(digest('replay-consent-migration', 'sha256'), 'hex'),
			'active', now(), now() + interval '30 days'
		)
		RETURNING id
	`, tenantA, projectA, widgetRuleA).Scan(&replayConsentID); err != nil {
		t.Fatalf("insert replay consent migration fixture: %v", err)
	}
	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tenant B replay consent visibility transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB); err != nil {
		t.Fatalf("set tenant B replay consent scope: %v", err)
	}
	var replayConsentVisible int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM replay_consents WHERE id = $1
	`, replayConsentID).Scan(&replayConsentVisible); err != nil || replayConsentVisible != 0 {
		t.Fatalf(
			"tenant B replay consent visibility = %d, error = %v",
			replayConsentVisible, err,
		)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("roll back tenant B replay consent visibility transaction: %v", err)
	}

	var legacyAnalyticsConsentMode string
	if err := root.QueryRow(ctx, `
		SELECT analytics_consent->>'mode' FROM widget_rules WHERE id = $1
	`, widgetRuleA).Scan(&legacyAnalyticsConsentMode); err != nil || legacyAnalyticsConsentMode != "none" {
		t.Fatalf(
			"legacy widget analytics consent mode = %q, error = %v, want none",
			legacyAnalyticsConsentMode, err,
		)
	}
	if _, err := root.Exec(ctx, `
		UPDATE widget_rules SET analytics_consent = '{"mode":"implicit"}'::jsonb WHERE id = $1
	`, widgetRuleA); err == nil {
		t.Fatal("widget analytics consent configuration accepted an implicit mode")
	}
	var analyticsConsentRLS, analyticsConsentForceRLS bool
	if err := root.QueryRow(ctx, `
		SELECT relrowsecurity, relforcerowsecurity
		FROM pg_class
		WHERE oid = 'widget_analytics_consents'::regclass
	`).Scan(&analyticsConsentRLS, &analyticsConsentForceRLS); err != nil ||
		!analyticsConsentRLS || !analyticsConsentForceRLS {
		t.Fatalf(
			"widget analytics consent RLS = enabled %t forced %t, error = %v",
			analyticsConsentRLS, analyticsConsentForceRLS, err,
		)
	}
	var analyticsConsentID string
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_analytics_consents (
			tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, token_hash, state, accepted_at, expires_at
		) VALUES (
			$1, $2, $3, 'widget_analytics', '2026-07', 'en', 'customer_cmp',
			encode(digest('analytics-consent-migration', 'sha256'), 'hex'),
			'active', now(), now() + interval '30 days'
		)
		RETURNING id
	`, tenantA, projectA, widgetRuleA).Scan(&analyticsConsentID); err != nil {
		t.Fatalf("insert widget analytics consent migration fixture: %v", err)
	}
	tx, err = appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tenant B widget analytics consent visibility transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantB); err != nil {
		t.Fatalf("set tenant B widget analytics consent scope: %v", err)
	}
	var analyticsConsentVisible int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM widget_analytics_consents WHERE id = $1
	`, analyticsConsentID).Scan(&analyticsConsentVisible); err != nil || analyticsConsentVisible != 0 {
		t.Fatalf(
			"tenant B widget analytics consent visibility = %d, error = %v",
			analyticsConsentVisible, err,
		)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("roll back tenant B widget analytics consent visibility transaction: %v", err)
	}

	if err := migrateToVersion(migrator, 93); err != nil {
		t.Fatalf("migrate before platform support permission backfill: %v", err)
	}
	var supportManagerID, limitedPlatformAdminID string
	if err := root.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('support-manager-migration@example.test', ARRAY['platform:admin:update'])
		RETURNING id
	`).Scan(&supportManagerID); err != nil {
		t.Fatalf("insert support-manager migration fixture: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES ('limited-platform-migration@example.test', ARRAY['platform:tenant:read'])
		RETURNING id
	`).Scan(&limitedPlatformAdminID); err != nil {
		t.Fatalf("insert limited platform-admin migration fixture: %v", err)
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("migrate platform support permission backfill: %v", err)
	}
	var supportManagerGranted, limitedAdminGranted bool
	if err := root.QueryRow(ctx, `
		SELECT 'platform:tenant:support_access' = ANY(permissions)
		FROM platform_admins
		WHERE id = $1
	`, supportManagerID).Scan(&supportManagerGranted); err != nil || !supportManagerGranted {
		t.Fatalf("support-manager permission granted = %t, error = %v", supportManagerGranted, err)
	}
	if err := root.QueryRow(ctx, `
		SELECT 'platform:tenant:support_access' = ANY(permissions)
		FROM platform_admins
		WHERE id = $1
	`, limitedPlatformAdminID).Scan(&limitedAdminGranted); err != nil || limitedAdminGranted {
		t.Fatalf("limited platform-admin permission granted = %t, error = %v", limitedAdminGranted, err)
	}
	if _, err := root.Exec(ctx, `
		DELETE FROM platform_admins WHERE id = ANY($1::uuid[])
	`, []string{supportManagerID, limitedPlatformAdminID}); err != nil {
		t.Fatalf("delete platform support permission fixtures: %v", err)
	}
	var twoFactorTablesProtected bool
	if err := root.QueryRow(ctx, `
		SELECT bool_and(relrowsecurity AND relforcerowsecurity)
		FROM pg_class
		WHERE oid = ANY(ARRAY[
			'tenant_member_totp'::regclass,
			'tenant_member_recovery_codes'::regclass
		])
	`).Scan(&twoFactorTablesProtected); err != nil || !twoFactorTablesProtected {
		t.Fatalf("two-factor credential RLS protected = %t, error = %v", twoFactorTablesProtected, err)
	}
	// The application role may switch the policy but must never be able to
	// rewrite ownership, plan, or billing columns on the same table.
	var canUpdatePolicy, canUpdatePlan bool
	if err := root.QueryRow(ctx, `
		SELECT has_column_privilege('fidibeki_app', 'tenants', 'two_factor_policy', 'UPDATE'),
		       has_column_privilege('fidibeki_app', 'tenants', 'plan', 'UPDATE')
	`).Scan(&canUpdatePolicy, &canUpdatePlan); err != nil || !canUpdatePolicy || canUpdatePlan {
		t.Fatalf(
			"two-factor policy grants: policy = %t, plan = %t, error = %v; want true, false",
			canUpdatePolicy, canUpdatePlan, err,
		)
	}
	var recipientTablesProtected bool
	if err := root.QueryRow(ctx, `
		SELECT bool_and(relrowsecurity AND relforcerowsecurity)
		FROM pg_class
		WHERE oid = ANY(ARRAY[
			'privacy_recipient_actions'::regclass,
			'privacy_recipient_action_attempts'::regclass
		])
	`).Scan(&recipientTablesProtected); err != nil || !recipientTablesProtected {
		t.Fatalf("recipient action RLS protected = %t, error = %v",
			recipientTablesProtected, err)
	}
	var canSelectActions, canInsertActions, canDeleteActions bool
	var canUpdateActionStatus, canUpdateRecipientEmail bool
	if err := root.QueryRow(ctx, `
		SELECT
			has_table_privilege('fidibeki_app', 'privacy_recipient_actions', 'SELECT'),
			has_table_privilege('fidibeki_app', 'privacy_recipient_actions', 'INSERT'),
			has_table_privilege('fidibeki_app', 'privacy_recipient_actions', 'DELETE'),
			has_column_privilege(
				'fidibeki_app', 'privacy_recipient_actions', 'status', 'UPDATE'
			),
			has_column_privilege(
				'fidibeki_app', 'privacy_recipient_actions', 'recipient_email', 'UPDATE'
			)
	`).Scan(
		&canSelectActions, &canInsertActions, &canDeleteActions,
		&canUpdateActionStatus, &canUpdateRecipientEmail,
	); err != nil {
		t.Fatalf("read recipient action privileges: %v", err)
	}
	if !canSelectActions || !canInsertActions || canDeleteActions ||
		!canUpdateActionStatus || canUpdateRecipientEmail {
		t.Fatalf(
			"recipient action privileges select=%t insert=%t delete=%t status_update=%t email_update=%t",
			canSelectActions, canInsertActions, canDeleteActions,
			canUpdateActionStatus, canUpdateRecipientEmail,
		)
	}
	var integrationActionsProtected bool
	if err := root.QueryRow(ctx, `
		SELECT relrowsecurity AND relforcerowsecurity
		FROM pg_class
		WHERE oid = 'privacy_integration_actions'::regclass
	`).Scan(&integrationActionsProtected); err != nil || !integrationActionsProtected {
		t.Fatalf("integration action RLS protected = %t, error = %v",
			integrationActionsProtected, err)
	}
	var canSelectIntegrationActions, canInsertIntegrationActions bool
	var canDeleteIntegrationActions, canUpdateIntegrationStatus bool
	var canUpdateIntegrationDestinationKey bool
	if err := root.QueryRow(ctx, `
		SELECT
			has_table_privilege(
				'fidibeki_app', 'privacy_integration_actions', 'SELECT'
			),
			has_table_privilege(
				'fidibeki_app', 'privacy_integration_actions', 'INSERT'
			),
			has_table_privilege(
				'fidibeki_app', 'privacy_integration_actions', 'DELETE'
			),
			has_column_privilege(
				'fidibeki_app', 'privacy_integration_actions', 'status', 'UPDATE'
			),
			has_column_privilege(
				'fidibeki_app', 'privacy_integration_actions', 'destination_key', 'UPDATE'
			)
	`).Scan(
		&canSelectIntegrationActions, &canInsertIntegrationActions,
		&canDeleteIntegrationActions, &canUpdateIntegrationStatus,
		&canUpdateIntegrationDestinationKey,
	); err != nil {
		t.Fatalf("read integration action privileges: %v", err)
	}
	if !canSelectIntegrationActions || !canInsertIntegrationActions ||
		canDeleteIntegrationActions || !canUpdateIntegrationStatus ||
		canUpdateIntegrationDestinationKey {
		t.Fatalf(
			"integration action privileges select=%t insert=%t delete=%t status_update=%t destination_key_update=%t",
			canSelectIntegrationActions, canInsertIntegrationActions,
			canDeleteIntegrationActions, canUpdateIntegrationStatus,
			canUpdateIntegrationDestinationKey,
		)
	}
	var communicationTablesProtected bool
	if err := root.QueryRow(ctx, `
		SELECT bool_and(relrowsecurity AND relforcerowsecurity)
		FROM pg_class
		WHERE oid = ANY(ARRAY[
			'privacy_case_communications'::regclass,
			'privacy_case_communication_attempts'::regclass
		])
	`).Scan(&communicationTablesProtected); err != nil || !communicationTablesProtected {
		t.Fatalf("case communication RLS protected = %t, error = %v",
			communicationTablesProtected, err)
	}
	var canSelectCommunications, canInsertCommunications, canDeleteCommunications bool
	var canUpdateCommunicationStatus, canUpdateCommunicationBody bool
	if err := root.QueryRow(ctx, `
		SELECT
			has_table_privilege('fidibeki_app', 'privacy_case_communications', 'SELECT'),
			has_table_privilege('fidibeki_app', 'privacy_case_communications', 'INSERT'),
			has_table_privilege('fidibeki_app', 'privacy_case_communications', 'DELETE'),
			has_column_privilege(
				'fidibeki_app', 'privacy_case_communications', 'status', 'UPDATE'
			),
			has_column_privilege(
				'fidibeki_app', 'privacy_case_communications', 'message_body', 'UPDATE'
			)
	`).Scan(
		&canSelectCommunications, &canInsertCommunications, &canDeleteCommunications,
		&canUpdateCommunicationStatus, &canUpdateCommunicationBody,
	); err != nil {
		t.Fatalf("read case communication privileges: %v", err)
	}
	if !canSelectCommunications || !canInsertCommunications || canDeleteCommunications ||
		!canUpdateCommunicationStatus || canUpdateCommunicationBody {
		t.Fatalf(
			"case communication privileges select=%t insert=%t delete=%t status_update=%t body_update=%t",
			canSelectCommunications, canInsertCommunications, canDeleteCommunications,
			canUpdateCommunicationStatus, canUpdateCommunicationBody,
		)
	}

	var privacyContact, privacyRights *string
	var privacyProfileVersion int
	if err := root.QueryRow(ctx, `
		SELECT privacy_contact_email::text, privacy_rights_email::text, privacy_profile_version
		FROM tenants WHERE id = $1
	`, tenantA).Scan(&privacyContact, &privacyRights, &privacyProfileVersion); err != nil {
		t.Fatalf("read contextual controller profile defaults: %v", err)
	}
	if privacyContact != nil || privacyRights != nil || privacyProfileVersion != 0 {
		t.Fatalf(
			"contextual controller profile defaults = %v/%v/%d, want nil/nil/0",
			privacyContact, privacyRights, privacyProfileVersion,
		)
	}
	if _, err := root.Exec(ctx, `
		UPDATE tenants SET privacy_contact_email = 'not-an-email' WHERE id = $1
	`, tenantA); err == nil {
		t.Fatal("invalid privacy contact email was accepted")
	}
	const controllerNoticeSnapshot = `{
		"schema_version":1,
		"notice_version":"2026-07-controller-v1",
		"controller_profile_version":1,
		"widget_definition_version":2,
		"locale":"en",
		"controller_name":"Migration Tenant",
		"privacy_contact_email":"privacy@example.test",
		"privacy_rights_email":"rights@example.test",
		"privacy_policy_url":"https://example.test/privacy",
		"purpose_summary":"Send updates about this feedback.",
		"lawful_basis_summary":"Consent for optional status updates.",
		"retention_summary":"Retained until withdrawal or the approved case period."
	}`
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks
		SET reporter_consent_notice_version = '2026-07-controller-v1',
		    reporter_consent_notice_snapshot = $2::jsonb
		WHERE id = $1
	`, feedbackA, controllerNoticeSnapshot); err != nil {
		t.Fatalf("apply contextual controller notice evidence: %v", err)
	}
	if _, err := root.Exec(ctx, `
		UPDATE issues
		SET reporter_consent_notice_version = '2026-07-controller-v1'
		WHERE id = $1
	`, issueA); err == nil {
		t.Fatal("contextual controller notice without snapshot was accepted")
	}
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks
		SET reporter_consent_notice_snapshot =
		    reporter_consent_notice_snapshot || '{"unexpected":"content"}'::jsonb
		WHERE id = $1
	`, feedbackA); err == nil {
		t.Fatal("contextual controller notice with an extra snapshot field was accepted")
	}
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks
		SET reporter_consent_notice_version = '2026-07',
		    reporter_consent_notice_snapshot = NULL
		WHERE id = $1
	`, feedbackA); err != nil {
		t.Fatalf("restore legacy consent evidence before rollback replay: %v", err)
	}

	var retentionHealthRows int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM retention_maintenance_status
	`).Scan(&retentionHealthRows); err != nil || retentionHealthRows != 11 {
		t.Fatalf(
			"retention health row count = %d, error = %v, want 11",
			retentionHealthRows, err,
		)
	}
	retentionHealthSeeds := map[string]struct {
		category string
		interval int
	}{
		"tenant_magic_links":         {category: "auth_magic_links", interval: 3600},
		"platform_admin_magic_links": {category: "auth_magic_links", interval: 3600},
		"attachments":                {category: "attachments", interval: 86400},
		"privacy_export_archives":    {category: "export_archives", interval: 86400},
		"reporter_privacy_requests":  {category: "privacy_rights", interval: 86400},
		"replay_sessions":            {category: "session_replays", interval: 86400},
		"widget_funnel_events":       {category: "widget_events", interval: 3600},
		"widget_funnel_daily":        {category: "widget_daily", interval: 3600},
		"abuse_diagnostics":          {category: "abuse_diagnostics", interval: 3600},
		"ai_budget_ledgers":          {category: "ai_processing", interval: 86400},
		"identity_asset_deletions":   {category: "identity_assets", interval: 60},
	}
	retentionSeedRows, err := root.Query(ctx, `
		SELECT operation_key, category, expected_interval_seconds,
		       alert_grace_seconds, last_result IS NULL
		FROM retention_maintenance_status
	`)
	if err != nil {
		t.Fatalf("list retention health seeds: %v", err)
	}
	for retentionSeedRows.Next() {
		var operationKey, category string
		var interval, grace int
		var unobserved bool
		if err := retentionSeedRows.Scan(
			&operationKey, &category, &interval, &grace, &unobserved,
		); err != nil {
			t.Fatalf("scan retention health seed: %v", err)
		}
		want, known := retentionHealthSeeds[operationKey]
		if !known || category != want.category || interval != want.interval ||
			grace != 900 || !unobserved {
			t.Fatalf(
				"retention health seed %s = category %s interval %d grace %d unobserved %t",
				operationKey, category, interval, grace, unobserved,
			)
		}
	}
	if err := retentionSeedRows.Err(); err != nil {
		t.Fatalf("iterate retention health seeds: %v", err)
	}
	retentionSeedRows.Close()
	var appRetentionSelect, appRetentionUpdate, appRetentionDelete bool
	var adminRetentionSelect, adminRetentionResultUpdate, adminRetentionKeyUpdate bool
	var adminRetentionInsert, adminRetentionDelete bool
	if err := root.QueryRow(ctx, `
		SELECT
			has_table_privilege(
				'fidibeki_app', 'retention_maintenance_status', 'SELECT'
			),
			has_table_privilege(
				'fidibeki_app', 'retention_maintenance_status', 'UPDATE'
			),
			has_table_privilege(
				'fidibeki_app', 'retention_maintenance_status', 'DELETE'
			),
			has_table_privilege(
				'fidibeki_admin', 'retention_maintenance_status', 'SELECT'
			),
			has_column_privilege(
				'fidibeki_admin', 'retention_maintenance_status', 'last_result', 'UPDATE'
			),
			has_column_privilege(
				'fidibeki_admin', 'retention_maintenance_status', 'operation_key', 'UPDATE'
			),
			has_table_privilege(
				'fidibeki_admin', 'retention_maintenance_status', 'INSERT'
			),
			has_table_privilege(
				'fidibeki_admin', 'retention_maintenance_status', 'DELETE'
			)
	`).Scan(
		&appRetentionSelect, &appRetentionUpdate, &appRetentionDelete,
		&adminRetentionSelect, &adminRetentionResultUpdate, &adminRetentionKeyUpdate,
		&adminRetentionInsert, &adminRetentionDelete,
	); err != nil {
		t.Fatalf("read retention health privileges: %v", err)
	}
	if !appRetentionSelect || appRetentionUpdate || appRetentionDelete ||
		!adminRetentionSelect || !adminRetentionResultUpdate || adminRetentionKeyUpdate ||
		adminRetentionInsert || adminRetentionDelete {
		t.Fatalf(
			"retention health privileges app select/update/delete=%t/%t/%t "+
				"admin select/result-update/key-update/insert/delete=%t/%t/%t/%t/%t",
			appRetentionSelect, appRetentionUpdate, appRetentionDelete,
			adminRetentionSelect, adminRetentionResultUpdate, adminRetentionKeyUpdate,
			adminRetentionInsert, adminRetentionDelete,
		)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO retention_maintenance_status (
			operation_key, category, expected_interval_seconds, alert_grace_seconds
		) VALUES ('arbitrary_cleaner', 'auth_magic_links', 3600, 900)
	`); err == nil {
		t.Fatal("arbitrary retention health operation was accepted")
	}
	if _, err := root.Exec(ctx, `
		UPDATE retention_maintenance_status
		SET last_attempt_at = now(),
		    last_result = 'failed',
		    consecutive_failures = 1,
		    total_runs = 1,
		    total_failures = 1,
		    failure_code = 'database unavailable',
		    updated_at = now()
		WHERE operation_key = 'tenant_magic_links'
	`); err == nil {
		t.Fatal("raw retention health failure text was accepted")
	}

	if err := migrateToVersion(migrator, 108); err != nil {
		t.Fatalf("migrate down to 108: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM retention_maintenance_status
	`).Scan(&retentionHealthRows); err != nil || retentionHealthRows != 2 {
		t.Fatalf(
			"retention health rows at version 108 = %d, error = %v, want 2",
			retentionHealthRows, err,
		)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO retention_maintenance_status (
			operation_key, category, expected_interval_seconds, alert_grace_seconds
		) VALUES ('attachments', 'attachments', 86400, 900)
	`); err == nil {
		t.Fatal("109 operation was accepted after rolling back to 108")
	}
	if err := migrateToVersion(migrator, latestMigrationVersion); err != nil {
		t.Fatalf("migrate back to latest: %v", err)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM retention_maintenance_status
	`).Scan(&retentionHealthRows); err != nil || retentionHealthRows != 11 {
		t.Fatalf(
			"retention health rows after 108 replay = %d, error = %v, want 11",
			retentionHealthRows, err,
		)
	}

	pools.Close()
	if err := migrator.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up after rollback: %v", err)
	}
	version, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("read final migration version: %v", err)
	}
	if version != latestMigrationVersion || dirty {
		t.Fatalf("migration state = version %d dirty %t, want version %d clean", version, dirty, latestMigrationVersion)
	}
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM retention_maintenance_status
	`).Scan(&retentionHealthRows); err != nil || retentionHealthRows != 11 {
		t.Fatalf(
			"retention health rows after down/up = %d, error = %v, want 11",
			retentionHealthRows, err,
		)
	}
	var analyticsIndexCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND indexname IN (
			'idx_widget_funnel_daily_tenant_rule_date',
			'idx_widget_funnel_daily_tenant_page_date',
			'idx_widget_funnel_events_project_page_occurred',
			'idx_widget_funnel_daily_retention'
		  )
	`).Scan(&analyticsIndexCount); err != nil || analyticsIndexCount != 4 {
		t.Fatalf("widget funnel analytics index count = %d, error = %v, want 4", analyticsIndexCount, err)
	}
}

func migrateToVersion(migrator *db.Migrator, target uint) error {
	_, dirty, err := migrator.Version()
	if err != nil {
		return err
	}
	if dirty {
		return errors.New("cannot migrate a dirty database")
	}
	return migrator.Migrate(target)
}

func migrationsPath(t *testing.T) string {
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

func roleURL(t *testing.T, directURL, username, password string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}

type tenantContextSetter interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func setTenantContext(t *testing.T, ctx context.Context, tx tenantContextSetter, tenantID string) {
	t.Helper()
	var configured string
	if err := tx.QueryRow(ctx,
		`SELECT set_config('app.tenant_id', $1, true)`, tenantID,
	).Scan(&configured); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}
	if configured != tenantID {
		t.Fatalf("configured tenant = %q, want %q", configured, tenantID)
	}
}
