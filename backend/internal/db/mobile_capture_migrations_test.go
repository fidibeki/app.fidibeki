package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMobileCaptureMigrationPreservesLegacyDataAndPermissionBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki_mobile_migration"),
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
	if err := migrator.Migrate(129); err != nil {
		t.Fatalf("migrate to pre-mobile version: %v", err)
	}
	appURL := roleURL(t, directURL, "fidibeki_app", "mobile-app-role-password")
	adminURL := roleURL(t, directURL, "fidibeki_admin", "mobile-admin-role-password")
	if err := db.ConfigureRuntimeRoles(ctx, directURL, appURL, adminURL); err != nil {
		t.Fatalf("configure runtime roles: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect migration user: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })

	var tenantA, tenantB string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Mobile A', 'mobile-a', 'scale', 'active') RETURNING id
	`).Scan(&tenantA); err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Mobile B', 'mobile-b', 'scale', 'active') RETURNING id
	`).Scan(&tenantB); err != nil {
		t.Fatalf("create tenant B: %v", err)
	}
	prerequisites := []string{
		"api_key:create", "project:read", "widget_rule:read", "member:read", "tenant:read",
	}
	var ownerID, adminID, limitedID, inactiveID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, email, permissions, status, authorization_version
		) VALUES ($1, 'owner@mobile.test', $2, 'active', 4) RETURNING id
	`, tenantA, prerequisites).Scan(&ownerID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := root.Exec(ctx, `UPDATE tenants SET owner_member_id = $2 WHERE id = $1`, tenantA, ownerID); err != nil {
		t.Fatalf("assign owner: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, email, permissions, status, authorization_version
		) VALUES ($1, 'admin@mobile.test', $2, 'active', 7) RETURNING id
	`, tenantA, prerequisites).Scan(&adminID); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, email, permissions, status, authorization_version
		) VALUES (
			$1, 'limited@mobile.test',
			ARRAY['api_key:create', 'project:read', 'widget_rule:read'],
			'active', 2
		) RETURNING id
	`, tenantA).Scan(&limitedID); err != nil {
		t.Fatalf("create limited member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, email, permissions, status, authorization_version
		) VALUES ($1, 'inactive@mobile.test', $2, 'deactivated', 9) RETURNING id
	`, tenantA, prerequisites).Scan(&inactiveID); err != nil {
		t.Fatalf("create inactive admin: %v", err)
	}

	var projectA, projectB, ruleA string
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'App A', 'pk_live_0123456789ABCDEFGHIJKL', ARRAY['app.example.com'])
		RETURNING id
	`, tenantA).Scan(&projectA); err != nil {
		t.Fatalf("create project A: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'App B', 'pk_live_ABCDEFGHIJKL0123456789', ARRAY['other.example.com'])
		RETURNING id
	`, tenantB).Scan(&projectB); err != nil {
		t.Fatalf("create project B: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, matcher_value, trigger_type,
			trigger_delay_seconds, definition_key, presentation
		) VALUES (
			$1, 'Mobile launcher', 'all_pages', NULL, 'delay', 0,
			'mobile-launcher', 'launcher'
		) RETURNING id
	`, projectA).Scan(&ruleA); err != nil {
		t.Fatalf("create widget definition: %v", err)
	}
	var legacyFeedbackID string
	if err := root.QueryRow(ctx, `
		INSERT INTO feedbacks (
			tenant_id, project_id, widget_rule_id, public_id, client_feedback_id,
			title, description, text, responses, page_url
		) VALUES (
			$1, $2, $3, 'fb_MOBILE01', gen_random_uuid(),
			'Legacy', 'Legacy', 'Legacy', '{}'::jsonb, 'https://app.example.com/checkout'
		) RETURNING id
	`, tenantA, projectA, ruleA).Scan(&legacyFeedbackID); err != nil {
		t.Fatalf("create legacy feedback: %v", err)
	}

	if err := migrator.Steps(1); err != nil {
		t.Fatalf("apply mobile capture migration: %v", err)
	}
	var source, pageURL string
	if err := root.QueryRow(ctx, `
		SELECT source::text, page_url FROM feedbacks WHERE id = $1
	`, legacyFeedbackID).Scan(&source, &pageURL); err != nil {
		t.Fatalf("read migrated feedback: %v", err)
	}
	if source != "web" || pageURL != "https://app.example.com/checkout" {
		t.Fatalf("migrated feedback = source %q page %q", source, pageURL)
	}

	for _, member := range []struct {
		id          string
		wantGrant   bool
		wantVersion int
	}{
		{id: ownerID, wantGrant: true, wantVersion: 5},
		{id: adminID, wantGrant: true, wantVersion: 8},
		{id: limitedID, wantGrant: false, wantVersion: 2},
		{id: inactiveID, wantGrant: false, wantVersion: 9},
	} {
		var granted bool
		var version int
		if err := root.QueryRow(ctx, `
			SELECT 'mobile_capture:create' = ANY(permissions), authorization_version
			FROM tenant_members WHERE id = $1
		`, member.id).Scan(&granted, &version); err != nil {
			t.Fatalf("read member permission: %v", err)
		}
		if granted != member.wantGrant || version != member.wantVersion {
			t.Fatalf(
				"member %s grant/version = %t/%d, want %t/%d",
				member.id, granted, version, member.wantGrant, member.wantVersion,
			)
		}
	}

	if _, err := root.Exec(ctx, `
		UPDATE feedbacks
		SET source = 'ios', page_url = '', mobile_application_id = NULL,
		    mobile_screen = '/checkout'
		WHERE id = $1
	`, legacyFeedbackID); err == nil {
		t.Fatal("mobile feedback without application ID passed storage constraint")
	}
	if _, err := root.Exec(ctx, `
		UPDATE feedbacks SET source = 'web', mobile_screen = '/checkout' WHERE id = $1
	`, legacyFeedbackID); err == nil {
		t.Fatal("web feedback with mobile context passed storage constraint")
	}

	var apiKeyID, crossTenantAPIKeyID string
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES (
			$1, 'Mobile capture',
			encode(digest('mobile-migration-api-key', 'sha256'), 'hex'),
			'fk_live_mobile', ARRAY['mobile_capture:create']
		) RETURNING id
	`, tenantA).Scan(&apiKeyID); err != nil {
		t.Fatalf("create issuer API key: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO api_keys (
			tenant_id, name, token_hash, token_prefix, permissions
		) VALUES (
			$1, 'Cross-tenant mobile capture',
			encode(digest('cross-tenant-mobile-migration-api-key', 'sha256'), 'hex'),
			'fk_live_cross', ARRAY['mobile_capture:create']
		) RETURNING id
	`, tenantB).Scan(&crossTenantAPIKeyID); err != nil {
		t.Fatalf("create cross-tenant issuer API key: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO mobile_capture_sessions (
			tenant_id, project_id, widget_rule_id, issuer_api_key_id, token_hash,
			platform, application_id, screen, display_locale,
			definition_version, controller_profile_version,
			submit_expires_at, cleanup_after
		) VALUES (
			$1, $2, $3, $4, $5, 'ios', 'com.example.app', '/checkout',
			'en', 1, 0, now() + interval '10 minutes', now() + interval '24 hours'
		)
	`, tenantA, projectA, ruleA, crossTenantAPIKeyID, strings.Repeat("c", 64)); err == nil {
		t.Fatal("cross-tenant issuer API key passed capture-session constraint")
	}
	var captureID string
	tokenHash := strings.Repeat("a", 64)
	if err := root.QueryRow(ctx, `
		INSERT INTO mobile_capture_sessions (
			tenant_id, project_id, widget_rule_id, issuer_api_key_id, token_hash,
			platform, application_id, screen, display_locale,
			definition_version, controller_profile_version,
			submit_expires_at, cleanup_after
		) VALUES (
			$1, $2, $3, $4, $5, 'ios', 'com.example.app', '/checkout',
			'en', 1, 0, now() + interval '10 minutes', now() + interval '24 hours'
		) RETURNING id
	`, tenantA, projectA, ruleA, apiKeyID, tokenHash).Scan(&captureID); err != nil {
		t.Fatalf("create capture session: %v", err)
	}
	if _, err := root.Exec(ctx, `
		INSERT INTO mobile_capture_sessions (
			tenant_id, project_id, widget_rule_id, issuer_api_key_id, token_hash,
			platform, application_id, screen, display_locale,
			definition_version, controller_profile_version,
			submit_expires_at, cleanup_after
		) VALUES (
			$1, $2, $3, $4, 'fmc_raw_secret', 'ios', 'com.example.app',
			'/checkout', 'en', 1, 0, now() + interval '10 minutes',
			now() + interval '24 hours'
		)
	`, tenantA, projectA, ruleA, apiKeyID); err == nil {
		t.Fatal("raw capture token passed hash constraint")
	}

	app, err := pgx.Connect(ctx, appURL)
	if err != nil {
		t.Fatalf("connect app role: %v", err)
	}
	t.Cleanup(func() { _ = app.Close(ctx) })
	for _, visibility := range []struct {
		tenantID string
		want     int
	}{
		{tenantID: tenantA, want: 1},
		{tenantID: tenantB, want: 0},
	} {
		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatalf("begin RLS transaction: %v", err)
		}
		setTenantContext(t, ctx, tx, visibility.tenantID)
		var count int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM mobile_capture_sessions WHERE id = $1
		`, captureID).Scan(&count); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("read RLS-scoped capture session: %v", err)
		}
		if count != visibility.want {
			_ = tx.Rollback(ctx)
			t.Fatalf("tenant %s visibility = %d, want %d", visibility.tenantID, count, visibility.want)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatalf("roll back RLS transaction: %v", err)
		}
	}
	var enabled, forced bool
	if err := root.QueryRow(ctx, `
		SELECT relrowsecurity, relforcerowsecurity
		FROM pg_class WHERE oid = 'mobile_capture_sessions'::regclass
	`).Scan(&enabled, &forced); err != nil || !enabled || !forced {
		t.Fatalf("mobile capture RLS = enabled %t forced %t, error %v", enabled, forced, err)
	}

	if _, err := root.Exec(ctx, `DELETE FROM mobile_capture_sessions WHERE id = $1`, captureID); err != nil {
		t.Fatalf("delete capture fixture: %v", err)
	}
	if err := migrator.Steps(-1); err != nil {
		t.Fatalf("roll back mobile capture migration: %v", err)
	}
	if err := migrator.Steps(1); err != nil {
		t.Fatalf("reapply mobile capture migration: %v", err)
	}
	var version uint
	var dirty bool
	version, dirty, err = migrator.Version()
	if err != nil || version != 130 || dirty {
		t.Fatalf("migration state = %d/%t, error %v", version, dirty, err)
	}
}
