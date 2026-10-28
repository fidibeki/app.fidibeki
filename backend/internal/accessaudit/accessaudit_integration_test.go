package accessaudit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/accessaudit"
	"github.com/fidibeki/app.fidibeki/backend/internal/db"
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

func TestDetectRevokedCredentialAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := accessAuditTestDatabase(t, ctx)
	store := accessaudit.Store{}
	tenantID := seedAccessAuditTenant(t, ctx, database, "revoked-audit")
	activeMemberID := seedAccessAuditMember(t, ctx, database, tenantID)
	removedMemberID := seedAccessAuditMember(t, ctx, database, tenantID)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	for _, memberID := range []string{activeMemberID, removedMemberID} {
		if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
			TenantID: tenantID, Kind: "feedback_attachment.accessed",
			Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
			ResourceType: accessaudit.ResourceFeedbackAttachment,
			ResourceID:   "55555555-5555-5555-5555-555555555555",
			Action:       "preview",
			Outcome:      accessaudit.OutcomeSuccess,
			Now:          now,
		}); err != nil {
			t.Fatalf("seed revoked-credential event for %s: %v", memberID, err)
		}
	}

	// Only removedMemberID's credential is revoked after the access above.
	if _, err := database.Exec(ctx, `UPDATE tenant_members SET status = 'removed' WHERE id = $1`, removedMemberID); err != nil {
		t.Fatalf("revoke member: %v", err)
	}

	signals, err := store.DetectRevokedCredentialAccess(ctx, database, tenantID, accessaudit.RevokedCredentialParams{
		Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("DetectRevokedCredentialAccess() error = %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("DetectRevokedCredentialAccess() = %+v, want exactly one flagged (removed) member", signals)
	}
	signal := signals[0]
	if signal.ActorID != removedMemberID || signal.Type != accessaudit.SignalRevokedCredential ||
		signal.ActorType != accessaudit.ActorHuman || signal.Detail != "removed" || signal.EventCount != 1 {
		t.Fatalf("DetectRevokedCredentialAccess() = %+v, want removed member signal", signal)
	}
	for _, other := range signals {
		if other.ActorID == activeMemberID {
			t.Fatalf("DetectRevokedCredentialAccess() flagged still-active member: %+v", other)
		}
	}
}

func TestDetectUnusualAPIKeyAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := accessAuditTestDatabase(t, ctx)
	store := accessaudit.Store{}
	tenantID := seedAccessAuditTenant(t, ctx, database, "unusual-key-audit")
	nonExpiringKeyID := seedAccessAuditAPIKey(t, ctx, database, tenantID)
	var expiringKeyID string
	if err := database.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, name, token_hash, token_prefix, permissions, expires_at)
		VALUES ($1, 'expiring key', $2, 'fk_live_exp', ARRAY['session:read'], now() + interval '30 days')
		RETURNING id
	`, tenantID, sha256Hex(tenantID+"-expiring")).Scan(&expiringKeyID); err != nil {
		t.Fatalf("create expiring api key: %v", err)
	}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	for _, keyID := range []string{nonExpiringKeyID, expiringKeyID} {
		if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
			TenantID: tenantID, Kind: "replay_session.accessed",
			Actor:        accessaudit.Actor{Type: accessaudit.ActorAPIKey, APIKeyID: keyID},
			ResourceType: accessaudit.ResourceReplaySession,
			ResourceID:   "66666666-6666-6666-6666-666666666666",
			Action:       "playback",
			Outcome:      accessaudit.OutcomeSuccess,
			Now:          now,
		}); err != nil {
			t.Fatalf("seed unusual-api-key event for %s: %v", keyID, err)
		}
	}

	signals, err := store.DetectUnusualAPIKeyAccess(ctx, database, tenantID, accessaudit.UnusualAPIKeyParams{
		Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("DetectUnusualAPIKeyAccess() error = %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("DetectUnusualAPIKeyAccess() = %+v, want exactly one flagged (non-expiring) key", signals)
	}
	signal := signals[0]
	if signal.ActorID != nonExpiringKeyID || signal.Type != accessaudit.SignalUnusualAPIKey ||
		signal.ActorType != accessaudit.ActorAPIKey || signal.Detail != "no_expiration" || signal.EventCount != 1 {
		t.Fatalf("DetectUnusualAPIKeyAccess() = %+v, want non-expiring key signal", signal)
	}
	for _, other := range signals {
		if other.ActorID == expiringKeyID {
			t.Fatalf("DetectUnusualAPIKeyAccess() flagged an expiring key: %+v", other)
		}
	}
}

func TestDetectSupportAccessVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := accessAuditTestDatabase(t, ctx)
	store := accessaudit.Store{}
	tenantID := seedAccessAuditTenant(t, ctx, database, "support-volume-audit")
	frequentAdminID := seedAccessAuditPlatformAdmin(t, ctx, database)
	quietAdminID := seedAccessAuditPlatformAdmin(t, ctx, database)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	for index := range 5 {
		resourceID := uuidFromIndex(index)
		if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
			TenantID: tenantID, Kind: "feedback_attachment.support_accessed",
			Actor:          accessaudit.PlatformAdminActor(frequentAdminID),
			ResourceType:   accessaudit.ResourceFeedbackAttachment,
			ResourceID:     resourceID,
			Action:         "preview",
			PermissionPath: accessaudit.PermissionPathSupportAccess,
			Outcome:        accessaudit.OutcomeSuccess,
			Now:            now.Add(time.Duration(index) * time.Second),
		}); err != nil {
			t.Fatalf("seed support access volume event %d: %v", index, err)
		}
	}
	if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "feedback_attachment.support_accessed",
		Actor:          accessaudit.PlatformAdminActor(quietAdminID),
		ResourceType:   accessaudit.ResourceFeedbackAttachment,
		ResourceID:     uuidFromIndex(5),
		Action:         "preview",
		PermissionPath: accessaudit.PermissionPathSupportAccess,
		Outcome:        accessaudit.OutcomeSuccess,
		Now:            now,
	}); err != nil {
		t.Fatalf("seed quiet support access event: %v", err)
	}
	if _, err := database.Exec(ctx, `DELETE FROM platform_admins WHERE id = $1`, frequentAdminID); err != nil {
		t.Fatalf("delete frequent support admin before anomaly scan: %v", err)
	}

	signals, err := store.DetectSupportAccessVolume(ctx, database, tenantID, accessaudit.SupportAccessVolumeParams{
		Window: time.Hour, Threshold: 5, Now: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("DetectSupportAccessVolume() error = %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("DetectSupportAccessVolume() = %+v, want exactly one flagged platform admin", signals)
	}
	signal := signals[0]
	if signal.ActorID != frequentAdminID || signal.Type != accessaudit.SignalSupportAccessVolume ||
		signal.ActorType != accessaudit.ActorPlatformAdmin || signal.EventCount != 5 || signal.DistinctResources != 5 {
		t.Fatalf("DetectSupportAccessVolume() = %+v, want the frequent platform admin flagged with count 5", signal)
	}
	for _, other := range signals {
		if other.ActorID == quietAdminID {
			t.Fatalf("DetectSupportAccessVolume() flagged an actor below threshold: %+v", other)
		}
	}
}

func TestActorProvenanceSurvivesIdentityDeletion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := accessAuditTestDatabase(t, ctx)
	store := accessaudit.Store{}
	tenantID := seedAccessAuditTenant(t, ctx, database, "deleted-actor-audit")
	memberID := seedAccessAuditMember(t, ctx, database, tenantID)
	apiKeyID := seedAccessAuditAPIKey(t, ctx, database, tenantID)
	adminID := seedAccessAuditPlatformAdmin(t, ctx, database)

	fixtures := []struct {
		actor        accessaudit.Actor
		actorType    string
		actorID      string
		resourceID   string
		resourceType string
		kind         string
		permission   string
		deleteQuery  string
	}{
		{
			actor:     accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
			actorType: accessaudit.ActorHuman, actorID: memberID,
			resourceID: "77777777-7777-7777-7777-777777777777", resourceType: accessaudit.ResourceFeedbackAttachment,
			kind: "feedback_attachment.accessed", permission: "feedback:read",
			deleteQuery: `DELETE FROM tenant_members WHERE id = $1`,
		},
		{
			actor:     accessaudit.Actor{Type: accessaudit.ActorAPIKey, APIKeyID: apiKeyID},
			actorType: accessaudit.ActorAPIKey, actorID: apiKeyID,
			resourceID: "88888888-8888-8888-8888-888888888888", resourceType: accessaudit.ResourceReplaySession,
			kind: "replay_session.accessed", permission: "session:read",
			deleteQuery: `DELETE FROM api_keys WHERE id = $1`,
		},
		{
			actor:     accessaudit.PlatformAdminActor(adminID),
			actorType: accessaudit.ActorPlatformAdmin, actorID: adminID,
			resourceID: "99999999-9999-9999-9999-999999999999", resourceType: accessaudit.ResourceFeedbackAttachment,
			kind:        "feedback_attachment.support_accessed",
			permission:  accessaudit.PermissionPathSupportAccess,
			deleteQuery: `DELETE FROM platform_admins WHERE id = $1`,
		},
	}

	for _, fixture := range fixtures {
		if err := accessaudit.Record(ctx, database, accessaudit.RecordInput{
			TenantID: tenantID, Kind: fixture.kind,
			Actor:          fixture.actor,
			ResourceType:   fixture.resourceType,
			ResourceID:     fixture.resourceID,
			Action:         "preview",
			PermissionPath: fixture.permission,
			Outcome:        accessaudit.OutcomeSuccess,
		}); err != nil {
			t.Fatalf("record %s access: %v", fixture.actorType, err)
		}
		if _, err := database.Exec(ctx, fixture.deleteQuery, fixture.actorID); err != nil {
			t.Fatalf("delete %s identity: %v", fixture.actorType, err)
		}
		events, err := store.ListEvents(ctx, database, tenantID, accessaudit.ListFilter{
			ActorType: fixture.actorType,
		})
		if err != nil {
			t.Fatalf("ListEvents(%s) error = %v", fixture.actorType, err)
		}
		if len(events) != 1 {
			t.Fatalf("ListEvents(%s) returned %d events, want 1", fixture.actorType, len(events))
		}
		if events[0].ActorType != fixture.actorType ||
			events[0].ActorID == nil || *events[0].ActorID != fixture.actorID {
			t.Fatalf("deleted %s provenance = %+v, want actor %s", fixture.actorType, events[0], fixture.actorID)
		}
		if events[0].ActorMemberID != nil || events[0].ActorAPIKeyID != nil ||
			events[0].ActorPlatformAdminID != nil {
			t.Fatalf("deleted %s live references were not cleared: %+v", fixture.actorType, events[0])
		}
	}
}

// TestConcurrentDedupUnderLoad exercises the dedup unique index under real
// concurrent write pressure (as opposed to the sequential dedup coverage in
// TestRecordAndListAccessEvents), matching the load this endpoint sees from
// replay chunk polling or attachment range-request retries: many concurrent
// requests for the same actor/resource/action landing in the same dedup
// bucket. Every concurrent Record call must return nil (ON CONFLICT DO
// NOTHING, never a unique-violation error), and exactly one row must survive.
func TestConcurrentDedupUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := accessAuditTestDatabase(t, ctx)
	tenantID := seedAccessAuditTenant(t, ctx, database, "concurrent-dedup-audit")
	memberID := seedAccessAuditMember(t, ctx, database, tenantID)
	apiKeyID := seedAccessAuditAPIKey(t, ctx, database, tenantID)
	now := time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC)

	const concurrency = 40
	const sharedResourceID = "77777777-7777-7777-7777-777777777777"
	const distinctResourceIDPrefix = "88888888-8888-8888-8888-88888888880"

	errs := make(chan error, concurrency*2)
	var wg sync.WaitGroup

	// All of these race to insert the *same* dedup bucket: only one should win.
	for range concurrency {
		wg.Go(func() {
			errs <- accessaudit.Record(ctx, database, accessaudit.RecordInput{
				TenantID: tenantID, Kind: "feedback_attachment.accessed",
				Actor:          accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
				ResourceType:   accessaudit.ResourceFeedbackAttachment,
				ResourceID:     sharedResourceID,
				Action:         "preview",
				PermissionPath: "feedback:read",
				Outcome:        accessaudit.OutcomeSuccess,
				DedupWindow:    time.Minute,
				Now:            now,
			})
		})
	}
	// Concurrently, these fan out across 10 distinct resources (4 racing
	// writers per resource) - proves the dedup index scopes per resource_id
	// under concurrent load rather than collapsing everything together.
	for index := range concurrency {
		wg.Go(func() {
			resourceID := distinctResourceIDPrefix + string(rune('0'+index%10))
			errs <- accessaudit.Record(ctx, database, accessaudit.RecordInput{
				TenantID: tenantID, Kind: "replay_session.accessed",
				Actor:        accessaudit.Actor{Type: accessaudit.ActorAPIKey, APIKeyID: apiKeyID},
				ResourceType: accessaudit.ResourceReplaySession,
				ResourceID:   resourceID,
				Action:       "playback",
				Outcome:      accessaudit.OutcomeSuccess,
				DedupWindow:  time.Minute,
				Now:          now.Add(time.Duration(index) * time.Millisecond),
			})
		})
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Record() returned an error instead of deduplicating: %v", err)
		}
	}

	var sharedCount int
	if err := database.QueryRow(ctx, `
		SELECT count(*) FROM tenant_access_events
		WHERE tenant_id = $1 AND resource_type = $2 AND resource_id = $3
	`, tenantID, accessaudit.ResourceFeedbackAttachment, sharedResourceID).Scan(&sharedCount); err != nil {
		t.Fatalf("count shared-resource rows: %v", err)
	}
	if sharedCount != 1 {
		t.Fatalf("concurrent dedup left %d rows for the same actor/resource/action/bucket, want 1", sharedCount)
	}

	var distinctCount int
	if err := database.QueryRow(ctx, `
		SELECT count(DISTINCT resource_id) FROM tenant_access_events
		WHERE tenant_id = $1 AND resource_type = $2
	`, tenantID, accessaudit.ResourceReplaySession).Scan(&distinctCount); err != nil {
		t.Fatalf("count distinct replay resources: %v", err)
	}
	if distinctCount != 10 {
		t.Fatalf("concurrent load under distinct resources produced %d distinct rows, want 10 (dedup must not cross resources)", distinctCount)
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

// seedAccessAuditMember creates an active member with a unique email (via
// gen_random_uuid()) so callers can seed more than one member per tenant.
func seedAccessAuditMember(t *testing.T, ctx context.Context, database *pgxpool.Pool, tenantID string) string {
	t.Helper()
	var memberID string
	if err := database.QueryRow(ctx, `
		INSERT INTO tenant_members (tenant_id, email, permissions, status)
		VALUES ($1, gen_random_uuid()::text || '@' || $2 || '.test', ARRAY['feedback:read', 'session:read'], 'active')
		RETURNING id
	`, tenantID, tenantID).Scan(&memberID); err != nil {
		t.Fatalf("create access-audit member: %v", err)
	}
	return memberID
}

// seedAccessAuditPlatformAdmin creates a platform admin with a unique email
// (via gen_random_uuid()) so callers can seed more than one per test.
func seedAccessAuditPlatformAdmin(t *testing.T, ctx context.Context, database *pgxpool.Pool) string {
	t.Helper()
	var adminID string
	if err := database.QueryRow(ctx, `
		INSERT INTO platform_admins (email, permissions)
		VALUES (gen_random_uuid()::text || '@fidibeki-support.test', ARRAY['platform:tenant:support_access'])
		RETURNING id
	`).Scan(&adminID); err != nil {
		t.Fatalf("create access-audit platform admin: %v", err)
	}
	return adminID
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
