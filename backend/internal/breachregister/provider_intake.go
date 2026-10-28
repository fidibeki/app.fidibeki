package breachregister

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/privacy"
)

// ProviderIncidentIntake is an immutable snapshot of an external provider
// notice and the code-owned governance facts known when it was received.
type ProviderIncidentIntake struct {
	ID                         string                           `json:"id"`
	Sequence                   int64                            `json:"sequence"`
	BreachIncidentID           string                           `json:"breach_incident_id"`
	ProviderID                 string                           `json:"provider_id"`
	GovernanceRegistryVersion  int                              `json:"governance_registry_version"`
	ProviderDisplayName        string                           `json:"provider_display_name"`
	BoundaryKind               privacy.GovernanceBoundaryKind   `json:"boundary_kind"`
	ApprovalStatus             privacy.GovernanceApprovalStatus `json:"approval_status"`
	ContractStatus             privacy.GovernanceContractStatus `json:"contract_status"`
	PrimaryRetentionCategory   string                           `json:"primary_retention_category"`
	ProviderNoticeReference    string                           `json:"provider_notice_reference"`
	ProviderReportedAt         time.Time                        `json:"provider_reported_at"`
	ReceivedAt                 time.Time                        `json:"received_at"`
	Summary                    string                           `json:"summary"`
	EvidenceReferences         []string                         `json:"evidence_references"`
	AffectedDataFlowCategories []string                         `json:"affected_data_flow_categories"`
	AffectedTenantIDs          []string                         `json:"affected_tenant_ids"`
	ActorPlatformAdminID       string                           `json:"actor_platform_admin_id"`
	CreatedAt                  time.Time                        `json:"created_at"`
}

type CreateProviderIncidentIntakeInput struct {
	ProviderID                 string
	ProviderNoticeReference    string
	ProviderReportedAt         time.Time
	Summary                    string
	EvidenceReferences         []string
	AffectedDataFlowCategories []string
	AffectedTenantIDs          []string
}

type normalizedProviderIncidentIntake struct {
	CreateProviderIncidentIntakeInput
	GovernanceRegistryVersion int
	ProviderDisplayName       string
	BoundaryKind              privacy.GovernanceBoundaryKind
	ApprovalStatus            privacy.GovernanceApprovalStatus
	ContractStatus            privacy.GovernanceContractStatus
	PrimaryRetentionCategory  string
}

func normalizeProviderIncidentIntake(
	input CreateProviderIncidentIntakeInput,
	registry privacy.ProviderGovernanceRegistry,
	now time.Time,
) (normalizedProviderIncidentIntake, error) {
	if registry.Version <= 0 || now.IsZero() {
		return normalizedProviderIncidentIntake{}, ErrInvalidRequest
	}
	providerID := strings.TrimSpace(input.ProviderID)
	var provider *privacy.ProviderGovernanceRecord
	for index := range registry.Providers {
		if registry.Providers[index].ProviderID == providerID {
			provider = &registry.Providers[index]
			break
		}
	}
	if provider == nil || provider.BoundaryKind == privacy.GovernanceBoundaryInternalRuntime {
		return normalizedProviderIncidentIntake{}, ErrInvalidRequest
	}
	noticeReference := strings.TrimSpace(input.ProviderNoticeReference)
	if noticeReference == "" || len(noticeReference) > 300 ||
		containsControlCharacter(noticeReference) {
		return normalizedProviderIncidentIntake{}, ErrInvalidRequest
	}
	if input.ProviderReportedAt.IsZero() || input.ProviderReportedAt.After(now) {
		return normalizedProviderIncidentIntake{}, ErrInvalidRequest
	}
	summary := strings.TrimSpace(input.Summary)
	if summary == "" || len(summary) > 4000 || strings.ContainsRune(summary, '\x00') {
		return normalizedProviderIncidentIntake{}, ErrInvalidRequest
	}
	evidenceReferences, err := normalizeEvidenceReferences(input.EvidenceReferences)
	if err != nil {
		return normalizedProviderIncidentIntake{}, err
	}
	tenantIDs, err := normalizeTenantIDs(input.AffectedTenantIDs)
	if err != nil {
		return normalizedProviderIncidentIntake{}, err
	}
	sort.Strings(tenantIDs)

	allowedCategories := make(map[string]struct{}, len(registry.RoleMatrix))
	for _, entry := range registry.RoleMatrix {
		allowedCategories[entry.Category] = struct{}{}
	}
	dataFlowCategories, err := validateBoundedSet(
		input.AffectedDataFlowCategories, allowedCategories, 32,
	)
	if err != nil {
		return normalizedProviderIncidentIntake{}, err
	}
	sort.Strings(dataFlowCategories)
	if _, included := allowedValue(dataFlowCategories, provider.RetentionReviewRef); !included {
		return normalizedProviderIncidentIntake{}, ErrInvalidRequest
	}

	return normalizedProviderIncidentIntake{
		CreateProviderIncidentIntakeInput: CreateProviderIncidentIntakeInput{
			ProviderID: providerID, ProviderNoticeReference: noticeReference,
			ProviderReportedAt: input.ProviderReportedAt.UTC(), Summary: summary,
			EvidenceReferences: evidenceReferences, AffectedTenantIDs: tenantIDs,
			AffectedDataFlowCategories: dataFlowCategories,
		},
		GovernanceRegistryVersion: registry.Version,
		ProviderDisplayName:       provider.DisplayName,
		BoundaryKind:              provider.BoundaryKind,
		ApprovalStatus:            provider.Approval,
		ContractStatus:            provider.ContractStatus,
		PrimaryRetentionCategory:  provider.RetentionReviewRef,
	}, nil
}

func allowedValue(values []string, target string) (string, bool) {
	for _, value := range values {
		if value == target {
			return value, true
		}
	}
	return "", false
}

func providerIncidentIntakeFingerprint(
	input normalizedProviderIncidentIntake,
) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode provider incident intake fingerprint: %w", err)
	}
	return sha256.Sum256(encoded), nil
}
