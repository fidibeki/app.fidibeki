package breachregister

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/privacy"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s Store) CreateProviderIncidentIntake(
	ctx context.Context,
	incidentID string,
	actorPlatformAdminID string,
	idempotencyKey string,
	input CreateProviderIncidentIntakeInput,
) (ProviderIncidentIntake, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if !isUUID(incidentID) || strings.TrimSpace(actorPlatformAdminID) == "" ||
		idempotencyKey == "" || len(idempotencyKey) > 200 {
		return ProviderIncidentIntake{}, ErrInvalidRequest
	}
	now := s.now().UTC()
	registry, err := privacy.LoadProviderGovernance(now)
	if err != nil {
		return ProviderIncidentIntake{}, fmt.Errorf("load provider governance: %w", err)
	}
	normalized, err := normalizeProviderIncidentIntake(input, registry, now)
	if err != nil {
		return ProviderIncidentIntake{}, err
	}
	fingerprint, err := providerIncidentIntakeFingerprint(normalized)
	if err != nil {
		return ProviderIncidentIntake{}, err
	}

	tx, err := s.Database.Begin(ctx)
	if err != nil {
		return ProviderIncidentIntake{}, fmt.Errorf("begin provider incident intake: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var awarenessAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT awareness_at
		FROM breach_incidents
		WHERE id = $1
		FOR UPDATE
	`, incidentID).Scan(&awarenessAt); errors.Is(err, pgx.ErrNoRows) {
		return ProviderIncidentIntake{}, ErrNotFound
	} else if err != nil {
		return ProviderIncidentIntake{}, fmt.Errorf("lock breach incident for provider intake: %w", err)
	}
	if now.Before(awarenessAt) {
		return ProviderIncidentIntake{}, ErrInvalidRequest
	}
	if err := requirePlatformAdminExists(ctx, tx, actorPlatformAdminID); err != nil {
		return ProviderIncidentIntake{}, err
	}
	if existing, storedFingerprint, found, err := providerIntakeByIdempotencyKey(
		ctx, tx, incidentID, idempotencyKey,
	); err != nil {
		return ProviderIncidentIntake{}, err
	} else if found {
		if !bytes.Equal(storedFingerprint, fingerprint[:]) {
			return ProviderIncidentIntake{}, ErrIdempotencyConflict
		}
		existing.AffectedTenantIDs, err = tenantIDsForProviderIntake(ctx, tx, existing.ID)
		if err != nil {
			return ProviderIncidentIntake{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ProviderIncidentIntake{}, fmt.Errorf("commit provider intake idempotent read: %w", err)
		}
		return existing, nil
	}
	if err := requireIncidentTenants(
		ctx, tx, incidentID, normalized.AffectedTenantIDs,
	); err != nil {
		return ProviderIncidentIntake{}, err
	}

	intake, err := scanProviderIncidentIntake(tx.QueryRow(ctx, `
		INSERT INTO breach_provider_incident_intakes (
			id, breach_incident_id, provider_id, governance_registry_version,
			provider_display_name, boundary_kind, approval_status, contract_status,
			primary_retention_category, provider_notice_reference, provider_reported_at,
			received_at, summary, evidence_references, affected_data_flow_categories,
			actor_platform_admin_id, idempotency_key, request_fingerprint, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $12
		)
		RETURNING `+providerIncidentIntakeSelectColumns("")+`
	`, uuid.NewString(), incidentID, normalized.ProviderID,
		normalized.GovernanceRegistryVersion, normalized.ProviderDisplayName,
		normalized.BoundaryKind, normalized.ApprovalStatus, normalized.ContractStatus,
		normalized.PrimaryRetentionCategory, normalized.ProviderNoticeReference,
		normalized.ProviderReportedAt, now, normalized.Summary,
		normalized.EvidenceReferences, normalized.AffectedDataFlowCategories,
		actorPlatformAdminID, idempotencyKey, fingerprint[:]))
	if err != nil {
		return ProviderIncidentIntake{}, fmt.Errorf("create provider incident intake: %w", err)
	}
	for _, tenantID := range normalized.AffectedTenantIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO breach_provider_incident_tenants (
				provider_incident_intake_id, breach_incident_id, tenant_id, created_at
			) VALUES ($1, $2, $3, $4)
		`, intake.ID, incidentID, tenantID, now); err != nil {
			return ProviderIncidentIntake{}, fmt.Errorf("link provider incident intake tenant: %w", err)
		}
	}
	intake.AffectedTenantIDs = normalized.AffectedTenantIDs
	if err := tx.Commit(ctx); err != nil {
		return ProviderIncidentIntake{}, fmt.Errorf("commit provider incident intake: %w", err)
	}
	return intake, nil
}

func requireIncidentTenants(
	ctx context.Context, query db.Querier, incidentID string, tenantIDs []string,
) error {
	var count int
	if err := query.QueryRow(ctx, `
		SELECT count(*)
		FROM breach_incident_tenants
		WHERE breach_incident_id = $1 AND tenant_id = ANY($2::uuid[])
	`, incidentID, tenantIDs).Scan(&count); err != nil {
		return fmt.Errorf("verify provider intake tenant scope: %w", err)
	}
	if count != len(tenantIDs) {
		return ErrTenantNotFound
	}
	return nil
}

func providerIntakeByIdempotencyKey(
	ctx context.Context,
	query db.Querier,
	incidentID string,
	idempotencyKey string,
) (ProviderIncidentIntake, []byte, bool, error) {
	var fingerprint []byte
	intake, err := scanProviderIncidentIntake(query.QueryRow(ctx, `
		SELECT `+providerIncidentIntakeSelectColumns("intake")+`, intake.request_fingerprint
		FROM breach_provider_incident_intakes AS intake
		WHERE intake.breach_incident_id = $1 AND intake.idempotency_key = $2
	`, incidentID, idempotencyKey), &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderIncidentIntake{}, nil, false, nil
	}
	if err != nil {
		return ProviderIncidentIntake{}, nil, false,
			fmt.Errorf("read provider incident intake idempotency key: %w", err)
	}
	return intake, fingerprint, true, nil
}

func (s Store) listProviderIncidentIntakes(
	ctx context.Context, query db.Querier, incidentID string,
) ([]ProviderIncidentIntake, error) {
	rows, err := query.Query(ctx, `
		SELECT `+providerIncidentIntakeSelectColumns("intake")+`,
		       COALESCE((
		           SELECT array_agg(link.tenant_id::text ORDER BY link.tenant_id)
		           FROM breach_provider_incident_tenants AS link
		           WHERE link.provider_incident_intake_id = intake.id
		       ), '{}') AS affected_tenant_ids
		FROM breach_provider_incident_intakes AS intake
		WHERE intake.breach_incident_id = $1
		ORDER BY intake.sequence
	`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("list provider incident intakes: %w", err)
	}
	defer rows.Close()
	intakes := make([]ProviderIncidentIntake, 0)
	for rows.Next() {
		intake, err := scanProviderIncidentIntakeWithTenants(rows)
		if err != nil {
			return nil, fmt.Errorf("scan provider incident intake: %w", err)
		}
		intakes = append(intakes, intake)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider incident intakes: %w", err)
	}
	return intakes, nil
}

func tenantIDsForProviderIntake(
	ctx context.Context, query db.Querier, intakeID string,
) ([]string, error) {
	var tenantIDs []string
	if err := query.QueryRow(ctx, `
		SELECT COALESCE(array_agg(tenant_id::text ORDER BY tenant_id), '{}')
		FROM breach_provider_incident_tenants
		WHERE provider_incident_intake_id = $1
	`, intakeID).Scan(&tenantIDs); err != nil {
		return nil, fmt.Errorf("list provider incident intake tenants: %w", err)
	}
	return tenantIDs, nil
}

func providerIncidentIntakeSelectColumns(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return prefix + `id, ` + prefix + `sequence, ` + prefix + `breach_incident_id, ` +
		prefix + `provider_id, ` + prefix + `governance_registry_version, ` +
		prefix + `provider_display_name, ` + prefix + `boundary_kind, ` +
		prefix + `approval_status, ` + prefix + `contract_status, ` +
		prefix + `primary_retention_category, ` + prefix + `provider_notice_reference, ` +
		prefix + `provider_reported_at, ` + prefix + `received_at, ` + prefix + `summary, ` +
		prefix + `evidence_references, ` + prefix + `affected_data_flow_categories, ` +
		prefix + `actor_platform_admin_id, ` + prefix + `created_at`
}

func scanProviderIncidentIntake(
	row rowScanner, extra ...any,
) (ProviderIncidentIntake, error) {
	var intake ProviderIncidentIntake
	destinations := []any{
		&intake.ID, &intake.Sequence, &intake.BreachIncidentID, &intake.ProviderID,
		&intake.GovernanceRegistryVersion, &intake.ProviderDisplayName, &intake.BoundaryKind,
		&intake.ApprovalStatus, &intake.ContractStatus, &intake.PrimaryRetentionCategory,
		&intake.ProviderNoticeReference, &intake.ProviderReportedAt, &intake.ReceivedAt,
		&intake.Summary, &intake.EvidenceReferences, &intake.AffectedDataFlowCategories,
		&intake.ActorPlatformAdminID, &intake.CreatedAt,
	}
	destinations = append(destinations, extra...)
	err := row.Scan(destinations...)
	return intake, err
}

func scanProviderIncidentIntakeWithTenants(row rowScanner) (ProviderIncidentIntake, error) {
	var tenantIDs []string
	intake, err := scanProviderIncidentIntake(row, &tenantIDs)
	intake.AffectedTenantIDs = tenantIDs
	return intake, err
}
