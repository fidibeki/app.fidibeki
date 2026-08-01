package replay

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/auth"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/privacy"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	maximumSessionBatches = 1000
	maximumSessionBytes   = 128 << 20
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
	replayConsentID string,
) (Session, error) {
	if _, err := query.Exec(ctx, `
		INSERT INTO sessions (
			id, tenant_id, project_id, session_id, page_url, started_at,
			rrweb_version, max_duration_seconds, max_events, replay_consent_id
		) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, $10)
		ON CONFLICT (project_id, session_id) DO NOTHING
	`, uuid.NewString(), tenantID, projectID, clientSessionID, pageURL, startedAt.UTC(),
		RRWebVersion, maxDurationSeconds, maxEvents, replayConsentID); err != nil {
		return Session{}, fmt.Errorf("create replay session: %w", err)
	}
	found, err := getSessionByClientID(ctx, query, tenantID, projectID, clientSessionID)
	if err == nil && found.PrivacyVersion < CurrentReplayPrivacyVersion {
		return Session{}, ErrPrivacyContract
	}
	if err == nil && (found.ReplayConsentID == nil || *found.ReplayConsentID != replayConsentID) {
		return Session{}, ErrConsentRequired
	}
	return found, err
}

func (Store) RecordConsent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	widgetRuleID string,
	noticeVersion string,
	locale string,
	source string,
	customerReference *string,
	tokenHash *string,
	state ConsentState,
	now time.Time,
	expiresAt *time.Time,
) (Consent, error) {
	var acceptedAt *time.Time
	if state == ConsentActive {
		accepted := now.UTC()
		acceptedAt = &accepted
	}
	consent, err := scanConsent(query.QueryRow(ctx, `
		INSERT INTO replay_consents (
			id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, token_hash, state, accepted_at, expires_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, 'session_replay', $5, $6, $7, $8, $9, $10, $11, $12, $13, $13
		)
		ON CONFLICT (token_hash) DO UPDATE SET updated_at = replay_consents.updated_at
		RETURNING id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, state, accepted_at, withdrawn_at, expires_at,
			created_at, updated_at
	`, uuid.NewString(), tenantID, projectID, widgetRuleID, noticeVersion, locale, source,
		customerReference, tokenHash, state, acceptedAt, expiresAt, now.UTC()))
	if err != nil {
		return Consent{}, fmt.Errorf("record replay consent: %w", err)
	}
	if consent.TenantID != tenantID || consent.ProjectID != projectID ||
		consent.WidgetRuleID != widgetRuleID || consent.NoticeVersion != noticeVersion ||
		consent.Locale != locale || consent.Source != source || consent.State != state ||
		!sameOptionalString(consent.CustomerConsentReference, customerReference) {
		return Consent{}, ErrConsentRequired
	}
	return consent, nil
}

func (Store) AuthorizeConsent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	widgetRuleID string,
	noticeVersion string,
	source string,
	tokenHash string,
	now time.Time,
) (Consent, error) {
	consent, err := scanConsent(query.QueryRow(ctx, `
		SELECT consent.id, consent.tenant_id, consent.project_id, consent.widget_rule_id,
			consent.purpose, consent.notice_version, consent.locale, consent.source,
			consent.customer_consent_reference, consent.state, consent.accepted_at,
			consent.withdrawn_at, consent.expires_at, consent.created_at, consent.updated_at
		FROM replay_consents AS consent
		JOIN widget_rules AS rule
		  ON rule.project_id = consent.project_id
		 AND rule.id = consent.widget_rule_id
		WHERE consent.tenant_id = $1
		  AND consent.project_id = $2
		  AND consent.widget_rule_id = $3
		  AND consent.purpose = 'session_replay'
		  AND consent.notice_version = $4
		  AND consent.source = $5
		  AND consent.token_hash = $6
		  AND rule.enabled
		  AND rule.replay_mode <> 'none'
		  AND rule.replay_consent->>'mode' = consent.source
		  AND rule.replay_consent->>'notice_version' = consent.notice_version
		FOR UPDATE OF consent
	`, tenantID, projectID, widgetRuleID, noticeVersion, source, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Consent{}, ErrConsentRequired
	}
	if err != nil {
		return Consent{}, fmt.Errorf("authorize replay consent: %w", err)
	}
	if consent.State == ConsentActive && consent.ExpiresAt != nil &&
		!now.UTC().Before(*consent.ExpiresAt) {
		if _, updateErr := query.Exec(ctx, `
			UPDATE replay_consents
			SET state = 'expired', updated_at = $2
			WHERE id = $1 AND state = 'active'
		`, consent.ID, now.UTC()); updateErr != nil {
			return Consent{}, fmt.Errorf("expire replay consent: %w", updateErr)
		}
		return Consent{}, errors.Join(ErrConsentRequired, ErrConsentExpired)
	}
	if consent.State != ConsentActive || consent.AcceptedAt == nil || consent.ExpiresAt == nil {
		return Consent{}, ErrConsentRequired
	}
	return consent, nil
}

func sameOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (Store) RevokeConsent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	tokenHash string,
	now time.Time,
) (Consent, error) {
	consent, err := scanConsent(query.QueryRow(ctx, `
		SELECT id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, state, accepted_at, withdrawn_at, expires_at,
			created_at, updated_at
		FROM replay_consents
		WHERE tenant_id = $1 AND project_id = $2 AND token_hash = $3
		FOR UPDATE
	`, tenantID, projectID, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return Consent{}, ErrConsentRequired
	}
	if err != nil {
		return Consent{}, fmt.Errorf("lock replay consent for revocation: %w", err)
	}
	if consent.State == ConsentWithdrawn {
		return consent, nil
	}
	if consent.State != ConsentActive {
		return Consent{}, ErrConsentRequired
	}
	withdrawnAt := now.UTC()
	consent, err = scanConsent(query.QueryRow(ctx, `
		UPDATE replay_consents
		SET state = 'withdrawn', withdrawn_at = $2, updated_at = $2
		WHERE id = $1 AND state = 'active'
		RETURNING id, tenant_id, project_id, widget_rule_id, purpose, notice_version, locale,
			source, customer_consent_reference, state, accepted_at, withdrawn_at, expires_at,
			created_at, updated_at
	`, consent.ID, withdrawnAt))
	if err != nil {
		return Consent{}, fmt.Errorf("revoke replay consent: %w", err)
	}
	return consent, nil
}

func (Store) AuthorizeSessionConsent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	projectID string,
	sessionID string,
	now time.Time,
) (Consent, error) {
	var consentID *string
	err := query.QueryRow(ctx, `
		SELECT replay_consent_id
		FROM sessions
		WHERE tenant_id = $1 AND project_id = $2 AND id = $3
		FOR SHARE
	`, tenantID, projectID, sessionID).Scan(&consentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Consent{}, ErrNotFound
	}
	if err != nil {
		return Consent{}, fmt.Errorf("lock replay session consent: %w", err)
	}
	if consentID == nil {
		return Consent{}, ErrConsentRequired
	}
	consent, err := scanConsent(query.QueryRow(ctx, `
		SELECT consent.id, consent.tenant_id, consent.project_id, consent.widget_rule_id,
			consent.purpose, consent.notice_version, consent.locale, consent.source,
			consent.customer_consent_reference, consent.state, consent.accepted_at,
			consent.withdrawn_at, consent.expires_at, consent.created_at, consent.updated_at
		FROM replay_consents AS consent
		JOIN widget_rules AS rule
		  ON rule.project_id = consent.project_id
		 AND rule.id = consent.widget_rule_id
		WHERE consent.tenant_id = $1
		  AND consent.project_id = $2
		  AND consent.id = $3
		  AND rule.enabled
		  AND rule.replay_mode <> 'none'
		  AND rule.replay_consent->>'mode' = consent.source
		  AND rule.replay_consent->>'notice_version' = consent.notice_version
		FOR UPDATE OF consent
	`, tenantID, projectID, *consentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Consent{}, ErrConsentRequired
	}
	if err != nil {
		return Consent{}, fmt.Errorf("authorize replay session consent: %w", err)
	}
	if consent.State == ConsentActive && consent.ExpiresAt != nil &&
		!now.UTC().Before(*consent.ExpiresAt) {
		if _, updateErr := query.Exec(ctx, `
			UPDATE replay_consents
			SET state = 'expired', updated_at = $2
			WHERE id = $1 AND state = 'active'
		`, consent.ID, now.UTC()); updateErr != nil {
			return Consent{}, fmt.Errorf("expire replay session consent: %w", updateErr)
		}
		return Consent{}, errors.Join(ErrConsentRequired, ErrConsentExpired)
	}
	if consent.State != ConsentActive || consent.AcceptedAt == nil || consent.ExpiresAt == nil {
		return Consent{}, ErrConsentRequired
	}
	return consent, nil
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

func (Store) GetFailureDetail(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	session Session,
) (*FailureDetail, error) {
	if session.Status() != "failed" || session.FailureCode == nil {
		return nil, nil
	}
	retryable := false
	if *session.FailureCode == FailureProcessingFailed && session.MergedAt == nil &&
		session.RetentionDeletedAt == nil && session.EndedAt != nil &&
		session.MergeRequestedAt != nil && session.MergeAttempts >= terminalMergeAttempts {
		if err := query.QueryRow(ctx, `
			SELECT EXISTS (
			           SELECT 1 FROM session_batches
			           WHERE tenant_id = $1 AND session_id = $2 AND safety_status = 'safe'
			       )
			       AND NOT EXISTS (
			           SELECT 1 FROM session_batches
			           WHERE tenant_id = $1 AND session_id = $2 AND safety_status <> 'safe'
			       )
		`, tenantID, session.ID).Scan(&retryable); err != nil {
			return nil, fmt.Errorf("inspect replay failure retryability: %w", err)
		}
	}
	return FailureDetailForCode(*session.FailureCode, retryable), nil
}

func (store Store) RetrySession(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	sessionID string,
) (Session, bool, error) {
	var endedAt, mergedAt, mergeRequestedAt, retentionDeletedAt *time.Time
	var failureCode *string
	var mergeAttempts, mergeGeneration int
	err := query.QueryRow(ctx, `
		SELECT ended_at, merged_at, merge_requested_at, retention_deleted_at,
		       failure_code, merge_attempts, merge_generation
		FROM sessions
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, sessionID).Scan(
		&endedAt, &mergedAt, &mergeRequestedAt, &retentionDeletedAt,
		&failureCode, &mergeAttempts, &mergeGeneration,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, ErrNotFound
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("lock replay session for retry: %w", err)
	}
	if mergedAt == nil && retentionDeletedAt == nil && endedAt != nil &&
		mergeRequestedAt != nil && failureCode == nil && mergeGeneration > 1 &&
		mergeAttempts < terminalMergeAttempts {
		found, getErr := store.GetSession(ctx, query, tenantID, sessionID)
		return found, false, getErr
	}
	if mergedAt != nil || retentionDeletedAt != nil || endedAt == nil ||
		mergeRequestedAt == nil || failureCode == nil ||
		*failureCode != string(FailureProcessingFailed) ||
		mergeAttempts < terminalMergeAttempts {
		return Session{}, false, ErrNotRetryable
	}
	var safe, other int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE safety_status = 'safe'),
		       count(*) FILTER (WHERE safety_status <> 'safe')
		FROM session_batches
		WHERE session_id = $1
	`, sessionID).Scan(&safe, &other); err != nil {
		return Session{}, false, fmt.Errorf("inspect replay retry batches: %w", err)
	}
	if safe == 0 || other != 0 {
		return Session{}, false, ErrNotRetryable
	}
	if _, err := query.Exec(ctx, `
		UPDATE sessions
		SET merge_generation = merge_generation + 1,
		    merge_attempts = 0,
		    merge_error = NULL,
		    failure_code = NULL,
		    merge_enqueued_at = NULL
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, sessionID); err != nil {
		return Session{}, false, fmt.Errorf("reset replay session for retry: %w", err)
	}
	found, err := store.GetSession(ctx, query, tenantID, sessionID)
	return found, true, err
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
	var endedAt, finalizeRequestedAt *time.Time
	var maxEvents int
	err := query.QueryRow(ctx, `
		SELECT ended_at, finalize_requested_at, max_events FROM sessions
		WHERE tenant_id = $1 AND project_id = $2 AND id = $3
		  AND replay_privacy_version = $4
		FOR UPDATE
	`, tenantID, projectID, sessionID, CurrentReplayPrivacyVersion).
		Scan(&endedAt, &finalizeRequestedAt, &maxEvents)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, "", ErrNotFound
	}
	if err != nil {
		return Batch{}, "", fmt.Errorf("lock replay session for batch: %w", err)
	}
	if endedAt != nil || finalizeRequestedAt != nil {
		return Batch{}, "", ErrSessionFinalized
	}
	var otherEvents, otherBatches int
	var otherBytes int64
	if err := query.QueryRow(ctx, `
		SELECT COALESCE(sum(events_count), 0)::integer,
		       COALESCE(sum(size_bytes), 0)::bigint,
		       count(*)::integer
		FROM session_batches
		WHERE session_id = $1 AND batch_index <> $2
	`, sessionID, batchIndex).Scan(&otherEvents, &otherBytes, &otherBatches); err != nil {
		return Batch{}, "", fmt.Errorf("count declared replay batches: %w", err)
	}
	if otherEvents+eventsCount > maxEvents {
		return Batch{}, "", ErrEventLimit
	}
	if otherBatches+1 > maximumSessionBatches {
		return Batch{}, "", ErrBatchLimit
	}
	if otherBytes+sizeBytes > maximumSessionBytes {
		return Batch{}, "", ErrByteLimit
	}
	rawToken, tokenHash, err := auth.NewOpaqueToken()
	if err != nil {
		return Batch{}, "", fmt.Errorf("create replay batch completion token: %w", err)
	}
	key := fmt.Sprintf("tenants/%s/sessions/%s/batches/%06d.json", tenantID, sessionID, batchIndex)
	expiresAt := now.UTC().Add(replayUploadTTL)
	batch, err := scanBatchRow(query.QueryRow(ctx, `
		INSERT INTO session_batches (
			id, tenant_id, project_id, session_id, batch_index, storage_key,
			events_count, size_bytes, content_type, complete_token_hash, upload_refreshed_at,
			upload_expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'application/json', $9, $10, $11)
		ON CONFLICT (session_id, batch_index) DO UPDATE
		SET complete_token_hash = EXCLUDED.complete_token_hash,
		    completed_at = NULL,
		    upload_refreshed_at = EXCLUDED.upload_refreshed_at,
		    upload_expires_at = EXCLUDED.upload_expires_at,
		    safety_status = 'pending',
		    safety_checked_at = NULL,
		    safety_error_reason = NULL,
		    verification_generation = session_batches.verification_generation + 1,
		    verification_enqueued_at = NULL,
		    verification_attempts = 0
		WHERE session_batches.events_count = EXCLUDED.events_count
		  AND session_batches.size_bytes = EXCLUDED.size_bytes
		  AND session_batches.content_type = EXCLUDED.content_type
		  AND (
		      session_batches.safety_status IN ('pending', 'expired')
		      OR (
		          session_batches.safety_status = 'rejected'
		          AND session_batches.verification_enqueued_at IS NULL
		      )
		  )
		RETURNING id, tenant_id, project_id, session_id, batch_index, storage_key,
		          events_count, size_bytes, content_type, complete_token_hash, completed_at,
		          safety_status, upload_expires_at, safety_checked_at, safety_error_reason,
		          verification_generation, verification_enqueued_at, verification_attempts
	`, uuid.NewString(), tenantID, projectID, sessionID, batchIndex, key,
		eventsCount, sizeBytes, tokenHash, now.UTC(), expiresAt))
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
	var endedAt *time.Time
	row := query.QueryRow(ctx, `
		SELECT batch.id, batch.tenant_id, batch.project_id, batch.session_id,
		       batch.batch_index, batch.storage_key, batch.events_count, batch.size_bytes,
		       batch.content_type, batch.complete_token_hash, batch.completed_at,
		       batch.safety_status, batch.upload_expires_at, batch.safety_checked_at,
		       batch.safety_error_reason, batch.verification_generation,
		       batch.verification_enqueued_at, batch.verification_attempts,
	       session.ended_at
		FROM session_batches AS batch
		JOIN sessions AS session ON session.id = batch.session_id
		WHERE batch.tenant_id = $1 AND batch.project_id = $2
		  AND batch.session_id = $3 AND batch.batch_index = $4
		  AND session.replay_privacy_version = $5
		FOR UPDATE OF batch
	`, tenantID, projectID, sessionID, batchIndex, CurrentReplayPrivacyVersion)
	var batch Batch
	err := row.Scan(
		&batch.ID, &batch.TenantID, &batch.ProjectID, &batch.SessionID, &batch.Index,
		&batch.StorageKey, &batch.EventsCount, &batch.SizeBytes, &batch.ContentType,
		&batch.CompleteTokenHash, &batch.CompletedAt, &batch.SafetyStatus,
		&batch.UploadExpiresAt, &batch.SafetyCheckedAt, &batch.SafetyErrorReason,
		&batch.Generation, &batch.VerificationEnqueuedAt, &batch.VerificationTries, &endedAt,
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
		SET completed_at = $3, complete_token_hash = NULL,
		    safety_status = 'quarantined', safety_checked_at = NULL,
		    safety_error_reason = NULL, verification_enqueued_at = NULL
		WHERE id = $1 AND session_id = $2
		  AND safety_status = 'pending' AND completed_at IS NULL
	`, batchID, sessionID, now.UTC())
	if err != nil {
		return fmt.Errorf("complete replay batch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBatchComplete
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
		  AND replay_privacy_version = $4
		FOR UPDATE
	`, tenantID, projectID, sessionID, CurrentReplayPrivacyVersion).
		Scan(&startedAt, &endedAt, &maxDuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, ErrNotFound
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("lock replay session for finalization: %w", err)
	}
	if err := privacy.EnsureSessionProcessingAllowed(
		ctx, query, tenantID, sessionID, privacy.PurposeReplay,
	); err != nil {
		return Session{}, false, err
	}
	if endedAt != nil {
		found, getErr := store.GetSession(ctx, query, tenantID, sessionID)
		return found, false, getErr
	}
	var pending, quarantined, rejected, expired, completed int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE safety_status IN ('pending', 'cleaning')),
		       count(*) FILTER (WHERE safety_status = 'quarantined'),
		       count(*) FILTER (WHERE safety_status = 'rejected'),
		       count(*) FILTER (WHERE safety_status = 'expired'),
		       count(*) FILTER (WHERE safety_status = 'safe')
		FROM session_batches WHERE session_id = $1
	`, sessionID).Scan(&pending, &quarantined, &rejected, &expired, &completed); err != nil {
		return Session{}, false, fmt.Errorf("inspect replay batches before finalization: %w", err)
	}
	if pending > 0 || completed+quarantined+rejected+expired == 0 {
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
	if rejected+expired > 0 {
		fallbackCode := FailureUploadExpired
		if rejected > 0 {
			fallbackCode = FailureUnsafePayload
		}
		if _, err := query.Exec(ctx, `
			UPDATE sessions
			SET finalize_requested_at = COALESCE(finalize_requested_at, $4),
			    ended_at = $4, merge_requested_at = NULL,
			    merge_enqueued_at = NULL, merge_error = $5,
			    merge_attempts = $6,
			    failure_code = CASE
			        WHEN failure_code = 'contract_violation' THEN failure_code
			        WHEN $7::text = 'contract_violation' THEN $7::text
			        WHEN failure_code = 'unsafe_payload' THEN failure_code
			        WHEN $7::text = 'unsafe_payload' THEN $7::text
			        WHEN failure_code = 'upload_expired' THEN failure_code
			        WHEN $7::text = 'upload_expired' THEN $7::text
			        ELSE COALESCE(failure_code, $7::text)
			    END
			WHERE tenant_id = $1 AND project_id = $2 AND id = $3
		`, tenantID, projectID, sessionID, ended, unavailableReplayMessage,
			terminalMergeAttempts, string(fallbackCode)); err != nil {
			return Session{}, false, fmt.Errorf("fail invalid replay session: %w", err)
		}
		found, err := store.GetSession(ctx, query, tenantID, sessionID)
		return found, false, err
	}
	if quarantined > 0 {
		if _, err := query.Exec(ctx, `
			UPDATE sessions
			SET finalize_requested_at = COALESCE(finalize_requested_at, $4)
			WHERE tenant_id = $1 AND project_id = $2 AND id = $3
		`, tenantID, projectID, sessionID, ended); err != nil {
			return Session{}, false, fmt.Errorf("request replay finalization: %w", err)
		}
		found, err := store.GetSession(ctx, query, tenantID, sessionID)
		return found, false, err
	}
	if _, err := query.Exec(ctx, `
		UPDATE sessions
		SET finalize_requested_at = COALESCE(finalize_requested_at, $4),
		    ended_at = $4, merge_requested_at = $4,
		    merge_enqueued_at = NULL, merge_error = NULL, failure_code = NULL
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
			       activity.safe_count,
			       activity.invalid_count,
			       activity.rejected_count
			FROM sessions AS session
			CROSS JOIN LATERAL (
				SELECT count(*) FILTER (WHERE batch.safety_status = 'safe') AS safe_count,
				       count(*) FILTER (
				           WHERE batch.safety_status IN ('pending', 'cleaning', 'quarantined')
				       ) AS unresolved_count,
			       count(*) FILTER (
			           WHERE batch.safety_status IN ('rejected', 'expired')
			       ) AS invalid_count,
			       count(*) FILTER (
			           WHERE batch.safety_status = 'rejected'
			       ) AS rejected_count,
				       COALESCE(
				           max(GREATEST(batch.upload_refreshed_at, COALESCE(batch.completed_at, batch.upload_refreshed_at))),
				           session.started_at
				       ) AS last_activity_at
				FROM session_batches AS batch
				WHERE batch.session_id = session.id
			) AS activity
			WHERE session.ended_at IS NULL AND session.started_at < $1
			  AND session.replay_privacy_version = $6
			  AND activity.last_activity_at < $1
			  AND activity.unresolved_count = 0
			ORDER BY activity.last_activity_at, session.id
			LIMIT $2
			FOR UPDATE OF session SKIP LOCKED
		)
		UPDATE sessions AS session
		SET ended_at = candidates.ended_at,
		    merge_requested_at = CASE
		        WHEN candidates.safe_count > 0 AND candidates.invalid_count = 0
		        THEN $3::timestamptz
		        ELSE NULL
		    END,
		    merge_enqueued_at = NULL,
		    merge_error = CASE
		        WHEN candidates.safe_count = 0 OR candidates.invalid_count > 0 THEN $4
		        ELSE NULL
		    END,
		    merge_attempts = CASE
		        WHEN candidates.safe_count = 0 OR candidates.invalid_count > 0 THEN $5
		        ELSE session.merge_attempts
		    END,
		    failure_code = CASE
		        WHEN candidates.invalid_count > 0 THEN CASE
		            WHEN session.failure_code = 'contract_violation' THEN session.failure_code
		            WHEN session.failure_code = 'unsafe_payload' THEN session.failure_code
		            WHEN candidates.rejected_count > 0 THEN 'unsafe_payload'
		            ELSE 'upload_expired'
		        END
		        WHEN candidates.safe_count = 0 THEN 'unavailable'
		        ELSE NULL
		    END
		FROM candidates
		WHERE session.id = candidates.id AND session.ended_at IS NULL
	`,
		staleBefore.UTC(),
		limit,
		now.UTC(),
		unavailableReplayMessage,
		terminalMergeAttempts,
		CurrentReplayPrivacyVersion,
	); err != nil {
		return fmt.Errorf("recover abandoned replay sessions: %w", err)
	}
	return nil
}

func (s Store) PendingMerges(ctx context.Context, limit int, staleBefore time.Time) ([]Session, error) {
	rows, err := s.Database.Query(ctx, sessionSelect+`
		WHERE session.merge_requested_at IS NOT NULL AND session.merged_at IS NULL
		  AND session.replay_privacy_version = $3
		  AND session.replay_consent_id IS NOT NULL
		  AND session.merge_attempts < 12
		  AND (session.merge_enqueued_at IS NULL OR session.merge_enqueued_at < $1)
		ORDER BY session.merge_requested_at, session.id
		LIMIT $2
	`, staleBefore.UTC(), limit, CurrentReplayPrivacyVersion)
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

func (s Store) MarkMergeEnqueued(
	ctx context.Context,
	sessionID string,
	generation int,
	now time.Time,
) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE sessions SET merge_enqueued_at = $3
		WHERE id = $1 AND merge_generation = $2 AND merged_at IS NULL
	`, sessionID, generation, now.UTC()); err != nil {
		return fmt.Errorf("mark replay merge enqueued: %w", err)
	}
	return nil
}

func (s Store) LoadMerge(
	ctx context.Context,
	sessionID string,
	generation int,
) (Session, []Batch, error) {
	found, err := scanSessionRow(s.Database.QueryRow(ctx, sessionSelect+`
		WHERE session.id = $1 AND session.merge_requested_at IS NOT NULL
		  AND session.replay_privacy_version = $2
		  AND session.merge_generation = $3
	`, sessionID, CurrentReplayPrivacyVersion, generation))
	if err != nil {
		return Session{}, nil, err
	}
	if found.ReplayConsentID == nil {
		return Session{}, nil, ErrConsentRequired
	}
	if err := privacy.EnsureSessionProcessingAllowed(
		ctx, s.Database, found.TenantID, found.ID, privacy.PurposeReplay,
	); err != nil {
		return Session{}, nil, err
	}
	var consentEvidenceValid bool
	if err := s.Database.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM replay_consents
			WHERE id = $1
			  AND tenant_id = $2
			  AND project_id = $3
			  AND purpose = 'session_replay'
			  AND accepted_at IS NOT NULL
			  AND state IN ('active', 'withdrawn', 'expired')
		)
	`, *found.ReplayConsentID, found.TenantID, found.ProjectID).Scan(&consentEvidenceValid); err != nil {
		return Session{}, nil, fmt.Errorf("verify replay merge consent evidence: %w", err)
	}
	if !consentEvidenceValid {
		return Session{}, nil, ErrConsentRequired
	}
	rows, err := s.Database.Query(ctx, `
		SELECT id, tenant_id, project_id, session_id, batch_index, storage_key,
		       events_count, size_bytes, content_type, complete_token_hash, completed_at,
		       safety_status, upload_expires_at, safety_checked_at, safety_error_reason,
		       verification_generation, verification_enqueued_at, verification_attempts
		FROM session_batches
		WHERE session_id = $1 AND safety_status = 'safe'
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
			&batch.CompleteTokenHash, &batch.CompletedAt, &batch.SafetyStatus,
			&batch.UploadExpiresAt, &batch.SafetyCheckedAt, &batch.SafetyErrorReason,
			&batch.Generation, &batch.VerificationEnqueuedAt, &batch.VerificationTries,
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
		    merge_error = NULL, failure_code = NULL,
		    merge_attempts = merge_attempts + 1
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
		    merge_enqueued_at = NULL,
		    failure_code = CASE
		        WHEN merge_attempts + 1 >= $3 THEN 'processing_failed'
		        ELSE NULL
		    END
		WHERE id = $1 AND merged_at IS NULL
	`, sessionID, message, terminalMergeAttempts); err != nil {
		return fmt.Errorf("mark replay merge failed: %w", err)
	}
	return nil
}

func (s Store) MarkUnavailable(ctx context.Context, sessionID string) error {
	if _, err := s.Database.Exec(ctx, `
		UPDATE sessions
		SET merge_error = $2, merge_attempts = $3, merge_enqueued_at = NULL,
		    failure_code = 'unavailable'
		WHERE id = $1 AND merged_at IS NULL
	`, sessionID, unavailableReplayMessage, terminalMergeAttempts); err != nil {
		return fmt.Errorf("mark replay unavailable: %w", err)
	}
	return nil
}

func (s Store) DeleteBatch(ctx context.Context, batchID string) error {
	if _, err := s.Database.Exec(ctx, `
		DELETE FROM session_batches AS batch
		WHERE batch.id = $1
		  AND batch.verification_enqueued_at IS NULL
		  AND EXISTS (
		      SELECT 1
		      FROM sessions AS session
		      WHERE session.id = batch.session_id
		        AND session.merged_at IS NOT NULL
		  )
	`, batchID); err != nil {
		return fmt.Errorf("delete replay batch record: %w", err)
	}
	return nil
}

const sessionSelect = `
	SELECT session.id, session.tenant_id, session.project_id, session.feedback_id,
	       session.session_id, session.page_url, session.started_at, session.ended_at,
	       session.events_count, session.batches_count, session.storage_key,
	       session.rrweb_version, session.merged_at, session.merge_requested_at,
	       session.finalize_requested_at, session.merge_enqueued_at,
	       session.merge_attempts, session.merge_error, session.merge_generation,
	       session.failure_code,
	       session.retention_deleted_at, session.replay_privacy_version,
	       session.max_duration_seconds, session.max_events, session.replay_consent_id,
	       session.created_at
	FROM sessions AS session
`

type rowScanner interface {
	Scan(...any) error
}

func scanBatchRow(row rowScanner) (Batch, error) {
	var batch Batch
	err := row.Scan(
		&batch.ID, &batch.TenantID, &batch.ProjectID, &batch.SessionID, &batch.Index,
		&batch.StorageKey, &batch.EventsCount, &batch.SizeBytes, &batch.ContentType,
		&batch.CompleteTokenHash, &batch.CompletedAt, &batch.SafetyStatus,
		&batch.UploadExpiresAt, &batch.SafetyCheckedAt, &batch.SafetyErrorReason,
		&batch.Generation, &batch.VerificationEnqueuedAt, &batch.VerificationTries,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, pgx.ErrNoRows
	}
	if err != nil {
		return Batch{}, fmt.Errorf("scan replay batch: %w", err)
	}
	return batch, nil
}

func scanSessionRow(row rowScanner) (Session, error) {
	var found Session
	err := row.Scan(
		&found.ID, &found.TenantID, &found.ProjectID, &found.FeedbackID,
		&found.ClientSessionID, &found.PageURL, &found.StartedAt, &found.EndedAt,
		&found.EventsCount, &found.BatchesCount, &found.StorageKey,
		&found.RRWebVersion, &found.MergedAt, &found.MergeRequestedAt,
		&found.FinalizeRequestedAt, &found.MergeEnqueuedAt, &found.MergeAttempts, &found.MergeError,
		&found.MergeGeneration, &found.FailureCode,
		&found.RetentionDeletedAt, &found.PrivacyVersion,
		&found.MaxDurationSeconds, &found.MaxEvents, &found.ReplayConsentID, &found.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("scan replay session: %w", err)
	}
	return found, nil
}

func scanConsent(row rowScanner) (Consent, error) {
	var consent Consent
	err := row.Scan(
		&consent.ID, &consent.TenantID, &consent.ProjectID, &consent.WidgetRuleID,
		&consent.Purpose, &consent.NoticeVersion, &consent.Locale, &consent.Source,
		&consent.CustomerConsentReference, &consent.State, &consent.AcceptedAt,
		&consent.WithdrawnAt, &consent.ExpiresAt, &consent.CreatedAt, &consent.UpdatedAt,
	)
	return consent, err
}
