package breachregister

import (
	"errors"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/privacy"
)

func TestNormalizeProviderIncidentIntakeFreezesGovernanceSnapshot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	registry, err := privacy.LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance() error = %v", err)
	}
	input := CreateProviderIncidentIntakeInput{
		ProviderID:              " resend_transactional_email ",
		ProviderNoticeReference: " provider-case-123 ",
		ProviderReportedAt:      now.Add(-time.Hour),
		Summary:                 " Provider reported a delivery-system incident. ",
		EvidenceReferences:      []string{"case:123"},
		AffectedDataFlowCategories: []string{
			"external_communications", "external_communications",
		},
		AffectedTenantIDs: []string{
			"22222222-2222-4222-8222-222222222222",
			"11111111-1111-4111-8111-111111111111",
		},
	}
	normalized, err := normalizeProviderIncidentIntake(input, registry, now)
	if err != nil {
		t.Fatalf("normalizeProviderIncidentIntake() error = %v", err)
	}
	if normalized.ProviderID != "resend_transactional_email" ||
		normalized.ProviderDisplayName != "Resend transactional email" ||
		normalized.GovernanceRegistryVersion != registry.Version ||
		normalized.BoundaryKind != privacy.GovernanceBoundaryServiceProvider ||
		normalized.ApprovalStatus != privacy.GovernanceApprovalPending ||
		normalized.ContractStatus != privacy.GovernanceContractUnknown ||
		normalized.PrimaryRetentionCategory != "external_communications" {
		t.Fatalf("normalized governance snapshot = %#v", normalized)
	}
	if len(normalized.AffectedDataFlowCategories) != 1 ||
		normalized.AffectedDataFlowCategories[0] != "external_communications" {
		t.Fatalf(
			"AffectedDataFlowCategories = %#v, want one normalized category",
			normalized.AffectedDataFlowCategories,
		)
	}
	if got := normalized.AffectedTenantIDs; len(got) != 2 ||
		got[0] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("AffectedTenantIDs = %#v, want deterministic order", got)
	}
}

func TestNormalizeProviderIncidentIntakeRejectsUnsafeOrUnmappedInput(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	registry, err := privacy.LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance() error = %v", err)
	}
	valid := CreateProviderIncidentIntakeInput{
		ProviderID:                 "resend_transactional_email",
		ProviderNoticeReference:    "provider-case-123",
		ProviderReportedAt:         now.Add(-time.Hour),
		Summary:                    "Provider reported an incident.",
		AffectedDataFlowCategories: []string{"external_communications"},
		AffectedTenantIDs:          []string{"11111111-1111-4111-8111-111111111111"},
	}
	tests := []struct {
		name   string
		mutate func(*CreateProviderIncidentIntakeInput)
	}{
		{name: "unknown provider", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.ProviderID = "unknown"
		}},
		{name: "internal runtime", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.ProviderID = "email_capture_filesystem"
		}},
		{name: "missing primary category", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.AffectedDataFlowCategories = []string{"external_infrastructure"}
		}},
		{name: "unknown category", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.AffectedDataFlowCategories = []string{"not-real"}
		}},
		{name: "future provider time", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.ProviderReportedAt = now.Add(time.Second)
		}},
		{name: "notice control character", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.ProviderNoticeReference = "provider\ncase"
		}},
		{name: "summary NUL", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.Summary = "provider\x00case"
		}},
		{name: "missing tenant", mutate: func(input *CreateProviderIncidentIntakeInput) {
			input.AffectedTenantIDs = nil
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := valid
			test.mutate(&input)
			if _, err := normalizeProviderIncidentIntake(input, registry, now); !errors.Is(
				err, ErrInvalidRequest,
			) {
				t.Fatalf("normalizeProviderIncidentIntake() error = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestProviderIncidentIntakeFingerprintIsOrderStableAndIncludesSnapshot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	registry, err := privacy.LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance() error = %v", err)
	}
	input := CreateProviderIncidentIntakeInput{
		ProviderID:                 "resend_transactional_email",
		ProviderNoticeReference:    "provider-case-123",
		ProviderReportedAt:         now.Add(-time.Hour),
		Summary:                    "Provider reported an incident.",
		AffectedDataFlowCategories: []string{"external_communications", "workflow_metadata"},
		AffectedTenantIDs: []string{
			"22222222-2222-4222-8222-222222222222",
			"11111111-1111-4111-8111-111111111111",
		},
	}
	first, err := normalizeProviderIncidentIntake(input, registry, now)
	if err != nil {
		t.Fatalf("normalize first input: %v", err)
	}
	input.AffectedDataFlowCategories[0], input.AffectedDataFlowCategories[1] =
		input.AffectedDataFlowCategories[1], input.AffectedDataFlowCategories[0]
	input.AffectedTenantIDs[0], input.AffectedTenantIDs[1] =
		input.AffectedTenantIDs[1], input.AffectedTenantIDs[0]
	second, err := normalizeProviderIncidentIntake(input, registry, now)
	if err != nil {
		t.Fatalf("normalize reordered input: %v", err)
	}
	firstFingerprint, _ := providerIncidentIntakeFingerprint(first)
	secondFingerprint, _ := providerIncidentIntakeFingerprint(second)
	if firstFingerprint != secondFingerprint {
		t.Fatal("semantically equal provider intake sets produced different fingerprints")
	}
	second.GovernanceRegistryVersion++
	changedFingerprint, _ := providerIncidentIntakeFingerprint(second)
	if changedFingerprint == firstFingerprint {
		t.Fatal("governance snapshot change was absent from fingerprint")
	}
}
