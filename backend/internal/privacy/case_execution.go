package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (Store) PlaceLegalHold(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	reason string,
	legalReference string,
	now time.Time,
) (LegalHold, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return LegalHold{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	reason = strings.TrimSpace(reason)
	legalReference = strings.TrimSpace(legalReference)
	if idempotencyKey == "" || len(idempotencyKey) > 200 ||
		reason == "" || len(reason) > 2000 || len(legalReference) > 500 {
		return LegalHold{}, ErrInvalidRequest
	}
	if _, err := uuid.Parse(caseID); err != nil {
		return LegalHold{}, ErrNotFound
	}
	var exists bool
	if err := query.QueryRow(ctx, `
		SELECT true FROM privacy_cases WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, caseID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return LegalHold{}, ErrNotFound
	} else if err != nil {
		return LegalHold{}, fmt.Errorf("authorize privacy case legal hold: %w", err)
	}

	fingerprint := sha256.Sum256([]byte(reason + "\x00" + legalReference))
	holdID := uuid.NewString()
	var storedFingerprint []byte
	var created bool
	hold, err := scanLegalHold(query.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO privacy_case_legal_holds (
				id, tenant_id, privacy_case_id, reason, legal_reference, active,
				placed_by_member_id, placed_at, idempotency_key, request_fingerprint,
				created_at, updated_at
			) VALUES ($1, $2, $3, $4, NULLIF($5, ''), true, $6, $7, $8, $9, $7, $7)
			ON CONFLICT (tenant_id, privacy_case_id, idempotency_key) DO NOTHING
			RETURNING *, true AS created
		), selected AS (
			SELECT * FROM inserted
			UNION ALL
			SELECT existing.*, false AS created
			FROM privacy_case_legal_holds AS existing
			WHERE existing.tenant_id = $2 AND existing.privacy_case_id = $3
			  AND existing.idempotency_key = $8
			  AND NOT EXISTS (SELECT 1 FROM inserted)
		)
		SELECT `+legalHoldSelectColumns("selected")+`,
		       selected.request_fingerprint, selected.created
		FROM selected
	`, holdID, tenantID, caseID, reason, legalReference, actorMemberID,
		now.UTC(), idempotencyKey, fingerprint[:]), &storedFingerprint, &created)
	if err != nil {
		return LegalHold{}, fmt.Errorf("place privacy case legal hold: %w", err)
	}
	if !bytes.Equal(storedFingerprint, fingerprint[:]) {
		return LegalHold{}, ErrIdempotencyConflict
	}
	if !created {
		return hold, nil
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "hold-place:"+idempotencyKey,
		"privacy_case.legal_hold_placed", &actorMemberID, nil, nil,
		map[string]any{
			"hold_id":         hold.ID,
			"legal_reference": hold.LegalReference,
		}, now,
	); err != nil {
		return LegalHold{}, err
	}
	if err := recordPrivacyRequestAudit(
		ctx, query, tenantID, "privacy.case_legal_hold_placed",
		actorMemberID, caseID, map[string]any{"hold_id": hold.ID},
	); err != nil {
		return LegalHold{}, err
	}
	return hold, nil
}

func (Store) ReleaseLegalHold(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	holdID string,
	actorMemberID string,
	idempotencyKey string,
	releaseReason string,
	now time.Time,
) (LegalHold, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return LegalHold{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	releaseReason = strings.TrimSpace(releaseReason)
	if idempotencyKey == "" || len(idempotencyKey) > 200 ||
		releaseReason == "" || len(releaseReason) > 2000 {
		return LegalHold{}, ErrInvalidRequest
	}
	hold, err := scanLegalHold(query.QueryRow(ctx, `
		SELECT `+legalHoldSelectColumns("legal_hold")+`
		FROM privacy_case_legal_holds AS legal_hold
		WHERE legal_hold.tenant_id = $1 AND legal_hold.privacy_case_id = $2
		  AND legal_hold.id = $3
		FOR UPDATE
	`, tenantID, caseID, holdID))
	if err != nil {
		return LegalHold{}, err
	}

	var existingHoldID, existingReason string
	err = query.QueryRow(ctx, `
		SELECT data->>'hold_id', data->>'release_reason'
		FROM privacy_case_events
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND idempotency_key = $3
	`, tenantID, caseID, "hold-release:"+idempotencyKey).Scan(
		&existingHoldID, &existingReason,
	)
	if err == nil {
		if existingHoldID != holdID || existingReason != releaseReason {
			return LegalHold{}, ErrIdempotencyConflict
		}
		return hold, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LegalHold{}, fmt.Errorf("check legal hold release idempotency: %w", err)
	}
	if !hold.Active {
		return LegalHold{}, ErrCaseTransition
	}

	hold, err = scanLegalHold(query.QueryRow(ctx, `
		UPDATE privacy_case_legal_holds
		SET active = false, released_by_member_id = $4, released_at = $5,
		    release_reason = $6
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND id = $3 AND active
		RETURNING `+legalHoldSelectColumns("privacy_case_legal_holds")+`
	`, tenantID, caseID, holdID, actorMemberID, now.UTC(), releaseReason))
	if err != nil {
		return LegalHold{}, err
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "hold-release:"+idempotencyKey,
		"privacy_case.legal_hold_released", &actorMemberID, nil, nil,
		map[string]any{"hold_id": hold.ID, "release_reason": releaseReason}, now,
	); err != nil {
		return LegalHold{}, err
	}
	if err := recordPrivacyRequestAudit(
		ctx, query, tenantID, "privacy.case_legal_hold_released",
		actorMemberID, caseID, map[string]any{"hold_id": hold.ID},
	); err != nil {
		return LegalHold{}, err
	}
	return hold, nil
}

func (Store) StartCaseExecution(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	now time.Time,
) (CaseExecution, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return CaseExecution{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return CaseExecution{}, false, ErrInvalidRequest
	}
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return CaseExecution{}, false, err
	}

	var existingEvent bool
	err = query.QueryRow(ctx, `
		SELECT true FROM privacy_case_events
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND idempotency_key = $3
	`, tenantID, caseID, "execute:"+idempotencyKey).Scan(&existingEvent)
	if err == nil {
		execution, loadErr := loadCaseExecution(ctx, query, tenantID, caseID)
		return execution, false, loadErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CaseExecution{}, false, fmt.Errorf("check case execution idempotency: %w", err)
	}
	if privacyCase.LegacyReporterRequestID != nil {
		execution, loadErr := loadCaseExecution(ctx, query, tenantID, caseID)
		return execution, false, loadErr
	}
	if privacyCase.SubjectType != SubjectReporter || privacyCase.SubjectEmail == nil ||
		privacyCase.VerifiedAt == nil || caseStatusTerminal(privacyCase.Status) ||
		privacyCase.Status == CasePendingVerification {
		return CaseExecution{}, false, ErrCaseExecutionUnsupported
	}

	var action ReporterAction
	var kind JobKind
	switch privacyCase.RightType {
	case RightAccess:
		action, kind = ReporterExport, JobReporterExport
	case RightErasure:
		var activeHolds int
		if err := query.QueryRow(ctx, `
			SELECT count(*) FROM privacy_case_legal_holds
			WHERE tenant_id = $1 AND privacy_case_id = $2 AND active
		`, tenantID, caseID).Scan(&activeHolds); err != nil {
			return CaseExecution{}, false, fmt.Errorf("check privacy case legal holds: %w", err)
		}
		if activeHolds > 0 {
			return CaseExecution{}, false, ErrLegalHoldActive
		}
		action, kind = ReporterAnonymize, JobReporterAnonymize
	default:
		return CaseExecution{}, false, ErrCaseExecutionUnsupported
	}

	var conflictingRequest bool
	digest := reporterEmailDigest(*privacyCase.SubjectEmail)
	err = query.QueryRow(ctx, `
		SELECT true
		FROM reporter_privacy_requests
		WHERE tenant_id = $1 AND email_digest = $2 AND action = $3
		  AND status IN ('pending_verification', 'processing')
		LIMIT 1
	`, tenantID, digest[:], action).Scan(&conflictingRequest)
	if err == nil {
		return CaseExecution{}, false, ErrCaseTransition
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CaseExecution{}, false, fmt.Errorf("check active reporter execution: %w", err)
	}

	_, tokenHash, err := reporterRequestToken()
	if err != nil {
		return CaseExecution{}, false, err
	}
	requestID := uuid.NewString()
	jobID := uuid.NewString()
	if _, err := query.Exec(ctx, `
		INSERT INTO reporter_privacy_requests (
			id, tenant_id, action, status, reporter_email, email_digest, token_hash,
			actor_member_id, verification_expires_at, verified_at, access_expires_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, 'processing', $4, $5, $6, $7,
			$8, $9, $10, $9, $9
		)
	`, requestID, tenantID, action, *privacyCase.SubjectEmail, digest[:], tokenHash[:],
		actorMemberID, now.UTC().Add(reporterVerificationTTL), now.UTC(),
		now.UTC().Add(reporterAccessTTL)); err != nil {
		return CaseExecution{}, false, fmt.Errorf("create case reporter execution request: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO privacy_jobs (
			id, tenant_id, kind, status, actor_member_id, reporter_request_id,
			created_at, updated_at
		) VALUES ($1, $2, $3, 'pending', $4, $5, $6, $6)
	`, jobID, tenantID, kind, actorMemberID, requestID, now.UTC()); err != nil {
		return CaseExecution{}, false, fmt.Errorf("create case execution job: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE privacy_cases SET legacy_reporter_request_id = $3
		WHERE tenant_id = $1 AND id = $2 AND legacy_reporter_request_id IS NULL
	`, tenantID, caseID, requestID); err != nil {
		return CaseExecution{}, false, fmt.Errorf("link case execution request: %w", err)
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "execute:"+idempotencyKey,
		"privacy_case.execution_started", &actorMemberID, nil, nil,
		map[string]any{
			"action": action, "privacy_job_id": jobID,
			"reporter_request_id": requestID,
		}, now,
	); err != nil {
		return CaseExecution{}, false, err
	}
	if err := recordPrivacyRequestAudit(
		ctx, query, tenantID, "privacy.case_execution_started",
		actorMemberID, caseID,
		map[string]any{"action": action, "privacy_job_id": jobID},
	); err != nil {
		return CaseExecution{}, false, err
	}
	execution, err := loadCaseExecution(ctx, query, tenantID, caseID)
	return execution, true, err
}

func attachCaseOperationalState(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	cases []PrivacyCase,
) ([]PrivacyCase, error) {
	if len(cases) == 0 {
		return cases, nil
	}
	caseIDs := make([]string, len(cases))
	caseIndex := make(map[string]int, len(cases))
	for index := range cases {
		caseIDs[index] = cases[index].ID
		caseIndex[cases[index].ID] = index
		cases[index].LegalHolds = []LegalHold{}
	}
	rows, err := query.Query(ctx, `
		SELECT `+legalHoldSelectColumns("legal_hold")+`
		FROM privacy_case_legal_holds AS legal_hold
		WHERE legal_hold.tenant_id = $1
		  AND legal_hold.privacy_case_id = ANY($2::uuid[])
		ORDER BY legal_hold.placed_at DESC, legal_hold.id DESC
	`, tenantID, caseIDs)
	if err != nil {
		return nil, fmt.Errorf("list privacy case legal holds: %w", err)
	}
	for rows.Next() {
		hold, scanErr := scanLegalHold(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		if index, exists := caseIndex[hold.PrivacyCaseID]; exists {
			cases[index].LegalHolds = append(cases[index].LegalHolds, hold)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate privacy case legal holds: %w", err)
	}
	rows.Close()

	for index := range cases {
		if cases[index].LegacyReporterRequestID == nil {
			continue
		}
		execution, loadErr := loadCaseExecution(
			ctx, query, tenantID, cases[index].ID,
		)
		if loadErr != nil {
			return nil, loadErr
		}
		cases[index].Execution = &execution
	}
	return cases, nil
}

func loadCaseExecution(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) (CaseExecution, error) {
	var execution CaseExecution
	err := query.QueryRow(ctx, `
		SELECT privacy_case.right_type, request.action, request.status,
		       job.id, job.status, job.expires_at,
		       request.record_count, request.manual_review_count, request.error_message,
		       request.created_at, request.completed_at
		FROM privacy_cases AS privacy_case
		JOIN reporter_privacy_requests AS request
		  ON request.tenant_id = privacy_case.tenant_id
		 AND request.id = privacy_case.legacy_reporter_request_id
		LEFT JOIN LATERAL (
			SELECT privacy_job.id, privacy_job.status, privacy_job.expires_at
			FROM privacy_jobs AS privacy_job
			WHERE privacy_job.tenant_id = request.tenant_id
			  AND privacy_job.reporter_request_id = request.id
			ORDER BY privacy_job.created_at DESC, privacy_job.id DESC
			LIMIT 1
		) AS job ON true
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
	`, tenantID, caseID).Scan(
		&execution.RightType, &execution.Primitive, &execution.RequestStatus, &execution.JobID,
		&execution.JobStatus, &execution.JobExpiresAt, &execution.RecordCount,
		&execution.ManualReviewCount, &execution.ErrorMessage, &execution.StartedAt,
		&execution.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CaseExecution{}, ErrNotFound
	}
	if err != nil {
		return CaseExecution{}, fmt.Errorf("load privacy case execution: %w", err)
	}
	return execution, nil
}

func ensureCaseErasureAllowed(
	ctx context.Context,
	query db.Querier,
	job Job,
) error {
	if job.ReporterRequestID == nil {
		return ErrJobUnavailable
	}
	var caseID string
	err := query.QueryRow(ctx, `
		SELECT id
		FROM privacy_cases
		WHERE tenant_id = $1 AND legacy_reporter_request_id = $2
		FOR UPDATE
	`, job.TenantID, *job.ReporterRequestID).Scan(&caseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock erasure privacy case: %w", err)
	}
	var activeHolds int
	if err := query.QueryRow(ctx, `
		SELECT count(*)
		FROM privacy_case_legal_holds
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND active
	`, job.TenantID, caseID).Scan(&activeHolds); err != nil {
		return fmt.Errorf("check erasure legal holds: %w", err)
	}
	if activeHolds > 0 {
		return ErrLegalHoldActive
	}
	return nil
}

func legalHoldSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.privacy_case_id, ` + alias + `.reason, ` +
		alias + `.legal_reference, ` + alias + `.active, ` +
		alias + `.placed_by_member_id, ` + alias + `.placed_at, ` +
		alias + `.released_by_member_id, ` + alias + `.released_at, ` +
		alias + `.release_reason, ` + alias + `.created_at, ` + alias + `.updated_at`
}

func scanLegalHold(row rowScanner, extra ...any) (LegalHold, error) {
	var hold LegalHold
	destinations := []any{
		&hold.ID, &hold.PrivacyCaseID, &hold.Reason, &hold.LegalReference,
		&hold.Active, &hold.PlacedByMemberID, &hold.PlacedAt,
		&hold.ReleasedByMemberID, &hold.ReleasedAt, &hold.ReleaseReason,
		&hold.CreatedAt, &hold.UpdatedAt,
	}
	destinations = append(destinations, extra...)
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return LegalHold{}, ErrNotFound
	} else if err != nil {
		return LegalHold{}, fmt.Errorf("scan privacy case legal hold: %w", err)
	}
	return hold, nil
}
