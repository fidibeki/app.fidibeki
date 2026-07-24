package widgetanalytics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/acidiney/app.fidibeki/backend/internal/widgetapi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestWidgetAnalyticsConsentHTTPFlow proves the widget_analytics purpose is independently
// gated end to end: grant, ingest, revoke, expiry, and a rule with no configured consent mode
// all reach the same backend enforcement covered for session_replay, but through a fully
// separate ledger/table/HTTP surface (issue #58).
func TestWidgetAnalyticsConsentHTTPFlow(t *testing.T) {
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
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, analyticsMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}

	var tenantID, projectID, consentedRuleID, dormantRuleID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	publicKey := "pk_live_ANALYTICSCONSENTABCDEF"
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', $2, '{example.com}') RETURNING id
	`, tenantID, publicKey).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds, analytics_consent
		) VALUES (
			$1, 'Checkout', 'all_pages', 'delay', 0,
			'{"mode":"customer_cmp","notice_version":"2026-07","supported_locales":["en"],"expiry_days":30}'::jsonb
		)
		RETURNING id
	`, projectID).Scan(&consentedRuleID); err != nil {
		t.Fatalf("create analytics-enabled widget rule: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (project_id, name, matcher_type, trigger_type, trigger_delay_seconds)
		VALUES ($1, 'Dormant', 'all_pages', 'delay', 0)
		RETURNING id
	`, projectID).Scan(&dormantRuleID); err != nil {
		t.Fatalf("create analytics-dormant widget rule: %v", err)
	}

	pools, err := db.OpenPools(ctx, analyticsRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	authorizer := widgetapi.Authorizer{Store: project.Store{}, Database: pools.Admin, Now: func() time.Time { return now }}
	router := chi.NewRouter()
	RegisterPublicRoutes(router, PublicHTTPOptions{
		Authorizer: authorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Consent: ConsentStore{}, Limiter: noopAnalyticsLimiter{},
		Now: func() time.Time { return now },
	})

	preflight := analyticsConsentRequest(router, http.MethodOptions,
		"/v1/widget/analytics-consents?key="+publicKey, "", "", "https://example.com")
	if preflight.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d", preflight.Code)
	}

	// A rule with no configured analytics_consent mode stays dormant: ingestion is rejected
	// even with a well-formed token, and no grant can ever be recorded for it.
	dormantIngest := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/events?key="+publicKey,
		ingestBodyFor(dormantRuleID, now, testAnalyticsConsentToken), publicKey, "https://example.com")
	if dormantIngest.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(dormantIngest.Body.String(), `"field":"events[0].rule_id"`) {
		t.Fatalf("dormant rule ingest = %d/%s", dormantIngest.Code, dormantIngest.Body.String())
	}
	dormantGrantBody := fmt.Sprintf(
		`{"widget_rule_id":%q,"purpose":"widget_analytics","notice_version":"2026-07",`+
			`"locale":"en","source":"customer_cmp","consent_id":"cmp-ref","state":"granted","grant_token":%q}`,
		dormantRuleID, testAnalyticsConsentToken,
	)
	dormantGrant := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/analytics-consents?key="+publicKey,
		dormantGrantBody, publicKey, "https://example.com")
	if dormantGrant.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(dormantGrant.Body.String(), `"field":"widget_rule_id"`) {
		t.Fatalf("dormant rule grant = %d/%s", dormantGrant.Code, dormantGrant.Body.String())
	}

	// Ingesting analytics events before any grant is rejected on the analytics-enabled rule too.
	beforeGrant := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/events?key="+publicKey,
		ingestBodyFor(consentedRuleID, now, testAnalyticsConsentToken), publicKey, "https://example.com")
	if beforeGrant.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(beforeGrant.Body.String(), `"field":"events[0].consent_token"`) {
		t.Fatalf("ingest before grant = %d/%s", beforeGrant.Code, beforeGrant.Body.String())
	}
	var eventsBeforeGrant int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_events WHERE project_id = $1
	`, projectID).Scan(&eventsBeforeGrant); err != nil || eventsBeforeGrant != 0 {
		t.Fatalf("events before grant = %d, error = %v, want 0", eventsBeforeGrant, err)
	}

	grantBody := fmt.Sprintf(
		`{"widget_rule_id":%q,"purpose":"widget_analytics","notice_version":"2026-07",`+
			`"locale":"en","source":"customer_cmp","consent_id":"cmp-ref-1","state":"granted","grant_token":%q}`,
		consentedRuleID, testAnalyticsConsentToken,
	)
	grant := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/analytics-consents?key="+publicKey,
		grantBody, publicKey, "https://example.com")
	if grant.Code != http.StatusCreated {
		t.Fatalf("grant analytics consent = %d/%s", grant.Code, grant.Body.String())
	}
	var grantResponse struct {
		Token     string    `json:"token"`
		State     string    `json:"state"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	decodeAnalyticsConsentResponse(t, grant, &grantResponse)
	if grantResponse.Token != testAnalyticsConsentToken || grantResponse.State != "active" ||
		!grantResponse.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("grant response = %#v", grantResponse)
	}
	var storedColumns []string
	rows, err := root.Query(ctx, `
		SELECT column_name FROM information_schema.columns WHERE table_name = 'widget_analytics_consents'
	`)
	if err != nil {
		t.Fatalf("inspect widget_analytics_consents columns: %v", err)
	}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan widget_analytics_consents column: %v", err)
		}
		storedColumns = append(storedColumns, column)
	}
	rows.Close()
	for _, forbidden := range []string{"ip_address", "user_agent", "email", "page_url", "visitor_id"} {
		for _, column := range storedColumns {
			if column == forbidden {
				t.Fatalf("widget_analytics_consents unexpectedly stores %q", forbidden)
			}
		}
	}

	// The now-active grant authorizes ingestion for its rule.
	accepted := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/events?key="+publicKey,
		ingestBodyFor(consentedRuleID, now, grantResponse.Token), publicKey, "https://example.com")
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("ingest after grant = %d/%s", accepted.Code, accepted.Body.String())
	}
	var eventsAfterGrant int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_events WHERE project_id = $1
	`, projectID).Scan(&eventsAfterGrant); err != nil || eventsAfterGrant != 1 {
		t.Fatalf("events after grant = %d, error = %v, want 1", eventsAfterGrant, err)
	}

	// A replay-purpose grant must never authorize analytics ingestion (independent purposes).
	var foreignPurposeCount int
	if err := root.QueryRow(ctx, `
		SELECT count(*) FROM widget_analytics_consents WHERE purpose <> 'widget_analytics'
	`).Scan(&foreignPurposeCount); err != nil || foreignPurposeCount != 0 {
		t.Fatalf("widget_analytics_consents foreign purpose rows = %d, error = %v", foreignPurposeCount, err)
	}

	// Revocation stops future ingestion immediately.
	revoke := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/analytics-consents/revoke?key="+publicKey,
		fmt.Sprintf(`{"token":%q}`, grantResponse.Token), publicKey, "https://example.com")
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke analytics consent = %d/%s", revoke.Code, revoke.Body.String())
	}
	afterRevoke := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/events?key="+publicKey,
		ingestBodyFor(consentedRuleID, now, grantResponse.Token), publicKey, "https://example.com")
	if afterRevoke.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(afterRevoke.Body.String(), `"field":"events[0].consent_token"`) {
		t.Fatalf("ingest after revoke = %d/%s", afterRevoke.Code, afterRevoke.Body.String())
	}
	var revokedState string
	if err := root.QueryRow(ctx, `
		SELECT state::text FROM widget_analytics_consents WHERE token_hash = $1
	`, auth.HashToken(grantResponse.Token)).Scan(&revokedState); err != nil || revokedState != "withdrawn" {
		t.Fatalf("revoked consent state = %q, error = %v, want withdrawn", revokedState, err)
	}
	idempotentRevoke := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/analytics-consents/revoke?key="+publicKey,
		fmt.Sprintf(`{"token":%q}`, grantResponse.Token), publicKey, "https://example.com")
	if idempotentRevoke.Code != http.StatusNoContent {
		t.Fatalf("idempotent revoke = %d/%s", idempotentRevoke.Code, idempotentRevoke.Body.String())
	}

	// Expiry: a fresh grant that is authorized once, then queried again after its expiry window,
	// transitions to expired and stops authorizing further ingestion.
	expiringToken := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	expiringGrantBody := fmt.Sprintf(
		`{"widget_rule_id":%q,"purpose":"widget_analytics","notice_version":"2026-07",`+
			`"locale":"en","source":"customer_cmp","consent_id":"cmp-ref-2","state":"granted","grant_token":%q}`,
		consentedRuleID, expiringToken,
	)
	expiringGrant := analyticsConsentRequest(router, http.MethodPost, "/v1/widget/analytics-consents?key="+publicKey,
		expiringGrantBody, publicKey, "https://example.com")
	if expiringGrant.Code != http.StatusCreated {
		t.Fatalf("grant expiring analytics consent = %d/%s", expiringGrant.Code, expiringGrant.Body.String())
	}
	afterExpiry := now.Add(31 * 24 * time.Hour)
	expiredAuthorizer := widgetapi.Authorizer{Store: project.Store{}, Database: pools.Admin, Now: func() time.Time { return afterExpiry }}
	expiredRouter := chi.NewRouter()
	RegisterPublicRoutes(expiredRouter, PublicHTTPOptions{
		Authorizer: expiredAuthorizer, Begin: db.BeginFromPool(pools.App), Store: Store{},
		Consent: ConsentStore{}, Limiter: noopAnalyticsLimiter{},
		Now: func() time.Time { return afterExpiry },
	})
	expiredIngest := analyticsConsentRequest(expiredRouter, http.MethodPost, "/v1/widget/events?key="+publicKey,
		ingestBodyFor(consentedRuleID, afterExpiry, expiringToken), publicKey, "https://example.com")
	if expiredIngest.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(expiredIngest.Body.String(), `"field":"events[0].consent_token"`) {
		t.Fatalf("ingest after expiry = %d/%s", expiredIngest.Code, expiredIngest.Body.String())
	}
	var expiredState string
	if err := root.QueryRow(ctx, `
		SELECT state::text FROM widget_analytics_consents WHERE token_hash = $1
	`, auth.HashToken(expiringToken)).Scan(&expiredState); err != nil || expiredState != "expired" {
		t.Fatalf("expired consent state = %q, error = %v, want expired", expiredState, err)
	}
}

func ingestBodyFor(ruleID string, occurredAt time.Time, consentToken string) string {
	return fmt.Sprintf(`{"events":[{`+
		`"event_id":"00000000-0000-4000-8000-000000000001",`+
		`"event_type":"matched",`+
		`"visitor_id":"00000000-0000-4000-8000-000000000002",`+
		`"page_view_id":"00000000-0000-4000-8000-000000000003",`+
		`"rule_id":%q,`+
		`"page_url":"https://example.com/checkout",`+
		`"occurred_at":%q,`+
		`"consent_token":%q}]}`,
		ruleID, occurredAt.Format(time.RFC3339), consentToken)
}

func analyticsConsentRequest(handler http.Handler, method, path, body, publicKey, origin string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://widget.localhost"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	if publicKey != "" {
		request.Header.Set(widgetapi.PublicKeyHeader, publicKey)
	}
	handler.ServeHTTP(response, request)
	return response
}

func decodeAnalyticsConsentResponse(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode response: %v, body = %s", err, response.Body.String())
	}
}

type noopAnalyticsLimiter struct{}

func (noopAnalyticsLimiter) Allow(context.Context, string, ratelimit.Window) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: true, Limit: 300, Remaining: 300}, nil
}

// TestWidgetAnalyticsConsentDiagnosticAggregatesSafelyAndEnforcesTenantScope proves the tenant-safe
// backoffice diagnostic for issue #58: it reports the rule's current consent configuration plus
// aggregate active/recent/expiry-health counts scoped to the current notice contract, excludes
// stale-notice and other-rule evidence, never leaks a token/consent reference, enforces
// widget_rule:read RBAC, and stays tenant-isolated.
func TestWidgetAnalyticsConsentDiagnosticAggregatesSafelyAndEnforcesTenantScope(t *testing.T) {
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
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, analyticsMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	root, err := pgx.Connect(ctx, directURL)
	if err != nil {
		t.Fatalf("connect as root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close(ctx) })
	if _, err := root.Exec(ctx, `ALTER ROLE fidibeki_app LOGIN PASSWORD 'app-password'`); err != nil {
		t.Fatalf("enable app login: %v", err)
	}

	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	var tenantID, projectID, ruleID, otherRuleID, otherTenantID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Beta', 'beta', 'growth', 'active') RETURNING id
	`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_DIAGNOSTICABCDEFGHIJKL', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds, analytics_consent
		) VALUES (
			$1, 'Checkout', 'all_pages', 'delay', 0,
			'{"mode":"customer_cmp","notice_version":"2026-07","supported_locales":["en"],"expiry_days":30}'::jsonb
		)
		RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create analytics-enabled widget rule: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (
			project_id, name, matcher_type, trigger_type, trigger_delay_seconds, analytics_consent
		) VALUES (
			$1, 'Pricing', 'all_pages', 'delay', 0,
			'{"mode":"customer_cmp","notice_version":"2026-07","supported_locales":["en"],"expiry_days":30}'::jsonb
		)
		RETURNING id
	`, projectID).Scan(&otherRuleID); err != nil {
		t.Fatalf("create second analytics-enabled widget rule: %v", err)
	}
	var dormantRuleID string
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (project_id, name, matcher_type, trigger_type, trigger_delay_seconds)
		VALUES ($1, 'Dormant', 'all_pages', 'delay', 0) RETURNING id
	`, projectID).Scan(&dormantRuleID); err != nil {
		t.Fatalf("create dormant widget rule: %v", err)
	}

	insertConsent := func(widgetRuleID, noticeVersion, state, tokenHash string, customerReference *string,
		createdAt, acceptedAt, withdrawnAt, expiresAt, updatedAt *time.Time,
	) {
		t.Helper()
		if _, err := root.Exec(ctx, `
			INSERT INTO widget_analytics_consents (
				tenant_id, project_id, widget_rule_id, purpose, notice_version, locale, source,
				customer_consent_reference, token_hash, state, accepted_at, withdrawn_at, expires_at,
				created_at, updated_at
			) VALUES (
				$1, $2, $3, 'widget_analytics', $4, 'en', 'customer_cmp', $5, $6, $7, $8, $9, $10, $11, $12
			)
		`, tenantID, projectID, widgetRuleID, noticeVersion, customerReference, nullableConsentString(tokenHash),
			state, acceptedAt, withdrawnAt, expiresAt, createdAt, updatedAt); err != nil {
			t.Fatalf("seed widget analytics consent (state=%s): %v", state, err)
		}
	}
	ref := func(value string) *string { return &value }
	at := func(offset time.Duration) *time.Time { d := now.Add(offset); return &d }

	// Current-cohort evidence for ruleID (notice_version=2026-07, source=customer_cmp): two live
	// active grants (one expiring soon), one technically-expired active row, one denied, one
	// withdrawn, and one explicitly expired row.
	insertConsent(ruleID, "2026-07", "active", strings.Repeat("1", 64), ref("cmp-ref-active-fresh"),
		at(-2*24*time.Hour), at(-2*24*time.Hour), nil, at(40*24*time.Hour), at(-24*time.Hour))
	insertConsent(ruleID, "2026-07", "active", strings.Repeat("2", 64), ref("cmp-ref-active-soon"),
		at(-20*24*time.Hour), at(-20*24*time.Hour), nil, at(3*24*time.Hour), at(-20*24*time.Hour))
	insertConsent(ruleID, "2026-07", "active", strings.Repeat("3", 64), ref("cmp-ref-active-stale"),
		at(-100*24*time.Hour), at(-100*24*time.Hour), nil, at(-1*24*time.Hour), at(-100*24*time.Hour))
	insertConsent(ruleID, "2026-07", "denied", "", nil,
		at(-5*24*time.Hour), nil, nil, at(25*24*time.Hour), at(-5*24*time.Hour))
	insertConsent(ruleID, "2026-07", "withdrawn", strings.Repeat("5", 64), ref("cmp-ref-withdrawn"),
		at(-50*24*time.Hour), at(-50*24*time.Hour), at(-2*24*time.Hour), at(10*24*time.Hour), at(-2*24*time.Hour))
	insertConsent(ruleID, "2026-07", "expired", strings.Repeat("6", 64), ref("cmp-ref-expired"),
		at(-90*24*time.Hour), at(-90*24*time.Hour), nil, at(-60*24*time.Hour), at(-90*24*time.Hour))
	// Stale-notice evidence: an earlier contract version must never bleed into the current
	// cohort's aggregate, even though it targets the same rule.
	insertConsent(ruleID, "2026-06", "active", strings.Repeat("7", 64), ref("cmp-ref-stale-notice"),
		at(-1*24*time.Hour), at(-1*24*time.Hour), nil, at(29*24*time.Hour), at(-1*24*time.Hour))
	// A different rule's evidence, same tenant/project/notice/source, must not be counted either.
	insertConsent(otherRuleID, "2026-07", "active", strings.Repeat("8", 64), ref("cmp-ref-other-rule"),
		at(-1*24*time.Hour), at(-1*24*time.Hour), nil, at(29*24*time.Hour), at(-1*24*time.Hour))

	var readerID, deniedID, otherTenantMemberID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'diagnostic-reader@acme.test', ARRAY['widget_rule:read']) RETURNING id
	`, tenantID).Scan(&readerID); err != nil {
		t.Fatalf("create diagnostic reader: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'diagnostic-denied@acme.test', '{}') RETURNING id
	`, tenantID).Scan(&deniedID); err != nil {
		t.Fatalf("create diagnostic-denied member: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions)
		VALUES ($1, 'diagnostic-other@beta.test', ARRAY['widget_rule:read']) RETURNING id
	`, otherTenantID).Scan(&otherTenantMemberID); err != nil {
		t.Fatalf("create other-tenant member: %v", err)
	}

	pools, err := db.OpenPools(ctx, analyticsRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	jwtManager, err := auth.NewJWTManager(strings.Repeat("d", 32), "fidibeki", time.Hour)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	readToken, _, err := jwtManager.IssueTenant(readerID, tenantID, []string{string(auth.PermissionWidgetRuleRead)})
	if err != nil {
		t.Fatalf("issue reader token: %v", err)
	}
	deniedToken, _, err := jwtManager.IssueTenant(deniedID, tenantID, nil)
	if err != nil {
		t.Fatalf("issue denied token: %v", err)
	}
	otherTenantToken, _, err := jwtManager.IssueTenant(
		otherTenantMemberID, otherTenantID, []string{string(auth.PermissionWidgetRuleRead)},
	)
	if err != nil {
		t.Fatalf("issue other-tenant token: %v", err)
	}

	routes := chi.NewRouter()
	RegisterTenantRoutes(routes, TenantHTTPOptions{
		Store: Store{}, JWT: jwtManager, Now: func() time.Time { return now },
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App),
	})(routes)

	diagnosticPath := "/v1/projects/" + projectID + "/widget-rules/" + ruleID + "/analytics-consent-diagnostic"
	response := analyticsDiagnosticRequest(handler, "acme.localhost", diagnosticPath, readToken)
	if response.Code != http.StatusOK {
		t.Fatalf("diagnostic status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{
		`"widget_rule_id":"` + ruleID + `"`,
		`"consent_mode":"customer_cmp"`,
		`"notice_version":"2026-07"`,
		`"active_count":2`,
		`"expiring_soon_count":1`,
		`"denied_count":1`,
		`"withdrawn_count":1`,
		`"expired_count":2`,
		`"recent_count":4`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("diagnostic body missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{
		"token", "cmp-ref", "customer_consent_reference", "locale", "source",
		strings.Repeat("1", 64), strings.Repeat("2", 64),
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("diagnostic leaked %q: %s", forbidden, body)
		}
	}

	if denied := analyticsDiagnosticRequest(handler, "acme.localhost", diagnosticPath, deniedToken); denied.Code != http.StatusForbidden {
		t.Fatalf("permission-denied diagnostic status = %d, body = %s", denied.Code, denied.Body.String())
	}
	if crossTenant := analyticsDiagnosticRequest(
		handler, "beta.localhost", diagnosticPath, otherTenantToken,
	); crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant diagnostic status = %d, body = %s", crossTenant.Code, crossTenant.Body.String())
	}

	dormantPath := "/v1/projects/" + projectID + "/widget-rules/" + dormantRuleID + "/analytics-consent-diagnostic"
	dormant := analyticsDiagnosticRequest(handler, "acme.localhost", dormantPath, readToken)
	if dormant.Code != http.StatusOK ||
		!strings.Contains(dormant.Body.String(), `"consent_mode":"none"`) ||
		!strings.Contains(dormant.Body.String(), `"active_count":0`) ||
		strings.Contains(dormant.Body.String(), `"next_expires_at"`) {
		t.Fatalf("dormant rule diagnostic = %d/%s", dormant.Code, dormant.Body.String())
	}

	invalidRulePath := "/v1/projects/" + projectID + "/widget-rules/not-a-uuid/analytics-consent-diagnostic"
	if invalid := analyticsDiagnosticRequest(handler, "acme.localhost", invalidRulePath, readToken); invalid.Code != http.StatusNotFound {
		t.Fatalf("invalid rule id diagnostic status = %d", invalid.Code)
	}
	missingPath := "/v1/projects/" + projectID +
		"/widget-rules/00000000-0000-4000-8000-000000000099/analytics-consent-diagnostic"
	if missing := analyticsDiagnosticRequest(handler, "acme.localhost", missingPath, readToken); missing.Code != http.StatusNotFound {
		t.Fatalf("missing rule diagnostic status = %d", missing.Code)
	}
}

func nullableConsentString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func analyticsDiagnosticRequest(handler http.Handler, host, path, token string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	request.Host = host
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	handler.ServeHTTP(response, request)
	return response
}
