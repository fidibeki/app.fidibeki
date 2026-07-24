// Package breachregister implements the restricted personal-data breach
// register from issue #64: a platform-admin-only record of a breach incident
// plus its append-only chronology. Ordinary tenant members and tenant API
// keys can never reach this data — the underlying tables carry no grant for
// the fidibeki_app role tenant credentials use (see migration 000092), so
// denial holds even if application wiring is misconfigured.
//
// Facts that change after an incident is created (containment progress,
// assessment, notifications) are recorded as new chronology entries rather
// than by rewriting the incident row: both breach_incidents and
// breach_chronology_entries reject UPDATE/DELETE at the database level.
package breachregister

import (
	"errors"
	"strings"
	"time"
)

type ControllerRole string

const (
	RoleController ControllerRole = "controller"
	RoleProcessor  ControllerRole = "processor"
)

type DetectionSource string

const (
	DetectionInternalMonitoring DetectionSource = "internal_monitoring"
	DetectionCustomerReport     DetectionSource = "customer_report"
	DetectionSubprocessorNotice DetectionSource = "subprocessor_notice"
	DetectionSecurityResearcher DetectionSource = "security_researcher"
	DetectionEmployeeReport     DetectionSource = "employee_report"
	DetectionAutomatedAlert     DetectionSource = "automated_alert"
	DetectionAuditFinding       DetectionSource = "audit_finding"
	DetectionOther              DetectionSource = "other"
)

type ChronologyEntryKind string

const (
	EntryDetected           ChronologyEntryKind = "detected"
	EntryTriaged            ChronologyEntryKind = "triaged"
	EntryContained          ChronologyEntryKind = "contained"
	EntryEradicated         ChronologyEntryKind = "eradicated"
	EntryRecovered          ChronologyEntryKind = "recovered"
	EntryAssessed           ChronologyEntryKind = "assessed"
	EntryEvidenceAdded      ChronologyEntryKind = "evidence_added"
	EntryContainmentUpdated ChronologyEntryKind = "containment_updated"
	EntryEscalated          ChronologyEntryKind = "escalated"
	EntryNote               ChronologyEntryKind = "note"
	EntryClosed             ChronologyEntryKind = "closed"
)

// AuthorityNotificationDecision, RiskDecision, and CommunicationDecision are
// operator-entered, reasoned decisions recorded on a breach assessment. None
// of these values is ever derived automatically: the store always requires
// an explicit decision plus non-empty reasoning (see issue #64's incident
// contract -- Fidibeki must not claim that every incident requires authority
// or data-subject notification).
type AuthorityNotificationDecision string

const (
	AuthorityNotificationAssessmentPending AuthorityNotificationDecision = "assessment_pending"
	AuthorityNotificationRequired          AuthorityNotificationDecision = "notification_required"
	AuthorityNotificationNotRequired       AuthorityNotificationDecision = "notification_not_required"
)

type RiskDecision string

const (
	RiskAssessmentPending RiskDecision = "assessment_pending"
	RiskHigh              RiskDecision = "high_risk"
	RiskNotHigh           RiskDecision = "not_high_risk"
)

type CommunicationDecision string

const (
	CommunicationAssessmentPending CommunicationDecision = "assessment_pending"
	CommunicationRequired          CommunicationDecision = "communication_required"
	CommunicationNotRequired       CommunicationDecision = "communication_not_required"
)

func ParseAuthorityNotificationDecision(value string) (AuthorityNotificationDecision, error) {
	switch AuthorityNotificationDecision(value) {
	case AuthorityNotificationAssessmentPending, AuthorityNotificationRequired, AuthorityNotificationNotRequired:
		return AuthorityNotificationDecision(value), nil
	}
	return "", ErrInvalidRequest
}

func ParseRiskDecision(value string) (RiskDecision, error) {
	switch RiskDecision(value) {
	case RiskAssessmentPending, RiskHigh, RiskNotHigh:
		return RiskDecision(value), nil
	}
	return "", ErrInvalidRequest
}

func ParseCommunicationDecision(value string) (CommunicationDecision, error) {
	switch CommunicationDecision(value) {
	case CommunicationAssessmentPending, CommunicationRequired, CommunicationNotRequired:
		return CommunicationDecision(value), nil
	}
	return "", ErrInvalidRequest
}

var (
	ErrInvalidRequest      = errors.New("breach register request is invalid")
	ErrNotFound            = errors.New("breach incident not found")
	ErrIdempotencyConflict = errors.New("idempotency key was reused with different input")
	ErrTenantNotFound      = errors.New("affected tenant reference is invalid")
)

var affectedEnvironments = map[string]struct{}{
	"production": {}, "staging": {}, "third_party_subprocessor": {}, "corporate_it": {}, "other": {},
}

var affectedSystemCategories = map[string]struct{}{
	"api": {}, "database": {}, "object_storage": {}, "email": {}, "integration": {},
	"replay": {}, "ai": {}, "analytics": {}, "widget": {}, "authentication": {}, "other": {},
}

var affectedDataCategories = map[string]struct{}{
	"contact_info": {}, "authentication_credentials": {}, "usage_analytics": {},
	"content_text": {}, "attachments_media": {}, "session_replay": {},
	"billing_payment": {}, "technical_identifiers": {}, "other": {},
}

var affectedSubjectCategories = map[string]struct{}{
	"tenant_members": {}, "reporters_end_users": {}, "prospects_leads": {},
	"employees_contractors": {}, "other": {},
}

func ParseControllerRole(value string) (ControllerRole, error) {
	switch ControllerRole(value) {
	case RoleController, RoleProcessor:
		return ControllerRole(value), nil
	}
	return "", ErrInvalidRequest
}

func ParseDetectionSource(value string) (DetectionSource, error) {
	switch DetectionSource(value) {
	case DetectionInternalMonitoring, DetectionCustomerReport, DetectionSubprocessorNotice,
		DetectionSecurityResearcher, DetectionEmployeeReport, DetectionAutomatedAlert,
		DetectionAuditFinding, DetectionOther:
		return DetectionSource(value), nil
	}
	return "", ErrInvalidRequest
}

func ParseChronologyEntryKind(value string) (ChronologyEntryKind, error) {
	switch ChronologyEntryKind(value) {
	case EntryDetected, EntryTriaged, EntryContained, EntryEradicated, EntryRecovered,
		EntryAssessed, EntryEvidenceAdded, EntryContainmentUpdated, EntryEscalated,
		EntryNote, EntryClosed:
		return ChronologyEntryKind(value), nil
	}
	return "", ErrInvalidRequest
}

// Incident is the immutable record created for a breach. AffectedTenantIDs
// carries only tenant references (UUIDs), never tenant name/slug/billing
// content, so list/get responses cannot leak tenant business data.
type Incident struct {
	ID                        string          `json:"id"`
	PublicID                  string          `json:"public_id"`
	AwarenessAt               time.Time       `json:"awareness_at"`
	DetectionSource           DetectionSource `json:"detection_source"`
	ControllerRole            ControllerRole  `json:"controller_role"`
	AffectedEnvironments      []string        `json:"affected_environments"`
	AffectedSystemCategories  []string        `json:"affected_system_categories"`
	AffectedDataCategories    []string        `json:"affected_data_categories"`
	AffectedSubjectCategories []string        `json:"affected_subject_categories"`
	ConfidentialityImpact     bool            `json:"confidentiality_impact"`
	IntegrityImpact           bool            `json:"integrity_impact"`
	AvailabilityImpact        bool            `json:"availability_impact"`
	ContainmentSummary        string          `json:"containment_summary"`
	CreatedByPlatformAdminID  string          `json:"created_by_platform_admin_id"`
	CreatedAt                 time.Time       `json:"created_at"`
	AffectedTenantIDs         []string        `json:"affected_tenant_ids"`
	// AuthorityNotificationDeadlineAt is the GDPR Article 33 72-hour clock,
	// started at AwarenessAt (not at completion of the investigation) and
	// computed server-side by the database (see migration 000096). It is
	// always present and never editable.
	AuthorityNotificationDeadlineAt time.Time `json:"authority_notification_deadline_at"`
}

// Assessment is one append-only, operator-entered snapshot of the
// responsible controller's reasoned breach decisions and the facts they were
// based on. Later facts are recorded as a new Assessment rather than by
// rewriting a prior one, so the full reasoning history survives even if a
// decision changes as the investigation progresses.
type Assessment struct {
	ID                                string                        `json:"id"`
	BreachIncidentID                  string                        `json:"breach_incident_id"`
	AuthorityNotificationDecision     AuthorityNotificationDecision `json:"authority_notification_decision"`
	AuthorityNotificationReasoning    string                        `json:"authority_notification_reasoning"`
	HighRiskToIndividuals             RiskDecision                  `json:"high_risk_to_individuals"`
	HighRiskReasoning                 string                        `json:"high_risk_reasoning"`
	DataSubjectCommunicationDecision  CommunicationDecision         `json:"data_subject_communication_decision"`
	DataSubjectCommunicationReasoning string                        `json:"data_subject_communication_reasoning"`
	EstimatedAffectedSubjectsMin      *int64                        `json:"estimated_affected_subjects_min,omitempty"`
	EstimatedAffectedSubjectsMax      *int64                        `json:"estimated_affected_subjects_max,omitempty"`
	EstimatedAffectedRecordsMin       *int64                        `json:"estimated_affected_records_min,omitempty"`
	EstimatedAffectedRecordsMax       *int64                        `json:"estimated_affected_records_max,omitempty"`
	LikelyConsequences                *string                       `json:"likely_consequences,omitempty"`
	RecoveryMeasures                  *string                       `json:"recovery_measures,omitempty"`
	EvidenceReferences                []string                      `json:"evidence_references"`
	AssessedByPlatformAdminID         string                        `json:"assessed_by_platform_admin_id"`
	AssessedAt                        time.Time                     `json:"assessed_at"`
	CreatedAt                         time.Time                     `json:"created_at"`
}

// ChronologyEntry is one append-only fact recorded against an incident.
// Summary and EvidenceReferences are bounded, opaque pointers/notes: callers
// must never place raw payloads, tokens, reporter content, or secrets here.
type ChronologyEntry struct {
	ID                   string              `json:"id"`
	BreachIncidentID     string              `json:"breach_incident_id"`
	EntryKind            ChronologyEntryKind `json:"entry_kind"`
	Summary              string              `json:"summary"`
	EvidenceReferences   []string            `json:"evidence_references"`
	ActorPlatformAdminID string              `json:"actor_platform_admin_id"`
	OccurredAt           time.Time           `json:"occurred_at"`
	CreatedAt            time.Time           `json:"created_at"`
}

type CreateIncidentInput struct {
	AwarenessAt               time.Time
	DetectionSource           DetectionSource
	ControllerRole            ControllerRole
	AffectedEnvironments      []string
	AffectedSystemCategories  []string
	AffectedDataCategories    []string
	AffectedSubjectCategories []string
	ConfidentialityImpact     bool
	IntegrityImpact           bool
	AvailabilityImpact        bool
	ContainmentSummary        string
	AffectedTenantIDs         []string
}

type AppendChronologyEntryInput struct {
	EntryKind          ChronologyEntryKind
	Summary            string
	EvidenceReferences []string
}

// CreateAssessmentInput is one reasoned assessment snapshot. Reasoning is
// required for every decision, every time -- an assessment always restates
// the current reasoning rather than leaving a prior one to speak for a
// changed decision. Estimates and narrative facts are optional: an early
// assessment may only have a decision and reasoning, with facts recorded as
// they become known.
type CreateAssessmentInput struct {
	AuthorityNotificationDecision     AuthorityNotificationDecision
	AuthorityNotificationReasoning    string
	HighRiskToIndividuals             RiskDecision
	HighRiskReasoning                 string
	DataSubjectCommunicationDecision  CommunicationDecision
	DataSubjectCommunicationReasoning string
	EstimatedAffectedSubjectsMin      *int64
	EstimatedAffectedSubjectsMax      *int64
	EstimatedAffectedRecordsMin       *int64
	EstimatedAffectedRecordsMax       *int64
	LikelyConsequences                *string
	RecoveryMeasures                  *string
	EvidenceReferences                []string
}

const maxAffectedTenants = 200

func normalizeCreateIncidentInput(input CreateIncidentInput) (CreateIncidentInput, error) {
	if _, err := ParseDetectionSource(string(input.DetectionSource)); err != nil {
		return CreateIncidentInput{}, err
	}
	if _, err := ParseControllerRole(string(input.ControllerRole)); err != nil {
		return CreateIncidentInput{}, err
	}
	if input.AwarenessAt.IsZero() {
		return CreateIncidentInput{}, ErrInvalidRequest
	}
	if !(input.ConfidentialityImpact || input.IntegrityImpact || input.AvailabilityImpact) {
		return CreateIncidentInput{}, ErrInvalidRequest
	}
	environments, err := validateBoundedSet(input.AffectedEnvironments, affectedEnvironments, 10)
	if err != nil {
		return CreateIncidentInput{}, err
	}
	systems, err := validateBoundedSet(input.AffectedSystemCategories, affectedSystemCategories, 20)
	if err != nil {
		return CreateIncidentInput{}, err
	}
	dataCategories, err := validateBoundedSet(input.AffectedDataCategories, affectedDataCategories, 20)
	if err != nil {
		return CreateIncidentInput{}, err
	}
	subjectCategories, err := validateBoundedSet(
		input.AffectedSubjectCategories, affectedSubjectCategories, 10,
	)
	if err != nil {
		return CreateIncidentInput{}, err
	}
	summary := strings.TrimSpace(input.ContainmentSummary)
	if summary == "" || len(summary) > 4000 {
		return CreateIncidentInput{}, ErrInvalidRequest
	}
	tenantIDs, err := normalizeTenantIDs(input.AffectedTenantIDs)
	if err != nil {
		return CreateIncidentInput{}, err
	}
	return CreateIncidentInput{
		AwarenessAt: input.AwarenessAt.UTC(), DetectionSource: input.DetectionSource,
		ControllerRole: input.ControllerRole, AffectedEnvironments: environments,
		AffectedSystemCategories: systems, AffectedDataCategories: dataCategories,
		AffectedSubjectCategories: subjectCategories,
		ConfidentialityImpact:     input.ConfidentialityImpact,
		IntegrityImpact:           input.IntegrityImpact,
		AvailabilityImpact:        input.AvailabilityImpact,
		ContainmentSummary:        summary, AffectedTenantIDs: tenantIDs,
	}, nil
}

func normalizeTenantIDs(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > maxAffectedTenants {
		return nil, ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if !isUUID(trimmed) {
			return nil, ErrInvalidRequest
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized, nil
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		isHex := (character >= '0' && character <= '9') ||
			(character >= 'a' && character <= 'f') ||
			(character >= 'A' && character <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func validateBoundedSet(values []string, allowed map[string]struct{}, max int) ([]string, error) {
	if len(values) == 0 || len(values) > max {
		return nil, ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if _, ok := allowed[trimmed]; !ok {
			return nil, ErrInvalidRequest
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized, nil
}

const maxEvidenceReferences = 20

func normalizeAppendChronologyEntryInput(
	input AppendChronologyEntryInput,
) (AppendChronologyEntryInput, error) {
	if _, err := ParseChronologyEntryKind(string(input.EntryKind)); err != nil {
		return AppendChronologyEntryInput{}, err
	}
	summary := strings.TrimSpace(input.Summary)
	if summary == "" || len(summary) > 4000 {
		return AppendChronologyEntryInput{}, ErrInvalidRequest
	}
	references, err := normalizeEvidenceReferences(input.EvidenceReferences)
	if err != nil {
		return AppendChronologyEntryInput{}, err
	}
	return AppendChronologyEntryInput{
		EntryKind: input.EntryKind, Summary: summary, EvidenceReferences: references,
	}, nil
}

func normalizeEvidenceReferences(values []string) ([]string, error) {
	if len(values) > maxEvidenceReferences {
		return nil, ErrInvalidRequest
	}
	references := make([]string, 0, len(values))
	for _, reference := range values {
		trimmed := strings.TrimSpace(reference)
		if trimmed == "" || len(trimmed) > 300 || containsControlCharacter(trimmed) {
			return nil, ErrInvalidRequest
		}
		references = append(references, trimmed)
	}
	return references, nil
}

// maxEstimatedCount bounds affected-subject/record estimates. It matches the
// database CHECK in migration 000096 so validation failures surface as a
// clean ErrInvalidRequest rather than a raw constraint violation.
const maxEstimatedCount = 1_000_000_000_000

func normalizeCreateAssessmentInput(input CreateAssessmentInput) (CreateAssessmentInput, error) {
	if _, err := ParseAuthorityNotificationDecision(string(input.AuthorityNotificationDecision)); err != nil {
		return CreateAssessmentInput{}, err
	}
	if _, err := ParseRiskDecision(string(input.HighRiskToIndividuals)); err != nil {
		return CreateAssessmentInput{}, err
	}
	if _, err := ParseCommunicationDecision(string(input.DataSubjectCommunicationDecision)); err != nil {
		return CreateAssessmentInput{}, err
	}
	authorityReasoning, err := normalizeRequiredReasoning(input.AuthorityNotificationReasoning)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	highRiskReasoning, err := normalizeRequiredReasoning(input.HighRiskReasoning)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	communicationReasoning, err := normalizeRequiredReasoning(input.DataSubjectCommunicationReasoning)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	subjectsMin, subjectsMax, err := normalizeEstimateRange(
		input.EstimatedAffectedSubjectsMin, input.EstimatedAffectedSubjectsMax,
	)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	recordsMin, recordsMax, err := normalizeEstimateRange(
		input.EstimatedAffectedRecordsMin, input.EstimatedAffectedRecordsMax,
	)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	likelyConsequences, err := normalizeOptionalNote(input.LikelyConsequences)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	recoveryMeasures, err := normalizeOptionalNote(input.RecoveryMeasures)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	references, err := normalizeEvidenceReferences(input.EvidenceReferences)
	if err != nil {
		return CreateAssessmentInput{}, err
	}
	return CreateAssessmentInput{
		AuthorityNotificationDecision:     input.AuthorityNotificationDecision,
		AuthorityNotificationReasoning:    authorityReasoning,
		HighRiskToIndividuals:             input.HighRiskToIndividuals,
		HighRiskReasoning:                 highRiskReasoning,
		DataSubjectCommunicationDecision:  input.DataSubjectCommunicationDecision,
		DataSubjectCommunicationReasoning: communicationReasoning,
		EstimatedAffectedSubjectsMin:      subjectsMin,
		EstimatedAffectedSubjectsMax:      subjectsMax,
		EstimatedAffectedRecordsMin:       recordsMin,
		EstimatedAffectedRecordsMax:       recordsMax,
		LikelyConsequences:                likelyConsequences,
		RecoveryMeasures:                  recoveryMeasures,
		EvidenceReferences:                references,
	}, nil
}

func normalizeRequiredReasoning(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 4000 {
		return "", ErrInvalidRequest
	}
	return trimmed, nil
}

func normalizeOptionalNote(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || len(trimmed) > 4000 {
		return nil, ErrInvalidRequest
	}
	return &trimmed, nil
}

// normalizeEstimateRange requires both bounds or neither, so a partial
// estimate can never be recorded as if it were exact: a lower bound alone
// without an upper one is not a bounded estimate.
func normalizeEstimateRange(min, max *int64) (*int64, *int64, error) {
	if (min == nil) != (max == nil) {
		return nil, nil, ErrInvalidRequest
	}
	if min == nil {
		return nil, nil, nil
	}
	if *min < 0 || *max < *min || *max > maxEstimatedCount {
		return nil, nil, ErrInvalidRequest
	}
	return min, max, nil
}

func containsControlCharacter(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}
