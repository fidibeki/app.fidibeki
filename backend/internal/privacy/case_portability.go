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

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const portabilityExportFormat = "jsonl_zip"

func (Store) RecordPortabilityAssessment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
	actorMemberID string,
	idempotencyKey string,
	input RecordPortabilityAssessmentInput,
	now time.Time,
) (PortabilityAssessment, bool, error) {
	if err := requireActiveHuman(ctx, query, tenantID, actorMemberID); err != nil {
		return PortabilityAssessment{}, false, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	input.Reason = strings.TrimSpace(input.Reason)
	input.ExportFormat = strings.TrimSpace(input.ExportFormat)
	categories, err := normalizePortabilityCategories(input.DataCategories)
	if err != nil {
		return PortabilityAssessment{}, false, err
	}
	input.DataCategories = categories
	if idempotencyKey == "" || len(idempotencyKey) > 200 ||
		input.Reason == "" || len(input.Reason) > 2000 {
		return PortabilityAssessment{}, false, ErrInvalidRequest
	}
	switch input.Decision {
	case PortabilityApproved, PortabilityPartiallyApproved:
		if len(input.DataCategories) == 0 || input.ExportFormat != portabilityExportFormat {
			return PortabilityAssessment{}, false, ErrInvalidRequest
		}
	case PortabilityNotApplicable, PortabilityRefused:
		if len(input.DataCategories) != 0 || input.ExportFormat != "" {
			return PortabilityAssessment{}, false, ErrInvalidRequest
		}
	default:
		return PortabilityAssessment{}, false, ErrInvalidRequest
	}
	fingerprint, err := portabilityAssessmentFingerprint(input)
	if err != nil {
		return PortabilityAssessment{}, false, err
	}

	privacyCase, err := scanPrivacyCase(query.QueryRow(ctx, `
		SELECT `+privacyCaseSelectColumns("privacy_case")+`
		FROM privacy_cases AS privacy_case
		WHERE privacy_case.tenant_id = $1 AND privacy_case.id = $2
		FOR UPDATE
	`, tenantID, caseID))
	if err != nil {
		return PortabilityAssessment{}, false, err
	}
	if privacyCase.RightType != RightPortabilityAssessment ||
		privacyCase.SubjectType != SubjectReporter ||
		privacyCase.VerifiedAt == nil ||
		privacyCase.Status == CasePendingVerification ||
		caseStatusTerminal(privacyCase.Status) ||
		privacyCase.LegacyReporterRequestID != nil {
		return PortabilityAssessment{}, false, ErrCaseExecutionUnsupported
	}

	assessment, storedFingerprint, err := scanPortabilityAssessment(query.QueryRow(ctx, `
		SELECT `+portabilityAssessmentSelectColumns("assessment")+`,
		       assessment.request_fingerprint
		FROM privacy_case_portability_assessments AS assessment
		WHERE assessment.tenant_id = $1 AND assessment.privacy_case_id = $2
		  AND assessment.idempotency_key = $3
	`, tenantID, caseID, idempotencyKey), true)
	if err == nil {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return PortabilityAssessment{}, false, ErrIdempotencyConflict
		}
		return assessment, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return PortabilityAssessment{}, false, err
	}

	assessmentID := uuid.NewString()
	assessment, _, err = scanPortabilityAssessment(query.QueryRow(ctx, `
		INSERT INTO privacy_case_portability_assessments (
			id, tenant_id, privacy_case_id, decision, data_categories, export_format,
			reason, assessed_by_member_id, assessed_at, idempotency_key,
			request_fingerprint
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10, $11)
		RETURNING `+portabilityAssessmentSelectColumns("privacy_case_portability_assessments")+`
	`, assessmentID, tenantID, caseID, input.Decision, portabilityCategoryStrings(categories),
		input.ExportFormat, input.Reason, actorMemberID, now.UTC(), idempotencyKey,
		fingerprint[:]), false)
	if err != nil {
		return PortabilityAssessment{}, false, fmt.Errorf("record portability assessment: %w", err)
	}
	eventData := map[string]any{
		"assessment_id":   assessment.ID,
		"decision":        assessment.Decision,
		"data_categories": assessment.DataCategories,
		"export_format":   assessment.ExportFormat,
		"reason":          assessment.Reason,
	}
	if err := insertCaseEvent(
		ctx, query, tenantID, caseID, "portability:"+idempotencyKey,
		"privacy_case.portability_assessed", &actorMemberID, nil, nil, eventData, now,
	); err != nil {
		return PortabilityAssessment{}, false, err
	}
	if err := recordPrivacyRequestAudit(
		ctx, query, tenantID, "privacy.case_portability_assessed",
		actorMemberID, caseID, eventData,
	); err != nil {
		return PortabilityAssessment{}, false, err
	}
	return assessment, true, nil
}

func normalizePortabilityCategories(
	values []PortabilityDataCategory,
) ([]PortabilityDataCategory, error) {
	seen := make(map[PortabilityDataCategory]struct{}, len(values))
	categories := make([]PortabilityDataCategory, 0, len(values))
	for _, raw := range values {
		category, err := ParsePortabilityDataCategory(string(raw))
		if err != nil {
			return nil, err
		}
		if _, exists := seen[category]; exists {
			continue
		}
		seen[category] = struct{}{}
		categories = append(categories, category)
	}
	sort.Slice(categories, func(left, right int) bool {
		return categories[left] < categories[right]
	})
	return categories, nil
}

func portabilityAssessmentFingerprint(input RecordPortabilityAssessmentInput) ([32]byte, error) {
	encoded, err := json.Marshal(struct {
		Decision       PortabilityDecision       `json:"decision"`
		DataCategories []PortabilityDataCategory `json:"data_categories"`
		ExportFormat   string                    `json:"export_format"`
		Reason         string                    `json:"reason"`
	}{
		Decision: input.Decision, DataCategories: input.DataCategories,
		ExportFormat: input.ExportFormat, Reason: input.Reason,
	})
	if err != nil {
		return [32]byte{}, fmt.Errorf("encode portability assessment fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func portabilityCategoryStrings(categories []PortabilityDataCategory) []string {
	values := make([]string, len(categories))
	for index, category := range categories {
		values[index] = string(category)
	}
	return values
}

func loadLatestPortabilityAssessment(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	caseID string,
) (PortabilityAssessment, error) {
	assessment, _, err := scanPortabilityAssessment(query.QueryRow(ctx, `
		SELECT `+portabilityAssessmentSelectColumns("assessment")+`
		FROM privacy_case_portability_assessments AS assessment
		WHERE assessment.tenant_id = $1 AND assessment.privacy_case_id = $2
		ORDER BY assessment.assessed_at DESC, assessment.id DESC
		LIMIT 1
	`, tenantID, caseID), false)
	return assessment, err
}

func portabilityAssessmentSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.privacy_case_id, ` + alias + `.decision, ` +
		alias + `.data_categories, ` + alias + `.export_format, ` + alias + `.reason, ` +
		alias + `.assessed_by_member_id, ` + alias + `.assessed_at, ` + alias + `.created_at`
}

func scanPortabilityAssessment(
	row rowScanner,
	withFingerprint bool,
) (PortabilityAssessment, []byte, error) {
	var assessment PortabilityAssessment
	var storedCategories []string
	var fingerprint []byte
	destinations := []any{
		&assessment.ID, &assessment.PrivacyCaseID, &assessment.Decision,
		&storedCategories, &assessment.ExportFormat, &assessment.Reason,
		&assessment.AssessedByMemberID, &assessment.AssessedAt, &assessment.CreatedAt,
	}
	if withFingerprint {
		destinations = append(destinations, &fingerprint)
	}
	if err := row.Scan(destinations...); errors.Is(err, pgx.ErrNoRows) {
		return PortabilityAssessment{}, nil, ErrNotFound
	} else if err != nil {
		return PortabilityAssessment{}, nil, fmt.Errorf("scan portability assessment: %w", err)
	}
	assessment.DataCategories = make([]PortabilityDataCategory, len(storedCategories))
	for index, category := range storedCategories {
		assessment.DataCategories[index] = PortabilityDataCategory(category)
	}
	return assessment, fingerprint, nil
}
