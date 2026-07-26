package privacy

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLoadProviderGovernanceReturnsPendingRegistry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	registry, err := LoadProviderGovernance(now)
	if err != nil {
		t.Fatalf("LoadProviderGovernance error = %v", err)
	}
	if registry.Version != governanceRegistryVersion {
		t.Fatalf("Version = %d, want %d", registry.Version, governanceRegistryVersion)
	}
	if len(registry.Providers) != 15 {
		t.Fatalf("len(Providers) = %d, want 15", len(registry.Providers))
	}
	if registry.Summary != (GovernanceSummary{PendingApproval: 15, ContractUnknown: 15}) {
		t.Fatalf("Summary = %#v, want all pending/unknown", registry.Summary)
	}
	if len(registry.RoleMatrix) != len(retentionRegistry()) {
		t.Fatalf(
			"len(RoleMatrix) = %d, want %d", len(registry.RoleMatrix), len(retentionRegistry()),
		)
	}
	for _, provider := range registry.Providers {
		if provider.LegalEntity != governanceUnknown || provider.TransferMechanism != governanceUnknown {
			t.Fatalf("provider %q claims evidenced legal/transfer facts: %#v", provider.ProviderID, provider)
		}
		if provider.Owner != governanceUnassigned {
			t.Fatalf("provider %q claims an owner: %#v", provider.ProviderID, provider)
		}
		if provider.Approval != GovernanceApprovalPending {
			t.Fatalf("provider %q is not pending: %#v", provider.ProviderID, provider)
		}
		if provider.ReviewedAt != nil || provider.ApprovedAt != nil {
			t.Fatalf("provider %q claims review/approval dates: %#v", provider.ProviderID, provider)
		}
		if strings.TrimSpace(provider.Purpose) == "" {
			t.Fatalf("provider %q has no purpose: %#v", provider.ProviderID, provider)
		}
		if len(provider.PersonalDataCategories) == 0 {
			t.Fatalf("provider %q has no personal-data categories: %#v", provider.ProviderID, provider)
		}
		switch provider.BoundaryKind {
		case GovernanceBoundaryInfrastructureProvider, GovernanceBoundaryServiceProvider,
			GovernanceBoundaryCustomerConfiguredDestination, GovernanceBoundaryInternalRuntime:
		default:
			t.Fatalf("provider %q has unknown boundary kind %q", provider.ProviderID, provider.BoundaryKind)
		}
	}
}

func TestLoadProviderGovernanceRejectsZeroValidationTime(t *testing.T) {
	t.Parallel()
	if _, err := LoadProviderGovernance(time.Time{}); err == nil ||
		!strings.Contains(err.Error(), "validation time must not be zero") {
		t.Fatalf("LoadProviderGovernance error = %v, want zero-time error", err)
	}
}

func TestProviderGovernanceNotEveryEntryIsASubprocessor(t *testing.T) {
	t.Parallel()
	byID := make(map[string]ProviderGovernanceRecord)
	for _, provider := range providerGovernanceRegistry() {
		byID[provider.ProviderID] = provider
	}
	tests := []struct {
		providerID string
		want       GovernanceBoundaryKind
	}{
		{"email_capture_filesystem", GovernanceBoundaryInternalRuntime},
		{"slack_incoming_webhook", GovernanceBoundaryCustomerConfiguredDestination},
		{"microsoft_teams_incoming_webhook", GovernanceBoundaryCustomerConfiguredDestination},
		{"customer_generic_webhook", GovernanceBoundaryCustomerConfiguredDestination},
		{"postgresql_runtime", GovernanceBoundaryInfrastructureProvider},
		{"redis_asynq", GovernanceBoundaryInfrastructureProvider},
		{"s3_compatible_object_storage", GovernanceBoundaryInfrastructureProvider},
		{"cloudflare_turnstile", GovernanceBoundaryServiceProvider},
	}
	for _, test := range tests {
		provider, ok := byID[test.providerID]
		if !ok {
			t.Fatalf("provider %q not found", test.providerID)
		}
		if provider.BoundaryKind != test.want {
			t.Fatalf("provider %q BoundaryKind = %q, want %q", test.providerID, provider.BoundaryKind, test.want)
		}
	}
}

func TestProviderGovernanceCoversTurnstileAsAnExternalBoundary(t *testing.T) {
	t.Parallel()
	var found bool
	for _, provider := range providerGovernanceRegistry() {
		if provider.ProviderID != "cloudflare_turnstile" {
			continue
		}
		found = true
		if provider.BoundaryKind != GovernanceBoundaryServiceProvider {
			t.Fatalf("cloudflare_turnstile BoundaryKind = %q, want service_provider", provider.BoundaryKind)
		}
		if provider.EnvironmentEnablement != GovernanceEnvironmentStagingProduction {
			t.Fatalf(
				"cloudflare_turnstile EnvironmentEnablement = %q, want required_in_staging_and_production",
				provider.EnvironmentEnablement,
			)
		}
		if provider.RetentionReviewRef != "security_verification" {
			t.Fatalf(
				"cloudflare_turnstile RetentionReviewRef = %q, want security_verification",
				provider.RetentionReviewRef,
			)
		}
	}
	if !found {
		t.Fatal("cloudflare_turnstile is missing from the provider governance registry")
	}
}

func TestProviderGovernanceSentryCoversBackendAndWidget(t *testing.T) {
	t.Parallel()
	var sentry ProviderGovernanceRecord
	for _, provider := range providerGovernanceRegistry() {
		if provider.ProviderID == "sentry_error_telemetry" {
			sentry = provider
			break
		}
	}
	if sentry.ProviderID == "" {
		t.Fatal("sentry_error_telemetry provider is missing")
	}
	if !strings.Contains(sentry.CodeEvidence, "widget/src/modules/error-reporting.ts") {
		t.Errorf("Sentry code evidence omits the widget path: %q", sentry.CodeEvidence)
	}
	categories := strings.Join(sentry.PersonalDataCategories, " ")
	for _, boundary := range []string{"backend error", "widget error"} {
		if !strings.Contains(categories, boundary) {
			t.Errorf("Sentry personal-data categories omit %q: %#v", boundary, sentry.PersonalDataCategories)
		}
	}
}

func TestProviderGovernanceCoversExactlyTheEvidencedProviderSet(t *testing.T) {
	t.Parallel()
	now := time.Now()
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)

	t.Run("missing evidenced provider fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		truncated := append([]ProviderGovernanceRecord(nil), providers[:len(providers)-1]...)
		err := validateProviderGovernance(truncated, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "missing from the provider governance registry") {
			t.Fatalf("error = %v, want missing-provider error", err)
		}
	})

	t.Run("unevidenced provider fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		bogus := pendingProvider(
			"fictional_analytics_provider", "Fictional analytics provider", "analytics",
			GovernanceBoundaryServiceProvider, "Analyze fictional data.",
			[]string{"fictional analytics identifiers"},
			"nowhere in the codebase", "no environment signal exists",
			GovernanceEnvironmentOptionalNeverRequired, "external_infrastructure",
		)
		providers = append(providers, bogus)
		err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "not an evidenced retention-registry provider") {
			t.Fatalf("error = %v, want unevidenced-provider error", err)
		}
	})

	t.Run("duplicate provider fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		providers = append(providers, providers[0])
		err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "duplicate provider governance entry") {
			t.Fatalf("error = %v, want duplicate error", err)
		}
	})
}

func TestProviderGovernanceRequiredFieldsMissingFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)

	providers := providerGovernanceRegistry()
	providers[0].DisplayName = ""
	err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
	if err == nil || !strings.Contains(err.Error(), "is incomplete") {
		t.Fatalf("error = %v, want incomplete error", err)
	}
}

func TestProviderGovernanceUnknownRetentionReviewRefFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)

	providers := providerGovernanceRegistry()
	providers[0].RetentionReviewRef = "not_a_real_category"
	err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
	if err == nil || !strings.Contains(err.Error(), "references unknown retention category") {
		t.Fatalf("error = %v, want unknown-category error", err)
	}
}

func TestProviderGovernanceUnknownBoundaryKindFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)

	providers := providerGovernanceRegistry()
	providers[0].BoundaryKind = "third_party_reseller"
	err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
	if err == nil || !strings.Contains(err.Error(), "unknown boundary kind") {
		t.Fatalf("error = %v, want unknown-boundary-kind error", err)
	}
}

func TestProviderGovernancePersonalDataCategoriesValidation(t *testing.T) {
	t.Parallel()
	now := time.Now()
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)

	t.Run("missing categories fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		providers[0].PersonalDataCategories = nil
		err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "no personal-data categories") {
			t.Fatalf("error = %v, want no-categories error", err)
		}
	})

	t.Run("blank category fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		providers[0].PersonalDataCategories = []string{"  "}
		err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "blank personal-data category") {
			t.Fatalf("error = %v, want blank-category error", err)
		}
	})

	t.Run("duplicate category fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		providers[0].PersonalDataCategories = []string{"Same", " same "}
		err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "duplicate personal-data category") {
			t.Fatalf("error = %v, want duplicate-category error", err)
		}
	})

	t.Run("too many categories fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		providers[0].PersonalDataCategories = make([]string, maxGovernancePersonalDataCategories+1)
		for index := range providers[0].PersonalDataCategories {
			providers[0].PersonalDataCategories[index] = fmt.Sprintf("category %d", index)
		}
		err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "too many personal-data categories") {
			t.Fatalf("error = %v, want category-count error", err)
		}
	})

	t.Run("oversized category fails", func(t *testing.T) {
		t.Parallel()
		providers := providerGovernanceRegistry()
		providers[0].PersonalDataCategories = []string{
			strings.Repeat("a", maxGovernancePersonalDataCategoryLen+1),
		}
		err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
		if err == nil || !strings.Contains(err.Error(), "oversized personal-data category") {
			t.Fatalf("error = %v, want oversized-category error", err)
		}
	})
}

func TestProviderGovernancePendingApprovalEvidenceFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)
	approvedAt := now.Add(-24 * time.Hour)

	tests := []struct {
		name   string
		mutate func([]ProviderGovernanceRecord)
	}{
		{"approved-by", func(providers []ProviderGovernanceRecord) {
			providers[0].ApprovedBy = "founder"
		}},
		{"approved-at", func(providers []ProviderGovernanceRecord) {
			providers[0].ApprovedAt = &approvedAt
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			providers := providerGovernanceRegistry()
			test.mutate(providers)
			err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
			if err == nil || !strings.Contains(err.Error(), "pending but carries approval evidence") {
				t.Fatalf("error = %v, want pending-approval-evidence error", err)
			}
		})
	}
}

func TestProviderGovernanceFalselyApprovedIncompleteEntryFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)

	providers := providerGovernanceRegistry()
	providers[0].Approval = GovernanceApprovalApproved
	err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
	if err == nil || !strings.Contains(err.Error(), "approved without complete") {
		t.Fatalf("error = %v, want falsely-approved error", err)
	}
}

func TestProviderGovernancePlaceholderBypassFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)
	reviewedAt := now.Add(-30 * 24 * time.Hour)
	approvedAt := now.Add(-29 * 24 * time.Hour)

	approvedComplete := func() []ProviderGovernanceRecord {
		providers := providerGovernanceRegistry()
		providers[0].LegalEntity = "Acme Cloud Hosting, Inc."
		providers[0].ProcessingLocations = []string{"European Union"}
		providers[0].TransferMechanism = "not_applicable_same_region"
		providers[0].ContractStatus = GovernanceContractExecuted
		providers[0].Owner = "founder"
		providers[0].ReviewedAt = &reviewedAt
		providers[0].Approval = GovernanceApprovalApproved
		providers[0].ApprovedBy = "founder"
		providers[0].ApprovedAt = &approvedAt
		return providers
	}

	tests := []struct {
		name   string
		mutate func([]ProviderGovernanceRecord)
	}{
		{"whitespace-padded unknown legal entity", func(p []ProviderGovernanceRecord) {
			p[0].LegalEntity = " unknown "
		}},
		{"uppercase unknown legal entity", func(p []ProviderGovernanceRecord) {
			p[0].LegalEntity = "UNKNOWN"
		}},
		{"whitespace-padded unassigned owner", func(p []ProviderGovernanceRecord) {
			p[0].Owner = "unassigned "
		}},
		{"mixed-case unassigned owner", func(p []ProviderGovernanceRecord) {
			p[0].Owner = "Unassigned"
		}},
		{"whitespace-padded unknown transfer mechanism", func(p []ProviderGovernanceRecord) {
			p[0].TransferMechanism = " Unknown"
		}},
		{"placeholder approved-by", func(p []ProviderGovernanceRecord) {
			p[0].ApprovedBy = " unassigned "
		}},
		{"placeholder processing location", func(p []ProviderGovernanceRecord) {
			p[0].ProcessingLocations = []string{" Unknown "}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			providers := approvedComplete()
			test.mutate(providers)
			err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
			if err == nil || !strings.Contains(err.Error(), "approved without complete") {
				t.Fatalf("error = %v, want falsely-approved error", err)
			}
		})
	}
}

func TestProviderGovernanceApprovedCompleteEntrySucceeds(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)
	reviewedAt := now.Add(-30 * 24 * time.Hour)
	approvedAt := now.Add(-29 * 24 * time.Hour)

	providers := providerGovernanceRegistry()
	providers[0].LegalEntity = "Acme Cloud Hosting, Inc."
	providers[0].ProcessingLocations = []string{"European Union"}
	providers[0].TransferMechanism = "not_applicable_same_region"
	providers[0].ContractStatus = GovernanceContractExecuted
	providers[0].Owner = "founder"
	providers[0].ReviewedAt = &reviewedAt
	providers[0].Approval = GovernanceApprovalApproved
	providers[0].ApprovedBy = "founder"
	providers[0].ApprovedAt = &approvedAt

	if err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now); err != nil {
		t.Fatalf("validateProviderGovernance error = %v, want a complete approved entry to pass", err)
	}
}

func TestProviderGovernanceStaleReviewFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)
	stale := now.Add(-200 * 24 * time.Hour)

	providers := providerGovernanceRegistry()
	providers[0].ReviewedAt = &stale
	err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
	if err == nil || !strings.Contains(err.Error(), "stale review") {
		t.Fatalf("error = %v, want stale-review error", err)
	}
}

func TestProviderGovernanceTimestampValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	retentionDefinitions := retentionRegistry()
	roleMatrix := governanceRoleMatrix(retentionDefinitions)
	future := now.Add(24 * time.Hour)
	var zero time.Time
	reviewedAt := now.Add(-30 * 24 * time.Hour)
	approvedBeforeReviewed := now.Add(-31 * 24 * time.Hour)

	tests := []struct {
		name   string
		mutate func([]ProviderGovernanceRecord)
		want   string
	}{
		{"zero reviewed-at", func(p []ProviderGovernanceRecord) {
			p[0].ReviewedAt = &zero
		}, "invalid reviewed-at timestamp"},
		{"future reviewed-at", func(p []ProviderGovernanceRecord) {
			p[0].ReviewedAt = &future
		}, "invalid reviewed-at timestamp"},
		{"zero approved-at", func(p []ProviderGovernanceRecord) {
			p[0].Approval = GovernanceApprovalApproved
			p[0].ReviewedAt = &reviewedAt
			p[0].ApprovedAt = &zero
		}, "invalid approved-at timestamp"},
		{"future approved-at", func(p []ProviderGovernanceRecord) {
			p[0].Approval = GovernanceApprovalApproved
			p[0].ReviewedAt = &reviewedAt
			p[0].ApprovedAt = &future
		}, "invalid approved-at timestamp"},
		{"approved-at before reviewed-at", func(p []ProviderGovernanceRecord) {
			p[0].Approval = GovernanceApprovalApproved
			p[0].ReviewedAt = &reviewedAt
			p[0].ApprovedAt = &approvedBeforeReviewed
		}, "approved-at timestamp before its reviewed-at timestamp"},
		{"approved-at without reviewed-at", func(p []ProviderGovernanceRecord) {
			p[0].Approval = GovernanceApprovalApproved
			p[0].ApprovedAt = &reviewedAt
		}, "approved-at timestamp without a reviewed-at timestamp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			providers := providerGovernanceRegistry()
			test.mutate(providers)
			err := validateProviderGovernance(providers, retentionDefinitions, roleMatrix, now)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestGovernanceRoleMatrixCoversEveryRetentionCategory(t *testing.T) {
	t.Parallel()
	retentionDefinitions := retentionRegistry()
	matrix := governanceRoleMatrix(retentionDefinitions)
	if len(matrix) != len(retentionDefinitions) {
		t.Fatalf("len(matrix) = %d, want %d", len(matrix), len(retentionDefinitions))
	}
	byCategory := make(map[string]GovernanceRoleMatrixEntry, len(matrix))
	for _, entry := range matrix {
		byCategory[entry.Category] = entry
	}
	tests := []struct {
		category string
		role     RetentionRole
	}{
		{"auth_magic_links", RetentionRoleController},
		{"attachments", RetentionRoleProcessor},
		{"privacy_rights", RetentionRoleControllerProcessor},
		{"billing_records", RetentionRoleController},
		{"security_verification", RetentionRoleController},
	}
	for _, test := range tests {
		entry, ok := byCategory[test.category]
		if !ok {
			t.Fatalf("role matrix is missing category %q", test.category)
		}
		if entry.Role != test.role {
			t.Fatalf("category %q role = %q, want %q", test.category, entry.Role, test.role)
		}
	}
}

func TestGovernanceRoleMatrixMissingCategoryFails(t *testing.T) {
	t.Parallel()
	now := time.Now()
	retentionDefinitions := retentionRegistry()
	fullMatrix := governanceRoleMatrix(retentionDefinitions)
	truncatedMatrix := append([]GovernanceRoleMatrixEntry(nil), fullMatrix[:len(fullMatrix)-1]...)

	err := validateProviderGovernance(
		providerGovernanceRegistry(), retentionDefinitions, truncatedMatrix, now,
	)
	if err == nil || !strings.Contains(err.Error(), "is missing from the role matrix") {
		t.Fatalf("error = %v, want role-matrix coverage error", err)
	}
}
