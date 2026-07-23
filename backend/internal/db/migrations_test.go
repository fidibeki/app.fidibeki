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
	const latestMigrationVersion = 63

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
	if err := migrator.Steps(-4); err != nil {
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
	var definitionKey, definitionStatus, presentation, objective, defaultLocale string
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
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM ai_gemini_files`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin Gemini file count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM privacy_jobs`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin privacy job count = %d, error = %v, want 2", count, err)
	}
	if err := adminPool.QueryRow(ctx, `SELECT count(*) FROM reporter_privacy_requests`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("admin reporter privacy request count = %d, error = %v, want 2", count, err)
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
		       localizations->'en'->>'prompt',
		       widget_appearance_overrides->>'position',
		       widget_appearance_overrides->>'theme'
		FROM widget_rules WHERE id = $1
	`, widgetRuleA).Scan(
		&definitionKey, &definitionVersion, &definitionStatus, &presentation, &localizedPrompt,
		&appearancePosition, &appearanceTheme,
	); err != nil {
		t.Fatalf("read backfilled widget definition: %v", err)
	}
	if definitionKey == "" || definitionVersion != 1 || definitionStatus != "paused" ||
		presentation != "prompt" || localizedPrompt != "Legacy localized prompt" ||
		appearancePosition == "" || appearanceTheme == "" {
		t.Fatalf(
			"backfilled widget definition = %q/%d/%q/%q/%q appearance %q/%q",
			definitionKey, definitionVersion, definitionStatus, presentation, localizedPrompt,
			appearancePosition, appearanceTheme,
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
	version, dirty, err := migrator.Version()
	if err != nil {
		return err
	}
	if dirty {
		return errors.New("cannot migrate a dirty database")
	}
	return migrator.Steps(int(target) - int(version))
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
