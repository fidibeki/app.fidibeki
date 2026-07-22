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
	var projectA, projectB, issueA, issueB, feedbackA, feedbackB, attachmentA, attachmentB string
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
			FROM tenant_members WHERE tenant_id = $1 LIMIT 1
			RETURNING id
		`, tenantID, issueID).Scan(&updateID); err != nil {
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
			content_type, size_bytes, processing_status, completed_at
		) VALUES ($1, $2, 'screenshot', gen_random_uuid(), 'rls/a.png',
		          'image/png', 1, 'skipped', now()) RETURNING id
	`, tenantA, feedbackA).Scan(&attachmentA); err != nil {
		t.Fatalf("create tenant A attachment: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO attachments (
			tenant_id, feedback_id, kind, client_attachment_id, storage_key,
			content_type, size_bytes, processing_status, completed_at
		) VALUES ($1, $2, 'screenshot', gen_random_uuid(), 'rls/b.png',
		          'image/png', 1, 'skipped', now()) RETURNING id
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
		INSERT INTO ai_gemini_files (tenant_id, attachment_id, file_name, delete_after)
		VALUES ($1, $2, 'files/rls-a', now()), ($3, $4, 'files/rls-b', now())
	`, tenantA, attachmentA, tenantB, attachmentB); err != nil {
		t.Fatalf("seed Gemini file ledger: %v", err)
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

	tx, err := appPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tenant A transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	setTenantContext(t, ctx, tx, tenantA)
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenant_members`).Scan(&count); err != nil {
		t.Fatalf("query tenant A members: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant A member count = %d, want 1", count)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_daily_usage`).Scan(&count); err != nil {
		t.Fatalf("query tenant A AI usage: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant A AI usage count = %d, want 1", count)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_gemini_files`).Scan(&count); err != nil {
		t.Fatalf("query tenant A Gemini files: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant A Gemini file count = %d, want 1", count)
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
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM tenant_members WHERE tenant_id = $1`, tenantB,
	).Scan(&count); err != nil {
		t.Fatalf("query tenant B through tenant A transaction: %v", err)
	}
	if count != 0 {
		t.Fatalf("tenant A transaction read %d tenant B rows, want 0", count)
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
	if count != 2 {
		t.Fatalf("admin member count = %d, want 2", count)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM ai_daily_usage`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin AI usage count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM ai_gemini_files`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin Gemini file count = %d, error = %v, want 2", count, err)
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
	if version != 40 || dirty {
		t.Fatalf("migration state = version %d dirty %t, want version 40 clean", version, dirty)
	}
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
