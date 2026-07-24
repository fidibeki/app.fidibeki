package breachregister

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AdminDatabase is satisfied by the platform-admin connection pool
// (pools.Admin in cmd/fidibeki-api). It never uses the tenant-scoped
// fidibeki_app role, matching the migration's grant boundary.
type AdminDatabase interface {
	db.Querier
	Begin(context.Context) (pgx.Tx, error)
}

type Store struct {
	Database AdminDatabase
	Now      func() time.Time
}

// authorityNotificationDeadline is GDPR Article 33's 72-hour clock for
// notifying the supervisory authority, started from awareness (see issue
// #64's incident contract).
const authorityNotificationDeadline = 72 * time.Hour

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Store) CreateIncident(
	ctx context.Context,
	actorPlatformAdminID string,
	idempotencyKey string,
	input CreateIncidentInput,
) (Incident, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 || strings.TrimSpace(actorPlatformAdminID) == "" {
		return Incident{}, ErrInvalidRequest
	}
	normalized, err := normalizeCreateIncidentInput(input)
	if err != nil {
		return Incident{}, err
	}
	now := s.now().UTC()
	if normalized.AwarenessAt.After(now) {
		return Incident{}, ErrInvalidRequest
	}
	fingerprint, err := createIncidentFingerprint(normalized)
	if err != nil {
		return Incident{}, err
	}

	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return Incident{}, fmt.Errorf("begin breach incident creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := requireTenantsExist(ctx, tx, normalized.AffectedTenantIDs); err != nil {
		return Incident{}, err
	}

	id := uuid.NewString()
	publicID := "BR-" + strings.ToUpper(strings.ReplaceAll(id[:8], "-", ""))
	// GDPR Article 33's 72-hour authority-notification clock, started at
	// awareness (not at completion of the investigation). Computed here --
	// not by the database -- because timestamptz + interval arithmetic isn't
	// IMMUTABLE in Postgres; migration 000096's CHECK constraint guards this
	// value can never drift from awareness_at.
	authorityNotificationDeadlineAt := normalized.AwarenessAt.Add(authorityNotificationDeadline)

	var storedFingerprint []byte
	var created bool
	incident, err := scanIncident(tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO breach_incidents (
				id, public_id, awareness_at, detection_source, controller_role,
				affected_environments, affected_system_categories, affected_data_categories,
				affected_subject_categories, confidentiality_impact, integrity_impact,
				availability_impact, containment_summary, created_by_platform_admin_id,
				idempotency_key, request_fingerprint, created_at, authority_notification_deadline_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
			)
			ON CONFLICT (idempotency_key) DO NOTHING
			RETURNING *, true AS created
		), selected AS (
			SELECT * FROM inserted
			UNION ALL
			SELECT existing.*, false AS created
			FROM breach_incidents AS existing
			WHERE existing.idempotency_key = $15
			  AND NOT EXISTS (SELECT 1 FROM inserted)
		)
		SELECT `+breachIncidentSelectColumns("selected")+`,
		       selected.request_fingerprint, selected.created
		FROM selected
	`, id, publicID, normalized.AwarenessAt, normalized.DetectionSource, normalized.ControllerRole,
		normalized.AffectedEnvironments, normalized.AffectedSystemCategories,
		normalized.AffectedDataCategories, normalized.AffectedSubjectCategories,
		normalized.ConfidentialityImpact, normalized.IntegrityImpact, normalized.AvailabilityImpact,
		normalized.ContainmentSummary, actorPlatformAdminID, idempotencyKey, fingerprint[:], now,
		authorityNotificationDeadlineAt,
	), &storedFingerprint, &created)
	if err != nil {
		return Incident{}, fmt.Errorf("create breach incident: %w", err)
	}
	if !bytes.Equal(storedFingerprint, fingerprint[:]) {
		return Incident{}, ErrIdempotencyConflict
	}
	if !created {
		incident.AffectedTenantIDs, err = tenantIDsForIncident(ctx, tx, incident.ID)
		if err != nil {
			return Incident{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Incident{}, fmt.Errorf("commit breach incident idempotent read: %w", err)
		}
		return incident, nil
	}

	for _, tenantID := range normalized.AffectedTenantIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO breach_incident_tenants (breach_incident_id, tenant_id, created_at)
			VALUES ($1, $2, $3)
		`, incident.ID, tenantID, now); err != nil {
			return Incident{}, fmt.Errorf("link breach incident tenant: %w", err)
		}
	}
	incident.AffectedTenantIDs = normalized.AffectedTenantIDs
	if err := insertContainmentChecklist(ctx, tx, incident.ID, actorPlatformAdminID, now); err != nil {
		return Incident{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Incident{}, fmt.Errorf("commit breach incident creation: %w", err)
	}
	return incident, nil
}

// ListFilter bounds ListIncidents. Limit is clamped by the HTTP layer before
// reaching the store.
type ListFilter struct {
	Limit int
}

func (s Store) ListIncidents(ctx context.Context, filter ListFilter) ([]Incident, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Database.Query(ctx, `
		SELECT `+breachIncidentSelectColumns("incident")+`,
		       COALESCE((
		           SELECT array_agg(link.tenant_id::text ORDER BY link.tenant_id)
		           FROM breach_incident_tenants AS link
		           WHERE link.breach_incident_id = incident.id
		       ), '{}') AS affected_tenant_ids
		FROM breach_incidents AS incident
		ORDER BY incident.created_at DESC, incident.id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list breach incidents: %w", err)
	}
	defer rows.Close()
	incidents := make([]Incident, 0)
	for rows.Next() {
		incident, err := scanIncidentWithTenants(rows)
		if err != nil {
			return nil, fmt.Errorf("scan breach incident: %w", err)
		}
		incidents = append(incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate breach incidents: %w", err)
	}
	return incidents, nil
}

func (s Store) GetIncident(
	ctx context.Context, incidentID string,
) (
	Incident,
	[]ChronologyEntry,
	[]Assessment,
	[]ActionEvent,
	[]ActionEvent,
	[]ControllerNotification,
	error,
) {
	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return Incident{}, nil, nil, nil, nil, nil, fmt.Errorf("begin breach incident read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return Incident{}, nil, nil, nil, nil, nil, fmt.Errorf("configure breach incident read snapshot: %w", err)
	}

	incident, err := scanIncidentWithTenants(tx.QueryRow(ctx, `
		SELECT `+breachIncidentSelectColumns("incident")+`,
		       COALESCE((
		           SELECT array_agg(link.tenant_id::text ORDER BY link.tenant_id)
		           FROM breach_incident_tenants AS link
		           WHERE link.breach_incident_id = incident.id
		       ), '{}') AS affected_tenant_ids
		FROM breach_incidents AS incident
		WHERE incident.id = $1
	`, incidentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, nil, nil, nil, nil, nil, ErrNotFound
	}
	if err != nil {
		return Incident{}, nil, nil, nil, nil, nil, fmt.Errorf("get breach incident: %w", err)
	}
	entries, err := s.listChronologyEntries(ctx, tx, incidentID)
	if err != nil {
		return Incident{}, nil, nil, nil, nil, nil, err
	}
	assessments, err := s.listAssessments(ctx, tx, incidentID)
	if err != nil {
		return Incident{}, nil, nil, nil, nil, nil, err
	}
	actions, actionEvents, err := s.listActions(ctx, tx, incidentID)
	if err != nil {
		return Incident{}, nil, nil, nil, nil, nil, err
	}
	controllerNotifications, err := listControllerNotifications(ctx, tx, incidentID)
	if err != nil {
		return Incident{}, nil, nil, nil, nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Incident{}, nil, nil, nil, nil, nil, fmt.Errorf("commit breach incident read: %w", err)
	}
	return incident, entries, assessments, actions, actionEvents, controllerNotifications, nil
}

func (s Store) listChronologyEntries(
	ctx context.Context, query db.Querier, incidentID string,
) ([]ChronologyEntry, error) {
	rows, err := query.Query(ctx, `
		SELECT `+chronologyEntrySelectColumns("entry")+`
		FROM breach_chronology_entries AS entry
		WHERE entry.breach_incident_id = $1
		ORDER BY entry.sequence
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list breach chronology entries: %w", err)
	}
	defer rows.Close()
	entries := make([]ChronologyEntry, 0)
	for rows.Next() {
		entry, err := scanChronologyEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan breach chronology entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate breach chronology entries: %w", err)
	}
	return entries, nil
}

func (s Store) AppendChronologyEntry(
	ctx context.Context,
	incidentID string,
	actorPlatformAdminID string,
	idempotencyKey string,
	input AppendChronologyEntryInput,
) (ChronologyEntry, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 || strings.TrimSpace(actorPlatformAdminID) == "" {
		return ChronologyEntry{}, ErrInvalidRequest
	}
	normalized, err := normalizeAppendChronologyEntryInput(input)
	if err != nil {
		return ChronologyEntry{}, err
	}
	fingerprint, err := chronologyEntryFingerprint(normalized)
	if err != nil {
		return ChronologyEntry{}, err
	}
	now := s.now().UTC()

	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return ChronologyEntry{}, fmt.Errorf("begin breach chronology append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var incidentExists string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM breach_incidents WHERE id = $1 FOR UPDATE
	`, incidentID).Scan(&incidentExists); errors.Is(err, pgx.ErrNoRows) {
		return ChronologyEntry{}, ErrNotFound
	} else if err != nil {
		return ChronologyEntry{}, fmt.Errorf("lock breach incident for chronology: %w", err)
	}

	var storedFingerprint []byte
	var created bool
	entry, err := scanChronologyEntry(tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO breach_chronology_entries (
				id, breach_incident_id, entry_kind, summary, evidence_references,
				actor_platform_admin_id, idempotency_key, request_fingerprint,
				occurred_at, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
			ON CONFLICT (breach_incident_id, idempotency_key) DO NOTHING
			RETURNING *, true AS created
		), selected AS (
			SELECT * FROM inserted
			UNION ALL
			SELECT existing.*, false AS created
			FROM breach_chronology_entries AS existing
			WHERE existing.breach_incident_id = $2 AND existing.idempotency_key = $7
			  AND NOT EXISTS (SELECT 1 FROM inserted)
		)
		SELECT `+chronologyEntrySelectColumns("selected")+`,
		       selected.request_fingerprint, selected.created
		FROM selected
	`, uuid.NewString(), incidentID, normalized.EntryKind, normalized.Summary,
		normalized.EvidenceReferences, actorPlatformAdminID, idempotencyKey, fingerprint[:], now,
	), &storedFingerprint, &created)
	if err != nil {
		return ChronologyEntry{}, fmt.Errorf("append breach chronology entry: %w", err)
	}
	if !bytes.Equal(storedFingerprint, fingerprint[:]) {
		return ChronologyEntry{}, ErrIdempotencyConflict
	}
	if !created {
		if err := tx.Commit(ctx); err != nil {
			return ChronologyEntry{}, fmt.Errorf("commit breach chronology idempotent read: %w", err)
		}
		return entry, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return ChronologyEntry{}, fmt.Errorf("commit breach chronology append: %w", err)
	}
	return entry, nil
}

func (s Store) listAssessments(
	ctx context.Context, query db.Querier, incidentID string,
) ([]Assessment, error) {
	rows, err := query.Query(ctx, `
		SELECT `+breachAssessmentSelectColumns("assessment")+`
		FROM breach_assessments AS assessment
		WHERE assessment.breach_incident_id = $1
		ORDER BY assessment.sequence
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list breach assessments: %w", err)
	}
	defer rows.Close()
	assessments := make([]Assessment, 0)
	for rows.Next() {
		assessment, err := scanAssessment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan breach assessment: %w", err)
		}
		assessments = append(assessments, assessment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate breach assessments: %w", err)
	}
	return assessments, nil
}

// CreateAssessment appends one reasoned assessment snapshot to an incident.
// Like AppendChronologyEntry, it locks the parent incident row so a
// concurrent append and a not-found race resolve deterministically, and it
// is idempotency-key/fingerprint protected so a retried request never
// creates a second row.
func (s Store) CreateAssessment(
	ctx context.Context,
	incidentID string,
	actorPlatformAdminID string,
	idempotencyKey string,
	input CreateAssessmentInput,
) (Assessment, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 200 || strings.TrimSpace(actorPlatformAdminID) == "" {
		return Assessment{}, ErrInvalidRequest
	}
	normalized, err := normalizeCreateAssessmentInput(input)
	if err != nil {
		return Assessment{}, err
	}
	fingerprint, err := createAssessmentFingerprint(normalized)
	if err != nil {
		return Assessment{}, err
	}
	now := s.now().UTC()

	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return Assessment{}, fmt.Errorf("begin breach assessment creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var incidentExists string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM breach_incidents WHERE id = $1 FOR UPDATE
	`, incidentID).Scan(&incidentExists); errors.Is(err, pgx.ErrNoRows) {
		return Assessment{}, ErrNotFound
	} else if err != nil {
		return Assessment{}, fmt.Errorf("lock breach incident for assessment: %w", err)
	}

	var storedFingerprint []byte
	var created bool
	assessment, err := scanAssessment(tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO breach_assessments (
				id, breach_incident_id, authority_notification_decision,
				authority_notification_reasoning, high_risk_to_individuals, high_risk_reasoning,
				data_subject_communication_decision, data_subject_communication_reasoning,
				estimated_affected_subjects_min, estimated_affected_subjects_max,
				estimated_affected_records_min, estimated_affected_records_max,
				likely_consequences, recovery_measures, evidence_references,
				assessed_by_platform_admin_id, idempotency_key, request_fingerprint,
				assessed_at, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $19
			)
			ON CONFLICT (breach_incident_id, idempotency_key) DO NOTHING
			RETURNING *, true AS created
		), selected AS (
			SELECT * FROM inserted
			UNION ALL
			SELECT existing.*, false AS created
			FROM breach_assessments AS existing
			WHERE existing.breach_incident_id = $2 AND existing.idempotency_key = $17
			  AND NOT EXISTS (SELECT 1 FROM inserted)
		)
		SELECT `+breachAssessmentSelectColumns("selected")+`,
		       selected.request_fingerprint, selected.created
		FROM selected
	`, uuid.NewString(), incidentID, normalized.AuthorityNotificationDecision,
		normalized.AuthorityNotificationReasoning, normalized.HighRiskToIndividuals,
		normalized.HighRiskReasoning, normalized.DataSubjectCommunicationDecision,
		normalized.DataSubjectCommunicationReasoning, normalized.EstimatedAffectedSubjectsMin,
		normalized.EstimatedAffectedSubjectsMax, normalized.EstimatedAffectedRecordsMin,
		normalized.EstimatedAffectedRecordsMax, normalized.LikelyConsequences,
		normalized.RecoveryMeasures, normalized.EvidenceReferences, actorPlatformAdminID,
		idempotencyKey, fingerprint[:], now,
	), &storedFingerprint, &created)
	if err != nil {
		return Assessment{}, fmt.Errorf("create breach assessment: %w", err)
	}
	if !bytes.Equal(storedFingerprint, fingerprint[:]) {
		return Assessment{}, ErrIdempotencyConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return Assessment{}, fmt.Errorf("commit breach assessment creation: %w", err)
	}
	return assessment, nil
}

func requireTenantsExist(ctx context.Context, query db.Querier, tenantIDs []string) error {
	var count int
	if err := query.QueryRow(ctx, `
		SELECT count(*) FROM tenants WHERE id = ANY($1::uuid[])
	`, tenantIDs).Scan(&count); err != nil {
		return fmt.Errorf("verify affected tenants: %w", err)
	}
	if count != len(tenantIDs) {
		return ErrTenantNotFound
	}
	return nil
}

func tenantIDsForIncident(ctx context.Context, query db.Querier, incidentID string) ([]string, error) {
	rows, err := query.Query(ctx, `
		SELECT tenant_id::text FROM breach_incident_tenants
		WHERE breach_incident_id = $1
		ORDER BY tenant_id
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list breach incident tenants: %w", err)
	}
	defer rows.Close()
	tenantIDs := make([]string, 0)
	for rows.Next() {
		var tenantID string
		if err := rows.Scan(&tenantID); err != nil {
			return nil, fmt.Errorf("scan breach incident tenant: %w", err)
		}
		tenantIDs = append(tenantIDs, tenantID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate breach incident tenants: %w", err)
	}
	return tenantIDs, nil
}

func createIncidentFingerprint(input CreateIncidentInput) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
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
		AffectedTenantIDs         []string        `json:"affected_tenant_ids"`
	}{
		AwarenessAt: input.AwarenessAt, DetectionSource: input.DetectionSource,
		ControllerRole: input.ControllerRole, AffectedEnvironments: input.AffectedEnvironments,
		AffectedSystemCategories:  input.AffectedSystemCategories,
		AffectedDataCategories:    input.AffectedDataCategories,
		AffectedSubjectCategories: input.AffectedSubjectCategories,
		ConfidentialityImpact:     input.ConfidentialityImpact,
		IntegrityImpact:           input.IntegrityImpact,
		AvailabilityImpact:        input.AvailabilityImpact,
		ContainmentSummary:        input.ContainmentSummary,
		AffectedTenantIDs:         input.AffectedTenantIDs,
	})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode breach incident fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func chronologyEntryFingerprint(input AppendChronologyEntryInput) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(struct {
		EntryKind          ChronologyEntryKind `json:"entry_kind"`
		Summary            string              `json:"summary"`
		EvidenceReferences []string            `json:"evidence_references"`
	}{EntryKind: input.EntryKind, Summary: input.Summary, EvidenceReferences: input.EvidenceReferences})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode breach chronology fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

func createAssessmentFingerprint(input CreateAssessmentInput) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode breach assessment fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}

type rowScanner interface {
	Scan(...any) error
}

func breachIncidentSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.public_id, ` + alias + `.awareness_at, ` +
		alias + `.detection_source, ` + alias + `.controller_role, ` +
		alias + `.affected_environments, ` + alias + `.affected_system_categories, ` +
		alias + `.affected_data_categories, ` + alias + `.affected_subject_categories, ` +
		alias + `.confidentiality_impact, ` + alias + `.integrity_impact, ` +
		alias + `.availability_impact, ` + alias + `.containment_summary, ` +
		alias + `.created_by_platform_admin_id, ` + alias + `.created_at, ` +
		alias + `.authority_notification_deadline_at`
}

func scanIncident(row rowScanner, extra ...any) (Incident, error) {
	var incident Incident
	destinations := []any{
		&incident.ID, &incident.PublicID, &incident.AwarenessAt, &incident.DetectionSource,
		&incident.ControllerRole, &incident.AffectedEnvironments, &incident.AffectedSystemCategories,
		&incident.AffectedDataCategories, &incident.AffectedSubjectCategories,
		&incident.ConfidentialityImpact, &incident.IntegrityImpact, &incident.AvailabilityImpact,
		&incident.ContainmentSummary, &incident.CreatedByPlatformAdminID, &incident.CreatedAt,
		&incident.AuthorityNotificationDeadlineAt,
	}
	destinations = append(destinations, extra...)
	err := row.Scan(destinations...)
	return incident, err
}

func scanIncidentWithTenants(row rowScanner) (Incident, error) {
	var incident Incident
	destinations := []any{
		&incident.ID, &incident.PublicID, &incident.AwarenessAt, &incident.DetectionSource,
		&incident.ControllerRole, &incident.AffectedEnvironments, &incident.AffectedSystemCategories,
		&incident.AffectedDataCategories, &incident.AffectedSubjectCategories,
		&incident.ConfidentialityImpact, &incident.IntegrityImpact, &incident.AvailabilityImpact,
		&incident.ContainmentSummary, &incident.CreatedByPlatformAdminID, &incident.CreatedAt,
		&incident.AuthorityNotificationDeadlineAt, &incident.AffectedTenantIDs,
	}
	err := row.Scan(destinations...)
	return incident, err
}

func breachAssessmentSelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.breach_incident_id, ` +
		alias + `.authority_notification_decision, ` + alias + `.authority_notification_reasoning, ` +
		alias + `.high_risk_to_individuals, ` + alias + `.high_risk_reasoning, ` +
		alias + `.data_subject_communication_decision, ` +
		alias + `.data_subject_communication_reasoning, ` +
		alias + `.estimated_affected_subjects_min, ` + alias + `.estimated_affected_subjects_max, ` +
		alias + `.estimated_affected_records_min, ` + alias + `.estimated_affected_records_max, ` +
		alias + `.likely_consequences, ` + alias + `.recovery_measures, ` +
		alias + `.evidence_references, ` + alias + `.assessed_by_platform_admin_id, ` +
		alias + `.assessed_at, ` + alias + `.created_at`
}

func scanAssessment(row rowScanner, extra ...any) (Assessment, error) {
	var assessment Assessment
	destinations := []any{
		&assessment.ID, &assessment.BreachIncidentID, &assessment.AuthorityNotificationDecision,
		&assessment.AuthorityNotificationReasoning, &assessment.HighRiskToIndividuals,
		&assessment.HighRiskReasoning, &assessment.DataSubjectCommunicationDecision,
		&assessment.DataSubjectCommunicationReasoning, &assessment.EstimatedAffectedSubjectsMin,
		&assessment.EstimatedAffectedSubjectsMax, &assessment.EstimatedAffectedRecordsMin,
		&assessment.EstimatedAffectedRecordsMax, &assessment.LikelyConsequences,
		&assessment.RecoveryMeasures, &assessment.EvidenceReferences,
		&assessment.AssessedByPlatformAdminID, &assessment.AssessedAt, &assessment.CreatedAt,
	}
	destinations = append(destinations, extra...)
	err := row.Scan(destinations...)
	return assessment, err
}

func chronologyEntrySelectColumns(alias string) string {
	return alias + `.id, ` + alias + `.breach_incident_id, ` + alias + `.entry_kind, ` +
		alias + `.summary, ` + alias + `.evidence_references, ` +
		alias + `.actor_platform_admin_id, ` + alias + `.occurred_at, ` + alias + `.created_at`
}

func scanChronologyEntry(row rowScanner, extra ...any) (ChronologyEntry, error) {
	var entry ChronologyEntry
	destinations := []any{
		&entry.ID, &entry.BreachIncidentID, &entry.EntryKind, &entry.Summary,
		&entry.EvidenceReferences, &entry.ActorPlatformAdminID, &entry.OccurredAt, &entry.CreatedAt,
	}
	destinations = append(destinations, extra...)
	err := row.Scan(destinations...)
	return entry, err
}
