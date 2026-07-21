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
	ID         string
	ObjectKeys []string
}

func (s Store) ExpiredSessions(ctx context.Context, now time.Time, limit int) ([]RetentionCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.Database.Query(ctx, `
		SELECT session.id,
		       array_remove(array_prepend(session.storage_key, ARRAY(
		           SELECT batch.storage_key FROM session_batches AS batch
		           WHERE batch.session_id = session.id ORDER BY batch.batch_index
		       )), NULL)
		FROM sessions AS session
		JOIN tenants AS tenant ON tenant.id = session.tenant_id
		WHERE COALESCE(session.ended_at, session.created_at) < CASE tenant.plan
			WHEN 'starter' THEN $1::timestamptz - interval '30 days'
			WHEN 'growth' THEN $1::timestamptz - interval '90 days'
			WHEN 'scale' THEN $1::timestamptz - interval '365 days'
			ELSE NULL
		END
		ORDER BY COALESCE(session.ended_at, session.created_at), session.id
		LIMIT $2
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired replay sessions: %w", err)
	}
	defer rows.Close()
	candidates := make([]RetentionCandidate, 0)
	for rows.Next() {
		var candidate RetentionCandidate
		if err := rows.Scan(&candidate.ID, &candidate.ObjectKeys); err != nil {
			return nil, fmt.Errorf("scan expired replay session: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired replay sessions: %w", err)
	}
	return candidates, nil
}

func (s Store) DeleteExpiredSession(ctx context.Context, sessionID string) error {
	if _, err := s.Database.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID); err != nil {
		return fmt.Errorf("delete expired replay session: %w", err)
	}
	return nil
}

type RetentionStore interface {
	ExpiredSessions(context.Context, time.Time, int) ([]RetentionCandidate, error)
	DeleteExpiredSession(context.Context, string) error
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
	candidates, err := c.Store.ExpiredSessions(ctx, c.now(), 100)
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
		if err := c.Store.DeleteExpiredSession(ctx, candidate.ID); err != nil {
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
