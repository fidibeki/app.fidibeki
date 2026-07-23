package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maximumJobAttempts = 12

type Store struct {
	Database db.Querier
}

func (Store) CreateWorkspaceExport(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	now time.Time,
) (Job, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return Job{}, false, err
	}
	job, err := scanJob(query.QueryRow(ctx, jobSelect+`
		WHERE job.tenant_id = $1 AND job.kind = 'workspace_export'
		  AND job.status IN ('pending', 'processing')
		ORDER BY job.created_at DESC, job.id DESC
		LIMIT 1
	`, tenantID))
	if err == nil {
		return job, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Job{}, false, err
	}
	jobID := uuid.NewString()
	job, err = scanJob(query.QueryRow(ctx, `
		INSERT INTO privacy_jobs (
			id, tenant_id, kind, status, actor_member_id, created_at, updated_at
		) VALUES ($1, $2, 'workspace_export', 'pending', $3, $4, $4)
		ON CONFLICT DO NOTHING
		RETURNING id, tenant_id, kind, status, actor_member_id, reporter_request_id, generation, attempts,
		          enqueued_at, started_at, heartbeat_at, completed_at, expires_at,
		          object_key, object_size_bytes, error_message, created_at, updated_at
	`, jobID, tenantID, actorMemberID, now.UTC()))
	if errors.Is(err, ErrNotFound) {
		job, getErr := scanJob(query.QueryRow(ctx, jobSelect+`
			WHERE job.tenant_id = $1 AND job.kind = 'workspace_export'
			  AND job.status IN ('pending', 'processing')
			ORDER BY job.created_at DESC, job.id DESC
			LIMIT 1
		`, tenantID))
		return job, false, getErr
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("create workspace export: %w", err)
	}
	if err := recordPrivacyAudit(ctx, query, tenantID, "privacy.export_requested", actorMemberID, job.ID, map[string]any{
		"kind": job.Kind,
	}); err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

func (Store) ListJobs(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	limit int,
) ([]Job, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := query.Query(ctx, jobSelect+`
		WHERE job.tenant_id = $1 AND job.kind = 'workspace_export'
		ORDER BY job.created_at DESC, job.id DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list privacy jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy jobs: %w", err)
	}
	return jobs, nil
}

func (Store) GetJob(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	jobID string,
) (Job, error) {
	return scanJob(query.QueryRow(ctx, jobSelect+`
		WHERE job.tenant_id = $1 AND job.id = $2
	`, tenantID, jobID))
}

func (Store) RetryJob(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	jobID string,
	now time.Time,
) (Job, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return Job{}, err
	}
	job, err := scanJob(query.QueryRow(ctx, `
		WITH target AS (
			SELECT id, tenant_id, kind, reporter_request_id
			FROM privacy_jobs
			WHERE tenant_id = $1 AND id = $2 AND status = 'failed'
			FOR UPDATE
		), updated_job AS (
			UPDATE privacy_jobs AS job
			SET status = 'pending', generation = job.generation + 1, attempts = 0,
			    enqueued_at = NULL, started_at = NULL, heartbeat_at = NULL,
			    error_message = NULL, updated_at = $3
			FROM target
			WHERE job.id = target.id AND NOT EXISTS (
				SELECT 1 FROM privacy_jobs AS active
				WHERE active.tenant_id = target.tenant_id
				  AND active.id <> target.id
				  AND active.status IN ('pending', 'processing')
				  AND (
					(target.kind = 'workspace_export' AND active.kind = 'workspace_export')
					OR (target.reporter_request_id IS NOT NULL
					    AND active.reporter_request_id = target.reporter_request_id)
				  )
			)
			RETURNING job.*
		), updated_request AS (
			UPDATE reporter_privacy_requests AS request
			SET status = 'processing', error_message = NULL
			FROM updated_job
			WHERE request.tenant_id = updated_job.tenant_id
			  AND request.id = updated_job.reporter_request_id
			  AND request.status = 'failed'
		)
		SELECT id, tenant_id, kind, status, actor_member_id, reporter_request_id, generation, attempts,
		          enqueued_at, started_at, heartbeat_at, completed_at, expires_at,
		          object_key, object_size_bytes, error_message, created_at, updated_at
		FROM updated_job
	`, tenantID, jobID, now.UTC()))
	if errors.Is(err, ErrNotFound) {
		return Job{}, ErrJobUnavailable
	}
	if err != nil {
		return Job{}, fmt.Errorf("retry privacy job: %w", err)
	}
	if job.ReporterRequestID != nil {
		if err := recordPrivacyRequestAudit(
			ctx, query, tenantID, "privacy.reporter_request_retried", actorMemberID,
			*job.ReporterRequestID, map[string]any{"kind": job.Kind, "privacy_job_id": job.ID},
		); err != nil {
			return Job{}, err
		}
	} else if err := recordPrivacyAudit(ctx, query, tenantID, "privacy.export_retried", actorMemberID, job.ID, map[string]any{
		"kind": job.Kind,
	}); err != nil {
		return Job{}, err
	}
	return job, nil
}

func (s Store) PrepareJobs(ctx context.Context, now time.Time, limit int) ([]PreparedJob, error) {
	if limit <= 0 {
		limit = 25
	}
	if _, err := s.Database.Exec(ctx, `
		UPDATE privacy_jobs
		SET status = CASE WHEN attempts >= $2 THEN 'failed'::privacy_job_status ELSE 'pending'::privacy_job_status END,
		    generation = generation + 1,
		    enqueued_at = NULL, started_at = NULL, heartbeat_at = NULL,
		    error_message = CASE WHEN attempts >= $2
		        THEN COALESCE(error_message, 'export worker lease expired after maximum attempts')
		        ELSE error_message END
		WHERE status = 'processing' AND heartbeat_at < $1
	`, now.UTC().Add(-30*time.Minute), maximumJobAttempts); err != nil {
		return nil, fmt.Errorf("reclaim stale privacy jobs: %w", err)
	}
	rows, err := s.Database.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM privacy_jobs
			WHERE status = 'pending' AND attempts < $2
			  AND (enqueued_at IS NULL OR enqueued_at < $1::timestamptz - interval '5 minutes')
			ORDER BY created_at, id
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE privacy_jobs AS job
		SET enqueued_at = $1
		FROM candidates
		WHERE job.id = candidates.id
		RETURNING job.id, job.tenant_id, job.kind, job.generation
	`, now.UTC(), maximumJobAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("prepare privacy jobs: %w", err)
	}
	defer rows.Close()
	prepared := make([]PreparedJob, 0)
	for rows.Next() {
		var job PreparedJob
		if err := rows.Scan(&job.ID, &job.TenantID, &job.Kind, &job.Generation); err != nil {
			return nil, fmt.Errorf("scan prepared privacy job: %w", err)
		}
		prepared = append(prepared, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prepared privacy jobs: %w", err)
	}
	return prepared, nil
}

func (s Store) UndoPrepared(ctx context.Context, prepared PreparedJob) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE privacy_jobs SET enqueued_at = NULL
		WHERE id = $1 AND tenant_id = $2 AND generation = $3 AND status = 'pending'
	`, prepared.ID, prepared.TenantID, prepared.Generation); err != nil {
		return fmt.Errorf("undo prepared privacy job: %w", err)
	}
	return nil
}

func (s Store) ClaimJob(
	ctx context.Context,
	jobID string,
	tenantID string,
	generation int,
	now time.Time,
) (Job, bool, error) {
	job, err := scanJob(s.Database.QueryRow(ctx, `
		UPDATE privacy_jobs
		SET status = 'processing',
		    attempts = CASE WHEN status = 'pending' THEN attempts + 1 ELSE attempts END,
		    started_at = COALESCE(started_at, $4), heartbeat_at = $4,
		    error_message = NULL
		WHERE id = $1 AND tenant_id = $2 AND generation = $3
		  AND (status = 'pending' OR status = 'processing')
		  AND attempts < $5
		RETURNING id, tenant_id, kind, status, actor_member_id, reporter_request_id, generation, attempts,
		          enqueued_at, started_at, heartbeat_at, completed_at, expires_at,
		          object_key, object_size_bytes, error_message, created_at, updated_at
	`, jobID, tenantID, generation, now.UTC(), maximumJobAttempts))
	if errors.Is(err, ErrNotFound) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("claim privacy job: %w", err)
	}
	return job, true, nil
}

func (s Store) MarkJobFailed(ctx context.Context, job Job, message string, now time.Time) error {
	if len(message) > 2000 {
		message = message[:2000]
	}
	if _, err := s.Database.Exec(ctx, `
		WITH updated_job AS (
			UPDATE privacy_jobs
			SET status = CASE WHEN attempts >= $5 THEN 'failed'::privacy_job_status ELSE 'pending'::privacy_job_status END,
			    generation = generation + 1, enqueued_at = NULL,
			    started_at = NULL, heartbeat_at = NULL, error_message = $4
			WHERE id = $1 AND tenant_id = $2 AND generation = $3 AND status = 'processing'
			RETURNING id, status, reporter_request_id
		), failed_request AS (
			UPDATE reporter_privacy_requests AS request
			SET status = 'failed', error_message = $4
			FROM updated_job
			WHERE updated_job.status = 'failed'
			  AND request.tenant_id = $2 AND request.id = updated_job.reporter_request_id
			  AND request.status = 'processing'
			RETURNING request.id
		)
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id, data
		)
		SELECT $2, 'privacy.reporter_request_failed', NULL, NULL,
		       jsonb_build_object(
			       'privacy_job_id', updated_job.id::text,
			       'reporter_request_id', failed_request.id::text,
			       'error', $4::text
		       )
		FROM updated_job CROSS JOIN failed_request
	`, job.ID, job.TenantID, job.Generation, message, maximumJobAttempts); err != nil {
		return fmt.Errorf("mark privacy job failed: %w", err)
	}
	return nil
}

func (s Store) CompleteJob(
	ctx context.Context,
	job Job,
	objectKey string,
	size int64,
	now time.Time,
) error {
	data, err := json.Marshal(map[string]any{"kind": job.Kind, "object_size_bytes": size})
	if err != nil {
		return fmt.Errorf("encode privacy completion audit: %w", err)
	}
	var completedID string
	err = s.Database.QueryRow(ctx, `
		WITH completed AS (
			UPDATE privacy_jobs
			SET status = 'completed', heartbeat_at = $4, completed_at = $4,
			    expires_at = $4::timestamptz + interval '7 days',
			    object_key = $5, object_size_bytes = $6, error_message = NULL
			WHERE id = $1 AND tenant_id = $2 AND generation = $3 AND status = 'processing'
			RETURNING id
		), audited AS (
			INSERT INTO tenant_access_events (
				tenant_id, kind, actor_member_id, subject_member_id, data
			)
			SELECT $2, 'privacy.export_completed', $7::uuid, NULL,
			       $8::jsonb || jsonb_build_object('privacy_job_id', completed.id::text)
			FROM completed
			RETURNING id
		)
		SELECT id FROM completed
	`, job.ID, job.TenantID, job.Generation, now.UTC(), objectKey, size, job.ActorMemberID, string(data)).
		Scan(&completedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobUnavailable
	}
	if err != nil {
		return fmt.Errorf("complete privacy job: %w", err)
	}
	if completedID != job.ID {
		return ErrJobUnavailable
	}
	return nil
}

func (Store) RecordDownload(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	jobID string,
	expiresAt time.Time,
) error {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return err
	}
	return recordPrivacyAudit(ctx, query, tenantID, "privacy.export_download_granted", actorMemberID, jobID, map[string]any{
		"link_expires_at": expiresAt.UTC(),
	})
}

func requireActiveHuman(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	memberID string,
) error {
	var active bool
	err := query.QueryRow(ctx, `
		SELECT status = 'active'
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, memberID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
		return ErrHumanRequired
	}
	if err != nil {
		return fmt.Errorf("authorize privacy member: %w", err)
	}
	return nil
}

func recordPrivacyAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind string,
	actorMemberID string,
	jobID string,
	data map[string]any,
) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode privacy audit data: %w", err)
	}
	var actor any
	if actorMemberID != "" {
		actor = actorMemberID
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id, data
		) VALUES ($1, $2, $3, NULL, $4::jsonb || jsonb_build_object('privacy_job_id', $5::text))
	`, tenantID, kind, actor, string(encoded), jobID); err != nil {
		return fmt.Errorf("record privacy audit: %w", err)
	}
	return nil
}

const jobSelect = `
	SELECT job.id, job.tenant_id, job.kind, job.status, job.actor_member_id, job.reporter_request_id,
	       job.generation, job.attempts, job.enqueued_at, job.started_at,
	       job.heartbeat_at, job.completed_at, job.expires_at, job.object_key,
	       job.object_size_bytes, job.error_message, job.created_at, job.updated_at
	FROM privacy_jobs AS job
`

type rowScanner interface {
	Scan(...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	err := row.Scan(
		&job.ID, &job.TenantID, &job.Kind, &job.Status, &job.ActorMemberID, &job.ReporterRequestID,
		&job.Generation, &job.Attempts, &job.EnqueuedAt, &job.StartedAt,
		&job.HeartbeatAt, &job.CompletedAt, &job.ExpiresAt, &job.ObjectKey,
		&job.ObjectSizeBytes, &job.ErrorMessage, &job.CreatedAt, &job.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan privacy job: %w", err)
	}
	return job, nil
}
