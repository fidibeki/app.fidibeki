package privacy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	reporterVerificationTTL = 48 * time.Hour
	reporterAccessTTL       = 30 * 24 * time.Hour
)

func NormalizeReporterEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || len(normalized) > 320 {
		return "", ErrInvalidRequest
	}
	return normalized, nil
}

func ParseReporterAction(value string) (ReporterAction, error) {
	action := ReporterAction(strings.TrimSpace(value))
	if action != ReporterExport && action != ReporterAnonymize {
		return "", ErrInvalidRequest
	}
	return action, nil
}

func reporterEmailDigest(email string) [sha256.Size]byte {
	return sha256.Sum256([]byte(email))
}

func reporterRequestToken() (string, [sha256.Size]byte, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("generate reporter privacy token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(value)
	return raw, sha256.Sum256([]byte(raw)), nil
}

func parseReporterRequestToken(raw string) ([sha256.Size]byte, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(decoded) != 32 {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256([]byte(strings.TrimSpace(raw))), true
}

func (Store) CreateReporterRequest(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	email string,
	action ReporterAction,
	now time.Time,
) (ReporterRequest, string, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return ReporterRequest{}, "", false, err
	}
	normalized, err := NormalizeReporterEmail(email)
	if err != nil {
		return ReporterRequest{}, "", false, err
	}
	if action != ReporterExport && action != ReporterAnonymize {
		return ReporterRequest{}, "", false, ErrInvalidRequest
	}
	rawToken, tokenHash, err := reporterRequestToken()
	if err != nil {
		return ReporterRequest{}, "", false, err
	}
	digest := reporterEmailDigest(normalized)
	requestID := uuid.NewString()
	var storedTokenHash []byte
	request, err := scanReporterRequest(query.QueryRow(ctx, `WITH upserted AS (
		INSERT INTO reporter_privacy_requests (
			id, tenant_id, action, status, reporter_email, email_digest, token_hash,
			actor_member_id, verification_expires_at, created_at, updated_at
		) VALUES ($1, $2, $3, 'pending_verification', $4, $5, $6, $7, $8, $9, $9)
		ON CONFLICT (tenant_id, email_digest, action)
			WHERE status IN ('pending_verification', 'processing')
		DO UPDATE SET
			token_hash = CASE
				WHEN reporter_privacy_requests.status = 'pending_verification' THEN EXCLUDED.token_hash
				ELSE reporter_privacy_requests.token_hash
			END,
			verification_expires_at = CASE
				WHEN reporter_privacy_requests.status = 'pending_verification' THEN EXCLUDED.verification_expires_at
				ELSE reporter_privacy_requests.verification_expires_at
			END,
			actor_member_id = EXCLUDED.actor_member_id,
			updated_at = EXCLUDED.updated_at
		RETURNING *
	)
	SELECT request.id, request.tenant_id, request.action, request.status,
	       request.reporter_email, request.actor_member_id, request.verification_expires_at,
	       request.verified_at, request.access_expires_at, request.completed_at,
	       request.record_count, request.manual_review_count, request.error_message,
	       job.id, job.status, job.expires_at, request.created_at, request.updated_at,
	       request.token_hash
	FROM upserted AS request
	LEFT JOIN LATERAL (
		SELECT privacy_job.id, privacy_job.status, privacy_job.expires_at
		FROM privacy_jobs AS privacy_job
		WHERE privacy_job.tenant_id = request.tenant_id
		  AND privacy_job.reporter_request_id = request.id
		ORDER BY privacy_job.created_at DESC, privacy_job.id DESC LIMIT 1
	) AS job ON true
`, requestID, tenantID, action, normalized, digest[:], tokenHash[:], actorMemberID,
		now.UTC().Add(reporterVerificationTTL), now.UTC()), &storedTokenHash)
	if err != nil {
		return ReporterRequest{}, "", false, fmt.Errorf("create reporter privacy request: %w", err)
	}
	shouldEmail := request.Status == ReporterPendingVerification && bytes.Equal(storedTokenHash, tokenHash[:])
	if err := recordPrivacyRequestAudit(ctx, query, tenantID, "privacy.reporter_request_requested", actorMemberID, request.ID, map[string]any{
		"action": action, "email_digest": hex.EncodeToString(digest[:]), "verification_email_queued": shouldEmail,
	}); err != nil {
		return ReporterRequest{}, "", false, err
	}
	return request, rawToken, shouldEmail, nil
}

func (Store) VerifyReporterRequest(
	ctx context.Context,
	query db.Querier,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (ReporterRequestView, error) {
	var requestID, tenantID string
	var action ReporterAction
	var status ReporterRequestStatus
	var verificationExpires time.Time
	var accessExpires *time.Time
	var emailDigest []byte
	err := query.QueryRow(ctx, `
		SELECT id, tenant_id, action, status, verification_expires_at,
		       access_expires_at, email_digest
		FROM reporter_privacy_requests
		WHERE token_hash = $1
		FOR UPDATE
	`, tokenHash[:]).Scan(
		&requestID, &tenantID, &action, &status, &verificationExpires, &accessExpires, &emailDigest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterRequestView{}, ErrNotFound
	}
	if err != nil {
		return ReporterRequestView{}, fmt.Errorf("lock reporter privacy request: %w", err)
	}
	if status == ReporterPendingVerification {
		if !verificationExpires.After(now) {
			return ReporterRequestView{}, ErrNotFound
		}
		jobID := uuid.NewString()
		kind := JobReporterExport
		if action == ReporterAnonymize {
			kind = JobReporterAnonymize
		}
		if _, err := query.Exec(ctx, `
			INSERT INTO privacy_jobs (
				id, tenant_id, kind, status, reporter_request_id, created_at, updated_at
			) VALUES ($1, $2, $3, 'pending', $4, $5, $5)
		`, jobID, tenantID, kind, requestID, now.UTC()); err != nil {
			return ReporterRequestView{}, fmt.Errorf("create reporter privacy job: %w", err)
		}
		access := now.UTC().Add(reporterAccessTTL)
		if _, err := query.Exec(ctx, `
			UPDATE reporter_privacy_requests
			SET status = 'processing', verified_at = $2, access_expires_at = $3,
			    error_message = NULL
			WHERE id = $1 AND status = 'pending_verification'
		`, requestID, now.UTC(), access); err != nil {
			return ReporterRequestView{}, fmt.Errorf("verify reporter privacy request: %w", err)
		}
		if err := recordPrivacyRequestAudit(
			ctx, query, tenantID, "privacy.reporter_request_verified", "", requestID,
			map[string]any{"action": action, "email_digest": hex.EncodeToString(emailDigest)},
		); err != nil {
			return ReporterRequestView{}, err
		}
		accessExpires = &access
		status = ReporterProcessing
	}
	if accessExpires == nil || !accessExpires.After(now) ||
		(status != ReporterProcessing && status != ReporterAwaitingReview &&
			status != ReporterCompleted && status != ReporterFailed) {
		return ReporterRequestView{}, ErrNotFound
	}
	return loadReporterRequestView(ctx, query, tokenHash, now)
}

func (store Store) GetReporterRequestView(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (ReporterRequestView, error) {
	if store.Database == nil {
		return ReporterRequestView{}, errors.New("privacy request database is unavailable")
	}
	return loadReporterRequestView(ctx, store.Database, tokenHash, now)
}

func loadReporterRequestView(
	ctx context.Context,
	query db.Querier,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (ReporterRequestView, error) {
	var view ReporterRequestView
	var jobKind *JobKind
	var jobExpiresAt *time.Time
	err := query.QueryRow(ctx, `
		SELECT request.action, request.status, request.completed_at,
		       request.record_count, request.manual_review_count,
		       job.kind, job.status, job.expires_at, request.access_expires_at
		FROM reporter_privacy_requests AS request
		LEFT JOIN LATERAL (
			SELECT privacy_job.kind, privacy_job.status,
			       CASE WHEN privacy_job.context_privacy_version >= 1
			            THEN privacy_job.expires_at END AS expires_at
			FROM privacy_jobs AS privacy_job
			WHERE privacy_job.tenant_id = request.tenant_id
			  AND privacy_job.reporter_request_id = request.id
			ORDER BY privacy_job.created_at DESC, privacy_job.id DESC LIMIT 1
		) AS job ON true
		WHERE request.token_hash = $1
		  AND request.status IN ('processing', 'awaiting_manual_review', 'completed', 'failed')
		  AND request.access_expires_at > $2
	`, tokenHash[:], now.UTC()).Scan(
		&view.Action, &view.Status, &view.CompletedAt,
		&view.RecordCount, &view.ManualReviewCount,
		&jobKind, &view.JobStatus, &jobExpiresAt, &view.AccessExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterRequestView{}, ErrNotFound
	}
	if err != nil {
		return ReporterRequestView{}, fmt.Errorf("load reporter privacy request status: %w", err)
	}
	view.DownloadAvailable = jobKind != nil && *jobKind == JobReporterExport &&
		view.JobStatus != nil && *view.JobStatus == JobCompleted &&
		jobExpiresAt != nil && jobExpiresAt.After(now)
	return view, nil
}

type reporterDownload struct {
	TenantID  string
	RequestID string
	JobID     string
	ObjectKey string
}

func (store Store) LoadReporterDownload(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (reporterDownload, error) {
	var download reporterDownload
	err := store.Database.QueryRow(ctx, `
		SELECT request.tenant_id, request.id, job.id, job.object_key
		FROM reporter_privacy_requests AS request
		JOIN privacy_jobs AS job
		  ON job.tenant_id = request.tenant_id AND job.reporter_request_id = request.id
		WHERE request.token_hash = $1 AND request.action = 'export'
		  AND request.status = 'completed' AND request.access_expires_at > $2
		  AND job.kind = 'reporter_export' AND job.status = 'completed'
		  AND job.context_privacy_version >= 1
		  AND job.expires_at > $2 AND job.object_key IS NOT NULL
	`, tokenHash[:], now.UTC()).Scan(
		&download.TenantID, &download.RequestID, &download.JobID, &download.ObjectKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return reporterDownload{}, ErrNotFound
	}
	if err != nil {
		return reporterDownload{}, fmt.Errorf("load reporter export download: %w", err)
	}
	return download, nil
}

func (store Store) RecordReporterDownload(
	ctx context.Context,
	download reporterDownload,
	expiresAt time.Time,
) error {
	return recordPrivacyRequestAudit(
		ctx, store.Database, download.TenantID, "privacy.reporter_export_download_granted", "",
		download.RequestID, map[string]any{
			"privacy_job_id": download.JobID, "link_expires_at": expiresAt.UTC(),
		},
	)
}

func recordPrivacyRequestAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	kind string,
	actorMemberID string,
	requestID string,
	data map[string]any,
) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode reporter privacy audit data: %w", err)
	}
	var actor any
	if actorMemberID != "" {
		actor = actorMemberID
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id, data
		) VALUES ($1, $2, $3, NULL,
		          $4::jsonb || jsonb_build_object('reporter_request_id', $5::text))
	`, tenantID, kind, actor, string(encoded), requestID); err != nil {
		return fmt.Errorf("record reporter privacy audit: %w", err)
	}
	return nil
}

func (Store) ListReporterRequests(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	limit int,
) ([]ReporterRequest, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := query.Query(ctx, `SELECT request.id, request.tenant_id, request.action, request.status,
		       request.reporter_email, request.actor_member_id, request.verification_expires_at,
		       request.verified_at, request.access_expires_at, request.completed_at,
		       request.record_count, request.manual_review_count, request.error_message,
		       job.id, job.status, job.expires_at, request.created_at, request.updated_at
		FROM reporter_privacy_requests AS request
		LEFT JOIN LATERAL (
			SELECT privacy_job.id, privacy_job.status, privacy_job.expires_at
			FROM privacy_jobs AS privacy_job
			WHERE privacy_job.tenant_id = request.tenant_id
			  AND privacy_job.reporter_request_id = request.id
			ORDER BY privacy_job.created_at DESC, privacy_job.id DESC LIMIT 1
		) AS job ON true
		WHERE request.tenant_id = $1
		ORDER BY request.created_at DESC, request.id DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list reporter privacy requests: %w", err)
	}
	defer rows.Close()
	requests := make([]ReporterRequest, 0)
	for rows.Next() {
		request, scanErr := scanReporterRequest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reporter privacy requests: %w", err)
	}
	return requests, nil
}

func scanReporterRequest(row rowScanner, extra ...*[]byte) (ReporterRequest, error) {
	var request ReporterRequest
	destinations := []any{
		&request.ID, &request.TenantID, &request.Action, &request.Status,
		&request.ReporterEmail, &request.ActorMemberID, &request.VerificationExpires,
		&request.VerifiedAt, &request.AccessExpiresAt, &request.CompletedAt,
		&request.RecordCount, &request.ManualReviewCount, &request.ErrorMessage,
		&request.JobID, &request.JobStatus, &request.JobExpiresAt,
		&request.CreatedAt, &request.UpdatedAt,
	}
	if len(extra) > 0 {
		destinations = append(destinations, extra[0])
	}
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return ReporterRequest{}, ErrNotFound
	} else if err != nil {
		return ReporterRequest{}, fmt.Errorf("scan reporter privacy request: %w", err)
	}
	return request, nil
}
