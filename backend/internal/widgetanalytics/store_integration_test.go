package widgetanalytics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/acidiney/app.fidibeki/backend/internal/project"
	"github.com/acidiney/app.fidibeki/backend/internal/tenant"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestStorePersistsMatchedEventIdempotentlyAndSummarizesIt(t *testing.T) {
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

	var tenantID, projectID, ruleID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', 'acme', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Acme Web', 'pk_live_ABCDEFGHIJKLMNOPQRSTUV', '{example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (project_id, name, matcher_type, trigger_type, trigger_delay_seconds)
		VALUES ($1, 'Checkout', 'all_pages', 'delay', 0) RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create widget rule: %v", err)
	}

	pools, err := db.OpenPools(ctx, analyticsRoleURL(t, directURL), directURL, 4, 2)
	if err != nil {
		t.Fatalf("open database pools: %v", err)
	}
	t.Cleanup(pools.Close)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	configuration := project.PublicConfiguration{
		TenantID: tenantID, ProjectID: projectID, Plan: tenant.PlanGrowth,
		Status: tenant.StatusActive, Rules: []project.WidgetRule{{ID: ruleID, Name: "Checkout", Enabled: true}},
	}
	inputs := []EventInput{{
		EventID: "00000000-0000-4000-8000-000000000001", EventType: EventMatched,
		VisitorID: "00000000-0000-4000-8000-000000000002", PageViewID: "00000000-0000-4000-8000-000000000003",
		RuleID: ruleID, PageURL: "https://example.com/chart/?asset=BAI#price", OccurredAt: now,
	}}
	values, fields := ValidateEvents(inputs, configuration, "https://example.com", now)
	if len(fields) > 0 {
		t.Fatalf("validate events: %#v", fields)
	}

	tx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin app transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		t.Fatalf("set tenant context: %v", err)
	}
	store := Store{}
	first, err := store.InsertEvents(ctx, tx, values)
	if err != nil {
		t.Fatalf("insert first event: %v", err)
	}
	retry, err := store.InsertEvents(ctx, tx, values)
	if err != nil {
		t.Fatalf("retry event: %v", err)
	}
	summary, err := store.Summary(ctx, tx, tenantID, projectID, 7, now)
	if err != nil {
		t.Fatalf("summarize events: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit events: %v", err)
	}
	if len(first.AcceptedEventIDs) != 1 || len(first.DuplicateEventIDs) != 0 ||
		len(retry.AcceptedEventIDs) != 0 || len(retry.DuplicateEventIDs) != 1 {
		t.Fatalf("first = %#v, retry = %#v", first, retry)
	}
	if summary.Matched != 1 || summary.Impressions != 0 || summary.ImpressionRate == nil || *summary.ImpressionRate != 0 {
		t.Fatalf("summary = %#v", summary)
	}
	assertEventWithoutMatchedIsRejected(t, ctx, pools, store, configuration, ruleID, now)
	assertLifecycleDuplicateWithDifferentEventID(t, ctx, pools, store, configuration, inputs[0], now)
	assertVisibleInteractionStages(t, ctx, pools, store, configuration, inputs[0], now)

	jwtManager, err := auth.NewJWTManager(strings.Repeat("a", 32), "fidibeki", 10*time.Minute)
	if err != nil {
		t.Fatalf("create JWT manager: %v", err)
	}
	readToken, _, err := jwtManager.IssueTenant("analytics-reader", tenantID, []string{string(auth.PermissionProjectRead)})
	if err != nil {
		t.Fatalf("issue analytics token: %v", err)
	}
	deniedToken, _, err := jwtManager.IssueTenant("analytics-denied", tenantID, []string{string(auth.PermissionFeedbackRead)})
	if err != nil {
		t.Fatalf("issue denied token: %v", err)
	}
	routes := chi.NewRouter()
	RegisterTenantRoutes(routes, TenantHTTPOptions{
		Store: store, JWT: jwtManager, Now: func() time.Time { return now },
	})
	handler := appmiddleware.TenantTransaction(appmiddleware.TenantTransactionOptions{
		BaseDomain: "localhost", Resolver: tenant.PostgresResolver{Admin: pools.Admin},
		Begin: db.BeginFromPool(pools.App), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})(routes)
	response := tenantAnalyticsRequest(handler, "/v1/analytics/widget?range=7d&project_id="+projectID, readToken)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"matched":1`) ||
		!strings.Contains(response.Body.String(), `"impressions":1`) ||
		!strings.Contains(response.Body.String(), `"interactions":1`) {
		t.Fatalf("analytics response = status %d body %s", response.Code, response.Body.String())
	}
	if denied := tenantAnalyticsRequest(handler, "/v1/analytics/widget", deniedToken); denied.Code != http.StatusForbidden {
		t.Fatalf("denied analytics status = %d, body = %s", denied.Code, denied.Body.String())
	}
	assertConcurrentReversedBatches(t, ctx, pools, store, configuration, tenantID, projectID, ruleID, now)
	assertConcurrentSameEventID(t, ctx, pools, configuration, projectID, ruleID, now)
	assertCrossTenantEventIDCollision(t, ctx, root, pools, store, configuration, inputs[0], now)

	if _, err := root.Exec(ctx, `DELETE FROM widget_rules WHERE id = $1`, ruleID); err != nil {
		t.Fatalf("delete source rule: %v", err)
	}
	var rawCount int
	var ruleName string
	if err := root.QueryRow(ctx, `SELECT count(*), max(rule_name) FROM widget_funnel_events WHERE project_id = $1`, projectID).
		Scan(&rawCount, &ruleName); err != nil || rawCount != 6 || ruleName != "Checkout" {
		t.Fatalf("historical event after rule delete = count %d name %q error %v", rawCount, ruleName, err)
	}
	if _, err := root.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	for _, table := range []string{"widget_funnel_events", "widget_funnel_daily"} {
		if err := root.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&rawCount); err != nil || rawCount != 0 {
			t.Fatalf("%s after project delete = %d, error %v", table, rawCount, err)
		}
	}
}

func assertCrossTenantEventIDCollision(
	t *testing.T,
	ctx context.Context,
	root *pgx.Conn,
	pools *db.Pools,
	store Store,
	configuration project.PublicConfiguration,
	localLifecycle EventInput,
	now time.Time,
) {
	t.Helper()
	var tenantID, projectID, ruleID string
	if err := root.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Other', 'other', 'growth', 'active') RETURNING id
	`).Scan(&tenantID); err != nil {
		t.Fatalf("create collision tenant: %v", err)
	}
	defer func() {
		if _, err := root.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, tenantID); err != nil {
			t.Errorf("delete collision tenant: %v", err)
		}
	}()
	if err := root.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Other Web', 'pk_live_ZYXWVUTSRQPONMLKJIHGFE', '{other.example.com}') RETURNING id
	`, tenantID).Scan(&projectID); err != nil {
		t.Fatalf("create collision project: %v", err)
	}
	if err := root.QueryRow(ctx, `
		INSERT INTO widget_rules (project_id, name, matcher_type, trigger_type, trigger_delay_seconds)
		VALUES ($1, 'Other rule', 'all_pages', 'delay', 0) RETURNING id
	`, projectID).Scan(&ruleID); err != nil {
		t.Fatalf("create collision rule: %v", err)
	}
	const collisionEventID = "00000000-0000-4000-8000-000000000061"
	if _, err := root.Exec(ctx, `
		INSERT INTO widget_funnel_events (
			event_id, tenant_id, project_id, rule_id, rule_name, event_type,
			visitor_id, page_view_id, page_url, page_key, occurred_at
		) VALUES (
			$1, $2, $3, $4, 'Other rule', 'matched', gen_random_uuid(), gen_random_uuid(),
			'https://other.example.com', decode(repeat('cd', 32), 'hex'), $5
		)
	`, collisionEventID, tenantID, projectID, ruleID, now); err != nil {
		t.Fatalf("seed cross-tenant event ID collision: %v", err)
	}

	collisionInput := localLifecycle
	collisionInput.EventID = collisionEventID
	values, fields := ValidateEvents([]EventInput{collisionInput}, configuration, "https://example.com", now)
	if len(fields) > 0 {
		t.Fatalf("validate cross-tenant collision: %#v", fields)
	}
	tx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-tenant collision transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID); err != nil {
		t.Fatalf("set cross-tenant collision tenant: %v", err)
	}
	if _, err := store.InsertEvents(ctx, tx, values); !errors.Is(err, errEventIDConflict) {
		_ = tx.Rollback(ctx)
		t.Fatalf("cross-tenant event ID collision error = %v, want %v", err, errEventIDConflict)
	}
	_ = tx.Rollback(ctx)
}

func assertConcurrentSameEventID(
	t *testing.T,
	ctx context.Context,
	pools *db.Pools,
	configuration project.PublicConfiguration,
	projectID string,
	ruleID string,
	now time.Time,
) {
	t.Helper()
	values, fields := ValidateEvents([]EventInput{{
		EventID: "00000000-0000-4000-8000-000000000051", EventType: EventMatched,
		VisitorID: "00000000-0000-4000-8000-000000000052", PageViewID: "00000000-0000-4000-8000-000000000053",
		RuleID: ruleID, PageURL: "https://example.com/concurrent", OccurredAt: now,
	}}, configuration, "https://example.com", now)
	if len(fields) > 0 {
		t.Fatalf("validate concurrent same-ID event: %#v", fields)
	}

	txs := make([]pgx.Tx, 2)
	for index := range txs {
		tx, err := pools.App.Begin(ctx)
		if err != nil {
			t.Fatalf("begin concurrent same-ID transaction %d: %v", index, err)
		}
		txs[index] = tx
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID); err != nil {
			t.Fatalf("set concurrent same-ID tenant %d: %v", index, err)
		}
	}

	type outcome struct {
		inserted bool
		err      error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	var group sync.WaitGroup
	for _, tx := range txs {
		group.Add(1)
		go func(tx pgx.Tx) {
			defer group.Done()
			<-start
			inserted, err := insertEvent(ctx, tx, values[0], false)
			if err == nil {
				err = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			outcomes <- outcome{inserted: inserted, err: err}
		}(tx)
	}
	close(start)
	group.Wait()
	close(outcomes)

	inserted, duplicate := 0, 0
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent same-ID insertion: %v", outcome.err)
		}
		if outcome.inserted {
			inserted++
		} else {
			duplicate++
		}
	}
	if inserted != 1 || duplicate != 1 {
		t.Fatalf("concurrent same-ID outcomes = inserted %d duplicate %d", inserted, duplicate)
	}

	var rawCount, matchedCount int
	if err := pools.Admin.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_events WHERE project_id = $1
	`, projectID).Scan(&rawCount); err != nil {
		t.Fatalf("count same-ID raw events: %v", err)
	}
	if err := pools.Admin.QueryRow(ctx, `
		SELECT COALESCE(sum(matched_count), 0) FROM widget_funnel_daily WHERE project_id = $1
	`, projectID).Scan(&matchedCount); err != nil {
		t.Fatalf("count same-ID aggregates: %v", err)
	}
	if rawCount != 6 || matchedCount != 4 {
		t.Fatalf("counts after same-ID concurrency = raw %d matched %d, want 6/4", rawCount, matchedCount)
	}
}

func assertLifecycleDuplicateWithDifferentEventID(
	t *testing.T,
	ctx context.Context,
	pools *db.Pools,
	store Store,
	configuration project.PublicConfiguration,
	matchedInput EventInput,
	now time.Time,
) {
	t.Helper()
	duplicateInput := matchedInput
	duplicateInput.EventID = "00000000-0000-4000-8000-000000000009"
	values, fields := ValidateEvents([]EventInput{duplicateInput}, configuration, "https://example.com", now)
	if len(fields) > 0 {
		t.Fatalf("validate lifecycle duplicate: %#v", fields)
	}
	tx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lifecycle duplicate transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID); err != nil {
		t.Fatalf("set lifecycle duplicate tenant: %v", err)
	}
	result, err := store.InsertEvents(ctx, tx, values)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert lifecycle duplicate: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lifecycle duplicate: %v", err)
	}
	if len(result.AcceptedEventIDs) != 0 || len(result.DuplicateEventIDs) != 1 {
		t.Fatalf("lifecycle duplicate result = %#v", result)
	}
}

func assertVisibleInteractionStages(
	t *testing.T,
	ctx context.Context,
	pools *db.Pools,
	store Store,
	configuration project.PublicConfiguration,
	matchedInput EventInput,
	now time.Time,
) {
	t.Helper()
	inputs := []EventInput{
		matchedInput,
		matchedInput,
	}
	inputs[0].EventID = "00000000-0000-4000-8000-000000000041"
	inputs[0].EventType = EventImpression
	inputs[1].EventID = "00000000-0000-4000-8000-000000000042"
	inputs[1].EventType = EventInteraction
	values, fields := ValidateEvents(inputs, configuration, "https://example.com", now)
	if len(fields) > 0 {
		t.Fatalf("validate visible interaction stages: %#v", fields)
	}
	tx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin visible interaction transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID); err != nil {
		t.Fatalf("set visible interaction tenant: %v", err)
	}
	result, err := store.InsertEvents(ctx, tx, values)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert visible interaction stages: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit visible interaction stages: %v", err)
	}
	if len(result.AcceptedEventIDs) != 2 || len(result.DuplicateEventIDs) != 0 {
		t.Fatalf("visible interaction result = %#v", result)
	}
}

func assertEventWithoutMatchedIsRejected(
	t *testing.T,
	ctx context.Context,
	pools *db.Pools,
	store Store,
	configuration project.PublicConfiguration,
	ruleID string,
	now time.Time,
) {
	t.Helper()
	values, fields := ValidateEvents([]EventInput{{
		EventID: "00000000-0000-4000-8000-000000000031", EventType: EventImpression,
		VisitorID: "00000000-0000-4000-8000-000000000032", PageViewID: "00000000-0000-4000-8000-000000000033",
		RuleID: ruleID, PageURL: "https://example.com/missing-match", OccurredAt: now,
	}}, configuration, "https://example.com", now)
	if len(fields) > 0 {
		t.Fatalf("validate unmatched impression: %#v", fields)
	}
	tx, err := pools.App.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unmatched impression transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, configuration.TenantID); err != nil {
		t.Fatalf("set unmatched impression tenant: %v", err)
	}
	if _, err := store.InsertEvents(ctx, tx, values); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("impression without matched event succeeded")
	}
	_ = tx.Rollback(ctx)
}

func assertConcurrentReversedBatches(
	t *testing.T,
	ctx context.Context,
	pools *db.Pools,
	store Store,
	configuration project.PublicConfiguration,
	tenantID string,
	projectID string,
	ruleID string,
	now time.Time,
) {
	t.Helper()
	inputs := []EventInput{
		{
			EventID: "00000000-0000-4000-8000-000000000011", EventType: EventMatched,
			VisitorID: "00000000-0000-4000-8000-000000000012", PageViewID: "00000000-0000-4000-8000-000000000013",
			RuleID: ruleID, PageURL: "https://example.com/alpha", OccurredAt: now,
		},
		{
			EventID: "00000000-0000-4000-8000-000000000021", EventType: EventMatched,
			VisitorID: "00000000-0000-4000-8000-000000000022", PageViewID: "00000000-0000-4000-8000-000000000023",
			RuleID: ruleID, PageURL: "https://example.com/beta", OccurredAt: now,
		},
	}
	values, fields := ValidateEvents(inputs, configuration, "https://example.com", now)
	if len(fields) > 0 {
		t.Fatalf("validate concurrent events: %#v", fields)
	}
	reversed := []EventValues{values[1], values[0]}
	txs := make([]pgx.Tx, 2)
	for index := range txs {
		tx, err := pools.App.Begin(ctx)
		if err != nil {
			t.Fatalf("begin concurrent transaction %d: %v", index, err)
		}
		txs[index] = tx
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
			t.Fatalf("set concurrent tenant context %d: %v", index, err)
		}
	}
	type outcome struct {
		result IngestResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	var group sync.WaitGroup
	for index, batch := range [][]EventValues{values, reversed} {
		group.Add(1)
		go func(tx pgx.Tx, events []EventValues) {
			defer group.Done()
			<-start
			result, err := store.InsertEvents(ctx, tx, events)
			if err == nil {
				err = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			outcomes <- outcome{result: result, err: err}
		}(txs[index], batch)
	}
	close(start)
	group.Wait()
	close(outcomes)
	acceptedBatch, duplicateBatch := 0, 0
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent ingestion: %v", outcome.err)
		}
		switch {
		case len(outcome.result.AcceptedEventIDs) == 2 && len(outcome.result.DuplicateEventIDs) == 0:
			acceptedBatch++
		case len(outcome.result.AcceptedEventIDs) == 0 && len(outcome.result.DuplicateEventIDs) == 2:
			duplicateBatch++
		default:
			t.Fatalf("unexpected concurrent outcome: %#v", outcome.result)
		}
	}
	if acceptedBatch != 1 || duplicateBatch != 1 {
		t.Fatalf("concurrent batches = accepted %d duplicate %d", acceptedBatch, duplicateBatch)
	}
	var rawCount, matchedCount int
	if err := pools.Admin.QueryRow(ctx, `
		SELECT count(*) FROM widget_funnel_events WHERE project_id = $1
	`, projectID).Scan(&rawCount); err != nil {
		t.Fatalf("count concurrent raw events: %v", err)
	}
	if err := pools.Admin.QueryRow(ctx, `
		SELECT COALESCE(sum(matched_count), 0) FROM widget_funnel_daily WHERE project_id = $1
	`, projectID).Scan(&matchedCount); err != nil {
		t.Fatalf("count concurrent aggregates: %v", err)
	}
	if rawCount != 5 || matchedCount != 3 {
		t.Fatalf("counts after concurrent ingestion = raw %d matched %d, want 5/3", rawCount, matchedCount)
	}
}

func tenantAnalyticsRequest(handler http.Handler, path, token string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://acme.localhost"+path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(response, request)
	return response
}

func analyticsMigrationsPath(t *testing.T) string {
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

func analyticsRoleURL(t *testing.T, directURL string) string {
	t.Helper()
	parsed, err := url.Parse(directURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	parsed.User = url.UserPassword("fidibeki_app", "app-password")
	return parsed.String()
}
