package replay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Store struct {
	Database db.Querier
}

func (Store) CreateSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	clientSessionID string,
	pageURL string,
	startedAt time.Time,
	maxDurationSeconds int,
	maxEvents int,
) (Session, error) {
	if _, err := query.Exec(ctx, `
		INSERT INTO sessions (
			id, tenant_id, project_id, session_id, page_url, started_at,
			rrweb_version, max_duration_seconds, max_events
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9)
		ON CONFLICT (project_id, session_id) DO NOTHING
	`, uuid.NewString(), tenantID, projectID, clientSessionID, pageURL, startedAt.UTC(),
		RRWebVersion, maxDurationSeconds, maxEvents); err != nil {
		return Session{}, fmt.Errorf("create replay session: %w", err)
	}
	return getSessionByClientID(ctx, query, tenantID, projectID, clientSessionID)
}

func (Store) GetSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	sessionID string,
) (Session, error) {
	return scanSessionRow(query.QueryRow(ctx, sessionSelect+`
		WHERE session.tenant_id = $1 AND session.id = $2
	`, tenantID, sessionID))
}

func (Store) DeleteSession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	sessionID string,
) ([]string, error) {
	var storageKey *string
	err := query.QueryRow(ctx, `
		SELECT storage_key FROM sessions
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, sessionID).Scan(&storageKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock replay session for deletion: %w", err)
	}
	rows, err := query.Query(ctx, `
		SELECT storage_key FROM session_batches
		WHERE tenant_id = $1 AND session_id = $2
	`, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list replay objects for deletion: %w", err)
	}
	keys := make([]string, 0)
	if storageKey != nil {
		keys = append(keys, *storageKey)
	}
	for rows.Next() {
		var key *string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan replay object for deletion: %w", err)
		}
		if key != nil {
			keys = append(keys, *key)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate replay objects for deletion: %w", err)
	}
	rows.Close()
	tag, err := query.Exec(ctx, `DELETE FROM sessions WHERE tenant_id = $1 AND id = $2`, tenantID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("delete replay session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrNotFound
	}
	return keys, nil
}

func getSessionByClientID(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	clientSessionID string,
) (Session, error) {
	return scanSessionRow(query.QueryRow(ctx, sessionSelect+`
		WHERE session.tenant_id = $1 AND session.project_id = $2 AND session.session_id = $3
	`, tenantID, projectID, clientSessionID))
}

func (Store) DeclareBatch(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	sessionID string,
	batchIndex int,
	eventsCount int,
	sizeBytes int64,
	now time.Time,
) (Batch, string, error) {
	var endedAt *time.Time
	var maxEvents int
	err := query.QueryRow(ctx, `
		SELECT ended_at, max_events FROM sessions
		WHERE tenant_id = $1 AND project_id = $2 AND id = $3
		FOR UPDATE
	`, tenantID, projectID, sessionID).Scan(&endedAt, &maxEvents)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, "", ErrNotFound
	}
	if err != nil {
		return Batch{}, "", fmt.Errorf("lock replay session for batch: %w", err)
	}
	if endedAt != nil {
		return Batch{}, "", ErrSessionFinalized
	}
	var otherEvents int
	if err := query.QueryRow(ctx, `
		SELECT COALESCE(sum(events_count), 0) FROM session_batches
		WHERE session_id = $1 AND batch_index <> $2
	`, sessionID, batchIndex).Scan(&otherEvents); err != nil {
		return Batch{}, "", fmt.Errorf("count declared replay events: %w", err)
	}
	if otherEvents+eventsCount > maxEvents {
		return Batch{}, "", ErrEventLimit
	}
	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		return Batch{}, "", fmt.Errorf("create replay batch completion token: %w", err)
	}
	key := fmt.Sprintf("tenants/%s/sessions/%s/batches/%06d.json", tenantID, sessionID, batchIndex)
	var batch Batch
	err = query.QueryRow(ctx, `
		INSERT INTO session_batches (
			id, tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, content_type, complete_token_hash, upload_refreshed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'application/json', $9, $10)
		ON CONFLICT (session_id, batch_index) DO UPDATE
		SET events_count = EXCLUDED.events_count,
		    size_bytes = EXCLUDED.size_bytes,
		    complete_token_hash = EXCLUDED.complete_token_hash,
		    upload_refreshed_at = EXCLUDED.upload_refreshed_at
		WHERE session_batches.completed_at IS NULL
		RETURNING id, tenant_id, project_id, session_id, batch_index, storage_key,
		          events_count, size_bytes, content_type, complete_token_hash, completed_at
	`, uuid.NewString(), tenantID, projectID, sessionID, batchIndex, key,
		eventsCount, sizeBytes, tokenHash, now.UTC()).Scan(
		&batch.ID, &batch.TenantID, &batch.ProjectID, &batch.SessionID, &batch.Index,
		&batch.StorageKey, &batch.EventsCount, &batch.SizeBytes, &batch.ContentType,
		&batch.CompleteTokenHash, &batch.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, "", ErrBatchComplete
	}
	if err != nil {
		return Batch{}, "", fmt.Errorf("declare replay batch: %w", err)
	}
	return batch, rawToken, nil
}

func (Store) LockBatch(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	sessionID string,
	batchIndex int,
) (Batch, error) {
	var batch Batch
	var endedAt *time.Time
	err := query.QueryRow(ctx, `
		SELECT batch.id, batch.tenant_id, batch.project_id, batch.session_id,
		       batch.batch_index, batch.storage_key, batch.events_count, batch.size_bytes,
		       batch.content_type, batch.complete_token_hash, batch.completed_at, session.ended_at
		FROM session_batches AS batch
		JOIN sessions AS session ON session.id = batch.session_id
		WHERE batch.tenant_id = $1 AND batch.project_id = $2
		  AND batch.session_id = $3 AND batch.batch_index = $4
		FOR UPDATE OF batch
	`, tenantID, projectID, sessionID, batchIndex).Scan(
		&batch.ID, &batch.TenantID, &batch.ProjectID, &batch.SessionID, &batch.Index,
		&batch.StorageKey, &batch.EventsCount, &batch.SizeBytes, &batch.ContentType,
		&batch.CompleteTokenHash, &batch.CompletedAt, &endedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, ErrNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("lock replay batch: %w", err)
	}
	if endedAt != nil {
		return Batch{}, ErrSessionFinalized
	}
	return batch, nil
}

func (Store) CompleteBatch(
	ctx context.Context,
	query db.Querier,
	sessionID string,
	batchID string,
	now time.Time,
) error {
	tag, err := query.Exec(ctx, `
		UPDATE session_batches
		SET completed_at = $3, complete_token_hash = NULL
		WHERE id = $1 AND session_id = $2 AND completed_at IS NULL
	`, batchID, sessionID, now.UTC())
	if err != nil {
		return fmt.Errorf("complete replay batch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBatchComplete
	}
	if _, err := query.Exec(ctx, `
		UPDATE sessions
		SET events_count = aggregate.events_count,
		    batches_count = aggregate.batches_count
		FROM (
			SELECT COALESCE(sum(events_count), 0)::integer AS events_count,
			       count(*)::integer AS batches_count
			FROM session_batches WHERE session_id = $1 AND completed_at IS NOT NULL
		) AS aggregate
		WHERE sessions.id = $1
	`, sessionID); err != nil {
		return fmt.Errorf("update replay session batch totals: %w", err)
	}
	return nil
}

func (store Store) Finalize(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	sessionID string,
	now time.Time,
) (Session, bool, error) {
	var startedAt time.Time
	var endedAt *time.Time
	var maxDuration int
	err := query.QueryRow(ctx, `
		SELECT started_at, ended_at, max_duration_seconds
		FROM sessions
		WHERE tenant_id = $1 AND project_id = $2 AND id = $3
		FOR UPDATE
	`, tenantID, projectID, sessionID).Scan(&startedAt, &endedAt, &maxDuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, ErrNotFound
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("lock replay session for finalization: %w", err)
	}
	if endedAt != nil {
		found, getErr := store.GetSession(ctx, query, tenantID, sessionID)
		return found, false, getErr
	}
	var incomplete, completed int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE completed_at IS NULL),
		       count(*) FILTER (WHERE completed_at IS NOT NULL)
		FROM session_batches WHERE session_id = $1
	`, sessionID).Scan(&incomplete, &completed); err != nil {
		return Session{}, false, fmt.Errorf("inspect replay batches before finalization: %w", err)
	}
	if incomplete > 0 || completed == 0 {
		return Session{}, false, ErrIncompleteBatches
	}
	ended := now.UTC()
	maximumEnd := startedAt.Add(time.Duration(maxDuration) * time.Second)
	if ended.After(maximumEnd) {
		ended = maximumEnd
	}
	if ended.Before(startedAt) {
		ended = startedAt
	}
	if _, err := query.Exec(ctx, `
		UPDATE sessions
		SET ended_at = $4, merge_requested_at = $4,
		    merge_enqueued_at = NULL, merge_error = NULL
		WHERE tenant_id = $1 AND project_id = $2 AND id = $3
	`, tenantID, projectID, sessionID, ended); err != nil {
		return Session{}, false, fmt.Errorf("finalize replay session: %w", err)
	}
	found, err := store.GetSession(ctx, query, tenantID, sessionID)
	return found, true, err
}

func (s Store) RecoverAbandoned(ctx context.Context, staleBefore time.Time, now time.Time, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	if _, err := s.Database.Exec(ctx, `
		WITH candidates AS (
			SELECT session.id,
			       LEAST(
			           GREATEST(session.started_at, activity.last_activity_at),
			           session.started_at + session.max_duration_seconds * interval '1 second'
			       ) AS ended_at,
			       activity.last_activity_at,
			       activity.batch_count
			FROM sessions AS session
			CROSS JOIN LATERAL (
				SELECT count(*) AS batch_count,
				       COALESCE(
				           max(GREATEST(batch.upload_refreshed_at, COALESCE(batch.completed_at, batch.upload_refreshed_at))),
				           session.started_at
				       ) AS last_activity_at
				FROM session_batches AS batch
				WHERE batch.session_id = session.id
			) AS activity
			WHERE session.ended_at IS NULL AND session.started_at < $1
			  AND activity.last_activity_at < $1
			ORDER BY activity.last_activity_at, session.id
			LIMIT $2
			FOR UPDATE OF session SKIP LOCKED
		)
		UPDATE sessions AS session
		SET ended_at = candidates.ended_at,
		    merge_requested_at = CASE WHEN candidates.batch_count > 0 THEN $3::timestamptz ELSE NULL END,
		    merge_enqueued_at = NULL,
		    merge_error = CASE WHEN candidates.batch_count = 0 THEN $4 ELSE NULL END,
		    merge_attempts = CASE WHEN candidates.batch_count = 0 THEN $5 ELSE session.merge_attempts END
		FROM candidates
		WHERE session.id = candidates.id AND session.ended_at IS NULL
	`, staleBefore.UTC(), limit, now.UTC(), unavailableReplayMessage, terminalMergeAttempts); err != nil {
		return fmt.Errorf("recover abandoned replay sessions: %w", err)
	}
	return nil
}

func (s Store) PendingMerges(ctx context.Context, limit int, staleBefore time.Time) ([]Session, error) {
	rows, err := s.Database.Query(ctx, sessionSelect+`
		WHERE session.merge_requested_at IS NOT NULL AND session.merged_at IS NULL
		  AND session.merge_attempts < 12
		  AND (session.merge_enqueued_at IS NULL OR session.merge_enqueued_at < $1)
		ORDER BY session.merge_requested_at, session.id
		LIMIT $2
	`, staleBefore.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list pending replay merges: %w", err)
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
		return nil, fmt.Errorf("iterate pending replay merges: %w", err)
	}
	return sessions, nil
}

func (s Store) MarkMergeEnqueued(ctx context.Context, sessionID string, now time.Time) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE sessions SET merge_enqueued_at = $2
		WHERE id = $1 AND merged_at IS NULL
	`, sessionID, now.UTC()); err != nil {
		return fmt.Errorf("mark replay merge enqueued: %w", err)
	}
	return nil
}

func (s Store) LoadMerge(ctx context.Context, sessionID string) (Session, []Batch, error) {
	found, err := scanSessionRow(s.Database.QueryRow(ctx, sessionSelect+`
		WHERE session.id = $1 AND session.merge_requested_at IS NOT NULL
	`, sessionID))
	if err != nil {
		return Session{}, nil, err
	}
	rows, err := s.Database.Query(ctx, `
		SELECT id, tenant_id, project_id, session_id, batch_index, storage_key,
		       events_count, size_bytes, content_type, complete_token_hash, completed_at
		FROM session_batches
		WHERE session_id = $1
		ORDER BY batch_index
	`, sessionID)
	if err != nil {
		return Session{}, nil, fmt.Errorf("list replay merge batches: %w", err)
	}
	defer rows.Close()
	batches := make([]Batch, 0)
	for rows.Next() {
		var batch Batch
		if err := rows.Scan(
			&batch.ID, &batch.TenantID, &batch.ProjectID, &batch.SessionID, &batch.Index,
			&batch.StorageKey, &batch.EventsCount, &batch.SizeBytes, &batch.ContentType,
			&batch.CompleteTokenHash, &batch.CompletedAt,
		); err != nil {
			return Session{}, nil, fmt.Errorf("scan replay merge batch: %w", err)
		}
		batches = append(batches, batch)
	}
	if err := rows.Err(); err != nil {
		return Session{}, nil, fmt.Errorf("iterate replay merge batches: %w", err)
	}
	return found, batches, nil
}

func (s Store) MarkMerged(
	ctx context.Context,
	sessionID string,
	storageKey string,
	eventsCount int,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE sessions
		SET storage_key = $2, events_count = $3, merged_at = $4,
		    merge_error = NULL, merge_attempts = merge_attempts + 1
		WHERE id = $1
	`, sessionID, storageKey, eventsCount, now.UTC()); err != nil {
		return fmt.Errorf("mark replay merged: %w", err)
	}
	return nil
}

func (s Store) MarkMergeFailed(ctx context.Context, sessionID string, message string) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE sessions
		SET merge_error = $2, merge_attempts = merge_attempts + 1,
		    merge_enqueued_at = NULL
		WHERE id = $1 AND merged_at IS NULL
	`, sessionID, message); err != nil {
		return fmt.Errorf("mark replay merge failed: %w", err)
	}
	return nil
}

func (s Store) MarkUnavailable(ctx context.Context, sessionID string) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE sessions
		SET merge_error = $2, merge_attempts = $3, merge_enqueued_at = NULL
		WHERE id = $1 AND merged_at IS NULL
	`, sessionID, unavailableReplayMessage, terminalMergeAttempts); err != nil {
		return fmt.Errorf("mark replay unavailable: %w", err)
	}
	return nil
}

func (s Store) DeleteBatch(ctx context.Context, batchID string) error {
	if _, err := s.Database.Exec(ctx, `DELETE FROM session_batches WHERE id = $1`, batchID); err != nil {
		return fmt.Errorf("delete replay batch record: %w", err)
	}
	return nil
}

const sessionSelect = `
	SELECT session.id, session.tenant_id, session.project_id, session.feedback_id,
	       session.session_id, session.page_url, session.started_at, session.ended_at,
	       session.events_count, session.batches_count, session.storage_key,
	       session.rrweb_version, session.merged_at, session.merge_requested_at,
	       session.merge_enqueued_at, session.merge_attempts, session.merge_error,
	       session.retention_deleted_at,
	       session.max_duration_seconds, session.max_events, session.created_at
	FROM sessions AS session
`

type rowScanner interface {
	Scan(...any) error
}

func scanSessionRow(row rowScanner) (Session, error) {
	var found Session
	err := row.Scan(
		&found.ID, &found.TenantID, &found.ProjectID, &found.FeedbackID,
		&found.ClientSessionID, &found.PageURL, &found.StartedAt, &found.EndedAt,
		&found.EventsCount, &found.BatchesCount, &found.StorageKey,
		&found.RRWebVersion, &found.MergedAt, &found.MergeRequestedAt,
		&found.MergeEnqueuedAt, &found.MergeAttempts, &found.MergeError,
		&found.RetentionDeletedAt,
		&found.MaxDurationSeconds, &found.MaxEvents, &found.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("scan replay session: %w", err)
	}
	return found, nil
}
