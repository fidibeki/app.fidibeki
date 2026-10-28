package privacy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenantmember"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var memberRectificationFields = map[string]func(*string) (*string, string){
	"name":     tenantmember.NormalizeMemberName,
	"locale":   tenantmember.NormalizeMemberLocale,
	"timezone": tenantmember.NormalizeMemberTimezone,
}

func (Store) ApplyCaseRectification(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	input RecordCaseRectificationInput,
	now time.Time,
) (CaseRectification, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return CaseRectification{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	input.Reason = strings.TrimSpace(input.Reason)
	changes, fields, err := normalizeMemberRectificationChanges(input.Changes)
	if err != nil || idempotencyKey == "" || len(idempotencyKey) > 200 ||
		input.Reason == "" || len(input.Reason) > 2000 {
		return CaseRectification{}, false, ErrInvalidRequest
	}
	input.Changes = changes
	fingerprint, err := caseRectificationFingerprint(input)
	if err != nil {
		return CaseRectification{}, false, err
	}

	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return CaseRectification{}, false, err
	}
	if privacyCase.RightType != RightRectification ||
		privacyCase.SubjectType != SubjectMember ||
		privacyCase.SubjectMemberID == nil ||
		privacyCase.VerifiedAt == nil ||
		privacyCase.Status == CasePendingVerification ||
		caseStatusTerminal(privacyCase.Status) {
		return CaseRectification{}, false, ErrCaseExecutionUnsupported
	}

	existing, storedFingerprint, err := scanCaseRectification(query.QueryRow(ctx, `
		SELECT `+caseRectificationSelectColumns("rectification")+`,
		       rectification.request_fingerprint
		FROM privacy_case_rectifications AS rectification
		WHERE rectification.tenant_id = $1
		  AND rectification.privacy_case_id = $2
		  AND rectification.idempotency_key = $3
	`, tenantID, caseID, idempotencyKey), true)
	if err == nil {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return CaseRectification{}, false, ErrIdempotencyConflict
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return CaseRectification{}, false, err
	}

	var currentName, currentLocale, currentTimezone *string
	if err := query.QueryRow(ctx, `
		SELECT name, locale, timezone
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, *privacyCase.SubjectMemberID).Scan(
		&currentName, &currentLocale, &currentTimezone,
	); errors.Is(err, pgx.ErrNoRows) {
		return CaseRectification{}, false, ErrNotFound
	} else if err != nil {
		return CaseRectification{}, false, fmt.Errorf("lock rectification subject: %w", err)
	}

	current := map[string]*string{
		"name": currentName, "locale": currentLocale, "timezone": currentTimezone,
	}
	previous := make(map[string]*string, len(changes))
	effective := make(map[string]*string, len(changes))
	for _, field := range fields {
		if optionalStringValuesEqual(current[field], changes[field]) {
			continue
		}
		previous[field] = current[field]
		effective[field] = changes[field]
		current[field] = changes[field]
	}
	if len(effective) == 0 {
		return CaseRectification{}, false, ErrInvalidRequest
	}

	if _, err := query.Exec(ctx, `
		UPDATE tenant_members
		SET name = $3, locale = $4, timezone = $5
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, *privacyCase.SubjectMemberID,
		current["name"], current["locale"], current["timezone"]); err != nil {
		return CaseRectification{}, false, fmt.Errorf("apply member rectification: %w", err)
	}
	encodedChanges, err := json.Marshal(effective)
	if err != nil {
		return CaseRectification{}, false, fmt.Errorf("encode rectification changes: %w", err)
	}
	encodedPrevious, err := json.Marshal(previous)
	if err != nil {
		return CaseRectification{}, false, fmt.Errorf("encode rectification previous values: %w", err)
	}

	rectification, _, err := scanCaseRectification(query.QueryRow(ctx, `
		INSERT INTO privacy_case_rectifications (
			id, tenant_id, privacy_case_id, subject_member_id, changes,
			previous_values, reason, applied_by_member_id, applied_at,
			idempotency_key, request_fingerprint, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $9
		)
		RETURNING `+caseRectificationSelectColumns("privacy_case_rectifications")+`
	`, uuid.NewString(), tenantID, caseID, *privacyCase.SubjectMemberID,
		string(encodedChanges), string(encodedPrevious), input.Reason, actorMemberID, now.UTC(),
		idempotencyKey, fingerprint[:]), false)
	if err != nil {
		return CaseRectification{}, false, fmt.Errorf("record member rectification: %w", err)
	}

	eventData := map[string]any{
		"rectification_id": rectification.ID,
		"fields":           sortedRectificationKeys(effective),
		"reason":           rectification.Reason,
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "rectification:"+idempotencyKey,
		"privacy_case.rectification_applied", &actorMemberID, nil, nil,
		eventData, now,
	); err != nil {
		return CaseRectification{}, false, err
	}
	if err := recordMemberRectificationAudit(
		ctx, query, tenantID, actorMemberID, *privacyCase.SubjectMemberID,
		caseID, eventData,
	); err != nil {
		return CaseRectification{}, false, err
	}
	return rectification, true, nil
}

func normalizeMemberRectificationChanges(
	input map[string]*string,
) (map[string]*string, []string, error) {
	if len(input) == 0 || len(input) > len(memberRectificationFields) {
		return nil, nil, ErrInvalidRequest
	}
	fields := make([]string, 0, len(input))
	changes := make(map[string]*string, len(input))
	for field, value := range input {
		normalize, supported := memberRectificationFields[field]
		if !supported {
			return nil, nil, ErrInvalidRequest
		}
		normalized, message := normalize(value)
		if message != "" {
			return nil, nil, ErrInvalidRequest
		}
		fields = append(fields, field)
		changes[field] = normalized
	}
	sort.Strings(fields)
	return changes, fields, nil
}

func caseRectificationFingerprint(input RecordCaseRectificationInput) ([32]byte, error) {
	encoded, err := json.Marshal(struct {
		Changes map[string]*string `json:"changes"`
		Reason  string             `json:"reason"`
	}{Changes: input.Changes, Reason: input.Reason})
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode rectification fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func optionalStringValuesEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sortedRectificationKeys(values map[string]*string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func recordMemberRectificationAudit(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	actorMemberID string,
	subjectMemberID string,
	caseID string,
	data map[string]any,
) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode member rectification audit: %w", err)
	}
	if _, err := query.Exec(ctx, `
		INSERT INTO tenant_access_events (
			tenant_id, kind, actor_member_id, subject_member_id,
			resource_type, resource_id, action, permission_path, outcome, data
		) VALUES (
			$1, 'privacy.member_rectification_applied', $2, $3,
			'member_profile', $3, 'rectify', 'tenant:update', 'success',
			$4::jsonb || jsonb_build_object('privacy_case_id', $5::text)
		)
	`, tenantID, actorMemberID, subjectMemberID, string(encoded), caseID); err != nil {
		return fmt.Errorf("record member rectification audit: %w", err)
	}
	return nil
}

func caseRectificationSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.privacy_case_id, ` +
		alias + `.subject_member_id, ` + alias + `.changes, ` +
		alias + `.previous_values, ` + alias + `.reason, ` +
		alias + `.applied_by_member_id, ` + alias + `.applied_at, ` +
		alias + `.created_at`
}

func scanCaseRectification(
	row rowScanner,
	withFingerprint bool,
) (CaseRectification, []byte, error) {
	var rectification CaseRectification
	var changes, previousValues []byte
	var fingerprint []byte
	destinations := []any{
		&rectification.ID, &rectification.PrivacyCaseID,
		&rectification.SubjectMemberID, &changes, &previousValues,
		&rectification.Reason, &rectification.AppliedByMemberID,
		&rectification.AppliedAt, &rectification.CreatedAt,
	}
	if withFingerprint {
		destinations = append(destinations, &fingerprint)
	}
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return CaseRectification{}, nil, ErrNotFound
	} else if err != nil {
		return CaseRectification{}, nil, fmt.Errorf("scan case rectification: %w", err)
	}
	if err := json.Unmarshal(changes, &rectification.Changes); err != nil {
		return CaseRectification{}, nil, fmt.Errorf("decode rectification changes: %w", err)
	}
	if err := json.Unmarshal(previousValues, &rectification.PreviousValues); err != nil {
		return CaseRectification{}, nil, fmt.Errorf("decode rectification previous values: %w", err)
	}
	return rectification, fingerprint, nil
}
