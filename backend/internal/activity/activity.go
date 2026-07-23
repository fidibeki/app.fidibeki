package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	VisibilityInternal = "internal"
	VisibilityReporter = "reporter"
)

type Actor struct {
	ID    string  `json:"id"`
	Name  *string `json:"name,omitempty"`
	Email string  `json:"email"`
}

type Event struct {
	ID         string         `json:"id"`
	IssueID    string         `json:"issue_id"`
	Actor      *Actor         `json:"actor,omitempty"`
	Kind       string         `json:"kind"`
	Visibility string         `json:"visibility"`
	Data       map[string]any `json:"data"`
	CreatedAt  time.Time      `json:"created_at"`
}

type RecordInput struct {
	ID            string
	TenantID      string
	IssueID       string
	ActorMemberID *string
	Kind          string
	Visibility    string
	Data          map[string]any
	CreatedAt     time.Time
}

func Record(ctx context.Context, query db.Querier, input RecordInput) (string, error) {
	id := input.ID
	if id == "" {
		id = uuid.NewString()
	}
	visibility := input.Visibility
	if visibility == "" {
		visibility = VisibilityInternal
	}
	data := input.Data
	if data == nil {
		data = map[string]any{}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("encode issue activity data: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO issue_activities (
			id, tenant_id, issue_id, actor_member_id, kind, visibility, data, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING
	`, id, input.TenantID, input.IssueID, input.ActorMemberID, input.Kind,
		visibility, string(payload), input.CreatedAt.UTC()); err != nil {
		return "", fmt.Errorf("record issue activity: %w", err)
	}
	return id, nil
}

func ListIssue(ctx context.Context, query db.Querier, tenantID, issueID string) ([]Event, error) {
	rows, err := query.Query(ctx, activitySelect+`
		WHERE activity.tenant_id = $1 AND activity.issue_id = $2
		ORDER BY activity.created_at, activity.id
	`, tenantID, issueID)
	if err != nil {
		return nil, fmt.Errorf("list issue activities: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		event, scanErr := scanEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue activities: %w", err)
	}
	return events, nil
}

func GetIssue(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	issueID string,
	eventID string,
) (Event, error) {
	event, err := scanEvent(query.QueryRow(ctx, activitySelect+`
		WHERE activity.tenant_id = $1 AND activity.issue_id = $2 AND activity.id = $3
	`, tenantID, issueID, eventID))
	if err != nil {
		return Event{}, fmt.Errorf("get issue activity: %w", err)
	}
	return event, nil
}

const activitySelect = `
	SELECT activity.id, activity.issue_id, activity.kind, activity.visibility,
	       activity.data, activity.created_at,
	       actor.id, actor.name, actor.email::text
	FROM issue_activities AS activity
	LEFT JOIN tenant_members AS actor
	  ON actor.tenant_id = activity.tenant_id AND actor.id = activity.actor_member_id
`

type rowScanner interface {
	Scan(...any) error
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	var payload []byte
	var actorID, actorName, actorEmail *string
	if err := row.Scan(
		&event.ID, &event.IssueID, &event.Kind, &event.Visibility,
		&payload, &event.CreatedAt, &actorID, &actorName, &actorEmail,
	); err != nil {
		if err == pgx.ErrNoRows {
			return Event{}, err
		}
		return Event{}, fmt.Errorf("scan issue activity: %w", err)
	}
	if err := json.Unmarshal(payload, &event.Data); err != nil {
		return Event{}, fmt.Errorf("decode issue activity data: %w", err)
	}
	if actorID != nil && actorEmail != nil {
		event.Actor = &Actor{ID: *actorID, Name: actorName, Email: *actorEmail}
	}
	return event, nil
}
