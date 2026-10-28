package testimonial

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/storage"
)

const (
	mediaCleanupBatch = 100
	videoOrphanGrace  = 5 * time.Minute
)

type MediaSourceCleanup struct {
	TenantID   string
	MediaID    string
	StorageKey string
	DeleteRow  bool
}

type MediaCleanupStore interface {
	ClaimAbandonedVideoSources(context.Context, time.Time, time.Time, int) ([]MediaSourceCleanup, error)
	ListTerminalVideoSources(context.Context, int) ([]MediaSourceCleanup, error)
	CompleteVideoSourceCleanup(context.Context, MediaSourceCleanup, time.Time) error
	VideoSourceKeyActive(context.Context, string) (bool, error)
}

type MediaCleanupStorage interface {
	ListObjects(context.Context, string, string, int) ([]storage.StoredObject, error)
	RemoveObject(context.Context, string) error
}

type MediaCleanupMaintainer struct {
	Store   MediaCleanupStore
	Storage MediaCleanupStorage
	Now     func() time.Time
	Logger  *slog.Logger

	cursorMu     sync.Mutex
	orphanCursor string
}

func (m *MediaCleanupMaintainer) ReconcileOnce(ctx context.Context) error {
	if m.Store == nil || m.Storage == nil {
		return errors.New("testimonial media cleanup dependencies are unavailable")
	}
	now := m.now()
	abandoned, abandonedErr := m.Store.ClaimAbandonedVideoSources(
		ctx, now, now.Add(-videoUploadTTL-videoOrphanGrace), mediaCleanupBatch,
	)
	terminal, terminalErr := m.Store.ListTerminalVideoSources(ctx, mediaCleanupBatch)
	failures := []error{abandonedErr, terminalErr}
	attempted := make(map[string]struct{}, len(abandoned)+len(terminal))
	for _, item := range append(abandoned, terminal...) {
		attempted[item.StorageKey] = struct{}{}
		if err := m.Storage.RemoveObject(ctx, item.StorageKey); err != nil {
			failures = append(failures, fmt.Errorf(
				"remove testimonial source %q: %w", item.StorageKey, err,
			))
			continue
		}
		if err := m.Store.CompleteVideoSourceCleanup(ctx, item, now); err != nil {
			failures = append(failures, err)
		}
	}

	cursor := m.cursor()
	objects, listErr := m.Storage.ListObjects(ctx, "testimonials/", cursor, mediaCleanupBatch)
	if listErr != nil {
		failures = append(failures, listErr)
	} else {
		m.advanceCursor(objects)
		orphanBefore := now.Add(-videoUploadTTL - videoOrphanGrace)
		for _, object := range objects {
			if _, alreadyHandled := attempted[object.Key]; alreadyHandled ||
				!strings.HasSuffix(object.Key, "/source") ||
				object.LastModified.IsZero() || object.LastModified.After(orphanBefore) {
				continue
			}
			active, err := m.Store.VideoSourceKeyActive(ctx, object.Key)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if active {
				continue
			}
			if err := m.Storage.RemoveObject(ctx, object.Key); err != nil {
				failures = append(failures, fmt.Errorf(
					"remove orphaned testimonial source %q: %w", object.Key, err,
				))
			}
		}
	}
	return errors.Join(failures...)
}

func (m *MediaCleanupMaintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := m.ReconcileOnce(ctx); err != nil {
			m.logger().ErrorContext(ctx, "testimonial media cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (m *MediaCleanupMaintainer) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m *MediaCleanupMaintainer) logger() *slog.Logger {
	if m.Logger != nil {
		return m.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func (m *MediaCleanupMaintainer) cursor() string {
	m.cursorMu.Lock()
	defer m.cursorMu.Unlock()
	return m.orphanCursor
}

func (m *MediaCleanupMaintainer) advanceCursor(objects []storage.StoredObject) {
	m.cursorMu.Lock()
	defer m.cursorMu.Unlock()
	if len(objects) < mediaCleanupBatch {
		m.orphanCursor = ""
		return
	}
	m.orphanCursor = objects[len(objects)-1].Key
}

type PostgresMediaCleanupStore struct {
	Database db.Querier
}

func (s PostgresMediaCleanupStore) ClaimAbandonedVideoSources(
	ctx context.Context,
	now time.Time,
	abandonedBefore time.Time,
	limit int,
) ([]MediaSourceCleanup, error) {
	rows, err := s.Database.Query(ctx, `
		WITH candidates AS (
			SELECT media.id
			FROM testimonial_media AS media
			JOIN testimonial_requests AS request
			  ON request.tenant_id = media.tenant_id AND request.id = media.request_id
			WHERE media.role = 'video' AND media.completed_at IS NULL
			  AND media.source_deleted_at IS NULL
			  AND (
				media.created_at < $1 OR request.expires_at <= $2
				OR request.status IN ('submitted', 'expired', 'cancelled')
			  )
			ORDER BY media.created_at, media.id
			LIMIT $3
			FOR UPDATE OF media SKIP LOCKED
		)
		UPDATE testimonial_media AS media
		SET transcode_status = 'failed', transcode_error = 'upload_expired',
		    complete_token_hash = NULL, updated_at = $2
		FROM candidates
		WHERE media.id = candidates.id
		RETURNING media.tenant_id, media.public_id, media.source_storage_key
	`, abandonedBefore, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim abandoned testimonial video sources: %w", err)
	}
	defer rows.Close()
	items := make([]MediaSourceCleanup, 0)
	for rows.Next() {
		var item MediaSourceCleanup
		if err := rows.Scan(&item.TenantID, &item.MediaID, &item.StorageKey); err != nil {
			return nil, fmt.Errorf("scan abandoned testimonial video source: %w", err)
		}
		item.DeleteRow = true
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate abandoned testimonial video sources: %w", err)
	}
	return items, nil
}

func (s PostgresMediaCleanupStore) ListTerminalVideoSources(
	ctx context.Context,
	limit int,
) ([]MediaSourceCleanup, error) {
	rows, err := s.Database.Query(ctx, `
		SELECT tenant_id, public_id, source_storage_key
		FROM testimonial_media
		WHERE role = 'video' AND completed_at IS NOT NULL
		  AND source_deleted_at IS NULL AND transcode_status IN ('ready', 'failed')
		ORDER BY updated_at, id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list terminal testimonial video sources: %w", err)
	}
	defer rows.Close()
	items := make([]MediaSourceCleanup, 0)
	for rows.Next() {
		var item MediaSourceCleanup
		if err := rows.Scan(&item.TenantID, &item.MediaID, &item.StorageKey); err != nil {
			return nil, fmt.Errorf("scan terminal testimonial video source: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal testimonial video sources: %w", err)
	}
	return items, nil
}

func (s PostgresMediaCleanupStore) CompleteVideoSourceCleanup(
	ctx context.Context,
	item MediaSourceCleanup,
	now time.Time,
) error {
	if item.DeleteRow {
		if _, err := s.Database.Exec(ctx, `
			DELETE FROM testimonial_media
			WHERE tenant_id = $1 AND public_id = $2 AND source_storage_key = $3
			  AND completed_at IS NULL AND transcode_error = 'upload_expired'
		`, item.TenantID, item.MediaID, item.StorageKey); err != nil {
			return fmt.Errorf("delete abandoned testimonial video: %w", err)
		}
		return nil
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE testimonial_media
		SET source_deleted_at = COALESCE(source_deleted_at, $4), updated_at = $4
		WHERE tenant_id = $1 AND public_id = $2 AND source_storage_key = $3
	`, item.TenantID, item.MediaID, item.StorageKey, now); err != nil {
		return fmt.Errorf("complete testimonial source cleanup: %w", err)
	}
	return nil
}

func (s PostgresMediaCleanupStore) VideoSourceKeyActive(
	ctx context.Context,
	key string,
) (bool, error) {
	var active bool
	if err := s.Database.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM testimonial_media
			WHERE source_storage_key = $1 AND source_deleted_at IS NULL
			UNION ALL
			SELECT 1 FROM testimonial_media_renditions WHERE storage_key = $1
		)
	`, key).Scan(&active); err != nil {
		return false, fmt.Errorf("check testimonial storage key: %w", err)
	}
	return active, nil
}
