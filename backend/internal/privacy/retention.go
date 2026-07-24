package privacy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/retentionhealth"
)

const (
	retentionBatchSize    = 100
	retentionMaxBatches   = 10
	retentionPhaseTimeout = time.Minute
)

type RetentionObjectCandidate struct {
	TenantID  string
	ID        string
	ObjectKey string
}

type RetentionOperations interface {
	ExpiredAttachments(context.Context, time.Time, int) ([]RetentionObjectCandidate, error)
	MarkAttachmentRetentionDeleted(context.Context, RetentionObjectCandidate, time.Time) error
	ExpiredExportArchives(context.Context, time.Time, int) ([]RetentionObjectCandidate, error)
	MarkExportArchiveExpired(context.Context, RetentionObjectCandidate, time.Time) error
	ExpireReporterRequests(context.Context, time.Time, int) (int64, error)
}

type RetentionObjectRemover interface {
	RemoveObject(context.Context, string) error
}

type RetentionMaintainer struct {
	Store        RetentionOperations
	Storage      RetentionObjectRemover
	Health       retentionhealth.Recorder
	Logger       *slog.Logger
	Now          func() time.Time
	BatchSize    int
	MaxBatches   int
	PhaseTimeout time.Duration
}

func (store Store) ExpiredAttachments(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]RetentionObjectCandidate, error) {
	if limit <= 0 {
		limit = retentionBatchSize
	}
	rows, err := store.Database.Query(ctx, `
		SELECT attachment.tenant_id, attachment.id, attachment.storage_key
		FROM attachments AS attachment
		JOIN tenants AS tenant ON tenant.id = attachment.tenant_id
		WHERE attachment.completed_at IS NOT NULL
		  AND attachment.retention_deleted_at IS NULL
		  AND attachment.storage_key IS NOT NULL
		  AND attachment.completed_at < CASE tenant.plan
			WHEN 'starter' THEN $1::timestamptz - interval '30 days'
			WHEN 'growth' THEN $1::timestamptz - interval '90 days'
			WHEN 'scale' THEN $1::timestamptz - interval '365 days'
			ELSE NULL
		  END
		ORDER BY attachment.completed_at, attachment.id
		LIMIT $2
		FOR UPDATE OF attachment SKIP LOCKED
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired attachments: %w", err)
	}
	defer rows.Close()
	candidates := make([]RetentionObjectCandidate, 0)
	for rows.Next() {
		var candidate RetentionObjectCandidate
		if err := rows.Scan(&candidate.TenantID, &candidate.ID, &candidate.ObjectKey); err != nil {
			return nil, fmt.Errorf("scan expired attachment: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired attachments: %w", err)
	}
	return candidates, nil
}

func (store Store) MarkAttachmentRetentionDeleted(
	ctx context.Context,
	candidate RetentionObjectCandidate,
	now time.Time,
) error {
	if _, err := store.Database.Exec(ctx, `
		UPDATE attachments
		SET storage_key = NULL, retention_deleted_at = $4,
		    transcription = NULL, sentiment_score = NULL,
		    moderation_flags = '{}', complete_token_hash = NULL
		WHERE tenant_id = $1 AND id = $2 AND storage_key = $3
		  AND completed_at IS NOT NULL AND retention_deleted_at IS NULL
	`, candidate.TenantID, candidate.ID, candidate.ObjectKey, now.UTC()); err != nil {
		return fmt.Errorf("preserve expired attachment manifest: %w", err)
	}
	return nil
}

func (store Store) ExpiredExportArchives(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]RetentionObjectCandidate, error) {
	if limit <= 0 {
		limit = retentionBatchSize
	}
	rows, err := store.Database.Query(ctx, `
		SELECT tenant_id, id, object_key
		FROM privacy_jobs
		WHERE kind IN ('workspace_export', 'reporter_export')
		  AND status = 'completed'
		  AND (expires_at <= $1 OR context_privacy_version < 1)
		  AND object_key IS NOT NULL
		ORDER BY context_privacy_version, expires_at, id
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired export archives: %w", err)
	}
	defer rows.Close()
	candidates := make([]RetentionObjectCandidate, 0)
	for rows.Next() {
		var candidate RetentionObjectCandidate
		if err := rows.Scan(&candidate.TenantID, &candidate.ID, &candidate.ObjectKey); err != nil {
			return nil, fmt.Errorf("scan expired export archive: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired export archives: %w", err)
	}
	return candidates, nil
}

func (store Store) MarkExportArchiveExpired(
	ctx context.Context,
	candidate RetentionObjectCandidate,
	now time.Time,
) error {
	var updated int
	err := store.Database.QueryRow(ctx, `
		WITH expired AS (
			UPDATE privacy_jobs
			SET status = 'expired', object_key = NULL, updated_at = $4
			WHERE tenant_id = $1 AND id = $2 AND object_key = $3
			  AND kind IN ('workspace_export', 'reporter_export')
			  AND status = 'completed'
			  AND (expires_at <= $4 OR context_privacy_version < 1)
			RETURNING tenant_id, id, kind, actor_member_id, reporter_request_id
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT tenant_id,
			       CASE WHEN reporter_request_id IS NULL
			           THEN 'privacy.export_expired'
			           ELSE 'privacy.reporter_export_expired' END,
			       actor_member_id, NULL,
			       jsonb_build_object(
			           'privacy_job_id', id::text, 'kind', kind::text,
			           'reporter_request_id', reporter_request_id::text
			       )
			FROM expired
		)
		SELECT count(*) FROM expired
	`, candidate.TenantID, candidate.ID, candidate.ObjectKey, now.UTC()).Scan(&updated)
	if err != nil {
		return fmt.Errorf("expire export archive: %w", err)
	}
	return nil
}

func (store Store) ExpireReporterRequests(
	ctx context.Context,
	now time.Time,
	limit int,
) (int64, error) {
	if limit <= 0 {
		limit = retentionBatchSize
	}
	var expired int64
	err := store.Database.QueryRow(ctx, `
		WITH candidates AS (
			SELECT id
			FROM reporter_privacy_requests
			WHERE (
				status = 'pending_verification' AND verification_expires_at <= $1
			) OR (
				status IN ('completed', 'failed') AND access_expires_at <= $1
			)
			ORDER BY COALESCE(access_expires_at, verification_expires_at), id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		), updated AS (
			UPDATE reporter_privacy_requests AS request
			SET status = 'expired', reporter_email = NULL, error_message = NULL
			FROM candidates
			WHERE request.id = candidates.id
			RETURNING request.tenant_id, request.id, request.action, request.email_digest
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT tenant_id, 'privacy.reporter_request_expired', NULL, NULL,
			       jsonb_build_object(
			           'reporter_request_id', id::text, 'action', action::text,
			           'email_digest', encode(email_digest, 'hex')
			       )
			FROM updated
		)
		SELECT count(*) FROM updated
	`, now.UTC(), limit).Scan(&expired)
	if err != nil {
		return 0, fmt.Errorf("expire reporter privacy requests: %w", err)
	}
	return expired, nil
}

func (maintainer RetentionMaintainer) MaintainOnce(ctx context.Context) error {
	if maintainer.Store == nil {
		return errors.New("privacy retention store is unavailable")
	}
	if maintainer.Storage == nil {
		return errors.New("privacy retention storage is unavailable")
	}
	if maintainer.Health == nil {
		return errors.New("privacy retention health recorder is unavailable")
	}
	now := maintainer.now()
	batchSize := maintainer.BatchSize
	if batchSize <= 0 {
		batchSize = retentionBatchSize
	}
	maxBatches := maintainer.MaxBatches
	if maxBatches <= 0 {
		maxBatches = retentionMaxBatches
	}
	attachmentCtx, cancelAttachments := maintainer.phaseContext(ctx)
	attachments, attachmentBudget, attachmentErr := drainObjectRetention(
		attachmentCtx, batchSize, maxBatches,
		func(listCtx context.Context, limit int) ([]RetentionObjectCandidate, error) {
			return maintainer.Store.ExpiredAttachments(listCtx, now, limit)
		},
		func(markCtx context.Context, candidate RetentionObjectCandidate) error {
			return maintainer.Store.MarkAttachmentRetentionDeleted(markCtx, candidate, now)
		},
		maintainer.Storage.RemoveObject,
	)
	cancelAttachments()
	archiveCtx, cancelArchives := maintainer.phaseContext(ctx)
	archives, archiveBudget, archiveErr := drainObjectRetention(
		archiveCtx, batchSize, maxBatches,
		func(listCtx context.Context, limit int) ([]RetentionObjectCandidate, error) {
			return maintainer.Store.ExpiredExportArchives(listCtx, now, limit)
		},
		func(markCtx context.Context, candidate RetentionObjectCandidate) error {
			return maintainer.Store.MarkExportArchiveExpired(markCtx, candidate, now)
		},
		maintainer.Storage.RemoveObject,
	)
	cancelArchives()
	requestCtx, cancelRequests := maintainer.phaseContext(ctx)
	requests, requestBudget, requestErr := drainRequestRetention(
		requestCtx, batchSize, maxBatches, func(expireCtx context.Context, limit int) (int64, error) {
			return maintainer.Store.ExpireReporterRequests(expireCtx, now, limit)
		},
	)
	cancelRequests()
	log := maintainer.logger()
	log.InfoContext(ctx, "privacy retention maintenance completed",
		"attachments_deleted", attachments, "archives_deleted", archives,
		"requests_expired", requests,
		"attachment_cleanup_failed", attachmentErr != nil,
		"archive_cleanup_failed", archiveErr != nil,
		"request_cleanup_failed", requestErr != nil,
	)
	if attachmentBudget || archiveBudget || requestBudget {
		log.WarnContext(ctx, "privacy retention budget exhausted",
			"attachment_budget_exhausted", attachmentBudget,
			"archive_budget_exhausted", archiveBudget,
			"request_budget_exhausted", requestBudget,
			"maximum_batches", maxBatches,
		)
	}
	recordErr := errors.Join(
		maintainer.recordPhase(ctx, retentionhealth.OperationAttachments,
			now, attachments, attachmentBudget, attachmentErr),
		maintainer.recordPhase(ctx, retentionhealth.OperationPrivacyExportArchives,
			now, archives, archiveBudget, archiveErr),
		maintainer.recordPhase(ctx, retentionhealth.OperationReporterPrivacyRequests,
			now, requests, requestBudget, requestErr),
	)
	return errors.Join(attachmentErr, archiveErr, requestErr, recordErr)
}

func (maintainer RetentionMaintainer) recordPhase(
	ctx context.Context,
	operation retentionhealth.Operation,
	attemptedAt time.Time,
	processed int64,
	budgetExhausted bool,
	phaseErr error,
) error {
	result := retentionhealth.ResultSucceeded
	if phaseErr != nil {
		result = retentionhealth.ResultFailed
	}
	if err := maintainer.Health.RecordRun(ctx, retentionhealth.Run{
		Operation: operation, AttemptedAt: attemptedAt, Result: result,
		Processed: processed, BudgetExhausted: budgetExhausted,
	}); err != nil {
		return fmt.Errorf("record %s retention health: %w", operation, err)
	}
	return nil
}

func drainObjectRetention(
	ctx context.Context,
	batchSize int,
	maxBatches int,
	list func(context.Context, int) ([]RetentionObjectCandidate, error),
	mark func(context.Context, RetentionObjectCandidate) error,
	remove func(context.Context, string) error,
) (int64, bool, error) {
	var total int64
	for batch := 0; batch < maxBatches; batch++ {
		candidates, err := list(ctx, batchSize)
		if err != nil {
			return total, false, err
		}
		var batchErr error
		for _, candidate := range candidates {
			if err := remove(ctx, candidate.ObjectKey); err != nil {
				batchErr = errors.Join(batchErr,
					fmt.Errorf("remove retained object %q: %w", candidate.ObjectKey, err))
				continue
			}
			if err := mark(ctx, candidate); err != nil {
				batchErr = errors.Join(batchErr, err)
				continue
			}
			total++
		}
		if batchErr != nil {
			return total, false, batchErr
		}
		if len(candidates) < batchSize {
			return total, false, nil
		}
	}
	return total, true, nil
}

func drainRequestRetention(
	ctx context.Context,
	batchSize int,
	maxBatches int,
	expire func(context.Context, int) (int64, error),
) (int64, bool, error) {
	var total int64
	for batch := 0; batch < maxBatches; batch++ {
		expired, err := expire(ctx, batchSize)
		total += expired
		if err != nil {
			return total, false, err
		}
		if expired < int64(batchSize) {
			return total, false, nil
		}
	}
	return total, true, nil
}

func (maintainer RetentionMaintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := maintainer.MaintainOnce(ctx); err != nil && ctx.Err() == nil {
			maintainer.logger().ErrorContext(ctx, "privacy retention maintenance failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (maintainer RetentionMaintainer) phaseContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	timeout := maintainer.PhaseTimeout
	if timeout <= 0 {
		timeout = retentionPhaseTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (maintainer RetentionMaintainer) now() time.Time {
	if maintainer.Now != nil {
		return maintainer.Now().UTC()
	}
	return time.Now().UTC()
}

func (maintainer RetentionMaintainer) logger() *slog.Logger {
	if maintainer.Logger != nil {
		return maintainer.Logger
	}
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

var _ RetentionOperations = Store{}
