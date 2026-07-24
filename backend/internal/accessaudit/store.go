package accessaudit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
)

var ErrInvalidCursor = errors.New("access audit cursor is invalid")

// Event is a tenant-visible projection of one tenant_access_events row.
// Never includes the raw credential, signed URL, page URL, or evidence
// content — only the structured provenance columns and the small data blob
// callers explicitly opted into recording.
type Event struct {
	ID                   string         `json:"id"`
	Kind                 string         `json:"kind"`
	ActorType            string         `json:"actor_type"`
	ActorMemberID        *string        `json:"actor_member_id,omitempty"`
	ActorAPIKeyID        *string        `json:"actor_api_key_id,omitempty"`
	ActorPlatformAdminID *string        `json:"actor_platform_admin_id,omitempty"`
	ResourceType         *string        `json:"resource_type,omitempty"`
	ResourceID           *string        `json:"resource_id,omitempty"`
	Action               *string        `json:"action,omitempty"`
	PermissionPath       *string        `json:"permission_path,omitempty"`
	RequestID            *string        `json:"request_id,omitempty"`
	Outcome              string         `json:"outcome"`
	Data                 map[string]any `json:"data"`
	CreatedAt            time.Time      `json:"created_at"`
}

// ListFilter narrows a tenant's access-event ledger for the admin view.
type ListFilter struct {
	ResourceType string
	ActorType    string
	Outcome      string
	From         *time.Time
	To           *time.Time
	Cursor       string
	Limit        int
	MaxLimit     int
}

const (
	defaultListLimit = 50
	maxListLimit     = 500
)

type cursorPayload struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func EncodeCursor(event Event) string {
	payload, _ := json.Marshal(cursorPayload{CreatedAt: event.CreatedAt, ID: event.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(value string) (cursorPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursorPayload{}, ErrInvalidCursor
	}
	var payload cursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.ID == "" {
		return cursorPayload{}, ErrInvalidCursor
	}
	return payload, nil
}

type Store struct{}

// ListEvents returns up to filter.Limit+1 events (caller trims and derives
// has_more), ordered newest first, matching the resource-scoped keyset
// pagination convention used elsewhere in the tenant API.
func (Store) ListEvents(ctx context.Context, query db.Querier, tenantID string, filter ListFilter) ([]Event, error) {
	maxLimit := filter.MaxLimit
	if maxLimit <= 0 {
		maxLimit = maxListLimit
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	conditions := []string{"tenant_id = $1"}
	args := []any{tenantID}
	addCondition := func(clause string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(clause, len(args)))
	}
	if filter.ResourceType != "" {
		addCondition("resource_type = $%d", filter.ResourceType)
	}
	if filter.ActorType != "" {
		addCondition("actor_type = $%d", filter.ActorType)
	}
	if filter.Outcome != "" {
		addCondition("outcome = $%d", filter.Outcome)
	}
	if filter.From != nil {
		addCondition("created_at >= $%d", filter.From.UTC())
	}
	if filter.To != nil {
		addCondition("created_at <= $%d", filter.To.UTC())
	}
	if filter.Cursor != "" {
		cursor, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, err
		}
		args = append(args, cursor.CreatedAt, cursor.ID)
		conditions = append(conditions, fmt.Sprintf(
			"(created_at, id) < ($%d, $%d)", len(args)-1, len(args),
		))
	}
	args = append(args, limit+1)
	statement := fmt.Sprintf(`
		SELECT id, kind, actor_type, actor_member_id, actor_api_key_id, actor_platform_admin_id,
		       resource_type, resource_id, action, permission_path, request_id, outcome, data, created_at
		FROM tenant_access_events
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d
	`, strings.Join(conditions, " AND "), len(args))
	rows, err := query.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list access events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0, limit+1)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan access event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list access events: %w", err)
	}
	return events, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	var data []byte
	if err := row.Scan(
		&event.ID, &event.Kind, &event.ActorType, &event.ActorMemberID, &event.ActorAPIKeyID, &event.ActorPlatformAdminID,
		&event.ResourceType, &event.ResourceID, &event.Action, &event.PermissionPath, &event.RequestID,
		&event.Outcome, &data, &event.CreatedAt,
	); err != nil {
		return Event{}, err
	}
	event.Data = map[string]any{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &event.Data); err != nil {
			return Event{}, err
		}
	}
	return event, nil
}

const (
	// SignalBulkAccess flags an actor whose recent volume of resource access
	// crosses a bounded, non-alerting threshold: bulk downloads, repeated
	// cross-resource access, or unusual denied-access volume.
	SignalBulkAccess = "bulk_access"
	// SignalRevokedCredential flags historical access by a now-inactive member.
	SignalRevokedCredential = "revoked_credential"
	// SignalUnusualAPIKey flags sensitive access by a non-expiring API key.
	SignalUnusualAPIKey = "unusual_api_key"
)

// ActorSignal is a bounded governance signal without evidence content.
type ActorSignal struct {
	Type              string    `json:"type"`
	ActorType         string    `json:"actor_type"`
	ActorID           string    `json:"actor_id"`
	EventCount        int       `json:"event_count"`
	DistinctResources int       `json:"distinct_resources,omitempty"`
	DeniedCount       int       `json:"denied_count,omitempty"`
	Detail            string    `json:"detail,omitempty"`
	LastAccessAt      time.Time `json:"last_access_at"`
}

// AnomalySignalParams bounds the anomaly scan so it never runs unbounded
// over the full ledger.
type AnomalySignalParams struct {
	Window            time.Duration
	BulkEventCount    int
	BulkResourceCount int
	DeniedCount       int
	Now               time.Time
}

func (Store) DetectAnomalies(
	ctx context.Context, query db.Querier, tenantID string, params AnomalySignalParams,
) ([]ActorSignal, error) {
	window := params.Window
	if window <= 0 {
		window = 24 * time.Hour
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	bulkEventCount := params.BulkEventCount
	if bulkEventCount <= 0 {
		bulkEventCount = 50
	}
	bulkResourceCount := params.BulkResourceCount
	if bulkResourceCount <= 0 {
		bulkResourceCount = 20
	}
	deniedCount := params.DeniedCount
	if deniedCount <= 0 {
		deniedCount = 10
	}
	rows, err := query.Query(ctx, `
		SELECT actor_type,
		       COALESCE(actor_member_id::text, actor_api_key_id::text, actor_platform_admin_id::text, '') AS actor_id,
		       count(*) AS event_count,
		       count(DISTINCT resource_id) AS distinct_resources,
		       count(*) FILTER (WHERE outcome = 'denied') AS denied_count,
		       max(created_at) AS last_access_at
		FROM tenant_access_events
		WHERE tenant_id = $1 AND created_at >= $2 AND resource_id IS NOT NULL
		GROUP BY actor_type, actor_id
		HAVING count(*) >= $3
		    OR count(DISTINCT resource_id) >= $4
		    OR count(*) FILTER (WHERE outcome = 'denied') >= $5
		ORDER BY event_count DESC
		LIMIT 50
	`, tenantID, now.Add(-window).UTC(), bulkEventCount, bulkResourceCount, deniedCount)
	if err != nil {
		return nil, fmt.Errorf("detect access anomalies: %w", err)
	}
	defer rows.Close()
	signals := make([]ActorSignal, 0)
	for rows.Next() {
		signal := ActorSignal{Type: SignalBulkAccess}
		if err := rows.Scan(
			&signal.ActorType, &signal.ActorID, &signal.EventCount, &signal.DistinctResources,
			&signal.DeniedCount, &signal.LastAccessAt,
		); err != nil {
			return nil, fmt.Errorf("scan access anomaly: %w", err)
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("detect access anomalies: %w", err)
	}
	return signals, nil
}

// RevokedCredentialParams bounds the revoked-credential scan.
type RevokedCredentialParams struct {
	Window time.Duration
	Now    time.Time
}

// DetectRevokedCredentialAccess finds recent access by members who are now inactive.
func (Store) DetectRevokedCredentialAccess(
	ctx context.Context, query db.Querier, tenantID string, params RevokedCredentialParams,
) ([]ActorSignal, error) {
	window := params.Window
	if window <= 0 {
		window = 90 * 24 * time.Hour
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	rows, err := query.Query(ctx, `
		SELECT events.actor_member_id, members.status::text,
		       count(*) AS event_count, max(events.created_at) AS last_access_at
		FROM tenant_access_events AS events
		JOIN tenant_members AS members
		  ON members.tenant_id = events.tenant_id AND members.id = events.actor_member_id
		WHERE events.tenant_id = $1 AND events.actor_type = 'human' AND members.status <> 'active'
		  AND events.created_at >= $2
		GROUP BY events.actor_member_id, members.status
		ORDER BY last_access_at DESC
		LIMIT 50
	`, tenantID, now.Add(-window).UTC())
	if err != nil {
		return nil, fmt.Errorf("detect revoked credential access: %w", err)
	}
	defer rows.Close()
	signals := make([]ActorSignal, 0)
	for rows.Next() {
		signal := ActorSignal{Type: SignalRevokedCredential, ActorType: ActorHuman}
		if err := rows.Scan(&signal.ActorID, &signal.Detail, &signal.EventCount, &signal.LastAccessAt); err != nil {
			return nil, fmt.Errorf("scan revoked credential access: %w", err)
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("detect revoked credential access: %w", err)
	}
	return signals, nil
}

// UnusualAPIKeyParams bounds the unusual-API-key scan.
type UnusualAPIKeyParams struct {
	Window time.Duration
	Now    time.Time
}

// DetectUnusualAPIKeyAccess finds recent sensitive access by non-expiring keys.
func (Store) DetectUnusualAPIKeyAccess(
	ctx context.Context, query db.Querier, tenantID string, params UnusualAPIKeyParams,
) ([]ActorSignal, error) {
	window := params.Window
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	rows, err := query.Query(ctx, `
		SELECT events.actor_api_key_id,
		       count(*) AS event_count, max(events.created_at) AS last_access_at
		FROM tenant_access_events AS events
		JOIN api_keys AS keys
		  ON keys.tenant_id = events.tenant_id AND keys.id = events.actor_api_key_id
		WHERE events.tenant_id = $1 AND events.actor_type = 'api_key' AND keys.expires_at IS NULL
		  AND events.resource_id IS NOT NULL AND events.created_at >= $2
		GROUP BY events.actor_api_key_id
		ORDER BY event_count DESC
		LIMIT 50
	`, tenantID, now.Add(-window).UTC())
	if err != nil {
		return nil, fmt.Errorf("detect unusual API key access: %w", err)
	}
	defer rows.Close()
	signals := make([]ActorSignal, 0)
	for rows.Next() {
		signal := ActorSignal{Type: SignalUnusualAPIKey, ActorType: ActorAPIKey, Detail: "no_expiration"}
		if err := rows.Scan(&signal.ActorID, &signal.EventCount, &signal.LastAccessAt); err != nil {
			return nil, fmt.Errorf("scan unusual API key access: %w", err)
		}
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("detect unusual API key access: %w", err)
	}
	return signals, nil
}
