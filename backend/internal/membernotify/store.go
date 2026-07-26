package membernotify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/accessaudit"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

// ErrMemberNotFound is returned when the acting member is not an active member
// of the resolved workspace.
var ErrMemberNotFound = errors.New("member not found")

// Delivery statuses recorded on member_digest_deliveries. Only sent and
// suppressed advance the cursor; claimed and failed leave it in place so no
// window is skipped after an interrupted run.
const (
	StatusClaimed    = "claimed"
	StatusSent       = "sent"
	StatusSuppressed = "suppressed"
	StatusFailed     = "failed"
)

// Store reads and writes notification preferences and the digest ledger. Tenant
// requests pass a tenant-scoped querier; the scheduler passes the admin pool
// because it works across workspaces.
type Store struct{}

// Delivery is one recorded digest attempt, exposed to the owning member as
// delivery diagnostics for their own mail.
type Delivery struct {
	Cadence     string     `json:"cadence"`
	Status      string     `json:"status"`
	EventCount  int        `json:"event_count"`
	WindowStart time.Time  `json:"window_start"`
	WindowEnd   time.Time  `json:"window_end"`
	Reason      *string    `json:"reason,omitempty"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Get returns the member's stored preferences, or the low-noise defaults when
// the member has never saved any.
func (Store) Get(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) (Preferences, error) {
	defaults := Defaults()
	preferences := defaults
	var updatedAt *time.Time
	err := query.QueryRow(ctx, `
		SELECT COALESCE(preference.assignments, $3),
		       COALESCE(preference.mentions, $4),
		       COALESCE(preference.comments, $5),
		       COALESCE(preference.issue_changes, $6),
		       COALESCE(preference.feedback_summary, $7),
		       COALESCE(preference.digest_hour, $8),
		       COALESCE(preference.digest_weekday, $9),
		       COALESCE(member.timezone, tenant.default_timezone, 'UTC'),
		       CASE
		           WHEN member.timezone IS NOT NULL THEN 'member'
		           WHEN tenant.default_timezone IS NOT NULL THEN 'workspace'
		           ELSE 'default'
		       END,
		       preference.updated_at
		FROM tenant_members AS member
		JOIN tenants AS tenant ON tenant.id = member.tenant_id
		LEFT JOIN member_notification_preferences AS preference
		  ON preference.tenant_id = member.tenant_id AND preference.member_id = member.id
		WHERE member.tenant_id = $1 AND member.id = $2 AND member.status = 'active'
	`, tenantID, memberID,
		defaults.Assignments, defaults.Mentions, defaults.Comments,
		defaults.IssueChanges, defaults.FeedbackSummary,
		defaults.DigestHour, defaults.DigestWeekday,
	).Scan(
		&preferences.Assignments, &preferences.Mentions, &preferences.Comments,
		&preferences.IssueChanges, &preferences.FeedbackSummary,
		&preferences.DigestHour, &preferences.DigestWeekday,
		&preferences.Timezone, &preferences.TimezoneSource, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preferences{}, ErrMemberNotFound
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("load member notification preferences: %w", err)
	}
	preferences.UpdatedAt = updatedAt
	return preferences, nil
}

// Save persists a validated preference set and, when the request carried one, the
// member's timezone override. The change is audited in the same transaction, so
// "preferences changed" and "the change was recorded" commit together.
func (s Store) Save(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	next Preferences,
	timezone *string,
	setTimezone bool,
	now time.Time,
) (Preferences, error) {
	before, err := s.Get(ctx, query, tenantID, memberID)
	if err != nil {
		return Preferences{}, err
	}
	if setTimezone {
		if _, err := query.Exec(ctx, `
			UPDATE tenant_members SET timezone = $3
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		`, tenantID, memberID, timezone); err != nil {
			return Preferences{}, fmt.Errorf("update member digest timezone: %w", err)
		}
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO member_notification_preferences (
			tenant_id, member_id, assignments, mentions, comments, issue_changes,
			feedback_summary, digest_hour, digest_weekday, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, member_id) DO UPDATE SET
			assignments = EXCLUDED.assignments,
			mentions = EXCLUDED.mentions,
			comments = EXCLUDED.comments,
			issue_changes = EXCLUDED.issue_changes,
			feedback_summary = EXCLUDED.feedback_summary,
			digest_hour = EXCLUDED.digest_hour,
			digest_weekday = EXCLUDED.digest_weekday,
			updated_at = EXCLUDED.updated_at
	`, tenantID, memberID, next.Assignments, next.Mentions, next.Comments,
		next.IssueChanges, next.FeedbackSummary, next.DigestHour, next.DigestWeekday,
		now.UTC()); err != nil {
		return Preferences{}, fmt.Errorf("save member notification preferences: %w", err)
	}
	saved, err := s.Get(ctx, query, tenantID, memberID)
	if err != nil {
		return Preferences{}, err
	}
	if err := accessaudit.Record(ctx, query, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "notification_preferences.updated",
		Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
		ResourceType: "notification_preference", ResourceID: memberID,
		Action: "update", Outcome: accessaudit.OutcomeSuccess,
		Data: AuditData(before, saved), Now: now,
	}); err != nil {
		return Preferences{}, err
	}
	return saved, nil
}

// RecordUnsubscribe turns every optional category off from a signed one-click
// link. The actor is the member themselves; the audit entry marks the channel so
// an emailed opt-out is distinguishable from a settings change.
func (s Store) RecordUnsubscribe(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	now time.Time,
) error {
	before, err := s.Get(ctx, query, tenantID, memberID)
	if err != nil {
		return err
	}
	silenced := before
	silenced.Assignments = ModeOff
	silenced.Mentions = ModeOff
	silenced.Comments = ModeOff
	silenced.IssueChanges = ModeOff
	silenced.FeedbackSummary = ModeOff
	if _, err := query.Exec(ctx, `
		INSERT INTO member_notification_preferences (
			tenant_id, member_id, assignments, mentions, comments, issue_changes,
			feedback_summary, digest_hour, digest_weekday, updated_at
		) VALUES ($1, $2, 'off', 'off', 'off', 'off', 'off', $3, $4, $5)
		ON CONFLICT (tenant_id, member_id) DO UPDATE SET
			assignments = 'off', mentions = 'off', comments = 'off',
			issue_changes = 'off', feedback_summary = 'off',
			updated_at = EXCLUDED.updated_at
	`, tenantID, memberID, before.DigestHour, before.DigestWeekday, now.UTC()); err != nil {
		return fmt.Errorf("apply member notification unsubscribe: %w", err)
	}
	return accessaudit.Record(ctx, query, accessaudit.RecordInput{
		TenantID: tenantID, Kind: "notification_preferences.unsubscribed",
		Actor:        accessaudit.Actor{Type: accessaudit.ActorHuman, MemberID: memberID},
		ResourceType: "notification_preference", ResourceID: memberID,
		Action: "unsubscribe", Outcome: accessaudit.OutcomeSuccess,
		Data: map[string]any{"channel": "email_link", "changes": AuditData(before, silenced)},
		Now:  now,
	})
}

// RecentDeliveries returns the member's own digest delivery ledger, newest
// first, so suppressed and failed sends stay visible to the person affected.
func (Store) RecentDeliveries(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
	limit int,
) ([]Delivery, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := query.Query(ctx, `
		SELECT cadence, status, event_count, window_start, window_end,
		       error_message, sent_at, created_at
		FROM member_digest_deliveries
		WHERE tenant_id = $1 AND member_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, tenantID, memberID, limit)
	if err != nil {
		return nil, fmt.Errorf("list member digest deliveries: %w", err)
	}
	defer rows.Close()
	deliveries := make([]Delivery, 0)
	for rows.Next() {
		var delivery Delivery
		if err := rows.Scan(
			&delivery.Cadence, &delivery.Status, &delivery.EventCount,
			&delivery.WindowStart, &delivery.WindowEnd, &delivery.Reason,
			&delivery.SentAt, &delivery.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan member digest delivery: %w", err)
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate member digest deliveries: %w", err)
	}
	return deliveries, nil
}
