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
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/ratelimit"
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
