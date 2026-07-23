package replay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

type RetentionCandidate struct {
	TenantID   string
	ID         string
	ObjectKeys []string
}

func (s Store) ExpiredSessions(ctx context.Context, now time.Time, limit int) ([]RetentionCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.Database.Query(ctx, `
		SELECT session.tenant_id, session.id,
		       array_remove(array_prepend(session.storage_key, ARRAY(
		           SELECT batch.storage_key FROM session_batches AS batch
		           WHERE batch.session_id = session.id ORDER BY batch.batch_index
		       )), NULL)
		FROM sessions AS session
		JOIN tenants AS tenant ON tenant.id = session.tenant_id
		WHERE session.retention_deleted_at IS NULL
		  AND COALESCE(session.ended_at, session.created_at) < CASE tenant.plan
			WHEN 'starter' THEN $1::timestamptz - interval '30 days'
			WHEN 'growth' THEN $1::timestamptz - interval '90 days'
			WHEN 'scale' THEN $1::timestamptz - interval '365 days'
			ELSE NULL
		END
		ORDER BY COALESCE(session.ended_at, session.created_at), session.id
		LIMIT $2
		FOR UPDATE OF session SKIP LOCKED
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired replay sessions: %w", err)
	}
	defer rows.Close()
	candidates := make([]RetentionCandidate, 0)
	for rows.Next() {
		var candidate RetentionCandidate
		if err := rows.Scan(&candidate.TenantID, &candidate.ID, &candidate.ObjectKeys); err != nil {
			return nil, fmt.Errorf("scan expired replay session: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired replay sessions: %w", err)
	}
	return candidates, nil
}

func (s Store) MarkSessionRetentionDeleted(
	ctx context.Context,
	tenantID string,
	sessionID string,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		WITH deleted_incomplete_batches AS (
			DELETE FROM session_batches
			WHERE tenant_id = $1 AND session_id = $2 AND completed_at IS NULL
		), retained_batches AS (
			UPDATE session_batches
			SET storage_key = NULL, complete_token_hash = NULL, retention_deleted_at = $3
			WHERE tenant_id = $1 AND session_id = $2
			  AND completed_at IS NOT NULL AND retention_deleted_at IS NULL
		)
		UPDATE sessions
		SET storage_key = NULL, retention_deleted_at = $3,
		    merge_enqueued_at = NULL,
		    merge_attempts = CASE WHEN merged_at IS NULL THEN 12 ELSE merge_attempts END,
		    merge_error = CASE WHEN merged_at IS NULL
		        THEN 'Replay data expired under the workspace retention policy.'
		        ELSE merge_error END
		WHERE tenant_id = $1 AND id = $2 AND retention_deleted_at IS NULL
	`, tenantID, sessionID, now.UTC()); err != nil {
		return fmt.Errorf("preserve expired replay manifest: %w", err)
	}
	return nil
}

type RetentionStore interface {
	ExpiredSessions(context.Context, time.Time, int) ([]RetentionCandidate, error)
	MarkSessionRetentionDeleted(context.Context, string, string, time.Time) error
}

type ObjectRemover interface {
	RemoveObject(context.Context, string) error
}

type Cleaner struct {
	Store   RetentionStore
	Storage ObjectRemover
	Logger  *slog.Logger
	Now     func() time.Time
}

func (c Cleaner) CleanupOnce(ctx context.Context) error {
	now := c.now()
	candidates, err := c.Store.ExpiredSessions(ctx, now, 100)
	if err != nil {
		return err
	}
	var result error
	for _, candidate := range candidates {
		removeFailed := false
		for _, key := range candidate.ObjectKeys {
			if err := c.Storage.RemoveObject(ctx, key); err != nil {
				result = errors.Join(result, fmt.Errorf("remove expired replay object %q: %w", key, err))
				removeFailed = true
			}
		}
		if removeFailed {
			continue
		}
		if err := c.Store.MarkSessionRetentionDeleted(
			ctx, candidate.TenantID, candidate.ID, now,
		); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (c Cleaner) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	logger := c.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.CleanupOnce(ctx); err != nil {
			logger.ErrorContext(ctx, "replay retention cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (c Cleaner) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

var _ RetentionStore = Store{}
