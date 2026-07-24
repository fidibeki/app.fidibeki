// Package accessaudit extends the append-only tenant_access_events ledger
// (introduced for workspace-ownership events, see internal/platformadmin)
// with a shared contract for recording sensitive-data access: attachment
// streaming, replay playback, and privacy export downloads. It never stores
// the underlying evidence, raw credentials, signed URLs, or reporter emails —
// only who accessed what resource, how, and whether it succeeded.
package accessaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

const (
	ActorHuman         = "human"
	ActorAPIKey        = "api_key"
	ActorPlatformAdmin = "platform_admin"
	ActorSystem        = "system"
)

const (
	OutcomeSuccess  = "success"
	OutcomeDenied   = "denied"
	OutcomeNotFound = "not_found"
)

const (
	ResourceFeedbackAttachment = "feedback_attachment"
	ResourceReplaySession      = "replay_session"
	ResourcePrivacyExport      = "privacy_export"
)

// DefaultDedupWindow bounds how many access events a single actor can
// generate for the same tenant/resource/action combination. Repeated reads
// within the same window (e.g. replay chunk polling, attachment range
// requests) collapse into a single row instead of flooding the ledger.
const DefaultDedupWindow = time.Minute

// Actor identifies who performed an access, split by provenance because the
// underlying table enforces exactly one identity column per row (see
// migration 000081).
type Actor struct {
	Type            string
	MemberID        string
	APIKeyID        string
	PlatformAdminID string
}

// ActorFromClaims classifies an authenticated tenant credential as a human
// session or an API key. claims.IsAPIKey is set fresh on every request by
// middleware.AuthenticateTenantCredential and is never part of the signed
// token payload, so it cannot be forged by the caller.
func ActorFromClaims(claims auth.Claims) Actor {
	if claims.IsAPIKey {
		return Actor{Type: ActorAPIKey, APIKeyID: claims.Subject}
	}
	return Actor{Type: ActorHuman, MemberID: claims.Subject}
}

// PlatformAdminActor identifies a platform-admin/support actor.
func PlatformAdminActor(platformAdminID string) Actor {
	return Actor{Type: ActorPlatformAdmin, PlatformAdminID: platformAdminID}
}

// RecordInput describes one sensitive-data access event.
type RecordInput struct {
	TenantID string
	// Kind follows the existing tenant_access_events dotted convention, e.g.
	// "feedback_attachment.accessed".
	Kind           string
	Actor          Actor
	ResourceType   string
	ResourceID     string
	Action         string
	PermissionPath string
	RequestID      string
	Outcome        string
	// Data is small, non-sensitive metadata only (never content, credentials,
	// signed URLs, emails, or IPs).
	Data map[string]any
	// DedupWindow, when set together with ResourceID, buckets this event so a
	// duplicate within the same window is silently skipped. Zero disables
	// deduplication.
	DedupWindow time.Duration
	// Now stamps the event's server timestamp (created_at) and, when
	// DedupWindow is set, the dedup bucket. Defaults to time.Now().
	Now time.Time
}

// Record inserts a sensitive-data access event. It is safe to call inside the
// same transaction that performed the authorization check, so "authorization
// succeeded" and "access was recorded" commit atomically. If persistence
// fails, callers are expected to fail closed (deny the access) rather than
// serve content without an audit trail, matching the existing
// platformadmin/privacy precedent.
func Record(ctx context.Context, query db.Querier, input RecordInput) error {
	if input.TenantID == "" || input.Kind == "" || input.Outcome == "" || input.Actor.Type == "" {
		return errors.New("access audit event requires tenant, kind, outcome, and actor type")
	}
	switch input.Actor.Type {
	case ActorHuman:
		if input.Actor.MemberID == "" || input.Actor.APIKeyID != "" || input.Actor.PlatformAdminID != "" {
			return errors.New("human access audit actor requires exactly one member ID")
		}
	case ActorAPIKey:
		if input.Actor.APIKeyID == "" || input.Actor.MemberID != "" || input.Actor.PlatformAdminID != "" {
			return errors.New("API-key access audit actor requires exactly one API-key ID")
		}
	case ActorPlatformAdmin:
		if input.Actor.PlatformAdminID == "" || input.Actor.MemberID != "" || input.Actor.APIKeyID != "" {
			return errors.New("platform-admin access audit actor requires exactly one platform-admin ID")
		}
	case ActorSystem:
		if input.Actor.MemberID != "" || input.Actor.APIKeyID != "" || input.Actor.PlatformAdminID != "" {
			return errors.New("system access audit actor cannot carry a user identity")
		}
	default:
		return errors.New("access audit actor type is invalid")
	}
	if input.Outcome != OutcomeSuccess && input.Outcome != OutcomeDenied &&
		input.Outcome != OutcomeNotFound {
		return errors.New("access audit outcome is invalid")
	}
	if input.DedupWindow > 0 && input.DedupWindow < time.Second {
		return errors.New("access audit deduplication window must be at least one second")
	}
	data := input.Data
	if data == nil {
		data = map[string]any{}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode access audit data: %w", err)
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	var dedupBucket any
	if input.DedupWindow > 0 && input.ResourceID != "" {
		dedupBucket = strconv.FormatInt(now.Unix()/int64(input.DedupWindow.Seconds()), 10)
	}
	var id string
	err = query.QueryRow(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_type, actor_member_id, actor_api_key_id, actor_platform_admin_id,
			resource_type, resource_id, action, permission_path, request_id, outcome, dedup_bucket, data,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15
		)
		ON CONFLICT (
			tenant_id, resource_type, resource_id, actor_type,
			COALESCE(actor_member_id::text, actor_api_key_id::text, actor_platform_admin_id::text, ''),
			action, outcome, dedup_bucket
		) WHERE resource_id IS NOT NULL AND dedup_bucket IS NOT NULL
		DO NOTHING
		RETURNING id
	`,
		input.TenantID, input.Kind, input.Actor.Type,
		nullable(input.Actor.MemberID), nullable(input.Actor.APIKeyID), nullable(input.Actor.PlatformAdminID),
		nullable(input.ResourceType), nullable(input.ResourceID), nullable(input.Action),
		nullable(input.PermissionPath), nullable(input.RequestID), input.Outcome, dedupBucket, string(encoded),
		now.UTC(),
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deduplicated: an equivalent event already exists in this window.
		return nil
	}
	if err != nil {
		return fmt.Errorf("record access audit event: %w", err)
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
