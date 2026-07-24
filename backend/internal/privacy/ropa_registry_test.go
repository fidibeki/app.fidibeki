package privacy

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoadProcessingActivityRegistryGovernanceReturnsPendingRegistry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	registry, err := LoadProcessingActivityRegistry(now)
	if err != nil {
		t.Fatalf("LoadProcessingActivityRegistry error = %v", err)
	}
	if registry.Version != ropaRegistryVersion {
		t.Fatalf("Version = %d, want %d", registry.Version, ropaRegistryVersion)
	}
	if len(registry.Activities) == 0 {
		t.Fatal("registry has no activities")
	}
	if registry.Summary.Activities != len(registry.Activities) {
		t.Fatalf("Summary.Activities = %d, want %d", registry.Summary.Activities, len(registry.Activities))
	}
	if registry.Summary.LawfulBasisIdentified != 0 || registry.Summary.DPIARequired != 0 ||
		registry.Summary.DPIANotRequired != 0 || registry.Summary.PublicationReady != 0 {
		t.Fatalf("Summary = %#v, want everything pending/not publication-ready", registry.Summary)
	}
	if registry.Summary.LawfulBasisPending != len(registry.Activities) ||
		registry.Summary.DPIAPending != len(registry.Activities) {
		t.Fatalf("Summary = %#v, want all activities lawful-basis/DPIA pending", registry.Summary)
	}
	for _, activity := range registry.Activities {
		if activity.LawfulBasis != LawfulBasisPending {
			t.Fatalf("activity %q claims a lawful basis: %#v", activity.ActivityID, activity)
		}
		if activity.DPIA.Outcome != DPIAOutcomePending {
			t.Fatalf("activity %q claims a DPIA outcome: %#v", activity.ActivityID, activity)
		}
		if activity.PublicationReady {
			t.Fatalf("activity %q claims publication readiness: %#v", activity.ActivityID, activity)
		}
		if activity.Owner != governanceUnassigned || activity.DPIA.Owner != governanceUnassigned {
			t.Fatalf("activity %q claims an owner: %#v", activity.ActivityID, activity)
		}
		if strings.TrimSpace(activity.LawfulBasisReason) == "" || strings.TrimSpace(activity.DPIA.Reason) == "" {
			t.Fatalf("activity %q has a blank reason: %#v", activity.ActivityID, activity)
		}
	}
}

func TestProcessingActivityRegistryCoversEveryRetentionCategoryExactlyOnce(t *testing.T) {
	t.Parallel()
	definitions := retentionRegistry()
	activities := processingActivityRegistry()

	covered := make(map[string]string)
	for _, activity := range activities {
		for _, category := range activity.RetentionCategories {
			if owner, exists := covered[category]; exists {
				t.Fatalf("retention category %q covered by both %q and %q", category, owner, activity.ActivityID)
			}
			covered[category] = activity.ActivityID
		}
	}
	for _, definition := range definitions {
		if _, ok := covered[definition.Category]; !ok {
			t.Fatalf("retention category %q is not covered by any processing activity", definition.Category)
		}
	}
	if len(covered) != len(definitions) {
		t.Fatalf("covered %d categories, want exactly %d", len(covered), len(definitions))
	}
}

func TestProcessingActivityRegistryMissingRetentionCategoryFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	activities[0].RetentionCategories = activities[0].RetentionCategories[:0]
	hydrateRecipientProviders(activities, definitions)
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil || !strings.Contains(err.Error(), "references no retention category") {
		t.Fatalf("error = %v, want no-retention-category error", err)
	}
}

func TestProcessingActivityRegistryDuplicateCategoryFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	// identity_assets is controller-role, matching authentication_and_account_security's existing
	// (also controller-role) categories, so this triggers the coverage-duplication check rather
	// than the unrelated role-consistency check.
	for index, activity := range activities {
		if activity.ActivityID == "authentication_and_account_security" {
			activities[index].RetentionCategories = append(activities[index].RetentionCategories, "identity_assets")
		}
	}
	hydrateRecipientProviders(activities, definitions)
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil || !strings.Contains(err.Error(), "is assigned to both") {
		t.Fatalf("error = %v, want duplicate-assignment error", err)
	}
}

func TestProcessingActivityRegistryUnknownCategoryFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	activities[0].RetentionCategories = append(activities[0].RetentionCategories, "not_a_real_category")
	hydrateRecipientProviders(activities, definitions)
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil || !strings.Contains(err.Error(), "references unknown retention category") {
		t.Fatalf("error = %v, want unknown-category error", err)
	}
}

func TestProcessingActivityRegistryDuplicateActivityIDFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	activities[1].ActivityID = activities[0].ActivityID
	activities[1].RetentionCategories = nil
	hydrateRecipientProviders(activities, definitions)
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil {
		t.Fatal("expected an error for duplicate activity ID or empty retention categories")
	}
}

func TestProcessingActivityRegistryRoleMismatchFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	for index, activity := range activities {
		if activity.ActivityID == "workspace_identity_and_access" {
			activities[index].Role = RetentionRoleProcessor
		}
	}
	hydrateRecipientProviders(activities, definitions)
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil || !strings.Contains(err.Error(), "evidence role") {
		t.Fatalf("error = %v, want role-mismatch error", err)
	}
}

func TestProcessingActivityRegistryInconsistentGroupedRoleFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	// A synthetic single-activity registry: auth_magic_links is controller-role and
	// workflow_metadata is processor-role, so grouping them under one activity is inconsistent
	// regardless of which categories any other activity owns.
	activities := []ProcessingActivityRecord{
		{
			ActivityID:          "synthetic_mixed_role_activity",
			Version:             1,
			Label:               "Synthetic mixed-role activity",
			Purpose:             "Test-only activity mixing two roles.",
			Role:                RetentionRoleController,
			DataSubjects:        []ROPADataSubjectClass{ROPADataSubjectTenantMembers},
			DataCategories:      []ROPADataCategoryClass{ROPADataCategoryAuthenticationSecurity},
			RetentionCategories: []string{"auth_magic_links", "workflow_metadata"},
			SecurityControls:    []string{"test-only control"},
			LawfulBasis:         LawfulBasisPending,
			LawfulBasisReason:   "Test-only pending reason.",
			Owner:               governanceUnassigned,
			ReviewedBy:          governanceUnassigned,
			DPIA: DPIAScreening{
				Outcome: DPIAOutcomePending, Reason: "Test-only pending reason.", Owner: governanceUnassigned,
			},
		},
	}
	hydrateRecipientProviders(activities, definitions)
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil || !strings.Contains(err.Error(), "inconsistent roles") {
		t.Fatalf("error = %v, want inconsistent-role error", err)
	}
}

func TestProcessingActivityRegistryRecipientProvidersAreDerived(t *testing.T) {
	t.Parallel()
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	hydrateRecipientProviders(activities, definitions)

	byID := make(map[string]ProcessingActivityRecord, len(activities))
	for _, activity := range activities {
		byID[activity.ActivityID] = activity
	}

	aiTriage, ok := byID["ai_assisted_triage"]
	if !ok {
		t.Fatal("ai_assisted_triage activity not found")
	}
	if len(aiTriage.RecipientProviders) != 1 || aiTriage.RecipientProviders[0] != "google_gemini_api" {
		t.Fatalf("ai_assisted_triage RecipientProviders = %v, want [google_gemini_api]", aiTriage.RecipientProviders)
	}

	workspace, ok := byID["workspace_identity_and_access"]
	if !ok {
		t.Fatal("workspace_identity_and_access activity not found")
	}
	if len(workspace.RecipientProviders) != 0 {
		t.Fatalf("workspace_identity_and_access RecipientProviders = %v, want none", workspace.RecipientProviders)
	}

	infra, ok := byID["infrastructure_and_platform_operations"]
	if !ok {
		t.Fatal("infrastructure_and_platform_operations activity not found")
	}
	if len(infra.RecipientProviders) != 5 {
		t.Fatalf("infrastructure_and_platform_operations RecipientProviders = %v, want 5 providers", infra.RecipientProviders)
	}
}

func TestProcessingActivityRegistryUnknownRecipientProviderFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	hydrateRecipientProviders(activities, definitions)
	activities[0].RecipientProviders = append(activities[0].RecipientProviders, "fictional_provider")
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil || !strings.Contains(err.Error(), "unknown provider governance entry") {
		t.Fatalf("error = %v, want unknown-provider error", err)
	}
}

func TestProcessingActivityRegistryLawfulBasisValidation(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()

	t.Run("pending with grounds fails", func(t *testing.T) {
		t.Parallel()
		activities := processingActivityRegistry()
		activities[0].LawfulBasisGrounds = []LawfulBasisGround{LawfulBasisGroundConsent}
		hydrateRecipientProviders(activities, definitions)
		err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "pending but carries lawful-basis grounds") {
			t.Fatalf("error = %v, want pending-with-grounds error", err)
		}
	})

	t.Run("identified without grounds fails", func(t *testing.T) {
		t.Parallel()
		activities := processingActivityRegistry()
		activities[0].LawfulBasis = LawfulBasisIdentified
		hydrateRecipientProviders(activities, definitions)
		err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "without any ground") {
			t.Fatalf("error = %v, want missing-ground error", err)
		}
	})

	t.Run("identified with placeholder reason fails", func(t *testing.T) {
		t.Parallel()
		activities := processingActivityRegistry()
		activities[0].LawfulBasis = LawfulBasisIdentified
		activities[0].LawfulBasisGrounds = []LawfulBasisGround{LawfulBasisGroundContract}
		activities[0].LawfulBasisReason = "unknown"
		hydrateRecipientProviders(activities, definitions)
		err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "placeholder reason") {
			t.Fatalf("error = %v, want placeholder-reason error", err)
		}
	})

	t.Run("unknown status fails", func(t *testing.T) {
		t.Parallel()
		activities := processingActivityRegistry()
		activities[0].LawfulBasis = "maybe"
		hydrateRecipientProviders(activities, definitions)
		err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "unknown lawful-basis status") {
			t.Fatalf("error = %v, want unknown-status error", err)
		}
	})
}

func TestProcessingActivityRegistryDPIAValidation(t *testing.T) {
	t.Parallel()
	definitions := retentionRegistry()

	t.Run("pending with approval evidence fails", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		activities[0].DPIA.ApprovedBy = "founder"
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "pending DPIA outcome but carries approval evidence") {
			t.Fatalf("error = %v, want pending-with-approval error", err)
		}
	})

	t.Run("required without evidence fails", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		activities[0].DPIA.Outcome = DPIAOutcomeRequired
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "without complete owner, reason, review, and approval evidence") {
			t.Fatalf("error = %v, want incomplete-evidence error", err)
		}
	})

	t.Run("stale DPIA review fails", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		stale := now.Add(-200 * 24 * time.Hour)
		activities[0].DPIA.Outcome = DPIAOutcomeNotRequired
		activities[0].DPIA.Owner = "privacy-owner"
		activities[0].DPIA.ApprovedBy = "founder"
		activities[0].DPIA.ReviewedAt = &stale
		approved := now.Add(-190 * 24 * time.Hour)
		activities[0].DPIA.ApprovedAt = &approved
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "stale review") {
			t.Fatalf("error = %v, want stale-review error", err)
		}
	})

	t.Run("approval before review fails", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		reviewed := now.Add(-time.Hour)
		approved := now.Add(-2 * time.Hour)
		activities[0].DPIA.Outcome = DPIAOutcomeRequired
		activities[0].DPIA.Owner = "privacy-owner"
		activities[0].DPIA.ApprovedBy = "founder"
		activities[0].DPIA.ReviewedAt = &reviewed
		activities[0].DPIA.ApprovedAt = &approved
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "before its reviewed-at timestamp") {
			t.Fatalf("error = %v, want approval-before-review error", err)
		}
	})

	t.Run("unknown risk indicator fails", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		activities[0].DPIA.RiskIndicators = []DPIARiskIndicator{"invented_risk"}
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "unknown DPIA risk indicator") {
			t.Fatalf("error = %v, want unknown-risk-indicator error", err)
		}
	})

	t.Run("fully evidenced not_required outcome and identified basis makes activity publication ready", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		reviewed := now.Add(-time.Hour)
		approved := now.Add(-30 * time.Minute)
		activities[0].LawfulBasis = LawfulBasisIdentified
		activities[0].LawfulBasisGrounds = []LawfulBasisGround{LawfulBasisGroundLegitimateInterest}
		activities[0].LawfulBasisReason = "Recorded founder/legal determination."
		activities[0].Owner = "privacy-owner"
		activities[0].ReviewedBy = "founder"
		activities[0].ReviewedAt = &reviewed
		activities[0].DPIA.Outcome = DPIAOutcomeNotRequired
		activities[0].DPIA.Owner = "privacy-owner"
		activities[0].DPIA.ApprovedBy = "founder"
		activities[0].DPIA.Reason = "Recorded founder/legal determination."
		activities[0].DPIA.ReviewedAt = &reviewed
		activities[0].DPIA.ApprovedAt = &approved
		hydrateRecipientProviders(activities, definitions)
		if err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now); err != nil {
			t.Fatalf("validateProcessingActivityRegistry error = %v", err)
		}
		if !processingActivityPublicationReady(activities[0], now) {
			t.Fatalf("activity %q should be publication ready: %#v", activities[0].ActivityID, activities[0])
		}
	})

	t.Run("required outcome never makes an activity publication ready even with full evidence", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		reviewed := now.Add(-time.Hour)
		approved := now.Add(-30 * time.Minute)
		activities[0].LawfulBasis = LawfulBasisIdentified
		activities[0].LawfulBasisGrounds = []LawfulBasisGround{LawfulBasisGroundLegitimateInterest}
		activities[0].LawfulBasisReason = "Recorded founder/legal determination."
		activities[0].Owner = "privacy-owner"
		activities[0].ReviewedBy = "founder"
		activities[0].ReviewedAt = &reviewed
		// Outcome required is a screening conclusion, not evidence that the required DPIA was
		// actually completed; every other field is deliberately as complete as the not_required
		// case above.
		activities[0].DPIA.Outcome = DPIAOutcomeRequired
		activities[0].DPIA.Owner = "privacy-owner"
		activities[0].DPIA.ApprovedBy = "founder"
		activities[0].DPIA.Reason = "Recorded founder/legal determination."
		activities[0].DPIA.ReviewedAt = &reviewed
		activities[0].DPIA.ApprovedAt = &approved
		hydrateRecipientProviders(activities, definitions)
		if err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now); err != nil {
			t.Fatalf("validateProcessingActivityRegistry error = %v", err)
		}
		if processingActivityPublicationReady(activities[0], now) {
			t.Fatalf(
				"activity %q with a required DPIA outcome must never be publication ready: %#v",
				activities[0].ActivityID, activities[0],
			)
		}
	})
}

func TestProcessingActivityRegistryPublicationReadyCannotBePreset(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	activities[0].PublicationReady = true
	hydrateRecipientProviders(activities, definitions)
	err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
	if err == nil || !strings.Contains(err.Error(), "asserts publication readiness in its source literal") {
		t.Fatalf("error = %v, want preset-publication-ready error", err)
	}
}

func TestProcessingActivityRegistryReviewerPairingValidation(t *testing.T) {
	t.Parallel()
	now := time.Now()
	definitions := retentionRegistry()

	t.Run("named reviewer without reviewed-at fails", func(t *testing.T) {
		t.Parallel()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		activities[0].ReviewedBy = "founder"
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "names a reviewer without a reviewed-at timestamp") {
			t.Fatalf("error = %v, want reviewer-without-timestamp error", err)
		}
	})

	t.Run("reviewed-at without named reviewer fails", func(t *testing.T) {
		t.Parallel()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		reviewed := now.Add(-time.Hour)
		activities[0].ReviewedAt = &reviewed
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "reviewed-at timestamp without a named reviewer") {
			t.Fatalf("error = %v, want timestamp-without-reviewer error", err)
		}
	})

	t.Run("blank reviewer fails field completeness", func(t *testing.T) {
		t.Parallel()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		activities[0].ReviewedBy = ""
		hydrateRecipientProviders(activities, definitions)
		err = validateProcessingActivityRegistry(activities, definitions, governance.Providers, now)
		if err == nil || !strings.Contains(err.Error(), "is incomplete") {
			t.Fatalf("error = %v, want incomplete error", err)
		}
	})

	t.Run("identified lawful basis without a named reviewer is not publication ready", func(t *testing.T) {
		t.Parallel()
		governance, err := LoadProviderGovernance(now)
		if err != nil {
			t.Fatalf("LoadProviderGovernance error = %v", err)
		}
		activities := processingActivityRegistry()
		reviewed := now.Add(-time.Hour)
		approved := now.Add(-30 * time.Minute)
		activities[0].LawfulBasis = LawfulBasisIdentified
		activities[0].LawfulBasisGrounds = []LawfulBasisGround{LawfulBasisGroundLegitimateInterest}
		activities[0].LawfulBasisReason = "Recorded founder/legal determination."
		activities[0].Owner = "privacy-owner"
		// ReviewedBy intentionally left at the unassigned sentinel with no ReviewedAt: this is a
		// consistent (both absent) but incomplete review, so it must fail readiness, not validation.
		activities[0].DPIA.Outcome = DPIAOutcomeNotRequired
		activities[0].DPIA.Owner = "privacy-owner"
		activities[0].DPIA.ApprovedBy = "founder"
		activities[0].DPIA.Reason = "Recorded founder/legal determination."
		activities[0].DPIA.ReviewedAt = &reviewed
		activities[0].DPIA.ApprovedAt = &approved
		hydrateRecipientProviders(activities, definitions)
		if err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now); err != nil {
			t.Fatalf("validateProcessingActivityRegistry error = %v", err)
		}
		if processingActivityPublicationReady(activities[0], now) {
			t.Fatalf(
				"activity %q without a named reviewer must not be publication ready: %#v",
				activities[0].ActivityID, activities[0],
			)
		}
	})
}

func TestProcessingActivityRegistryDataSubjectCoverage(t *testing.T) {
	t.Parallel()
	activities := processingActivityRegistry()
	byID := make(map[string]ProcessingActivityRecord, len(activities))
	for _, activity := range activities {
		byID[activity.ActivityID] = activity
	}
	hasSubject := func(activityID string, subject ROPADataSubjectClass) bool {
		activity, ok := byID[activityID]
		if !ok {
			t.Fatalf("activity %q not found", activityID)
		}
		return slices.Contains(activity.DataSubjects, subject)
	}

	tests := []struct {
		activityID string
		subject    ROPADataSubjectClass
	}{
		{"feedback_workflow_management", ROPADataSubjectThirdParties},
		{"attachment_evidence_handling", ROPADataSubjectThirdParties},
		{"ai_assisted_triage", ROPADataSubjectThirdParties},
		{"integration_notification_delivery", ROPADataSubjectReporters},
		{"privacy_rights_operations", ROPADataSubjectThirdParties},
		{"breach_response_recordkeeping", ROPADataSubjectWebsiteVisitors},
		{"breach_response_recordkeeping", ROPADataSubjectReporters},
		{"breach_response_recordkeeping", ROPADataSubjectPlatformAdmins},
		{"breach_response_recordkeeping", ROPADataSubjectThirdParties},
		{"external_communications_delivery", ROPADataSubjectPlatformAdmins},
		{"external_communications_delivery", ROPADataSubjectBillingContacts},
		{"infrastructure_and_platform_operations", ROPADataSubjectBillingContacts},
		{"infrastructure_and_platform_operations", ROPADataSubjectThirdParties},
	}
	for _, test := range tests {
		if !hasSubject(test.activityID, test.subject) {
			t.Fatalf("activity %q is missing data-subject class %q", test.activityID, test.subject)
		}
	}
}

func TestProcessingActivityRegistryAcceptsPreventsRightsOrServiceUseRiskIndicator(t *testing.T) {
	t.Parallel()
	now := time.Now()
	governance, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	definitions := retentionRegistry()
	activities := processingActivityRegistry()
	activities[0].DPIA.RiskIndicators = append(
		activities[0].DPIA.RiskIndicators, DPIARiskPreventsRightsOrServiceUse,
	)
	hydrateRecipientProviders(activities, definitions)
	if err := validateProcessingActivityRegistry(activities, definitions, governance.Providers, now); err != nil {
		t.Fatalf("validateProcessingActivityRegistry error = %v", err)
	}
}

func TestLoadProcessingActivityRegistryGovernanceRejectsZeroValidationTime(t *testing.T) {
	t.Parallel()
	if _, err := LoadProcessingActivityRegistry(time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "validation time must not be zero") {
		t.Fatalf("LoadProcessingActivityRegistry error = %v, want zero-time error", err)
	}
}
