package membernotify_test

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/membernotify"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type captureQueue struct{ messages []email.Message }

func (q *captureQueue) Enqueue(_ context.Context, message email.Message) error {
	q.messages = append(q.messages, message)
	return nil
}

type workspace struct {
	TenantID  string
	ProjectID string
	MemberID  string
	OtherID   string
	IssueID   string
}

func TestPreferencesRoundTripAndAudit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "round-trip")
	store := membernotify.Store{}

	defaults, err := store.Get(ctx, database, fixture.TenantID, fixture.MemberID)
	if err != nil {
		t.Fatalf("read default preferences: %v", err)
	}
	if defaults != withTimezone(membernotify.Defaults(), "UTC", "default") {
		t.Fatalf("stored defaults diverge from the documented defaults: %+v", defaults)
	}

	next := defaults
	next.Comments = membernotify.ModeWeekly
	next.DigestHour = 18
	timezone := "Africa/Luanda"
	saved, err := store.Save(
		ctx, database, fixture.TenantID, fixture.MemberID, next, &timezone, true,
		time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("save preferences: %v", err)
	}
	if saved.Comments != membernotify.ModeWeekly || saved.DigestHour != 18 {
		t.Fatalf("saved preferences = %+v", saved)
	}
	if saved.Timezone != "Africa/Luanda" || saved.TimezoneSource != "member" {
		t.Fatalf("timezone = %q from %q, want the member override", saved.Timezone, saved.TimezoneSource)
	}

	var kind, action, resourceType, resourceID string
	var data []byte
	if err := database.QueryRow(ctx, `
		SELECT kind, action, resource_type, resource_id::text, data
		FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'notification_preferences.updated'
	`, fixture.TenantID).Scan(&kind, &action, &resourceType, &resourceID, &data); err != nil {
		t.Fatalf("read preference audit event: %v", err)
	}
	if action != "update" || resourceType != "notification_preference" ||
		resourceID != fixture.MemberID {
		t.Fatalf("audit event = %s/%s/%s", action, resourceType, resourceID)
	}
	for _, want := range []string{"comments", "digest_hour", "timezone"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("audit data is missing %q: %s", want, data)
		}
	}
}

func TestUnsubscribeSilencesEveryOptionalCategory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "unsubscribe")
	store := membernotify.Store{}
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)

	if err := store.RecordUnsubscribe(
		ctx, database, fixture.TenantID, fixture.MemberID, now,
	); err != nil {
		t.Fatalf("record unsubscribe: %v", err)
	}
	silenced, err := store.Get(ctx, database, fixture.TenantID, fixture.MemberID)
	if err != nil {
		t.Fatalf("read preferences: %v", err)
	}
	for _, category := range membernotify.Categories {
		if mode := silenced.Mode(category); mode != membernotify.ModeOff {
			t.Fatalf("category %q = %q after unsubscribe, want off", category, mode)
		}
	}
	var audited int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND kind = 'notification_preferences.unsubscribed'
		  AND action = 'unsubscribe' AND data->>'channel' = 'email_link'
	`, fixture.TenantID).Scan(&audited); err != nil {
		t.Fatalf("count unsubscribe audit events: %v", err)
	}
	if audited != 1 {
		t.Fatalf("unsubscribe audit events = %d, want 1", audited)
	}
}

// The delivery contract: a window with events is sent exactly once, a rerun sends
// nothing more, and a window with nothing in it leaves no trace at all.
func TestDigestDeliversEachWindowExactlyOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "delivery")
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	recordMention(t, ctx, database, fixture, now.Add(-3*time.Minute))

	queue := &captureQueue{}
	scheduler := digestScheduler(t, database, queue, now)
	if err := scheduler.Execute(ctx); err != nil {
		t.Fatalf("first digest pass: %v", err)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("first pass queued %d messages, want 1", len(queue.messages))
	}
	if !strings.Contains(queue.messages[0].Text, "ACME-2026-0001") {
		t.Fatalf("digest does not reference the issue:\n%s", queue.messages[0].Text)
	}

	// A rerun at the same instant must find the window already delivered.
	if err := scheduler.Execute(ctx); err != nil {
		t.Fatalf("repeated digest pass: %v", err)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("a repeated pass sent %d messages, want 1", len(queue.messages))
	}

	// A later pass with no new activity must not send an empty digest, and must
	// not write a ledger row for the empty window.
	later := digestScheduler(t, database, queue, now.Add(30*time.Minute))
	if err := later.Execute(ctx); err != nil {
		t.Fatalf("later digest pass: %v", err)
	}
	if len(queue.messages) != 1 {
		t.Fatalf("an empty window was delivered: %d messages", len(queue.messages))
	}
	var deliveries, sent int
	if err := database.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE status = 'sent')
		FROM member_digest_deliveries WHERE tenant_id = $1 AND member_id = $2
	`, fixture.TenantID, fixture.MemberID).Scan(&deliveries, &sent); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveries != 1 || sent != 1 {
		t.Fatalf("delivery rows = %d (%d sent), want exactly one sent window", deliveries, sent)
	}
}

// After an interrupted attempt the same window must be retaken and delivered,
// not skipped: the events inside it were never mailed.
func TestFailedWindowIsRetakenWithoutDuplicating(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "retry")
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	recordMention(t, ctx, database, fixture, now.Add(-3*time.Minute))

	queue := &captureQueue{}
	if err := digestScheduler(t, database, queue, now).Execute(ctx); err != nil {
		t.Fatalf("first digest pass: %v", err)
	}
	if _, err := database.Exec(ctx, `
		UPDATE member_digest_deliveries
		SET status = 'failed', sent_at = NULL, error_message = 'provider unavailable'
		WHERE tenant_id = $1 AND member_id = $2
	`, fixture.TenantID, fixture.MemberID); err != nil {
		t.Fatalf("simulate interrupted delivery: %v", err)
	}
	if err := digestScheduler(t, database, queue, now.Add(time.Minute)).Execute(ctx); err != nil {
		t.Fatalf("retry digest pass: %v", err)
	}
	var rows int
	var status string
	if err := database.QueryRow(ctx, `
		SELECT count(*), min(status)
		FROM member_digest_deliveries WHERE tenant_id = $1 AND member_id = $2
	`, fixture.TenantID, fixture.MemberID).Scan(&rows, &status); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if rows != 1 || status != "sent" {
		t.Fatalf("delivery rows = %d with status %q, want one retaken and sent window", rows, status)
	}
	if len(queue.messages) != 2 {
		t.Fatalf("queued %d messages, want the retry to hand off again", len(queue.messages))
	}
	// Both hand-offs carry the same idempotency key, so the queue collapses them
	// into a single provider send.
	if queue.messages[0].IdempotencyKey != queue.messages[1].IdempotencyKey {
		t.Fatalf(
			"retry used a different idempotency key: %q then %q",
			queue.messages[0].IdempotencyKey, queue.messages[1].IdempotencyKey,
		)
	}

	// A pass that dies between claiming and delivering leaves the window claimed
	// forever. Once it is stale, a later pass must take it over rather than let
	// the events inside it go unsent.
	if _, err := database.Exec(ctx, `
		UPDATE member_digest_deliveries
		SET status = 'claimed', sent_at = NULL, created_at = $3
		WHERE tenant_id = $1 AND member_id = $2
	`, fixture.TenantID, fixture.MemberID, now.Add(-time.Hour)); err != nil {
		t.Fatalf("simulate abandoned claim: %v", err)
	}
	if err := digestScheduler(t, database, queue, now.Add(2*time.Minute)).Execute(ctx); err != nil {
		t.Fatalf("stale claim digest pass: %v", err)
	}
	if err := database.QueryRow(ctx, `
		SELECT count(*), min(status)
		FROM member_digest_deliveries WHERE tenant_id = $1 AND member_id = $2
	`, fixture.TenantID, fixture.MemberID).Scan(&rows, &status); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if rows != 1 || status != "sent" {
		t.Fatalf("after a stale claim: %d rows with status %q, want one sent window", rows, status)
	}
	if len(queue.messages) != 3 {
		t.Fatalf("queued %d messages, want the stale window handed off again", len(queue.messages))
	}
}

func TestBouncedMemberIsSuppressedAndVisible(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "bounced")
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	recordMention(t, ctx, database, fixture, now.Add(-3*time.Minute))
	if _, err := database.Exec(ctx, `
		UPDATE tenant_members SET email_bounced = true WHERE tenant_id = $1 AND id = $2
	`, fixture.TenantID, fixture.MemberID); err != nil {
		t.Fatalf("mark member bounced: %v", err)
	}

	queue := &captureQueue{}
	if err := digestScheduler(t, database, queue, now).Execute(ctx); err != nil {
		t.Fatalf("digest pass: %v", err)
	}
	if len(queue.messages) != 0 {
		t.Fatalf("mail was sent to a bounced address: %+v", queue.messages)
	}
	deliveries, err := membernotify.Store{}.RecentDeliveries(
		ctx, database, fixture.TenantID, fixture.MemberID, 10,
	)
	if err != nil {
		t.Fatalf("read delivery diagnostics: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != membernotify.StatusSuppressed {
		t.Fatalf("delivery diagnostics = %+v, want one suppressed row", deliveries)
	}
	if deliveries[0].Reason == nil || *deliveries[0].Reason == "" {
		t.Fatal("suppressed delivery carries no reason")
	}
}

// A member who cannot read issues must not learn about them through email, even
// with the category switched on.
func TestDigestRespectsMemberPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	database := digestTestDatabase(t, ctx)
	fixture := seedWorkspace(t, ctx, database, "rbac")
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	recordMention(t, ctx, database, fixture, now.Add(-3*time.Minute))
	if _, err := database.Exec(ctx, `
		UPDATE tenant_members SET permissions = ARRAY['member:read']
		WHERE tenant_id = $1 AND id = $2
	`, fixture.TenantID, fixture.MemberID); err != nil {
		t.Fatalf("narrow member permissions: %v", err)
	}

	queue := &captureQueue{}
	if err := digestScheduler(t, database, queue, now).Execute(ctx); err != nil {
		t.Fatalf("digest pass: %v", err)
	}
	if len(queue.messages) != 0 {
		t.Fatalf("a member without issue access received a digest: %+v", queue.messages)
	}
	var rows int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM member_digest_deliveries WHERE tenant_id = $1
	`, fixture.TenantID).Scan(&rows); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if rows != 0 {
		t.Fatalf("delivery rows = %d, want none", rows)
	}
}

func digestScheduler(
	t *testing.T,
	database *pgxpool.Pool,
	queue *captureQueue,
	now time.Time,
) membernotify.Scheduler {
	t.Helper()
	tokens, err := auth.NewJWTManager(strings.Repeat("t", 32), "fidibeki", 90*24*time.Hour)
	if err != nil {
		t.Fatalf("create token manager: %v", err)
	}
	return membernotify.Scheduler{
		Store: membernotify.DigestStore{Database: database}, Email: queue, Tokens: tokens,
		PublicBaseURL: "http://localhost:8088", BaseDomain: "localhost",
		AuthFrom: "Fidibeki <notifications@fidibeki.com>", MaxMembers: 50,
		Now: func() time.Time { return now },
	}
}

func withTimezone(
	preferences membernotify.Preferences,
	timezone string,
	source string,
) membernotify.Preferences {
	preferences.Timezone = timezone
	preferences.TimezoneSource = source
	return preferences
}

func seedWorkspace(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	slug string,
) workspace {
	t.Helper()
	var fixture workspace
	if err := database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ('Acme', $1, 'growth', 'active') RETURNING id
	`, slug).Scan(&fixture.TenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions, status)
		VALUES ($1, $2, 'Member', ARRAY['issue:read', 'feedback:read'], 'active')
		RETURNING id
	`, fixture.TenantID, "member-"+slug+"@example.test").Scan(&fixture.MemberID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, name, permissions, status)
		VALUES ($1, $2, 'Ana', ARRAY['issue:read', 'issue:update'], 'active')
		RETURNING id
	`, fixture.TenantID, "ana-"+slug+"@example.test").Scan(&fixture.OtherID); err != nil {
		t.Fatalf("create second member: %v", err)
	}
	publicKey := "pk_live_" + (strings.ToUpper(strings.ReplaceAll(slug, "-", "")) +
		strings.Repeat("A", 22))[:22]
	if err := database.QueryRow(ctx, `
		INSERT INTO projects (tenant_id, name, public_key, allowed_domains)
		VALUES ($1, 'Web', $2, '{example.test}') RETURNING id
	`, fixture.TenantID, publicKey).Scan(&fixture.ProjectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := database.QueryRow(ctx, `
		INSERT INTO issues (tenant_id, project_id, public_id, title, status)
		VALUES ($1, $2, 'ACME-2026-0001', 'Checkout fails', 'open') RETURNING id
	`, fixture.TenantID, fixture.ProjectID).Scan(&fixture.IssueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return fixture
}

func recordMention(
	t *testing.T,
	ctx context.Context,
	database *pgxpool.Pool,
	fixture workspace,
	at time.Time,
) {
	t.Helper()
	if _, err := database.Exec(ctx, `
		INSERT INTO issue_activities (
			tenant_id, issue_id, actor_member_id, kind, visibility, data, created_at
		) VALUES (
			$1, $2, $3, 'issue.comment_created', 'internal',
			jsonb_build_object('mention_member_ids', jsonb_build_array($4::text)), $5
		)
	`, fixture.TenantID, fixture.IssueID, fixture.OtherID, fixture.MemberID, at); err != nil {
		t.Fatalf("record mention activity: %v", err)
	}
}

func digestTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
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
		t.Fatalf("get PostgreSQL URL: %v", err)
	}
	migrator, err := db.NewMigrator(directURL, digestMigrationsPath(t))
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	config, err := pgxpool.ParseConfig(directURL)
	if err != nil {
		t.Fatalf("parse database URL: %v", err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	config.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func digestMigrationsPath(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate the digest test")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations")
}
