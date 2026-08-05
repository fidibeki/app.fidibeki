package replay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
)

type SessionListCursor struct {
	StartedAt time.Time
	ID        string
}

type SessionListOptions struct {
	ProjectID string
	Status    string
	Feedback  string
	Limit     int
	Cursor    *SessionListCursor
}

func parseTenantSessionListOptions(r *http.Request) (SessionListOptions, map[string]string) {
	query := r.URL.Query()
	options := SessionListOptions{Limit: 50}
	fields := make(map[string]string)

	options.ProjectID = query.Get("project_id")
	if options.ProjectID != "" {
		if _, err := uuid.Parse(options.ProjectID); err != nil {
			fields["project_id"] = "must be a UUID"
		}
	}
	if raw := query.Get("status"); raw != "" {
		switch raw {
		case "recording", "processing", "ready", "failed", "expired":
			options.Status = raw
		default:
			fields["status"] = "must be recording, processing, ready, failed, or expired"
		}
	}
	if raw := query.Get("feedback"); raw != "" {
		if raw != "linked" && raw != "unlinked" {
			fields["feedback"] = "must be linked or unlinked"
		} else {
			options.Feedback = raw
		}
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			fields["limit"] = "must be an integer from 1 to 100"
		} else {
			options.Limit = limit
		}
	}
	if raw := query.Get("cursor"); raw != "" {
		cursor, err := decodeTenantSessionCursor(raw)
		if err != nil {
			fields["cursor"] = "is invalid"
		} else {
			options.Cursor = &cursor
		}
	}
	return options, fields
}

const sessionStatusExpression = `CASE
	WHEN session.retention_deleted_at IS NOT NULL OR session.replay_privacy_version < %[1]s THEN 'expired'
	WHEN session.merged_at IS NOT NULL AND session.storage_key IS NOT NULL THEN 'ready'
	WHEN session.ended_at IS NOT NULL AND session.failure_code IS NOT NULL THEN 'failed'
	WHEN session.ended_at IS NOT NULL OR session.finalize_requested_at IS NOT NULL THEN 'processing'
	ELSE 'recording'
END`

func (Store) ListSessions(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	options SessionListOptions,
) ([]Session, error) {
	arguments := []any{tenantID}
	conditions := []string{"session.tenant_id = $1"}
	appendArgument := func(value any) string {
		arguments = append(arguments, value)
		return fmt.Sprintf("$%d", len(arguments))
	}
	if options.ProjectID != "" {
		conditions = append(conditions, "session.project_id = "+appendArgument(options.ProjectID))
	}
	if options.Status != "" {
		expression := fmt.Sprintf(
			sessionStatusExpression, appendArgument(CurrentReplayPrivacyVersion),
		)
		conditions = append(conditions, expression+" = "+appendArgument(options.Status))
	}
	switch options.Feedback {
	case "linked":
		conditions = append(conditions, "session.feedback_id IS NOT NULL")
	case "unlinked":
		conditions = append(conditions, "session.feedback_id IS NULL")
	}
	if options.Cursor != nil {
		conditions = append(conditions, fmt.Sprintf(
			"(session.started_at, session.id) < (%s::timestamptz, %s::uuid)",
			appendArgument(options.Cursor.StartedAt.UTC()), appendArgument(options.Cursor.ID),
		))
	}
	statement := sessionSelect + "WHERE " + strings.Join(conditions, " AND ") +
		" ORDER BY session.started_at DESC, session.id DESC LIMIT " +
		appendArgument(options.Limit+1)
	rows, err := query.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list tenant replay sessions: %w", err)
	}
	defer rows.Close()
	sessions := make([]Session, 0)
	for rows.Next() {
		found, scanErr := scanSessionRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sessions = append(sessions, found)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant replay sessions: %w", err)
	}
	return sessions, nil
}

type tenantSessionCursorEnvelope struct {
	Version   int       `json:"v"`
	StartedAt time.Time `json:"started_at"`
	ID        string    `json:"id"`
}

func encodeTenantSessionCursor(session Session) (string, error) {
	payload, err := json.Marshal(tenantSessionCursorEnvelope{
		Version: 1, StartedAt: session.StartedAt.UTC(), ID: session.ID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeTenantSessionCursor(value string) (SessionListCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return SessionListCursor{}, err
	}
	var envelope tenantSessionCursorEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return SessionListCursor{}, err
	}
	if envelope.Version != 1 || envelope.StartedAt.IsZero() {
		return SessionListCursor{}, errors.New("invalid replay session cursor")
	}
	if _, err := uuid.Parse(envelope.ID); err != nil {
		return SessionListCursor{}, errors.New("invalid replay session cursor")
	}
	return SessionListCursor{StartedAt: envelope.StartedAt.UTC(), ID: envelope.ID}, nil
}
