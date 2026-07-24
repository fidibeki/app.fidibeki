package accessaudit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestRecordAndListAccessEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := accessAuditTestDatabase(t, ctx)
	store := accessaudit.Store{}

	tenantID := seedAccessAuditTenant(t, ctx, database, "acme-audit")
	otherTenantID := seedAccessAuditTenant(t, ctx, database, "beta-audit")
	memberID := seedAccessAuditMember(t, ctx, database, tenantID)
	apiKeyID := seedAccessAuditAPIKey(t, ctx, database, tenantID)

	base := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

	// Human preview, success.
	if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "feedback_attachment.accessed",
		Actor:          accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
		ResourceType:   accessaudit.ResourceFeedbackAttachment,
		ResourceID:     "11111111-1111-1111-1111-111111111111",
		Action:         "preview",
		PermissionPath: "feedback:read",
		RequestID:      "req-1",
		Outcome:        accessaudit.OutcomeSuccess,
		DedupWindow:    time.Minute,
		Now:            base,
	}); err != nil {
		t.Fatalf("record human preview: %v", err)
	}

	// Duplicate within the same dedup window must be skipped (volume safeguard).
	if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "feedback_attachment.accessed",
		Actor:          accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
		ResourceType:   accessaudit.ResourceFeedbackAttachment,
		ResourceID:     "11111111-1111-1111-1111-111111111111",
		Action:         "preview",
		PermissionPath: "feedback:read",
		RequestID:      "req-2",
		Outcome:        accessaudit.OutcomeSuccess,
		DedupWindow:    time.Minute,
		Now:            base.Add(5 * time.Second),
	}); err != nil {
		t.Fatalf("record duplicate preview: %v", err)
	}

	// A different action on the same resource is a distinct event.
	if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "feedback_attachment.accessed",
		Actor:          accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
		ResourceType:   accessaudit.ResourceFeedbackAttachment,
		ResourceID:     "11111111-1111-1111-1111-111111111111",
		Action:         "download",
		PermissionPath: "feedback:read",
		Outcome:        accessaudit.OutcomeSuccess,
		DedupWindow:    time.Minute,
		Now:            base.Add(6 * time.Second),
	}); err != nil {
		t.Fatalf("record download: %v", err)
	}

	// A later dedup bucket is a distinct event even for the same action.
	if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "feedback_attachment.accessed",
		Actor:          accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
		ResourceType:   accessaudit.ResourceFeedbackAttachment,
		ResourceID:     "11111111-1111-1111-1111-111111111111",
		Action:         "preview",
		PermissionPath: "feedback:read",
		Outcome:        accessaudit.OutcomeSuccess,
		DedupWindow:    time.Minute,
		Now:            base.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("record later preview: %v", err)
	}

	// API-key actor, denied.
	if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "replay_session.accessed",
		Actor:          accessaudit.Actor{Type: accessaudit.ActorAPIKey, APIKeyID: apiKeyID},
		ResourceType:   accessaudit.ResourceReplaySession,
		ResourceID:     "22222222-2222-2222-2222-222222222222",
		Action:         "playback",
		PermissionPath: "session:read",
		Outcome:        accessaudit.OutcomeDenied,
		Now:            base,
	}); err != nil {
		t.Fatalf("record api key denial: %v", err)
	}

	// Row in a different tenant must never leak into tenantID's listing.
	if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
		TenantID: otherTenantID, Kind: "feedback_attachment.accessed",
		Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
		ResourceType: accessaudit.ResourceFeedbackAttachment,
		ResourceID:   "33333333-3333-3333-3333-333333333333",
		Action:       "preview",
		Outcome:      accessaudit.OutcomeSuccess,
		Now:          base,
	}); err == nil {
		t.Fatal("expected foreign-tenant actor reference to fail")
	}

	events, err := store.ListEvents(ctx, database, tenantID, accessaudit.ListFilter{})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("ListEvents() returned %d events, want 4 (dedup should have skipped one)", len(events))
	}
	for _, event := range events {
		if event.ResourceType != nil && *event.ResourceType == accessaudit.ResourceFeedbackAttachment &&
			event.ResourceID != nil && *event.ResourceID == "33333333-3333-3333-3333-333333333333" {
			t.Fatalf("cross-tenant event leaked into tenant listing: %+v", event)
		}
	}

	deniedOnly, err := store.ListEvents(ctx, database, tenantID, accessaudit.ListFilter{Outcome: accessaudit.OutcomeDenied})
	if err != nil {
		t.Fatalf("ListEvents(denied) error = %v", err)
	}
	if len(deniedOnly) != 1 || deniedOnly[0].ActorType != accessaudit.ActorAPIKey {
		t.Fatalf("ListEvents(denied) = %+v, want a single api_key denial", deniedOnly)
	}

	downloadsOnly, err := store.ListEvents(ctx, database, tenantID, accessaudit.ListFilter{
		ResourceType: accessaudit.ResourceFeedbackAttachment, ActorType: accessaudit.ActorHuman,
	})
	if err != nil {
		t.Fatalf("ListEvents(resource/actor) error = %v", err)
	}
	if len(downloadsOnly) != 3 {
		t.Fatalf("ListEvents(resource/actor) = %d events, want 3", len(downloadsOnly))
	}

	page, err := store.ListEvents(ctx, database, tenantID, accessaudit.ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListEvents(limit) error = %v", err)
	}
	if len(page) != 3 {
		// Limit+1 is returned so callers can compute has_more themselves.
		t.Fatalf("ListEvents(limit=2) returned %d rows, want 3 (limit+1)", len(page))
	}
	cursor := accessaudit.EncodeCursor(page[1])
	nextPage, err := store.ListEvents(ctx, database, tenantID, accessaudit.ListFilter{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatalf("ListEvents(cursor) error = %v", err)
	}
	for _, event := range nextPage {
		if event.ID == page[0].ID || event.ID == page[1].ID {
			t.Fatalf("cursor pagination repeated an already-seen event: %+v", event)
		}
	}
}

func TestDetectAnomalies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := accessAuditTestDatabase(t, ctx)
	store := accessaudit.Store{}
	tenantID := seedAccessAuditTenant(t, ctx, database, "bulk-audit")
	memberID := seedAccessAuditMember(t, ctx, database, tenantID)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	for index := range 6 {
		resourceID := uuidFromIndex(index)
		if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
			TenantID: tenantID, Kind: "feedback_attachment.accessed",
			Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
			ResourceType: accessaudit.ResourceFeedbackAttachment,
			ResourceID:   resourceID,
			Action:       "preview",
			Outcome:      accessaudit.OutcomeSuccess,
			Now:          now.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatalf("seed anomaly event %d: %v", index, err)
		}
	}

	signals, err := store.DetectAnomalies(ctx, database, tenantID, accessaudit.AnomalySignalParams{
		Window: time.Hour, BulkResourceCount: 5, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("DetectAnomalies() error = %v", err)
	}
	if len(signals) != 1 || signals[0].ActorID != memberID || signals[0].DistinctResources < 5 {
		t.Fatalf("DetectAnomalies() = %+v, want one flagged actor with >=5 distinct resources", signals)
	}
}

func uuidFromIndex(index int) string {
	return "44444444-4444-4444-4444-4444444444" + string(rune('0'+index)) + "0"
}

func seedAccessAuditTenant(t *testing.T, ctx context.Context, database *pgxpool.Pool, slug string) string {
	t.Helper()
	var tenantID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenants (name, slug, plan, status)
		VALUES ($1, $2, 'growth', 'active') RETURNING id
	`, slug, slug).Scan(&tenantID); err != nil {
		t.Fatalf("create access-audit tenant: %v", err)
	}
	return tenantID
}

func seedAccessAuditMember(t *testing.T, ctx context.Context, database *pgxpool.Pool, tenantID string) string {
	t.Helper()
	var memberID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, $2, ARRAY['feedback:read', 'session:read'], 'active') RETURNING id
	`, tenantID, "member@"+tenantID+".test").Scan(&memberID); err != nil {
		t.Fatalf("create access-audit member: %v", err)
	}
	return memberID
}

func seedAccessAuditAPIKey(t *testing.T, ctx context.Context, database *pgxpool.Pool, tenantID string) string {
	t.Helper()
	var keyID string
	if err := database.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions)
		VALUES ($1, 'CI key', $2, 'fk_live_ci', ARRAY['session:read'])
		RETURNING id
	`, tenantID, sha256Hex(tenantID)).Scan(&keyID); err != nil {
		t.Fatalf("create access-audit api key: %v", err)
	}
	return keyID
}

func accessAuditTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgrescontainer.Run(ctx,
		"postgres:16-alpine",
		postgrescontainer.WithDatabase("fidibeki"),
		postgrescontainer.WithUsername("postgres"),
		postgrescontainer.WithPassword("postgres"),
		postgrescontainer.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start access-audit PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	directURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("access-audit PostgreSQL URL: %v", err)
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve access-audit test path")
	}
	migrations := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "infra", "migrations")
	migrator, err := db.NewMigrator(directURL, migrations)
	if err != nil {
		t.Fatalf("create access-audit migrator: %v", err)
	}
	t.Cleanup(func() { _ = migrator.Close() })
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate access-audit database: %v", err)
	}
	pool, err := pgxpool.New(ctx, directURL)
	if err != nil {
		t.Fatalf("connect access-audit PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
