package breachregister

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func insertContainmentChecklist(
	ctx context.Context,
	query db.Querier,
	incidentID string,
	actorPlatformAdminID string,
	now time.Time,
) error {
	for _, control := range containmentControls {
		area := control.Area
		idempotencyKey := "seed:containment:" + string(area)
		fingerprint := sha256.Sum256([]byte(idempotencyKey + ":" + control.Title))
		if _, err := query.Exec(ctx, `
			INSERT INTO breach_action_events (
				id, breach_incident_id, action_id, revision, action_kind,
				containment_area, title, status, evidence_references,
				actor_platform_admin_id, idempotency_key, request_fingerprint,
				occurred_at, created_at
			) VALUES (
				$1, $2, $3, 1, 'containment', $4, $5, 'pending', '{}',
				$6, $7, $8, $9, $9
			)
		`, uuid.NewString(), incidentID, uuid.NewString(), area, control.Title,
			actorPlatformAdminID, idempotencyKey, fingerprint[:], now); err != nil {
			return fmt.Errorf("seed breach containment action %s: %w", area, err)
		}
	}
	return nil
}

func (s Store) CreatePostIncidentAction(
	ctx context.Context,
	incidentID string,
	actorPlatformAdminID string,
	idempotencyKey string,
	input CreatePostIncidentActionInput,
) (ActionEvent, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !isUUID(incidentID) || idempotencyKey == "" || len(idempotencyKey) > 200 ||
		strings.TrimSpace(actorPlatformAdminID) == "" {
		return ActionEvent{}, ErrInvalidRequest
	}
	now := s.now().UTC()
	normalized, err := normalizeCreatePostIncidentActionInput(input)
	if err != nil {
		return ActionEvent{}, err
	}
	fingerprint, err := createPostIncidentActionFingerprint(normalized)
	if err != nil {
		return ActionEvent{}, err
	}

	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return ActionEvent{}, fmt.Errorf("begin post-incident action creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockIncident(ctx, tx, incidentID); err != nil {
		return ActionEvent{}, err
	}
	if existing, storedFingerprint, found, err := actionEventByIdempotencyKey(
		ctx, tx, incidentID, idempotencyKey,
	); err != nil {
		return ActionEvent{}, err
	} else if found {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return ActionEvent{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ActionEvent{}, fmt.Errorf("commit post-incident action idempotent read: %w", err)
		}
		return existing, nil
	}
	if !normalized.DueAt.After(now) {
		return ActionEvent{}, ErrInvalidRequest
	}
	if err := requirePlatformAdminExists(ctx, tx, normalized.OwnerPlatformAdminID); err != nil {
		return ActionEvent{}, err
	}

	action, err := scanActionEvent(tx.QueryRow(ctx, `
		INSERT INTO breach_action_events (
			id, breach_incident_id, action_id, revision, action_kind, containment_area,
			title, status, owner_platform_admin_id, due_at, outcome_summary,
			corrective_reference, evidence_references, actor_platform_admin_id,
			idempotency_key, request_fingerprint, occurred_at, created_at
		) VALUES (
			$1, $2, $3, 1, 'post_incident', NULL, $4, 'pending', $5, $6, NULL,
			$7, '{}', $8, $9, $10, $11, $11
		)
		RETURNING `+actionEventSelectColumns("")+`
	`, uuid.NewString(), incidentID, uuid.NewString(), normalized.Title,
		normalized.OwnerPlatformAdminID, normalized.DueAt, normalized.CorrectiveReference,
		actorPlatformAdminID, idempotencyKey, fingerprint[:], now))
	if err != nil {
		return ActionEvent{}, fmt.Errorf("create post-incident action: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionEvent{}, fmt.Errorf("commit post-incident action creation: %w", err)
	}
	return action, nil
}

func (s Store) AppendActionRevision(
	ctx context.Context,
	incidentID string,
	actionID string,
	actorPlatformAdminID string,
	idempotencyKey string,
	input AppendActionRevisionInput,
) (ActionEvent, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !isUUID(incidentID) || !isUUID(actionID) || idempotencyKey == "" ||
		len(idempotencyKey) > 200 || strings.TrimSpace(actorPlatformAdminID) == "" {
		return ActionEvent{}, ErrInvalidRequest
	}
	normalized, err := normalizeAppendActionRevisionInput(input)
	if err != nil {
		return ActionEvent{}, err
	}
	fingerprint, err := appendActionRevisionFingerprint(actionID, normalized)
	if err != nil {
		return ActionEvent{}, err
	}
	now := s.now().UTC()

	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return ActionEvent{}, fmt.Errorf("begin breach action revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockIncident(ctx, tx, incidentID); err != nil {
		return ActionEvent{}, err
	}
	if existing, storedFingerprint, found, err := actionEventByIdempotencyKey(
		ctx, tx, incidentID, idempotencyKey,
	); err != nil {
		return ActionEvent{}, err
	} else if found {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return ActionEvent{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ActionEvent{}, fmt.Errorf("commit breach action revision idempotent read: %w", err)
		}
		return existing, nil
	}

	// No FOR UPDATE here: fidibeki_admin deliberately has no UPDATE grant on
	// the append-only ledger, so a row lock is a permission error at runtime.
	// lockIncident above already serializes every writer for this incident,
	// which keeps the expected_revision check race-free.
	current, err := scanActionEvent(tx.QueryRow(ctx, `
		SELECT `+actionEventSelectColumns("event")+`
		FROM breach_action_events AS event
		WHERE event.breach_incident_id = $1 AND event.action_id = $2
		ORDER BY event.revision DESC
		LIMIT 1
	`, incidentID, actionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionEvent{}, ErrNotFound
	}
	if err != nil {
		return ActionEvent{}, fmt.Errorf("lock current breach action revision: %w", err)
	}
	if current.Revision != normalized.ExpectedRevision {
		return ActionEvent{}, ErrRevisionConflict
	}
	if !validActionTransition(current.Status, normalized.Status) {
		return ActionEvent{}, ErrInvalidRequest
	}
	if current.ActionKind == ActionPostIncident &&
		(normalized.OwnerPlatformAdminID == nil || normalized.DueAt == nil) {
		return ActionEvent{}, ErrInvalidRequest
	}
	if normalized.OwnerPlatformAdminID != nil {
		if err := requirePlatformAdminExists(ctx, tx, *normalized.OwnerPlatformAdminID); err != nil {
			return ActionEvent{}, err
		}
	}

	action, err := scanActionEvent(tx.QueryRow(ctx, `
		INSERT INTO breach_action_events (
			id, breach_incident_id, action_id, revision, action_kind, containment_area,
			title, status, owner_platform_admin_id, due_at, outcome_summary,
			corrective_reference, evidence_references, actor_platform_admin_id,
			idempotency_key, request_fingerprint, occurred_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $17
		)
		RETURNING `+actionEventSelectColumns("")+`
	`, uuid.NewString(), incidentID, actionID, current.Revision+1, current.ActionKind,
		current.ContainmentArea, current.Title, normalized.Status,
		normalized.OwnerPlatformAdminID, normalized.DueAt, normalized.OutcomeSummary,
		normalized.CorrectiveReference, normalized.EvidenceReferences,
		actorPlatformAdminID, idempotencyKey, fingerprint[:], now))
	if err != nil {
		return ActionEvent{}, fmt.Errorf("append breach action revision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ActionEvent{}, fmt.Errorf("commit breach action revision: %w", err)
	}
	return action, nil
}

func (s Store) listActions(
	ctx context.Context, query db.Querier, incidentID string,
) ([]ActionEvent, []ActionEvent, error) {
	currentRows, err := query.Query(ctx, `
		SELECT `+actionEventSelectColumns("current")+`
		FROM (
			SELECT DISTINCT ON (event.action_id) event.*
			FROM breach_action_events AS event
			WHERE event.breach_incident_id = $1
			ORDER BY event.action_id, event.revision DESC
		) AS current
		ORDER BY current.action_kind, current.containment_area NULLS LAST,
		         current.title, current.action_id
	`, incidentID)
	if err != nil {
		return nil, nil, fmt.Errorf("list current breach actions: %w", err)
	}
	actions, err := scanActionEventRows(currentRows)
	if err != nil {
		return nil, nil, fmt.Errorf("scan current breach actions: %w", err)
	}

	eventRows, err := query.Query(ctx, `
		SELECT `+actionEventSelectColumns("event")+`
		FROM breach_action_events AS event
		WHERE event.breach_incident_id = $1
		ORDER BY event.sequence
	`, incidentID)
	if err != nil {
		return nil, nil, fmt.Errorf("list breach action history: %w", err)
	}
	events, err := scanActionEventRows(eventRows)
	if err != nil {
		return nil, nil, fmt.Errorf("scan breach action history: %w", err)
	}
	return actions, events, nil
}

func scanActionEventRows(rows pgx.Rows) ([]ActionEvent, error) {
	defer rows.Close()
	events := make([]ActionEvent, 0)
	for rows.Next() {
		event, err := scanActionEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func lockIncident(ctx context.Context, query db.Querier, incidentID string) error {
	var found string
	if err := query.QueryRow(ctx, `
		SELECT id FROM breach_incidents WHERE id = $1 FOR UPDATE
	`, incidentID).Scan(&found); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock breach incident: %w", err)
	}
	return nil
}

func requirePlatformAdminExists(
	ctx context.Context, query db.Querier, platformAdminID string,
) error {
	var exists bool
	if err := query.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM platform_admins WHERE id = $1)
	`, platformAdminID).Scan(&exists); err != nil {
		return fmt.Errorf("verify breach action owner: %w", err)
	}
	if !exists {
		return ErrInvalidRequest
	}
	return nil
}

func actionEventByIdempotencyKey(
	ctx context.Context,
	query db.Querier,
	incidentID string,
	idempotencyKey string,
) (ActionEvent, []byte, bool, error) {
	var fingerprint []byte
	event, err := scanActionEvent(query.QueryRow(ctx, `
		SELECT `+actionEventSelectColumns("event")+`, event.request_fingerprint
		FROM breach_action_events AS event
		WHERE event.breach_incident_id = $1 AND event.idempotency_key = $2
	`, incidentID, idempotencyKey), &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionEvent{}, nil, false, nil
	}
	if err != nil {
		return ActionEvent{}, nil, false, fmt.Errorf("read breach action idempotency key: %w", err)
	}
	return event, fingerprint, true, nil
}

func createPostIncidentActionFingerprint(
	input CreatePostIncidentActionInput,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		Operation string                        `json:"operation"`
		Input     CreatePostIncidentActionInput `json:"input"`
	}{Operation: "create_post_incident_action", Input: input})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode post-incident action fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func appendActionRevisionFingerprint(
	actionID string,
	input AppendActionRevisionInput,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		Operation string                    `json:"operation"`
		ActionID  string                    `json:"action_id"`
		Input     AppendActionRevisionInput `json:"input"`
	}{Operation: "append_action_revision", ActionID: actionID, Input: input})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode breach action revision fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func actionEventSelectColumns(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return prefix + `id, ` + prefix + `sequence, ` + prefix + `breach_incident_id, ` +
		prefix + `action_id, ` + prefix + `revision, ` + prefix + `action_kind, ` +
		prefix + `containment_area, ` + prefix + `title, ` + prefix + `status, ` +
		prefix + `owner_platform_admin_id, ` + prefix + `due_at, ` +
		prefix + `outcome_summary, ` + prefix + `corrective_reference, ` +
		prefix + `evidence_references, ` + prefix + `actor_platform_admin_id, ` +
		prefix + `occurred_at, ` + prefix + `created_at`
}

func scanActionEvent(row rowScanner, extra ...any) (ActionEvent, error) {
	var event ActionEvent
	destinations := []any{
		&event.ID, &event.Sequence, &event.BreachIncidentID, &event.ActionID,
		&event.Revision, &event.ActionKind, &event.ContainmentArea, &event.Title,
		&event.Status, &event.OwnerPlatformAdminID, &event.DueAt, &event.OutcomeSummary,
		&event.CorrectiveReference, &event.EvidenceReferences, &event.ActorPlatformAdminID,
		&event.OccurredAt, &event.CreatedAt,
	}
	destinations = append(destinations, extra...)
	err := row.Scan(destinations...)
	return event, err
}
