package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	privacyCaseDeadlineTTL     = 30 * 24 * time.Hour
	privacyCaseVerificationTTL = 48 * time.Hour
)

func (Store) CreateCase(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	idempotencyKey string,
	input CreateCaseInput,
	now time.Time,
) (PrivacyCase, string, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return PrivacyCase{}, "", false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return PrivacyCase{}, "", false, ErrInvalidRequest
	}
	normalized, err := normalizeCreateCaseInput(ctx, query, tenantID, input)
	if err != nil {
		return PrivacyCase{}, "", false, err
	}
	fingerprint, err := caseInputFingerprint(normalized)
	if err != nil {
		return PrivacyCase{}, "", false, err
	}

	caseID := uuid.NewString()
	publicID := "PRV-" + strings.ToUpper(strings.ReplaceAll(caseID[:8], "-", ""))
	status := CaseInProgress
	var reporterEmail *string
	var emailDigest []byte
	var subjectMemberID *string
	var tokenHash []byte
	var verificationExpiresAt *time.Time
	var verificationMethod *string
	var verifiedAt *time.Time
	rawToken := ""
	if normalized.SubjectType == SubjectReporter {
		email := normalized.SubjectEmail
		reporterEmail = &email
		digest := reporterEmailDigest(email)
		emailDigest = digest[:]
		token, hash, tokenErr := reporterRequestToken()
		if tokenErr != nil {
			return PrivacyCase{}, "", false, tokenErr
		}
		rawToken = token
		tokenHash = hash[:]
		expires := now.UTC().Add(privacyCaseVerificationTTL)
		verificationExpiresAt = &expires
		status = CasePendingVerification
	} else {
		memberID := normalized.SubjectMemberID
		subjectMemberID = &memberID
		method := "workspace_membership"
		verificationMethod = &method
		verified := now.UTC()
		verifiedAt = &verified
	}
	var ownerMemberID *string
	if normalized.OwnerMemberID != "" {
		owner := normalized.OwnerMemberID
		ownerMemberID = &owner
	}
	scopeJSON, err := json.Marshal(normalized.Scope)
	if err != nil {
		return PrivacyCase{}, "", false, fmt.Errorf("encode privacy case scope: %w", err)
	}
	storedFingerprint := []byte(nil)
	created := false
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO privacy_cases (
				id, tenant_id, public_id, subject_type, right_type, controller_role, status,
				subject_email, subject_email_digest, subject_member_id, requested_by_member_id,
				owner_member_id, verification_token_hash, verification_expires_at,
				verification_method, verified_at, received_at, deadline_at, scope,
				idempotency_key, request_fingerprint, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
				$15, $16, $17, $18, $19::jsonb, $20, $21, $17, $17
			)
			ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
			RETURNING *, true AS created
		), selected AS (
			SELECT * FROM inserted
			UNION ALL
			SELECT existing.*, false AS created
			FROM privacy_cases AS existing
			WHERE existing.tenant_id = $2 AND existing.idempotency_key = $20
			  AND NOT EXISTS (SELECT 1 FROM inserted)
		)
		SELECT `+privacyCaseSelectColumns("selected")+`, selected.request_fingerprint, selected.created
		FROM selected
	`, caseID, tenantID, publicID, normalized.SubjectType, normalized.RightType,
		normalized.ControllerRole, status, reporterEmail, emailDigest, subjectMemberID,
		actorMemberID, ownerMemberID, tokenHash, verificationExpiresAt, verificationMethod,
		verifiedAt, now.UTC(), now.UTC().Add(privacyCaseDeadlineTTL), string(scopeJSON),
		idempotencyKey, fingerprint[:]), &storedFingerprint, &created)
	if err != nil {
		return PrivacyCase{}, "", false, fmt.Errorf("create privacy case: %w", err)
	}
	if !bytes.Equal(storedFingerprint, fingerprint[:]) {
		return PrivacyCase{}, "", false, ErrIdempotencyConflict
	}
	if !created {
		return privacyCase, "", false, nil
	}
	if err := insertCaseEvent(ctx, query, tenantID, privacyCase.ID, "create:"+idempotencyKey,
		"privacy_case.created", &actorMemberID, nil, &privacyCase.Status, map[string]any{
			"subject_type":    normalized.SubjectType,
			"right_type":      normalized.RightType,
			"controller_role": normalized.ControllerRole,
		}, now); err != nil {
		return PrivacyCase{}, "", false, err
	}
	if privacyCase.Status == CaseInProgress {
		restriction, restrictionErr := activateRestrictionForRight(
			ctx, query, privacyCase, "verified data-rights request", now,
		)
		if restrictionErr != nil {
			return PrivacyCase{}, "", false, restrictionErr
		}
		privacyCase.Restriction = restriction
	}
	if err := recordPrivacyRequestAudit(ctx, query, tenantID, "privacy.case_created",
		actorMemberID, privacyCase.ID, map[string]any{
			"public_id":             privacyCase.PublicID,
			"subject_type":          privacyCase.SubjectType,
			"right_type":            privacyCase.RightType,
			"reporter_email_digest": reporterDigestHex(emailDigest),
		}); err != nil {
		return PrivacyCase{}, "", false, err
	}
	return privacyCase, rawToken, normalized.SubjectType == SubjectReporter, nil
}

func normalizeCreateCaseInput(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	input CreateCaseInput,
) (CreateCaseInput, error) {
	if _, err := ParseSubjectType(string(input.SubjectType)); err != nil {
		return CreateCaseInput{}, err
	}
	if _, err := ParseRightType(string(input.RightType)); err != nil {
		return CreateCaseInput{}, err
	}
	if _, err := ParseControllerRole(string(input.ControllerRole)); err != nil {
		return CreateCaseInput{}, err
	}
	if input.Scope == nil {
		input.Scope = map[string]any{}
	}
	if input.SubjectType == SubjectReporter {
		email, err := NormalizeReporterEmail(input.SubjectEmail)
		if err != nil || strings.TrimSpace(input.SubjectMemberID) != "" {
			return CreateCaseInput{}, ErrInvalidRequest
		}
		input.SubjectEmail = email
		input.SubjectMemberID = ""
	} else {
		input.SubjectEmail = ""
		if _, err := uuid.Parse(input.SubjectMemberID); err != nil {
			return CreateCaseInput{}, ErrInvalidRequest
		}
		if err := requireActiveHuman(ctx, query, tenantID, input.SubjectMemberID); err != nil {
			return CreateCaseInput{}, ErrInvalidRequest
		}
	}
	if input.OwnerMemberID != "" {
		if _, err := uuid.Parse(input.OwnerMemberID); err != nil {
			return CreateCaseInput{}, ErrInvalidRequest
		}
		if err := requireActiveHuman(ctx, query, tenantID, input.OwnerMemberID); err != nil {
			return CreateCaseInput{}, ErrInvalidRequest
		}
	}
	return input, nil
}

func caseInputFingerprint(input CreateCaseInput) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode privacy case idempotency input: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func (Store) ListCases(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	limit int,
) ([]PrivacyCase, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := query.Query(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1
		ORDER BY privacy_case.received_at DESC, privacy_case.id DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list privacy cases: %w", err)
	}
	cases := make([]PrivacyCase, 0)
	for rows.Next() {
		privacyCase, scanErr := scanPrivacyCase(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		cases = append(cases, privacyCase)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate privacy cases: %w", err)
	}
	rows.Close()
	cases, err = attachCaseRestrictions(ctx, query, tenantID, cases)
	if err != nil {
		return nil, err
	}
	return attachCaseOperationalState(ctx, query, tenantID, cases)
}

func attachCaseRestrictions(
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
	}
	rows, err := query.Query(ctx, `
		SELECT id, privacy_case_id, purposes, active, reason, activated_at,
		       deactivated_at, deactivated_by_member_id
		FROM privacy_processing_restrictions
		WHERE tenant_id = $1 AND privacy_case_id = ANY($2::uuid[])
	`, tenantID, caseIDs)
	if err != nil {
		return nil, fmt.Errorf("list privacy case restrictions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var restriction Restriction
		var storedPurposes []string
		if err := rows.Scan(
			&restriction.ID, &restriction.PrivacyCaseID, &storedPurposes,
			&restriction.Active, &restriction.Reason, &restriction.ActivatedAt,
			&restriction.DeactivatedAt, &restriction.DeactivatedByMemberID,
		); err != nil {
			return nil, fmt.Errorf("scan privacy case restriction: %w", err)
		}
		restriction.Purposes = processingPurposes(storedPurposes)
		if index, exists := caseIndex[restriction.PrivacyCaseID]; exists {
			cases[index].Restriction = &restriction
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy case restrictions: %w", err)
	}
	return cases, nil
}

func (Store) GetCase(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) (PrivacyCase, error) {
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
	`, tenantID, caseID))
	if err != nil {
		return PrivacyCase{}, err
	}
	restriction, err := loadCaseRestriction(ctx, query, tenantID, caseID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return PrivacyCase{}, err
	}
	if err == nil {
		privacyCase.Restriction = &restriction
	}
	cases, err := attachCaseOperationalState(
		ctx, query, tenantID, []PrivacyCase{privacyCase},
	)
	if err != nil {
		return PrivacyCase{}, err
	}
	return cases[0], nil
}

func (Store) ListCaseEvents(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) ([]CaseEvent, error) {
	rows, err := query.Query(ctx, `
		SELECT id, sequence, privacy_case_id, event_type, actor_member_id,
		       previous_status, new_status, data, occurred_at
		FROM privacy_case_events
		WHERE tenant_id = $1 AND privacy_case_id = $2
		ORDER BY sequence
	`, tenantID, caseID)
	if err != nil {
		return nil, fmt.Errorf("list privacy case events: %w", err)
	}
	defer rows.Close()
	events := make([]CaseEvent, 0)
	for rows.Next() {
		var event CaseEvent
		var data []byte
		if err := rows.Scan(&event.ID, &event.Sequence, &event.PrivacyCaseID, &event.EventType,
			&event.ActorMemberID, &event.PreviousStatus, &event.NewStatus, &data,
			&event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan privacy case event: %w", err)
		}
		if err := json.Unmarshal(data, &event.Data); err != nil {
			return nil, fmt.Errorf("decode privacy case event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privacy case events: %w", err)
	}
	return events, nil
}

func (Store) TransitionCase(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	input TransitionCaseInput,
	now time.Time,
) (PrivacyCase, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return PrivacyCase{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return PrivacyCase{}, ErrInvalidRequest
	}
	current, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return PrivacyCase{}, err
	}
	decision := optionalTrimmed(input.DecisionSummary)
	legalNotes := optionalTrimmed(input.LegalReviewNotes)
	exception := optionalTrimmed(input.ExceptionReason)
	var existingType string
	var existingStatus *CaseStatus
	var existingData []byte
	err = query.QueryRow(ctx, `
		SELECT event_type, new_status, data
		FROM privacy_case_events
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND idempotency_key = $3
	`, tenantID, caseID, "transition:"+idempotencyKey).Scan(
		&existingType, &existingStatus, &existingData,
	)
	if err == nil {
		var evidence struct {
			DecisionSummary  *string `json:"decision_summary"`
			LegalReviewNotes *string `json:"legal_review_notes"`
			ExceptionReason  *string `json:"exception_reason"`
		}
		if unmarshalErr := json.Unmarshal(existingData, &evidence); unmarshalErr != nil {
			return PrivacyCase{}, fmt.Errorf(
				"decode privacy case transition evidence: %w", unmarshalErr,
			)
		}
		if existingType != "privacy_case.transitioned" || existingStatus == nil ||
			*existingStatus != input.Status ||
			!equalOptionalString(evidence.DecisionSummary, decision) ||
			!equalOptionalString(evidence.LegalReviewNotes, legalNotes) ||
			!equalOptionalString(evidence.ExceptionReason, exception) {
			return PrivacyCase{}, ErrIdempotencyConflict
		}
		return current, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PrivacyCase{}, fmt.Errorf("check privacy case transition idempotency: %w", err)
	}
	if !validCaseTransition(current.Status, input.Status) {
		return PrivacyCase{}, ErrCaseTransition
	}
	if (input.Status == CaseRefused || input.Status == CasePartiallyFulfilled) && decision == nil {
		return PrivacyCase{}, ErrCaseReasonRequired
	}
	if current.RightType == RightErasure &&
		(input.Status == CaseRefused || input.Status == CasePartiallyFulfilled) &&
		(legalNotes == nil || exception == nil) {
		return PrivacyCase{}, ErrCaseReasonRequired
	}
	if err := validateErasureTerminalTransition(
		ctx, query, current, input.Status,
	); err != nil {
		return PrivacyCase{}, err
	}
	pausedSeconds := current.PausedSeconds
	var pauseStarted *time.Time
	if current.Status == CasePaused && current.PauseStartedAt != nil {
		pausedSeconds += int64(now.UTC().Sub(*current.PauseStartedAt).Seconds())
	}
	if input.Status == CasePaused {
		pause := now.UTC()
		pauseStarted = &pause
	}
	var completedAt *time.Time
	if caseStatusTerminal(input.Status) {
		completed := now.UTC()
		completedAt = &completed
	}
	updated, err := scanPrivacyCase(query.QueryRow(ctx, `
		UPDATE privacy_cases
		SET status = $3, decision_summary = $4, legal_review_notes = $5,
		    exception_reason = $6, pause_started_at = $7, paused_seconds = $8,
		    completed_at = $9
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+privacyCaseSelectColumns("privacy_cases")+`
	`, tenantID, caseID, input.Status, decision, legalNotes, exception, pauseStarted,
		pausedSeconds, completedAt))
	if err != nil {
		return PrivacyCase{}, fmt.Errorf("transition privacy case: %w", err)
	}
	if err := insertCaseEvent(ctx, query, tenantID, caseID, "transition:"+idempotencyKey,
		"privacy_case.transitioned", &actorMemberID, &current.Status, &updated.Status,
		map[string]any{
			"decision_summary":   decision,
			"legal_review_notes": legalNotes,
			"exception_reason":   exception,
		}, now); err != nil {
		return PrivacyCase{}, err
	}
	return updated, nil
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateErasureTerminalTransition(
	ctx context.Context,
	query db.Querier,
	privacyCase PrivacyCase,
	target CaseStatus,
) error {
	if privacyCase.RightType != RightErasure ||
		(target != CaseCompleted && target != CasePartiallyFulfilled &&
			target != CaseRefused) {
		return nil
	}
	if target == CaseRefused {
		if privacyCase.LegacyReporterRequestID != nil {
			return ErrCaseTransition
		}
		return nil
	}
	execution, err := loadCaseExecution(
		ctx, query, privacyCase.TenantID, privacyCase.ID,
	)
	if errors.Is(err, ErrNotFound) {
		return ErrCaseTransition
	}
	if err != nil {
		return err
	}
	if execution.RequestStatus == nil || *execution.RequestStatus != ReporterCompleted ||
		execution.JobStatus == nil || *execution.JobStatus != JobCompleted ||
		execution.ErasureReview == nil || execution.ErasureReview.Pending > 0 {
		return ErrCaseTransition
	}
	var activeHolds int
	if err := query.QueryRow(ctx, `
		SELECT count(*)
		FROM privacy_case_legal_holds
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND active
	`, privacyCase.TenantID, privacyCase.ID).Scan(&activeHolds); err != nil {
		return fmt.Errorf("check terminal erasure legal holds: %w", err)
	}
	hasException := execution.ErasureReview.RetainedException > 0 || activeHolds > 0
	if target == CaseCompleted && hasException {
		return ErrCaseTransition
	}
	if target == CasePartiallyFulfilled && !hasException {
		return ErrCaseTransition
	}
	return nil
}

func validCaseTransition(from, to CaseStatus) bool {
	if from == to {
		return true
	}
	if caseStatusTerminal(from) || from == CasePendingVerification {
		return false
	}
	switch to {
	case CaseInProgress:
		return from == CasePaused || from == CaseAwaitingClarification ||
			from == CaseAwaitingLegalReview || from == CaseReceived
	case CasePaused, CaseAwaitingClarification, CaseAwaitingLegalReview:
		return from == CaseInProgress
	case CasePartiallyFulfilled, CaseCompleted, CaseRefused, CaseCancelled:
		return from == CaseInProgress || from == CaseAwaitingClarification ||
			from == CaseAwaitingLegalReview || from == CasePaused
	default:
		return false
	}
}

func caseStatusTerminal(status CaseStatus) bool {
	return status == CasePartiallyFulfilled || status == CaseCompleted ||
		status == CaseRefused || status == CaseCancelled
}

func (Store) VerifyCase(
	ctx context.Context,
	query db.Querier,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (ReporterCaseView, error) {
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.verification_token_hash = $1
		FOR UPDATE
	`, tokenHash[:]))
	if err != nil {
		return ReporterCaseView{}, err
	}
	if privacyCase.Status != CasePendingVerification ||
		privacyCase.VerificationExpiresAt == nil ||
		!privacyCase.VerificationExpiresAt.After(now.UTC()) {
		return ReporterCaseView{}, ErrNotFound
	}
	method := "email_link"
	verifiedAt := now.UTC()
	updated, err := scanPrivacyCase(query.QueryRow(ctx, `
		UPDATE privacy_cases
		SET status = 'in_progress', verification_token_hash = NULL,
		    verification_method = $2, verified_at = $3
		WHERE id = $1 AND status = 'pending_verification'
		RETURNING `+privacyCaseSelectColumns("privacy_cases")+`
	`, privacyCase.ID, method, verifiedAt))
	if err != nil {
		return ReporterCaseView{}, fmt.Errorf("verify privacy case: %w", err)
	}
	if err := insertCaseEvent(ctx, query, updated.TenantID, updated.ID, "verify",
		"privacy_case.verified", nil, &privacyCase.Status, &updated.Status,
		map[string]any{"verification_method": method}, now); err != nil {
		return ReporterCaseView{}, err
	}
	if _, err := activateRestrictionForRight(
		ctx, query, updated, "verified data-rights request", now,
	); err != nil {
		return ReporterCaseView{}, err
	}
	return reporterCaseView(updated), nil
}

func (store Store) GetCaseView(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) (ReporterCaseView, error) {
	if store.Database == nil {
		return ReporterCaseView{}, errors.New("privacy case database is unavailable")
	}
	privacyCase, err := scanPrivacyCase(store.Database.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.verification_token_hash = $1
		  AND privacy_case.verification_expires_at > $2
	`, tokenHash[:], now.UTC()))
	if err != nil {
		return ReporterCaseView{}, err
	}
	return reporterCaseView(privacyCase), nil
}

func reporterCaseView(privacyCase PrivacyCase) ReporterCaseView {
	return ReporterCaseView{
		PublicID: privacyCase.PublicID, RightType: privacyCase.RightType,
		Status: privacyCase.Status, ReceivedAt: privacyCase.ReceivedAt,
		DeadlineAt: privacyCase.DeadlineAt,
	}
}

func (Store) CaseDashboard(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	now time.Time,
) (CaseDashboard, error) {
	var dashboard CaseDashboard
	err := query.QueryRow(ctx, `
		SELECT
			count(*) FILTER (
				WHERE status NOT IN ('partially_fulfilled', 'completed', 'refused', 'cancelled')
				  AND deadline_at < $2
			),
			count(*) FILTER (
				WHERE status NOT IN ('partially_fulfilled', 'completed', 'refused', 'cancelled')
				  AND deadline_at >= $2 AND deadline_at < $2::timestamptz + interval '7 days'
			),
			count(*) FILTER (WHERE status IN ('awaiting_clarification', 'paused')),
			count(*) FILTER (WHERE status = 'awaiting_legal_review'),
			(
				SELECT count(DISTINCT failed.privacy_case_id)
				FROM (
					SELECT action.privacy_case_id
					FROM privacy_recipient_actions AS action
					WHERE action.tenant_id = $1 AND action.status = 'failed'
					UNION
					SELECT communication.privacy_case_id
					FROM privacy_case_communications AS communication
					WHERE communication.tenant_id = $1 AND communication.status = 'failed'
				) AS failed
			),
			count(*) FILTER (WHERE status IN ('partially_fulfilled', 'completed'))
		FROM privacy_cases
		WHERE tenant_id = $1
	`, tenantID, now.UTC()).Scan(
		&dashboard.Overdue, &dashboard.DueSoon, &dashboard.AwaitingManual,
		&dashboard.AwaitingLegalReview, &dashboard.FailedExternal, &dashboard.Completed,
	)
	if err != nil {
		return CaseDashboard{}, fmt.Errorf("load privacy case dashboard: %w", err)
	}
	return dashboard, nil
}

func (Store) SetCaseRestriction(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	active bool,
	reason string,
	now time.Time,
) (Restriction, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return Restriction{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	reason = strings.TrimSpace(reason)
	if idempotencyKey == "" || len(idempotencyKey) > 200 ||
		reason == "" || len(reason) > 2000 {
		return Restriction{}, ErrInvalidRequest
	}
	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return Restriction{}, err
	}
	if privacyCase.VerifiedAt == nil || len(restrictionPurposes(privacyCase.RightType)) == 0 {
		return Restriction{}, ErrCaseTransition
	}
	var existingActive *bool
	err = query.QueryRow(ctx, `
		SELECT (data->>'active')::boolean
		FROM privacy_case_events
		WHERE tenant_id = $1 AND privacy_case_id = $2 AND idempotency_key = $3
	`, tenantID, caseID, "restriction:"+idempotencyKey).Scan(&existingActive)
	if err == nil {
		if existingActive == nil || *existingActive != active {
			return Restriction{}, ErrIdempotencyConflict
		}
		return loadCaseRestriction(ctx, query, tenantID, caseID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Restriction{}, fmt.Errorf("check restriction command idempotency: %w", err)
	}
	restriction, err := loadCaseRestriction(ctx, query, tenantID, caseID)
	if errors.Is(err, ErrNotFound) && active {
		created, activateErr := activateRestrictionForRight(
			ctx, query, privacyCase, reason, now,
		)
		if activateErr != nil {
			return Restriction{}, activateErr
		}
		restriction = *created
	} else if err != nil {
		return Restriction{}, err
	} else if restriction.Active != active {
		var deactivatedAt *time.Time
		var deactivatedBy *string
		if !active {
			deactivated := now.UTC()
			deactivatedAt = &deactivated
			deactivatedBy = &actorMemberID
		}
		var storedPurposes []string
		err = query.QueryRow(ctx, `
			UPDATE privacy_processing_restrictions
			SET active = $3, reason = $4,
			    activated_at = CASE WHEN $3 THEN $5 ELSE activated_at END,
			    deactivated_at = $6, deactivated_by_member_id = $7
			WHERE tenant_id = $1 AND privacy_case_id = $2
			RETURNING id, privacy_case_id, purposes, active, reason, activated_at,
			          deactivated_at, deactivated_by_member_id
		`, tenantID, caseID, active, reason, now.UTC(), deactivatedAt, deactivatedBy).Scan(
			&restriction.ID, &restriction.PrivacyCaseID, &storedPurposes,
			&restriction.Active, &restriction.Reason, &restriction.ActivatedAt,
			&restriction.DeactivatedAt, &restriction.DeactivatedByMemberID,
		)
		if err != nil {
			return Restriction{}, fmt.Errorf("update privacy processing restriction: %w", err)
		}
		restriction.Purposes = processingPurposes(storedPurposes)
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "restriction:"+idempotencyKey,
		"privacy_case.restriction_updated", &actorMemberID, nil, nil,
		map[string]any{"active": active, "reason": reason}, now,
	); err != nil {
		return Restriction{}, err
	}
	return restriction, nil
}

func activateRestrictionForRight(
	ctx context.Context,
	query db.Querier,
	privacyCase PrivacyCase,
	reason string,
	now time.Time,
) (*Restriction, error) {
	purposes := restrictionPurposes(privacyCase.RightType)
	if len(purposes) == 0 {
		return nil, nil
	}
	var restriction Restriction
	purposeValues := make([]string, len(purposes))
	for index, purpose := range purposes {
		purposeValues[index] = string(purpose)
	}
	var storedPurposes []string
	err := query.QueryRow(ctx, `
		INSERT INTO privacy_processing_restrictions (
			tenant_id, privacy_case_id, purposes, active, reason, activated_at
		) VALUES ($1, $2, $3, true, $4, $5)
		ON CONFLICT (tenant_id, privacy_case_id) DO UPDATE SET
			purposes = EXCLUDED.purposes,
			active = true,
			reason = EXCLUDED.reason,
			activated_at = CASE
				WHEN privacy_processing_restrictions.active
				THEN privacy_processing_restrictions.activated_at
				ELSE EXCLUDED.activated_at
			END,
			deactivated_at = NULL,
			deactivated_by_member_id = NULL
		RETURNING id, privacy_case_id, purposes, active, reason, activated_at,
		          deactivated_at, deactivated_by_member_id
	`, privacyCase.TenantID, privacyCase.ID, purposeValues, reason, now.UTC()).Scan(
		&restriction.ID, &restriction.PrivacyCaseID, &storedPurposes,
		&restriction.Active, &restriction.Reason, &restriction.ActivatedAt,
		&restriction.DeactivatedAt, &restriction.DeactivatedByMemberID,
	)
	if err != nil {
		return nil, fmt.Errorf("activate privacy processing restriction: %w", err)
	}
	restriction.Purposes = processingPurposes(storedPurposes)
	return &restriction, nil
}

func restrictionPurposes(rightType RightType) []ProcessingPurpose {
	switch rightType {
	case RightRestriction:
		return []ProcessingPurpose{
			PurposeAI, PurposeIntegration, PurposeReporterEmail, PurposeReplay,
			PurposeAnalytics, PurposeBackgroundProcessing,
		}
	case RightObjection:
		return []ProcessingPurpose{
			PurposeAI, PurposeIntegration, PurposeAnalytics, PurposeBackgroundProcessing,
		}
	case RightConsentWithdrawal:
		return []ProcessingPurpose{
			PurposeAI, PurposeIntegration, PurposeReporterEmail, PurposeReplay, PurposeAnalytics,
		}
	default:
		return nil
	}
}

func loadCaseRestriction(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) (Restriction, error) {
	var restriction Restriction
	var storedPurposes []string
	err := query.QueryRow(ctx, `
		SELECT id, privacy_case_id, purposes, active, reason, activated_at,
		       deactivated_at, deactivated_by_member_id
		FROM privacy_processing_restrictions
		WHERE tenant_id = $1 AND privacy_case_id = $2
	`, tenantID, caseID).Scan(
		&restriction.ID, &restriction.PrivacyCaseID, &storedPurposes,
		&restriction.Active, &restriction.Reason, &restriction.ActivatedAt,
		&restriction.DeactivatedAt, &restriction.DeactivatedByMemberID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Restriction{}, ErrNotFound
	}
	if err != nil {
		return Restriction{}, fmt.Errorf("load privacy processing restriction: %w", err)
	}
	restriction.Purposes = processingPurposes(storedPurposes)
	return restriction, nil
}

func processingPurposes(values []string) []ProcessingPurpose {
	purposes := make([]ProcessingPurpose, len(values))
	for index, value := range values {
		purposes[index] = ProcessingPurpose(value)
	}
	return purposes
}

func insertCaseEvent(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	idempotencyKey string,
	eventType string,
	actorMemberID *string,
	previousStatus *CaseStatus,
	newStatus *CaseStatus,
	data map[string]any,
	now time.Time,
) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode privacy case event: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO privacy_case_events (
			tenant_id, privacy_case_id, idempotency_key, event_type, actor_member_id,
			previous_status, new_status, data, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)
		ON CONFLICT (tenant_id, privacy_case_id, idempotency_key) DO NOTHING
	`, tenantID, caseID, idempotencyKey, eventType, actorMemberID, previousStatus,
		newStatus, string(encoded), now.UTC()); err != nil {
		return fmt.Errorf("record privacy case event: %w", err)
	}
	return nil
}

func privacyCaseSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.tenant_id, ` + alias + `.public_id, ` +
		alias + `.subject_type, ` + alias + `.right_type, ` + alias + `.controller_role, ` +
		alias + `.status, ` + alias + `.subject_email, ` + alias + `.subject_member_id, ` +
		alias + `.requested_by_member_id, ` + alias + `.owner_member_id, ` +
		alias + `.verification_expires_at, ` + alias + `.verification_method, ` +
		alias + `.verified_at, ` + alias + `.received_at, ` + alias + `.deadline_at, ` +
		alias + `.pause_started_at, ` + alias + `.paused_seconds, ` + alias + `.completed_at, ` +
		alias + `.scope, ` + alias + `.decision_summary, ` + alias + `.legal_review_notes, ` +
		alias + `.exception_reason, ` + alias + `.legacy_reporter_request_id, ` +
		alias + `.created_at, ` + alias + `.updated_at`
}

func scanPrivacyCase(row rowScanner, extra ...any) (PrivacyCase, error) {
	var privacyCase PrivacyCase
	var scope []byte
	destinations := []any{
		&privacyCase.ID, &privacyCase.TenantID, &privacyCase.PublicID,
		&privacyCase.SubjectType, &privacyCase.RightType, &privacyCase.ControllerRole,
		&privacyCase.Status, &privacyCase.SubjectEmail, &privacyCase.SubjectMemberID,
		&privacyCase.RequestedByMemberID, &privacyCase.OwnerMemberID,
		&privacyCase.VerificationExpiresAt, &privacyCase.VerificationMethod,
		&privacyCase.VerifiedAt, &privacyCase.ReceivedAt, &privacyCase.DeadlineAt,
		&privacyCase.PauseStartedAt, &privacyCase.PausedSeconds, &privacyCase.CompletedAt,
		&scope, &privacyCase.DecisionSummary, &privacyCase.LegalReviewNotes,
		&privacyCase.ExceptionReason, &privacyCase.LegacyReporterRequestID,
		&privacyCase.CreatedAt, &privacyCase.UpdatedAt,
	}
	destinations = append(destinations, extra...)
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return PrivacyCase{}, ErrNotFound
	} else if err != nil {
		return PrivacyCase{}, fmt.Errorf("scan privacy case: %w", err)
	}
	if err := json.Unmarshal(scope, &privacyCase.Scope); err != nil {
		return PrivacyCase{}, fmt.Errorf("decode privacy case scope: %w", err)
	}
	return privacyCase, nil
}

func optionalTrimmed(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func reporterDigestHex(digest []byte) string {
	if len(digest) == 0 {
		return ""
	}
	return hex.EncodeToString(digest)
}
